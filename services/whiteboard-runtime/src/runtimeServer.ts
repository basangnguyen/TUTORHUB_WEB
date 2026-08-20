import { timingSafeEqual } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { Server, type Connection } from "@hocuspocus/server";
import * as Y from "yjs";
import type { RuntimeConfig } from "./config.js";
import type {
  CheckpointStore,
  CollaborationCapability,
  CollaborationScope,
  ControlPlane,
  PortableSnapshotStore,
  RuntimeMode,
  SafeLogger,
} from "./contracts.js";
import { RuntimeTelemetry } from "./telemetry.js";

type RuntimeState =
  "draining" | "ready" | "starting" | "stopped" | "unavailable";

export interface RuntimeDependencies {
  checkpoints: CheckpointStore;
  controlPlane: ControlPlane;
  logger: SafeLogger;
  snapshots: PortableSnapshotStore;
}

export interface CollaborationRuntime {
  address(): { address: string; port: number };
  drain(): Promise<void>;
  readiness(): { reason: string; ready: boolean };
  start(): Promise<void>;
}

interface ActiveConnection {
  capability: CollaborationCapability;
  connection?: Connection<CollaborationScope>;
  release: () => void;
  scope: CollaborationScope;
}

class RuntimeSocketError extends Error {
  readonly code = 1008;

  constructor(readonly reason: string) {
    super(reason);
    this.name = "RuntimeSocketError";
  }
}

export function createCollaborationRuntime(
  config: RuntimeConfig,
  dependencies: RuntimeDependencies,
): CollaborationRuntime {
  const telemetry = new RuntimeTelemetry(config.buildId);
  const connections = new Map<string, ActiveConnection>();
  const documentScopes = new Map<string, CollaborationScope>();
  const dirtyDocuments = new Set<string>();
  const traffic = new Map<string, { count: number; startedAt: number }>();
  const quota = new ConnectionQuota(config);
  let authorityMode: RuntimeMode = "off";
  let dependencyTimer: NodeJS.Timeout | undefined;
  let authorityTimer: NodeJS.Timeout | undefined;
  let state: RuntimeState = "starting";
  let drainPromise: Promise<void> | undefined;

  const server = new Server<CollaborationScope>({
    address: config.address,
    debounce: 2_000,
    maxDebounce: 10_000,
    maxPendingDocuments: 16,
    maxUnauthenticatedQueueMessages: 16,
    maxUnauthenticatedQueueSize: 16 * 1024,
    port: config.port,
    quiet: true,
    stopOnSignals: false,
    timeout: 30_000,
    unloadImmediately: false,
    websocketOptions: { maxPayload: config.maxFrameBytes },
    async onConnect() {
      if (state !== "ready" || authorityMode === "off") {
        telemetry.connection("view", "rejected");
        throw new RuntimeSocketError("runtime_not_ready");
      }
    },
    async onAuthenticate({
      connectionConfig,
      documentName,
      requestHeaders,
      socketId,
      token,
    }) {
      try {
        const origin = requestHeaders.get("origin") ?? "";
        if (
          !config.allowedOrigins.has(origin) ||
          token.length < 20 ||
          token.length > 1_024
        ) {
          throw new RuntimeSocketError("grant_denied");
        }
        const currentAuthority = await dependencies.controlPlane.probe();
        authorityMode = currentAuthority.mode;
        if (authorityMode === "off")
          throw new RuntimeSocketError("runtime_off");
        const scope = await dependencies.controlPlane.exchangeGrant({
          documentName,
          grant: token,
          origin,
        });
        if (authorityMode === "read_only" && scope.capability !== "view") {
          throw new RuntimeSocketError("write_disabled");
        }
        assertDocumentScope(documentScopes.get(documentName), scope);
        const release = quota.acquire(scope);
        connectionConfig.readOnly = scope.capability === "view";
        connections.set(socketId, {
          capability: scope.capability,
          release,
          scope,
        });
        telemetry.connection(scope.capability, "accepted");
        return scope;
      } catch (error) {
        telemetry.connection("view", "rejected");
        dependencies.logger.event("connection_denied", "denied");
        if (error instanceof RuntimeSocketError) throw error;
        throw new RuntimeSocketError("grant_denied");
      }
    },
    async connected({ connection, socketId }) {
      const active = connections.get(socketId);
      if (active) active.connection = connection;
      dependencies.logger.event("connection_ok", "ok");
    },
    async beforeHandleMessage({ socketId, update }) {
      if (state !== "ready" || update.byteLength > config.maxFrameBytes) {
        throw new RuntimeSocketError("update_denied");
      }
      const now = Date.now();
      const previous = traffic.get(socketId);
      const window =
        !previous || now - previous.startedAt >= 1_000
          ? { count: 0, startedAt: now }
          : previous;
      window.count += 1;
      traffic.set(socketId, window);
      if (window.count > config.maxMessagesPerSecond) {
        throw new RuntimeSocketError("update_rate_limited");
      }
    },
    async beforeSync({ context, type }) {
      if (type === 0) return;
      if (
        state !== "ready" ||
        authorityMode !== "enabled" ||
        context.capability === "view"
      ) {
        throw new RuntimeSocketError("write_disabled");
      }
    },
    async onLoadDocument({ context, document, documentName }) {
      try {
        assertDocumentScope(documentScopes.get(documentName), context);
        documentScopes.set(documentName, context);
        const checkpoint = await dependencies.checkpoints.load(context);
        if (checkpoint) {
          Y.applyUpdate(document, checkpoint.state);
          telemetry.checkpoint("loaded");
        }
        telemetry.setDocuments(server.hocuspocus.getDocumentsCount());
        return document;
      } catch {
        state = "unavailable";
        telemetry.checkpoint("failed");
        dependencies.logger.event("checkpoint_failed", "failed");
        throw new RuntimeSocketError("checkpoint_unavailable");
      }
    },
    async onChange({ documentName }) {
      dirtyDocuments.add(documentName);
      telemetry.setDirtyDocuments(dirtyDocuments.size);
    },
    async onStoreDocument({ document, documentName, lastContext }) {
      try {
        await dependencies.checkpoints.store(
          lastContext,
          Y.encodeStateAsUpdate(document),
        );
        dirtyDocuments.delete(documentName);
        telemetry.setDirtyDocuments(dirtyDocuments.size);
        telemetry.checkpoint("stored");
        dependencies.logger.event("checkpoint_ok", "ok");
      } catch {
        state = "unavailable";
        telemetry.checkpoint("failed");
        dependencies.logger.event("checkpoint_failed", "failed");
        throw new RuntimeSocketError("checkpoint_unavailable");
      }
    },
    async afterUnloadDocument() {
      telemetry.setDocuments(server.hocuspocus.getDocumentsCount());
    },
    async onDisconnect({ clientsCount, documentName, socketId }) {
      const active = connections.get(socketId);
      if (active) {
        active.release();
        telemetry.connection(active.capability, "closed");
      }
      connections.delete(socketId);
      traffic.delete(socketId);
      if (clientsCount === 0) documentScopes.delete(documentName);
      telemetry.setDocuments(server.hocuspocus.getDocumentsCount());
    },
  });

  server.httpServer.removeAllListeners("request");
  server.httpServer.on("request", (request, response) => {
    void handleHttpRequest(request, response, {
      buildId: config.buildId,
      metricsToken: config.metricsToken,
      readiness: () => readiness(),
      telemetry,
    });
  });

  const readiness = (): { reason: string; ready: boolean } => {
    if (state === "draining") return { ready: false, reason: "draining" };
    if (state !== "ready")
      return { ready: false, reason: "dependency_unavailable" };
    if (authorityMode === "off") return { ready: false, reason: "runtime_off" };
    return { ready: true, reason: "ready" };
  };

  const refreshDependencies = async (): Promise<void> => {
    if (state === "draining" || state === "stopped") return;
    const [persistence, control] = await Promise.allSettled([
      dependencies.checkpoints.probe(),
      dependencies.controlPlane.probe(),
    ]);
    telemetry.dependency("persistence", persistence.status === "fulfilled");
    telemetry.dependency("control_plane", control.status === "fulfilled");
    if (persistence.status === "fulfilled" && control.status === "fulfilled") {
      authorityMode = control.value.mode;
      state = authorityMode === "off" ? "unavailable" : "ready";
      dependencies.logger.event("dependency_up", "ok");
    } else {
      state = "unavailable";
      dependencies.logger.event("dependency_down", "failed");
    }
  };

  const refreshAuthority = async (): Promise<void> => {
    if (state === "draining" || state === "stopped") return;
    try {
      const next = await dependencies.controlPlane.probe();
      telemetry.dependency("control_plane", true);
      const changed = next.mode !== authorityMode;
      authorityMode = next.mode;
      if (changed && authorityMode !== "enabled")
        closeWriters(connections, authorityMode);
      if (authorityMode === "off") state = "unavailable";
    } catch {
      telemetry.dependency("control_plane", false);
      state = "unavailable";
      closeWriters(connections, "off");
    }
  };

  const drain = (): Promise<void> => {
    drainPromise ??= (async () => {
      const startedAt = Date.now();
      state = "draining";
      telemetry.setDraining(true);
      dependencies.logger.event("drain_started", "started");
      if (dependencyTimer) clearInterval(dependencyTimer);
      if (authorityTimer) clearInterval(authorityTimer);
      try {
        server.hocuspocus.flushPendingStores();
        await withDeadline(server.destroy(), config.drainTimeoutMs);
        if (dirtyDocuments.size !== 0)
          throw new Error("dirty_documents_remain");
        await dependencies.checkpoints.close();
        state = "stopped";
        telemetry.setDocuments(0);
        telemetry.setDraining(false);
        dependencies.logger.event(
          "drain_complete",
          "ok",
          Date.now() - startedAt,
        );
      } catch {
        state = "stopped";
        dependencies.logger.event(
          "drain_failed",
          "failed",
          Date.now() - startedAt,
        );
        throw new Error("runtime_drain_failed");
      }
    })();
    return drainPromise;
  };

  return {
    address: () => ({
      address: server.address.address,
      port: server.address.port,
    }),
    drain,
    readiness,
    start: async () => {
      await server.listen();
      await refreshDependencies();
      void dependencies.snapshots
        .probe()
        .then(() => telemetry.dependency("snapshot", true))
        .catch(() => telemetry.dependency("snapshot", false));
      dependencyTimer = setInterval(() => void refreshDependencies(), 5_000);
      dependencyTimer.unref();
      authorityTimer = setInterval(() => void refreshAuthority(), 750);
      authorityTimer.unref();
      dependencies.logger.event("runtime_started", "ok");
    },
  };
}

class ConnectionQuota {
  private active = 0;
  private readonly actorCounts = new Map<string, number>();
  private readonly documentCounts = new Map<string, number>();

  constructor(private readonly config: RuntimeConfig) {}

  acquire(scope: CollaborationScope): () => void {
    const actorKey = `${scope.tenantId}\u0000${scope.actorId}`;
    const documentKey = `${scope.tenantId}\u0000${scope.documentId}\u0000${scope.generation}`;
    const actorCount = this.actorCounts.get(actorKey) ?? 0;
    const documentCount = this.documentCounts.get(documentKey) ?? 0;
    if (
      this.active >= this.config.maxConnections ||
      actorCount >= this.config.maxConnectionsPerActor ||
      documentCount >= this.config.maxConnectionsPerDocument
    ) {
      throw new RuntimeSocketError("connection_quota_exceeded");
    }
    this.active += 1;
    this.actorCounts.set(actorKey, actorCount + 1);
    this.documentCounts.set(documentKey, documentCount + 1);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      this.active = Math.max(0, this.active - 1);
      decrement(this.actorCounts, actorKey);
      decrement(this.documentCounts, documentKey);
    };
  }
}

function assertDocumentScope(
  current: CollaborationScope | undefined,
  candidate: CollaborationScope,
): void {
  if (
    current &&
    (current.tenantId !== candidate.tenantId ||
      current.documentId !== candidate.documentId ||
      current.generation !== candidate.generation ||
      current.providerDocumentName !== candidate.providerDocumentName ||
      current.writerFence !== candidate.writerFence)
  ) {
    throw new RuntimeSocketError("document_scope_mismatch");
  }
}

function closeWriters(
  connections: Map<string, ActiveConnection>,
  mode: RuntimeMode,
): void {
  for (const active of connections.values()) {
    if (mode === "off" || active.capability !== "view") {
      active.connection?.close({ code: 4403, reason: "runtime_mode_changed" });
    }
  }
}

async function handleHttpRequest(
  request: IncomingMessage,
  response: ServerResponse,
  options: {
    buildId: string;
    metricsToken: string;
    readiness: () => { reason: string; ready: boolean };
    telemetry: RuntimeTelemetry;
  },
): Promise<void> {
  const path = new URL(request.url ?? "/", "http://runtime.invalid").pathname;
  if (request.method !== "GET") {
    writeJson(response, 405, { status: "method_not_allowed" });
    return;
  }
  if (path === "/livez") {
    writeJson(response, 200, { build: options.buildId, status: "ok" });
    return;
  }
  if (path === "/readyz") {
    const readiness = options.readiness();
    writeJson(response, readiness.ready ? 200 : 503, {
      reason: readiness.reason,
      status: readiness.ready ? "ready" : "not_ready",
    });
    return;
  }
  if (path === "/metrics") {
    if (!authorized(request.headers.authorization, options.metricsToken)) {
      writeJson(response, 401, { status: "unauthorized" });
      return;
    }
    response.writeHead(200, {
      "cache-control": "no-store",
      "content-type": "text/plain; version=0.0.4; charset=utf-8",
    });
    response.end(options.telemetry.render());
    return;
  }
  writeJson(response, 404, { status: "not_found" });
}

function authorized(header: string | undefined, expected: string): boolean {
  const prefix = "Bearer ";
  if (!header?.startsWith(prefix)) return false;
  const received = Buffer.from(header.slice(prefix.length));
  const wanted = Buffer.from(expected);
  return (
    received.byteLength === wanted.byteLength &&
    timingSafeEqual(received, wanted)
  );
}

function writeJson(
  response: ServerResponse,
  status: number,
  payload: Record<string, string>,
): void {
  response.writeHead(status, {
    "cache-control": "no-store",
    "content-type": "application/json; charset=utf-8",
    "x-content-type-options": "nosniff",
  });
  response.end(JSON.stringify(payload));
}

function decrement(values: Map<string, number>, key: string): void {
  const next = (values.get(key) ?? 1) - 1;
  if (next <= 0) values.delete(key);
  else values.set(key, next);
}

async function withDeadline<T>(
  promise: Promise<T>,
  timeoutMs: number,
): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(
          () => reject(new Error("deadline_exceeded")),
          timeoutMs,
        );
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}
