import { expect, test, type Browser, type CDPSession } from "@playwright/test";

interface LoadMetric {
  engine: "tldraw" | "excalidraw";
  fixtureSize: 500 | 2000;
  readyMs: number;
  heapBytes: number;
}

const cases: Pick<LoadMetric, "engine" | "fixtureSize">[] = [
  { engine: "tldraw", fixtureSize: 500 },
  { engine: "tldraw", fixtureSize: 2000 },
  { engine: "excalidraw", fixtureSize: 500 },
  { engine: "excalidraw", fixtureSize: 2000 },
];

test("ghi lại cold-context load/heap evidence cho cùng fixture 500 và 2.000", async ({
  browser,
}) => {
  const baseURL = String(test.info().project.use.baseURL);
  const metrics: LoadMetric[] = [];

  for (const testCase of cases) {
    metrics.push(await measureCase(browser, baseURL, testCase));
  }

  console.log(`P5_WHITEBOARD_METRICS ${JSON.stringify(metrics)}`);
  await test.info().attach("whiteboard-load-metrics.json", {
    body: JSON.stringify(metrics, null, 2),
    contentType: "application/json",
  });

  expect(metrics.every((metric) => metric.readyMs < 60_000)).toBe(true);
});

async function measureCase(
  browser: Browser,
  baseURL: string,
  testCase: Pick<LoadMetric, "engine" | "fixtureSize">,
): Promise<LoadMetric> {
  const context = await browser.newContext({ baseURL, locale: "vi-VN" });
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  const startedAt = performance.now();

  await page.goto(
    `/?engine=${testCase.engine}&fixture=${testCase.fixtureSize.toString()}`,
  );
  await expect(page.getByRole("status")).toContainText(
    `${testCase.engine} sẵn sàng với ${testCase.fixtureSize.toLocaleString("vi-VN")} đối tượng`,
    { timeout: 60_000 },
  );

  const metric = await captureMetric(cdp, testCase, startedAt);
  await context.close();
  return metric;
}

async function captureMetric(
  cdp: CDPSession,
  testCase: Pick<LoadMetric, "engine" | "fixtureSize">,
  startedAt: number,
): Promise<LoadMetric> {
  const heap = await cdp.send("Runtime.getHeapUsage");
  return {
    ...testCase,
    readyMs: Math.round(performance.now() - startedAt),
    heapBytes: Math.round(heap.usedSize),
  };
}
