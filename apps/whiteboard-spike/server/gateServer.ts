import { createHash, randomBytes } from "node:crypto";
import { mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import {
  createServer,
  type IncomingMessage,
  type ServerResponse,
} from "node:http";
import { DatabaseSync } from "node:sqlite";
import { dirname, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import {
  NodeSqliteWrapper,
  SQLiteSyncStorage,
  TLSocketRoom,
  TLSyncErrorCloseEventCode,
  type RoomSnapshot,
  type WebSocketMinimal,
} from "@tldraw/sync-core";
import type { TLRecord } from "tldraw";
import {
  WebSocket,
  WebSocketServer,
  type WebSocketServer as WebSocketServerType,
} from "ws";

const DEFAULT_PORT = 4179;
const DEFAULT_ALLOWED_ORIGIN = "http://127.0.0.1:4178";
const MAX_HTTP_BODY_BYTES = 8 * 1024;
const MAX_FRAME_BYTES = 512 * 1024;
const MAX_MESSAGES_PER_SECOND = 120;
const MAX_SNAPSHOT_BYTES = 16 * 1024 * 1024;
const MAX_CONNECTIONS_PER_ACTOR = 4;
const MAX_CONNECTIONS_PER_DOCUMENT = 50;
const MAX_CONNECTIONS_PER_TENANT = 100;
const GRANT_TTL_MS = 30_000;
const PROTOCOL_NAME = "tutorhub-sync-v1";
const GRANT_PROTOCOL_PREFIX = "tutorhub-grant.";
const ID_PATTERN = /^[a-z0-9][a-z0-9:_-]{2,95}$/;
const ARTIFACT_ID_PATTERN = /^[a-f0-9]{32}$/;

export type GateCapability = "view" | "edit" | "present";

interface GrantClaims {
  tenantHash: string;
  documentHash: string;
  actorHash: string;
  capability: GateCapability;
  generation: number;
  origin: string;
  expiresAt: number;
}

interface GrantExchangeContext {
  documentHash: string;
  origin: string;
}

interface GateMetadata {
  tenantHash: string;
  documentHash: string;
  actorHash: string;
  capability: GateCapability;
  generation: number;
}

interface RoomHandle {
  key: string;
  tenantHash: string;
  documentHash: string;
  generation: number;
  database: DatabaseSync;
  room: TLSocketRoom<TLRecord, GateMetadata>;
  sessions: Map<string, GateMetadata>;
}

interface SnapshotEnvelope {
  envelopeVersion: 1;
  engine: "tldraw";
  syncVersion: "5.3.1";
  tenantBinding: string;
  documentBinding: string;
  generation: number;
  recordCount: number;
  snapshotBytes: number;
  checksum: string;
  snapshot: RoomSnapshot;
}

interface GateServerOptions {
  port?: number;
  allowedOrigin?: string;
  dataDir?: string;
  now?: () => number;
}

export class OneTimeGrantAuthority {
  private readonly grants = new Map<string, GrantClaims>();

  constructor(private readonly now: () => number = Date.now) {}

  issue(
    claims: Omit<GrantClaims, "expiresAt">,
    ttlMs = GRANT_TTL_MS,
  ): { grant: string; expiresAt: number } {
    if (!Number.isInteger(ttlMs) || ttlMs <= 0 || ttlMs > 60_000) {
      throw new Error("invalid_grant_ttl");
    }
    this.pruneExpired();
    const grant = randomBytes(32).toString("base64url");
    const expiresAt = this.now() + ttlMs;
    this.grants.set(hash(`grant:${grant}`), { ...claims, expiresAt });
    return { grant, expiresAt };
  }

  exchange(grant: string, context: GrantExchangeContext): GrantClaims {
    const grantHash = hash(`grant:${grant}`);
    const claims = this.grants.get(grantHash);
    this.grants.delete(grantHash);
    if (!claims) {
      throw new Error("grant_invalid_or_replayed");
    }
    if (claims.expiresAt <= this.now()) {
      throw new Error("grant_expired");
    }
    if (
      claims.documentHash !== context.documentHash ||
      claims.origin !== context.origin
    ) {
      throw new Error("grant_binding_mismatch");
    }
    return claims;
  }

  private pruneExpired(): void {
    const currentTime = this.now();
    for (const [grantHash, claims] of this.grants) {
      if (claims.expiresAt <= currentTime) {
        this.grants.delete(grantHash);
      }
    }
  }
}

class MetadataStore {
  private readonly database: DatabaseSync;

  constructor(path: string) {
    mkdirSync(dirname(path), { recursive: true });
    this.database = new DatabaseSync(path);
    this.database.exec(`
      PRAGMA journal_mode = WAL;
      CREATE TABLE IF NOT EXISTS gate_documents (
        tenant_hash TEXT NOT NULL,
        document_hash TEXT NOT NULL,
        generation INTEGER NOT NULL,
        PRIMARY KEY (tenant_hash, document_hash)
      );
      CREATE TABLE IF NOT EXISTS gate_revoked_actors (
        tenant_hash TEXT NOT NULL,
        document_hash TEXT NOT NULL,
        actor_hash TEXT NOT NULL,
        PRIMARY KEY (tenant_hash, document_hash, actor_hash)
      );
    `);
  }

  generation(tenantHash: string, documentHash: string): number {
    const row = this.database
      .prepare(
        "SELECT generation FROM gate_documents WHERE tenant_hash = ? AND document_hash = ?",
      )
      .get(tenantHash, documentHash) as { generation: number } | undefined;
    if (row) {
      return row.generation;
    }
    this.database
      .prepare(
        "INSERT INTO gate_documents (tenant_hash, document_hash, generation) VALUES (?, ?, 1)",
      )
      .run(tenantHash, documentHash);
    return 1;
  }

  incrementGeneration(tenantHash: string, documentHash: string): number {
    const nextGeneration = this.generation(tenantHash, documentHash) + 1;
    this.database
      .prepare(
        "UPDATE gate_documents SET generation = ? WHERE tenant_hash = ? AND document_hash = ?",
      )
      .run(nextGeneration, tenantHash, documentHash);
    return nextGeneration;
  }

  revokeActor(
    tenantHash: string,
    documentHash: string,
    actorHash: string,
  ): void {
    this.database
      .prepare(
        "INSERT OR IGNORE INTO gate_revoked_actors (tenant_hash, document_hash, actor_hash) VALUES (?, ?, ?)",
      )
      .run(tenantHash, documentHash, actorHash);
  }

  isActorRevoked(
    tenantHash: string,
    documentHash: string,
    actorHash: string,
  ): boolean {
    return Boolean(
      this.database
        .prepare(
          "SELECT 1 AS found FROM gate_revoked_actors WHERE tenant_hash = ? AND document_hash = ? AND actor_hash = ?",
        )
        .get(tenantHash, documentHash, actorHash),
    );
  }

  close(): void {
    this.database.close();
  }
}

class ConnectionQuota {
  private readonly actors = new Map<string, number>();
  private readonly documents = new Map<string, number>();
  private readonly tenants = new Map<string, number>();

  acquire(metadata: GateMetadata): () => void {
    const actorKey = `${metadata.tenantHash}:${metadata.actorHash}`;
    const documentKey = `${metadata.tenantHash}:${metadata.documentHash}`;
    if ((this.actors.get(actorKey) ?? 0) >= MAX_CONNECTIONS_PER_ACTOR) {
      throw new Error("actor_connection_quota");
    }
    if (
      (this.documents.get(documentKey) ?? 0) >= MAX_CONNECTIONS_PER_DOCUMENT
    ) {
      throw new Error("document_connection_quota");
    }
    if (
      (this.tenants.get(metadata.tenantHash) ?? 0) >= MAX_CONNECTIONS_PER_TENANT
    ) {
      throw new Error("tenant_connection_quota");
    }

    increment(this.actors, actorKey);
    increment(this.documents, documentKey);
    increment(this.tenants, metadata.tenantHash);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      decrement(this.actors, actorKey);
      decrement(this.documents, documentKey);
      decrement(this.tenants, metadata.tenantHash);
    };
  }
}

class BudgetedSocket implements WebSocketMinimal {
  private readonly listenerWrappers = new Map<
    (event: unknown) => void,
    (event: unknown) => void
  >();
  private windowStartedAt = Date.now();
  private messagesInWindow = 0;

  constructor(private readonly socket: WebSocket) {}

  get readyState(): number {
    return this.socket.readyState;
  }

  send(data: string): void {
    this.socket.send(data);
  }

  close(code?: number, reason?: string): void {
    this.socket.close(code, reason);
  }

  addEventListener(
    type: "message" | "close" | "error",
    listener: (event: unknown) => void,
  ): void {
    if (type !== "message") {
      this.socket.addEventListener(type, listener as never);
      return;
    }
    const wrapped = (event: unknown) => {
      const messageEvent = event as MessageEvent;
      const bytes = rawByteLength(messageEvent.data);
      const now = Date.now();
      if (now - this.windowStartedAt >= 1_000) {
        this.windowStartedAt = now;
        this.messagesInWindow = 0;
      }
      this.messagesInWindow += 1;
      if (bytes > MAX_FRAME_BYTES) {
        this.socket.close(TLSyncErrorCloseEventCode, "PAYLOAD_TOO_LARGE");
        return;
      }
      if (this.messagesInWindow > MAX_MESSAGES_PER_SECOND) {
        this.socket.close(TLSyncErrorCloseEventCode, "RATE_LIMITED");
        return;
      }
      listener(messageEvent);
    };
    this.listenerWrappers.set(listener, wrapped);
    this.socket.addEventListener("message", wrapped as never);
  }

  removeEventListener(
    type: "message" | "close" | "error",
    listener: (event: unknown) => void,
  ): void {
    if (type !== "message") {
      this.socket.removeEventListener(type, listener as never);
      return;
    }
    const wrapped = this.listenerWrappers.get(listener);
    if (wrapped) {
      this.socket.removeEventListener("message", wrapped as never);
      this.listenerWrappers.delete(listener);
    }
  }
}

export function createGateServer(options: GateServerOptions = {}) {
  const port = options.port ?? DEFAULT_PORT;
  const allowedOrigin = options.allowedOrigin ?? DEFAULT_ALLOWED_ORIGIN;
  const dataDir = resolve(
    options.dataDir ?? "../../test-results/whiteboard-spike-gate-data",
  );
  const roomsDir = join(dataDir, "rooms");
  const artifactsDir = join(dataDir, "artifacts");
  mkdirSync(roomsDir, { recursive: true });
  mkdirSync(artifactsDir, { recursive: true });

  const grants = new OneTimeGrantAuthority(options.now);
  const metadataStore = new MetadataStore(join(dataDir, "control.sqlite"));
  const connectionQuota = new ConnectionQuota();
  const rooms = new Map<string, RoomHandle>();
  const sockets = new Set<WebSocket>();
  let shuttingDown = false;
  const webSocketServer: WebSocketServerType = new WebSocketServer({
    noServer: true,
    maxPayload: MAX_FRAME_BYTES,
    handleProtocols(protocols) {
      return protocols.has(PROTOCOL_NAME) ? PROTOCOL_NAME : false;
    },
  });

  const httpServer = createServer(async (request, response) => {
    setCorsHeaders(response, allowedOrigin);
    if (request.method === "OPTIONS") {
      response.writeHead(204).end();
      return;
    }

    try {
      await routeRequest(request, response);
    } catch (error) {
      sendJson(response, errorStatus(error), {
        error: safeErrorCode(error),
      });
    }
  });

  httpServer.on("upgrade", (request, socket, head) => {
    try {
      if (shuttingDown) {
        rejectUpgrade(socket, 503);
        return;
      }
      const origin = request.headers.origin;
      if (origin !== allowedOrigin) {
        rejectUpgrade(socket, 403);
        return;
      }
      const url = new URL(request.url ?? "/", `http://${request.headers.host}`);
      const match = /^\/connect\/([^/]+)$/.exec(url.pathname);
      const documentId = match?.[1] ? decodeURIComponent(match[1]) : "";
      if (!isBoundedId(documentId)) {
        rejectUpgrade(socket, 404);
        return;
      }
      const sessionId = url.searchParams.get("sessionId") ?? "";
      const storeId = url.searchParams.get("storeId") ?? "";
      if (!isSessionIdentifier(sessionId) || !isSessionIdentifier(storeId)) {
        rejectUpgrade(socket, 400);
        return;
      }
      const token = extractGrantProtocol(
        request.headers["sec-websocket-protocol"],
      );
      const claims = grants.exchange(token, {
        documentHash: hash(`document:${documentId}`),
        origin,
      });
      if (
        metadataStore.generation(claims.tenantHash, claims.documentHash) !==
          claims.generation ||
        metadataStore.isActorRevoked(
          claims.tenantHash,
          claims.documentHash,
          claims.actorHash,
        )
      ) {
        rejectUpgrade(socket, 401);
        return;
      }

      const sessionMetadata: GateMetadata = {
        tenantHash: claims.tenantHash,
        documentHash: claims.documentHash,
        actorHash: claims.actorHash,
        capability: claims.capability,
        generation: claims.generation,
      };
      const releaseQuota = connectionQuota.acquire(sessionMetadata);
      webSocketServer.handleUpgrade(request, socket, head, (webSocket) => {
        sockets.add(webSocket);
        webSocket.once("close", () => {
          sockets.delete(webSocket);
          releaseQuota();
        });
        const handle = getOrCreateRoom(
          claims.tenantHash,
          claims.documentHash,
          claims.generation,
        );
        handle.sessions.set(sessionId, sessionMetadata);
        webSocket.once("close", () => handle.sessions.delete(sessionId));
        handle.room.handleSocketConnect({
          sessionId,
          socket: new BudgetedSocket(webSocket),
          isReadonly: claims.capability === "view",
          objectAccess: "read",
          meta: sessionMetadata,
        });
      });
    } catch {
      rejectUpgrade(socket, 401);
    }
  });

  async function routeRequest(
    request: IncomingMessage,
    response: ServerResponse,
  ): Promise<void> {
    const url = new URL(request.url ?? "/", `http://${request.headers.host}`);
    if (request.method === "GET" && url.pathname === "/health") {
      sendJson(response, 200, { status: "ok", syncVersion: "5.3.1" });
      return;
    }
    if (request.method !== "POST") {
      sendJson(response, 404, { error: "not_found" });
      return;
    }
    if (request.headers.origin !== allowedOrigin) {
      throw new GateHttpError(403, "origin_denied");
    }

    if (url.pathname === "/gate/grants") {
      const body = await readJsonBody(request);
      const tenantId = readId(body, "tenantId");
      const documentId = readId(body, "documentId");
      const actorId = readId(body, "actorId");
      const requestedCapability = readCapability(body, "requestedCapability");
      const resolvedCapability = resolveFixtureCapability(tenantId, actorId);
      if (!capabilityIncludes(resolvedCapability, requestedCapability)) {
        throw new GateHttpError(403, "capability_escalation_denied");
      }

      const tenantHash = hash(`tenant:${tenantId}`);
      const documentHash = hash(`document:${documentId}`);
      const actorHash = hash(`actor:${actorId}`);
      if (metadataStore.isActorRevoked(tenantHash, documentHash, actorHash)) {
        throw new GateHttpError(403, "actor_revoked");
      }
      const generation = metadataStore.generation(tenantHash, documentHash);
      const expectedGeneration = readOptionalPositiveInteger(
        body,
        "expectedGeneration",
      );
      if (
        expectedGeneration !== undefined &&
        expectedGeneration !== generation
      ) {
        throw new GateHttpError(409, "stale_generation");
      }
      const issued = grants.issue({
        tenantHash,
        documentHash,
        actorHash,
        capability: requestedCapability,
        generation,
        origin: allowedOrigin,
      });
      sendJson(response, 201, {
        grant: issued.grant,
        expiresInSeconds: Math.floor((issued.expiresAt - Date.now()) / 1_000),
        capability: requestedCapability,
        generation,
      });
      return;
    }

    if (url.pathname === "/gate/status") {
      const binding = await readDocumentBinding(request);
      const generation = metadataStore.generation(
        binding.tenantHash,
        binding.documentHash,
      );
      const handle = getOrCreateRoom(
        binding.tenantHash,
        binding.documentHash,
        generation,
      );
      sendJson(response, 200, roomEvidence(handle));
      return;
    }

    if (url.pathname === "/gate/restart") {
      await readJsonBody(request);
      const closedRooms = rooms.size;
      closeRooms();
      sendJson(response, 200, { restarted: true, closedRooms });
      return;
    }

    if (url.pathname === "/gate/revoke-actor") {
      const body = await readJsonBody(request);
      const tenantId = readId(body, "tenantId");
      const documentId = readId(body, "documentId");
      const actorId = readId(body, "actorId");
      const tenantHash = hash(`tenant:${tenantId}`);
      const documentHash = hash(`document:${documentId}`);
      const actorHash = hash(`actor:${actorId}`);
      metadataStore.revokeActor(tenantHash, documentHash, actorHash);
      let closedSessions = 0;
      for (const handle of rooms.values()) {
        for (const [sessionId, session] of handle.sessions) {
          if (
            session.tenantHash === tenantHash &&
            session.documentHash === documentHash &&
            session.actorHash === actorHash
          ) {
            handle.room.closeSession(sessionId, "PERMISSION_DENIED");
            closedSessions += 1;
          }
        }
      }
      sendJson(response, 200, { revoked: true, closedSessions });
      return;
    }

    if (url.pathname === "/gate/snapshots") {
      const binding = await readDocumentBinding(request);
      const generation = metadataStore.generation(
        binding.tenantHash,
        binding.documentHash,
      );
      const handle = getOrCreateRoom(
        binding.tenantHash,
        binding.documentHash,
        generation,
      );
      const snapshot = handle.room.getCurrentSnapshot();
      const serializedSnapshot = JSON.stringify(snapshot);
      const snapshotBytes = Buffer.byteLength(serializedSnapshot);
      if (snapshotBytes > MAX_SNAPSHOT_BYTES) {
        throw new GateHttpError(413, "snapshot_too_large");
      }
      const artifactId = randomBytes(16).toString("hex");
      const envelope: SnapshotEnvelope = {
        envelopeVersion: 1,
        engine: "tldraw",
        syncVersion: "5.3.1",
        tenantBinding: binding.tenantHash,
        documentBinding: binding.documentHash,
        generation,
        recordCount: snapshot.documents.length,
        snapshotBytes,
        checksum: hash(serializedSnapshot),
        snapshot,
      };
      writeFileSync(artifactPath(artifactId), JSON.stringify(envelope), {
        encoding: "utf8",
        flag: "wx",
      });
      sendJson(response, 201, {
        artifactId,
        generation,
        recordCount: envelope.recordCount,
        snapshotBytes,
        checksum: envelope.checksum,
      });
      return;
    }

    if (url.pathname === "/gate/corrupt-artifact") {
      const body = await readJsonBody(request);
      const artifactId = readArtifactId(body);
      const envelope = readSnapshotEnvelope(artifactId);
      envelope.checksum = "0".repeat(64);
      writeFileSync(artifactPath(artifactId), JSON.stringify(envelope), "utf8");
      sendJson(response, 200, { corrupted: true, artifactId });
      return;
    }

    if (url.pathname === "/gate/restore") {
      const body = await readJsonBody(request);
      const tenantId = readId(body, "tenantId");
      const documentId = readId(body, "documentId");
      const artifactId = readArtifactId(body);
      const expectedGeneration = readPositiveInteger(
        body,
        "expectedGeneration",
      );
      const tenantHash = hash(`tenant:${tenantId}`);
      const documentHash = hash(`document:${documentId}`);
      const currentGeneration = metadataStore.generation(
        tenantHash,
        documentHash,
      );
      if (currentGeneration !== expectedGeneration) {
        throw new GateHttpError(409, "stale_generation");
      }
      const envelope = readSnapshotEnvelope(artifactId);
      validateSnapshotEnvelope(envelope, tenantHash, documentHash);

      const oldRoomKey = roomKey(tenantHash, documentHash, currentGeneration);
      closeRoom(oldRoomKey);
      const nextGeneration = metadataStore.incrementGeneration(
        tenantHash,
        documentHash,
      );
      const nextRoom = getOrCreateRoom(
        tenantHash,
        documentHash,
        nextGeneration,
      );
      nextRoom.room.loadSnapshot(envelope.snapshot);
      sendJson(response, 200, {
        restored: true,
        previousGeneration: currentGeneration,
        generation: nextGeneration,
        checksum: envelope.checksum,
      });
      return;
    }

    sendJson(response, 404, { error: "not_found" });
  }

  async function readDocumentBinding(request: IncomingMessage): Promise<{
    tenantHash: string;
    documentHash: string;
  }> {
    const body = await readJsonBody(request);
    const tenantId = readId(body, "tenantId");
    const documentId = readId(body, "documentId");
    return {
      tenantHash: hash(`tenant:${tenantId}`),
      documentHash: hash(`document:${documentId}`),
    };
  }

  function getOrCreateRoom(
    tenantHash: string,
    documentHash: string,
    generation: number,
  ): RoomHandle {
    const key = roomKey(tenantHash, documentHash, generation);
    const existing = rooms.get(key);
    if (existing && !existing.room.isClosed()) {
      return existing;
    }
    const database = new DatabaseSync(join(roomsDir, `${key}.sqlite`));
    const storage = new SQLiteSyncStorage<TLRecord>({
      sql: new NodeSqliteWrapper(database),
    });
    const sessions = new Map<string, GateMetadata>();
    const room = new TLSocketRoom<TLRecord, GateMetadata>({ storage });
    const handle: RoomHandle = {
      key,
      tenantHash,
      documentHash,
      generation,
      database,
      room,
      sessions,
    };
    rooms.set(key, handle);
    return handle;
  }

  function closeRoom(key: string): void {
    const handle = rooms.get(key);
    if (!handle) return;
    handle.room.close();
    handle.database.close();
    rooms.delete(key);
  }

  function closeRooms(): void {
    for (const key of [...rooms.keys()]) {
      closeRoom(key);
    }
  }

  function artifactPath(artifactId: string): string {
    if (!ARTIFACT_ID_PATTERN.test(artifactId)) {
      throw new GateHttpError(400, "invalid_artifact_id");
    }
    return join(artifactsDir, `${artifactId}.json`);
  }

  function readSnapshotEnvelope(artifactId: string): SnapshotEnvelope {
    const path = artifactPath(artifactId);
    if (statSync(path).size > MAX_SNAPSHOT_BYTES * 2) {
      throw new GateHttpError(413, "snapshot_envelope_too_large");
    }
    const candidate = JSON.parse(readFileSync(path, "utf8")) as unknown;
    if (!isSnapshotEnvelope(candidate)) {
      throw new GateHttpError(422, "snapshot_envelope_invalid");
    }
    return candidate;
  }

  function validateSnapshotEnvelope(
    envelope: SnapshotEnvelope,
    tenantHash: string,
    documentHash: string,
  ): void {
    if (
      envelope.tenantBinding !== tenantHash ||
      envelope.documentBinding !== documentHash
    ) {
      throw new GateHttpError(404, "snapshot_not_found");
    }
    const serializedSnapshot = JSON.stringify(envelope.snapshot);
    const snapshotBytes = Buffer.byteLength(serializedSnapshot);
    if (
      snapshotBytes !== envelope.snapshotBytes ||
      snapshotBytes > MAX_SNAPSHOT_BYTES ||
      hash(serializedSnapshot) !== envelope.checksum ||
      envelope.snapshot.documents.length !== envelope.recordCount
    ) {
      throw new GateHttpError(422, "snapshot_integrity_failed");
    }
  }

  function roomEvidence(handle: RoomHandle): Record<string, unknown> {
    const snapshot = handle.room.getCurrentSnapshot();
    const serialized = JSON.stringify(snapshot);
    return {
      generation: handle.generation,
      activeSessions: handle.sessions.size,
      recordCount: snapshot.documents.length,
      shapeCount: snapshot.documents.filter(
        (entry) => entry.state.typeName === "shape",
      ).length,
      checksum: hash(serialized),
    };
  }

  return {
    port,
    dataDir,
    async start(): Promise<void> {
      await new Promise<void>((resolveStart, rejectStart) => {
        httpServer.once("error", rejectStart);
        httpServer.listen(port, "127.0.0.1", () => {
          httpServer.off("error", rejectStart);
          resolveStart();
        });
      });
    },
    async stop(): Promise<void> {
      shuttingDown = true;
      for (const socket of sockets) {
        socket.terminate();
      }
      closeRooms();
      webSocketServer.close();
      httpServer.closeAllConnections();
      await new Promise<void>((resolveStop) =>
        httpServer.close(() => resolveStop()),
      );
      metadataStore.close();
    },
  };
}

class GateHttpError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(code);
  }
}

function resolveFixtureCapability(
  tenantId: string,
  actorId: string,
): GateCapability {
  if (!actorId.startsWith(`${tenantId}:`)) {
    throw new GateHttpError(404, "membership_not_found");
  }
  const persona = actorId.slice(tenantId.length + 1);
  if (persona.startsWith("teacher-")) return "present";
  if (persona.startsWith("editor-")) return "edit";
  if (persona.startsWith("viewer-") || persona.startsWith("load-")) {
    return "view";
  }
  throw new GateHttpError(404, "membership_not_found");
}

function capabilityIncludes(
  resolved: GateCapability,
  requested: GateCapability,
): boolean {
  const rank: Record<GateCapability, number> = {
    view: 0,
    edit: 1,
    present: 2,
  };
  return rank[resolved] >= rank[requested];
}

function roomKey(
  tenantHash: string,
  documentHash: string,
  generation: number,
): string {
  return hash(`room:v1:${tenantHash}:${documentHash}:${generation}`);
}

function hash(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function extractGrantProtocol(header: string | string[] | undefined): string {
  const values = Array.isArray(header) ? header : (header ?? "").split(",");
  const grantProtocol = values
    .map((entry) => entry.trim())
    .find((entry) => entry.startsWith(GRANT_PROTOCOL_PREFIX));
  if (!grantProtocol) {
    throw new Error("grant_missing");
  }
  const grant = grantProtocol.slice(GRANT_PROTOCOL_PREFIX.length);
  if (!/^[A-Za-z0-9_-]{40,64}$/.test(grant)) {
    throw new Error("grant_malformed");
  }
  return grant;
}

function isBoundedId(value: string): boolean {
  return ID_PATTERN.test(value);
}

function isSessionIdentifier(value: string): boolean {
  return value.length >= 1 && value.length <= 160 && !/[\r\n]/.test(value);
}

function rawByteLength(value: unknown): number {
  if (typeof value === "string") return Buffer.byteLength(value);
  if (Buffer.isBuffer(value)) return value.byteLength;
  if (value instanceof ArrayBuffer) return value.byteLength;
  if (ArrayBuffer.isView(value)) return value.byteLength;
  return MAX_FRAME_BYTES + 1;
}

function increment(map: Map<string, number>, key: string): void {
  map.set(key, (map.get(key) ?? 0) + 1);
}

function decrement(map: Map<string, number>, key: string): void {
  const next = (map.get(key) ?? 1) - 1;
  if (next <= 0) map.delete(key);
  else map.set(key, next);
}

async function readJsonBody(
  request: IncomingMessage,
): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  let bytes = 0;
  for await (const chunk of request) {
    const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    bytes += buffer.byteLength;
    if (bytes > MAX_HTTP_BODY_BYTES) {
      throw new GateHttpError(413, "request_too_large");
    }
    chunks.push(buffer);
  }
  try {
    const value = JSON.parse(Buffer.concat(chunks).toString("utf8")) as unknown;
    if (!isRecord(value)) throw new Error("not_object");
    return value;
  } catch {
    throw new GateHttpError(400, "invalid_json");
  }
}

function readId(body: Record<string, unknown>, key: string): string {
  const value = body[key];
  if (typeof value !== "string" || !isBoundedId(value)) {
    throw new GateHttpError(400, `invalid_${key}`);
  }
  return value;
}

function readCapability(
  body: Record<string, unknown>,
  key: string,
): GateCapability {
  const value = body[key];
  if (value !== "view" && value !== "edit" && value !== "present") {
    throw new GateHttpError(400, `invalid_${key}`);
  }
  return value;
}

function readPositiveInteger(
  body: Record<string, unknown>,
  key: string,
): number {
  const value = body[key];
  if (typeof value !== "number" || !Number.isInteger(value) || value <= 0) {
    throw new GateHttpError(400, `invalid_${key}`);
  }
  return value;
}

function readOptionalPositiveInteger(
  body: Record<string, unknown>,
  key: string,
): number | undefined {
  if (!(key in body)) return undefined;
  return readPositiveInteger(body, key);
}

function readArtifactId(body: Record<string, unknown>): string {
  const value = body.artifactId;
  if (typeof value !== "string" || !ARTIFACT_ID_PATTERN.test(value)) {
    throw new GateHttpError(400, "invalid_artifact_id");
  }
  return value;
}

function isSnapshotEnvelope(value: unknown): value is SnapshotEnvelope {
  if (!isRecord(value)) return false;
  return (
    value.envelopeVersion === 1 &&
    value.engine === "tldraw" &&
    value.syncVersion === "5.3.1" &&
    typeof value.tenantBinding === "string" &&
    typeof value.documentBinding === "string" &&
    typeof value.generation === "number" &&
    typeof value.recordCount === "number" &&
    typeof value.snapshotBytes === "number" &&
    typeof value.checksum === "string" &&
    isRecord(value.snapshot) &&
    Array.isArray(value.snapshot.documents)
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function setCorsHeaders(response: ServerResponse, allowedOrigin: string): void {
  response.setHeader("Access-Control-Allow-Origin", allowedOrigin);
  response.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
  response.setHeader("Access-Control-Allow-Headers", "Content-Type");
  response.setHeader("Vary", "Origin");
  response.setHeader("Cache-Control", "no-store");
  response.setHeader("Referrer-Policy", "no-referrer");
  response.setHeader("X-Content-Type-Options", "nosniff");
}

function sendJson(
  response: ServerResponse,
  status: number,
  body: Record<string, unknown>,
): void {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
  });
  response.end(JSON.stringify(body));
}

function rejectUpgrade(
  socket: { end: (data?: string | Uint8Array) => void },
  status: number,
): void {
  const reason =
    status === 403
      ? "Forbidden"
      : status === 404
        ? "Not Found"
        : status === 503
          ? "Service Unavailable"
          : "Unauthorized";
  socket.end(
    `HTTP/1.1 ${status} ${reason}\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`,
  );
}

function safeErrorCode(error: unknown): string {
  if (error instanceof GateHttpError) return error.code;
  if (
    error instanceof Error &&
    (error as NodeJS.ErrnoException).code === "ENOENT"
  ) {
    return "artifact_not_found";
  }
  return "internal_error";
}

function errorStatus(error: unknown): number {
  if (error instanceof GateHttpError) return error.status;
  if (
    error instanceof Error &&
    (error as NodeJS.ErrnoException).code === "ENOENT"
  ) {
    return 404;
  }
  return 500;
}

function parseArgument(name: string): string | undefined {
  const prefix = `--${name}=`;
  return process.argv
    .find((value) => value.startsWith(prefix))
    ?.slice(prefix.length);
}

async function runFromCommandLine(): Promise<void> {
  const parsedPort = Number(parseArgument("port") ?? DEFAULT_PORT);
  const server = createGateServer({
    port: Number.isInteger(parsedPort) ? parsedPort : DEFAULT_PORT,
    dataDir: parseArgument("data-dir"),
  });
  await server.start();
  process.stdout.write("P5 tldraw gate server ready\n");
  const stop = async () => {
    await server.stop();
    process.exit(0);
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
}

const invokedPath = process.argv[1]
  ? pathToFileURL(resolve(process.argv[1])).href
  : "";
if (import.meta.url === invokedPath) {
  await runFromCommandLine();
}
