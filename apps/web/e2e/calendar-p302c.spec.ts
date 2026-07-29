import {
  expect,
  test,
  type Page,
  type Request,
  type Route,
} from "@playwright/test";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const optionalUserID = "80113acb-9865-46aa-a9bf-d7bde155208a";
const classID = "1dcf37ff-4450-49ff-90aa-74dfbd551da2";
const sessionID = "00000000-0000-4000-8000-000000000001";
const fixtureDate = "2026-08-17";

interface MockContext {
  handle: (route: Route, request: Request, url: URL) => Promise<boolean>;
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
      class_session_recurrence: { enabled: false },
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
    secondary_timezone: "Europe/London",
    time_format: "24h",
    time_scale_minutes: 30,
    updated_at: "2026-08-16T08:00:00Z",
    version: 3,
    viewer_timezone: "Asia/Ho_Chi_Minh",
    week_start: "monday",
  };
}

function calendarItem() {
  return {
    all_day: false,
    class_id: classID,
    class_title: "Advanced Mathematics",
    color_token: "class_session",
    display_timezone: "Asia/Ho_Chi_Minh",
    ends_at: "2026-08-17T03:00:00Z",
    id: `class_session:${sessionID}`,
    occurrence_key: sessionID,
    source_id: sessionID,
    source_type: "class_session",
    starts_at: "2026-08-17T02:00:00Z",
    status: "scheduled",
    title: "P3-02C acceptance class",
    version: 4,
    viewer_capabilities: {
      can_cancel: true,
      can_edit: true,
      can_reschedule: true,
      can_view: true,
    },
  };
}

function managerAudience(attendeeVersion = 2) {
  return {
    attendees: [
      {
        business_role: "organizer",
        id: "bcd16c31-c8b9-4c2f-a0e9-d87e4220d32a",
        is_self: true,
        participation_role: "required",
        responded_at: null,
        rsvp_state: "needs_action",
        user_id: userID,
        version: attendeeVersion,
      },
      {
        business_role: "student",
        id: "85e4d84c-cf88-45b7-845c-6dd1b777a311",
        is_self: false,
        participation_role: "optional",
        responded_at: "2026-08-16T04:00:00Z",
        rsvp_state: "accepted",
        user_id: optionalUserID,
        version: 5,
      },
    ],
    audience_revision: 8,
    external_attendees: [],
    response_requested: true,
    viewer_access: {
      can_manage_attendees: true,
      can_respond: true,
      can_see_guest_list: true,
    },
  };
}

function workingSchedule(version = 4) {
  return {
    exceptions: [],
    source: "user_override",
    timezone: "Asia/Ho_Chi_Minh",
    updated_at: "2026-08-16T08:00:00Z",
    version,
    weekly_intervals: [
      { ends_at: "12:00", starts_at: "08:00", weekday: "monday" },
      { ends_at: "17:00", starts_at: "13:00", weekday: "monday" },
    ],
  };
}

function availabilityResponse() {
  return {
    empty_suggestions_reason: null,
    participants: [
      {
        intervals: [
          {
            ends_at: "2026-08-18T03:00:00Z",
            starts_at: "2026-08-18T00:00:00Z",
            status: "unknown",
          },
        ],
        participant: { id: userID, kind: "internal_user" },
        role: "required",
        working_intervals: [
          {
            ends_at: "2026-08-18T10:00:00Z",
            starts_at: "2026-08-18T02:00:00Z",
          },
        ],
      },
      {
        intervals: [
          {
            ends_at: "2026-08-18T03:00:00Z",
            starts_at: "2026-08-18T00:00:00Z",
            status: "busy",
          },
        ],
        participant: { id: optionalUserID, kind: "internal_user" },
        role: "optional",
        working_intervals: [],
      },
    ],
    suggestions: [
      {
        ends_at: "2026-08-18T03:00:00Z",
        reason_breakdown: {
          optional_busy: 1,
          optional_out_of_office: 0,
          optional_outside_working: 0,
          optional_tentative: 0,
          optional_unknown: 0,
          required_busy: 0,
          required_out_of_office: 0,
          required_outside_working: 0,
          required_tentative: 0,
          required_unknown: 1,
        },
        stable_slot_key: "slot-1",
        starts_at: "2026-08-18T02:00:00Z",
      },
    ],
    timezone: "Asia/Ho_Chi_Minh",
  };
}

function externalProjection() {
  return {
    attendee_version: 4,
    capability_expires_at: "2026-08-24T03:00:00Z",
    ends_at: "2026-08-18T03:00:00Z",
    invitation_sequence: 2,
    response_requested: true,
    rsvp_state: "needs_action",
    starts_at: "2026-08-18T02:00:00Z",
    timezone: "Asia/Ho_Chi_Minh",
    title: "External RSVP acceptance class",
  };
}

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    body: JSON.stringify(body),
    contentType:
      status >= 400 ? "application/problem+json" : "application/json",
    status,
  });
}

function requestJSON(request: Request): unknown {
  return JSON.parse(request.postData() ?? "{}") as unknown;
}

async function installAuthenticatedCalendarMocks(
  page: Page,
  context: MockContext,
) {
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());

    if (await context.handle(route, request, url)) {
      return;
    }
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
      url.pathname.endsWith("/api/v1/auth/csrf")
    ) {
      await fulfillJSON(route, { csrf_token: "synthetic-e2e-csrf" });
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
      await fulfillJSON(route, { items: [calendarItem()], next_cursor: null });
      return;
    }

    await fulfillJSON(
      route,
      {
        detail: `Unexpected P3-02C E2E request: ${url.pathname}`,
        status: 404,
        title: "Not Found",
        type: "https://tutorhub.test/problems/not-found",
      },
      404,
    );
  });
}

async function openCalendar(page: Page) {
  await page.goto(`/app/calendar?view=week&date=${fixtureDate}`, {
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
    "1",
  );
}

test.describe("P3-02C Calendar route acceptance", () => {
  test("saves a versioned working schedule with multiple intervals and an exception", async ({
    page,
  }) => {
    let updateBody: unknown;
    let updateHeaders: Record<string, string> | undefined;
    await installAuthenticatedCalendarMocks(page, {
      handle: async (route, request, url) => {
        if (!url.pathname.endsWith("/api/v1/calendar/working-schedule")) {
          return false;
        }
        if (request.method() === "GET") {
          await fulfillJSON(route, workingSchedule());
          return true;
        }
        if (request.method() === "PUT") {
          updateBody = requestJSON(request);
          updateHeaders = request.headers();
          await fulfillJSON(route, {
            ...workingSchedule(5),
            exceptions: [
              { date: "2026-09-02", intervals: [], kind: "holiday" },
            ],
            weekly_intervals: [
              {
                ends_at: "12:00",
                starts_at: "08:00",
                weekday: "monday",
              },
              {
                ends_at: "17:00",
                starts_at: "13:30",
                weekday: "monday",
              },
            ],
          });
          return true;
        }
        return false;
      },
    });

    await openCalendar(page);
    await page.getByRole("button", { name: "Working hours" }).click();
    await expect(
      page.getByRole("heading", { name: "Working hours and exceptions" }),
    ).toBeVisible();

    const monday = page.getByRole("group", { name: "Monday" });
    await monday.getByLabel("Monday Starts").nth(1).fill("13:30");
    await page.getByRole("button", { name: "Add exception" }).click();
    await page
      .locator('input[type="date"][aria-label="Date"]')
      .fill("2026-09-02");
    await page.getByRole("button", { name: "Save working hours" }).click();

    await expect(page.getByText("Working hours saved.")).toBeVisible();
    expect(updateHeaders?.["x-csrf-token"]).toBe("synthetic-e2e-csrf");
    expect(updateHeaders?.["x-tutorhub-expected-tenant-id"]).toBe(tenantID);
    expect(updateBody).toMatchObject({
      exceptions: [{ date: "2026-09-02", intervals: [], kind: "holiday" }],
      expected_version: 4,
      timezone: "Asia/Ho_Chi_Minh",
      weekly_intervals: [
        { ends_at: "12:00", starts_at: "08:00", weekday: "monday" },
        { ends_at: "17:00", starts_at: "13:30", weekday: "monday" },
      ],
    });
  });

  test("renders privacy-safe Scheduling Assistant reasons and recovers focus after RSVP 409", async ({
    page,
  }) => {
    let audienceReads = 0;
    let availabilityBody: unknown;
    let rsvpBody: unknown;
    await installAuthenticatedCalendarMocks(page, {
      handle: async (route, request, url) => {
        const audiencePath = `/api/v1/classes/${classID}/sessions/${sessionID}/attendees`;
        const responsePath = `/api/v1/classes/${classID}/sessions/${sessionID}/responses`;
        if (request.method() === "GET" && url.pathname.endsWith(audiencePath)) {
          audienceReads += 1;
          await fulfillJSON(route, managerAudience(audienceReads + 1));
          return true;
        }
        if (
          request.method() === "POST" &&
          url.pathname.endsWith("/api/v1/calendar/availability/query")
        ) {
          availabilityBody = requestJSON(request);
          await fulfillJSON(route, availabilityResponse());
          return true;
        }
        if (
          request.method() === "POST" &&
          url.pathname.endsWith(responsePath)
        ) {
          rsvpBody = requestJSON(request);
          await fulfillJSON(
            route,
            {
              detail: "The attendee changed.",
              status: 409,
              title: "Conflict",
              type: "https://tutorhub.test/problems/conflict",
            },
            409,
          );
          return true;
        }
        return false;
      },
    });

    await openCalendar(page);
    await page.locator("[data-calendar-event-id]").first().click();
    await expect(
      page.getByRole("heading", { name: "Attendees" }),
    ).toBeVisible();
    await expect(page.getByText("2 internal attendees")).toBeVisible();

    await page.getByRole("button", { name: "Find available times" }).click();
    const suggestions = page.getByRole("table", { name: "Suggested times" });
    await expect(suggestions).toBeVisible();
    await expect(suggestions.getByText("Required unknown: 1")).toBeVisible();
    await expect(suggestions.getByText("Optional busy: 1")).toBeVisible();
    const timezoneSummary = page.locator(
      ".calendar-scheduling-assistant__timezone",
    );
    await expect(
      timezoneSummary.getByText("Asia/Ho_Chi_Minh", { exact: true }),
    ).toBeVisible();
    await expect(
      timezoneSummary.getByText("Europe/London", { exact: true }),
    ).toBeVisible();
    expect(availabilityBody).toMatchObject({
      class_id: classID,
      duration_minutes: 60,
      max_candidates: 10,
      optional: [{ id: optionalUserID, kind: "internal_user" }],
      required: [{ id: userID, kind: "internal_user" }],
      step_minutes: 30,
      timezone: "Asia/Ho_Chi_Minh",
    });

    await page.getByRole("button", { name: "Decline" }).click();
    const reload = page.getByRole("button", { name: "Reload invitation" });
    await expect(reload).toBeVisible();
    await expect(reload).toBeFocused();
    expect(rsvpBody).toMatchObject({
      expected_attendee_version: 2,
      idempotency_key: expect.stringMatching(/^rsvp:/u),
      state: "declined",
    });
    await reload.click();
    await expect.poll(() => audienceReads).toBe(2);
  });
});

test.describe("P3-02C public RSVP privacy boundary", () => {
  test("scrubs fragment capabilities before resolving and never places them in request URLs", async ({
    page,
  }) => {
    const requestURLs: string[] = [];
    const requestBodies: unknown[] = [];
    await page.route("**/api/v1/me", (route) =>
      route.abort("connectionrefused"),
    );
    await page.route("**/api/v1/calendar/invitations/**", async (route) => {
      const request = route.request();
      const body = requestJSON(request);
      requestURLs.push(request.url());
      requestBodies.push(body);
      if (new URL(request.url()).pathname.endsWith("/resolve")) {
        await fulfillJSON(route, externalProjection());
        return;
      }
      await fulfillJSON(route, {
        projection: {
          ...externalProjection(),
          attendee_version: 5,
          rsvp_state: "tentative",
        },
        replayed: false,
      });
    });

    await page.goto(
      "/calendar/respond#resolve_token=synthetic-resolve-capability&respond_token=synthetic-respond-capability&tracking=discard",
      { waitUntil: "domcontentloaded" },
    );

    await expect(page).toHaveURL(/\/calendar\/respond$/u);
    await expect(
      page.getByText("External RSVP acceptance class"),
    ).toBeVisible();
    const submit = page.getByRole("button", { name: "Gửi phản hồi" });
    await expect(submit).toBeDisabled();
    const tentative = page.getByRole("radio", { name: "Có thể tham gia" });
    await tentative.focus();
    await page.keyboard.press("Space");
    await expect(tentative).toBeChecked();
    await page
      .getByRole("textbox", { name: /^Ghi chú/u })
      .fill("Đến muộn ít phút.");
    const confirmation = page.getByRole("checkbox", {
      name: /Tôi xác nhận đây là phản hồi/u,
    });
    await confirmation.focus();
    await page.keyboard.press("Space");
    await expect(confirmation).toBeChecked();
    await submit.click();

    await expect(page.getByText("Đã lưu phản hồi")).toBeVisible();
    expect(
      requestURLs.every(
        (url) =>
          !url.includes("synthetic-resolve-capability") &&
          !url.includes("synthetic-respond-capability"),
      ),
    ).toBe(true);
    const resolveBodies = requestBodies.filter((_, index) =>
      new URL(requestURLs[index] ?? "https://invalid.test").pathname.endsWith(
        "/resolve",
      ),
    );
    const respondBodies = requestBodies.filter((_, index) =>
      new URL(requestURLs[index] ?? "https://invalid.test").pathname.endsWith(
        "/respond",
      ),
    );
    expect(resolveBodies.length).toBeGreaterThanOrEqual(1);
    expect(resolveBodies).toEqual(
      resolveBodies.map(() => ({ token: "synthetic-resolve-capability" })),
    );
    expect(respondBodies).toHaveLength(1);
    expect(respondBodies[0]).toMatchObject({
      expected_attendee_version: 4,
      idempotency_key: expect.stringMatching(/^external-rsvp:/u),
      note: "Đến muộn ít phút.",
      state: "tentative",
      token: "synthetic-respond-capability",
    });
    expect(await page.evaluate(() => window.location.hash)).toBe("");
    expect(await page.evaluate(() => window.sessionStorage.length)).toBe(0);
    expect(await page.evaluate(() => window.localStorage.length)).toBe(0);
  });
});
