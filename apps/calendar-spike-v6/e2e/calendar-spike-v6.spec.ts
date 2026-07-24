import { expect, test } from "@playwright/test";

const PERFORMANCE_RUNS = 5;

interface PerformanceMemory {
  usedJSHeapSize: number;
}

interface ComparatorWindow extends Window {
  __calendarLongTasks?: number[];
}

function percentile(values: readonly number[], ratio: number): number {
  const sorted = [...values].sort((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(sorted.length * ratio) - 1)] ?? 0;
}

function summarize(values: readonly number[]) {
  return {
    raw: values,
    min: Math.min(...values),
    p50: percentile(values, 0.5),
    p95: percentile(values, 0.95),
    max: Math.max(...values),
  };
}

test.describe("P3-CAL-01 FullCalendar v6.1.21 fallback comparator", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const target = window as ComparatorWindow;
      target.__calendarLongTasks = [];
      try {
        const observer = new PerformanceObserver((list) => {
          for (const entry of list.getEntries()) {
            target.__calendarLongTasks?.push(entry.duration);
          }
        });
        observer.observe({ type: "longtask", buffered: true });
      } catch {
        // Keep an empty sample when Long Tasks are unavailable.
      }
    });
  });

  for (const fixtureCount of [500, 1000, 2000]) {
    test(`records five v6 runs for ${fixtureCount} fixtures`, async ({
      page,
    }) => {
      const renderReadyMs: number[] = [];
      const navigationMs: number[] = [];
      const longTaskMaxMs: number[] = [];
      const browserErrors: string[] = [];

      page.on("console", (message) => {
        if (message.type() === "error") {
          browserErrors.push(message.text());
        }
      });
      page.on("pageerror", (error) => browserErrors.push(error.message));

      for (let run = 1; run <= PERFORMANCE_RUNS; run += 1) {
        await page.goto(`/?events=${fixtureCount}&run=${run}`, {
          waitUntil: "domcontentloaded",
        });
        await expect(page.getByTestId("calendar-ready-ms")).not.toHaveText(
          "Đang đo…",
        );
        const readyText = await page
          .getByTestId("calendar-ready-ms")
          .innerText();
        renderReadyMs.push(Number.parseInt(readyText, 10));

        const startedAt = await page.evaluate(() => performance.now());
        await page.getByRole("button", { name: "Tháng" }).click();
        await expect(page.locator("body")).toHaveAttribute(
          "data-calendar-rendered-view",
          "dayGridMonth",
        );
        const finishedAt = await page.evaluate(
          () =>
            new Promise<number>((resolve) => {
              requestAnimationFrame(() => {
                requestAnimationFrame(() => resolve(performance.now()));
              });
            }),
        );
        navigationMs.push(Math.max(0, Math.round(finishedAt - startedAt)));

        const longTasks = await page.evaluate(
          () => (window as ComparatorWindow).__calendarLongTasks ?? [],
        );
        longTaskMaxMs.push(Math.round(Math.max(0, ...longTasks)));
      }

      const metrics = {
        renderer: "FullCalendar Standard 6.1.21",
        fixtureCount,
        runs: PERFORMANCE_RUNS,
        renderReadyMs: summarize(renderReadyMs),
        navigationMs: summarize(navigationMs),
        longTaskMaxMs: summarize(longTaskMaxMs),
        browserErrors,
      };
      console.log(JSON.stringify(metrics));
      expect(renderReadyMs.every(Number.isFinite)).toBe(true);
      expect(browserErrors).toEqual([]);
    });
  }

  test("records the settled 2000-fixture heap delta", async ({ page }) => {
    const readHeap = () =>
      page.evaluate(() => {
        const memory = (
          performance as Performance & { memory?: PerformanceMemory }
        ).memory;
        return memory?.usedJSHeapSize ?? null;
      });

    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(page.getByTestId("calendar-ready-ms")).not.toHaveText(
      "Đang đo…",
    );
    await page.requestGC();
    const baselineBytes = await readHeap();

    await page.goto("/?events=2000&heap=1", {
      waitUntil: "domcontentloaded",
    });
    await expect(page.getByTestId("calendar-ready-ms")).not.toHaveText(
      "Đang đo…",
    );
    await page.requestGC();
    const fixtureBytes = await readHeap();

    expect(baselineBytes).not.toBeNull();
    expect(fixtureBytes).not.toBeNull();
    const deltaBytes = Math.max(0, (fixtureBytes ?? 0) - (baselineBytes ?? 0));
    console.log(
      JSON.stringify({
        renderer: "FullCalendar Standard 6.1.21",
        fixtureCount: 2000,
        heapSettled: {
          baselineBytes,
          fixtureBytes,
          deltaBytes,
          deltaMiB: Number((deltaBytes / 1024 / 1024).toFixed(2)),
        },
      }),
    );
  });
});
