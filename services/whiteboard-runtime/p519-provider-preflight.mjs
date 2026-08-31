import { createHash } from "node:crypto";
import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import { WebSocket as NodeWebSocket } from "ws";
import * as Y from "yjs";
import { pathToFileURL } from "node:url";

import {
  P519_PROVIDER_DOCUMENTS,
  P519_PROVIDER_FIXTURE,
} from "../../scripts/p519-provider-fixture.mjs";

export const P519_DOCUMENTS = P519_PROVIDER_DOCUMENTS;
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY";
const CLIENTS_PER_DOCUMENT = 5;
const TOTAL_CLIENTS = P519_DOCUMENTS.length * CLIENTS_PER_DOCUMENT;
const SHAPES_PER_DOCUMENT = 500;

function requiredSecret(value, code) {
  if (typeof value !== "string" || value.length < 20) throw new Error(code);
  return value;
}

function requiredHttpsUrl(value, code) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(code);
  }
  if (url.protocol !== "https:" || url.username || url.password) {
    throw new Error(code);
  }
  return url.toString().replace(/\/$/u, "");
}

export function validateP519ProviderEnvironment(environment = process.env) {
  if (environment.P5_COLLAB_19_DISPOSABLE_CONFIRM !== EXACT_CONFIRMATION) {
    throw new Error("p519_disposable_confirmation_required");
  }
  const controlUrl = requiredHttpsUrl(
    environment.P5_COLLAB_19_CONTROL_URL,
    "p519_control_url_invalid",
  );
  const runtimeUrl = requiredHttpsUrl(
    environment.P5_COLLAB_19_RUNTIME_URL,
    "p519_runtime_url_invalid",
  );
  if (controlUrl === runtimeUrl)
    throw new Error("p519_services_must_be_isolated");
  const allowedOrigin = requiredHttpsUrl(
    environment.P5_COLLAB_19_ALLOWED_ORIGIN,
    "p519_allowed_origin_invalid",
  );
  const documents = String(
    environment.P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES ?? "",
  )
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  if (
    documents.length !== P519_DOCUMENTS.length ||
    documents.some((value, index) => value !== P519_DOCUMENTS[index])
  ) {
    throw new Error("p519_documents_must_match_contract");
  }
  return {
    adminToken: requiredSecret(
      environment.P5_COLLAB_19_CONTROL_ADMIN_TOKEN,
      "p519_admin_token_invalid",
    ),
    allowedOrigin,
    controlUrl,
    documents,
    metricsToken: requiredSecret(
      environment.COLLAB_METRICS_TOKEN,
      "p519_metrics_token_invalid",
    ),
    runtimeUrl,
  };
}

export function metricValue(metrics, name, labels = "") {
  const expected = labels ? `${name}{${labels}}` : name;
  for (const line of metrics.split(/\r?\n/u)) {
    if (!line.startsWith(`${expected} `)) continue;
    const value = Number(line.slice(expected.length + 1));
    return Number.isFinite(value) ? value : undefined;
  }
  return undefined;
}

export function cleanupZero(metrics) {
  return (
    metricValue(metrics, "collab_connections_current", 'capability="edit"') ===
      0 &&
    metricValue(
      metrics,
      "collab_connections_current",
      'capability="present"',
    ) === 0 &&
    metricValue(metrics, "collab_connections_current", 'capability="view"') ===
      0 &&
    metricValue(metrics, "collab_documents_current") === 0 &&
    metricValue(metrics, "collab_dirty_documents") === 0
  );
}

export function dependencyUp(metrics, dependency) {
  return (
    metricValue(
      metrics,
      "collab_dependency_up",
      `dependency="${dependency}"`,
    ) === 1
  );
}

export function durationBucket(durationMs) {
  if (durationMs < 1_000) return "lt_1s";
  if (durationMs < 2_500) return "lt_2_5s";
  if (durationMs < 7_500) return "lt_7_5s";
  if (durationMs < 30_000) return "lt_30s";
  if (durationMs < 120_000) return "lt_120s";
  return "gte_120s";
}

function stableSceneHash(document) {
  const entries = [...document.getMap("scene").entries()].sort(
    ([left], [right]) => left.localeCompare(right),
  );
  return createHash("sha256").update(JSON.stringify(entries)).digest("hex");
}

function seedScene(document, documentIndex) {
  const scene = document.getMap("scene");
  document.transact(() => {
    for (let index = 0; index < SHAPES_PER_DOCUMENT; index += 1) {
      const id = `shape-${String(index + 1).padStart(4, "0")}`;
      scene.set(id, {
        height: 70,
        id,
        label: `Synthetic lesson shape ${documentIndex + 1}-${index + 1}`,
        type: "rectangle",
        width: 110,
        x: (index % 20) * 130,
        y: Math.floor(index / 20) * 90,
      });
    }
  }, "p519-provider-preflight");
}

async function fetchJson(url, token, init = {}) {
  const response = await fetch(url, {
    ...init,
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
      ...(init.headers ?? {}),
    },
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok) throw new Error("p519_control_request_failed");
  return response.json();
}

async function readMetrics(options) {
  const response = await fetch(`${options.runtimeUrl}/metrics`, {
    headers: { authorization: `Bearer ${options.metricsToken}` },
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok) throw new Error("p519_metrics_unavailable");
  return response.text();
}

function syntheticActorId(participantIndex) {
  if (
    !Number.isSafeInteger(participantIndex) ||
    participantIndex < 0 ||
    participantIndex >= TOTAL_CLIENTS
  ) {
    throw new Error("p519_participant_index_invalid");
  }
  return `51900000-0000-4000-8000-${String(participantIndex + 1).padStart(12, "0")}`;
}

export function buildP519GrantRequest(documentName, participantIndex = 0) {
  const document = P519_PROVIDER_FIXTURE.documents.find(
    (candidate) => candidate.providerDocumentName === documentName,
  );
  if (!document) throw new Error("p519_document_fixture_missing");
  return {
    actor_id: syntheticActorId(participantIndex),
    capability: "edit",
    document_id: document.documentId,
    provider_document_name: document.providerDocumentName,
    session_id: document.sessionId,
    tenant_id: P519_PROVIDER_FIXTURE.tenantId,
  };
}

async function issueGrant(options, documentName, participantIndex) {
  const result = await fetchJson(
    `${options.controlUrl}/p519/v1/grants`,
    options.adminToken,
    {
      body: JSON.stringify(
        buildP519GrantRequest(documentName, participantIndex),
      ),
      method: "POST",
    },
  );
  if (typeof result.grant !== "string" || result.grant.length < 20) {
    throw new Error("p519_grant_invalid");
  }
  return result.grant;
}

function createProvider(options, documentName, token) {
  const document = new Y.Doc();
  const OriginWebSocket = class extends NodeWebSocket {
    constructor(address, protocols) {
      super(address, protocols, {
        headers: { Origin: options.allowedOrigin },
      });
    }
  };
  const socket = new HocuspocusProviderWebsocket({
    WebSocketPolyfill: OriginWebSocket,
    autoConnect: false,
    delay: 100,
    factor: 1,
    jitter: false,
    maxAttempts: 3,
    maxDelay: 500,
    minDelay: 100,
    url: options.runtimeUrl.replace(/^https:/u, "wss:"),
  });
  let resolveSynced;
  let rejectSynced;
  const synced = new Promise((resolve, reject) => {
    resolveSynced = resolve;
    rejectSynced = reject;
  });
  const provider = new HocuspocusProvider({
    document,
    name: documentName,
    onAuthenticationFailed: () =>
      rejectSynced(new Error("p519_provider_authentication_failed")),
    onSynced: () => resolveSynced(),
    token,
    websocketProvider: socket,
  });
  const startedAt = Date.now();
  provider.attach();
  void socket.connect();
  return {
    document,
    documentName,
    joinStartedAt: startedAt,
    provider,
    socket,
    synced,
  };
}

function destroyClients(clients) {
  for (const client of clients) {
    client.provider.destroy();
    client.socket.destroy();
    client.document.destroy();
  }
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

async function ensureReady(options) {
  const startedAt = Date.now();
  const response = await fetch(`${options.runtimeUrl}/readyz`, {
    signal: AbortSignal.timeout(120_000),
  });
  if (!response.ok) throw new Error("p519_runtime_not_ready");
  const status = await fetchJson(
    `${options.controlUrl}/p519/v1/status`,
    options.adminToken,
  );
  if (
    status.authority_available !== true ||
    status.documents !== 2 ||
    status.mode !== "enabled"
  ) {
    throw new Error("p519_control_state_invalid");
  }
  return Date.now() - startedAt;
}

export async function runP519ProviderPreflight(environment = process.env) {
  const options = validateP519ProviderEnvironment(environment);
  const coldStartMs = await ensureReady(options);
  const clients = [];
  const joinLatencies = [];
  let convergenceMs;
  try {
    for (const [documentIndex, documentName] of options.documents.entries()) {
      const grants = await Promise.all(
        Array.from({ length: CLIENTS_PER_DOCUMENT }, (_, clientIndex) =>
          issueGrant(
            options,
            documentName,
            documentIndex * CLIENTS_PER_DOCUMENT + clientIndex,
          ),
        ),
      );
      for (const grant of grants) {
        clients.push(createProvider(options, documentName, grant));
      }
    }
    await Promise.all(
      clients.map(async (client) => {
        await withTimeout(client.synced, 45_000, "p519_provider_sync_timeout");
        joinLatencies.push(Date.now() - client.joinStartedAt);
      }),
    );

    const activeMetrics = await retry(
      () => readMetrics(options),
      (metrics) =>
        typeof metrics === "string" &&
        metricValue(
          metrics,
          "collab_connections_current",
          'capability="edit"',
        ) === 10 &&
        metricValue(metrics, "collab_documents_current") === 2,
      30_000,
    );
    if (
      typeof activeMetrics !== "string" ||
      metricValue(
        activeMetrics,
        "collab_connections_current",
        'capability="edit"',
      ) !== 10 ||
      metricValue(activeMetrics, "collab_documents_current") !== 2
    ) {
      throw new Error("p519_active_metrics_mismatch");
    }

    const convergenceStartedAt = Date.now();
    for (const [documentIndex, documentName] of options.documents.entries()) {
      const documentClients = clients.filter(
        (client) => client.documentName === documentName,
      );
      seedScene(documentClients[0].document, documentIndex);
    }
    const converged = await retry(
      () => {
        return options.documents.every((documentName) => {
          const documents = clients
            .filter((client) => client.documentName === documentName)
            .map((client) => client.document);
          const hashes = documents.map(stableSceneHash);
          return (
            documents.every(
              (document) =>
                document.getMap("scene").size === SHAPES_PER_DOCUMENT,
            ) && hashes.every((hash) => hash === hashes[0])
          );
        });
      },
      (value) => value === true,
      30_000,
    );
    convergenceMs = Date.now() - convergenceStartedAt;
    if (!converged) throw new Error("p519_scene_convergence_failed");
  } finally {
    destroyClients(clients);
  }

  const cleanupMetrics = await retry(
    () => readMetrics(options),
    (metrics) =>
      typeof metrics === "string" &&
      cleanupZero(metrics) &&
      ["control_plane", "persistence", "snapshot"].every((dependency) =>
        dependencyUp(metrics, dependency),
      ),
    60_000,
  );
  if (
    typeof cleanupMetrics !== "string" ||
    !cleanupZero(cleanupMetrics) ||
    !["control_plane", "persistence", "snapshot"].every((dependency) =>
      dependencyUp(cleanupMetrics, dependency),
    )
  ) {
    throw new Error("p519_cleanup_or_dependency_gate_failed");
  }

  return {
    cleanup_zero: true,
    cold_start_bucket: durationBucket(coldStartMs),
    connections: clients.length,
    convergence_bucket: durationBucket(convergenceMs),
    documents: options.documents.length,
    join_max_bucket: durationBucket(Math.max(...joinLatencies)),
    outcome: "pass",
    provider_observed: true,
    shapes_per_document: SHAPES_PER_DOCUMENT,
  };
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  runP519ProviderPreflight()
    .then((result) => process.stdout.write(`${JSON.stringify(result)}\n`))
    .catch(() => {
      process.stderr.write(
        `${JSON.stringify({ outcome: "fail", reason: "bounded_p519_provider_preflight_failure" })}\n`,
      );
      process.exitCode = 1;
    });
}
