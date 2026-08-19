import { expect, test } from "@playwright/test";

const engineAsset = /\/assets\/ExcalidrawBoard-[^/]+\.js(?:\?|$)/;

test("initial shell excludes Excalidraw and loads it only after user action", async ({
  page,
}) => {
  test.setTimeout(60_000);
  const requests: string[] = [];
  const runtimeErrors: string[] = [];
  page.on("request", (request) => requests.push(request.url()));
  page.on("pageerror", (error) => runtimeErrors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") runtimeErrors.push(message.text());
  });

  await page.goto("/excalidraw.html?fixture=500");
  await expect(page.getByTestId("candidate-placeholder")).toBeVisible();
  await expect(page.getByTestId("excalidraw-canvas")).toHaveCount(0);
  expect(requests.some((url) => engineAsset.test(url))).toBe(false);

  const engineRequest = page.waitForRequest((request) =>
    engineAsset.test(request.url()),
  );
  await page.getByRole("button", { name: "Mở bảng Excalidraw" }).click();
  await engineRequest;
  expect(requests.some((url) => engineAsset.test(url))).toBe(true);
  await expect(page.getByRole("status")).toContainText(
    "Excalidraw sẵn sàng với 500 đối tượng.",
  );
  await expect(page.getByTestId("excalidraw-canvas")).toBeVisible();

  await page.locator(".default-sidebar-trigger").click();
  await expect(page.locator(".sidebar-tabs-root")).toBeVisible();
  await expect(page.getByRole("tablist")).toBeVisible();
  expect(runtimeErrors).toEqual([]);
});

test("two Excalidraw browsers converge and canonical undo preserves the remote actor", async ({
  browser,
}) => {
  test.setTimeout(120_000);
  const providerUrl = process.env.P5_GATE_B_PROVIDER_URL;
  expect(providerUrl).toBeTruthy();
  const context = await browser.newContext({ locale: "vi-VN" });
  const teacher = await context.newPage();
  const student = await context.newPage();
  const runtimeErrors: string[] = [];
  for (const page of [teacher, student]) {
    page.on("pageerror", (error) => runtimeErrors.push(error.message));
    page.on("console", (message) => {
      if (message.type() === "error") runtimeErrors.push(message.text());
    });
  }

  const teacherQuery = new URLSearchParams({
    actor: "teacher-a",
    bootstrap: "1",
    collab: "1",
    provider: providerUrl as string,
  });
  const studentQuery = new URLSearchParams({
    actor: "student-b",
    collab: "1",
    provider: providerUrl as string,
  });
  await teacher.goto(`/excalidraw.html?${teacherQuery.toString()}`);
  await expect(teacher.getByTestId("canonical-state")).toHaveAttribute(
    "data-element-count",
    "1",
  );
  await student.goto(`/excalidraw.html?${studentQuery.toString()}`);
  await expect(student.getByTestId("canonical-state")).toHaveAttribute(
    "data-element-count",
    "1",
  );

  await Promise.all([
    teacher.getByRole("button", { name: "Mở bảng Excalidraw" }).click(),
    student.getByRole("button", { name: "Mở bảng Excalidraw" }).click(),
  ]);
  await Promise.all([
    expect(teacher.getByTestId("excalidraw-canvas")).toHaveAttribute(
      "data-rendered-element-count",
      "1",
    ),
    expect(student.getByTestId("excalidraw-canvas")).toHaveAttribute(
      "data-rendered-element-count",
      "1",
    ),
  ]);

  await Promise.all([
    teacher.getByRole("button", { name: "Thêm qua Excalidraw" }).click(),
    student.getByRole("button", { name: "Thêm qua Excalidraw" }).click(),
  ]);
  for (const page of [teacher, student]) {
    await expect(page.getByTestId("canonical-state")).toHaveAttribute(
      "data-element-count",
      "3",
    );
    await expect(page.getByTestId("excalidraw-canvas")).toHaveAttribute(
      "data-rendered-element-count",
      "3",
    );
  }
  await expectCanonicalHashesToMatch(teacher, student);

  await teacher.getByRole("button", { name: "Hoàn tác canonical" }).click();
  for (const page of [teacher, student]) {
    await expect(page.getByTestId("canonical-state")).toHaveAttribute(
      "data-element-count",
      "2",
    );
    await expect(page.getByTestId("canonical-state")).toHaveAttribute(
      "data-element-ids",
      /student-b-shape-1/,
    );
    await expect(page.getByTestId("canonical-state")).not.toHaveAttribute(
      "data-element-ids",
      /teacher-a-shape-1/,
    );
  }
  await expectCanonicalHashesToMatch(teacher, student);

  await teacher.getByRole("button", { name: "Làm lại canonical" }).click();
  for (const page of [teacher, student]) {
    await expect(page.getByTestId("canonical-state")).toHaveAttribute(
      "data-element-count",
      "3",
    );
  }
  await expectCanonicalHashesToMatch(teacher, student);
  expect(runtimeErrors).toEqual([]);
  await context.close();
});

test("Gate C issues memory-only grants and enforces editor to viewer data flow", async ({
  browser,
}) => {
  test.setTimeout(30_000);
  const controlUrl = process.env.P5_GATE_C_CONTROL_URL;
  const providerUrl = process.env.P5_GATE_C_PROVIDER_URL;
  expect(controlUrl).toBeTruthy();
  expect(providerUrl).toBeTruthy();
  const context = await browser.newContext({ locale: "vi-VN" });
  const teacher = await context.newPage();
  const viewer = await context.newPage();
  const runtimeErrors: string[] = [];
  for (const page of [teacher, viewer]) {
    page.on("pageerror", (error) => runtimeErrors.push(error.message));
    page.on("console", (message) => {
      if (message.type() === "error") runtimeErrors.push(message.text());
    });
  }

  const teacherGrantResponse = teacher.waitForResponse(
    (response) => response.url() === `${controlUrl}/gate-c/grants`,
  );
  await teacher.goto(
    `/excalidraw.html?${gateCQuery({
      actor: "teacher-a",
      bootstrap: true,
      capability: "edit",
      controlUrl: controlUrl as string,
      providerUrl: providerUrl as string,
      session: "teacher-session",
    })}`,
  );
  const teacherGrantNetworkResponse = await teacherGrantResponse;
  expect(teacherGrantNetworkResponse.status()).toBe(201);
  expect(teacherGrantNetworkResponse.headers()["cache-control"]).toBe(
    "no-store",
  );
  expect(teacherGrantNetworkResponse.headers()["referrer-policy"]).toBe(
    "no-referrer",
  );
  await expect(teacher.getByTestId("canonical-state")).toHaveAttribute(
    "data-capability",
    "edit",
  );
  await expect(teacher.getByTestId("canonical-state")).toHaveAttribute(
    "data-generation",
    "1",
  );

  const viewerGrantResponse = viewer.waitForResponse(
    (response) => response.url() === `${controlUrl}/gate-c/grants`,
  );
  await viewer.goto(
    `/excalidraw.html?${gateCQuery({
      actor: "viewer-c",
      bootstrap: false,
      capability: "view",
      controlUrl: controlUrl as string,
      providerUrl: providerUrl as string,
      session: "viewer-session",
    })}`,
  );
  const viewerGrantNetworkResponse = await viewerGrantResponse;
  expect(viewerGrantNetworkResponse.status()).toBe(201);
  expect(viewerGrantNetworkResponse.headers()["cache-control"]).toBe(
    "no-store",
  );
  expect(viewerGrantNetworkResponse.headers()["referrer-policy"]).toBe(
    "no-referrer",
  );
  await expect(viewer.getByTestId("canonical-state")).toHaveAttribute(
    "data-capability",
    "view",
  );

  await Promise.all([
    teacher.getByRole("button", { name: "Mở bảng Excalidraw" }).click(),
    viewer.getByRole("button", { name: "Mở bảng Excalidraw" }).click(),
  ]);
  await Promise.all([
    expect(teacher.getByTestId("excalidraw-canvas")).toHaveAttribute(
      "data-rendered-element-count",
      "1",
    ),
    expect(viewer.getByTestId("excalidraw-canvas")).toHaveAttribute(
      "data-rendered-element-count",
      "1",
    ),
  ]);
  await expect(
    viewer.getByRole("button", { name: "Thêm qua Excalidraw" }),
  ).toBeDisabled();
  await teacher.getByRole("button", { name: "Thêm qua Excalidraw" }).click();
  await expect(viewer.getByTestId("canonical-state")).toHaveAttribute(
    "data-element-count",
    "2",
  );
  await expect(viewer.getByTestId("excalidraw-canvas")).toHaveAttribute(
    "data-rendered-element-count",
    "2",
  );
  await expectCanonicalHashesToMatch(teacher, viewer);

  for (const page of [teacher, viewer]) {
    expect(page.url()).not.toContain("grant");
    const leakEvidence = await page.evaluate(() => {
      const localValues = Object.values(localStorage);
      const sessionValues = Object.values(sessionStorage);
      return {
        cookie: document.cookie,
        grantAttributes: document.querySelectorAll(
          "[data-grant], [name='grant']",
        ).length,
        localStorage: localValues.length,
        sessionStorage: sessionValues.length,
      };
    });
    expect(leakEvidence).toEqual({
      cookie: "",
      grantAttributes: 0,
      localStorage: 0,
      sessionStorage: 0,
    });
  }
  expect(runtimeErrors).toEqual([]);
  await context.close();
});

async function expectCanonicalHashesToMatch(
  first: import("@playwright/test").Page,
  second: import("@playwright/test").Page,
): Promise<void> {
  await expect
    .poll(async () => {
      const firstHash = await first
        .getByTestId("canonical-state")
        .getAttribute("data-canonical-hash");
      const secondHash = await second
        .getByTestId("canonical-state")
        .getAttribute("data-canonical-hash");
      return firstHash !== null && firstHash === secondHash;
    })
    .toBe(true);
}

function gateCQuery({
  actor,
  bootstrap,
  capability,
  controlUrl,
  providerUrl,
  session,
}: {
  actor: "teacher-a" | "viewer-c";
  bootstrap: boolean;
  capability: "edit" | "view";
  controlUrl: string;
  providerUrl: string;
  session: "teacher-session" | "viewer-session";
}): string {
  return new URLSearchParams({
    actor,
    authz: "1",
    bootstrap: bootstrap ? "1" : "0",
    capability,
    control: controlUrl,
    document: "board-1",
    provider: providerUrl,
    session,
    tenant: "tenant-a",
  }).toString();
}
