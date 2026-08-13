import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

const fixturePath = "/__p4-06-participant-signals-fixture";

test.skip(
  process.env.E2E_MODE === "staging",
  "The deterministic P4-06 fixture loads Vite source modules; staging acceptance runs separately.",
);

interface FixtureOptions {
  moderator?: boolean;
  participantCount: number;
}

interface MediaProbeState {
  enumerateDevicesCalls: number;
  getUserMediaCalls: number;
}

interface SignalCall {
  kind:
    "hand_lower_all" | "hand_lower_one" | "hand_toggle" | "reaction" | "resync";
  value?: string;
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
    __p406Fixture?: {
      calls: SignalCall[];
      participantCount: number;
    };
    __p406MediaProbe?: MediaProbeState;
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
        "<title>P4-06 participant signals fixture</title>",
        '<script type="module">',
        "import RefreshRuntime from '/@react-refresh';",
        "RefreshRuntime.injectIntoGlobalHook(window);",
        "window.$RefreshReg$ = () => {};",
        "window.$RefreshSig$ = () => (type) => type;",
        "window.__vite_plugin_react_preamble_installed__ = true;",
        "</script>",
        "</head>",
        '<body><div id="p406-fixture-root"></div></body>',
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
            "The deterministic participant fixture must not capture media.",
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
    window.__p406MediaProbe = state;
  });
}

async function mountParticipantFixture(page: Page, options: FixtureOptions) {
  const sockets: string[] = [];
  const pageErrors: string[] = [];
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
        throw new Error(`Could not load the P4-06 shell: ${response.status}.`);
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
      loggerName: "tutorhub-p406-browser-fixture",
    });
    livekit
      .getLogger("tutorhub-p406-browser-fixture")
      .setLevel(livekit.LogLevel.silent);
    await room.simulateParticipants({
      participants: {
        audio: false,
        count: fixtureOptions.participantCount,
        video: true,
      },
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
      calls: [] as SignalCall[],
      participantCount: fixtureOptions.participantCount,
    };
    window.__p406Fixture = fixtureState;
    const roster = Array.from(
      { length: fixtureOptions.participantCount },
      (_, index) => ({
        participant_key: participantKey(index + 1),
        roster_sequence: index + 1,
        display_name: index === 0 ? "Teacher One" : `Student ${index}`,
        instance_role: index === 0 ? "host" : "attendee",
        connection_state: "connected",
      }),
    );
    const initialHandIndexes =
      fixtureOptions.participantCount > 2 ? [1, 2] : [1];
    const raisedHands = initialHandIndexes.map((index, queueIndex) => ({
      participant_key: participantKey(index + 1),
      signal_sequence: queueIndex + 1,
      raised_at: new Date(Date.now() - (2 - queueIndex) * 1_000).toISOString(),
      display_name: `Student ${index}`,
      queue_position: queueIndex + 1,
    }));
    const futureExpiry = new Date(Date.now() + 60_000).toISOString();
    let projection = {
      room_instance_id: "10000000-0000-4000-8000-000000000001",
      projection_version: 4,
      last_signal_sequence: 9,
      self_participant_key: participantKey(1),
      viewer_operations: {
        can_raise_hand: true,
        can_send_reaction: true,
        can_moderate_hands: fixtureOptions.moderator ?? false,
      },
      roster,
      raised_hands: raisedHands,
      reactions: {
        clusters: [
          {
            cluster_id: "thumbs_up:4",
            reaction: "thumbs_up",
            count: 2,
            count_label: "2",
            first_signal_sequence: 4,
            last_signal_sequence: 5,
            accepted_at: new Date(Date.now() - 500).toISOString(),
            expires_at: futureExpiry,
          },
          {
            cluster_id: "clap:6",
            reaction: "clap",
            count: 3,
            count_label: "3",
            first_signal_sequence: 6,
            last_signal_sequence: 8,
            accepted_at: new Date(Date.now() - 300).toISOString(),
            expires_at: futureExpiry,
          },
          {
            cluster_id: "heart:9",
            reaction: "heart",
            count: 1,
            count_label: "1",
            first_signal_sequence: 9,
            last_signal_sequence: 9,
            accepted_at: new Date(Date.now() - 100).toISOString(),
            expires_at: futureExpiry,
          },
        ],
        hidden_cluster_count: 0,
        summary: [
          { reaction: "thumbs_up", count: 2, count_label: "2" },
          { reaction: "clap", count: 3, count_label: "3" },
          { reaction: "heart", count: 1, count_label: "1" },
        ],
      },
      server_time: new Date().toISOString(),
    };

    const normalizeHands = (hands: typeof raisedHands) =>
      hands.map((hand, index) => ({ ...hand, queue_position: index + 1 }));
    const rootElement = document.getElementById("p406-fixture-root");
    if (!rootElement) throw new Error("P4-06 fixture root is missing.");
    const root = ReactDOM.createRoot(rootElement);
    const render = () => {
      const signals = {
        error: false,
        loading: false,
        mutating: false,
        projection,
        refreshing: false,
        onLowerAllHands: async () => {
          fixtureState.calls.push({ kind: "hand_lower_all" });
          projection = { ...projection, raised_hands: [] };
          render();
        },
        onLowerHand: async (targetParticipantKey: string) => {
          fixtureState.calls.push({
            kind: "hand_lower_one",
            value: targetParticipantKey,
          });
          projection = {
            ...projection,
            raised_hands: normalizeHands(
              projection.raised_hands.filter(
                ({ participant_key }) =>
                  participant_key !== targetParticipantKey,
              ),
            ),
          };
          render();
        },
        onResync: async () => {
          fixtureState.calls.push({ kind: "resync" });
        },
        onSendReaction: async (reaction: string) => {
          fixtureState.calls.push({ kind: "reaction", value: reaction });
        },
        onToggleHand: async () => {
          fixtureState.calls.push({ kind: "hand_toggle" });
          const ownKey = projection.self_participant_key;
          const ownRaised = projection.raised_hands.some(
            ({ participant_key }) => participant_key === ownKey,
          );
          projection = {
            ...projection,
            raised_hands: ownRaised
              ? normalizeHands(
                  projection.raised_hands.filter(
                    ({ participant_key }) => participant_key !== ownKey,
                  ),
                )
              : [
                  ...projection.raised_hands,
                  {
                    participant_key: ownKey,
                    signal_sequence: projection.last_signal_sequence + 1,
                    raised_at: new Date().toISOString(),
                    display_name: "Teacher One",
                    queue_position: projection.raised_hands.length + 1,
                  },
                ],
          };
          render();
        },
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
                onLeave: () => undefined,
                onTerminalMediaCleanup: async () => undefined,
                signals,
              }),
            ),
          ),
        ),
      );
    };
    render();
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    );
  }, options);

  await page.waitForTimeout(50);
  expect(pageErrors).toEqual([]);
  await expect(page.locator(".media-p405-shell")).toBeVisible();
  expect(sockets).toEqual([]);
}

async function readFixtureState(page: Page) {
  return page.evaluate(() => window.__p406Fixture);
}

async function readMediaProbe(page: Page) {
  return page.evaluate(() => window.__p406MediaProbe);
}

for (const participantCount of [2, 5, 25, 50]) {
  test(`P4-06 renders an authoritative ${participantCount}-participant roster without identifiers`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1_280, height: 900 });
    await mountParticipantFixture(page, { participantCount });

    await expect(
      page.getByText(`Connected / ${participantCount} participants`, {
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.locator(".media-p406-reaction-cluster")).toHaveCount(3);
    await page.getByRole("button", { name: "Open classroom roster" }).click();
    const roster = page.getByRole("list", {
      name: "Participants in classroom order",
    });
    await expect(roster.getByRole("listitem")).toHaveCount(participantCount);
    await expect(roster.getByRole("listitem").first()).toContainText("You");
    await expect(page.locator("body")).not.toContainText(
      "00000000-0000-4000-8000",
    );
    expect(await readMediaProbe(page)).toEqual({
      enumerateDevicesCalls: 0,
      getUserMediaCalls: 0,
    });
  });
}

test("P4-06 supports toolbar keyboard order, hand ACK, and allowlisted reaction", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await mountParticipantFixture(page, { participantCount: 5 });

  const hand = page.getByRole("button", { name: "Raise hand" });
  await hand.focus();
  await expect(hand).toBeFocused();
  await page
    .getByRole("toolbar", { name: "Classroom controls" })
    .press("ArrowRight");
  await expect(
    page.getByRole("button", { name: "Send a reaction" }),
  ).toBeFocused();

  await hand.click();
  await expect(
    page.getByRole("button", { name: "Lower your hand" }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".media-p406-status")).toContainText(
    "Your queue position is 3",
  );

  await page.getByRole("button", { name: "Send a reaction" }).click();
  await page.getByRole("menuitem", { name: "Clap" }).click();
  await expect(page.locator(".media-p406-status")).toContainText(
    "Reaction sent",
  );
  await expect
    .poll(async () => (await readFixtureState(page))?.calls)
    .toEqual([{ kind: "hand_toggle" }, { kind: "reaction", value: "clap" }]);
});

test("P4-06 moderator lowers one hand then the remaining FIFO queue", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await mountParticipantFixture(page, { moderator: true, participantCount: 5 });

  const rosterTrigger = page.getByRole("button", {
    name: "Open classroom roster",
  });
  await rosterTrigger.click();
  await page.getByRole("button", { name: "Lower Student 1's hand" }).click();
  await expect(
    page.getByRole("button", { name: "Lower Student 1's hand" }),
  ).toHaveCount(0);
  await expect(page.getByText("Hand raised, position 1")).toBeVisible();
  await page.getByRole("button", { name: "Lower all hands" }).click();
  await expect(page.getByText(/Hand raised, position/)).toHaveCount(0);

  const calls = (await readFixtureState(page))?.calls;
  expect(calls?.map(({ kind }) => kind)).toEqual([
    "hand_lower_one",
    "hand_lower_all",
  ]);
});

test("P4-06 reflows at 320 CSS pixels and 200% with Axe, forced colors, and reduced motion", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  await mountParticipantFixture(page, {
    moderator: true,
    participantCount: 50,
  });

  await page.getByRole("button", { name: "Open classroom roster" }).click();
  await expect(
    page.getByRole("list", { name: "Participants in classroom order" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
  await expect(page.locator(".media-p406-reaction-cluster").first()).toHaveCSS(
    "animation-name",
    "none",
  );
  await expect(page.locator(".media-p406-roster li").first()).toHaveCSS(
    "border-top-width",
    "2px",
  );

  await page.getByRole("button", { name: "Close" }).last().click();
  await expect(
    page.getByRole("heading", { name: "Classroom roster" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Send a reaction" }).click();
  await expect(
    page.getByRole("menu", { name: "Choose a reaction" }),
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
    page.getByRole("button", { name: "Send a reaction" }),
  ).toBeFocused();
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
  await expect(page.locator(".media-p406-reaction-cluster")).toHaveCount(3);
  await page.getByRole("button", { name: "Open classroom roster" }).click();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});
