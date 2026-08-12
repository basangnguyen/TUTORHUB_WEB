import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

const fixturePath = "/__p4-05-classroom-shell-fixture";

test.skip(
  process.env.E2E_MODE === "staging",
  "The deterministic P4-05 shell fixture loads Vite source modules; provider-backed staging acceptance runs separately.",
);

interface FixtureOptions {
  canPublishCameraMicrophone?: boolean;
  canShareScreen?: boolean;
  canSubscribe?: boolean;
  participantCount: number;
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

interface BrowserRoom {
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
    __p405Fixture?: {
      leaveCalls: number;
      participantCount: number;
    };
    __p405MediaProbe?: MediaProbeState;
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
        "<title>P4-05 classroom shell fixture</title>",
        '<script type="module">',
        "import RefreshRuntime from '/@react-refresh';",
        "RefreshRuntime.injectIntoGlobalHook(window);",
        "window.$RefreshReg$ = () => {};",
        "window.$RefreshSig$ = () => (type) => type;",
        "window.__vite_plugin_react_preamble_installed__ = true;",
        "</script>",
        "</head>",
        '<body><div id="p405-fixture-root"></div></body>',
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
            "The deterministic shell fixture must not capture media.",
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
    window.__p405MediaProbe = state;
  });
}

async function mountClassroomFixture(page: Page, options: FixtureOptions) {
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
        throw new Error(`Could not load the P4-05 shell: ${response.status}.`);
      }
      return response.text();
    });
    const dependencyPath = (pattern: RegExp, label: string) => {
      const path = transformedShell.match(pattern)?.[1];
      if (!path)
        throw new Error(`Could not resolve the ${label} fixture module.`);
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
      loggerName: "tutorhub-p405-browser-fixture",
    });
    livekit
      .getLogger("tutorhub-p405-browser-fixture")
      .setLevel(livekit.LogLevel.silent);

    const publishLocalVideo = fixtureOptions.canPublishCameraMicrophone ?? true;
    await room.simulateParticipants({
      participants: {
        audio: false,
        count: fixtureOptions.participantCount + (publishLocalVideo ? 0 : 1),
        video: true,
      },
      publish: {
        audio: false,
        useRealTracks: false,
        video: publishLocalVideo,
      },
    });

    const fixtureState = {
      leaveCalls: 0,
      participantCount: fixtureOptions.participantCount,
    };
    window.__p405Fixture = fixtureState;
    const rootElement = document.getElementById("p405-fixture-root");
    if (!rootElement) throw new Error("P4-05 fixture root is missing.");
    const root = ReactDOM.createRoot(rootElement);
    root.render(
      React.createElement(
        "main",
        { className: "media-p403-room media-p405-room", "data-theme": "dark" },
        React.createElement(
          i18n.I18nProvider,
          { initialLanguage: "en" },
          React.createElement(
            components.RoomContext.Provider,
            { value: room },
            React.createElement(shell.ClassroomMediaShell, {
              canPublishCameraMicrophone:
                fixtureOptions.canPublishCameraMicrophone ?? true,
              canShareScreen: fixtureOptions.canShareScreen ?? true,
              canSubscribe: fixtureOptions.canSubscribe ?? true,
              connectionStatus: "connected",
              onLeave: () => {
                fixtureState.leaveCalls += 1;
              },
            }),
          ),
        ),
      ),
    );
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    );
  }, options);

  await page.waitForTimeout(50);
  expect(pageErrors).toEqual([]);
  await expect(page.locator(".media-p405-shell")).toBeVisible();
  expect(sockets).toEqual([]);
}

async function readMediaProbe(page: Page) {
  return page.evaluate(() => window.__p405MediaProbe);
}

for (const participantCount of [2, 5, 25, 50]) {
  test(`P4-05 bounds the ${participantCount}-participant desktop fixture`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1_280, height: 900 });
    await mountClassroomFixture(page, { participantCount });

    const expectedVisible = Math.min(participantCount, 12);
    await expect(page.locator(".media-p405-grid > li")).toHaveCount(
      expectedVisible,
    );
    await expect(page.locator(".media-p405-tile video")).toHaveCount(
      expectedVisible,
    );
    await expect(page.locator(".media-p405-grid--capacity-12")).toBeVisible();
    await expect(
      page.getByText(
        `Page 1 of ${Math.max(1, Math.ceil(participantCount / 12))}`,
        { exact: true },
      ),
    ).toBeVisible();
    expect(await readMediaProbe(page)).toEqual({
      enumerateDevicesCalls: 0,
      getUserMediaCalls: 0,
    });
  });
}

test("P4-05 keeps pagination focus and toolbar roving deterministic", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await mountClassroomFixture(page, { participantCount: 25 });

  const toolbar = page.getByRole("toolbar", { name: "Classroom controls" });
  const microphone = page.getByRole("button", { name: "Turn microphone on" });
  const camera = page.getByRole("button", { name: "Turn camera off" });
  await microphone.focus();
  await expect(microphone).toBeFocused();
  await toolbar.press("ArrowRight");
  await expect(camera).toBeFocused();
  await toolbar.press("End");
  await expect(
    page.getByRole("button", { name: "Active speaker" }),
  ).toBeFocused();
  await toolbar.press("Home");
  await expect(microphone).toBeFocused();

  const nextPage = page.getByRole("button", { name: "Next video page" });
  await nextPage.click();
  await expect(page.getByText("Page 2 of 3", { exact: true })).toBeVisible();
  await expect(nextPage).toBeFocused();
  await expect(page.locator(".media-p405-grid > li")).toHaveCount(12);
  await expect(page.locator(".media-p405-tile video")).toHaveCount(12);

  const leaveRoom = page.getByRole("button", { name: "Leave room" });
  await leaveRoom.click();
  const leaveDialog = page.getByRole("dialog", {
    name: "Leave the classroom?",
  });
  await expect(leaveDialog).toBeVisible();
  await leaveDialog
    .getByRole("button", { name: "Stay" })
    .filter({ hasText: "Stay" })
    .click();
  await expect(leaveRoom).toBeFocused();

  await leaveRoom.click();
  await page.getByRole("button", { name: "Leave classroom" }).click();
  await expect
    .poll(() => page.evaluate(() => window.__p405Fixture?.leaveCalls))
    .toBe(1);
});

test("P4-05 reflows at 320 CSS pixels and 200% without accessibility violations", async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 900 });
  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  await mountClassroomFixture(page, { participantCount: 50 });

  await expect(page.locator(".media-p405-grid--capacity-4")).toBeVisible();
  await expect(page.locator(".media-p405-grid > li")).toHaveCount(4);
  await expect(page.locator(".media-p405-tile video")).toHaveCount(4);
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);
  expect(
    (await new AxeBuilder({ page }).include(".media-p405-shell").analyze())
      .violations,
  ).toEqual([]);

  await page.setViewportSize({ width: 640, height: 900 });
  await page.evaluate(() => {
    document.documentElement.style.zoom = "200%";
  });
  await expect(page.locator(".media-p405-grid--capacity-4")).toBeVisible();
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    ),
  ).toBeLessThanOrEqual(1);

  await page.getByRole("button", { name: "Active speaker" }).click();
  const participantDrawerTrigger = page
    .locator("button")
    .filter({ hasText: "Open participant list" });
  await participantDrawerTrigger.click();
  await expect(participantDrawerTrigger).toHaveAttribute(
    "aria-expanded",
    "true",
  );
  await expect(page.locator(".media-p405-drawer")).toBeVisible();
  await page
    .locator(".media-p405-drawer")
    .getByRole("button", { name: "Close" })
    .filter({ hasText: "Close" })
    .click();
  await expect(participantDrawerTrigger).toBeFocused();
});

test("P4-05 listen-only fixture never captures media and exposes no publish controls", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1_280, height: 900 });
  await page.emulateMedia({ forcedColors: "active", reducedMotion: "reduce" });
  await mountClassroomFixture(page, {
    canPublishCameraMicrophone: false,
    canShareScreen: false,
    canSubscribe: true,
    participantCount: 5,
  });

  await expect(
    page.getByText("Listen-only mode", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "You are in listen-only mode; camera and microphone publishing was not granted.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Turn microphone|Turn camera/i }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /Share screen|Stop sharing screen/i }),
  ).toHaveCount(0);
  await expect(page.locator(".media-p405-tile video")).toHaveCount(5);
  expect(await readMediaProbe(page)).toEqual({
    enumerateDevicesCalls: 0,
    getUserMediaCalls: 0,
  });
  expect(
    (await new AxeBuilder({ page }).include(".media-p405-shell").analyze())
      .violations,
  ).toEqual([]);
});
