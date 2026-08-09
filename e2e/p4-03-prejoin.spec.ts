import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page, type Route } from "@playwright/test";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const sessionID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const participantSessionID = "f680fd29-c7f1-4083-af9b-52ad1db14ba9";
const joinAttemptID = "a860f06d-34f9-4c57-89f8-1541bfb3b6d7";
const admissionRequestID = "d48a301d-c468-4f65-8da2-029fc379ee74";
const credentialToken = "p4-03-memory-only-livekit-token";

const prejoinPath = `/app/media/spaces/${spaceID}/prejoin`;
const roomPath = `/app/media/spaces/${spaceID}/instances/${roomInstanceID}/room`;

const currentUser = {
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

const mediaSpace = {
  id: spaceID,
  source: { kind: "class_session", class_session_id: sessionID },
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
  },
  created_at: "2030-08-03T00:00:00Z",
  updated_at: "2030-08-03T00:01:00Z",
};

interface ObservedRequests {
  attemptBodies: unknown[];
  credentialBodies: unknown[];
  healthCalls: number;
  order: string[];
}

interface MediaProbeState {
  enumerateDevicesCalls: number;
  getUserMediaCalls: number;
  stoppedTracks: number;
}

function tenantCapabilities() {
  return {
    tenant_id: tenantID,
    version: 1,
    can_manage_overrides: false,
    features: {
      in_app_notifications: {
        configured_enabled: false,
        enabled: false,
      },
    },
    quotas: {},
    operations: {},
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

async function installMediaProbe(page: Page) {
  await page.addInitScript(() => {
    const state: MediaProbeState = {
      enumerateDevicesCalls: 0,
      getUserMediaCalls: 0,
      stoppedTracks: 0,
    };
    const events = new EventTarget();
    const track = (kind: "audio" | "video", deviceId: string) => ({
      contentHint: "",
      getSettings: () =>
        kind === "audio"
          ? {
              autoGainControl: true,
              deviceId,
              echoCancellation: true,
              noiseSuppression: true,
            }
          : { deviceId },
      kind,
      stop: () => {
        state.stoppedTracks += 1;
      },
    });
    const audioTrack = track("audio", "microphone-1");
    const videoTrack = track("video", "camera-1");
    const stream = {
      getAudioTracks: () => [audioTrack],
      getTracks: () => [audioTrack, videoTrack],
      getVideoTracks: () => [videoTrack],
    };
    const device = (
      kind: MediaDeviceKind,
      deviceId: string,
      label: string,
    ) => ({
      deviceId,
      groupId: `${deviceId}-group`,
      kind,
      label,
      toJSON: () => ({ deviceId, kind, label }),
    });

    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
        addEventListener: events.addEventListener.bind(events),
        dispatchEvent: events.dispatchEvent.bind(events),
        enumerateDevices: async () => {
          state.enumerateDevicesCalls += 1;
          return [
            device("audioinput", "microphone-1", "Class microphone"),
            device("videoinput", "camera-1", "Class camera"),
            device("audiooutput", "speaker-1", "Class speaker"),
          ];
        },
        getSupportedConstraints: () => ({
          autoGainControl: true,
          echoCancellation: true,
          noiseSuppression: true,
        }),
        getUserMedia: async () => {
          state.getUserMediaCalls += 1;
          return stream;
        },
        removeEventListener: events.removeEventListener.bind(events),
      },
    });
    Object.defineProperty(HTMLMediaElement.prototype, "srcObject", {
      configurable: true,
      get() {
        return (this as HTMLMediaElement & { __p403Source?: unknown })
          .__p403Source;
      },
      set(value: unknown) {
        (this as HTMLMediaElement & { __p403Source?: unknown }).__p403Source =
          value;
      },
    });
    HTMLMediaElement.prototype.play = async () => undefined;
    HTMLMediaElement.prototype.pause = () => undefined;
    (
      window as unknown as { __p403MediaProbe: MediaProbeState }
    ).__p403MediaProbe = state;
  });
}

async function readMediaProbe(page: Page) {
  return page.evaluate(
    () =>
      (window as unknown as { __p403MediaProbe: MediaProbeState })
        .__p403MediaProbe,
  );
}

async function installDeniedMediaProbe(page: Page) {
  await page.addInitScript(() => {
    const state: MediaProbeState = {
      enumerateDevicesCalls: 0,
      getUserMediaCalls: 0,
      stoppedTracks: 0,
    };
    const events = new EventTarget();
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
        addEventListener: events.addEventListener.bind(events),
        enumerateDevices: async () => {
          state.enumerateDevicesCalls += 1;
          return [];
        },
        getSupportedConstraints: () => ({}),
        getUserMedia: async () => {
          state.getUserMediaCalls += 1;
          throw new DOMException("fixture detail", "NotAllowedError");
        },
        removeEventListener: events.removeEventListener.bind(events),
      },
    });
    (
      window as unknown as { __p403MediaProbe: MediaProbeState }
    ).__p403MediaProbe = state;
  });
}

async function installOfflineState(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "onLine", {
      configurable: true,
      get: () => false,
    });
  });
}

async function installApiMocks(page: Page, admission: "waiting" | "admitted") {
  const observed: ObservedRequests = {
    attemptBodies: [],
    credentialBodies: [],
    healthCalls: 0,
    order: [],
  };

  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const { pathname } = new URL(request.url());

    if (request.method() === "GET" && pathname === "/api/health") {
      observed.healthCalls += 1;
      await fulfillJSON(route, 200, { status: "ok" });
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v1/me") {
      await fulfillJSON(route, 200, currentUser);
      return;
    }
    if (
      request.method() === "GET" &&
      pathname === `/api/v1/tenants/${tenantID}/capabilities`
    ) {
      await fulfillJSON(route, 200, tenantCapabilities());
      return;
    }
    if (
      request.method() === "GET" &&
      pathname === `/api/v1/media/spaces/${spaceID}`
    ) {
      await fulfillJSON(route, 200, mediaSpace);
      return;
    }
    if (request.method() === "GET" && pathname === "/api/v1/auth/csrf") {
      observed.order.push("csrf");
      await fulfillJSON(route, 200, { csrf_token: "p4-03-csrf" });
      return;
    }
    if (
      request.method() === "POST" &&
      pathname === `/api/v1/media/spaces/${spaceID}/join-attempts`
    ) {
      observed.order.push("attempt");
      observed.attemptBodies.push(request.postDataJSON());
      await fulfillJSON(route, 201, {
        admission_request_id:
          admission === "waiting" ? admissionRequestID : null,
        can_publish_camera_microphone: true,
        can_share_screen: false,
        can_subscribe: true,
        created_at: "2030-08-03T00:02:00Z",
        instance_role: "attendee",
        join_attempt_id: joinAttemptID,
        participant_session_id: participantSessionID,
        room_instance_id: roomInstanceID,
        status: admission,
        updated_at: "2030-08-03T00:02:00Z",
        version: 1,
      });
      return;
    }
    if (
      request.method() === "POST" &&
      pathname === `/api/v1/media/spaces/${spaceID}/join-credentials`
    ) {
      observed.order.push("credential");
      observed.credentialBodies.push(request.postDataJSON());
      await fulfillJSON(route, 200, {
        access_token: credentialToken,
        can_publish_camera_microphone: true,
        can_share_screen: false,
        can_subscribe: true,
        expires_at: "2030-08-03T00:05:00Z",
        instance_role: "attendee",
        join_attempt_id: joinAttemptID,
        participant_session_id: participantSessionID,
        room_instance_id: roomInstanceID,
        server_url: "wss://media.example.test",
      });
      return;
    }

    await fulfillJSON(route, 404, {
      detail: `No P4-03 fixture for ${request.method()} ${pathname}`,
      status: 404,
      title: "Fixture not found",
      type: "urn:tutorhub:problem:not-found",
    });
  });

  return observed;
}

async function openEnglishPrejoin(page: Page) {
  await page.goto(prejoinPath);
  await expect(
    page.getByRole("heading", {
      name: /^(?:Sẵn sàng vào phòng học|Get ready for class)$/,
    }),
  ).toBeVisible();
  const language = page.locator(".language-select select");
  await language.selectOption("en");
  await expect(
    page.getByRole("heading", { name: "Get ready for class" }),
  ).toBeVisible();
}

test.beforeEach(async ({ page }) => {
  await installMediaProbe(page);
});

test("P4-03 keeps initial capture at zero and starts a keyboard-accessible explicit probe", async ({
  page,
}) => {
  const observed = await installApiMocks(page, "admitted");
  await openEnglishPrejoin(page);

  expect(await readMediaProbe(page)).toEqual({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
    stoppedTracks: 0,
  });
  expect(observed.order).toEqual([]);

  const probe = page.getByRole("button", {
    name: "Test camera and microphone",
  });
  await probe.focus();
  await expect(probe).toBeFocused();
  await probe.press("Enter");

  await expect
    .poll(async () => readMediaProbe(page))
    .toMatchObject({ enumerateDevicesCalls: 1, getUserMediaCalls: 1 });
  await expect(page.getByRole("combobox", { name: "Microphone" })).toHaveValue(
    "microphone-1",
  );
  await expect(page.getByRole("combobox", { name: "Camera" })).toHaveValue(
    "camera-1",
  );
  expect(observed.order).toEqual([]);
  expect(
    (await new AxeBuilder({ page }).include(".media-p403-page").analyze())
      .violations,
  ).toEqual([]);
});

test("P4-03 listen-only waiting never captures, requests a credential, or navigates", async ({
  page,
}) => {
  const observed = await installApiMocks(page, "waiting");
  await openEnglishPrejoin(page);

  await page.getByRole("button", { name: "Join listen-only" }).click();

  await expect(
    page.getByRole("heading", { name: "Waiting to be admitted" }),
  ).toBeVisible();
  expect(observed.order).toEqual(["csrf", "attempt"]);
  expect(observed.attemptBodies).toEqual([
    {
      expected_room_instance_id: roomInstanceID,
      expected_space_version: mediaSpace.version,
      join_attempt_id: expect.stringMatching(
        /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
      ),
    },
  ]);
  expect(observed.credentialBodies).toEqual([]);
  expect(await readMediaProbe(page)).toMatchObject({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
  });
  await expect(page).toHaveURL(new RegExp(`${prejoinPath}$`));
});

test("P4-03 requests an admitted attempt before a memory-only credential handoff", async ({
  page,
}) => {
  const observed = await installApiMocks(page, "admitted");
  await openEnglishPrejoin(page);

  await page.getByRole("button", { name: "Join listen-only" }).click();

  await expect(page).toHaveURL(new RegExp(`${roomPath}$`));
  expect(observed.order).toEqual(["csrf", "attempt", "credential"]);
  expect(observed.credentialBodies).toEqual([
    { join_attempt_id: joinAttemptID },
  ]);
  expect(await readMediaProbe(page)).toMatchObject({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
  });

  const privacySurfaces = await page.evaluate(() => ({
    history: JSON.stringify(window.history.state ?? null),
    localStorage: Object.values(window.localStorage),
    sessionStorage: Object.values(window.sessionStorage),
    url: window.location.href,
  }));
  expect(privacySurfaces.url).not.toContain(credentialToken);
  expect(privacySurfaces.history).not.toContain(credentialToken);
  expect(privacySurfaces.localStorage).not.toContain(credentialToken);
  expect(privacySurfaces.sessionStorage).not.toContain(credentialToken);
});

test("P4-03 permission denial remains bounded and preserves listen-only waiting", async ({
  page,
}) => {
  await installDeniedMediaProbe(page);
  const observed = await installApiMocks(page, "waiting");
  await openEnglishPrejoin(page);

  await page
    .getByRole("button", { name: "Test camera and microphone" })
    .click();
  await expect(
    page.getByText(
      "Camera or microphone permission was denied. You can still join listen-only.",
    ),
  ).toBeVisible();
  expect(await readMediaProbe(page)).toMatchObject({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 1,
    stoppedTracks: 0,
  });

  await page.getByRole("button", { name: "Join listen-only" }).click();
  await expect(
    page.getByRole("heading", { name: "Waiting to be admitted" }),
  ).toBeVisible();
  expect(observed.order).toEqual(["csrf", "attempt"]);
  expect(observed.credentialBodies).toEqual([]);
  await expect(page).toHaveURL(new RegExp(`${prejoinPath}$`));
});

test("P4-03 reports offline without probing health or opening media", async ({
  page,
}) => {
  await installOfflineState(page);
  const observed = await installApiMocks(page, "admitted");
  await page.goto(prejoinPath);

  await expect(
    page.getByRole("heading", { level: 1, name: /ngoại tuyến|offline/i }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Thử lại|Retry/ }),
  ).toBeVisible();
  expect(observed.healthCalls).toBe(0);
  expect(observed.order).toEqual([]);
  expect(await readMediaProbe(page)).toEqual({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
    stoppedTracks: 0,
  });
});

test("P4-03 keeps keyboard actions and reflow at forced colors and 320 CSS pixels", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  await installApiMocks(page, "admitted");
  await openEnglishPrejoin(page);

  const probe = page.getByRole("button", {
    name: "Test camera and microphone",
  });
  const listenOnly = page.getByRole("button", { name: "Join listen-only" });
  await expect(probe).toBeVisible();
  await expect(listenOnly).toBeVisible();
  await probe.focus();
  await expect(probe).toBeFocused();
  const horizontalOverflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(horizontalOverflow).toBeLessThanOrEqual(1);
  expect(
    (await new AxeBuilder({ page }).include(".media-p403-page").analyze())
      .violations,
  ).toEqual([]);
});
