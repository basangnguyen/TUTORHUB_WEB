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
import {
  createCollaborationRuntime,
  type CollaborationRuntime,
} from "./runtimeServer.js";

const ORIGIN = "http://127.0.0.1:4180";
const DOCUMENT_NAME = "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1";
const METRICS_TOKEN = "metrics-token-that-is-long-enough";

const scope: CollaborationScope = {
  actorId: "teacher-a",
  capability: "edit",
  documentId: "document-a",
  generation: 1,
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

    expect(candidate.runtime.readiness().reason).toBe("dependency_unavailable");
    await expect(
      fetch(`${httpUrl(candidate.runtime)}/readyz`).then(
        (response) => response.status,
      ),
    ).resolves.toBe(503);
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

class FakeControlPlane implements ControlPlane {
  mode: RuntimeAuthorityState["mode"] = "enabled";

  async exchangeGrant(input: {
    documentName: string;
  }): Promise<CollaborationScope> {
    if (input.documentName !== DOCUMENT_NAME) throw new Error("denied");
    return scope;
  }
  async probe(): Promise<RuntimeAuthorityState> {
    return { mode: this.mode };
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
): { logger: CollectingLogger; runtime: CollaborationRuntime } {
  const logger = new CollectingLogger();
  return {
    logger,
    runtime: createCollaborationRuntime(runtimeConfig(), {
      checkpoints,
      controlPlane,
      logger,
      snapshots: new FakeSnapshots(),
    }),
  };
}

function runtimeConfig(): RuntimeConfig {
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
    maxConnections: 10,
    maxConnectionsPerActor: 3,
    maxConnectionsPerDocument: 10,
    maxFrameBytes: 512 * 1024,
    maxMessagesPerSecond: 60,
    metricsToken: METRICS_TOKEN,
    port: 0,
    probeTimeoutMs: 250,
    profile: "FREE_PRIVATE_ALPHA",
  };
}

interface TestClient {
  closed: Promise<void>;
  connected: Promise<void>;
  destroy(): void;
  document: Y.Doc;
  synced: Promise<void>;
}

function createClient(runtime: CollaborationRuntime): TestClient {
  const document = new Y.Doc();
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
    closed,
    connected,
    destroy: () => {
      provider.destroy();
      socket.destroy();
      document.destroy();
    },
    document,
    synced,
  };
}

function httpUrl(runtime: CollaborationRuntime): string {
  return `http://127.0.0.1:${runtime.address().port}`;
}

function wsUrl(runtime: CollaborationRuntime): string {
  return `ws://127.0.0.1:${runtime.address().port}`;
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
