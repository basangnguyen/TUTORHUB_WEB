import {
  HocuspocusProvider,
  type AuthorizedScope,
  HocuspocusProviderWebsocket,
  WebSocketStatus,
} from "@hocuspocus/provider";
import { Server } from "@hocuspocus/server";
import { WebSocket as NodeWebSocket } from "ws";
import * as Y from "yjs";

export const NETWORK_SPIKE_LIMITS = {
  maxInboundFrameBytes: 32 * 1024,
  maxTransportPayloadBytes: 64 * 1024,
  maxUnauthenticatedQueueBytes: 16 * 1024,
  maxUnauthenticatedQueueMessages: 32,
  maxPendingDocuments: 2,
} as const;

export type SpikeRole = "editor" | "viewer";

interface SpikeContext {
  role: SpikeRole;
  tenantId: string;
}

export interface SpikeRejections {
  authentication: number;
  frameBudget: number;
  tenant: number;
}

export interface SpikeServer {
  destroy: () => Promise<void>;
  rejections: SpikeRejections;
  server: Server<SpikeContext>;
  url: string;
}

export interface SpikeClient {
  authenticationFailures: string[];
  destroy: () => void;
  disconnect: () => Promise<void>;
  document: Y.Doc;
  provider: HocuspocusProvider;
  reconnect: () => Promise<void>;
  socket: HocuspocusProviderWebsocket;
}

class FrameBudgetError extends Error {
  code = 1008;
  reason = "inbound_frame_budget_exceeded";
}

function parseTestCredential(token: string): SpikeContext | null {
  if (token === "fixture-tenant-a-editor") {
    return { role: "editor", tenantId: "tenant-a" };
  }
  if (token === "fixture-tenant-a-viewer") {
    return { role: "viewer", tenantId: "tenant-a" };
  }
  if (token === "fixture-tenant-b-editor") {
    return { role: "editor", tenantId: "tenant-b" };
  }
  return null;
}

export async function startSpikeServer(): Promise<SpikeServer> {
  const rejections: SpikeRejections = {
    authentication: 0,
    frameBudget: 0,
    tenant: 0,
  };

  const server = new Server<SpikeContext>({
    address: "127.0.0.1",
    maxPendingDocuments: NETWORK_SPIKE_LIMITS.maxPendingDocuments,
    maxUnauthenticatedQueueMessages:
      NETWORK_SPIKE_LIMITS.maxUnauthenticatedQueueMessages,
    maxUnauthenticatedQueueSize:
      NETWORK_SPIKE_LIMITS.maxUnauthenticatedQueueBytes,
    port: 0,
    quiet: true,
    stopOnSignals: false,
    websocketOptions: {
      maxPayload: NETWORK_SPIKE_LIMITS.maxTransportPayloadBytes,
    },
    async onAuthenticate({ connectionConfig, documentName, token }) {
      const context = parseTestCredential(token);
      if (context === null) {
        rejections.authentication += 1;
        throw new Error("authentication_denied");
      }

      if (!documentName.startsWith(`${context.tenantId}/`)) {
        rejections.tenant += 1;
        throw new Error("tenant_scope_denied");
      }

      if (context.role === "viewer") {
        connectionConfig.readOnly = true;
      }

      return context;
    },
    async beforeHandleMessage({ update }) {
      if (update.byteLength > NETWORK_SPIKE_LIMITS.maxInboundFrameBytes) {
        rejections.frameBudget += 1;
        throw new FrameBudgetError();
      }
    },
  });

  await server.listen();

  return {
    destroy: () => server.destroy(),
    rejections,
    server,
    url: `ws://127.0.0.1:${server.address.port}`,
  };
}

export function createSpikeClient({
  documentName = "tenant-a/classroom-1/board-1",
  role = "editor",
  tenantId = "tenant-a",
  token,
  url,
}: {
  documentName?: string;
  role?: SpikeRole;
  tenantId?: "tenant-a" | "tenant-b";
  token?: string;
  url: string;
}): SpikeClient {
  const document = new Y.Doc();
  const authenticationFailures: string[] = [];
  const socket = new HocuspocusProviderWebsocket({
    WebSocketPolyfill: NodeWebSocket,
    autoConnect: false,
    delay: 25,
    factor: 1,
    jitter: false,
    maxAttempts: 2,
    maxDelay: 25,
    messageReconnectTimeout: 5_000,
    minDelay: 25,
    url,
  });
  const provider = new HocuspocusProvider({
    document,
    name: documentName,
    onAuthenticationFailed: ({ reason }) => {
      authenticationFailures.push(reason);
    },
    sessionAwareness: true,
    token: token ?? `fixture-${tenantId}-${role}`,
    websocketProvider: socket,
  });

  provider.attach();
  void socket.connect();

  return {
    authenticationFailures,
    destroy: () => {
      provider.destroy();
      socket.destroy();
      document.destroy();
    },
    disconnect: async () => {
      socket.disconnect();
      await waitUntil(
        () => socket.status === WebSocketStatus.Disconnected,
        "client did not disconnect",
      );
    },
    document,
    provider,
    reconnect: async () => {
      await socket.connect();
      await waitForSyncedProvider(provider);
    },
    socket,
  };
}

export async function waitForSyncedProvider(
  provider: HocuspocusProvider,
): Promise<void> {
  await waitUntil(
    () => provider.synced && provider.isAuthenticated,
    "provider did not authenticate and sync",
  );
}

export async function waitUntil(
  predicate: () => boolean,
  failureMessage: string,
  timeoutMs = 4_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  throw new Error(failureMessage);
}

export function getAuthorizedScope(
  provider: HocuspocusProvider,
): AuthorizedScope | undefined {
  return provider.authorizedScope;
}
