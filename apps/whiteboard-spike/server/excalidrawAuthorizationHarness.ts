import { createHash, randomBytes } from "node:crypto";
import {
  createServer,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
  WebSocketStatus,
} from "@hocuspocus/provider";
import { Server, type Connection } from "@hocuspocus/server";
import { WebSocket as NodeWebSocket, type RawData } from "ws";
import * as Y from "yjs";
import {
  CollaborationAuthorizationError,
  EXCALIDRAW_AUTHORIZATION_LIMITS,
  capabilityIncludes,
  type CollaborationAuthorizationErrorCode,
  type CollaborationCapability,
  type CollaborationGrantRequest,
  type CollaborationGrantResponse,
} from "../src/excalidraw/authorizationContract";
import { CanonicalExcalidrawAuthority } from "../src/excalidraw/canonicalAuthority";

const ID_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/;

export interface MembershipFixture {
  actorId: string;
  capability: CollaborationCapability;
  sessionId: string;
  tenantId: string;
}

interface DocumentState {
  generation: number;
  status: "active" | "closed";
}

interface GrantClaims {
  actorId: string;
  capability: CollaborationCapability;
  documentId: string;
  expiresAt: number;
  generation: number;
  origin: string;
  providerDocumentName: string;
  sessionId: string;
  tenantId: string;
}

export interface AuthorizedConnectionContext {
  actorId: string;
  capability: CollaborationCapability;
  documentId: string;
  generation: number;
  providerDocumentName: string;
  sessionId: string;
  tenantId: string;
}

export interface LifecycleTransition {
  action: "close" | "restore" | "revoke";
  actorId?: string;
  documentId: string;
  nextGeneration: number;
  tenantId: string;
}

export interface AuthorizationEvidence {
  activeConnections: number;
  rejectionCounts: Partial<Record<CollaborationAuthorizationErrorCode, number>>;
}

export interface AuthorizedExcalidrawServer {
  controlPlane: CollaborationControlPlane;
  controlUrl: string;
  destroy: () => Promise<void>;
  evidence: AuthorizationEvidence;
  providerUrl: string;
}

export interface AuthorizedTestClient {
  authenticationFailures: string[];
  closeReasons: string[];
  destroy: () => void;
  disconnect: () => Promise<void>;
  document: Y.Doc;
  provider: HocuspocusProvider;
  socket: HocuspocusProviderWebsocket;
  traffic: AuthorizedClientTraffic;
}

export interface AuthorizedClientTraffic {
  receivedBytes: number;
}

const DEFAULT_MEMBERSHIPS: MembershipFixture[] = [
  {
    actorId: "teacher-a",
    capability: "present",
    sessionId: "teacher-session",
    tenantId: "tenant-a",
  },
  {
    actorId: "student-b",
    capability: "edit",
    sessionId: "student-session",
    tenantId: "tenant-a",
  },
  {
    actorId: "viewer-c",
    capability: "view",
    sessionId: "viewer-session",
    tenantId: "tenant-a",
  },
  {
    actorId: "tenant-b-teacher",
    capability: "present",
    sessionId: "tenant-b-session",
    tenantId: "tenant-b",
  },
];

export class CollaborationControlPlane {
  private readonly documents = new Map<string, DocumentState>();
  private readonly grants = new Map<string, GrantClaims>();
  private readonly issueBudget = new RollingWindowBudget(
    EXCALIDRAW_AUTHORIZATION_LIMITS.maxGrantIssuesPerWindow,
    EXCALIDRAW_AUTHORIZATION_LIMITS.grantIssueWindowMs,
  );
  private readonly memberships = new Map<string, MembershipFixture>();
  private readonly revokedActors = new Set<string>();
  private readonly transitionListeners = new Set<
    (transition: LifecycleTransition) => void
  >();
  private available = true;

  constructor(
    readonly allowedOrigin: string,
    memberships: MembershipFixture[] = DEFAULT_MEMBERSHIPS,
    private readonly now: () => number = Date.now,
  ) {
    for (const membership of memberships) {
      assertIdentifier(membership.tenantId);
      assertIdentifier(membership.actorId);
      assertIdentifier(membership.sessionId);
      this.memberships.set(
        membershipKey(membership.tenantId, membership.actorId),
        {
          ...membership,
        },
      );
    }
  }

  issueGrant(
    request: CollaborationGrantRequest,
    origin: string,
    ttlMs: number = EXCALIDRAW_AUTHORIZATION_LIMITS.defaultGrantTtlMs,
  ): CollaborationGrantResponse {
    this.assertAvailable();
    assertOrigin(origin, this.allowedOrigin);
    validateGrantRequest(request);
    if (
      !Number.isSafeInteger(ttlMs) ||
      ttlMs <= 0 ||
      ttlMs > EXCALIDRAW_AUTHORIZATION_LIMITS.maxGrantTtlMs
    ) {
      throw new CollaborationAuthorizationError("grant_ttl_invalid");
    }

    const membership = this.memberships.get(
      membershipKey(request.tenantId, request.actorId),
    );
    if (!membership) {
      throw new CollaborationAuthorizationError("membership_denied");
    }
    if (membership.sessionId !== request.sessionId) {
      throw new CollaborationAuthorizationError("session_binding_denied");
    }
    if (
      !capabilityIncludes(membership.capability, request.requestedCapability)
    ) {
      throw new CollaborationAuthorizationError("capability_escalation_denied");
    }

    const document = this.getOrCreateDocument(
      request.tenantId,
      request.documentId,
    );
    if (document.status !== "active") {
      throw new CollaborationAuthorizationError("document_closed");
    }
    if (
      request.expectedGeneration !== undefined &&
      request.expectedGeneration !== document.generation
    ) {
      throw new CollaborationAuthorizationError("stale_generation");
    }
    if (
      this.revokedActors.has(
        actorDocumentKey(request.tenantId, request.documentId, request.actorId),
      )
    ) {
      throw new CollaborationAuthorizationError("actor_revoked");
    }

    this.issueBudget.consume(
      membershipKey(request.tenantId, request.actorId),
      this.now(),
      "grant_issue_rate_limited",
    );
    this.pruneExpiredGrants();
    const grant = randomBytes(32).toString("base64url");
    const expiresAt = this.now() + ttlMs;
    const providerDocumentName = opaqueProviderDocumentName(
      request.tenantId,
      request.documentId,
      document.generation,
    );
    this.grants.set(hash(`grant:${grant}`), {
      actorId: request.actorId,
      capability: request.requestedCapability,
      documentId: request.documentId,
      expiresAt,
      generation: document.generation,
      origin,
      providerDocumentName,
      sessionId: request.sessionId,
      tenantId: request.tenantId,
    });

    return {
      capability: request.requestedCapability,
      expiresInSeconds: Math.floor((expiresAt - this.now()) / 1_000),
      generation: document.generation,
      grant,
      providerDocumentName,
    };
  }

  exchangeGrant(
    grant: string,
    origin: string,
    providerDocumentName: string,
  ): AuthorizedConnectionContext {
    this.assertAvailable();
    assertOrigin(origin, this.allowedOrigin);
    const grantHash = hash(`grant:${grant}`);
    const claims = this.grants.get(grantHash);
    this.grants.delete(grantHash);
    if (!claims) {
      throw new CollaborationAuthorizationError("grant_invalid_or_replayed");
    }
    if (claims.expiresAt <= this.now()) {
      throw new CollaborationAuthorizationError("grant_expired");
    }
    if (
      claims.origin !== origin ||
      claims.providerDocumentName !== providerDocumentName
    ) {
      throw new CollaborationAuthorizationError("grant_binding_mismatch");
    }
    const document = this.getOrCreateDocument(
      claims.tenantId,
      claims.documentId,
    );
    if (document.status !== "active") {
      throw new CollaborationAuthorizationError("document_closed");
    }
    if (document.generation !== claims.generation) {
      throw new CollaborationAuthorizationError("stale_generation");
    }
    if (
      this.revokedActors.has(
        actorDocumentKey(claims.tenantId, claims.documentId, claims.actorId),
      )
    ) {
      throw new CollaborationAuthorizationError("actor_revoked");
    }

    return {
      actorId: claims.actorId,
      capability: claims.capability,
      documentId: claims.documentId,
      generation: claims.generation,
      providerDocumentName: claims.providerDocumentName,
      sessionId: claims.sessionId,
      tenantId: claims.tenantId,
    };
  }

  transitionDocument({
    action,
    actorId,
    documentId,
    tenantId,
  }: Omit<LifecycleTransition, "nextGeneration">): LifecycleTransition {
    this.assertAvailable();
    assertIdentifier(tenantId);
    assertIdentifier(documentId);
    if (actorId !== undefined) {
      assertIdentifier(actorId);
    }
    if (action === "revoke" && actorId === undefined) {
      throw new CollaborationAuthorizationError("identifier_invalid");
    }
    const document = this.getOrCreateDocument(tenantId, documentId);
    document.generation += 1;
    document.status = action === "close" ? "closed" : "active";
    if (action === "revoke" && actorId) {
      this.revokedActors.add(actorDocumentKey(tenantId, documentId, actorId));
    }
    const transition: LifecycleTransition = {
      action,
      actorId,
      documentId,
      nextGeneration: document.generation,
      tenantId,
    };
    for (const listener of this.transitionListeners) {
      listener(transition);
    }
    return transition;
  }

  currentGeneration(tenantId: string, documentId: string): number {
    this.assertAvailable();
    return this.getOrCreateDocument(tenantId, documentId).generation;
  }

  setAvailable(available: boolean): void {
    this.available = available;
  }

  subscribeTransitions(
    listener: (transition: LifecycleTransition) => void,
  ): () => void {
    this.transitionListeners.add(listener);
    return () => this.transitionListeners.delete(listener);
  }

  private assertAvailable(): void {
    if (!this.available) {
      throw new CollaborationAuthorizationError(
        "authorization_authority_unavailable",
      );
    }
  }

  private getOrCreateDocument(
    tenantId: string,
    documentId: string,
  ): DocumentState {
    const key = documentKey(tenantId, documentId);
    const existing = this.documents.get(key);
    if (existing) return existing;
    const created: DocumentState = { generation: 1, status: "active" };
    this.documents.set(key, created);
    return created;
  }

  private pruneExpiredGrants(): void {
    const now = this.now();
    for (const [grantHash, claims] of this.grants) {
      if (claims.expiresAt <= now) {
        this.grants.delete(grantHash);
      }
    }
  }
}

export class ConnectionQuota {
  private readonly actors = new Map<string, number>();
  private readonly documents = new Map<string, number>();
  private readonly tenants = new Map<string, number>();
  private readonly reconnectBudget = new RollingWindowBudget(
    EXCALIDRAW_AUTHORIZATION_LIMITS.maxReconnectAttemptsPerWindow,
    EXCALIDRAW_AUTHORIZATION_LIMITS.reconnectWindowMs,
  );
  private available = true;

  acquire(context: AuthorizedConnectionContext, now = Date.now()): () => void {
    if (!this.available) {
      throw new CollaborationAuthorizationError("rate_authority_unavailable");
    }
    const actorKey = membershipKey(context.tenantId, context.actorId);
    const scopedDocumentKey = documentKey(context.tenantId, context.documentId);
    this.reconnectBudget.consume(actorKey, now, "reconnect_storm_denied");
    if (
      (this.actors.get(actorKey) ?? 0) >=
      EXCALIDRAW_AUTHORIZATION_LIMITS.maxConnectionsPerActor
    ) {
      throw new CollaborationAuthorizationError("actor_connection_quota");
    }
    if (
      (this.documents.get(scopedDocumentKey) ?? 0) >=
      EXCALIDRAW_AUTHORIZATION_LIMITS.maxConnectionsPerDocument
    ) {
      throw new CollaborationAuthorizationError("document_connection_quota");
    }
    if (
      (this.tenants.get(context.tenantId) ?? 0) >=
      EXCALIDRAW_AUTHORIZATION_LIMITS.maxConnectionsPerTenant
    ) {
      throw new CollaborationAuthorizationError("tenant_connection_quota");
    }

    increment(this.actors, actorKey);
    increment(this.documents, scopedDocumentKey);
    increment(this.tenants, context.tenantId);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      decrement(this.actors, actorKey);
      decrement(this.documents, scopedDocumentKey);
      decrement(this.tenants, context.tenantId);
    };
  }

  activeConnections(): number {
    return [...this.tenants.values()].reduce(
      (total, count) => total + count,
      0,
    );
  }

  setAvailable(available: boolean): void {
    this.available = available;
  }
}

class RollingWindowBudget {
  private readonly events = new Map<string, number[]>();

  constructor(
    private readonly limit: number,
    private readonly windowMs: number,
  ) {}

  consume(
    key: string,
    now: number,
    code: CollaborationAuthorizationErrorCode,
  ): void {
    const events = (this.events.get(key) ?? []).filter(
      (timestamp) => now - timestamp < this.windowMs,
    );
    if (events.length >= this.limit) {
      this.events.set(key, events);
      throw new CollaborationAuthorizationError(code);
    }
    events.push(now);
    this.events.set(key, events);
  }
}

class SocketPolicyError extends Error {
  readonly code = 1008;

  constructor(readonly reason: CollaborationAuthorizationErrorCode) {
    super(reason);
    this.name = "SocketPolicyError";
  }
}

export async function startAuthorizedExcalidrawServer({
  allowedOrigin = "http://127.0.0.1:4180",
  controlPlane = new CollaborationControlPlane(allowedOrigin),
}: {
  allowedOrigin?: string;
  controlPlane?: CollaborationControlPlane;
} = {}): Promise<AuthorizedExcalidrawServer> {
  const evidence: AuthorizationEvidence = {
    activeConnections: 0,
    rejectionCounts: {},
  };
  const connectionQuota = new ConnectionQuota();
  const trafficBudget = new Map<string, { count: number; startedAt: number }>();
  const connections = new Map<
    string,
    {
      connection: Connection<AuthorizedConnectionContext>;
      context: AuthorizedConnectionContext;
      release: () => void;
    }
  >();

  const countRejection = (code: CollaborationAuthorizationErrorCode) => {
    evidence.rejectionCounts[code] = (evidence.rejectionCounts[code] ?? 0) + 1;
  };

  const server = new Server<AuthorizedConnectionContext>({
    address: "127.0.0.1",
    maxPendingDocuments: 8,
    maxUnauthenticatedQueueMessages: 16,
    maxUnauthenticatedQueueSize: 16 * 1024,
    port: 0,
    quiet: true,
    stopOnSignals: false,
    websocketOptions: {
      maxPayload: EXCALIDRAW_AUTHORIZATION_LIMITS.maxFrameBytes,
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
        const context = controlPlane.exchangeGrant(token, origin, documentName);
        const release = connectionQuota.acquire(context);
        connectionConfig.readOnly = context.capability === "view";
        connections.set(socketId, {
          connection:
            undefined as unknown as Connection<AuthorizedConnectionContext>,
          context,
          release,
        });
        evidence.activeConnections = connectionQuota.activeConnections();
        return context;
      } catch (error) {
        const code = safeAuthorizationCode(error);
        countRejection(code);
        throw new SocketPolicyError(code);
      }
    },
    async connected({ connection, context, socketId }) {
      const pending = connections.get(socketId);
      if (pending) {
        pending.connection = connection;
        pending.context = context;
      }
    },
    async beforeHandleMessage({ socketId, update }) {
      try {
        if (update.byteLength > EXCALIDRAW_AUTHORIZATION_LIMITS.maxFrameBytes) {
          throw new CollaborationAuthorizationError("update_too_large");
        }
        const now = Date.now();
        const window = trafficBudget.get(socketId);
        const activeWindow =
          !window || now - window.startedAt >= 1_000
            ? { count: 0, startedAt: now }
            : window;
        activeWindow.count += 1;
        trafficBudget.set(socketId, activeWindow);
        if (
          activeWindow.count >
          EXCALIDRAW_AUTHORIZATION_LIMITS.maxMessagesPerSecond
        ) {
          throw new CollaborationAuthorizationError("update_rate_limited");
        }
      } catch (error) {
        const code = safeAuthorizationCode(error);
        countRejection(code);
        throw new SocketPolicyError(code);
      }
    },
    async beforeSync({ context, document, payload, type }) {
      if (type === 0) return;
      try {
        validateCanonicalUpdate(document, payload, context);
      } catch (error) {
        const code = safeAuthorizationCode(error, "scene_budget_denied");
        countRejection(code);
        throw new SocketPolicyError(code);
      }
    },
    async beforeHandleAwareness({ states }) {
      try {
        validateAwareness(states);
      } catch (error) {
        const code = safeAuthorizationCode(error, "scene_budget_denied");
        countRejection(code);
        throw new SocketPolicyError(code);
      }
    },
    async onDisconnect({ socketId }) {
      const active = connections.get(socketId);
      active?.release();
      connections.delete(socketId);
      trafficBudget.delete(socketId);
      evidence.activeConnections = connectionQuota.activeConnections();
    },
  });

  const unsubscribeTransitions = controlPlane.subscribeTransitions(
    (transition) => {
      for (const active of connections.values()) {
        if (
          active.context.tenantId === transition.tenantId &&
          active.context.documentId === transition.documentId
        ) {
          active.connection?.close({
            code: 4403,
            reason: "scope_generation_revoked",
          });
        }
      }
    },
  );

  await server.listen();
  const providerUrl = `ws://127.0.0.1:${server.address.port}`;
  const controlServer = createControlHttpServer(controlPlane, evidence);
  await new Promise<void>((resolve, reject) => {
    controlServer.once("error", reject);
    controlServer.listen(0, "127.0.0.1", () => resolve());
  });
  const address = controlServer.address();
  if (!address || typeof address === "string") {
    throw new Error("gate_c_control_address_unavailable");
  }

  return {
    controlPlane,
    controlUrl: `http://127.0.0.1:${address.port}`,
    destroy: async () => {
      unsubscribeTransitions();
      await new Promise<void>((resolveClose, rejectClose) => {
        controlServer.close((error) => {
          if (error) rejectClose(error);
          else resolveClose();
        });
      });
      await server.destroy();
    },
    evidence,
    providerUrl,
  };
}

export function createAuthorizedTestClient({
  grant,
  origin,
  providerDocumentName,
  providerUrl,
}: {
  grant: string;
  origin: string;
  providerDocumentName: string;
  providerUrl: string;
}): AuthorizedTestClient {
  const document = new Y.Doc();
  const authenticationFailures: string[] = [];
  const closeReasons: string[] = [];
  const traffic: AuthorizedClientTraffic = { receivedBytes: 0 };
  const OriginWebSocket = createOriginWebSocket(origin, traffic);
  const socket = new HocuspocusProviderWebsocket({
    WebSocketPolyfill: OriginWebSocket,
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
    name: providerDocumentName,
    onAuthenticationFailed: ({ reason }) => {
      authenticationFailures.push(reason);
    },
    onClose: ({ event }) => {
      closeReasons.push(event.reason);
    },
    sessionAwareness: true,
    token: grant,
    websocketProvider: socket,
  });
  provider.attach();
  void socket.connect();

  return {
    authenticationFailures,
    closeReasons,
    destroy: () => {
      provider.destroy();
      socket.destroy();
      document.destroy();
    },
    disconnect: async () => {
      socket.disconnect();
      await waitUntil(
        () => socket.status === WebSocketStatus.Disconnected,
        "gate_c_client_disconnect_timeout",
      );
    },
    document,
    provider,
    socket,
    traffic,
  };
}

export async function waitForAuthorizedClient(
  client: AuthorizedTestClient,
  timeoutMs = 5_000,
): Promise<void> {
  await waitUntil(
    () => client.provider.synced && client.provider.isAuthenticated,
    "gate_c_client_authentication_timeout",
    timeoutMs,
  );
}

export async function waitUntil(
  predicate: () => boolean,
  failureMessage: string,
  timeoutMs = 5_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  throw new Error(failureMessage);
}

function createControlHttpServer(
  controlPlane: CollaborationControlPlane,
  evidence: AuthorizationEvidence,
) {
  return createServer(async (request, response) => {
    setPrivateResponseHeaders(response, controlPlane.allowedOrigin);
    if (request.method === "OPTIONS") {
      if (request.headers.origin !== controlPlane.allowedOrigin) {
        sendJson(response, 403, { error: "origin_denied" });
        return;
      }
      response.writeHead(204).end();
      return;
    }
    try {
      if (
        request.method !== "POST" ||
        new URL(request.url ?? "/", "http://127.0.0.1").pathname !==
          "/gate-c/grants"
      ) {
        sendJson(response, 404, { error: "document_scope_denied" });
        return;
      }
      const origin = request.headers.origin ?? "";
      const body = await readJsonBody(request);
      const grant = controlPlane.issueGrant(
        body as unknown as CollaborationGrantRequest,
        origin,
      );
      sendJson(response, 201, grant);
    } catch (error) {
      const code = safeAuthorizationCode(error);
      evidence.rejectionCounts[code] =
        (evidence.rejectionCounts[code] ?? 0) + 1;
      sendJson(response, errorStatus(code), { error: code });
    }
  });
}

function validateCanonicalUpdate(
  currentDocument: Y.Doc,
  update: Uint8Array,
  context: AuthorizedConnectionContext,
): void {
  if (update.byteLength > EXCALIDRAW_AUTHORIZATION_LIMITS.maxUpdateBytes) {
    throw new CollaborationAuthorizationError("update_too_large");
  }
  const beforeVector = Y.encodeStateVector(currentDocument);
  const probe = new Y.Doc();
  let authority: CanonicalExcalidrawAuthority | undefined;
  try {
    Y.applyUpdate(probe, Y.encodeStateAsUpdate(currentDocument));
    Y.applyUpdate(probe, update);
    const changed = !equalBytes(beforeVector, Y.encodeStateVector(probe));
    if (!changed) return;
    if (context.capability === "view") {
      throw new CollaborationAuthorizationError("reader_mutation_denied");
    }
    authority = new CanonicalExcalidrawAuthority(
      probe,
      {
        documentId: context.documentId,
        generation: context.generation,
        tenantId: context.tenantId,
      },
      context.actorId,
    );
    if (!authority.isInitialized()) {
      throw new CollaborationAuthorizationError("scene_budget_denied");
    }
    authority.getScene();
  } catch (error) {
    if (error instanceof CollaborationAuthorizationError) throw error;
    throw new CollaborationAuthorizationError("scene_budget_denied");
  } finally {
    authority?.destroy();
    probe.destroy();
  }
}

function validateAwareness(states: Map<number, Record<string, unknown>>): void {
  if (states.size > EXCALIDRAW_AUTHORIZATION_LIMITS.maxAwarenessStates) {
    throw new CollaborationAuthorizationError("scene_budget_denied");
  }
  const values = [...states.values()];
  const serialized = JSON.stringify(values);
  if (
    Buffer.byteLength(serialized) >
    EXCALIDRAW_AUTHORIZATION_LIMITS.maxAwarenessBytes
  ) {
    throw new CollaborationAuthorizationError("scene_budget_denied");
  }
  validateDepth(values, 0);
}

function validateDepth(value: unknown, depth: number): void {
  if (depth > EXCALIDRAW_AUTHORIZATION_LIMITS.maxAwarenessDepth) {
    throw new CollaborationAuthorizationError("scene_budget_denied");
  }
  if (Array.isArray(value)) {
    for (const item of value) validateDepth(item, depth + 1);
  } else if (typeof value === "object" && value !== null) {
    for (const item of Object.values(value)) validateDepth(item, depth + 1);
  }
}

function createOriginWebSocket(
  origin: string,
  traffic: AuthorizedClientTraffic,
) {
  return class OriginWebSocket extends NodeWebSocket {
    constructor(address: string | URL, protocols?: string | string[]) {
      super(address, protocols, { origin });
      this.on("message", (data: RawData) => {
        traffic.receivedBytes += rawDataByteLength(data);
      });
    }
  };
}

function rawDataByteLength(data: RawData): number {
  if (Array.isArray(data)) {
    return data.reduce((total, chunk) => total + chunk.byteLength, 0);
  }
  return data.byteLength;
}

async function readJsonBody(request: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  let bytes = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    bytes += buffer.byteLength;
    if (bytes > EXCALIDRAW_AUTHORIZATION_LIMITS.maxHttpBodyBytes) {
      throw new CollaborationAuthorizationError("scene_budget_denied");
    }
    chunks.push(buffer);
  }
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
  } catch {
    throw new CollaborationAuthorizationError("scene_budget_denied");
  }
}

function validateGrantRequest(
  request: CollaborationGrantRequest,
): asserts request is CollaborationGrantRequest {
  if (!request || typeof request !== "object") {
    throw new CollaborationAuthorizationError("identifier_invalid");
  }
  assertIdentifier(request.tenantId);
  assertIdentifier(request.documentId);
  assertIdentifier(request.actorId);
  assertIdentifier(request.sessionId);
  if (!isCapability(request.requestedCapability)) {
    throw new CollaborationAuthorizationError("capability_escalation_denied");
  }
  if (
    request.expectedGeneration !== undefined &&
    (!Number.isSafeInteger(request.expectedGeneration) ||
      request.expectedGeneration < 1)
  ) {
    throw new CollaborationAuthorizationError("stale_generation");
  }
}

function assertIdentifier(value: unknown): asserts value is string {
  if (typeof value !== "string" || !ID_PATTERN.test(value)) {
    throw new CollaborationAuthorizationError("identifier_invalid");
  }
}

function assertOrigin(origin: string, allowedOrigin: string): void {
  if (origin !== allowedOrigin) {
    throw new CollaborationAuthorizationError("origin_denied");
  }
}

function isCapability(value: unknown): value is CollaborationCapability {
  return value === "view" || value === "edit" || value === "present";
}

function setPrivateResponseHeaders(
  response: ServerResponse,
  allowedOrigin: string,
): void {
  response.setHeader("Access-Control-Allow-Headers", "Content-Type");
  response.setHeader("Access-Control-Allow-Methods", "POST, OPTIONS");
  response.setHeader("Access-Control-Allow-Origin", allowedOrigin);
  response.setHeader("Access-Control-Allow-Credentials", "true");
  response.setHeader(
    "Access-Control-Expose-Headers",
    "Cache-Control, Referrer-Policy",
  );
  response.setHeader("Cache-Control", "no-store");
  response.setHeader("Content-Type", "application/json; charset=utf-8");
  response.setHeader("Referrer-Policy", "no-referrer");
  response.setHeader("Vary", "Origin");
}

function sendJson(
  response: ServerResponse,
  status: number,
  body: unknown,
): void {
  if (response.writableEnded) return;
  response.writeHead(status).end(JSON.stringify(body));
}

function safeAuthorizationCode(
  error: unknown,
  fallback: CollaborationAuthorizationErrorCode = "membership_denied",
): CollaborationAuthorizationErrorCode {
  return error instanceof CollaborationAuthorizationError
    ? error.code
    : fallback;
}

function errorStatus(code: CollaborationAuthorizationErrorCode): number {
  if (code === "stale_generation") return 409;
  if (code === "scene_budget_denied" || code === "update_too_large") {
    return 413;
  }
  if (
    code.endsWith("quota") ||
    code.endsWith("limited") ||
    code === "reconnect_storm_denied"
  ) {
    return 429;
  }
  return 403;
}

function opaqueProviderDocumentName(
  tenantId: string,
  documentId: string,
  generation: number,
): string {
  return `wb/${hash(`tenant:${tenantId}`).slice(0, 24)}/${hash(
    `document:${documentId}`,
  ).slice(0, 24)}/g${generation}`;
}

function membershipKey(tenantId: string, actorId: string): string {
  return `${tenantId}\u0000${actorId}`;
}

function documentKey(tenantId: string, documentId: string): string {
  return `${tenantId}\u0000${documentId}`;
}

function actorDocumentKey(
  tenantId: string,
  documentId: string,
  actorId: string,
): string {
  return `${tenantId}\u0000${documentId}\u0000${actorId}`;
}

function hash(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function increment(map: Map<string, number>, key: string): void {
  map.set(key, (map.get(key) ?? 0) + 1);
}

function decrement(map: Map<string, number>, key: string): void {
  const next = (map.get(key) ?? 1) - 1;
  if (next <= 0) map.delete(key);
  else map.set(key, next);
}

function equalBytes(first: Uint8Array, second: Uint8Array): boolean {
  if (first.byteLength !== second.byteLength) return false;
  return first.every((value, index) => value === second[index]);
}
