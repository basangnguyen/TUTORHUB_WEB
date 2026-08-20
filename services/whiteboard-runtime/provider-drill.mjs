import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import { WebSocket as NodeWebSocket } from "ws";
import * as Y from "yjs";
import { validateGateF3Environment } from "../../scripts/require-p5-collab-01-gate-f3-confirm.mjs";
import { NeonCheckpointStore } from "./dist/checkpointStore.js";
import { B2PortableSnapshotStore } from "./dist/snapshotStore.js";

const DOCUMENT_NAME = "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1";
const SCOPE = {
  actorId: "gate-f3-teacher",
  capability: "edit",
  documentId: "gate-f3-document",
  generation: 1,
  providerDocumentName: DOCUMENT_NAME,
  sessionId: "gate-f3-session",
  tenantId: "gate-f3-tenant",
  writerFence: 1,
};

async function run() {
  const env = process.env;
  const validated = validateGateF3Environment(env);
  if (env.P5_F3_PROVIDER_DOCUMENT_NAME !== DOCUMENT_NAME) {
    throw new Error("gate_f3_document_name_mismatch");
  }
  const startedAt = Date.now();
  const readyResponse = await fetch(`${validated.runtimeUrl}/readyz`, {
    signal: AbortSignal.timeout(120_000),
  });
  if (!readyResponse.ok) throw new Error("gate_f3_runtime_not_ready");
  const coldStartMs = Date.now() - startedAt;

  const issued = await adminJson(validated.controlUrl, "/gate-f3/v1/grants", {
    body: JSON.stringify({ capability: "edit" }),
    method: "POST",
  });
  if (
    typeof issued.grant !== "string" ||
    issued.provider_document_name !== DOCUMENT_NAME
  ) {
    throw new Error("gate_f3_grant_invalid");
  }

  const marker = Math.floor(Date.now() / 1000);
  const document = new Y.Doc();
  const OriginWebSocket = class extends NodeWebSocket {
    constructor(address, protocols) {
      super(address, protocols, {
        headers: { Origin: validated.allowedOrigin },
      });
    }
  };
  const socket = new HocuspocusProviderWebsocket({
    WebSocketPolyfill: OriginWebSocket,
    autoConnect: false,
    delay: 100,
    factor: 1,
    jitter: false,
    maxAttempts: 2,
    maxDelay: 250,
    minDelay: 100,
    url: validated.runtimeUrl.replace(/^https:/, "wss:"),
  });
  let resolveSynced;
  let rejectSync;
  const synced = new Promise((resolve, reject) => {
    resolveSynced = resolve;
    rejectSync = reject;
  });
  const provider = new HocuspocusProvider({
    document,
    name: DOCUMENT_NAME,
    onAuthenticationFailed: () => rejectSync(new Error("gate_f3_auth_failed")),
    onSynced: () => resolveSynced(),
    token: issued.grant,
    websocketProvider: socket,
  });
  provider.attach();
  void socket.connect();
  await withTimeout(synced, 30_000, "gate_f3_sync_timeout");
  document.getMap("gate-f3").set("marker", marker);
  await new Promise((resolve) => setTimeout(resolve, 3_000));
  provider.destroy();
  socket.destroy();
  document.destroy();

  const checkpoints = new NeonCheckpointStore(
    validated.runtimeDatabase.toString(),
  );
  try {
    const checkpoint = await retry(
      () => checkpoints.load(SCOPE),
      (value) => value !== null,
      20_000,
    );
    if (!checkpoint) throw new Error("gate_f3_checkpoint_missing");
    const recovered = new Y.Doc();
    Y.applyUpdate(recovered, checkpoint.state);
    const recoveredMarker = recovered.getMap("gate-f3").get("marker");
    recovered.destroy();
    if (recoveredMarker !== marker) {
      throw new Error("gate_f3_checkpoint_semantic_mismatch");
    }
  } finally {
    await checkpoints.close();
  }

  const snapshots = new B2PortableSnapshotStore(env.B2_BUCKET, {
    applicationKey: env.B2_APPLICATION_KEY,
    endpoint: env.B2_ENDPOINT,
    keyId: env.B2_KEY_ID,
    region: env.B2_REGION,
  });
  await snapshots.probe();
  const portableBytes = new TextEncoder().encode(
    JSON.stringify({
      canonicalSchemaVersion: 1,
      engine: { name: "excalidraw", version: "0.18.1" },
      exportedAt: new Date().toISOString(),
      format: "tutorhub.excalidraw.portable-scene",
      formatVersion: 1,
      scene: { elements: [], files: {}, schemaVersion: 1 },
      semanticHash: "gate-f3-disposable-probe",
    }),
  );
  const artifact = await snapshots.put(portableBytes);
  if (
    artifact.bytes.byteLength !== portableBytes.byteLength ||
    !artifact.bytes.every((byte, index) => byte === portableBytes[index])
  ) {
    throw new Error("gate_f3_snapshot_round_trip_mismatch");
  }
  const restoredEnvelope = JSON.parse(new TextDecoder().decode(artifact.bytes));
  if (
    restoredEnvelope.format !== "tutorhub.excalidraw.portable-scene" ||
    restoredEnvelope.formatVersion !== 1 ||
    restoredEnvelope.engine?.name !== "excalidraw" ||
    restoredEnvelope.semanticHash !== "gate-f3-disposable-probe"
  ) {
    throw new Error("gate_f3_portable_restore_semantic_mismatch");
  }

  const runtimeMetrics = await retry(
    async () => {
      return fetch(`${validated.runtimeUrl}/metrics`, {
        headers: { authorization: `Bearer ${env.COLLAB_METRICS_TOKEN}` },
        signal: AbortSignal.timeout(5_000),
      }).then((response) => {
        if (!response.ok) throw new Error("gate_f3_metrics_unavailable");
        return response.text();
      });
    },
    (metrics) => cleanupZero(metrics) && dependencyUp(metrics, "snapshot"),
    20_000,
  );
  if (!cleanupZero(runtimeMetrics)) throw new Error("gate_f3_cleanup_not_zero");
  if (!dependencyUp(runtimeMetrics, "snapshot")) {
    throw new Error("gate_f3_snapshot_dependency_not_ready");
  }

  console.log(
    JSON.stringify({
      b2_read_after_write: true,
      checkpoint_recovery: true,
      cleanup_zero: true,
      cold_start_bucket: durationBucket(coldStartMs),
      hocuspocus_sync: true,
      outcome: "pass",
      portable_restore_round_trip: true,
      snapshot_dependency_up: true,
    }),
  );
}

async function adminJson(baseUrl, path, init) {
  const response = await fetch(`${baseUrl}${path}`, {
    ...init,
    headers: {
      authorization: `Bearer ${process.env.P5_F3_CONTROL_ADMIN_TOKEN}`,
      "content-type": "application/json",
    },
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error("gate_f3_control_admin_failed");
  return response.json();
}

function cleanupZero(metrics) {
  return (
    metrics.includes('collab_connections_current{capability="edit"} 0') &&
    metrics.includes('collab_connections_current{capability="present"} 0') &&
    metrics.includes('collab_connections_current{capability="view"} 0') &&
    metrics.includes("collab_documents_current 0") &&
    metrics.includes("collab_dirty_documents 0")
  );
}

function dependencyUp(metrics, dependency) {
  return metrics.includes(`collab_dependency_up{dependency="${dependency}"} 1`);
}

function durationBucket(durationMs) {
  if (durationMs < 5_000) return "lt_5s";
  if (durationMs < 30_000) return "lt_30s";
  if (durationMs < 120_000) return "lt_120s";
  return "gte_120s";
}

async function retry(operation, predicate, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastValue;
  while (Date.now() < deadline) {
    try {
      lastValue = await operation();
    } catch {
      lastValue = undefined;
    }
    if (predicate(lastValue)) return lastValue;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  return lastValue;
}

async function withTimeout(promise, timeoutMs, code) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(code)), timeoutMs);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

run().catch(() => {
  console.error(
    JSON.stringify({
      outcome: "fail",
      reason: "bounded_provider_gate_failure",
    }),
  );
  process.exitCode = 1;
});
