import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import * as Y from "yjs";
import {
  type CollaborationCapability,
  type CollaborationGrantRequest,
  type CollaborationGrantResponse,
} from "./authorizationContract";
import {
  CanonicalExcalidrawAuthority,
  excalidrawSceneToCanonical,
} from "./canonicalAuthority";
import type { BrowserCanonicalSession } from "./browserCanonicalSession";

export interface BrowserAuthorizedSession extends BrowserCanonicalSession {
  capability: CollaborationCapability;
  generation: number;
}

export async function createBrowserAuthorizedSession({
  actorId,
  bootstrap,
  controlUrl,
  documentId,
  providerUrl,
  requestedCapability,
  sessionId,
  tenantId,
}: {
  actorId: string;
  bootstrap: boolean;
  controlUrl: string;
  documentId: string;
  providerUrl: string;
  requestedCapability: CollaborationCapability;
  sessionId: string;
  tenantId: string;
}): Promise<BrowserAuthorizedSession> {
  assertLocalEndpoint(controlUrl, "http:");
  assertLocalEndpoint(providerUrl, "ws:");
  const grantRequest: CollaborationGrantRequest = {
    actorId,
    documentId,
    requestedCapability,
    sessionId,
    tenantId,
  };
  const response = await fetch(`${controlUrl}/gate-c/grants`, {
    body: JSON.stringify(grantRequest),
    cache: "no-store",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    method: "POST",
    referrerPolicy: "no-referrer",
  });
  if (!response.ok) {
    throw new Error(await readBoundedError(response));
  }
  if (
    response.headers.get("cache-control") !== "no-store" ||
    response.headers.get("referrer-policy") !== "no-referrer"
  ) {
    throw new Error("gate_c_private_response_headers_missing");
  }
  const grant = validateGrantResponse(await response.json());

  const document = new Y.Doc();
  const socket = new HocuspocusProviderWebsocket({
    autoConnect: false,
    delay: 25,
    factor: 1,
    jitter: false,
    maxAttempts: 1,
    maxDelay: 25,
    minDelay: 25,
    url: providerUrl,
  });
  const provider = new HocuspocusProvider({
    document,
    name: grant.providerDocumentName,
    sessionAwareness: true,
    token: grant.grant,
    websocketProvider: socket,
  });
  const authority = new CanonicalExcalidrawAuthority(
    document,
    { documentId, generation: grant.generation, tenantId },
    actorId,
  );

  provider.attach();
  await socket.connect();
  await waitUntil(
    () => provider.synced && provider.isAuthenticated,
    "gate_c_provider_sync_timeout",
  );
  if (bootstrap && grant.capability !== "view" && !authority.isInitialized()) {
    authority.initialize(
      excalidrawSceneToCanonical({
        appState: { viewBackgroundColor: "#f8fafc" },
        elements: [createRectangle("initial-shape", 100, "#a5d8ff")],
        files: {},
        page: { id: "page-1", name: "Bai 1" },
      }),
    );
  }
  await waitUntil(
    () => authority.isInitialized(),
    "gate_c_authority_bootstrap_timeout",
  );

  return {
    actorId,
    authority,
    capability: grant.capability,
    destroy: () => {
      authority.destroy();
      provider.destroy();
      socket.destroy();
      document.destroy();
    },
    generation: grant.generation,
  };
}

function validateGrantResponse(value: unknown): CollaborationGrantResponse {
  if (typeof value !== "object" || value === null) {
    throw new Error("gate_c_grant_response_invalid");
  }
  const candidate = value as Partial<CollaborationGrantResponse>;
  if (
    (candidate.capability !== "view" &&
      candidate.capability !== "edit" &&
      candidate.capability !== "present") ||
    typeof candidate.grant !== "string" ||
    candidate.grant.length < 32 ||
    typeof candidate.providerDocumentName !== "string" ||
    candidate.providerDocumentName.length < 8 ||
    !Number.isSafeInteger(candidate.generation) ||
    (candidate.generation ?? 0) < 1 ||
    !Number.isSafeInteger(candidate.expiresInSeconds) ||
    (candidate.expiresInSeconds ?? 0) < 1 ||
    (candidate.expiresInSeconds ?? 0) > 60
  ) {
    throw new Error("gate_c_grant_response_invalid");
  }
  return candidate as CollaborationGrantResponse;
}

async function readBoundedError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: unknown };
    return typeof body.error === "string"
      ? body.error.slice(0, 96)
      : "gate_c_grant_denied";
  } catch {
    return "gate_c_grant_denied";
  }
}

function assertLocalEndpoint(value: string, protocol: "http:" | "ws:"): void {
  const parsed = new URL(value);
  if (parsed.protocol !== protocol || parsed.hostname !== "127.0.0.1") {
    throw new Error("gate_c_endpoint_denied");
  }
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
    if (predicate()) return;
    await new Promise((resolve) => window.setTimeout(resolve, 20));
  }
  throw new Error(errorCode);
}
