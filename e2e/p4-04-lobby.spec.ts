import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page, type Route } from "@playwright/test";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const meetingID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const participantSessionID = "f680fd29-c7f1-4083-af9b-52ad1db14ba9";
const admissionRequestID = "d48a301d-c468-4f65-8da2-029fc379ee74";
const invitedUserID = "a6bfdf58-fd9c-49db-bf42-ab7a0364a574";
const prejoinPath = `/app/media/spaces/${spaceID}/prejoin`;

type SelfStatus = "waiting" | "denied" | "cancelled";

const student = {
  user: {
    id: userID,
    email: "student@example.test",
    display_name: "Student One",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role: "student",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: ["session.join"],
};

function mediaSpace(canManageInvites = false) {
  return {
    id: spaceID,
    source: { kind: "study_meeting", study_meeting_id: meetingID },
    status: "open",
    version: 4,
    active_room_instance: {
      id: roomInstanceID,
      status: "active",
      version: 2,
      created_at: "2030-08-03T00:00:00Z",
      updated_at: "2030-08-03T00:01:00Z",
    },
    viewer_operations: {
      can_start: false,
      can_end: false,
      can_cancel: false,
      can_manage_admissions: false,
      can_manage_invites: canManageInvites,
    },
    created_at: "2030-08-03T00:00:00Z",
    updated_at: "2030-08-03T00:01:00Z",
  };
}

async function fulfillJSON(route: Route, status: number, body: unknown) {
  await route.fulfill({
    body: JSON.stringify(body),
    contentType: "application/json",
    headers: {
      "Cache-Control": "private, no-store",
      "Referrer-Policy": "no-referrer",
    },
    status,
  });
}

async function installWaitingFixture(page: Page) {
  let status: SelfStatus = "waiting";
  let attemptID = "";
  const observed = {
    cancelBodies: [] as unknown[],
    credentialCalls: 0,
    statusCalls: 0,
  };
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;
    if (request.method() === "GET" && pathname === "/api/health") {
      await fulfillJSON(route, 200, { status: "ok" });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v1/me") {
      await fulfillJSON(route, 200, student);
      return;
    }
    if (
      request.method() === "GET" &&
      pathname === `/api/v1/media/spaces/${spaceID}`
    ) {
      await fulfillJSON(route, 200, mediaSpace());
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v1/auth/csrf") {
      await fulfillJSON(route, 200, { csrf_token: "p4-04-csrf" });
      return;
    }
    if (
      request.method() === "POST" &&
      pathname === `/api/v1/media/spaces/${spaceID}/join-attempts`
    ) {
      attemptID = (request.postDataJSON() as { join_attempt_id: string })
        .join_attempt_id;
      await fulfillJSON(route, 201, attemptProjection(attemptID, "waiting", 1));
      return;
    }
    if (
      request.method() === "GET" &&
      attemptID &&
      pathname === `/api/v1/media/spaces/${spaceID}/join-attempts/${attemptID}`
    ) {
      observed.statusCalls += 1;
      await fulfillJSON(route, 200, attemptProjection(attemptID, status, 2));
      return;
    }
    if (
      request.method() === "POST" &&
      attemptID &&
      pathname ===
        `/api/v1/media/spaces/${spaceID}/join-attempts/${attemptID}/cancel`
    ) {
      observed.cancelBodies.push(request.postDataJSON());
      status = "cancelled";
      await fulfillJSON(route, 200, attemptProjection(attemptID, status, 2));
      return;
    }
    if (
      request.method() === "POST" &&
      pathname === `/api/v1/media/spaces/${spaceID}/join-credentials`
    ) {
      observed.credentialCalls += 1;
      await fulfillJSON(route, 500, { status: 500, title: "Unexpected" });
      return;
    }
    if (pathname.includes("/capabilities")) {
      await fulfillJSON(route, 200, {
        tenant_id: tenantID,
        version: 1,
        can_manage_overrides: false,
        features: {},
        quotas: {},
        operations: {},
      });
      return;
    }
    await fulfillJSON(route, 404, {
      status: 404,
      title: "Fixture not found",
      type: "urn:tutorhub:problem:not-found",
    });
  });
  return {
    deny: () => {
      status = "denied";
    },
    observed,
  };
}

function attemptProjection(
  attemptID: string,
  status: SelfStatus,
  version: number,
) {
  return {
    admission_request_id: admissionRequestID,
    admission_version: version,
    can_publish_camera_microphone: true,
    can_share_screen: false,
    can_subscribe: true,
    created_at: "2030-08-03T00:02:00Z",
    expires_at: "2030-08-03T00:07:00Z",
    instance_role: "attendee",
    join_attempt_id: attemptID,
    participant_session_id: participantSessionID,
    room_instance_id: roomInstanceID,
    status,
    updated_at: "2030-08-03T00:03:00Z",
    version,
  };
}

async function openEnglishPrejoin(page: Page) {
  await page.goto(prejoinPath);
  const language = page.locator(".language-select select");
  await language.selectOption("en");
  await expect(
    page.getByRole("heading", { name: "Get ready for class" }),
  ).toBeVisible();
}

function observeProviderWebSockets(page: Page) {
  const sockets: string[] = [];
  page.on("websocket", (socket) => {
    const hostname = new URL(socket.url()).hostname;
    if (hostname !== "127.0.0.1" && hostname !== "localhost") {
      sockets.push(socket.url());
    }
  });
  return sockets;
}

test("P4-04 waiting cancellation keeps provider credential and connection at zero", async ({
  page,
}) => {
  const fixture = await installWaitingFixture(page);
  const websockets = observeProviderWebSockets(page);
  await openEnglishPrejoin(page);

  await page.getByRole("button", { name: "Join listen-only" }).click();
  await expect(
    page.getByRole("heading", { name: "Waiting to be admitted" }),
  ).toBeVisible();
  expect(fixture.observed.credentialCalls).toBe(0);
  expect(websockets).toEqual([]);

  await page.getByRole("button", { name: "Leave waiting room" }).click();
  await expect(
    page.getByRole("heading", { name: "You left the waiting room" }),
  ).toBeVisible();
  expect(fixture.observed.cancelBodies).toEqual([
    expect.objectContaining({
      expected_space_version: 4,
      expected_room_instance_id: roomInstanceID,
      expected_room_instance_version: 2,
      expected_admission_version: expect.any(Number),
    }),
  ]);
  expect(fixture.observed.credentialCalls).toBe(0);
  expect(websockets).toEqual([]);
  expect(
    (await new AxeBuilder({ page }).include(".media-p403-page").analyze())
      .violations,
  ).toEqual([]);
});

test("P4-04 denied projection is terminal and never mints a credential", async ({
  page,
}) => {
  const fixture = await installWaitingFixture(page);
  const websockets = observeProviderWebSockets(page);
  await openEnglishPrejoin(page);
  await page.getByRole("button", { name: "Join listen-only" }).click();
  await expect(
    page.getByRole("heading", { name: "Waiting to be admitted" }),
  ).toBeVisible();
  fixture.deny();
  const check = page.getByRole("button", { name: "Check admission status" });
  const deniedHeading = page.getByRole("heading", {
    name: "Your join request was denied",
  });
  await expect
    .poll(async () => {
      if (await deniedHeading.isVisible()) return true;
      if ((await check.isVisible()) && (await check.isEnabled())) {
        await check.click();
      }
      return deniedHeading.isVisible();
    })
    .toBe(true);
  expect(fixture.observed.statusCalls).toBeGreaterThan(0);
  expect(fixture.observed.credentialCalls).toBe(0);
  expect(websockets).toEqual([]);
});

test("P4-04 StudyMeeting invite is explicit, same-tenant, and renders no raw email", async ({
  page,
}) => {
  const inviteBodies: unknown[] = [];
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const { pathname } = new URL(request.url());
    if (request.method() === "GET" && pathname === "/api/health") {
      await fulfillJSON(route, 200, { status: "ok" });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v1/me") {
      await fulfillJSON(route, 200, {
        ...student,
        active_tenant: { ...student.active_tenant, role: "teacher" },
      });
      return;
    }
    if (
      request.method() === "GET" &&
      pathname === `/api/v1/media/spaces/${spaceID}`
    ) {
      await fulfillJSON(route, 200, mediaSpace(true));
      return;
    }
    if (
      request.method() === "GET" &&
      pathname === `/api/v1/media/spaces/${spaceID}/members`
    ) {
      await fulfillJSON(route, 200, {
        items: [
          {
            user_id: invitedUserID,
            display_name: "Invited Student",
            status: "active",
            version: 1,
            created_at: "2030-08-03T00:00:00Z",
            updated_at: "2030-08-03T00:00:00Z",
          },
        ],
      });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v1/auth/csrf") {
      await fulfillJSON(route, 200, { csrf_token: "p4-04-csrf" });
      return;
    }
    if (
      request.method() === "POST" &&
      pathname === `/api/v1/media/spaces/${spaceID}/members`
    ) {
      inviteBodies.push(request.postDataJSON());
      await fulfillJSON(route, 201, {
        user_id: invitedUserID,
        display_name: "Invited Student",
        status: "active",
        version: 1,
        created_at: "2030-08-03T00:00:00Z",
        updated_at: "2030-08-03T00:00:00Z",
      });
      return;
    }
    if (pathname.includes("/capabilities")) {
      await fulfillJSON(route, 200, {
        tenant_id: tenantID,
        version: 1,
        can_manage_overrides: false,
        features: {},
        quotas: {},
        operations: {},
      });
      return;
    }
    await fulfillJSON(route, 404, { status: 404, title: "Not found" });
  });
  await openEnglishPrejoin(page);

  await expect(
    page.getByRole("heading", { name: "Invited members" }),
  ).toBeVisible();
  await expect(page.getByText("Invited Student")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("student@example.test");
  await page
    .getByRole("textbox", { name: "Workspace member email" })
    .fill("other@example.test");
  await page.getByRole("button", { name: "Invite member" }).click();
  await expect.poll(() => inviteBodies.length).toBe(1);
  expect(inviteBodies).toEqual([
    expect.objectContaining({
      expected_space_version: 4,
      target_member_email: "other@example.test",
    }),
  ]);
  await expect(page).not.toHaveURL(/invite|token|email/i);
});
