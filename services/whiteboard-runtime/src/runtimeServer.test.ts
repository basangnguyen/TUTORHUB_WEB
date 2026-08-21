import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
  WebSocketStatus,
} from "@hocuspocus/provider";
import { afterEach, describe, expect, it } from "vitest";
import { WebSocket as NodeWebSocket } from "ws";
import * as Y from "yjs";
import type { RuntimeConfig } from "./config.js";
import type {
  CheckpointStore,
  CollaborationScope,
  ControlPlane,
  PortableSnapshotStore,
  RuntimeAuthorityState,
  SafeLogger,
  StoredCheckpoint,
} from "./contracts.js";
import type { ProviderAuthorityGuard } from "./providerAuthorityGuard.js";
import {
  createCollaborationRuntime,
  type CollaborationRuntime,
} from "./runtimeServer.js";

const ORIGIN = "http://127.0.0.1:4180";
const DOCUMENT_NAME = "wb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const METRICS_TOKEN = "metrics-token-that-is-long-enough";

const scope: CollaborationScope = {
  authorityLease: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  actorId: "teacher-a",
  capability: "edit",
  documentId: "document-a",
  generation: 1,
  origin: ORIGIN,
  providerDocumentName: DOCUMENT_NAME,
  sessionId: "session-a",
  tenantId: "tenant-a",
  writerFence: 1,
};

describe("OCI collaboration runtime", () => {
  const runtimes: CollaborationRuntime[] = [];
  const clients: TestClient[] = [];

  afterEach(async () => {
    for (const client of clients.splice(0)) client.destroy();
    for (const runtime of runtimes.splice(0))
      await runtime.drain().catch(() => undefined);
  });

  it("serves bounded probes, authenticates Hocuspocus, flushes and recovers a checkpoint", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const first = createRuntime(checkpoints);
    runtimes.push(first.runtime);
    await first.runtime.start();
    expect(first.runtime.readiness()).toEqual({ ready: true, reason: "ready" });

    const baseUrl = httpUrl(first.runtime);
    await expect(
      fetch(`${baseUrl}/livez`).then((response) => response.status),
    ).resolves.toBe(200);
    await expect(
      fetch(`${baseUrl}/readyz`).then((response) => response.status),
    ).resolves.toBe(200);
    await expect(
      fetch(`${baseUrl}/metrics`).then((response) => response.status),
    ).resolves.toBe(401);
    const metrics = await fetch(`${baseUrl}/metrics`, {
      headers: { authorization: `Bearer ${METRICS_TOKEN}` },
    }).then((response) => response.text());
    expect(metrics).toContain(
      'collab_dependency_up{dependency="persistence"} 1',
    );
    expect(metrics).not.toContain(scope.tenantId);
    expect(metrics).not.toContain(scope.documentId);

    const firstClient = createClient(first.runtime);
    clients.push(firstClient);
    await firstClient.connected;
    firstClient.document.getMap("scene").set("revision", 7);
    await waitFor(() => checkpoints.storeCount > 0, 12_000);
    await first.runtime.drain();
    runtimes.splice(runtimes.indexOf(first.runtime), 1);
    expect(first.runtime.readiness()).toEqual({
      ready: false,
      reason: "dependency_unavailable",
    });

    firstClient.destroy();
    clients.splice(clients.indexOf(firstClient), 1);
    const second = createRuntime(checkpoints);
    runtimes.push(second.runtime);
    await second.runtime.start();
    const recoveredClient = createClient(second.runtime);
    clients.push(recoveredClient);
    await recoveredClient.synced;
    expect(recoveredClient.document.getMap("scene").get("revision")).toBe(7);
  }, 25_000);

  it("turns readiness red and closes writers when authority switches off", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const authority = new FakeControlPlane();
    const candidate = createRuntime(checkpoints, authority);
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const client = createClient(candidate.runtime);
    clients.push(client);
    await client.synced;

    authority.mode = "off";
    await waitFor(() => !candidate.runtime.readiness().ready, 2_500);
    await client.closed;
    client.destroy();
    clients.splice(clients.indexOf(client), 1);

    expect(candidate.runtime.readiness().reason).toBe("runtime_off");
    await expect(
      fetch(`${httpUrl(candidate.runtime)}/readyz`).then(
        (response) => response.status,
      ),
    ).resolves.toBe(503);
  }, 10_000);

  it("closes an exact connection when its authority lease is revoked", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const authority = new FakeControlPlane();
    const candidate = createRuntime(checkpoints, authority);
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const client = createClient(candidate.runtime);
    clients.push(client);
    await client.synced;

    authority.validLeases.clear();
    await client.closed;
    client.destroy();
    clients.splice(clients.indexOf(client), 1);

    expect(candidate.runtime.readiness()).toEqual({
      ready: true,
      reason: "ready",
    });
  }, 10_000);

  it("reports cleanup-zero after the last client disconnects and the document unloads", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const candidate = createRuntime(checkpoints);
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const client = createClient(candidate.runtime);
    clients.push(client);
    await client.synced;
    client.document.getMap("scene").set("revision", 1);
    await waitFor(() => checkpoints.storeCount > 0, 12_000);

    client.destroy();
    clients.splice(clients.indexOf(client), 1);
    await waitForMetrics(
      httpUrl(candidate.runtime),
      [
        'collab_connections_current{capability="edit"} 0',
        "collab_documents_current 0",
        "collab_dirty_documents 0",
      ],
      12_000,
    );
  }, 20_000);

  it("binds ephemeral awareness to the authenticated actor without checkpointing it", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const candidate = createRuntime(checkpoints);
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const sender = createClient(candidate.runtime);
    const observer = createClient(candidate.runtime);
    clients.push(sender, observer);
    await Promise.all([sender.synced, observer.synced]);

    const storesBeforeAwareness = checkpoints.storeCount;
    sender.setAwarenessField("actorId", "forged-actor");
    sender.setAwarenessField("user", {
      displayName: "Teacher",
      id: "forged-user",
    });
    await waitFor(
      () =>
        observer.awarenessStates().some((state) => {
          const marker = state.tutorhub;
          return (
            typeof marker === "object" &&
            marker !== null &&
            (marker as { actorId?: unknown }).actorId === scope.actorId
          );
        }),
      2_000,
    );
    const remote = observer
      .awarenessStates()
      .find((state) => "tutorhub" in state);
    expect(remote).not.toHaveProperty("actorId");
    expect(remote?.user).toEqual({
      displayName: "Teacher",
      id: scope.actorId,
    });
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(checkpoints.storeCount).toBe(storesBeforeAwareness);

    sender.destroy();
    clients.splice(clients.indexOf(sender), 1);
    await waitFor(() => observer.awarenessStates().length === 1, 2_000);
  }, 10_000);

  it("rejects a connection above the per-tenant quota and releases the slot", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const candidate = createRuntime(checkpoints, new FakeControlPlane(), {
      maxConnectionsPerTenant: 1,
    });
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const first = createClient(candidate.runtime);
    clients.push(first);
    await first.synced;

    const denied = createClient(candidate.runtime);
    clients.push(denied);
    await waitFor(() => denied.authenticationFailures.length > 0, 2_000);
    denied.destroy();
    clients.splice(clients.indexOf(denied), 1);
    await waitForMetrics(
      httpUrl(candidate.runtime),
      ['collab_policy_rejection_total{reason="connection_quota"} 1'],
      2_000,
    );

    first.destroy();
    clients.splice(clients.indexOf(first), 1);
    const replacement = createClient(candidate.runtime);
    clients.push(replacement);
    await replacement.synced;
  }, 10_000);

  it("does not admit an authentication that finishes after drain starts", async () => {
    const checkpoints = new MemoryCheckpointStore();
    const authority = new DeferredExchangeControlPlane();
    const candidate = createRuntime(checkpoints, authority);
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const client = createClient(candidate.runtime);
    clients.push(client);
    await authority.exchangeStarted;

    const draining = candidate.runtime.drain();
    authority.resolveExchange();
    await draining;
    runtimes.splice(runtimes.indexOf(candidate.runtime), 1);
    await waitFor(() => client.authenticationFailures.length > 0, 2_000);
    expect(candidate.logger.codes).not.toContain("connection_ok");
  }, 10_000);

  it("does not commit a session whose document load finishes after authority is disabled", async () => {
    const checkpoints = new DeferredLoadCheckpointStore();
    const authority = new FakeControlPlane();
    const candidate = createRuntime(checkpoints, authority);
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const client = createClient(candidate.runtime);
    clients.push(client);
    await checkpoints.loadStarted;

    authority.mode = "off";
    await waitFor(() => !candidate.runtime.readiness().ready, 2_500);
    checkpoints.resolveLoad();
    await client.closed;

    expect(candidate.logger.codes).not.toContain("connection_ok");
    await waitForMetrics(
      httpUrl(candidate.runtime),
      [
        'collab_connection_total{outcome="accepted"} 0',
        'collab_connections_current{capability="edit"} 0',
      ],
      2_000,
    );
  }, 10_000);

  it("fails the HTTP readiness probe immediately when local authority is stale", async () => {
    const guard = new FakeAuthorityGuard();
    const candidate = createRuntime(
      new MemoryCheckpointStore(),
      new FakeControlPlane(),
      {},
      guard,
    );
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    await expect(
      fetch(`${httpUrl(candidate.runtime)}/readyz`).then(
        (response) => response.status,
      ),
    ).resolves.toBe(200);

    guard.stale = true;

    await expect(
      fetch(`${httpUrl(candidate.runtime)}/readyz`).then(
        (response) => response.status,
      ),
    ).resolves.toBe(503);
    expect(candidate.runtime.readiness()).toEqual({
      ready: false,
      reason: "dependency_unavailable",
    });
  });

  it("prevents two sockets from owning one awareness client id", async () => {
    const candidate = createRuntime(new MemoryCheckpointStore());
    runtimes.push(candidate.runtime);
    await candidate.runtime.start();
    const owner = createClient(candidate.runtime, 4_242);
    const observer = createClient(candidate.runtime, 4_343);
    clients.push(owner, observer);
    await Promise.all([owner.synced, observer.synced]);
    owner.setAwarenessField("user", { displayName: "Owner" });
    await waitFor(
      () =>
        observer
          .awarenessStates()
          .some((state) => awarenessDisplayName(state) === "Owner"),
      2_000,
    );

    const intruder = createClient(candidate.runtime, 4_242);
    clients.push(intruder);
    await intruder.closed;
    owner.setAwarenessField("user", { displayName: "Owner Updated" });
    await waitFor(
      () =>
        observer
          .awarenessStates()
          .some((state) => awarenessDisplayName(state) === "Owner Updated"),
      2_000,
    );

    owner.destroy();
    clients.splice(clients.indexOf(owner), 1);
    await waitFor(
      () =>
        !observer
          .awarenessStates()
          .some((state) => awarenessDisplayName(state) === "Owner Updated"),
      2_000,
    );
    const replacement = createClient(candidate.runtime, 4_242);
    clients.push(replacement);
    await replacement.synced;
  }, 15_000);
});

class MemoryCheckpointStore implements CheckpointStore {
  checkpoint: StoredCheckpoint | null = null;
  storeCount = 0;

  async close(): Promise<void> {}
  async probe(): Promise<void> {}
  async load(): Promise<StoredCheckpoint | null> {
    return this.checkpoint;
  }
  async store(_scope: CollaborationScope, state: Uint8Array): Promise<number> {
    this.storeCount += 1;
    this.checkpoint = {
      checksum: "0".repeat(64),
      state: new Uint8Array(state),
      watermark: this.storeCount,
    };
    return this.storeCount;
  }
}

class DeferredLoadCheckpointStore extends MemoryCheckpointStore {
  private resolveStarted!: () => void;
  private resolvePending!: (checkpoint: StoredCheckpoint | null) => void;
  readonly loadStarted = new Promise<void>(
    (resolve) => (this.resolveStarted = resolve),
  );
  private readonly pending = new Promise<StoredCheckpoint | null>(
    (resolve) => (this.resolvePending = resolve),
  );

  override async load(): Promise<StoredCheckpoint | null> {
    this.resolveStarted();
    return this.pending;
  }

  resolveLoad(): void {
    this.resolvePending(null);
  }
}

class FakeControlPlane implements ControlPlane {
  mode: RuntimeAuthorityState["mode"] = "enabled";
  readonly validLeases = new Set([scope.authorityLease]);

  async exchangeGrant(input: {
    documentName: string;
  }): Promise<CollaborationScope> {
    if (input.documentName !== DOCUMENT_NAME) throw new Error("denied");
    return scope;
  }
  async probe(): Promise<RuntimeAuthorityState> {
    return { mode: this.mode };
  }
  async validateScopes(scopes: CollaborationScope[]): Promise<Set<string>> {
    return new Set(
      scopes
        .map((candidate) => candidate.authorityLease)
        .filter((lease) => this.validLeases.has(lease)),
    );
  }
}

class DeferredExchangeControlPlane extends FakeControlPlane {
  private resolveStarted!: () => void;
  private resolveScope!: (value: CollaborationScope) => void;
  readonly exchangeStarted = new Promise<void>(
    (resolve) => (this.resolveStarted = resolve),
  );
  private readonly scopePromise = new Promise<CollaborationScope>(
    (resolve) => (this.resolveScope = resolve),
  );

  override async exchangeGrant(): Promise<CollaborationScope> {
    this.resolveStarted();
    return this.scopePromise;
  }

  resolveExchange(): void {
    this.resolveScope(scope);
  }
}

class FakeSnapshots implements PortableSnapshotStore {
  async probe(): Promise<void> {}
  async put(): Promise<never> {
    throw new Error("not_used");
  }
}

class CollectingLogger implements SafeLogger {
  readonly codes: string[] = [];
  event(code: string): void {
    this.codes.push(code);
  }
}

function createRuntime(
  checkpoints: CheckpointStore,
  controlPlane: ControlPlane = new FakeControlPlane(),
  configOverrides: Partial<RuntimeConfig> = {},
  authorityGuard: ProviderAuthorityGuard = new FakeAuthorityGuard(),
): { logger: CollectingLogger; runtime: CollaborationRuntime } {
  const logger = new CollectingLogger();
  return {
    logger,
    runtime: createCollaborationRuntime(runtimeConfig(configOverrides), {
      authorityGuard,
      checkpoints,
      controlPlane,
      logger,
      snapshots: new FakeSnapshots(),
    }),
  };
}

function runtimeConfig(overrides: Partial<RuntimeConfig> = {}): RuntimeConfig {
  return {
    address: "127.0.0.1",
    allowedOrigins: new Set([ORIGIN]),
    b2: {
      applicationKey: "unused",
      bucket: "unused",
      endpoint: "https://unused.example",
      keyId: "unused",
      region: "unused",
    },
    buildId: "test-build",
    controlPlaneToken: "unused",
    controlPlaneUrl: "http://127.0.0.1",
    databaseUrl: "postgresql://unused:unused@127.0.0.1/unused",
    drainTimeoutMs: 5_000,
    maxAwarenessBytes: 16 * 1024,
    maxAwarenessDepth: 8,
    maxAwarenessMessagesPerSecond: 30,
    maxAwarenessStates: 1,
    maxConnections: 10,
    maxConnectionsPerActor: 3,
    maxConnectionsPerDocument: 10,
    maxConnectionsPerTenant: 10,
    maxDocumentBytes: 10 * 1024 * 1024,
    maxFrameBytes: 1024 * 1024,
    maxIngressBytesPerSecond: 4 * 1024 * 1024,
    maxMessagesPerSecond: 60,
    maxReconnectAttempts: 8,
    maxUpdateBytes: 768 * 1024,
    metricsToken: METRICS_TOKEN,
    port: 0,
    probeTimeoutMs: 250,
    profile: "FREE_PRIVATE_ALPHA",
    ...overrides,
  };
}

class FakeAuthorityGuard implements ProviderAuthorityGuard {
  acquired = false;
  stale = false;

  async acquire(): Promise<void> {
    this.acquired = true;
  }
  assertHeld(): void {
    if (!this.acquired || this.stale) throw new Error("not_acquired");
  }
  async probe(): Promise<void> {
    if (!this.acquired || this.stale) throw new Error("not_acquired");
  }
  async release(): Promise<void> {
    this.acquired = false;
  }
}

interface TestClient {
  awarenessStates(): Record<string, unknown>[];
  authenticationFailures: string[];
  closed: Promise<void>;
  connected: Promise<void>;
  destroy(): void;
  document: Y.Doc;
  setAwarenessField(key: string, value: unknown): void;
  synced: Promise<void>;
}

function createClient(
  runtime: CollaborationRuntime,
  clientId?: number,
): TestClient {
  const document = new Y.Doc();
  if (clientId !== undefined) document.clientID = clientId;
  const authenticationFailures: string[] = [];
  let resolveClosed!: () => void;
  let resolveConnected!: () => void;
  let resolveSynced!: () => void;
  const closed = new Promise<void>((resolve) => (resolveClosed = resolve));
  const connected = new Promise<void>(
    (resolve) => (resolveConnected = resolve),
  );
  const synced = new Promise<void>((resolve) => (resolveSynced = resolve));
  const OriginWebSocket = class extends NodeWebSocket {
    constructor(address: string | URL, protocols?: string | string[]) {
      super(address, protocols, { headers: { Origin: ORIGIN } });
    }
  };
  const socket = new HocuspocusProviderWebsocket({
    WebSocketPolyfill: OriginWebSocket,
    autoConnect: false,
    delay: 25,
    factor: 1,
    jitter: false,
    maxAttempts: 1,
    maxDelay: 25,
    minDelay: 25,
    url: wsUrl(runtime),
  });
  const provider = new HocuspocusProvider({
    document,
    name: DOCUMENT_NAME,
    onAuthenticationFailed: ({ reason }) => authenticationFailures.push(reason),
    onClose: () => resolveClosed(),
    onStatus: ({ status }) => {
      if (status === WebSocketStatus.Connected) resolveConnected();
    },
    onSynced: () => resolveSynced(),
    token: "one-time-grant-that-is-long-enough",
    websocketProvider: socket,
  });
  provider.attach();
  void socket.connect();
  return {
    awarenessStates: () =>
      [...(provider.awareness?.getStates().values() ?? [])] as Record<
        string,
        unknown
      >[],
    authenticationFailures,
    closed,
    connected,
    destroy: () => {
      provider.destroy();
      socket.destroy();
      document.destroy();
    },
    document,
    setAwarenessField: (key, value) => provider.setAwarenessField(key, value),
    synced,
  };
}

function httpUrl(runtime: CollaborationRuntime): string {
  return `http://127.0.0.1:${runtime.address().port}`;
}

function wsUrl(runtime: CollaborationRuntime): string {
  return `ws://127.0.0.1:${runtime.address().port}`;
}

function awarenessDisplayName(state: Record<string, unknown>): unknown {
  const user = state.user;
  return typeof user === "object" && user !== null && "displayName" in user
    ? user.displayName
    : undefined;
}

async function waitFor(
  predicate: () => boolean,
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("wait_timeout");
}

async function waitForMetrics(
  baseUrl: string,
  expected: string[],
  timeoutMs: number,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const metrics = await fetch(`${baseUrl}/metrics`, {
      headers: { authorization: `Bearer ${METRICS_TOKEN}` },
    }).then((response) => response.text());
    if (expected.every((line) => metrics.includes(line))) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("metrics_wait_timeout");
}
