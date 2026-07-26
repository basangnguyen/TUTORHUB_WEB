import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

const PERFORMANCE_RUNS = 5;
const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const classID = "1dcf37ff-4450-49ff-90aa-74dfbd551da2";
const fixtureDate = "2026-08-17";

interface PerformanceWindow extends Window {
  __calendarLongTasks?: number[];
}

interface CalendarMockOptions {
  itemCount: number;
  pageSize?: number;
  requests?: Request[];
}

function currentUser() {
  const tenant = {
    id: tenantID,
    is_active: true,
    name: "TutorHub Academy",
    role: "teacher",
    slug: "tutorhub-academy",
    status: "active",
    version: 2,
  };
  return {
    active_tenant: tenant,
    memberships: [tenant],
    permissions: ["class.view"],
    user: {
      display_name: "Calendar Teacher",
      email: "teacher@example.com",
      id: userID,
      locale: "en",
      timezone: "Asia/Ho_Chi_Minh",
    },
  };
}

function capabilities() {
  const available = { available: true, reason: "available" };
  return {
    can_manage_overrides: false,
    features: {
      class_invite_links: { enabled: true },
      class_management: { enabled: true },
      class_session_scheduling: { enabled: true },
      in_app_notifications: { enabled: false },
      membership_invitations: { enabled: true },
    },
    operations: {
      accept_membership_invitation: available,
      activate_class: available,
      create_class: available,
      create_class_invite_link: available,
      create_membership_invitation: available,
      join_class_invite_link: available,
      restore_active_class: available,
      schedule_class_session: available,
    },
    quotas: {
      active_classes: { limit: 100, remaining: 96, used: 4 },
      invite_creations_per_hour: {
        limit: 100,
        remaining: 100,
        reset_at: "2026-08-17T01:00:00Z",
        used: 0,
      },
      members: { limit: 500, remaining: 480, used: 20 },
    },
    tenant_id: tenantID,
    version: 1,
  };
}

function preference() {
  return {
    default_view: "week",
    density: "comfortable",
    locale: "en-US",
    secondary_timezone: "UTC",
    time_format: "24h",
    time_scale_minutes: 30,
    updated_at: "2026-08-16T08:00:00Z",
    version: 3,
    viewer_timezone: "Asia/Ho_Chi_Minh",
    week_start: "monday",
  };
}

function fixtureUUID(index: number) {
  return `00000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`;
}

function calendarItems(count: number) {
  return Array.from({ length: count }, (_, index) => {
    const sourceID = fixtureUUID(index);
    const day = 17 + (index % 7);
    const hour = 1 + (Math.floor(index / 7) % 14);
    const minute = Math.floor(index / 98) % 2 === 0 ? 0 : 30;
    const startsAt = new Date(
      Date.UTC(2026, 7, day, hour, minute, 0),
    ).toISOString();
    const endsAt = new Date(
      Date.UTC(2026, 7, day, hour, minute + 25, 0),
    ).toISOString();
    const canChange = index % 5 !== 0;
    return {
      all_day: false,
      class_id: classID,
      class_title: index % 2 === 0 ? "Advanced Mathematics" : "Literature Lab",
      color_token: "class_session",
      display_timezone: "Asia/Ho_Chi_Minh",
      ends_at: endsAt,
      id: `class_session:${sourceID}`,
      occurrence_key: sourceID,
      source_id: sourceID,
      source_type: "class_session",
      starts_at: startsAt,
      status: index % 17 === 0 ? "cancelled" : "scheduled",
      title: `Session ${String(index + 1).padStart(4, "0")}`,
      version: 1,
      viewer_capabilities: {
        can_cancel: canChange,
        can_edit: canChange,
        can_reschedule: canChange,
        can_view: true,
      },
    };
  });
}

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    body: JSON.stringify(body),
    contentType: "application/json",
    status,
  });
}

async function installCalendarMocks(
  page: Page,
  { itemCount, pageSize = itemCount, requests = [] }: CalendarMockOptions,
) {
  const items = calendarItems(itemCount);
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    requests.push(request);

    if (request.method() === "GET" && url.pathname.endsWith("/api/v1/me")) {
      await fulfillJSON(route, currentUser());
      return;
    }
    if (
      request.method() === "GET" &&
      url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)
    ) {
      await fulfillJSON(route, capabilities());
      return;
    }
    if (
      request.method() === "GET" &&
      url.pathname.endsWith("/api/v1/calendar/preferences/display")
    ) {
      await fulfillJSON(route, preference());
      return;
    }
    if (
      request.method() === "GET" &&
      url.pathname.endsWith("/api/v1/calendar/items")
    ) {
      const cursor = url.searchParams.get("cursor");
      const pageIndex = cursor?.startsWith("page-")
        ? Number.parseInt(cursor.slice(5), 10)
        : 0;
      const start = pageIndex * pageSize;
      const end = Math.min(start + pageSize, items.length);
      await fulfillJSON(route, {
        items: items.slice(start, end),
        next_cursor: end < items.length ? `page-${pageIndex + 1}` : null,
      });
      return;
    }

    await fulfillJSON(
      route,
      {
        error: {
          code: "calendar_test_unexpected_request",
          message: `Unexpected route-level test request: ${url.pathname}`,
        },
      },
      404,
    );
  });
}

async function openCalendar(
  page: Page,
  {
    date = fixtureDate,
    expectedItems,
    view = "week",
  }: { date?: string; expectedItems: number; view?: string },
) {
  await page.goto(`/app/calendar?view=${view}&date=${date}`, {
    waitUntil: "domcontentloaded",
  });
  await page.locator(".language-select select").selectOption("en");
  await expect(
    page.locator(
      '[data-calendar-ready="ready"][data-calendar-renderer="fullcalendar-standard"]',
    ),
  ).toBeVisible();
  await expect(page.locator("body")).toHaveAttribute(
    "data-calendar-visible-events",
    String(expectedItems),
  );
}

function percentile(values: readonly number[], ratio: number) {
  const sorted = [...values].sort((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(sorted.length * ratio) - 1)] ?? 0;
}

function summarize(values: readonly number[]) {
  return {
    max: Math.max(...values),
    min: Math.min(...values),
    p50: percentile(values, 0.5),
    p95: percentile(values, 0.95),
    raw: values,
  };
}

test.describe("P3-02A production Calendar route acceptance", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      const target = window as PerformanceWindow;
      target.__calendarLongTasks = [];
      if ("PerformanceObserver" in window) {
        try {
          const observer = new PerformanceObserver((list) => {
            for (const entry of list.getEntries()) {
              target.__calendarLongTasks?.push(entry.duration);
            }
          });
          observer.observe({ buffered: true, type: "longtask" });
        } catch {
          // Browsers without Long Tasks support keep an empty sample.
        }
      }
    });
  });

  test("keeps tenant/range/pagination/source permissions on the real route", async ({
    page,
  }) => {
    const requests: Request[] = [];
    await installCalendarMocks(page, {
      itemCount: 201,
      pageSize: 200,
      requests,
    });
    await openCalendar(page, { expectedItems: 200 });

    const firstItemsRequest = requests.find((request) =>
      new URL(request.url()).pathname.endsWith("/api/v1/calendar/items"),
    );
    expect(firstItemsRequest).toBeDefined();
    expect(firstItemsRequest?.headers()["x-tutorhub-expected-tenant-id"]).toBe(
      tenantID,
    );
    const firstURL = new URL(firstItemsRequest?.url() ?? "http://localhost");
    expect(firstURL.searchParams.get("limit")).toBe("200");
    expect(firstURL.searchParams.get("from")).toBeTruthy();
    expect(firstURL.searchParams.get("to")).toBeTruthy();
    expect(firstURL.searchParams.get("viewer_timezone")).toBe(
      "Asia/Ho_Chi_Minh",
    );
    expect(firstURL.searchParams.get("cursor")).toBeNull();

    await page
      .getByRole("button", { name: "Load more calendar items" })
      .click();
    await expect(page.locator("body")).toHaveAttribute(
      "data-calendar-visible-events",
      "201",
    );
    const itemRequests = requests.filter((request) =>
      new URL(request.url()).pathname.endsWith("/api/v1/calendar/items"),
    );
    expect(itemRequests).toHaveLength(2);
    expect(new URL(itemRequests[1]!.url()).searchParams.get("cursor")).toBe(
      "page-1",
    );
    await expect(
      page.locator('[data-calendar-event-reschedulable="false"]').first(),
    ).toBeVisible();
  });

  test("passes axe, semantic Agenda, 200% zoom and preference media modes", async ({
    page,
  }) => {
    await page.emulateMedia({
      colorScheme: "light",
      forcedColors: "active",
      reducedMotion: "reduce",
    });
    await installCalendarMocks(page, { itemCount: 24 });
    await openCalendar(page, { expectedItems: 24 });

    await expect(
      page.getByRole("heading", { level: 1, name: "Calendar" }),
    ).toBeVisible();
    await page.getByText("Open keyboard-friendly agenda").click();
    await expect(
      page.getByRole("heading", { level: 2, name: "Agenda" }),
    ).toBeVisible();
    const axeResults = await new AxeBuilder({ page })
      .include(".calendar-page")
      .analyze();
    const allowedFullCalendarWaivers = axeResults.violations.filter(
      ({ id, impact, nodes }) =>
        id === "empty-table-header" &&
        impact === "minor" &&
        nodes.length === 1 &&
        nodes[0]?.target[0] === 'div[role="rowheader"]',
    );
    expect(allowedFullCalendarWaivers.length).toBeLessThanOrEqual(1);
    expect(
      axeResults.violations.filter(
        (violation) => !allowedFullCalendarWaivers.includes(violation),
      ),
    ).toEqual([]);

    await page.evaluate(() => {
      document.documentElement.style.zoom = "2";
    });
    await expect(
      page.getByRole("button", { name: "New session" }),
    ).toBeVisible();
    await expect(page.locator("[data-calendar-announcement]")).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
  });

  for (const fixtureCount of [500, 1_000, 2_000]) {
    test(`meets route budgets with ${fixtureCount} visible items`, async ({
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
      await installCalendarMocks(page, { itemCount: fixtureCount });

      for (let run = 1; run <= PERFORMANCE_RUNS; run += 1) {
        await page.goto(
          `/app/calendar?view=week&date=${fixtureDate}&run=${run}`,
          { waitUntil: "domcontentloaded" },
        );
        await expect(
          page.locator(
            '[data-calendar-ready="ready"][data-calendar-renderer="fullcalendar-standard"]',
          ),
        ).toBeVisible();
        await expect(page.locator("body")).toHaveAttribute(
          "data-calendar-visible-events",
          String(fixtureCount),
        );
        const renderReady = await page.evaluate(() => {
          const readyAt = Number(
            document.body.dataset.calendarRenderReadyAt ?? "NaN",
          );
          const calendarResponses = performance
            .getEntriesByType("resource")
            .filter(
              (entry): entry is PerformanceResourceTiming =>
                entry instanceof PerformanceResourceTiming &&
                entry.name.includes("/api/v1/calendar/items"),
            );
          const responseEnd = Math.max(
            ...calendarResponses.map((entry) => entry.responseEnd),
          );
          return Math.max(0, Math.round(readyAt - responseEnd));
        });
        expect(Number.isFinite(renderReady)).toBe(true);
        renderReadyMs.push(renderReady);

        const navigationStartedAt = await page.evaluate(() => {
          delete document.body.dataset.calendarNavigationReadyAt;
          return performance.now();
        });
        await page
          .getByRole("button", { name: /Tháng|Month/ })
          .first()
          .click();
        await page.waitForFunction(
          () =>
            document.body.dataset.calendarRenderedView === "dayGridMonth" &&
            document.body.dataset.calendarNavigationReadyAt !== undefined,
        );
        const navigationFinishedAt = await page.evaluate(() =>
          Number(document.body.dataset.calendarNavigationReadyAt ?? "NaN"),
        );
        expect(Number.isFinite(navigationFinishedAt)).toBe(true);
        navigationMs.push(
          Math.max(0, Math.round(navigationFinishedAt - navigationStartedAt)),
        );
        const longTasks = await page.evaluate(
          () => (window as PerformanceWindow).__calendarLongTasks ?? [],
        );
        longTaskMaxMs.push(Math.round(Math.max(0, ...longTasks)));
      }

      const renderSummary = summarize(renderReadyMs);
      const navigationSummary = summarize(navigationMs);
      const longTaskSummary = summarize(longTaskMaxMs);
      const renderBudget =
        fixtureCount === 500 ? 500 : fixtureCount === 1_000 ? 900 : 1_800;
      const navigationBudget =
        fixtureCount === 500 ? 350 : fixtureCount === 1_000 ? 500 : 800;
      const longTaskBudget =
        fixtureCount === 500 ? 200 : fixtureCount === 1_000 ? 300 : 400;
      console.log(
        JSON.stringify({
          browserErrors,
          fixtureCount,
          longTaskMaxMs: longTaskSummary,
          navigationMs: navigationSummary,
          renderReadyMs: renderSummary,
          runs: PERFORMANCE_RUNS,
        }),
      );
      expect(browserErrors).toEqual([]);
      expect(renderSummary.p95).toBeLessThanOrEqual(renderBudget);
      expect(navigationSummary.p95).toBeLessThanOrEqual(navigationBudget);
      expect(longTaskSummary.max).toBeLessThanOrEqual(longTaskBudget);
    });
  }
});

test.describe("P3-02A Warm Academic visual baselines", () => {
  const cases = [
    {
      name: "desktop-week",
      size: { height: 900, width: 1440 },
      view: "week",
    },
    {
      name: "tablet-month",
      size: { height: 900, width: 1024 },
      view: "month",
    },
    {
      name: "mobile-agenda",
      size: { height: 844, width: 390 },
      view: "agenda",
    },
  ] as const;

  for (const visualCase of cases) {
    test(`${visualCase.name} matches the approved layout`, async ({ page }) => {
      await page.setViewportSize(visualCase.size);
      await installCalendarMocks(page, { itemCount: 18 });
      if (visualCase.view === "agenda") {
        await page.goto(`/app/calendar?date=${fixtureDate}`, {
          waitUntil: "domcontentloaded",
        });
        await page.locator(".language-select select").selectOption("en");
        await expect(
          page.getByRole("heading", { level: 2, name: "Agenda" }),
        ).toBeVisible();
      } else {
        await openCalendar(page, {
          expectedItems: 18,
          view: visualCase.view,
        });
      }
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth + 1,
        ),
      ).toBe(true);
      await expect(page).toHaveScreenshot(`${visualCase.name}.png`, {
        animations: "disabled",
        fullPage: true,
      });
    });
  }
});
