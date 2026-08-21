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
import type { ProviderAuthorityGuard } from "./providerAuthorityGuard.js";
import {
  inspectRawHocuspocusAwareness,
  RawAwarenessInspectionError,
  type RawHocuspocusAwarenessInspection,
} from "./rawHocuspocusAwareness.js";
import { installRawWebSocketIngressGate } from "./rawWebSocketIngressGate.js";
import { RuntimeDocumentBudget } from "./runtimeDocumentBudget.js";
import {
  RuntimeConnectionPolicy,
  RuntimeIngressPolicy,
  RuntimePolicyError,
  validateAwarenessEnvelope,
} from "./runtimePolicy.js";
import { RuntimeTelemetry, type RuntimePolicyReason } from "./telemetry.js";
import {
  RuntimeSessionRegistry,
  RuntimeSessionRegistryError,
  type RuntimeSessionReservation,
} from "./runtimeSessionRegistry.js";
import {
  RuntimeReadinessCoordinator,
  RuntimeReadinessError,
} from "./runtimeReadinessCoordinator.js";

export interface RuntimeDependencies {
  authorityGuard: ProviderAuthorityGuard;
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
  reservation: RuntimeSessionReservation;
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
  const dirtyDocuments = new Set<string>();
  const connectionPolicy = new RuntimeConnectionPolicy({
    maxConnections: config.maxConnections,
    maxConnectionsPerActor: config.maxConnectionsPerActor,
    maxConnectionsPerDocument: config.maxConnectionsPerDocument,
    maxConnectionsPerTenant: config.maxConnectionsPerTenant,
    maxReconnectAttempts: config.maxReconnectAttempts,
    reconnectWindowMs: 10_000,
  });
  const sessionRegistry = new RuntimeSessionRegistry(connectionPolicy);
  const documentBudget = new RuntimeDocumentBudget(
    config.maxDocumentBytes,
    config.maxUpdateBytes,
  );
  const awarenessIngressPolicy = new RuntimeIngressPolicy({
    maxBytesPerWindow:
      config.maxAwarenessBytes * config.maxAwarenessMessagesPerSecond,
    maxMessagesPerWindow: config.maxAwarenessMessagesPerSecond,
    windowMs: 1_000,
  });
  const readinessCoordinator = new RuntimeReadinessCoordinator(() =>
    dependencies.authorityGuard.assertHeld(),
  );
  const rawAwarenessInspections = new Map<
    string,
    Extract<RawHocuspocusAwarenessInspection, { kind: "awareness" }>
  >();
  const awarenessClientIds = new Map<string, number>();
  const awarenessClientOwners = new Map<string, Map<number, string>>();
  let dependencyTimer: NodeJS.Timeout | undefined;
  let authorityTimer: NodeJS.Timeout | undefined;
  let dependencyRefreshRunning = false;
  let authorityRefreshRunning = false;
  let drainPromise: Promise<void> | undefined;

  const requireAdmissionReady = (write = false): void => {
    try {
      readinessCoordinator.assertAdmissionReady({ write });
    } catch (error) {
      if (
        error instanceof RuntimeReadinessError &&
        error.code === "write_disabled"
      ) {
        throw new RuntimeSocketError("write_disabled");
      }
      throw new RuntimeSocketError("runtime_not_ready");
    }
  };

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
      try {
        requireAdmissionReady();
      } catch {
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
      let reservation: RuntimeSessionReservation | undefined;
      try {
        requireAdmissionReady();
        const origin = requestHeaders.get("origin") ?? "";
        if (
          !config.allowedOrigins.has(origin) ||
          token.length < 20 ||
          token.length > 1_024
        ) {
          throw new RuntimeSocketError("grant_denied");
        }
        const authorityOutcome = await readinessCoordinator.refreshAuthority(
          async () => {
            await dependencies.authorityGuard.probe();
            const current = await dependencies.controlPlane.probe();
            return { mode: current.mode, value: current };
          },
        );
        if (!authorityOutcome.applied) {
          throw new RuntimeSocketError("runtime_not_ready");
        }
        requireAdmissionReady();
        if (authorityOutcome.value.mode === "off")
          throw new RuntimeSocketError("runtime_off");
        const scope = await dependencies.controlPlane.exchangeGrant({
          documentName,
          grant: token,
          origin,
        });
        requireAdmissionReady();
        if (
          scope.origin !== origin ||
          scope.providerDocumentName !== documentName
        ) {
          throw new RuntimeSocketError("grant_denied");
        }
        if (
          authorityOutcome.value.mode === "read_only" &&
          scope.capability !== "view"
        ) {
          throw new RuntimeSocketError("write_disabled");
        }
        const key = connectionKey(socketId, documentName);
        reservation = sessionRegistry.reserve(key, scope);
        requireAdmissionReady();
        connectionConfig.readOnly = scope.capability === "view";
        connections.set(key, {
          capability: scope.capability,
          reservation,
          scope,
        });
        return scope;
      } catch (error) {
        reservation?.rollback("authentication");
        telemetry.connection("view", "rejected");
        dependencies.logger.event("connection_denied", "denied");
        if (error instanceof RuntimeSocketError) throw error;
        if (error instanceof RuntimePolicyError) {
          const denial = policyDenial(error);
          telemetry.policyRejected(denial.metric);
          throw new RuntimeSocketError(denial.socketReason);
        }
        if (error instanceof RuntimeSessionRegistryError) {
          throw new RuntimeSocketError(
            error.code === "document_scope_conflict"
              ? "document_scope_mismatch"
              : error.code === "duplicate_session_reservation"
                ? "duplicate_provider_session"
                : "grant_denied",
          );
        }
        throw new RuntimeSocketError("grant_denied");
      }
    },
    async connected({ connection, documentName, socketId }) {
      const key = connectionKey(socketId, documentName);
      const active = connections.get(key);
      try {
        if (!active) throw new RuntimeSocketError("grant_denied");
        requireAdmissionReady(active.capability !== "view");
        active.reservation.commit();
        active.connection = connection;
        rawIngressGate?.markAuthenticated(socketId);
        telemetry.connection(active.capability, "accepted");
        dependencies.logger.event("connection_ok", "ok");
      } catch (error) {
        active?.reservation.rollback("setup");
        connections.delete(key);
        telemetry.connection(active?.capability ?? "view", "rejected");
        dependencies.logger.event("connection_denied", "denied");
        throw error;
      }
    },
    async beforeHandleMessage({ documentName, socketId, update }) {
      const key = connectionKey(socketId, documentName);
      rawAwarenessInspections.delete(key);
      try {
        requireAdmissionReady();
        const inspection = inspectRawHocuspocusAwareness(update, {
          maxAwarenessBytes: config.maxAwarenessBytes,
          maxDepth: config.maxAwarenessDepth,
          maxFrameBytes: config.maxFrameBytes,
        });
        if (inspection.kind === "awareness") {
          awarenessIngressPolicy.consume(key, inspection.awarenessBytes);
          rawAwarenessInspections.set(key, inspection);
        }
      } catch (error) {
        if (error instanceof RuntimePolicyError) {
          const denial = policyDenial(error);
          telemetry.policyRejected(denial.metric);
          throw new RuntimeSocketError(denial.socketReason);
        }
        if (error instanceof RawAwarenessInspectionError) {
          telemetry.policyRejected(
            error.code === "raw_frame_too_large" ? "frame" : "awareness",
          );
          throw new RuntimeSocketError("awareness_denied");
        }
        throw error;
      }
    },
    async afterHandleMessage({ documentName, socketId, update }) {
      rawAwarenessInspections.delete(connectionKey(socketId, documentName));
      rawIngressGate?.completeProcessing(socketId, update);
    },
    async beforeHandleAwareness({ context, documentName, socketId, states }) {
      if (!context) throw new RuntimeSocketError("grant_denied");
      requireAdmissionReady();
      const key = connectionKey(socketId, documentName);
      try {
        const inspection = rawAwarenessInspections.get(key);
        if (!inspection) {
          throw new RuntimePolicyError("awareness_structure_invalid");
        }
        inspection.statePayload.restoreInto(states as Map<number, unknown>);
        const values = [...states.values()];
        validateAwarenessEnvelope(
          { byteLength: inspection.awarenessBytes, states: values },
          {
            maxBytes: config.maxAwarenessBytes,
            maxDepth: config.maxAwarenessDepth,
            maxStates: config.maxAwarenessStates,
          },
        );
        const awarenessIdentity = assertSingleAwarenessState(
          states as Map<number, unknown>,
        );
        const boundClientId = awarenessClientIds.get(key);
        const clientOwner = awarenessClientOwners
          .get(documentName)
          ?.get(awarenessIdentity.clientId);
        if (
          (boundClientId === undefined && awarenessIdentity.removal) ||
          (boundClientId !== undefined &&
            boundClientId !== awarenessIdentity.clientId) ||
          (clientOwner !== undefined && clientOwner !== key)
        ) {
          throw new RuntimePolicyError("awareness_identity_invalid");
        }
        if (!awarenessIdentity.removal) {
          sanitizeAwarenessStates(states, context);
        }
        if (boundClientId === undefined) {
          const owners =
            awarenessClientOwners.get(documentName) ??
            new Map<number, string>();
          owners.set(awarenessIdentity.clientId, key);
          awarenessClientOwners.set(documentName, owners);
          awarenessClientIds.set(key, awarenessIdentity.clientId);
        }
        telemetry.awareness();
      } catch (error) {
        if (error instanceof RuntimePolicyError) {
          const denial = policyDenial(error);
          telemetry.policyRejected(denial.metric);
          throw new RuntimeSocketError(denial.socketReason);
        }
        throw error;
      }
    },
    async beforeSync({ context, documentName, payload, type }) {
      requireAdmissionReady(type !== 0);
      if (type === 0) return;
      if (context.capability === "view") {
        throw new RuntimeSocketError("write_disabled");
      }
      try {
        documentBudget.reserve(documentName, payload.byteLength);
      } catch {
        telemetry.policyRejected("update");
        throw new RuntimeSocketError("update_denied");
      }
    },
    async onLoadDocument({ context, document, documentName }) {
      try {
        const checkpoint = await dependencies.checkpoints.load(context);
        if (checkpoint) {
          Y.applyUpdate(document, checkpoint.state);
          telemetry.checkpoint("loaded");
        }
        documentBudget.load(
          documentName,
          Y.encodeStateAsUpdate(document).byteLength,
        );
        telemetry.setDocuments(server.hocuspocus.getDocumentsCount());
        return document;
      } catch {
        rollbackPendingDocument(connections, documentName, "load");
        readinessCoordinator.markCheckpointFailed();
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
        const state = Y.encodeStateAsUpdate(document);
        await dependencies.checkpoints.store(lastContext, state);
        documentBudget.load(documentName, state.byteLength);
        dirtyDocuments.delete(documentName);
        if (dirtyDocuments.size === 0) {
          readinessCoordinator.markCheckpointRecovered();
        }
        telemetry.setDirtyDocuments(dirtyDocuments.size);
        telemetry.checkpoint("stored");
        dependencies.logger.event("checkpoint_ok", "ok");
      } catch {
        readinessCoordinator.markCheckpointFailed();
        closeInvalidScopes(connections, new Set<string>());
        telemetry.checkpoint("failed");
        dependencies.logger.event("checkpoint_failed", "failed");
        throw new RuntimeSocketError("checkpoint_unavailable");
      }
    },
    async afterUnloadDocument({ documentName }) {
      documentBudget.release(documentName);
      telemetry.setDocuments(server.hocuspocus.getDocumentsCount());
    },
    async onDisconnect({ documentName, socketId }) {
      const key = connectionKey(socketId, documentName);
      const active = connections.get(key);
      if (active) {
        const released = active.reservation.release();
        if (released.released) {
          telemetry.connection(active.capability, "closed");
        }
      }
      connections.delete(key);
      const clientId = awarenessClientIds.get(key);
      if (clientId !== undefined) {
        const owners = awarenessClientOwners.get(documentName);
        if (owners?.get(clientId) === key) owners.delete(clientId);
        if (owners?.size === 0) awarenessClientOwners.delete(documentName);
      }
      awarenessClientIds.delete(key);
      rawAwarenessInspections.delete(key);
      awarenessIngressPolicy.release(key);
      telemetry.setDocuments(server.hocuspocus.getDocumentsCount());
    },
  });

  const rawIngressGate = installRawWebSocketIngressGate(server.hocuspocus, {
    awareness: {
      maxBytes: config.maxAwarenessBytes,
      maxDepth: config.maxAwarenessDepth,
      maxStates: config.maxAwarenessStates,
    },
    maxBytesPerWindow: config.maxIngressBytesPerSecond,
    maxFrameBytes: config.maxFrameBytes,
    maxMessagesPerWindow: config.maxMessagesPerSecond,
    maxPendingUnauthenticatedSockets: config.maxConnections,
    maxQueuedBytesPerSocket: config.maxIngressBytesPerSecond,
    maxQueuedMessagesPerSocket: Math.min(16, config.maxMessagesPerSecond),
    onReject: (reason) => {
      telemetry.policyRejected(
        reason === "raw_frame_too_large" ? "frame" : "backpressure",
      );
      dependencies.logger.event("connection_denied", "denied");
    },
    windowMs: 1_000,
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
    const snapshot = readinessCoordinator.readiness();
    return { ready: snapshot.ready, reason: snapshot.reason };
  };

  const refreshDependencies = async (): Promise<void> => {
    const outcome = await readinessCoordinator.refreshDependencies(async () => {
      const [authorityGuard, persistence, control] = await Promise.allSettled([
        dependencies.authorityGuard.probe(),
        dependencies.checkpoints.probe(),
        dependencies.controlPlane.probe(),
      ]);
      const authorityGuardHealthy = authorityGuard.status === "fulfilled";
      const persistenceHealthy = persistence.status === "fulfilled";
      const controlPlaneHealthy = control.status === "fulfilled";
      telemetry.dependency("authority_guard", authorityGuardHealthy);
      telemetry.dependency("persistence", persistenceHealthy);
      telemetry.dependency("control_plane", controlPlaneHealthy);
      return {
        authorityGuard: authorityGuardHealthy,
        controlPlane: controlPlaneHealthy,
        mode: control.status === "fulfilled" ? control.value.mode : "off",
        persistence: persistenceHealthy,
      };
    });
    if (!outcome.applied) {
      if (outcome.reason === "failed") {
        closeInvalidScopes(connections, new Set<string>());
        dependencies.logger.event("dependency_down", "failed");
      }
      return;
    }
    if (!outcome.snapshot.ready) {
      closeInvalidScopes(connections, new Set<string>());
      dependencies.logger.event("dependency_down", "failed");
      return;
    }
    dependencies.logger.event("dependency_up", "ok");
  };

  const refreshAuthority = async (): Promise<void> => {
    const outcome = await readinessCoordinator.refreshAuthority(async () => {
      await dependencies.authorityGuard.probe();
      telemetry.dependency("authority_guard", true);
      const next = await dependencies.controlPlane.probe();
      telemetry.dependency("control_plane", true);
      const activeScopes = [...connections.values()].map(
        (active) => active.scope,
      );
      const validLeases =
        await dependencies.controlPlane.validateScopes(activeScopes);
      return { mode: next.mode, value: validLeases };
    });
    if (!outcome.applied) {
      if (outcome.reason === "failed") {
        telemetry.dependency("authority_guard", false);
        telemetry.dependency("control_plane", false);
        closeInvalidScopes(connections, new Set<string>());
        dependencies.logger.event("dependency_down", "failed");
      }
      return;
    }
    if (outcome.value.mode !== "enabled") {
      closeWriters(connections, outcome.value.mode);
    }
    closeInvalidScopes(connections, outcome.value.value);
  };

  const scheduleDependencyRefresh = (): void => {
    if (dependencyRefreshRunning) return;
    dependencyRefreshRunning = true;
    void refreshDependencies().finally(() => {
      dependencyRefreshRunning = false;
    });
  };

  const scheduleAuthorityRefresh = (): void => {
    if (authorityRefreshRunning) return;
    authorityRefreshRunning = true;
    void refreshAuthority().finally(() => {
      authorityRefreshRunning = false;
    });
  };

  const drain = (): Promise<void> => {
    drainPromise ??= (async () => {
      const startedAt = Date.now();
      readinessCoordinator.beginDrain();
      telemetry.setDraining(true);
      dependencies.logger.event("drain_started", "started");
      if (dependencyTimer) clearInterval(dependencyTimer);
      if (authorityTimer) clearInterval(authorityTimer);
      let failed = false;
      try {
        server.hocuspocus.flushPendingStores();
        await withDeadline(server.destroy(), config.drainTimeoutMs);
        rawIngressGate?.dispose();
        if (dirtyDocuments.size !== 0)
          throw new Error("dirty_documents_remain");
        await dependencies.checkpoints.close();
      } catch {
        failed = true;
      }
      try {
        await dependencies.authorityGuard.release();
      } catch {
        failed = true;
      }
      readinessCoordinator.markStopped();
      telemetry.setDocuments(0);
      telemetry.setDraining(false);
      if (failed) {
        dependencies.logger.event(
          "drain_failed",
          "failed",
          Date.now() - startedAt,
        );
        throw new Error("runtime_drain_failed");
      }
      dependencies.logger.event("drain_complete", "ok", Date.now() - startedAt);
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
      await dependencies.authorityGuard.acquire();
      try {
        readinessCoordinator.activate();
        await server.listen();
        await refreshDependencies();
        void dependencies.snapshots
          .probe()
          .then(() => telemetry.dependency("snapshot", true))
          .catch(() => telemetry.dependency("snapshot", false));
        dependencyTimer = setInterval(scheduleDependencyRefresh, 5_000);
        dependencyTimer.unref();
        authorityTimer = setInterval(scheduleAuthorityRefresh, 750);
        authorityTimer.unref();
        dependencies.logger.event("runtime_started", "ok");
      } catch (error) {
        readinessCoordinator.markStopped();
        rawIngressGate?.dispose();
        await dependencies.authorityGuard.release().catch(() => undefined);
        throw error;
      }
    },
  };
}

function connectionKey(socketId: string, documentName: string): string {
  return `${socketId}:${documentName}`;
}

function rollbackPendingDocument(
  connections: Map<string, ActiveConnection>,
  documentName: string,
  stage: "load" | "setup",
): void {
  for (const [key, active] of connections) {
    if (
      active.connection === undefined &&
      active.scope.providerDocumentName === documentName
    ) {
      active.reservation.rollback(stage);
      connections.delete(key);
    }
  }
}

function assertSingleAwarenessState(states: Map<number, unknown>): {
  clientId: number;
  removal: boolean;
} {
  if (states.size !== 1) {
    throw new RuntimePolicyError("awareness_state_count_exceeded");
  }
  const entry = states.entries().next().value;
  if (!entry) {
    throw new RuntimePolicyError("awareness_state_count_exceeded");
  }
  const [candidate, state] = entry;
  if (
    !Number.isSafeInteger(candidate) ||
    candidate < 0 ||
    candidate > 0xffff_ffff
  ) {
    throw new RuntimePolicyError("awareness_identity_invalid");
  }
  if (state !== null && !isPlainRecord(state)) {
    throw new RuntimePolicyError("awareness_structure_invalid");
  }
  return { clientId: candidate, removal: state === null };
}

const FORBIDDEN_AWARENESS_FIELDS = [
  "actorId",
  "authorityLease",
  "capability",
  "documentId",
  "generation",
  "grant",
  "sessionId",
  "tenantId",
] as const;

function sanitizeAwarenessStates(
  states: Map<number, Record<string, unknown>>,
  scope: CollaborationScope,
): void {
  for (const state of states.values()) {
    for (const field of FORBIDDEN_AWARENESS_FIELDS) delete state[field];
    const currentUser = isPlainRecord(state.user) ? state.user : undefined;
    const displayName = currentUser?.displayName;
    state.user = {
      id: scope.actorId,
      ...(typeof displayName === "string" && displayName.length <= 128
        ? { displayName }
        : {}),
    };
    state.tutorhub = {
      actorId: scope.actorId,
      capability: scope.capability,
      generation: scope.generation,
    };
  }
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function policyDenial(error: RuntimePolicyError): {
  metric: RuntimePolicyReason;
  socketReason: string;
} {
  switch (error.code) {
    case "actor_connection_quota":
    case "document_connection_quota":
    case "global_connection_quota":
    case "tenant_connection_quota":
      return {
        metric: "connection_quota",
        socketReason: "connection_quota_exceeded",
      };
    case "reconnect_storm_denied":
      return { metric: "reconnect", socketReason: "reconnect_storm_denied" };
    case "awareness_bytes_exceeded":
    case "awareness_depth_exceeded":
    case "awareness_identity_invalid":
    case "awareness_state_count_exceeded":
    case "awareness_structure_invalid":
      return { metric: "awareness", socketReason: error.code };
    case "ingress_byte_budget_exceeded":
    case "ingress_message_budget_exceeded":
      return { metric: "backpressure", socketReason: "backpressure_denied" };
    default:
      return { metric: "backpressure", socketReason: "policy_denied" };
  }
}

function closeWriters(
  connections: Map<string, ActiveConnection>,
  mode: RuntimeMode,
): void {
  for (const [key, active] of connections) {
    if (mode === "off" || active.capability !== "view") {
      if (active.connection) {
        active.connection.close({ code: 4403, reason: "runtime_mode_changed" });
      } else {
        active.reservation.rollback("setup");
        connections.delete(key);
      }
    }
  }
}

function closeInvalidScopes(
  connections: Map<string, ActiveConnection>,
  validLeases: Set<string>,
): void {
  for (const [key, active] of connections) {
    if (!validLeases.has(active.scope.authorityLease)) {
      if (active.connection) {
        active.connection.close({ code: 4403, reason: "authority_lost" });
      } else {
        active.reservation.rollback("setup");
        connections.delete(key);
      }
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
