import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
  WebSocketStatus,
} from "@hocuspocus/provider";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  excalidrawSceneToCanonical,
} from "./canonicalAuthority.js";

export type CollaborationCapability = "view" | "edit" | "present";
export type CollaborationConnectionStatus =
  "connecting" | "connected" | "reconnecting" | "failed";
export type CollaborationTerminalReason =
  | "authority_changed"
  | "grant_rejected"
  | "reconnect_exhausted"
  | "recovery_required"
  | "runtime_unavailable";

export interface BrowserCollaborationGrant {
  capability: CollaborationCapability;
  credential: string;
  documentId: string;
  expiresAt: string;
  generation: number;
  providerUrl: string;
  revokeGeneration: number;
}

export interface BrowserCollaborationSession {
  authority: CanonicalExcalidrawAuthority;
  capability: CollaborationCapability;
  destroy(): void;
  generation: number;
}

export interface BrowserCollaborationSessionOptions {
  actorId: string;
  grant: BrowserCollaborationGrant;
  onStatus?: (status: CollaborationConnectionStatus) => void;
  onTerminal?: (reason: CollaborationTerminalReason) => void;
  tenantId: string;
}

export async function createBrowserCollaborationSession({
  actorId,
  grant,
  onStatus = () => undefined,
  onTerminal = () => undefined,
  tenantId,
}: BrowserCollaborationSessionOptions): Promise<BrowserCollaborationSession> {
  validateGrant(grant);
  const document = new Y.Doc();
  let authenticationFailed = false;
  let everConnected = false;
  let terminal = false;
  const notifyTerminal = (reason: CollaborationTerminalReason): void => {
    if (terminal) return;
    terminal = true;
    onStatus("failed");
    onTerminal(reason);
  };
  const socket = new HocuspocusProviderWebsocket({
    autoConnect: false,
    delay: 250,
    factor: 1.5,
    jitter: true,
    maxAttempts: 8,
    maxDelay: 4_000,
    minDelay: 250,
    onClose: ({ event }) => {
      const reason = classifyProviderClose(event.code, event.reason);
      if (reason) notifyTerminal(reason);
    },
    onStatus: ({ status }) => {
      if (terminal) return;
      const projected = projectProviderStatus(status, everConnected);
      if (projected === "connected") everConnected = true;
      onStatus(projected);
    },
    url: grant.providerUrl,
  });
  const provider = new HocuspocusProvider({
    document,
    name: deriveProviderDocumentName(grant.documentId),
    onAuthenticationFailed: () => {
      authenticationFailed = true;
      notifyTerminal("grant_rejected");
    },
    onSynced: ({ state }) => {
      if (state) {
        everConnected = true;
        onStatus("connected");
      }
    },
    sessionAwareness: true,
    token: grant.credential,
    websocketProvider: socket,
  });
  const authority = new CanonicalExcalidrawAuthority(
    document,
    {
      documentId: grant.documentId,
      generation: grant.generation,
      tenantId,
    },
    actorId,
  );

  onStatus("connecting");
  try {
    provider.attach();
    await socket.connect();
    await waitUntil(
      () => provider.synced && provider.isAuthenticated,
      () => authenticationFailed,
      "collaboration_provider_sync_timeout",
    );
    if (!authority.isInitialized() && grant.capability !== "view") {
      authority.initialize(
        excalidrawSceneToCanonical({
          appState: { viewBackgroundColor: "#f8fafc" },
          elements: [],
          files: {},
          page: { id: "page-1", name: "Bảng lớp học" },
        }),
      );
    }
    await waitUntil(
      () => authority.isInitialized(),
      () => authenticationFailed,
      "collaboration_authority_not_ready",
      grant.capability === "view" ? 10_000 : 8_000,
    );
  } catch (error) {
    onStatus("failed");
    authority.destroy();
    provider.destroy();
    socket.destroy();
    document.destroy();
    throw error;
  }

  return {
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

export function deriveProviderDocumentName(documentId: string): string {
  if (!UUID_PATTERN.test(documentId)) {
    throw new Error("collaboration_document_id_invalid");
  }
  return `wb_${documentId.replaceAll("-", "")}`;
}

export function projectProviderStatus(
  status: WebSocketStatus,
  everConnected: boolean,
): CollaborationConnectionStatus {
  if (status === WebSocketStatus.Connected) return "connected";
  if (status === WebSocketStatus.Connecting) {
    return everConnected ? "reconnecting" : "connecting";
  }
  return everConnected ? "reconnecting" : "connecting";
}

export function classifyProviderClose(
  code: number,
  reason: string,
): CollaborationTerminalReason | null {
  switch (reason) {
    case "authority_lost":
    case "document_scope_mismatch":
      return "authority_changed";
    case "checkpoint_unavailable":
      return "recovery_required";
    case "reconnect_storm_denied":
      return "reconnect_exhausted";
    case "runtime_mode_changed":
    case "runtime_not_ready":
    case "runtime_off":
    case "write_disabled":
      return "runtime_unavailable";
    case "grant_denied":
      return "grant_rejected";
    default:
      return code === 4403 || code === 1008 ? "grant_rejected" : null;
  }
}

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iu;

function validateGrant(grant: BrowserCollaborationGrant): void {
  deriveProviderDocumentName(grant.documentId);
  const providerUrl = new URL(grant.providerUrl);
  const expiresAt = Date.parse(grant.expiresAt);
  if (
    (providerUrl.protocol !== "wss:" && providerUrl.protocol !== "ws:") ||
    grant.credential.length < 32 ||
    !Number.isSafeInteger(grant.generation) ||
    grant.generation < 1 ||
    !Number.isSafeInteger(grant.revokeGeneration) ||
    grant.revokeGeneration < 1 ||
    !Number.isFinite(expiresAt) ||
    expiresAt <= Date.now()
  ) {
    throw new Error("collaboration_grant_invalid");
  }
}

async function waitUntil(
  predicate: () => boolean,
  failed: () => boolean,
  errorCode: string,
  timeoutMs = 8_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    if (failed()) throw new Error("collaboration_grant_rejected");
    await new Promise((resolve) => globalThis.setTimeout(resolve, 20));
  }
  throw new Error(errorCode);
}
