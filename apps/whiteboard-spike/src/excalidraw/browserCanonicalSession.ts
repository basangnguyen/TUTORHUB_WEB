import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  excalidrawSceneToCanonical,
} from "./canonicalAuthority";

export interface BrowserCanonicalSession {
  actorId: string;
  authority: CanonicalExcalidrawAuthority;
  destroy: () => void;
}

export async function createBrowserCanonicalSession({
  actorId,
  bootstrap,
  providerUrl,
}: {
  actorId: string;
  bootstrap: boolean;
  providerUrl: string;
}): Promise<BrowserCanonicalSession> {
  if (!providerUrl.startsWith("ws://127.0.0.1:")) {
    throw new Error("gate_b_provider_url_denied");
  }

  const document = new Y.Doc();
  const socket = new HocuspocusProviderWebsocket({
    autoConnect: false,
    delay: 25,
    factor: 1,
    jitter: false,
    maxAttempts: 4,
    maxDelay: 50,
    minDelay: 25,
    url: providerUrl,
  });
  const provider = new HocuspocusProvider({
    document,
    name: "tenant-a/gate-b/board-1",
    sessionAwareness: true,
    token: "fixture-tenant-a-editor",
    websocketProvider: socket,
  });
  const authority = new CanonicalExcalidrawAuthority(
    document,
    {
      documentId: "board-1",
      generation: 1,
      tenantId: "tenant-a",
    },
    actorId,
  );

  provider.attach();
  await socket.connect();
  await waitUntil(
    () => provider.synced && provider.isAuthenticated,
    "gate_b_provider_sync_timeout",
  );

  if (bootstrap && !authority.isInitialized()) {
    authority.initialize(
      excalidrawSceneToCanonical({
        appState: { viewBackgroundColor: "#f8fafc" },
        elements: [createRectangle("initial-shape", 100, "#a5d8ff")],
        files: {},
        page: { id: "page-1", name: "Bài 1" },
      }),
    );
  }
  await waitUntil(
    () => authority.isInitialized(),
    "gate_b_authority_bootstrap_timeout",
  );

  return {
    actorId,
    authority,
    destroy: () => {
      authority.destroy();
      provider.destroy();
      socket.destroy();
      document.destroy();
    },
  };
}

function createRectangle(id: string, x: number, backgroundColor: string) {
  return {
    angle: 0,
    backgroundColor,
    boundElements: null,
    fillStyle: "solid",
    frameId: null,
    groupIds: [],
    height: 100,
    id,
    index: null,
    isDeleted: false,
    link: null,
    locked: false,
    opacity: 100,
    roughness: 1,
    roundness: null,
    seed: x + 1,
    strokeColor: "#1c3f60",
    strokeStyle: "solid",
    strokeWidth: 2,
    type: "rectangle",
    updated: 1_786_000_000_000,
    version: 1,
    versionNonce: x + 2,
    width: 160,
    x,
    y: 120,
  };
}

async function waitUntil(
  predicate: () => boolean,
  errorCode: string,
  timeoutMs = 8_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 20));
  }
  throw new Error(errorCode);
}
