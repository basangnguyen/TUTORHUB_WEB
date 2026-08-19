import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

const GATE_URL = "http://127.0.0.1:4179";

test.describe("P5-COLLAB-01 tldraw official sync automated gates", () => {
  test("hai browser hội tụ và undo chỉ đảo thao tác của actor cục bộ", async ({
    browser,
  }) => {
    const documentId = uniqueDocument("convergence");
    const firstContext = await browser.newContext();
    const secondContext = await browser.newContext();
    const first = await firstContext.newPage();
    const second = await secondContext.newPage();

    await Promise.all([
      openEditor(first, documentId, "editor-first"),
      openEditor(second, documentId, "editor-second"),
    ]);
    await first.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("from-first", 100, 120),
    );
    await second.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("from-second", 360, 120),
    );

    await expect.poll(() => convergence(first, second, 2)).toBe(true);
    await first.evaluate(() => window.__TUTORHUB_WHITEBOARD_GATE__?.undo());
    await expect.poll(() => convergence(first, second, 1)).toBe(true);
    const remainingIds = (await evidence(first)).shapes.map(
      (shape) => shape.id,
    );
    expect(remainingIds).toEqual(["shape:from-second"]);

    await first.evaluate(() => window.__TUTORHUB_WHITEBOARD_GATE__?.redo());
    await expect.poll(() => convergence(first, second, 2)).toBe(true);
    await firstContext.close();
    await secondContext.close();
  });

  test("offline edits hội tụ lại và SQLite sống qua room restart", async ({
    browser,
  }) => {
    const documentId = uniqueDocument("reconnect");
    const firstContext = await browser.newContext();
    const secondContext = await browser.newContext();
    const first = await firstContext.newPage();
    const second = await secondContext.newPage();
    await Promise.all([
      openEditor(first, documentId, "editor-offline"),
      openEditor(second, documentId, "editor-online"),
    ]);

    await first.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.goOffline(),
    );
    await expect.poll(() => connectionStatus(first)).toBe("offline");
    await first.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("offline-change", 80, 80),
    );
    await second.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("online-change", 280, 80),
    );
    await first.evaluate(() => window.__TUTORHUB_WHITEBOARD_GATE__?.goOnline());
    await expect.poll(() => convergence(first, second, 2)).toBe(true);

    const restart = await gatePost(second, "/gate/restart", {});
    expect(restart.status).toBe(200);
    await expect
      .poll(() => connectionStatus(first), { timeout: 20_000 })
      .toBe("online");
    await expect
      .poll(() => connectionStatus(second), { timeout: 20_000 })
      .toBe("online");
    await expect.poll(() => convergence(first, second, 2)).toBe(true);
    await firstContext.close();
    await secondContext.close();
  });

  test("grant/capability/origin/tenant/read-only được fail closed", async ({
    browser,
    request,
  }) => {
    const documentId = uniqueDocument("authorization");
    const wrongOrigin = await request.post(`${GATE_URL}/gate/grants`, {
      headers: { origin: "https://attacker.invalid" },
      data: {
        tenantId: "tenant-gate",
        documentId,
        actorId: "tenant-gate:editor-wrong-origin",
        requestedCapability: "edit",
      },
    });
    expect(wrongOrigin.status()).toBe(403);

    const escalation = await request.post(`${GATE_URL}/gate/grants`, {
      headers: { origin: "http://127.0.0.1:4178" },
      data: {
        tenantId: "tenant-gate",
        documentId,
        actorId: "tenant-gate:viewer-escalation",
        requestedCapability: "edit",
      },
    });
    expect(escalation.status()).toBe(403);

    const writerContext = await browser.newContext();
    const viewerContext = await browser.newContext();
    const foreignContext = await browser.newContext();
    const writer = await writerContext.newPage();
    const viewer = await viewerContext.newPage();
    const foreign = await foreignContext.newPage();
    await Promise.all([
      openEditor(writer, documentId, "editor-authorized"),
      openEditor(viewer, documentId, "viewer-readonly", "view"),
      openEditor(foreign, documentId, "editor-foreign", "edit", "tenant-other"),
    ]);

    await viewer.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.forceCreateRect(
        "viewer-forced-write",
      ),
    );
    await expect.poll(async () => (await evidence(writer)).shapeCount).toBe(0);
    await writer.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("tenant-private"),
    );
    await expect.poll(async () => (await evidence(writer)).shapeCount).toBe(1);
    await expect.poll(async () => (await evidence(foreign)).shapeCount).toBe(0);

    const leakage = await writer.evaluate(() => ({
      url: location.href,
      html: document.documentElement.innerHTML,
      local: JSON.stringify({ ...localStorage }),
      session: JSON.stringify({ ...sessionStorage }),
    }));
    expect(JSON.stringify(leakage)).not.toContain("tutorhub-grant.");

    await writerContext.close();
    await viewerContext.close();
    await foreignContext.close();
  });

  test("actor revoke đóng socket hiện tại và chặn grant mới", async ({
    page,
  }) => {
    const documentId = uniqueDocument("revoke");
    await openEditor(page, documentId, "editor-revoked");
    const result = await gatePost(page, "/gate/revoke-actor", {
      tenantId: "tenant-gate",
      documentId,
      actorId: "tenant-gate:editor-revoked",
    });
    expect(result.status).toBe(200);
    await expect(page.getByTestId("store-status")).toHaveText("error", {
      timeout: 15_000,
    });
    expect(await connectionStatus(page)).not.toBe("online");
    const retry = await gatePost(page, "/gate/grants", {
      tenantId: "tenant-gate",
      documentId,
      actorId: "tenant-gate:editor-revoked",
      requestedCapability: "edit",
    });
    expect(retry.status).toBe(403);
  });

  test("snapshot checksum, corrupt denial, generation swap và stale restore", async ({
    page,
  }) => {
    const documentId = uniqueDocument("snapshot");
    await openEditor(page, documentId, "editor-snapshot");
    await page.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("snapshot-base"),
    );
    await expect.poll(async () => (await evidence(page)).shapeCount).toBe(1);
    await expect.poll(() => serverShapeCount(page, documentId)).toBe(1);

    const clean = await gatePost(page, "/gate/snapshots", {
      tenantId: "tenant-gate",
      documentId,
    });
    expect(clean.status).toBe(201);
    const corrupt = await gatePost(page, "/gate/snapshots", {
      tenantId: "tenant-gate",
      documentId,
    });
    expect(corrupt.status).toBe(201);
    expect(
      (
        await gatePost(page, "/gate/corrupt-artifact", {
          artifactId: corrupt.body.artifactId,
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await gatePost(page, "/gate/restore", {
          tenantId: "tenant-gate",
          documentId,
          artifactId: corrupt.body.artifactId,
          expectedGeneration: 1,
        })
      ).status,
    ).toBe(422);

    await page.evaluate(() =>
      window.__TUTORHUB_WHITEBOARD_GATE__?.createRect("after-snapshot"),
    );
    await expect.poll(async () => (await evidence(page)).shapeCount).toBe(2);
    await expect.poll(() => serverShapeCount(page, documentId)).toBe(2);
    const restored = await gatePost(page, "/gate/restore", {
      tenantId: "tenant-gate",
      documentId,
      artifactId: clean.body.artifactId,
      expectedGeneration: 1,
    });
    expect(restored.status).toBe(200);
    expect(restored.body.generation).toBe(2);
    await expect(page.getByTestId("store-status")).toHaveText("error", {
      timeout: 20_000,
    });
    expect(await connectionStatus(page)).not.toBe("online");
    await page.reload();
    await expect(page.getByTestId("store-status")).toHaveText("synced-remote", {
      timeout: 30_000,
    });
    await expect
      .poll(() =>
        page.evaluate(() => Boolean(window.__TUTORHUB_WHITEBOARD_GATE__)),
      )
      .toBe(true);
    await expect.poll(async () => (await evidence(page)).shapeCount).toBe(1);
    expect((await evidence(page)).shapes.map((shape) => shape.id)).toEqual([
      "shape:snapshot-base",
    ]);

    const stale = await gatePost(page, "/gate/restore", {
      tenantId: "tenant-gate",
      documentId,
      artifactId: clean.body.artifactId,
      expectedGeneration: 1,
    });
    expect(stale.status).toBe(409);
  });

  test("shell cộng tác đạt axe, keyboard focus, 200% và forced colors", async ({
    page,
  }) => {
    await openEditor(page, uniqueDocument("a11y"), "editor-accessibility");
    const results = await new AxeBuilder({ page })
      .include(".collab-header")
      .include(".gate-note")
      .analyze();
    expect(results.violations).toEqual([]);
    await page.emulateMedia({
      forcedColors: "active",
      reducedMotion: "reduce",
    });
    await page.evaluate(() => {
      document.documentElement.style.zoom = "2";
    });
    await expect(
      page.getByRole("heading", { name: "tldraw official sync acceptance" }),
    ).toBeVisible();
    await page.keyboard.press("Tab");
    expect(
      await page.evaluate(() => document.activeElement !== document.body),
    ).toBe(true);
  });
});

async function openEditor(
  page: Page,
  documentId: string,
  persona: string,
  capability: "view" | "edit" | "present" = "edit",
  tenantId = "tenant-gate",
) {
  await page.goto(
    `/?mode=collab&tenant=${tenantId}&document=${documentId}&actor=${tenantId}:${persona}&capability=${capability}`,
  );
  await expect(page.getByTestId("store-status")).toHaveText("synced-remote", {
    timeout: 30_000,
  });
  await expect
    .poll(() =>
      page.evaluate(() => Boolean(window.__TUTORHUB_WHITEBOARD_GATE__)),
    )
    .toBe(true);
}

async function evidence(page: Page) {
  return page.evaluate(() => {
    const api = window.__TUTORHUB_WHITEBOARD_GATE__;
    if (!api) throw new Error("gate_api_not_ready");
    return api.evidence();
  });
}

async function convergence(first: Page, second: Page, shapeCount: number) {
  const [left, right] = await Promise.all([evidence(first), evidence(second)]);
  return (
    left.shapeCount === shapeCount &&
    right.shapeCount === shapeCount &&
    left.digest === right.digest
  );
}

async function connectionStatus(page: Page) {
  return page.evaluate(
    () => window.__TUTORHUB_WHITEBOARD_GATE__?.connectionStatus() ?? "missing",
  );
}

async function gatePost(
  page: Page,
  path: string,
  body: Record<string, unknown>,
): Promise<{ status: number; body: Record<string, unknown> }> {
  return page.evaluate(
    async ({ url, payload }) => {
      const response = await fetch(url, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(payload),
      });
      return {
        status: response.status,
        body: (await response.json()) as Record<string, unknown>,
      };
    },
    { url: `${GATE_URL}${path}`, payload: body },
  );
}

async function serverShapeCount(
  page: Page,
  documentId: string,
): Promise<number> {
  const status = await gatePost(page, "/gate/status", {
    tenantId: "tenant-gate",
    documentId,
  });
  if (status.status !== 200 || typeof status.body.shapeCount !== "number") {
    return -1;
  }
  return status.body.shapeCount;
}

function uniqueDocument(prefix: string): string {
  return `document-${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
}
