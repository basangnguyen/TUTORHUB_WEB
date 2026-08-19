import { expect, test, type Page } from "@playwright/test";

interface LoadMetric {
  clients: 2 | 10 | 50;
  shapes: 500 | 2000;
  joinMs: number;
  convergenceMs: number;
  heapBytes: number;
}

const profiles: Array<Pick<LoadMetric, "clients" | "shapes">> = [
  { clients: 2, shapes: 500 },
  { clients: 10, shapes: 500 },
  { clients: 50, shapes: 2000 },
];

test("official sync profile 2/10/50 hội tụ với fixture 500/2.000", async ({
  page,
}) => {
  test.setTimeout(180_000);
  const metrics: LoadMetric[] = [];

  for (const profile of profiles) {
    const documentId = `document-load-${profile.clients}-${Date.now().toString(36)}`;
    const startedAt = performance.now();
    await page.goto(
      `/?mode=load&clients=${profile.clients}&tenant=tenant-gate&document=${documentId}`,
    );
    await expect
      .poll(() => loadEvidence(page, "connectedCount"), { timeout: 60_000 })
      .toBe(profile.clients);
    await expect
      .poll(() => loadEvidence(page, "writerReady"), { timeout: 30_000 })
      .toBe(true);
    const joinedAt = performance.now();

    const created = await page.evaluate((count) => {
      const api = window.__TUTORHUB_WHITEBOARD_LOAD_GATE__;
      if (!api) throw new Error("load_gate_not_ready");
      return api.createFixture(count);
    }, profile.shapes);
    expect(created).toBe(profile.shapes);
    await expect
      .poll(() => allShapeCounts(page, profile.clients, profile.shapes), {
        timeout: 90_000,
      })
      .toBe(true);
    const convergedAt = performance.now();
    const heapBytes = await page.evaluate(() => {
      const memory = (
        performance as Performance & {
          memory?: { usedJSHeapSize: number };
        }
      ).memory;
      return memory?.usedJSHeapSize ?? -1;
    });

    metrics.push({
      ...profile,
      joinMs: Math.round(joinedAt - startedAt),
      convergenceMs: Math.round(convergedAt - joinedAt),
      heapBytes,
    });

    await page.goto("/");
    await expect
      .poll(() => activeSessions(page, documentId), { timeout: 20_000 })
      .toBe(0);
  }

  console.log(`P5_TLDRAW_SYNC_LOAD ${JSON.stringify(metrics)}`);
  await test.info().attach("p5-tldraw-sync-load.json", {
    body: JSON.stringify(metrics, null, 2),
    contentType: "application/json",
  });
  expect(metrics.every((metric) => metric.joinMs < 60_000)).toBe(true);
  expect(metrics.every((metric) => metric.convergenceMs < 90_000)).toBe(true);
});

async function loadEvidence(
  page: Page,
  field: "connectedCount" | "writerReady",
): Promise<number | boolean> {
  return page.evaluate((name) => {
    const api = window.__TUTORHUB_WHITEBOARD_LOAD_GATE__;
    if (!api) return name === "writerReady" ? false : -1;
    return api[name]();
  }, field);
}

async function allShapeCounts(
  page: Page,
  clients: number,
  shapes: number,
): Promise<boolean> {
  return page.evaluate(
    ({ expectedClients, expectedShapes }) => {
      const counts =
        window.__TUTORHUB_WHITEBOARD_LOAD_GATE__?.shapeCounts() ?? [];
      return (
        counts.length === expectedClients &&
        counts.every((count) => count === expectedShapes)
      );
    },
    { expectedClients: clients, expectedShapes: shapes },
  );
}

async function activeSessions(page: Page, documentId: string): Promise<number> {
  return page.evaluate(
    async ({ gateUrl, currentDocument }) => {
      const response = await fetch(`${gateUrl}/gate/status`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          tenantId: "tenant-gate",
          documentId: currentDocument,
        }),
      });
      if (!response.ok) return -1;
      const body = (await response.json()) as { activeSessions?: number };
      return body.activeSessions ?? -1;
    },
    { gateUrl: "http://127.0.0.1:4179", currentDocument: documentId },
  );
}
