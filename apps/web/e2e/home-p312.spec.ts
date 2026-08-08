import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Route } from "@playwright/test";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const classID = "fda36d51-27ef-4b0c-a1cb-7c78b3d899ec";

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    body: JSON.stringify(body),
    contentType:
      status >= 400 ? "application/problem+json" : "application/json",
    status,
  });
}

test("P3-12 Home degrades one card and keeps search keyboard accessible", async ({
  page,
}) => {
  await page.route("**/health", (route) =>
    json(route, {
      status: "ok",
      service: "tutorhub-core-api",
      environment: "test",
      timestamp: "2026-08-08T08:00:00Z",
    }),
  );
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === "/api/v1/me") {
      await json(route, {
        active_tenant: {
          id: tenantID,
          is_active: true,
          name: "P3-12 Workspace",
          role: "teacher",
          slug: "p3-12",
          status: "active",
          version: 1,
        },
        memberships: [],
        permissions: ["tenant.view", "class.view"],
        user: {
          display_name: "P3-12 Teacher",
          email: "teacher@example.test",
          id: userID,
          locale: "en",
          timezone: "Asia/Ho_Chi_Minh",
        },
      });
      return;
    }
    if (url.pathname.endsWith("/capabilities")) {
      await json(route, {
        tenant_id: tenantID,
        version: 1,
        features: {},
        operations: {},
        quotas: {},
        can_manage_overrides: false,
      });
      return;
    }
    expect(request.headers()["x-tutorhub-expected-tenant-id"]).toBe(tenantID);
    if (url.pathname === "/api/v1/calendar/items") {
      await json(route, {
        items: [
          {
            class_title: "Algebra 12",
            id: "session-1",
            starts_at: "2026-08-09T01:00:00Z",
            title: "Advanced algebra",
          },
        ],
        next_cursor: null,
      });
      return;
    }
    if (url.pathname === "/api/v1/notifications/unread-count") {
      await json(
        route,
        {
          type: "urn:tutorhub:problem:notification_unavailable",
          title: "Notifications unavailable",
          status: 503,
          code: "notification_unavailable",
        },
        503,
      );
      return;
    }
    if (url.pathname === "/api/v1/conversations") {
      await json(route, {
        items: [
          {
            id: "conversation-1",
            unread_count: 3,
            unread_count_capped: false,
          },
        ],
      });
      return;
    }
    if (url.pathname === "/api/v1/home/recent-files") {
      await json(route, {
        items: [
          {
            id: "file-1",
            class_id: classID,
            class_title: "Algebra 12",
            declared_media_type: "application/pdf",
            display_name: "practice.pdf",
            size_bytes: 42000,
            updated_at: "2026-08-08T08:00:00Z",
          },
        ],
      });
      return;
    }
    if (url.pathname === "/api/v1/search") {
      await json(route, { items: [] });
      return;
    }
    await json(route, { title: "Not found", status: 404 }, 404);
  });

  await page.goto("/app/home");
  await expect(
    page.getByRole("heading", { name: /Today at a glance|Tổng quan hôm nay/ }),
  ).toBeVisible();
  await page.locator(".language-select select").selectOption("en");
  await expect(
    page.getByRole("heading", { name: "Today at a glance" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Upcoming sessions" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Advanced algebra" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "practice.pdf" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Retry Notifications" }),
  ).toBeVisible();

  const search = page.getByRole("searchbox", { name: "Search terms" });
  await page.locator("body").click({ position: { x: 1, y: 1 } });
  for (let index = 0; index < 30; index += 1) {
    if (
      await search.evaluate((element) => element === document.activeElement)
    ) {
      break;
    }
    await page.keyboard.press("Tab");
  }
  await expect(search).toBeFocused();
  await page.keyboard.type("no authorized resource should match this value");
  await expect(page.getByText(/No authorized results match/)).toBeVisible();

  const results = await new AxeBuilder({ page }).include("main").analyze();
  expect(results.violations).toEqual([]);
});
