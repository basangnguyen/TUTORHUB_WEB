import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const PERFORMANCE_RUNS = 5;

interface PerformanceMemory {
  usedJSHeapSize: number;
}

interface CalendarPerformanceWindow extends Window {
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

test.describe("P3-CAL-01 calendar renderer spike", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const target = window as CalendarPerformanceWindow;
      target.__calendarLongTasks = [];
      if ("PerformanceObserver" in window) {
        try {
          const observer = new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
              target.__calendarLongTasks?.push(entry.duration);
            }
          });
          observer.observe({ type: "longtask", buffered: true });
        } catch {
          // Older engines without Long Tasks support keep an empty sample.
        }
      }
    });
  });

  test("passes keyboard, agenda alternative and axe checks", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(
      page.getByRole("heading", {
        name: /Calendar renderer & recurrence spike/i,
      }),
    ).toBeVisible();
    await expect(page.locator("[data-calendar-renderer]")).toBeVisible();
    await expect(page.getByTestId("calendar-ready-ms")).not.toHaveText(
      "Đang đo…",
    );

    const axeResults = await new AxeBuilder({ page }).analyze();
    const allowedFullCalendarWaivers = axeResults.violations.filter(
      ({ id, impact, nodes }) => {
        if (
          id !== "empty-table-header" ||
          impact !== "minor" ||
          nodes.length !== 1
        ) {
          return false;
        }
        const [node] = nodes;
        return (
          node?.impact === "minor" &&
          node.target.length === 1 &&
          node.target[0] === 'div[role="rowheader"]' &&
          /^<div role="rowheader" aria-label="Timed" class="fc-[^"]+"(?: style="width: \d+(?:\.\d+)?px;")?><\/div>$/.test(
            node.html,
          )
        );
      },
    );
    expect(allowedFullCalendarWaivers.length).toBeLessThanOrEqual(1);
    await expect(
      page.locator(
        '[data-calendar-renderer="fullcalendar-standard"] div[role="rowheader"][aria-label="Timed"]',
      ),
    ).toHaveCount(1);
    await expect(
      page.locator('div[role="rowheader"][aria-label="Timed"]'),
    ).toHaveCount(1);
    const unexpectedViolations = axeResults.violations.filter(
      (violation) => !allowedFullCalendarWaivers.includes(violation),
    );
    expect(unexpectedViolations).toEqual([]);
    expect(
      axeResults.violations.filter(
        ({ impact }) => impact === "critical" || impact === "serious",
      ),
    ).toEqual([]);

    const firstMove = page
      .getByRole("button", { name: "Dời sau 30 phút" })
      .first();
    await firstMove.focus();
    await expect(firstMove).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.getByTestId("calendar-announcement")).toContainText(
      "Đã cập nhật thời gian",
    );

    await expect(page.getByTestId("agenda-count")).toHaveText(
      "Hiển thị 24/51 mục",
    );
    await page.getByRole("button", { name: "Hiển thị thêm 24 mục" }).click();
    await page.getByRole("button", { name: "Hiển thị thêm 3 mục" }).click();
    await expect(page.getByTestId("agenda-count")).toHaveText(
      "Hiển thị 51/51 mục",
    );
    await expect(
      page.getByRole("button", { name: "Dời sau 30 phút" }),
    ).toHaveCount(51);
    await expect(
      page.locator('[data-agenda-event-id="ny-fall-later"]'),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Hiển thị thêm/ }),
    ).toHaveCount(0);
  });

  test("reverts the conflict fixture through the keyboard alternative", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    const conflict = page.locator('[data-agenda-event-id="hcm-conflict"]');
    await conflict.getByRole("button", { name: "Dời sau 30 phút" }).click();
    await expect(page.getByTestId("calendar-announcement")).toContainText(
      "409",
    );
    await expect(page.getByTestId("calendar-announcement")).toHaveAttribute(
      "class",
      /error/,
    );
  });

  test("supports pointer drag and resize with authoritative announcements", async ({
    page,
  }) => {
    await page.goto("/", { waitUntil: "domcontentloaded" });

    const conflictEvent = page
      .locator('[data-calendar-event-id="hcm-conflict"]')
      .first();
    await conflictEvent.scrollIntoViewIfNeeded();
    const conflictBox = await conflictEvent.boundingBox();
    expect(conflictBox).not.toBeNull();
    if (conflictBox) {
      await page.mouse.move(
        conflictBox.x + conflictBox.width / 2,
        conflictBox.y + conflictBox.height / 2,
      );
      await page.mouse.down();
      await page.waitForTimeout(150);
      await page.mouse.move(
        conflictBox.x + conflictBox.width / 2,
        conflictBox.y + conflictBox.height + 48,
        { steps: 16 },
      );
      await page.mouse.up();
    }
    await expect(page.getByTestId("calendar-announcement")).toContainText(
      "409",
    );

    const editableEvent = page
      .locator('[data-calendar-event-id="hcm-class-001"]')
      .first();
    await editableEvent.scrollIntoViewIfNeeded();
    await editableEvent.hover();
    const eventLayers = editableEvent.locator(":scope > div");
    expect(await eventLayers.count()).toBe(3);
    const resizeHandle = eventLayers.nth(2);
    const resizeBox = await resizeHandle.boundingBox();
    expect(resizeBox).not.toBeNull();
    if (resizeBox) {
      await page.mouse.move(
        resizeBox.x + resizeBox.width / 2,
        resizeBox.y + resizeBox.height / 2,
      );
      await page.mouse.down();
      await page.mouse.move(
        resizeBox.x + resizeBox.width / 2,
        resizeBox.y + resizeBox.height + 36,
        { steps: 8 },
      );
      await page.mouse.up();
    }
    await expect(page.getByTestId("calendar-announcement")).toContainText(
      "Đã cập nhật thời gian",
    );
  });

  test("uses agenda as the mobile-first alternative", async ({ page }) => {
    await page.setViewportSize({ width: 640, height: 900 });
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(page.locator(".calendar-spike__calendar")).toBeHidden();
    await expect(
      page.getByRole("heading", { name: /Chương trình thay thế/ }),
    ).toBeVisible();
    await expect(page.getByTestId("agenda-count")).toHaveText(
      "Hiển thị 24/51 mục",
    );
    await page.getByRole("button", { name: "Hiển thị thêm 24 mục" }).click();
    await page.getByRole("button", { name: "Hiển thị thêm 3 mục" }).click();
    await expect(
      page.locator('[data-agenda-event-id="ny-fall-later"]'),
    ).toBeVisible();
  });

  test("supports zoom, forced colors and reduced motion preferences", async ({
    page,
  }) => {
    await page.emulateMedia({
      colorScheme: "light",
      forcedColors: "active",
      reducedMotion: "reduce",
    });
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/", { waitUntil: "domcontentloaded" });
    await expect(page.locator("[data-calendar-renderer]")).toBeVisible();

    await page.evaluate(() => {
      document.documentElement.style.zoom = "2";
    });
    await expect(
      page.getByRole("button", { name: "Chương trình" }),
    ).toBeVisible();
    await expect(page.getByTestId("calendar-announcement")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Hiển thị thêm 24 mục" }),
    ).toBeVisible();
    const hasHorizontalOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    );
    expect(hasHorizontalOverflow).toBe(false);
  });

  for (const fixtureCount of [500, 1000, 2000]) {
    test(`records five bounded performance runs for ${fixtureCount} loaded fixtures`, async ({
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
        const readyMs = Number.parseInt(readyText, 10);
        expect(Number.isFinite(readyMs)).toBe(true);
        renderReadyMs.push(readyMs);

        const navigationStartedAt = await page.evaluate(() =>
          performance.now(),
        );
        await page.getByRole("button", { name: "Tháng" }).click();
        await expect(page.locator("body")).toHaveAttribute(
          "data-calendar-rendered-view",
          "dayGridMonth",
        );
        const navigationFinishedAt = await page.evaluate(
          () =>
            new Promise<number>((resolve) => {
              requestAnimationFrame(() => {
                requestAnimationFrame(() => resolve(performance.now()));
              });
            }),
        );
        navigationMs.push(
          Math.max(0, Math.round(navigationFinishedAt - navigationStartedAt)),
        );

        const longTasks = await page.evaluate(
          () => (window as CalendarPerformanceWindow).__calendarLongTasks ?? [],
        );
        longTaskMaxMs.push(Math.round(Math.max(0, ...longTasks)));
      }

      const renderSummary = summarize(renderReadyMs);
      const navigationSummary = summarize(navigationMs);
      const longTaskSummary = summarize(longTaskMaxMs);
      const renderBudget =
        fixtureCount === 500 ? 500 : fixtureCount === 1000 ? 900 : 1_800;
      const navigationBudget =
        fixtureCount === 500 ? 350 : fixtureCount === 1000 ? 500 : 800;
      const longTaskBudget =
        fixtureCount === 500 ? 200 : fixtureCount === 1000 ? 300 : 400;
      console.log(
        JSON.stringify({
          fixtureCount,
          runs: PERFORMANCE_RUNS,
          renderReadyMs: renderSummary,
          navigationMs: navigationSummary,
          longTaskMaxMs: longTaskSummary,
          browserErrors,
        }),
      );
      expect(renderSummary.p95).toBeLessThanOrEqual(renderBudget);
      expect(navigationSummary.p95).toBeLessThanOrEqual(navigationBudget);
      expect(longTaskSummary.max).toBeLessThanOrEqual(longTaskBudget);
      expect(browserErrors).toEqual([]);
    });
  }

  test("keeps the settled 2000-item heap delta below the ADR budget", async ({
    page,
  }) => {
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
    const deltaMiB = Number((deltaBytes / 1024 / 1024).toFixed(2));
    expect(deltaMiB).toBeLessThanOrEqual(80);
    console.log(
      JSON.stringify({
        fixtureCount: 2000,
        heapSettled: {
          baselineBytes,
          fixtureBytes,
          deltaBytes,
          deltaMiB,
        },
      }),
    );
  });
});
