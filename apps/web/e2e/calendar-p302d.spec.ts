import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const publicID = "8818c018-b6c5-4f44-a844-7cbec84a986d";
const slotID = "7d84f838-e788-4ae1-894a-a02984f58826";
const syntheticCapability = ["fixture", "fragment"].join("-");

interface RouteRecorder {
  unexpected: string[];
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
  const quota = (limit: number) => ({
    configured_limit: limit,
    limit,
    remaining: limit,
    used: 0,
  });
  return {
    can_manage_overrides: false,
    features: {
      availability_polls: { configured_enabled: true, enabled: true },
      class_invite_links: { configured_enabled: true, enabled: true },
      class_management: { configured_enabled: true, enabled: true },
      class_session_recurrence: { configured_enabled: false, enabled: false },
      class_session_scheduling: { configured_enabled: true, enabled: true },
      in_app_notifications: { configured_enabled: false, enabled: false },
      membership_invitations: { configured_enabled: true, enabled: true },
    },
    operations: {
      accept_membership_invitation: available,
      activate_class: available,
      create_availability_poll: available,
      create_availability_poll_capability: available,
      create_class: available,
      create_class_invite_link: available,
      create_membership_invitation: available,
      join_class_invite_link: available,
      restore_active_class: available,
      schedule_class_session: available,
      schedule_study_meeting: available,
    },
    quotas: {
      active_availability_polls: quota(20),
      active_classes: quota(25),
      active_study_meetings: quota(20),
      availability_poll_capability_creations_per_hour: quota(60),
      availability_poll_creations_per_hour: quota(20),
      availability_poll_participants: quota(100),
      availability_poll_range_days: quota(31),
      availability_poll_slots: quota(336),
      invite_creations_per_hour: quota(60),
      members: { configured_limit: 100, limit: 100, remaining: 99, used: 1 },
      study_meeting_creations_per_hour: quota(20),
    },
    tenant_id: tenantID,
    version: 1,
  };
}

async function fulfillJSON(
  route: Route,
  body: unknown,
  status = 200,
  headers: Record<string, string> = {},
) {
  await route.fulfill({
    body: JSON.stringify(body),
    contentType: "application/json",
    headers,
    status,
  });
}

function publicPollExchange() {
  const slot = {
    ends_at: "2030-08-05T03:00:00Z",
    id: slotID,
    ordinal: 0,
    starts_at: "2030-08-05T02:00:00Z",
  };
  return {
    poll: {
      deadline_at: "2030-08-04T12:00:00Z",
      description: "Synthetic acceptance poll.",
      my_response: null,
      public_id: publicID,
      ranked_slots: [
        {
          aggregate_bucket: "medium",
          available_count: null,
          cohort_satisfied: true,
          preferred_count: null,
          rank: 1,
          slot,
          unavailable_count: null,
        },
      ],
      slots: [slot],
      status: "open",
      timezone: "UTC",
      title: "Synthetic availability poll",
    },
    response_token: ["fixture", "response"].join("-"),
    response_token_expires_at: "2030-08-05T01:30:00Z",
  };
}

async function installOrganizerMocks(page: Page, recorder: RouteRecorder) {
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
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
      url.pathname.endsWith("/api/v1/calendar/availability-polls")
    ) {
      expect(request.headers()["x-tutorhub-expected-tenant-id"]).toBe(tenantID);
      await fulfillJSON(route, { polls: [] });
      return;
    }
    if (
      request.method() === "GET" &&
      url.pathname.endsWith("/api/v1/calendar/study-meetings")
    ) {
      expect(request.headers()["x-tutorhub-expected-tenant-id"]).toBe(tenantID);
      await fulfillJSON(route, { meetings: [] });
      return;
    }

    recorder.unexpected.push(`${request.method()} ${url.pathname}`);
    await fulfillJSON(
      route,
      { detail: "Unexpected P3-02D organizer request.", status: 404 },
      404,
    );
  });
}

test.describe("P3-02D availability poll acceptance", () => {
  test("loads organizer capabilities with an empty poll list and editor", async ({
    page,
  }) => {
    const recorder: RouteRecorder = { unexpected: [] };
    let capabilityRequest: Request | undefined;
    await installOrganizerMocks(page, recorder);
    page.on("request", (request) => {
      if (request.url().includes(`/api/v1/tenants/${tenantID}/capabilities`)) {
        capabilityRequest = request;
      }
    });

    await page.goto("/app/calendar/availability-polls", {
      waitUntil: "domcontentloaded",
    });
    await page.locator(".language-select select").selectOption("en");

    await expect(
      page.getByRole("heading", { level: 1, name: "Availability polls" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Create a poll" }),
    ).toBeVisible();
    await expect(page.getByText("No availability polls yet")).toBeVisible();
    await expect(page.getByText("No study meetings scheduled")).toBeVisible();
    await expect(
      page.getByRole("textbox", { name: "Poll title" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Create draft poll" }),
    ).toBeEnabled();
    await expect(page.getByText(/Generated slots/).first()).toBeVisible();

    expect(capabilityRequest).toBeDefined();
    expect(recorder.unexpected).toEqual([]);
    const capabilityResponse = await capabilityRequest?.response();
    expect(capabilityResponse?.status()).toBe(200);
    await expect(capabilityResponse?.json()).resolves.toMatchObject({
      features: { availability_polls: { enabled: true } },
    });
    const axeResults = await new AxeBuilder({ page })
      .include(".availability-poll-management")
      .analyze();
    expect(axeResults.violations).toEqual([]);
  });

  test("scrubs public capability fragments before API resolution and keeps the exchange private", async ({
    page,
  }) => {
    const requestURLs: string[] = [];
    const hashesAtRequest: string[] = [];
    const requestBodies: unknown[] = [];
    const securityHeaders = {
      "Cache-Control": "no-store",
      "Content-Security-Policy":
        "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
      "Referrer-Policy": "no-referrer",
      "X-Robots-Tag": "noindex, nofollow",
    };

    await page.route("**/api/v1/me", (route) => route.abort("blockedbyclient"));
    await page.route(
      "**/api/v1/calendar/availability-polls/resolve",
      async (route) => {
        const request = route.request();
        requestURLs.push(request.url());
        requestBodies.push(JSON.parse(request.postData() ?? "{}") as unknown);
        hashesAtRequest.push(new URL(page.url()).hash);
        await fulfillJSON(route, publicPollExchange(), 200, securityHeaders);
      },
    );

    const resolveResponsePromise = page.waitForResponse((response) =>
      response.url().endsWith("/api/v1/calendar/availability-polls/resolve"),
    );
    await page.goto(
      `/availability/${publicID}#token=${syntheticCapability}&tracking=discard`,
      { waitUntil: "domcontentloaded" },
    );
    const resolveResponse = await resolveResponsePromise;

    await expect(
      page.getByRole("heading", { name: "Synthetic availability poll" }),
    ).toBeVisible();
    expect(page.url()).toBe(
      `${test.info().project.use.baseURL}/availability/${publicID}`,
    );
    expect(hashesAtRequest).toEqual([""]);
    expect(requestURLs.every((url) => !url.includes(syntheticCapability))).toBe(
      true,
    );
    expect(requestBodies).toEqual([
      { public_id: publicID, token: syntheticCapability },
    ]);
    expect(resolveResponse.headers()).toMatchObject({
      "cache-control": "no-store",
      "referrer-policy": "no-referrer",
      "x-robots-tag": "noindex, nofollow",
    });
    expect(resolveResponse.headers()["content-security-policy"]).toContain(
      "frame-ancestors 'none'",
    );

    const storage = await page.evaluate(async () => {
      const values = [
        ...Object.values(localStorage),
        ...Object.values(sessionStorage),
      ];
      const databaseNames =
        typeof indexedDB.databases === "function"
          ? (await indexedDB.databases()).map((database) => database.name ?? "")
          : [];
      return { databaseNames, values };
    });
    expect(JSON.stringify(storage)).not.toContain(syntheticCapability);
    expect(await page.evaluate(() => window.location.hash)).toBe("");
    const axeResults = await new AxeBuilder({ page })
      .include(".public-availability-page")
      .analyze();
    expect(axeResults.violations).toEqual([]);
  });

  test("keeps the organizer flow keyboard-accessible on mobile and forced colors", async ({
    page,
  }) => {
    await page.setViewportSize({ height: 844, width: 390 });
    await page.emulateMedia({
      colorScheme: "light",
      forcedColors: "active",
      reducedMotion: "reduce",
    });
    const recorder: RouteRecorder = { unexpected: [] };
    await installOrganizerMocks(page, recorder);

    await page.goto("/app/calendar/availability-polls", {
      waitUntil: "domcontentloaded",
    });
    await page.locator(".language-select select").selectOption("en");

    const titleField = page.getByRole("textbox", { name: "Poll title" });
    const descriptionField = page.getByRole("textbox", {
      name: "Description",
    });
    await titleField.focus();
    await page.keyboard.press("Tab");
    await expect(descriptionField).toBeFocused();
    await expect(
      page.getByRole("button", { name: "Create draft poll" }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth + 1,
      ),
    ).toBe(true);
    expect(recorder.unexpected).toEqual([]);

    const axeResults = await new AxeBuilder({ page })
      .include(".availability-poll-management")
      .analyze();
    expect(axeResults.violations).toEqual([]);
  });
});
