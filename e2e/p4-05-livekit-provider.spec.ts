import { createHmac, randomUUID } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";

const providerConfirmation = "I_UNDERSTAND_P4_05_BROWSER_PROVIDER_RESOURCE";
const providerEnabled =
  process.env.P4_05_BROWSER_PROVIDER_CONFIRM?.trim() === providerConfirmation;
const publisherFixturePath = "/__p4-05-provider-publisher";
const subscriberFixturePath = "/__p4-05-provider-subscriber";

interface ProviderConfiguration {
  apiKey: string;
  apiSecret: string;
  roomName: string;
  serverURL: string;
}

interface BrowserReactModule {
  createElement(
    type: unknown,
    props?: Record<string, unknown> | null,
    ...children: unknown[]
  ): unknown;
}

interface BrowserReactDOMModule {
  createRoot(element: Element): {
    render(node: unknown): void;
    unmount(): void;
  };
}

interface BrowserCommonJSModule<Module> {
  default: Module;
}

interface BrowserClassroomModule {
  ClassroomLiveKitRoom: unknown;
}

interface BrowserI18nModule {
  I18nProvider: unknown;
}

interface BrowserRemoteTrackPublication {
  source: string;
  setSubscribed(subscribed: boolean): void;
}

interface BrowserRawRoom {
  connect(
    serverURL: string,
    accessToken: string,
    options: { autoSubscribe: boolean },
  ): Promise<void>;
  disconnect(stopTracks?: boolean): Promise<void>;
  on(
    event: unknown,
    listener: (...arguments_: unknown[]) => void,
  ): BrowserRawRoom;
  remoteParticipants: Map<
    string,
    {
      trackPublications: Map<string, BrowserRemoteTrackPublication>;
    }
  >;
}

interface BrowserLiveKitModule {
  Room: {
    new (options: {
      adaptiveStream: boolean;
      dynacast: boolean;
      loggerName: string;
    }): BrowserRawRoom;
    prototype: BrowserRawRoom;
  };
  RoomEvent: {
    Connected: unknown;
    DataReceived: unknown;
    ParticipantConnected: unknown;
    ParticipantDisconnected: unknown;
    TrackPublished: unknown;
    TrackSubscribed: unknown;
  };
  RemoteTrackPublication: {
    prototype: BrowserRemoteTrackPublication;
  };
}

interface PublisherProbe {
  capturedTracks: MediaStreamTrack[];
  connected: boolean;
  disconnected: boolean;
  displayMediaCalls: number;
  leaveCompleted: boolean;
  providerFailed: boolean;
  userMediaCalls: number;
}

interface SubscriberProbe {
  connected: boolean;
  dataPackets: number;
  failed: boolean;
  publishedSources: string[];
  remoteParticipantCount: number;
  remotePublicationCount: number;
  remoteParticipantDisconnects: number;
  requestedSources: string[];
  subscribedSources: string[];
}

declare global {
  interface Window {
    __p405ProviderCleanup?: () => Promise<void> | void;
    __p405PublisherProbe?: PublisherProbe;
    __p405SubscriberProbe?: SubscriberProbe;
    __p405SubscriberRefresh?: () => void;
  }
}

const providerConfiguration = providerEnabled
  ? readProviderConfiguration()
  : null;

test.use({
  launchOptions: {
    args: [
      "--use-fake-device-for-media-stream",
      "--use-fake-ui-for-media-stream",
    ],
  },
  permissions: ["camera", "microphone"],
  screenshot: "off",
  trace: "off",
  video: "off",
});

test.describe("P4-05 isolated browser provider gate", () => {
  test.skip(
    !providerEnabled,
    "P4-05 browser provider acceptance is explicit opt-in only.",
  );

  test("real classroom room publishes camera, microphone, then explicit screen share", async ({
    browserName,
    context,
    page: publisherPage,
  }) => {
    test.skip(
      browserName !== "chromium",
      "This gate requires Chromium fake media.",
    );
    test.setTimeout(120_000);
    if (!providerConfiguration) {
      throw new Error("P4-05 provider configuration was not initialized.");
    }

    const publisherToken = issueParticipantToken(providerConfiguration, {
      canPublish: true,
      canSubscribe: true,
      name: "Publisher fixture",
      publishSources: ["camera", "microphone", "screen_share"],
    });
    const subscriberToken = issueParticipantToken(providerConfiguration, {
      canPublish: false,
      canSubscribe: true,
      name: "Subscriber fixture",
      publishSources: [],
    });
    const subscriberPage = await context.newPage();

    try {
      await installFixtureDocument(subscriberPage, subscriberFixturePath);
      await subscriberPage.goto(subscriberFixturePath);
      expect(await isLoopbackFixture(subscriberPage)).toBe(true);
      await mountClassroomSubscriber(
        subscriberPage,
        providerConfiguration.serverURL,
        subscriberToken,
      );
      await expect
        .poll(() => readSubscriberConnected(subscriberPage), {
          timeout: 30_000,
        })
        .toBe(true);
      await expect(
        subscriberPage.getByRole("button", { name: "Turn microphone on" }),
      ).toHaveCount(0);
      await expect(
        subscriberPage.getByRole("button", { name: "Turn camera on" }),
      ).toHaveCount(0);
      await expect(
        subscriberPage.getByRole("button", { name: "Share screen" }),
      ).toHaveCount(0);

      await installFixtureDocument(publisherPage, publisherFixturePath);
      await installPublisherMedia(publisherPage);
      await publisherPage.goto(publisherFixturePath);
      expect(await isLoopbackFixture(publisherPage)).toBe(true);
      await mountClassroomPublisher(
        publisherPage,
        providerConfiguration.serverURL,
        publisherToken,
      );

      await expect
        .poll(() => readPublisherConnected(publisherPage), { timeout: 30_000 })
        .toBe(true);
      await expect
        .poll(() => readPublisherUserMediaCalls(publisherPage), {
          timeout: 30_000,
        })
        .toBeGreaterThanOrEqual(2);
      await expect(
        subscriberPage.locator(
          '.media-p405-tile[data-lk-source="camera"] video',
        ),
      ).toHaveCount(1, { timeout: 30_000 });
      await expect(subscriberPage.locator("audio")).toHaveCount(1, {
        timeout: 30_000,
      });

      expect(await readDisplayMediaCalls(publisherPage)).toBe(0);
      await expect(
        subscriberPage.locator(
          '.media-p405-tile[data-lk-source="screen_share"] video',
        ),
      ).toHaveCount(0);

      await publisherPage.getByRole("button", { name: "Share screen" }).click();
      await expect
        .poll(() => readDisplayMediaCalls(publisherPage), { timeout: 15_000 })
        .toBe(1);
      await expect(
        subscriberPage.locator(
          '.media-p405-tile[data-lk-source="screen_share"] video',
        ),
      ).toHaveCount(1, { timeout: 30_000 });

      expect(await readDisplayMediaCalls(publisherPage)).toBe(1);

      expect(
        await containsCredentialMaterial(publisherPage, [
          publisherToken,
          subscriberToken,
        ]),
      ).toBe(false);
      expect(
        await containsCredentialMaterial(subscriberPage, [
          publisherToken,
          subscriberToken,
        ]),
      ).toBe(false);

      await publisherPage.getByRole("button", { name: "Leave room" }).click();
      await publisherPage
        .getByRole("dialog", { name: "Leave the classroom?" })
        .getByRole("button", { name: "Leave classroom" })
        .click();
      await expect
        .poll(() => readPublisherLeaveCompleted(publisherPage), {
          timeout: 30_000,
        })
        .toBe(true);
      await expect
        .poll(() => publisherCapturedTracksEnded(publisherPage), {
          timeout: 30_000,
        })
        .toBe(true);
      await expect(
        subscriberPage.locator(
          '.media-p405-tile[data-lk-source="camera"] video',
        ),
      ).toHaveCount(0, { timeout: 30_000 });
      await expect(
        subscriberPage.locator(
          '.media-p405-tile[data-lk-source="screen_share"] video',
        ),
      ).toHaveCount(0, { timeout: 30_000 });
      await expect(subscriberPage.locator("audio")).toHaveCount(0, {
        timeout: 30_000,
      });
      expect(await readSubscriberConnected(subscriberPage)).toBe(true);
      expect(await readDisplayMediaCalls(publisherPage)).toBe(1);
    } finally {
      await Promise.allSettled([
        safelyCleanupFixture(publisherPage),
        safelyCleanupFixture(subscriberPage),
      ]);
      await subscriberPage.close();
    }
  });
});

function readProviderConfiguration(): ProviderConfiguration {
  rejectConflictingProviderEnvironment();
  if ((process.env.E2E_MODE?.trim() || "local") !== "local") {
    throw new Error(
      "P4-05 browser provider gate requires a local Vite fixture.",
    );
  }
  const serverURL = requireProviderVariable("LIVEKIT_URL");
  const apiKey = requireProviderVariable("LIVEKIT_API_KEY");
  const apiSecret = requireProviderVariable("LIVEKIT_API_SECRET");
  const roomName = requireProviderVariable("P4_05_PROVIDER_ROOM_NAME");

  let parsedServerURL: URL;
  try {
    parsedServerURL = new URL(serverURL);
  } catch {
    throw new Error("P4-05 provider URL must be a secure WebSocket origin.");
  }
  if (
    parsedServerURL.protocol !== "wss:" ||
    parsedServerURL.username ||
    parsedServerURL.password ||
    parsedServerURL.pathname !== "/" ||
    parsedServerURL.search ||
    parsedServerURL.hash
  ) {
    throw new Error("P4-05 provider URL must be a secure WebSocket origin.");
  }
  if (!/^r_[a-f0-9]{32}$/.test(roomName)) {
    throw new Error("P4-05 provider room name is not an accepted opaque name.");
  }

  return {
    apiKey,
    apiSecret,
    roomName,
    serverURL: parsedServerURL.origin,
  };
}

function rejectConflictingProviderEnvironment(): void {
  const conflictingNames = [
    "DATABASE_MIGRATION_URL",
    "DATABASE_POOL_URL",
    "P4_02_DISPOSABLE_CONFIRM",
    "P4_02_OWNER_PREFLIGHT",
    "P4_02_ACL_PROVISION_CONFIRM",
    "P4_02_PROVIDER_SMOKE_CONFIRM",
    "P4_02_SHARED_CONFIRM",
    "P4_02_SHARED_ACL_PROVISION_CONFIRM",
    "P4_04_DISPOSABLE_CONFIRM",
    "P4_04_OWNER_PREFLIGHT",
    "P4_04_ACL_PROVISION_CONFIRM",
    "P4_04_SHARED_CONFIRM",
    "P4_04_SHARED_ACL_PROVISION_CONFIRM",
    "P4_04_SHARED_SNAPSHOT_CONFIRM",
    "P4_05_DISPOSABLE_CONFIRM",
    "P4_05_ACL_PROVISION_CONFIRM",
    "P4_05_SHARED_CONFIRM",
    "P4_05_SHARED_ACL_PROVISION_CONFIRM",
    "P4_05_SHARED_SNAPSHOT_CONFIRM",
  ] as const;
  if (
    conflictingNames.some((name) => (process.env[name]?.trim().length ?? 0) > 0)
  ) {
    throw new Error(
      "P4-05 browser provider gate refuses database, stale, shared, or provisioning state.",
    );
  }
}

function requireProviderVariable(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) {
    throw new Error(`P4-05 browser provider gate requires ${name}.`);
  }
  return value;
}

function issueParticipantToken(
  configuration: ProviderConfiguration,
  grant: {
    canPublish: boolean;
    canSubscribe: boolean;
    name: string;
    publishSources: readonly string[];
  },
): string {
  const issuedAt = Math.floor(Date.now() / 1_000);
  const identity = `p405-${randomUUID()}`;
  const header = encodeTokenSegment({ alg: "HS256", typ: "JWT" });
  const payload = encodeTokenSegment({
    exp: issuedAt + 300,
    iat: issuedAt,
    identity,
    iss: configuration.apiKey,
    name: grant.name,
    nbf: issuedAt,
    sub: identity,
    video: {
      canPublish: grant.canPublish,
      canPublishData: false,
      ...(grant.publishSources.length > 0
        ? { canPublishSources: grant.publishSources }
        : {}),
      canSubscribe: grant.canSubscribe,
      canUpdateOwnMetadata: false,
      room: configuration.roomName,
      roomJoin: true,
    },
  });
  const unsignedToken = `${header}.${payload}`;
  const signature = createHmac("sha256", configuration.apiSecret)
    .update(unsignedToken)
    .digest("base64url");
  return `${unsignedToken}.${signature}`;
}

function encodeTokenSegment(value: unknown): string {
  return Buffer.from(JSON.stringify(value), "utf8").toString("base64url");
}

async function installFixtureDocument(page: Page, fixturePath: string) {
  await page.route(`**${fixturePath}`, async (route) => {
    await route.fulfill({
      body: [
        "<!doctype html>",
        '<html lang="en">',
        "<head>",
        '<meta charset="UTF-8">',
        '<meta name="viewport" content="width=device-width, initial-scale=1.0">',
        "<title>P4-05 provider fixture</title>",
        '<script type="module">',
        "import RefreshRuntime from '/@react-refresh';",
        "RefreshRuntime.injectIntoGlobalHook(window);",
        "window.$RefreshReg$ = () => {};",
        "window.$RefreshSig$ = () => (type) => type;",
        "window.__vite_plugin_react_preamble_installed__ = true;",
        "</script>",
        "</head>",
        '<body><div id="p405-provider-root"></div></body>',
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

async function installPublisherMedia(page: Page) {
  await page.addInitScript(() => {
    const mediaDevices = navigator.mediaDevices;
    const originalGetUserMedia = mediaDevices.getUserMedia.bind(mediaDevices);
    const probe: PublisherProbe = {
      capturedTracks: [],
      connected: false,
      disconnected: false,
      displayMediaCalls: 0,
      leaveCompleted: false,
      providerFailed: false,
      userMediaCalls: 0,
    };
    window.__p405PublisherProbe = probe;

    Object.defineProperty(mediaDevices, "getUserMedia", {
      configurable: true,
      value: async (constraints: MediaStreamConstraints) => {
        probe.userMediaCalls += 1;
        const stream = await originalGetUserMedia(constraints);
        probe.capturedTracks.push(...stream.getTracks());
        return stream;
      },
    });
    Object.defineProperty(mediaDevices, "getDisplayMedia", {
      configurable: true,
      value: async () => {
        probe.displayMediaCalls += 1;
        const canvas = document.createElement("canvas");
        canvas.width = 640;
        canvas.height = 360;
        const context = canvas.getContext("2d");
        if (!context) {
          throw new DOMException(
            "Display fixture unavailable.",
            "NotReadableError",
          );
        }
        let frame = 0;
        const draw = () => {
          frame += 1;
          context.fillStyle = frame % 2 === 0 ? "#1f6f78" : "#143642";
          context.fillRect(0, 0, canvas.width, canvas.height);
        };
        draw();
        const timer = window.setInterval(draw, 100);
        const stream = canvas.captureStream(10);
        const [videoTrack] = stream.getVideoTracks();
        if (!videoTrack) {
          window.clearInterval(timer);
          throw new DOMException(
            "Display fixture unavailable.",
            "NotReadableError",
          );
        }
        videoTrack.addEventListener(
          "ended",
          () => {
            window.clearInterval(timer);
          },
          { once: true },
        );
        probe.capturedTracks.push(videoTrack);
        return stream;
      },
    });
  });
}

async function mountClassroomPublisher(
  page: Page,
  serverURL: string,
  accessToken: string,
) {
  await page.evaluate(
    async ({ providerURL, token }) => {
      const dynamicImport = new Function(
        "specifier",
        "return import(specifier)",
      ) as <Module>(specifier: string) => Promise<Module>;
      const roomModulePath = "/src/features/media/ClassroomLiveKitRoom.tsx";
      const transformedRoom = await fetch(roomModulePath).then((response) => {
        if (!response.ok) {
          throw new Error("Could not load the P4-05 classroom fixture.");
        }
        return response.text();
      });
      const dependencyPath = (pattern: RegExp, label: string) => {
        const path = transformedRoom.match(pattern)?.[1];
        if (!path)
          throw new Error(`Could not resolve the ${label} fixture module.`);
        return path;
      };
      const reactPath = dependencyPath(
        /from "([^"]*\/react\.js\?v=[^"]+)"/,
        "React",
      );
      const dependencyVersion = new URL(reactPath, window.location.origin)
        .search;
      const reactDOMPath = `/node_modules/.vite/deps/react-dom_client.js${dependencyVersion}`;
      const i18nPath = dependencyPath(
        /from "([^"]*\/src\/app\/i18n\.tsx(?:\?t=[^"]+)?)"/,
        "TutorHub i18n",
      );
      const [ReactImport, ReactDOMImport, classroom, i18n] = await Promise.all([
        dynamicImport<BrowserCommonJSModule<BrowserReactModule>>(reactPath),
        dynamicImport<BrowserCommonJSModule<BrowserReactDOMModule>>(
          reactDOMPath,
        ),
        dynamicImport<BrowserClassroomModule>(roomModulePath),
        dynamicImport<BrowserI18nModule>(i18nPath),
        dynamicImport<unknown>("/src/styles.css"),
      ]);
      const React = ReactImport.default;
      const ReactDOM = ReactDOMImport.default;
      const rootElement = document.getElementById("p405-provider-root");
      const probe = window.__p405PublisherProbe;
      if (!rootElement || !probe) {
        throw new Error("P4-05 publisher fixture was not initialized.");
      }
      const root = ReactDOM.createRoot(rootElement);
      root.render(
        React.createElement(
          i18n.I18nProvider,
          { initialLanguage: "en" },
          React.createElement(
            "main",
            {
              className: "media-p403-room media-p405-room",
              "data-theme": "dark",
            },
            React.createElement(classroom.ClassroomLiveKitRoom, {
              choices: {
                audioDeviceId: "",
                audioEnabled: true,
                audioMode: "speech",
                speakerDeviceId: "",
                videoDeviceId: "",
                videoEnabled: true,
              },
              connectionStatus: "connected",
              credential: {
                access_token: token,
                can_publish_camera_microphone: true,
                can_share_screen: true,
                can_subscribe: true,
                expires_at: new Date(Date.now() + 300_000).toISOString(),
                instance_role: "host",
                join_attempt_id: "provider-fixture-attempt",
                participant_session_id: "provider-fixture-session",
                room_instance_id: "provider-fixture-instance",
                server_url: providerURL,
              },
              onConnected: () => {
                probe.connected = true;
              },
              onDisconnected: () => {
                probe.disconnected = true;
              },
              onLeave: () => {
                probe.leaveCompleted = true;
              },
              onProviderError: () => {
                probe.providerFailed = true;
              },
            }),
          ),
        ),
      );
      window.__p405ProviderCleanup = () => root.unmount();
    },
    { providerURL: serverURL, token: accessToken },
  );
  await expect(page.locator(".media-p405-shell")).toBeVisible({
    timeout: 30_000,
  });
}

async function mountClassroomSubscriber(
  page: Page,
  serverURL: string,
  accessToken: string,
): Promise<void> {
  await page.evaluate(
    async ({ providerURL, token }) => {
      const dynamicImport = new Function(
        "specifier",
        "return import(specifier)",
      ) as <Module>(specifier: string) => Promise<Module>;
      const roomModulePath = "/src/features/media/ClassroomLiveKitRoom.tsx";
      const transformedRoom = await fetch(roomModulePath).then((response) => {
        if (!response.ok) {
          throw new Error("Could not load the P4-05 subscriber fixture.");
        }
        return response.text();
      });
      const liveKitPath = transformedRoom.match(
        /from "([^"]*\/livekit-client\.js\?v=[^"]+)"/,
      )?.[1];
      if (!liveKitPath) {
        throw new Error("Could not resolve the LiveKit fixture module.");
      }
      const dependencyPath = (pattern: RegExp, label: string) => {
        const path = transformedRoom.match(pattern)?.[1];
        if (!path)
          throw new Error(`Could not resolve the ${label} fixture module.`);
        return path;
      };
      const reactPath = dependencyPath(
        /from "([^"]*\/react\.js\?v=[^"]+)"/,
        "React",
      );
      const dependencyVersion = new URL(reactPath, window.location.origin)
        .search;
      const reactDOMPath = `/node_modules/.vite/deps/react-dom_client.js${dependencyVersion}`;
      const i18nPath = dependencyPath(
        /from "([^"]*\/src\/app\/i18n\.tsx(?:\?t=[^"]+)?)"/,
        "TutorHub i18n",
      );
      const [ReactImport, ReactDOMImport, classroom, i18n, livekit] =
        await Promise.all([
          dynamicImport<BrowserCommonJSModule<BrowserReactModule>>(reactPath),
          dynamicImport<BrowserCommonJSModule<BrowserReactDOMModule>>(
            reactDOMPath,
          ),
          dynamicImport<BrowserClassroomModule>(roomModulePath),
          dynamicImport<BrowserI18nModule>(i18nPath),
          dynamicImport<BrowserLiveKitModule>(liveKitPath),
          dynamicImport<unknown>("/src/styles.css"),
        ]);
      const React = ReactImport.default;
      const ReactDOM = ReactDOMImport.default;
      const rootElement = document.getElementById("p405-provider-root");
      const probe: SubscriberProbe = {
        connected: false,
        dataPackets: 0,
        failed: false,
        publishedSources: [],
        remoteParticipantCount: 0,
        remotePublicationCount: 0,
        remoteParticipantDisconnects: 0,
        requestedSources: [],
        subscribedSources: [],
      };
      if (!rootElement) {
        throw new Error("P4-05 subscriber fixture was not initialized.");
      }
      window.__p405SubscriberProbe = probe;
      const originalConnect = livekit.Room.prototype.connect;
      const originalSetSubscribed =
        livekit.RemoteTrackPublication.prototype.setSubscribed;
      const instrumentedSetSubscribed: BrowserRemoteTrackPublication["setSubscribed"] =
        function (this: BrowserRemoteTrackPublication, subscribed) {
          if (
            subscribed &&
            this.source &&
            !probe.requestedSources.includes(this.source)
          ) {
            probe.requestedSources.push(this.source);
          }
          originalSetSubscribed.call(this, subscribed);
        };
      livekit.RemoteTrackPublication.prototype.setSubscribed =
        instrumentedSetSubscribed;
      const instrumentedConnect: BrowserRawRoom["connect"] = async function (
        this: BrowserRawRoom,
        roomServerURL,
        roomToken,
        options,
      ) {
        const refreshRoomState = () => {
          probe.remoteParticipantCount = this.remoteParticipants.size;
          probe.remotePublicationCount = 0;
          for (const participant of this.remoteParticipants.values()) {
            for (const publication of participant.trackPublications.values()) {
              probe.remotePublicationCount += 1;
              recordPublication(publication);
            }
          }
        };
        window.__p405SubscriberRefresh = refreshRoomState;
        const recordPublication = (
          publication: BrowserRemoteTrackPublication | undefined,
        ) => {
          if (
            publication?.source &&
            !probe.publishedSources.includes(publication.source)
          ) {
            probe.publishedSources.push(publication.source);
          }
        };
        const recordParticipant = (participant: {
          trackPublications: Map<string, BrowserRemoteTrackPublication>;
        }) => {
          for (const publication of participant.trackPublications.values()) {
            recordPublication(publication);
          }
        };
        this.on(livekit.RoomEvent.Connected, () => {
          probe.connected = true;
        });
        this.on(livekit.RoomEvent.DataReceived, () => {
          probe.dataPackets += 1;
        });
        this.on(livekit.RoomEvent.ParticipantConnected, (...arguments_) => {
          const participant = arguments_[0] as
            | {
                trackPublications: Map<string, BrowserRemoteTrackPublication>;
              }
            | undefined;
          if (participant) recordParticipant(participant);
        });
        this.on(livekit.RoomEvent.ParticipantDisconnected, () => {
          probe.remoteParticipantDisconnects += 1;
        });
        this.on(livekit.RoomEvent.TrackPublished, (...arguments_) => {
          const publication = arguments_[0] as
            BrowserRemoteTrackPublication | undefined;
          recordPublication(publication);
        });
        this.on(livekit.RoomEvent.TrackSubscribed, (...arguments_) => {
          const publication = arguments_[1] as
            BrowserRemoteTrackPublication | undefined;
          if (
            publication?.source &&
            !probe.subscribedSources.includes(publication.source)
          ) {
            probe.subscribedSources.push(publication.source);
          }
        });
        await originalConnect.call(this, roomServerURL, roomToken, options);
        refreshRoomState();
      };
      livekit.Room.prototype.connect = instrumentedConnect;

      const root = ReactDOM.createRoot(rootElement);
      root.render(
        React.createElement(
          i18n.I18nProvider,
          { initialLanguage: "en" },
          React.createElement(
            "main",
            {
              className: "media-p403-room media-p405-room",
              "data-theme": "dark",
            },
            React.createElement(classroom.ClassroomLiveKitRoom, {
              choices: {
                audioDeviceId: "",
                audioEnabled: false,
                audioMode: "speech",
                speakerDeviceId: "",
                videoDeviceId: "",
                videoEnabled: false,
              },
              connectionStatus: "connected",
              credential: {
                access_token: token,
                can_publish_camera_microphone: false,
                can_share_screen: false,
                can_subscribe: true,
                expires_at: new Date(Date.now() + 300_000).toISOString(),
                instance_role: "participant",
                join_attempt_id: "provider-fixture-subscriber-attempt",
                participant_session_id: "provider-fixture-subscriber-session",
                room_instance_id: "provider-fixture-instance",
                server_url: providerURL,
              },
              onConnected: () => {
                probe.connected = true;
              },
              onDisconnected: () => {
                probe.failed = true;
              },
              onLeave: () => undefined,
              onProviderError: () => {
                probe.failed = true;
              },
            }),
          ),
        ),
      );
      window.__p405ProviderCleanup = async () => {
        root.unmount();
        if (livekit.Room.prototype.connect === instrumentedConnect) {
          livekit.Room.prototype.connect = originalConnect;
        }
        if (
          livekit.RemoteTrackPublication.prototype.setSubscribed ===
          instrumentedSetSubscribed
        ) {
          livekit.RemoteTrackPublication.prototype.setSubscribed =
            originalSetSubscribed;
        }
        await Promise.resolve();
        window.__p405SubscriberRefresh = undefined;
      };
    },
    { providerURL: serverURL, token: accessToken },
  );
  await expect(page.locator(".media-p405-shell")).toBeVisible({
    timeout: 30_000,
  });
}

async function isLoopbackFixture(page: Page): Promise<boolean> {
  return page.evaluate(
    () =>
      window.location.hostname === "127.0.0.1" ||
      window.location.hostname === "localhost",
  );
}

async function readPublisherConnected(page: Page): Promise<boolean> {
  return page.evaluate(
    () =>
      window.__p405PublisherProbe?.connected === true &&
      window.__p405PublisherProbe.providerFailed === false,
  );
}

async function readSubscriberConnected(page: Page): Promise<boolean> {
  return page.evaluate(
    () =>
      window.__p405SubscriberProbe?.connected === true &&
      window.__p405SubscriberProbe.failed === false,
  );
}

async function readDisplayMediaCalls(page: Page): Promise<number> {
  return page.evaluate(
    () => window.__p405PublisherProbe?.displayMediaCalls ?? -1,
  );
}

async function readPublisherUserMediaCalls(page: Page): Promise<number> {
  return page.evaluate(() => window.__p405PublisherProbe?.userMediaCalls ?? -1);
}

async function readPublisherLeaveCompleted(page: Page): Promise<boolean> {
  return page.evaluate(
    () => window.__p405PublisherProbe?.leaveCompleted === true,
  );
}

async function publisherCapturedTracksEnded(page: Page): Promise<boolean> {
  return page.evaluate(() => {
    const tracks = window.__p405PublisherProbe?.capturedTracks ?? [];
    return (
      tracks.length >= 3 &&
      tracks.every((track) => track.readyState === "ended")
    );
  });
}

async function containsCredentialMaterial(
  page: Page,
  candidates: readonly string[],
): Promise<boolean> {
  return page.evaluate((values) => {
    const storageText = (storage: Storage) => {
      const entries: string[] = [];
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index) ?? "";
        entries.push(key, storage.getItem(key) ?? "");
      }
      return entries.join("\u0000");
    };
    const exposedText = [
      window.location.href,
      document.documentElement.outerHTML,
      storageText(window.localStorage),
      storageText(window.sessionStorage),
    ].join("\u0000");
    return values.some((value) => exposedText.includes(value));
  }, candidates);
}

async function safelyCleanupFixture(page: Page): Promise<void> {
  if (page.isClosed()) return;
  await page.evaluate(async () => {
    try {
      await window.__p405ProviderCleanup?.();
    } catch {
      // Browser/context teardown is the final cleanup fallback.
    }
  });
}
