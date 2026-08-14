import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

const fixturePath = "/__p4-07-moderation-fixture";

test.skip(
  process.env.E2E_MODE === "staging",
  "The deterministic P4-07 fixture loads Vite source modules; staging acceptance runs separately.",
);

type ModerationAction =
  | "lock_room"
  | "unlock_room"
  | "end_room"
  | "promote_co_host"
  | "demote_co_host"
  | "remote_mute"
  | "remove_participant";

interface FixtureOptions {
  host: boolean;
}

interface FixtureCall {
  action: ModerationAction;
  targetParticipantKey?: string;
}

interface MediaProbeState {
  enumerateDevicesCalls: number;
  getUserMediaCalls: number;
}

interface BrowserReactModule {
  createElement(
    type: unknown,
    props?: Record<string, unknown> | null,
    ...children: unknown[]
  ): unknown;
}

interface BrowserReactDOMModule {
  createRoot(element: Element): { render(node: unknown): void };
}

interface BrowserCommonJSModule<Module> {
  default: Module;
}

interface BrowserComponentsModule {
  RoomContext: { Provider: unknown };
}

interface BrowserParticipant {
  readonly attributes: Readonly<Record<string, string>>;
}

interface BrowserRoom {
  readonly localParticipant: BrowserParticipant;
  readonly remoteParticipants: Map<string, BrowserParticipant>;
  simulateParticipants(options: {
    participants: { audio: boolean; count: number; video: boolean };
    publish: { audio: boolean; useRealTracks: boolean; video: boolean };
  }): Promise<void>;
}

interface BrowserLiveKitModule {
  LogLevel: { silent: unknown };
  Room: new (options: {
    adaptiveStream: boolean;
    dynacast: boolean;
    loggerName: string;
  }) => BrowserRoom;
  getLogger(name: string): { setLevel(level: unknown): void };
  setLogLevel(level: unknown): void;
}

interface BrowserShellModule {
  ClassroomMediaShell: unknown;
}

interface BrowserI18nModule {
  I18nProvider: unknown;
}

declare global {
  interface Window {
    __p407Fixture?: {
      calls: FixtureCall[];
      roomLocked: boolean;
    };
    __p407MediaProbe?: MediaProbeState;
  }
}

async function installFixtureDocument(page: Page) {
  await page.route(`**${fixturePath}`, async (route) => {
    await route.fulfill({
      body: [
        "<!doctype html>",
        '<html lang="en">',
        "<head>",
        '<meta charset="UTF-8">',
        '<meta name="viewport" content="width=device-width, initial-scale=1.0">',
        "<title>P4-07 moderation fixture</title>",
        '<script type="module">',
        "import RefreshRuntime from '/@react-refresh';",
        "RefreshRuntime.injectIntoGlobalHook(window);",
        "window.$RefreshReg$ = () => {};",
        "window.$RefreshSig$ = () => (type) => type;",
        "window.__vite_plugin_react_preamble_installed__ = true;",
        "</script>",
        "</head>",
        '<body><div id="p407-fixture-root"></div></body>',
        "</html>",
      ].join(""),
      contentType: "text/html",
      headers: {
        "Cache-Control": "no-store",
        "Referrer-Policy": "no-referrer",
      },
      status: 200,
    });
  });
}

async function installMediaProbe(page: Page) {
  await page.addInitScript(() => {
    const state: MediaProbeState = {
      enumerateDevicesCalls: 0,
      getUserMediaCalls: 0,
    };
    const events = new EventTarget();
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
        addEventListener: events.addEventListener.bind(events),
        dispatchEvent: events.dispatchEvent.bind(events),
        enumerateDevices: async () => {
          state.enumerateDevicesCalls += 1;
          return [];
        },
        getSupportedConstraints: () => ({}),
        getUserMedia: async () => {
          state.getUserMediaCalls += 1;
          throw new DOMException(
            "The deterministic moderation fixture must not capture media.",
            "NotAllowedError",
          );
        },
        removeEventListener: events.removeEventListener.bind(events),
      },
    });
    Object.defineProperty(window, "RTCRtpReceiver", {
      configurable: true,
      value: class FixtureRTCRtpReceiver {
        playoutDelayHint: number | null = null;

        async getStats() {
          return new Map();
        }
      },
    });
    window.__p407MediaProbe = state;
  });
}

async function mountModerationFixture(page: Page, options: FixtureOptions) {
  const externalRequests: string[] = [];
  const sockets: string[] = [];
  const pageErrors: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.hostname !== "127.0.0.1" && url.hostname !== "localhost") {
      externalRequests.push(request.url());
    }
  });
  page.on("websocket", (socket) => {
    const hostname = new URL(socket.url()).hostname;
    if (hostname !== "127.0.0.1" && hostname !== "localhost") {
      sockets.push(socket.url());
    }
  });
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await installFixtureDocument(page);
  await installMediaProbe(page);
  await page.goto(fixturePath);

  await page.evaluate(async (fixtureOptions) => {
    const dynamicImport = new Function(
      "specifier",
      "return import(specifier)",
    ) as <Module>(specifier: string) => Promise<Module>;
    const shellModulePath = "/src/features/media/ClassroomMediaShell.tsx";
    const transformedShell = await fetch(shellModulePath).then((response) => {
      if (!response.ok) {
        throw new Error(`Could not load the P4-07 shell: ${response.status}.`);
      }
      return response.text();
    });
    const dependencyPath = (pattern: RegExp, label: string) => {
      const path = transformedShell.match(pattern)?.[1];
      if (!path) {
        throw new Error(`Could not resolve the ${label} fixture module.`);
      }
      return path;
    };
    const reactPath = dependencyPath(
      /from "([^"]*\/react\.js\?v=[^"]+)"/,
      "React",
    );
    const reactDOMPath = "/@id/react-dom/client";
    const componentsPath = dependencyPath(
      /from "([^"]*\/@livekit_components-react\.js\?v=[^"]+)"/,
      "LiveKit components",
    );
    const liveKitPath = dependencyPath(
      /from "([^"]*\/livekit-client\.js\?v=[^"]+)"/,
      "LiveKit client",
    );
    const i18nPath = dependencyPath(
      /from "([^"]*\/src\/app\/i18n\.tsx(?:\?t=[^"]+)?)"/,
      "TutorHub i18n",
    );
    const [ReactImport, ReactDOMImport, components, livekit, shell, i18n] =
      await Promise.all([
        dynamicImport<BrowserCommonJSModule<BrowserReactModule>>(reactPath),
        dynamicImport<BrowserCommonJSModule<BrowserReactDOMModule>>(
          reactDOMPath,
        ),
        dynamicImport<BrowserComponentsModule>(componentsPath),
        dynamicImport<BrowserLiveKitModule>(liveKitPath),
        dynamicImport<BrowserShellModule>(shellModulePath),
        dynamicImport<BrowserI18nModule>(i18nPath),
        dynamicImport<unknown>("/src/styles.css"),
      ]);
    const React = ReactImport.default;
    const ReactDOM = ReactDOMImport.default;

    livekit.setLogLevel(livekit.LogLevel.silent);
    const room = new livekit.Room({
      adaptiveStream: true,
      dynacast: true,
      loggerName: "tutorhub-p407-browser-fixture",
    });
    livekit
      .getLogger("tutorhub-p407-browser-fixture")
      .setLevel(livekit.LogLevel.silent);
    await room.simulateParticipants({
      participants: { audio: false, count: 3, video: true },
      publish: { audio: false, useRealTracks: false, video: true },
    });

    const participantKey = (index: number) =>
      `00000000-0000-4000-8000-${String(index).padStart(12, "0")}`;
    const roomParticipants = [
      room.localParticipant,
      ...room.remoteParticipants.values(),
    ];
    roomParticipants.forEach((participant, index) => {
      Object.defineProperty(participant, "_attributes", {
        configurable: true,
        value: { "tutorhub.participant_key": participantKey(index + 1) },
        writable: true,
      });
    });

    const fixtureState = {
      calls: [] as FixtureCall[],
      roomLocked: false,
    };
    window.__p407Fixture = fixtureState;
    const deniedOperations = {
      can_promote_co_host: false,
      can_demote_co_host: false,
      can_remote_mute: false,
      can_remove: false,
    };
    const fullOperations = {
      can_promote_co_host: true,
      can_demote_co_host: false,
      can_remote_mute: true,
      can_remove: true,
    };
    let projection = {
      room_instance_id: "10000000-0000-4000-8000-000000000001",
      room_locked: false,
      projection_version: 7,
      last_signal_sequence: 9,
      self_participant_key: participantKey(1),
      viewer_operations: {
        can_raise_hand: true,
        can_send_reaction: true,
        can_moderate_hands: fixtureOptions.host,
        can_lock_room: fixtureOptions.host,
        can_end_room: fixtureOptions.host,
      },
      roster: [
        {
          participant_key: participantKey(1),
          roster_sequence: 1,
          display_name: "Teacher One",
          instance_role: fixtureOptions.host ? "host" : "attendee",
          connection_state: "connected",
          moderation_operations: deniedOperations,
        },
        {
          participant_key: participantKey(2),
          roster_sequence: 2,
          display_name: "Student One",
          instance_role: "attendee",
          connection_state: "connected",
          moderation_operations: fixtureOptions.host
            ? fullOperations
            : deniedOperations,
        },
        {
          participant_key: participantKey(3),
          roster_sequence: 3,
          display_name: "Student Two",
          instance_role: "attendee",
          connection_state: "connected",
          moderation_operations: deniedOperations,
        },
      ],
      raised_hands: [],
      reactions: { clusters: [], hidden_cluster_count: 0, summary: [] },
      server_time: new Date().toISOString(),
    };
    let providerEffect:
      | { status: "idle" }
      | {
          status: "applied";
          action: ModerationAction;
          targetParticipantKey?: string;
        } = { status: "idle" };
    const rootElement = document.getElementById("p407-fixture-root");
    if (!rootElement) throw new Error("P4-07 fixture root is missing.");
    const root = ReactDOM.createRoot(rootElement);

    const apply = async (
      action: ModerationAction,
      targetParticipantKey?: string,
    ) => {
      fixtureState.calls.push({
        action,
        ...(targetParticipantKey === undefined ? {} : { targetParticipantKey }),
      });
      if (action === "lock_room" || action === "unlock_room") {
        const roomLocked = action === "lock_room";
        fixtureState.roomLocked = roomLocked;
        projection = {
          ...projection,
          room_locked: roomLocked,
          projection_version: projection.projection_version + 1,
        };
      }
      if (action === "promote_co_host" && targetParticipantKey) {
        projection = {
          ...projection,
          projection_version: projection.projection_version + 1,
          roster: projection.roster.map((participant) =>
            participant.participant_key === targetParticipantKey
              ? {
                  ...participant,
                  instance_role: "co_host",
                  moderation_operations: {
                    can_promote_co_host: false,
                    can_demote_co_host: true,
                    can_remote_mute: true,
                    can_remove: true,
                  },
                }
              : participant,
          ),
        };
      }
      providerEffect = {
        status: "applied",
        action,
        ...(targetParticipantKey === undefined ? {} : { targetParticipantKey }),
      };
      render();
    };

    function render() {
      const signals = {
        error: false,
        loading: false,
        mutating: false,
        projection,
        refreshing: false,
        onLowerAllHands: async () => undefined,
        onLowerHand: async () => undefined,
        onResync: async () => undefined,
        onSendReaction: async () => undefined,
        onToggleHand: async () => undefined,
      };
      const moderation = {
        roomLocked: projection.room_locked,
        canLockRoom: projection.viewer_operations.can_lock_room,
        canEndRoom: projection.viewer_operations.can_end_room,
        participantOperations: projection.roster.map((participant) => ({
          participantKey: participant.participant_key,
          canPromoteCoHost:
            participant.moderation_operations.can_promote_co_host,
          canDemoteCoHost: participant.moderation_operations.can_demote_co_host,
          canRemoteMute: participant.moderation_operations.can_remote_mute,
          canRemove: participant.moderation_operations.can_remove,
        })),
        mutationState: { status: "idle" },
        providerEffect,
        onSetRoomLocked: async (locked: boolean) =>
          apply(locked ? "lock_room" : "unlock_room"),
        onEndRoom: async () => apply("end_room"),
        onPromoteCoHost: async (target: string) =>
          apply("promote_co_host", target),
        onDemoteCoHost: async (target: string) =>
          apply("demote_co_host", target),
        onRemoteMute: async (target: string) => apply("remote_mute", target),
        onRemoveParticipant: async (target: string) =>
          apply("remove_participant", target),
      };
      root.render(
        React.createElement(
          "main",
          {
            className: "media-p403-room media-p405-room",
            "data-theme": "dark",
          },
          React.createElement(
            i18n.I18nProvider,
            { initialLanguage: "en" },
            React.createElement(
              components.RoomContext.Provider,
              { value: room },
              React.createElement(shell.ClassroomMediaShell, {
                canPublishCameraMicrophone: true,
                canShareScreen: true,
                canSubscribe: true,
                connectionStatus: "connected",
                moderation,
                onLeave: () => undefined,
                onTerminalMediaCleanup: async () => undefined,
                signals,
              }),
            ),
          ),
        ),
      );
    }
    render();
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    );
  }, options);

  await page.waitForTimeout(50);
  expect(pageErrors).toEqual([]);
  await expect(page.locator(".media-p405-shell")).toBeVisible();
  expect(externalRequests).toEqual([]);
  expect(sockets).toEqual([]);
}

async function readFixtureState(page: Page) {
  return page.evaluate(() => window.__p407Fixture);
}

async function readMediaProbe(page: Page) {
  return page.evaluate(() => window.__p407MediaProbe);
}

test("P4-07 exposes only server-projected moderation actions and never remote unmute", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await mountModerationFixture(page, { host: true });

  const lock = page.getByRole("button", { name: "Lock room" });
  await expect(lock).toHaveAttribute("aria-pressed", "false");
  await lock.click();
  await expect(
    page.getByRole("button", { name: "Unlock room" }),
  ).toHaveAttribute("aria-pressed", "true");

  await page.getByRole("button", { name: "Open classroom roster" }).click();
  await expect(
    page.getByRole("button", { name: "Moderate Student One" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Moderate Student Two" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Moderate Student One" }).click();
  await expect(
    page.getByRole("menuitem", { name: "Promote to co-host" }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "Mute microphone remotely" }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "Remove from room" }),
  ).toBeVisible();
  await expect(page.getByText(/unmute/i)).toHaveCount(0);
  await page.getByRole("menuitem", { name: "Promote to co-host" }).click();

  await page.getByRole("button", { name: "Moderate Student One" }).click();
  await expect(
    page.getByRole("menuitem", { name: "Demote co-host" }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "Promote to co-host" }),
  ).toHaveCount(0);
  await page
    .getByRole("menuitem", { name: "Mute microphone remotely" })
    .click();

  expect((await readFixtureState(page))?.calls).toEqual([
    { action: "lock_room" },
    {
      action: "promote_co_host",
      targetParticipantKey: "00000000-0000-4000-8000-000000000002",
    },
    {
      action: "remote_mute",
      targetParticipantKey: "00000000-0000-4000-8000-000000000002",
    },
  ]);
  expect(await readMediaProbe(page)).toEqual({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
  });
});

test("P4-07 confirms destructive commands and restores keyboard focus", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await mountModerationFixture(page, { host: true });

  await page.getByRole("button", { name: "Open classroom roster" }).click();
  const participantTrigger = page.getByRole("button", {
    name: "Moderate Student One",
  });
  await participantTrigger.focus();
  await page.keyboard.press("Enter");
  const remove = page.getByRole("menuitem", { name: "Remove from room" });
  await expect(remove).toBeVisible();
  await remove.focus();
  await expect(remove).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "Remove this participant?" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Confirm removal" }).click();
  await expect(
    page.getByRole("heading", { name: "Remove this participant?" }),
  ).toHaveCount(0);
  await expect(participantTrigger).toBeFocused();

  await page.getByRole("button", { name: "Close" }).last().click();
  const endRoom = page.getByRole("button", { name: "End room" });
  await endRoom.focus();
  await page.keyboard.press("Enter");
  await expect(
    page.getByRole("heading", { name: "End this classroom?" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Confirm end" }).click();
  await expect(
    page.getByRole("heading", { name: "End this classroom?" }),
  ).toHaveCount(0);
  await expect(endRoom).toBeFocused();

  expect((await readFixtureState(page))?.calls).toEqual([
    {
      action: "remove_participant",
      targetParticipantKey: "00000000-0000-4000-8000-000000000002",
    },
    { action: "end_room" },
  ]);
});

test("P4-07 fails closed when the server projection grants no moderation authority", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await mountModerationFixture(page, { host: false });

  await expect(
    page.getByRole("region", { name: "Classroom moderation controls" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Open classroom roster" }).click();
  await expect(page.getByRole("button", { name: /Moderate / })).toHaveCount(0);
  expect((await readFixtureState(page))?.calls).toEqual([]);
});

test("P4-07 reflows at 320 CSS pixels and 200% with Axe, forced colors, and reduced motion", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  await mountModerationFixture(page, { host: true });

  await expect(page.locator(".media-p407-room-controls")).toHaveCSS(
    "border-top-width",
    "2px",
  );
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

  await page.getByRole("button", { name: "Open classroom roster" }).click();
  await page.getByRole("button", { name: "Moderate Student One" }).click();
  await expect(
    page.getByRole("menu", { name: "Moderate Student One" }),
  ).toBeVisible();
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "Moderate Student One" }),
  ).toBeFocused();
  await page.getByRole("button", { name: "Close" }).last().click();

  await page.setViewportSize({ width: 640, height: 900 });
  await page.evaluate(() => {
    document.documentElement.style.zoom = "200%";
  });
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);
  await expect(page.locator(".media-p405-shell")).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  expect(await readMediaProbe(page)).toEqual({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
  });
});
