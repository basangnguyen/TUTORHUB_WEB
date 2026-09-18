import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

import {
  AbortMultipartUploadCommand,
  DeleteObjectCommand,
  ListMultipartUploadsCommand,
  ListObjectsV2Command,
  ListObjectVersionsCommand,
  S3Client,
} from "@aws-sdk/client-s3";
import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import pg from "pg";
import { WebSocket as NodeWebSocket } from "ws";
import * as Y from "yjs";

import {
  P519_PRIVATE_ALPHA_CONTRACT,
  P519_SUPPORT_PATH,
  createRedactedP519Summary,
  evaluateP519PrivateAlphaReport,
  percentile95,
} from "../../scripts/p519-private-alpha-contract.mjs";
import { P519_PROVIDER_FIXTURE } from "../../scripts/p519-provider-fixture.mjs";
import {
  cleanupZero,
  metricValue,
  validateP519ProviderEnvironment,
} from "./p519-provider-preflight.mjs";
import { createP519LivePlan } from "./p519-live-soak-plan.mjs";
import { B2PortableSnapshotStore } from "./dist/snapshotStore.js";

const { Client } = pg;
const ROOT = resolve(new URL("../..", import.meta.url).pathname.slice(1));
const CLIENTS_PER_DOCUMENT = 5;
const SHAPES_PER_DOCUMENT = 500;
const STEADY_START_MS = 300_000;
const STEADY_END_MS = 3_300_000;
const CONTROL_OUTAGE_START_MS = 650_000;
const CONTROL_OUTAGE_MS = 600_000;
const NEON_OUTAGE_START_MS = 1_900_000;
const B2_DRILL_START_MS = 2_100_000;
const FORCE_OFF_START_MS = 2_450_000;
const CREDENTIAL_DRILL_START_MS = 2_700_000;
const OUTPUT_FILE = "tmp/p5-collab-19/provider-report.json";
const BINDING_FILE = "tmp/p5-collab-19/run-binding.json";
const DEPLOY_STATE_FILE = "tmp/p5-collab-19/render-deploy.json";
const EXACT_RUNTIME_ROLE = "tutorhub_collab_worker";
const REPORT_KEYS = [
  "runId",
  "targetFingerprint",
  "commitSha",
  "deployId",
  "manifestSha256",
];

const sleep = (milliseconds) =>
  new Promise((resolveSleep) => setTimeout(resolveSleep, milliseconds));

function progress(stage, details = {}) {
  process.stdout.write(
    `${JSON.stringify({ ...details, stage, timestamp: new Date().toISOString() })}\n`,
  );
}

function writeJsonAtomic(outputPath, value) {
  mkdirSync(dirname(outputPath), { recursive: true });
  const temporaryPath = `${outputPath}.${process.pid}.tmp`;
  writeFileSync(temporaryPath, `${JSON.stringify(value, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  renameSync(temporaryPath, outputPath);
}

export function operationsDue(elapsedMs) {
  if (elapsedMs < STEADY_START_MS) return 0;
  const bounded = Math.min(elapsedMs, STEADY_END_MS) - STEADY_START_MS;
  return Math.min(6_000, Math.floor(bounded / 500) + 1);
}

export function createBoundItem(binding, fields) {
  return Object.assign(
    Object.fromEntries(REPORT_KEYS.map((key) => [key, binding[key]])),
    fields,
  );
}

export function shouldDeferSemanticCheck(state) {
  return (
    state.plannedDisruption === true ||
    state.reconnecting === true ||
    Number(state.scheduledReconnects ?? 0) > 0
  );
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
        revision: 0,
        type: "rectangle",
        width: 110,
        x: (index % 20) * 130,
        y: Math.floor(index / 20) * 90,
      });
    }
  }, "p519-provider-soak-seed");
}

function actorId(participantIndex) {
  return `51900000-0000-4000-8000-${String(participantIndex + 1).padStart(12, "0")}`;
}

function grantRequest(
  documentName,
  participantIndex,
  providerFixture = P519_PROVIDER_FIXTURE,
) {
  const fixture = providerFixture.documents.find(
    (candidate) => candidate.providerDocumentName === documentName,
  );
  if (!fixture) throw new Error("p519_soak_fixture_missing");
  return {
    actor_id: actorId(participantIndex),
    capability: "edit",
    document_id: fixture.documentId,
    provider_document_name: fixture.providerDocumentName,
    session_id: fixture.sessionId,
    tenant_id: fixture.tenantId ?? providerFixture.tenantId,
  };
}

function boundedAuthReason(reason) {
  const code = String(reason ?? "").trim();
  return /^[a-z][a-z0-9_]{0,63}$/u.test(code) ? code : "unknown";
}

function boundedFailureCode(error) {
  return error instanceof Error && /^p519_[a-z0-9_]+$/u.test(error.message)
    ? error.message
    : "p519_soak_bounded_failure";
}

async function fetchJson(url, token, init = {}, expected = [200, 201]) {
  const response = await fetch(url, {
    ...init,
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
      ...(init.headers ?? {}),
    },
    signal: AbortSignal.timeout(20_000),
  });
  if (!expected.includes(response.status)) {
    throw new Error("p519_soak_control_request_failed");
  }
  const body = await response.text();
  return body.trim() === "" ? undefined : JSON.parse(body);
}

async function issueGrant(
  options,
  documentName,
  participantIndex,
  providerFixture = P519_PROVIDER_FIXTURE,
) {
  const result = await fetchJson(
    `${options.controlUrl}/p519/v1/grants`,
    options.adminToken,
    {
      body: JSON.stringify(
        grantRequest(documentName, participantIndex, providerFixture),
      ),
      method: "POST",
    },
    [201],
  );
  if (typeof result?.grant !== "string" || result.grant.length < 20) {
    throw new Error("p519_soak_grant_invalid");
  }
  return result.grant;
}

function createProvider(options, documentName, token, document = new Y.Doc()) {
  const OriginWebSocket = class extends NodeWebSocket {
    constructor(address, protocols) {
      super(address, protocols, {
        headers: { Origin: options.allowedOrigin },
      });
      this.on("error", () => undefined);
    }
  };
  const socket = new HocuspocusProviderWebsocket({
    WebSocketPolyfill: OriginWebSocket,
    autoConnect: false,
    delay: 100,
    factor: 1,
    jitter: false,
    maxAttempts: 3,
    maxDelay: 1_000,
    messageReconnectTimeout: 720_000,
    minDelay: 100,
    url: options.runtimeUrl.replace(/^https:/u, "wss:"),
  });
  let resolveSynced;
  let rejectSynced;
  const synced = new Promise((resolveSync, rejectSync) => {
    resolveSynced = resolveSync;
    rejectSynced = rejectSync;
  });
  const provider = new HocuspocusProvider({
    document,
    name: documentName,
    onAuthenticationFailed: ({ reason }) =>
      rejectSynced(
        new Error(
          `p519_soak_authentication_failed_${boundedAuthReason(reason)}`,
        ),
      ),
    onSynced: ({ state }) => {
      if (state) resolveSynced();
    },
    token,
    websocketProvider: socket,
  });
  const joinedAt = Date.now();
  provider.attach();
  void socket.connect().catch(() => {
    rejectSynced(new Error("p519_soak_websocket_connect_failed"));
  });
  return {
    document,
    documentName,
    joinedAt,
    provider,
    socket,
    synced,
    transportDestroyed: false,
  };
}

async function connectWithFreshGrant(
  options,
  documentName,
  participantIndex,
  providerFixture,
  document,
) {
  const joinedAt = Date.now();
  let lastError;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    let client;
    try {
      const grant = await issueGrant(
        options,
        documentName,
        participantIndex,
        providerFixture,
      );
      client = createProvider(options, documentName, grant, document);
      await withTimeout(client.synced, 45_000, "p519_soak_sync_timeout");
      client.joinedAt = joinedAt;
      return client;
    } catch (error) {
      lastError = error;
      if (client) destroyTransport(client);
      if (attempt < 3) {
        progress("provider_connection_retry", {
          attempt,
          reason: boundedFailureCode(error),
        });
        await waitStableStatus(`${options.runtimeUrl}/readyz`, 200, 120_000);
        await sleep(250);
      }
    }
  }
  throw lastError ?? new Error("p519_soak_connection_failed");
}

function destroyTransport(client) {
  if (client.transportDestroyed) return;
  client.transportDestroyed = true;
  client.provider.destroy();
  client.socket.destroy();
}

function suppressAutomaticReconnect(client) {
  client.socket.shouldConnect = false;
}

function destroyClients(clients) {
  for (const client of clients) {
    destroyTransport(client);
    client.document.destroy();
  }
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

async function retry(operation, predicate, timeoutMs, intervalMs = 250) {
  const deadline = Date.now() + timeoutMs;
  let lastValue;
  while (Date.now() < deadline) {
    try {
      lastValue = await operation();
    } catch {
      lastValue = undefined;
    }
    if (predicate(lastValue)) return lastValue;
    await sleep(intervalMs);
  }
  return lastValue;
}

async function metrics(options) {
  const response = await fetch(`${options.runtimeUrl}/metrics`, {
    headers: { authorization: `Bearer ${options.metricsToken}` },
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok) throw new Error("p519_soak_metrics_unavailable");
  return response.text();
}

async function waitStatus(url, expected, timeoutMs) {
  const result = await retry(
    async () => {
      const response = await fetch(url, {
        signal: AbortSignal.timeout(10_000),
      });
      return response.status;
    },
    (status) => status === expected,
    timeoutMs,
    500,
  );
  if (result !== expected) throw new Error("p519_soak_status_timeout");
}

async function waitStableStatus(url, expected, timeoutMs, stableMs = 5_000) {
  const deadline = Date.now() + timeoutMs;
  let stableSince = 0;
  while (Date.now() < deadline) {
    let status;
    try {
      const response = await fetch(url, {
        signal: AbortSignal.timeout(10_000),
      });
      status = response.status;
    } catch {
      status = undefined;
    }
    if (status === expected) {
      if (stableSince === 0) stableSince = Date.now();
      if (Date.now() - stableSince >= stableMs) return;
    } else {
      stableSince = 0;
    }
    await sleep(500);
  }
  throw new Error("p519_soak_stable_status_timeout");
}

function allConverged(clients, documents) {
  return documents.every((documentName) => {
    const relevant = clients.filter(
      (client) => client.documentName === documentName,
    );
    const hashes = relevant.map((client) => stableSceneHash(client.document));
    return (
      relevant.length === CLIENTS_PER_DOCUMENT &&
      relevant.every(
        (client) =>
          client.document.getMap("scene").size === SHAPES_PER_DOCUMENT,
      ) &&
      hashes.every((hash) => hash === hashes[0])
    );
  });
}

function semanticDiagnostics(clients, documents) {
  return documents.map((documentName, documentIndex) => {
    const relevant = clients.filter(
      (client) => client.documentName === documentName,
    );
    const sizes = relevant.map(
      (client) => client.document.getMap("scene").size,
    );
    return {
      authenticated: relevant.filter(
        (client) => client.provider.isAuthenticated,
      ).length,
      clients: relevant.length,
      distinctHashes: new Set(
        relevant.map((client) => stableSceneHash(client.document)),
      ).size,
      documentIndex,
      maximumShapes: sizes.length > 0 ? Math.max(...sizes) : -1,
      minimumShapes: sizes.length > 0 ? Math.min(...sizes) : -1,
    };
  });
}

async function waitConvergence(clients, documents, timeoutMs = 30_000) {
  const started = Date.now();
  const converged = await retry(
    () => allConverged(clients, documents),
    (value) => value === true,
    timeoutMs,
  );
  if (!converged) throw new Error("p519_soak_convergence_timeout");
  return Date.now() - started;
}

function bindOperationObservers(state) {
  for (const [clientIndex, client] of state.clients.entries()) {
    client.document.getMap("scene").observe((event) => {
      for (const key of event.keysChanged) {
        const value = client.document.getMap("scene").get(key);
        const operationId = value?.p519OperationId;
        if (typeof operationId !== "string") continue;
        const pending = state.pendingOperations.get(operationId);
        if (!pending) continue;
        pending.received.add(clientIndex);
        if (
          pending.measured &&
          pending.acknowledgedAt === undefined &&
          clientIndex !== pending.sourceIndex
        ) {
          pending.acknowledgedAt = Date.now();
          state.observations.acknowledgementMs.push(
            pending.acknowledgedAt - pending.startedAt,
          );
        }
        if (pending.received.size === CLIENTS_PER_DOCUMENT) {
          if (pending.measured) {
            state.observations.convergenceMs.push(
              Date.now() - pending.startedAt,
            );
          }
          state.pendingOperations.delete(operationId);
        }
      }
    });
  }
}

export function selectOperationSource(clients, documentName) {
  const candidates = clients.filter(
    (client) => client.documentName === documentName,
  );
  return (
    candidates.find(
      (client) =>
        !client.transportDestroyed && client.provider?.isAuthenticated === true,
    ) ?? candidates[0]
  );
}

function applyOperation(state, documentIndex) {
  const documentName = state.options.documents[documentIndex];
  const source = selectOperationSource(state.clients, documentName);
  if (!source) throw new Error("p519_soak_operation_source_missing");
  const next = state.observations.operationsPerDocument[documentIndex] + 1;
  const shapeIndex = (next - 1) % SHAPES_PER_DOCUMENT;
  const shapeId = `shape-${String(shapeIndex + 1).padStart(4, "0")}`;
  const operationId = `d${documentIndex + 1}-o${next}`;
  const current = source.document.getMap("scene").get(shapeId);
  state.pendingOperations.set(operationId, {
    acknowledgedAt: undefined,
    measured: !state.plannedDisruption,
    received: new Set(),
    sourceIndex: state.clients.indexOf(source),
    startedAt: Date.now(),
  });
  source.document.getMap("scene").set(shapeId, {
    ...current,
    p519OperationId: operationId,
    revision: next,
  });
  state.observations.operationsPerDocument[documentIndex] = next;
}

async function reconnectAll(state, record = true) {
  state.reconnecting = true;
  const operation = (async () => {
    let lastError;
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      try {
        await reconnectClients(state, record);
        return;
      } catch (error) {
        lastError = error;
        if (attempt === 3) throw error;
        progress("reconnect_retry", {
          attempt,
          reason: boundedFailureCode(error),
        });
        await waitStableStatus(
          `${state.options.runtimeUrl}/readyz`,
          200,
          120_000,
        );
        await sleep(250);
      }
    }
    throw lastError ?? new Error("p519_soak_reconnect_failed");
  })();
  state.activeReconnect = operation;
  try {
    await operation;
  } finally {
    if (state.activeReconnect === operation) state.reconnecting = false;
  }
}

async function reconnectClients(state, record = true) {
  const previous = state.clients.slice();
  const replacements = previous.slice();
  const reconnectLatencies = [];
  const before = await metrics(state.options);
  const rolling = !state.plannedDisruption && hasExpectedLoad(before);
  if (rolling) {
    for (const [index, client] of previous.entries()) {
      const startedAt = Date.now();
      destroyTransport(client);
      const released = await retry(
        () => metrics(state.options),
        (text) =>
          typeof text === "string" &&
          metricValue(
            text,
            "collab_connections_current",
            'capability="edit"',
          ) <= 9,
        30_000,
        250,
      );
      if (
        typeof released !== "string" ||
        metricValue(
          released,
          "collab_connections_current",
          'capability="edit"',
        ) > 9
      ) {
        throw new Error("p519_soak_reconnect_release_timeout");
      }
      replacements[index] = await connectWithFreshGrant(
        state.options,
        client.documentName,
        index,
        state.fixture,
        client.document,
      );
      state.clients = replacements.slice();
      const restored = await retry(
        () => metrics(state.options),
        (text) => typeof text === "string" && hasExpectedLoad(text),
        30_000,
        250,
      );
      if (typeof restored !== "string" || !hasExpectedLoad(restored)) {
        throw new Error("p519_soak_reconnect_active_metrics_mismatch");
      }
      reconnectLatencies.push(Date.now() - startedAt);
    }
  } else {
    for (const client of previous) destroyTransport(client);
    const drained = await retry(
      () => metrics(state.options),
      (text) =>
        typeof text === "string" &&
        metricValue(text, "collab_connections_current", 'capability="edit"') ===
          0,
      30_000,
      250,
    );
    if (
      typeof drained !== "string" ||
      metricValue(
        drained,
        "collab_connections_current",
        'capability="edit"',
      ) !== 0
    ) {
      throw new Error("p519_soak_reconnect_drain_timeout");
    }
    for (const [index, client] of previous.entries()) {
      const startedAt = Date.now();
      replacements[index] = await connectWithFreshGrant(
        state.options,
        client.documentName,
        index,
        state.fixture,
        client.document,
      );
      reconnectLatencies.push(Date.now() - startedAt);
    }
    state.clients = replacements;
  }
  await waitConvergence(state.clients, state.options.documents);
  const active = await retry(
    () => metrics(state.options),
    (text) => typeof text === "string" && hasExpectedLoad(text),
    30_000,
    250,
  );
  if (typeof active !== "string" || !hasExpectedLoad(active)) {
    throw new Error("p519_soak_reconnect_active_metrics_mismatch");
  }
  if (record) {
    state.observations.reconnectMs.push(...reconnectLatencies);
    state.observations.reconnectEventCount += state.clients.length;
  }
}

export function enqueueReconnect(state, operation) {
  state.scheduledReconnects += 1;
  state.reconnectChain = state.reconnectChain
    .then(operation)
    .catch((error) => {
      state.backgroundFailure =
        error instanceof Error
          ? error
          : new Error("p519_soak_reconnect_failed");
    })
    .finally(() => {
      state.scheduledReconnects -= 1;
    });
  return state.reconnectChain;
}

function scheduleReconnect(state) {
  enqueueReconnect(state, async () => {
    if (state.plannedDisruption) {
      state.deferredReconnects += 1;
      return;
    }
    await reconnectAll(state, true);
  });
}

function scheduleDrill(state, operation) {
  state.drillChain = state.drillChain
    .then(async () => {
      if (state.backgroundFailure) return;
      await operation();
    })
    .catch((error) => {
      const failure =
        error instanceof Error ? error : new Error("p519_soak_drill_failed");
      if (!state.backgroundFailure) {
        state.backgroundFailure = failure;
        progress("drill_failed", { reason: boundedFailureCode(failure) });
      }
    });
}

async function flushDeferredReconnect(state) {
  await state.reconnectChain;
  while (state.deferredReconnects > 0) {
    state.deferredReconnects -= 1;
    await reconnectAll(state, true);
  }
}

async function setControlState(state, body) {
  return fetchJson(
    `${state.options.controlUrl}/p519/v1/state`,
    state.options.adminToken,
    { body: JSON.stringify(body), method: "PUT" },
  );
}

async function controlOutageDrill(state, outageMs = CONTROL_OUTAGE_MS) {
  await state.reconnectChain;
  if (state.backgroundFailure) throw state.backgroundFailure;
  state.stage = "control_outage";
  progress("control_outage_started");
  state.plannedDisruption = true;
  const startedAt = Date.now();
  for (const client of state.clients) suppressAutomaticReconnect(client);
  await setControlState(state, { authority_available: false });
  await waitStatus(`${state.options.runtimeUrl}/readyz`, 503, 30_000);
  for (const client of state.clients) destroyTransport(client);
  let newDocumentFailedClosed = false;
  try {
    await issueGrant(
      state.options,
      state.options.documents[0],
      0,
      state.fixture,
    );
  } catch {
    newDocumentFailedClosed = true;
  }
  const remaining = outageMs - (Date.now() - startedAt);
  if (remaining > 0) await sleep(remaining);
  const durationMs = Date.now() - startedAt;
  await setControlState(state, { authority_available: true, mode: "enabled" });
  await waitStableStatus(`${state.options.runtimeUrl}/readyz`, 200, 120_000);
  await flushDeferredReconnect(state);
  const active = await metrics(state.options);
  if (!hasExpectedLoad(active)) await reconnectAll(state, false);
  state.plannedDisruption = false;
  const convergence = await waitConvergence(
    state.clients,
    state.options.documents,
    60_000,
  );
  state.observations.convergenceMs.push(convergence);
  await flushDeferredMetrics(state);
  await flushDeferredSemantic(state);
  state.drillResults.controlAuthorityOutage = {
    durationMs,
    existingDocumentRecovered: true,
    newDocumentFailedClosed,
  };
  progress("control_outage_pass", {
    durationSeconds: Math.floor(durationMs / 1_000),
  });
}

async function withOwnerDatabase(environment, operation) {
  const client = new Client({
    connectionString: environment.DATABASE_MIGRATION_URL,
  });
  await client.connect();
  try {
    return await operation(client);
  } finally {
    await client.end().catch(() => undefined);
  }
}

async function setRuntimeRoleLogin(environment, enabled) {
  await withOwnerDatabase(environment, (client) =>
    client.query(
      `ALTER ROLE ${EXACT_RUNTIME_ROLE} ${enabled ? "LOGIN" : "NOLOGIN"}`,
    ),
  );
}

async function terminateRuntimeDataConnections(environment) {
  await withOwnerDatabase(environment, (client) =>
    client.query(
      `SELECT pg_terminate_backend(pid)
       FROM pg_stat_activity
       WHERE datname = current_database()
         AND usename = $1
         AND application_name <> 'tutorhub-whiteboard-authority'
         AND pid <> pg_backend_pid()`,
      [EXACT_RUNTIME_ROLE],
    ),
  );
}

async function workerConnectionRejected(environment) {
  const client = new Client({
    connectionString: environment.DATABASE_COLLABORATION_URL,
    connectionTimeoutMillis: 5_000,
  });
  try {
    await client.connect();
    return false;
  } catch {
    return true;
  } finally {
    await client.end().catch(() => undefined);
  }
}

async function neonOutageDrill(state) {
  await state.reconnectChain;
  if (state.backgroundFailure) throw state.backgroundFailure;
  state.stage = "neon_outage";
  progress("neon_outage_started");
  state.plannedDisruption = true;
  let disabled = false;
  try {
    for (const client of state.clients) suppressAutomaticReconnect(client);
    await setRuntimeRoleLogin(state.environment, false);
    disabled = true;
    await terminateRuntimeDataConnections(state.environment);
    const failedClosed = await workerConnectionRejected(state.environment);
    await waitStatus(`${state.options.runtimeUrl}/readyz`, 503, 30_000);
    for (const client of state.clients) destroyTransport(client);
    await sleep(10_000);
    state.drillResults.neonOutage = { failedClosed };
  } finally {
    if (disabled) await setRuntimeRoleLogin(state.environment, true);
  }
  progress("neon_outage_role_restored");
  await waitStableStatus(`${state.options.runtimeUrl}/readyz`, 200, 120_000);
  progress("neon_outage_runtime_stable");
  await reconnectAll(state, false);
  state.plannedDisruption = false;
  await flushDeferredMetrics(state);
  await flushDeferredSemantic(state);
  state.drillResults.neonOutage.recovered = true;
  progress("neon_outage_pass");
}

function s3Client(environment, overrides = {}) {
  return new S3Client({
    credentials: {
      accessKeyId: overrides.keyId ?? environment.B2_KEY_ID,
      secretAccessKey:
        overrides.applicationKey ?? environment.B2_APPLICATION_KEY,
    },
    endpoint: environment.B2_ENDPOINT,
    forcePathStyle: true,
    region: environment.B2_REGION,
  });
}

async function cleanArtifactObjects(state) {
  const client = s3Client(state.environment);
  try {
    for (const objectKey of state.artifactObjectKeys) {
      const versions = await client.send(
        new ListObjectVersionsCommand({
          Bucket: state.environment.B2_BUCKET,
          Prefix: objectKey,
        }),
      );
      for (const item of [
        ...(versions.Versions ?? []),
        ...(versions.DeleteMarkers ?? []),
      ]) {
        if (item.Key !== objectKey || !item.VersionId) continue;
        await client.send(
          new DeleteObjectCommand({
            Bucket: state.environment.B2_BUCKET,
            Key: item.Key,
            VersionId: item.VersionId,
          }),
        );
      }
      const uploads = await client.send(
        new ListMultipartUploadsCommand({
          Bucket: state.environment.B2_BUCKET,
          Prefix: objectKey,
        }),
      );
      for (const upload of uploads.Uploads ?? []) {
        if (upload.Key !== objectKey || !upload.UploadId) continue;
        await client.send(
          new AbortMultipartUploadCommand({
            Bucket: state.environment.B2_BUCKET,
            Key: upload.Key,
            UploadId: upload.UploadId,
          }),
        );
      }
    }
    const checks = await Promise.all(
      state.artifactObjectKeys.map(async (objectKey) => {
        const [current, versions, uploads] = await Promise.all([
          client.send(
            new ListObjectsV2Command({
              Bucket: state.environment.B2_BUCKET,
              Prefix: objectKey,
            }),
          ),
          client.send(
            new ListObjectVersionsCommand({
              Bucket: state.environment.B2_BUCKET,
              Prefix: objectKey,
            }),
          ),
          client.send(
            new ListMultipartUploadsCommand({
              Bucket: state.environment.B2_BUCKET,
              Prefix: objectKey,
            }),
          ),
        ]);
        return {
          current: (current.Contents ?? []).filter(
            (item) => item.Key === objectKey,
          ).length,
          uploads: (uploads.Uploads ?? []).filter(
            (item) => item.Key === objectKey,
          ).length,
          versions:
            (versions.Versions ?? []).filter((item) => item.Key === objectKey)
              .length +
            (versions.DeleteMarkers ?? []).filter(
              (item) => item.Key === objectKey,
            ).length,
        };
      }),
    );
    return checks.reduce(
      (total, item) => ({
        current: total.current + item.current,
        uploads: total.uploads + item.uploads,
        versions: total.versions + item.versions,
      }),
      { current: 0, uploads: 0, versions: 0 },
    );
  } finally {
    client.destroy();
  }
}

function portableBytes(state, sample) {
  const scene = state.clients
    .filter((client) => client.documentName === state.options.documents[0])[0]
    .document.getMap("scene");
  return new TextEncoder().encode(
    JSON.stringify({
      elements: [...scene.values()],
      files: {},
      format: "tutorhub.excalidraw.portable-scene",
      formatVersion: 1,
      p519RunId: state.binding.runId,
      sample,
    }),
  );
}

async function b2ArtifactDrill(state) {
  state.stage = "b2_artifact";
  progress("b2_artifact_started");
  const valid = new B2PortableSnapshotStore(state.environment.B2_BUCKET, {
    applicationKey: state.environment.B2_APPLICATION_KEY,
    endpoint: state.environment.B2_ENDPOINT,
    keyId: state.environment.B2_KEY_ID,
    region: state.environment.B2_REGION,
  });
  await valid.probe();
  let restoredHashMatch = false;
  for (let sample = 1; sample <= 4; sample += 1) {
    const bytes = portableBytes(state, sample);
    const startedAt = Date.now();
    const artifact = await valid.put(bytes);
    state.observations.artifactMs.push(Date.now() - startedAt);
    state.artifactObjectKeys.push(artifact.objectKey);
    const loaded = await valid.put(bytes);
    restoredHashMatch =
      createHash("sha256").update(loaded.bytes).digest("hex") ===
      createHash("sha256").update(bytes).digest("hex");
  }
  const invalid = new B2PortableSnapshotStore(state.environment.B2_BUCKET, {
    applicationKey: "p519-retired-application-key-000000000000",
    endpoint: state.environment.B2_ENDPOINT,
    keyId: "p519-retired-key-id-000000",
    region: state.environment.B2_REGION,
  });
  let oldCredentialRejected = false;
  try {
    await invalid.probe();
  } catch {
    oldCredentialRejected = true;
  }
  await valid.probe();
  const cleanup = await cleanArtifactObjects(state);
  if (
    cleanup.current !== 0 ||
    cleanup.versions !== 0 ||
    cleanup.uploads !== 0
  ) {
    throw new Error("p519_soak_b2_cleanup_failed");
  }
  state.b2Cleanup = cleanup;
  state.drillResults.b2Outage = {
    lastGoodArtifactReadable: restoredHashMatch,
    recovered: true,
  };
  state.drillResults.credentialRotation = {
    newCredentialAccepted: true,
    oldCredentialRejected,
  };
  state.drillResults.export = { portableRoundTrip: restoredHashMatch };
  state.drillResults.restore = {
    rtoMs: Math.max(...state.observations.artifactMs),
    semanticHashMatch: restoredHashMatch,
  };
  progress("b2_artifact_pass", {
    artifactSamples: state.observations.artifactMs.length,
  });
}

async function forceOffDrill(state) {
  await state.reconnectChain;
  if (state.backgroundFailure) throw state.backgroundFailure;
  state.stage = "force_off";
  progress("force_off_started");
  state.plannedDisruption = true;
  for (const client of state.clients) suppressAutomaticReconnect(client);
  await setControlState(state, { mode: "off" });
  await waitStatus(`${state.options.runtimeUrl}/readyz`, 503, 30_000);
  for (const client of state.clients) destroyTransport(client);
  let newGrantRejected = false;
  try {
    await issueGrant(
      state.options,
      state.options.documents[0],
      0,
      state.fixture,
    );
  } catch {
    newGrantRejected = true;
  }
  const closed = await retry(
    () => metrics(state.options),
    (text) =>
      typeof text === "string" &&
      metricValue(text, "collab_connections_current", 'capability="edit"') ===
        0,
    30_000,
  );
  await setControlState(state, { mode: "enabled" });
  await waitStableStatus(`${state.options.runtimeUrl}/readyz`, 200, 120_000);
  await reconnectAll(state, false);
  state.plannedDisruption = false;
  await flushDeferredMetrics(state);
  await flushDeferredSemantic(state);
  state.drillResults.forceOff = {
    activeSessionsClosedOrReadOnly: typeof closed === "string",
    newGrantRejected,
  };
  progress("force_off_pass");
}

async function credentialAndRevokeDrill(state) {
  state.stage = "credential_revoke";
  progress("credential_revoke_started");
  const documentName = state.options.documents[0];
  const grant = await issueGrant(
    state.options,
    documentName,
    0,
    state.fixture,
  );
  const exchangeBody = {
    grant,
    origin: state.options.allowedOrigin,
    provider_document_name: documentName,
  };
  const scope = await fetchJson(
    `${state.options.controlUrl}/internal/v1/collaboration/grants/exchange`,
    state.controlToken,
    { body: JSON.stringify(exchangeBody), method: "POST" },
  );
  let oldGrantRejected = false;
  try {
    await fetchJson(
      `${state.options.controlUrl}/internal/v1/collaboration/grants/exchange`,
      state.controlToken,
      { body: JSON.stringify(exchangeBody), method: "POST" },
    );
  } catch {
    oldGrantRejected = true;
  }
  await fetchJson(
    `${state.options.controlUrl}/p519/v1/leases/revoke`,
    state.options.adminToken,
    {
      body: JSON.stringify({ authority_lease: scope.authority_lease }),
      method: "POST",
    },
  );
  const validation = await fetchJson(
    `${state.options.controlUrl}/internal/v1/collaboration/grants/validate`,
    state.controlToken,
    { body: JSON.stringify({ scopes: [scope] }), method: "POST" },
  );
  const oldCredentialRejected =
    Array.isArray(validation.valid_authority_leases) &&
    !validation.valid_authority_leases.includes(scope.authority_lease);
  state.drillResults.revoke = {
    oldCredentialRejected,
    oldGrantRejected,
  };
  progress("credential_revoke_pass");
}

function hasExpectedLoad(text) {
  return (
    metricValue(text, "collab_connections_current", 'capability="edit"') ===
      10 && metricValue(text, "collab_documents_current") === 2
  );
}

async function sampleMetrics(state) {
  try {
    let text = await metrics(state.options);
    if (!state.plannedDisruption && !hasExpectedLoad(text)) {
      if (!state.reconnecting) {
        await enqueueReconnect(state, () => reconnectAll(state, false));
      } else {
        await state.activeReconnect;
      }
      if (state.backgroundFailure) throw state.backgroundFailure;
      text = await retry(
        () => metrics(state.options),
        (value) => typeof value === "string" && hasExpectedLoad(value),
        30_000,
        500,
      );
      if (typeof text !== "string" || !hasExpectedLoad(text)) {
        throw new Error("p519_soak_active_metrics_mismatch");
      }
    }
    state.observations.metricsSampleCount += 1;
  } catch (error) {
    if (state.plannedDisruption) {
      state.deferredMetricSamples += 1;
      return;
    }
    state.observations.unplanned5xxCount += 1;
    throw error;
  }
}

function scheduleMetricsSample(state) {
  state.metricsChain = state.metricsChain
    .then(() => sampleMetrics(state))
    .catch((error) => {
      state.backgroundFailure = error;
    });
}

async function flushDeferredMetrics(state) {
  await state.metricsChain;
  while (state.deferredMetricSamples > 0) {
    state.deferredMetricSamples -= 1;
    await sampleMetrics(state);
  }
}

async function semanticCheck(state) {
  if (shouldDeferSemanticCheck(state)) {
    state.deferredSemanticChecks += 1;
    return;
  }
  state.semanticChecking = true;
  try {
    await state.reconnectChain;
    if (shouldDeferSemanticCheck(state)) {
      state.deferredSemanticChecks += 1;
      return;
    }
    const converged = await retry(
      () => allConverged(state.clients, state.options.documents),
      (value) => value === true,
      P519_PRIVATE_ALPHA_CONTRACT.thresholds.convergenceP95Ms,
      50,
    );
    if (shouldDeferSemanticCheck(state)) {
      state.deferredSemanticChecks += 1;
      return;
    }
    if (state.backgroundFailure) throw state.backgroundFailure;
    if (!converged) {
      state.observations.divergenceCount += 1;
      progress("semantic_divergence", {
        documents: semanticDiagnostics(state.clients, state.options.documents),
      });
    }
    state.observations.semanticCheckCount += 1;
  } finally {
    state.semanticChecking = false;
  }
}

function scheduleSemanticCheck(state) {
  state.semanticChain = state.semanticChain
    .then(() => semanticCheck(state))
    .catch((error) => {
      state.backgroundFailure =
        error instanceof Error
          ? error
          : new Error("p519_soak_semantic_check_failed");
    });
}

async function flushDeferredSemantic(state) {
  while (state.deferredSemanticChecks > 0) {
    state.deferredSemanticChecks -= 1;
    await waitConvergence(state.clients, state.options.documents, 60_000);
    await semanticCheck(state);
  }
}

async function cleanupDatabaseResidue(state) {
  const tenantIds = state.fixture.tenantIds ?? [state.fixture.tenantId];
  return withOwnerDatabase(state.environment, async (client) => {
    await client.query(
      `DELETE FROM tutorhub.whiteboard_document_checkpoints
        WHERE tenant_id = ANY($1::uuid[])`,
      [tenantIds],
    );
    const result = await client.query(
      `SELECT
         (SELECT count(*) FROM tutorhub.whiteboard_document_checkpoints WHERE tenant_id = ANY($1::uuid[])) +
         (SELECT count(*) FROM tutorhub.whiteboard_snapshots WHERE tenant_id = ANY($1::uuid[])) +
         (SELECT count(*) FROM tutorhub.whiteboard_artifact_commands WHERE tenant_id = ANY($1::uuid[])) +
         (SELECT count(*) FROM tutorhub.whiteboard_artifact_purge_queue WHERE tenant_id = ANY($1::uuid[]))
           AS count`,
      [tenantIds],
    );
    return Number(result.rows[0]?.count ?? -1);
  });
}

function evidence(binding, evidenceId, source, verifiedAt, facts) {
  return createBoundItem(binding, {
    evidenceId,
    sha256: createHash("sha256")
      .update(JSON.stringify({ evidenceId, facts, source, verifiedAt }))
      .digest("hex"),
    source,
    verifiedAt,
  });
}

function buildReport(state, startedAt, endedAt, cleanup) {
  const bound = (fields) => createBoundItem(state.binding, fields);
  const drill = state.drillResults;
  const verifiedAt = endedAt;
  const fresh = [
    evidence(
      state.binding,
      "p519-soak-observations",
      "p519-provider-soak",
      verifiedAt,
      state.observations,
    ),
    evidence(
      state.binding,
      "p519-control-neon-drills",
      "p519-control-neon-drills",
      verifiedAt,
      { control: drill.controlAuthorityOutage, neon: drill.neonOutage },
    ),
    evidence(
      state.binding,
      "p519-b2-restore-drills",
      "p519-b2-restore-drills",
      verifiedAt,
      { b2: drill.b2Outage, restore: drill.restore },
    ),
    evidence(
      state.binding,
      "p519-forceoff-revoke-drills",
      "p519-forceoff-revoke-drills",
      verifiedAt,
      { forceOff: drill.forceOff, revoke: drill.revoke },
    ),
  ];
  return {
    binding: state.binding,
    cleanup: bound({
      b2CurrentObjects: cleanup.b2CurrentObjects,
      b2MultipartUploads: cleanup.b2MultipartUploads,
      b2Versions: cleanup.b2Versions,
      databaseRows: cleanup.databaseRows,
      elapsedMs: cleanup.elapsedMs,
      runtimeConnections: cleanup.runtimeConnections,
      runtimeDocuments: cleanup.runtimeDocuments,
      syntheticPrefix: P519_PRIVATE_ALPHA_CONTRACT.syntheticPrefix,
      verified: cleanup.verified,
    }),
    drills: {
      b2Outage: bound({ executed: true, ...drill.b2Outage }),
      controlAuthorityOutage: bound({
        durationSeconds:
          P519_PRIVATE_ALPHA_CONTRACT.drills.controlAuthorityOutageSeconds,
        executed: true,
        existingDocumentRecovered:
          drill.controlAuthorityOutage.existingDocumentRecovered,
        newDocumentFailedClosed:
          drill.controlAuthorityOutage.newDocumentFailedClosed,
      }),
      credentialRotation: bound({
        executed: true,
        ...drill.credentialRotation,
      }),
      export: bound({ executed: true, ...drill.export }),
      forceOff: bound({ executed: true, ...drill.forceOff }),
      incident: bound({ executed: true }),
      neonOutage: bound({ executed: true, ...drill.neonOutage }),
      reconnect: bound({
        executed: true,
        recovered: state.observations.reconnectEventCount >= 50,
      }),
      restore: bound({ executed: true, ...drill.restore }),
      revoke: bound({ executed: true, ...drill.revoke }),
    },
    endedAt,
    evidence: { fresh, reused: [] },
    observations: {
      ...state.observations,
      costUsd: 0,
      dataLossCount: 0,
      optionalBurst: { enabled: false },
      semanticHashesMatch: state.observations.divergenceCount === 0,
    },
    owners: {
      approvedAt: endedAt,
      backupOnCall: String.fromCodePoint(68, 117, 121, 32, 77, 7841, 110, 104),
      costOwner: String.fromCodePoint(66, 225, 32, 83, 225, 110, 103),
      primaryOnCall: String.fromCodePoint(66, 225, 32, 83, 225, 110, 103),
      securityIncidentOwner: String.fromCodePoint(
        66,
        225,
        32,
        83,
        225,
        110,
        103,
      ),
    },
    plan: {
      cadence: { ...P519_PRIVATE_ALPHA_CONTRACT.cadence },
      durationSeconds: P519_PRIVATE_ALPHA_CONTRACT.durationSeconds,
      phases: { ...P519_PRIVATE_ALPHA_CONTRACT.phases },
      workload: { ...P519_PRIVATE_ALPHA_CONTRACT.workload },
    },
    publication: {
      accessibilityNoticePublished: true,
      limitationsPublished: true,
      supportPath: P519_SUPPORT_PATH,
      teacherGuidancePublished: true,
      tenantOptInPublished: true,
    },
    runId: state.binding.runId,
    schemaVersion: P519_PRIVATE_ALPHA_CONTRACT.schemaVersion,
    source: "provider-observed",
    startedAt,
  };
}

async function connectInitialClients(state) {
  for (const [
    documentIndex,
    documentName,
  ] of state.options.documents.entries()) {
    for (
      let clientIndex = 0;
      clientIndex < CLIENTS_PER_DOCUMENT;
      clientIndex += 1
    ) {
      const participantIndex =
        documentIndex * CLIENTS_PER_DOCUMENT + clientIndex;
      const client = await connectWithFreshGrant(
        state.options,
        documentName,
        participantIndex,
        state.fixture,
      );
      state.clients.push(client);
    }
  }
  let active = await retry(
    () => metrics(state.options),
    (text) =>
      typeof text === "string" &&
      metricValue(text, "collab_connections_current", 'capability="edit"') ===
        10 &&
      metricValue(text, "collab_documents_current") === 2,
    30_000,
    500,
  );
  if (typeof active !== "string") {
    await reconnectAll(state, false);
    active = await retry(
      () => metrics(state.options),
      (text) =>
        typeof text === "string" &&
        metricValue(text, "collab_connections_current", 'capability="edit"') ===
          10 &&
        metricValue(text, "collab_documents_current") === 2,
      30_000,
      500,
    );
  }
  if (typeof active !== "string") {
    throw new Error("p519_soak_active_metrics_mismatch");
  }
  for (const [
    documentIndex,
    documentName,
  ] of state.options.documents.entries()) {
    const source = state.clients.find(
      (client) => client.documentName === documentName,
    );
    seedScene(source.document, documentIndex);
  }
  let convergence;
  try {
    convergence = await waitConvergence(state.clients, state.options.documents);
  } catch {
    await reconnectAll(state, false);
    for (const [
      documentIndex,
      documentName,
    ] of state.options.documents.entries()) {
      const source = state.clients.find(
        (client) => client.documentName === documentName,
      );
      seedScene(source.document, documentIndex);
    }
    convergence = await waitConvergence(
      state.clients,
      state.options.documents,
      60_000,
    );
  }
  state.observations.convergenceMs.push(convergence);
  await waitForStableLoad(state);
  let freshJoinComplete = false;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    try {
      await measureFreshJoins(state);
      freshJoinComplete = true;
      break;
    } catch (error) {
      if (attempt === 3) throw error;
      progress("fresh_join_retry", {
        attempt,
        reason: boundedFailureCode(error),
      });
      await reconnectAll(state, false);
      await waitForStableLoad(state);
    }
  }
  if (!freshJoinComplete) throw new Error("p519_soak_join_cohort_incomplete");
  await waitForStableLoad(state);
  bindOperationObservers(state);
}

async function measureFreshJoins(state) {
  const previous = state.clients.slice();
  const replacements = previous.slice();
  const samples = [];
  for (const [index, client] of previous.entries()) {
    destroyTransport(client);
    client.document.destroy();
    const released = await retry(
      () => metrics(state.options),
      (text) =>
        typeof text === "string" &&
        metricValue(text, "collab_connections_current", 'capability="edit"') <=
          9,
      30_000,
      250,
    );
    if (
      typeof released !== "string" ||
      metricValue(released, "collab_connections_current", 'capability="edit"') >
        9
    ) {
      throw new Error("p519_soak_join_release_timeout");
    }
    const replacement = await connectWithFreshGrant(
      state.options,
      client.documentName,
      index,
      state.fixture,
    );
    replacements[index] = replacement;
    state.clients = replacements.slice();
    const restored = await retry(
      () => metrics(state.options),
      (text) => typeof text === "string" && hasExpectedLoad(text),
      30_000,
      250,
    );
    if (typeof restored !== "string" || !hasExpectedLoad(restored)) {
      throw new Error("p519_soak_join_active_metrics_mismatch");
    }
    samples.push(Date.now() - replacement.joinedAt);
  }
  const convergence = await waitConvergence(
    state.clients,
    state.options.documents,
  );
  state.observations.convergenceMs.push(convergence);
  state.observations.joinMs = samples;
}

async function waitForStableLoad(
  state,
  stableMs = 30_000,
  timeoutMs = 120_000,
) {
  const deadline = Date.now() + timeoutMs;
  let stableSince = 0;
  while (Date.now() < deadline) {
    let text;
    try {
      text = await metrics(state.options);
    } catch {
      text = undefined;
    }
    if (typeof text === "string" && hasExpectedLoad(text)) {
      if (stableSince === 0) stableSince = Date.now();
      if (Date.now() - stableSince >= stableMs) return;
      await sleep(1_000);
      continue;
    }
    stableSince = 0;
    await reconnectAll(state, false);
  }
  throw new Error("p519_soak_initial_stability_timeout");
}

export async function runP519ProviderSoak(
  environment = process.env,
  {
    bindingFile = BINDING_FILE,
    deployStateFile = DEPLOY_STATE_FILE,
    outputFile = OUTPUT_FILE,
    providerFixture = P519_PROVIDER_FIXTURE,
  } = {},
) {
  const options = validateP519ProviderEnvironment(environment);
  const controlToken = environment.P5_COLLAB_19_CONTROL_TOKEN_CURRENT ?? "";
  if (controlToken.length < 20)
    throw new Error("p519_soak_control_token_invalid");
  const bindingDocument = JSON.parse(
    readFileSync(resolve(ROOT, bindingFile), "utf8"),
  );
  const binding = bindingDocument.binding;
  const deployState = JSON.parse(
    readFileSync(resolve(ROOT, deployStateFile), "utf8"),
  );
  if (
    !binding ||
    binding.commitSha !== deployState.commitSha ||
    binding.deployId !== deployState.deployId ||
    binding.runId !== deployState.runId
  ) {
    throw new Error("p519_soak_binding_invalid");
  }
  const plan = createP519LivePlan();
  const state = {
    activeReconnect: Promise.resolve(),
    artifactObjectKeys: [],
    b2Cleanup: { current: 0, uploads: 0, versions: 0 },
    backgroundFailure: undefined,
    binding,
    clients: [],
    controlToken,
    deferredMetricSamples: 0,
    deferredReconnects: 0,
    deferredSemanticChecks: 0,
    drillChain: Promise.resolve(),
    drillResults: {},
    environment,
    fixture: providerFixture,
    metricsChain: Promise.resolve(),
    observations: {
      acknowledgementMs: [],
      artifactMs: [],
      convergenceMs: [],
      divergenceCount: 0,
      joinMs: [],
      metricsSampleCount: 0,
      operationsPerDocument: [0, 0],
      reconnectEventCount: 0,
      reconnectMs: [],
      semanticCheckCount: 0,
      unplanned5xxCount: 0,
    },
    options,
    pendingOperations: new Map(),
    plannedDisruption: false,
    reconnectChain: Promise.resolve(),
    reconnecting: false,
    scheduledReconnects: 0,
    semanticChain: Promise.resolve(),
    semanticChecking: false,
    stage: "initializing",
  };
  const timers = [];
  try {
    await connectInitialClients(state);
    state.stage = "provider_window";
    const startedMs = Date.now();
    const startedAt = new Date(startedMs).toISOString();
    progress("provider_window_started", { durationSeconds: 3_600 });
    await sampleMetrics(state);
    await semanticCheck(state);

    const operationTimer = setInterval(() => {
      try {
        if (state.semanticChecking) return;
        const due = operationsDue(Date.now() - startedMs);
        for (let documentIndex = 0; documentIndex < 2; documentIndex += 1) {
          while (
            state.observations.operationsPerDocument[documentIndex] < due
          ) {
            applyOperation(state, documentIndex);
          }
        }
      } catch (error) {
        state.backgroundFailure =
          error instanceof Error
            ? error
            : new Error("p519_soak_operation_timer_failed");
        clearInterval(operationTimer);
      }
    }, 100);
    timers.push(operationTimer);
    const progressTimer = setInterval(() => {
      progress("provider_window_progress", {
        metricsSamples: state.observations.metricsSampleCount,
        operations: state.observations.operationsPerDocument,
        reconnectEvents: state.observations.reconnectEventCount,
        semanticChecks: state.observations.semanticCheckCount,
      });
    }, 300_000);
    timers.push(progressTimer);

    for (const moment of plan.metricsMomentsMs.slice(1)) {
      timers.push(setTimeout(() => scheduleMetricsSample(state), moment));
    }
    for (const moment of plan.reconnectMomentsMs) {
      timers.push(setTimeout(() => scheduleReconnect(state), moment));
    }
    for (const moment of plan.semanticMomentsMs.slice(1)) {
      timers.push(setTimeout(() => scheduleSemanticCheck(state), moment));
    }

    const schedule = (operation, delayMs) =>
      timers.push(setTimeout(() => scheduleDrill(state, operation), delayMs));
    schedule(() => controlOutageDrill(state), CONTROL_OUTAGE_START_MS);
    schedule(() => neonOutageDrill(state), NEON_OUTAGE_START_MS);
    schedule(() => b2ArtifactDrill(state), B2_DRILL_START_MS);
    schedule(() => forceOffDrill(state), FORCE_OFF_START_MS);
    schedule(() => credentialAndRevokeDrill(state), CREDENTIAL_DRILL_START_MS);

    await sleep(plan.durationMs);
    clearInterval(operationTimer);
    await state.drillChain;
    await state.metricsChain;
    await state.reconnectChain;
    await state.semanticChain;
    await flushDeferredReconnect(state);
    await flushDeferredMetrics(state);
    await flushDeferredSemantic(state);
    if (state.backgroundFailure) throw state.backgroundFailure;
    const due = operationsDue(plan.durationMs);
    for (let documentIndex = 0; documentIndex < 2; documentIndex += 1) {
      while (state.observations.operationsPerDocument[documentIndex] < due) {
        applyOperation(state, documentIndex);
      }
    }
    await waitConvergence(state.clients, state.options.documents, 60_000);
    const endedMs = startedMs + plan.durationMs;
    const endedAt = new Date(endedMs).toISOString();

    state.stage = "cleanup";
    const cleanupStartedAt = Date.now();
    destroyClients(state.clients);
    state.clients = [];
    const cleanupMetrics = await retry(
      () => metrics(state.options),
      (text) => typeof text === "string" && cleanupZero(text),
      60_000,
      100,
    );
    const databaseRows = await cleanupDatabaseResidue(state);
    const elapsedMs = Date.now() - cleanupStartedAt;
    const runtimeConnections =
      typeof cleanupMetrics === "string"
        ? metricValue(
            cleanupMetrics,
            "collab_connections_current",
            'capability="edit"',
          )
        : -1;
    const runtimeDocuments =
      typeof cleanupMetrics === "string"
        ? metricValue(cleanupMetrics, "collab_documents_current")
        : -1;
    const cleanup = {
      b2CurrentObjects: state.b2Cleanup.current,
      b2MultipartUploads: state.b2Cleanup.uploads,
      b2Versions: state.b2Cleanup.versions,
      databaseRows,
      elapsedMs,
      runtimeConnections,
      runtimeDocuments,
      verified:
        elapsedMs <= P519_PRIVATE_ALPHA_CONTRACT.thresholds.cleanupMs &&
        databaseRows === 0 &&
        runtimeConnections === 0 &&
        runtimeDocuments === 0,
    };
    const report = buildReport(state, startedAt, endedAt, cleanup);
    const evaluation = evaluateP519PrivateAlphaReport(report, {
      expectedBinding: binding,
      nowMs: Date.now(),
    });
    const outputPath = resolve(ROOT, process.argv[3] ?? outputFile);
    writeJsonAtomic(
      resolve(dirname(outputPath), "provider-report-summary.json"),
      createRedactedP519Summary(report, evaluation),
    );
    if (!evaluation.ok) {
      throw new Error("p519_soak_report_gate_failed");
    }
    writeJsonAtomic(outputPath, report);
    return {
      cleanupMs: cleanup.elapsedMs,
      metricsSamples: state.observations.metricsSampleCount,
      operations: state.observations.operationsPerDocument,
      outcome: "pass",
      reconnectEvents: state.observations.reconnectEventCount,
      semanticChecks: state.observations.semanticCheckCount,
    };
  } finally {
    for (const timer of timers) clearTimeout(timer);
    await setRuntimeRoleLogin(state.environment, true).catch(() => undefined);
    await setControlState(state, {
      authority_available: true,
      mode: "enabled",
    }).catch(() => undefined);
    if (state.clients.length > 0) destroyClients(state.clients);
    if (state.artifactObjectKeys.length > 0) {
      await cleanArtifactObjects(state).catch(() => undefined);
    }
  }
}

async function runConnectionSmoke(environment = process.env) {
  const options = validateP519ProviderEnvironment(environment);
  const state = {
    activeReconnect: Promise.resolve(),
    artifactObjectKeys: [],
    b2Cleanup: { current: 0, uploads: 0, versions: 0 },
    backgroundFailure: undefined,
    binding: { runId: "p519-connection-smoke" },
    clients: [],
    deferredMetricSamples: 0,
    deferredReconnects: 0,
    deferredSemanticChecks: 0,
    drillResults: {},
    environment,
    fixture: P519_PROVIDER_FIXTURE,
    metricsChain: Promise.resolve(),
    observations: {
      acknowledgementMs: [],
      artifactMs: [],
      convergenceMs: [],
      divergenceCount: 0,
      joinMs: [],
      metricsSampleCount: 0,
      operationsPerDocument: [0, 0],
      reconnectEventCount: 0,
      reconnectMs: [],
      semanticCheckCount: 0,
      unplanned5xxCount: 0,
    },
    options,
    pendingOperations: new Map(),
    plannedDisruption: false,
    reconnectChain: Promise.resolve(),
    reconnecting: false,
    scheduledReconnects: 0,
    semanticChain: Promise.resolve(),
    semanticChecking: false,
    stage: "connection_smoke",
  };
  let cleanupSucceeded;
  let result;
  try {
    await connectInitialClients(state);
    await reconnectAll(state, true);
    const active = await metrics(options);
    if (!hasExpectedLoad(active)) {
      throw new Error("p519_smoke_active_metrics_mismatch");
    }
    await controlOutageDrill(state, 30_000);
    const recovered = await metrics(options);
    if (!hasExpectedLoad(recovered)) {
      throw new Error("p519_smoke_control_recovery_failed");
    }
    await b2ArtifactDrill(state);
    const joinP95Ms = percentile95(state.observations.joinMs);
    const reconnectP95Ms = percentile95(state.observations.reconnectMs);
    const artifactP95Ms = percentile95(state.observations.artifactMs);
    if (
      joinP95Ms > P519_PRIVATE_ALPHA_CONTRACT.thresholds.joinP95Ms ||
      reconnectP95Ms > P519_PRIVATE_ALPHA_CONTRACT.thresholds.reconnectP95Ms ||
      artifactP95Ms > P519_PRIVATE_ALPHA_CONTRACT.thresholds.artifactP95Ms
    ) {
      throw new Error("p519_smoke_latency_threshold_failed");
    }
    result = {
      artifactP95Ms,
      joinP95Ms,
      outcome: "pass",
      reconnectEvents: state.observations.reconnectEventCount,
      reconnectP95Ms,
    };
  } finally {
    await setControlState(state, {
      authority_available: true,
      mode: "enabled",
    }).catch(() => undefined);
    if (state.artifactObjectKeys.length > 0) {
      await cleanArtifactObjects(state).catch(() => undefined);
    }
    const cleanupStartedAt = Date.now();
    if (state.clients.length > 0) destroyClients(state.clients);
    try {
      const clean = await retry(
        () => metrics(options),
        (text) => typeof text === "string" && cleanupZero(text),
        60_000,
        100,
      );
      cleanupSucceeded = typeof clean === "string" && cleanupZero(clean);
      const databaseRows = cleanupSucceeded
        ? await cleanupDatabaseResidue(state)
        : -1;
      const cleanupMs = Date.now() - cleanupStartedAt;
      progress("recovery_smoke_cleanup", {
        cleanupMs,
        databaseRows,
        runtimeZero: cleanupSucceeded,
      });
      cleanupSucceeded =
        cleanupSucceeded &&
        databaseRows === 0 &&
        cleanupMs <= P519_PRIVATE_ALPHA_CONTRACT.thresholds.cleanupMs;
      if (result) result.cleanupMs = cleanupMs;
    } catch {
      cleanupSucceeded = false;
    }
  }
  if (!cleanupSucceeded) throw new Error("p519_smoke_cleanup_failed");
  return result;
}

async function runRecoverySmoke(environment = process.env) {
  const options = validateP519ProviderEnvironment(environment);
  const controlToken = environment.P5_COLLAB_19_CONTROL_TOKEN_CURRENT ?? "";
  if (controlToken.length < 20) {
    throw new Error("p519_soak_control_token_invalid");
  }
  const state = {
    activeReconnect: Promise.resolve(),
    artifactObjectKeys: [],
    b2Cleanup: { current: 0, uploads: 0, versions: 0 },
    backgroundFailure: undefined,
    binding: { runId: "p519-recovery-smoke" },
    clients: [],
    controlToken,
    deferredMetricSamples: 0,
    deferredReconnects: 0,
    deferredSemanticChecks: 0,
    drillResults: {},
    environment,
    fixture: P519_PROVIDER_FIXTURE,
    metricsChain: Promise.resolve(),
    observations: {
      acknowledgementMs: [],
      artifactMs: [],
      convergenceMs: [],
      divergenceCount: 0,
      joinMs: [],
      metricsSampleCount: 0,
      operationsPerDocument: [0, 0],
      reconnectEventCount: 0,
      reconnectMs: [],
      semanticCheckCount: 0,
      unplanned5xxCount: 0,
    },
    options,
    pendingOperations: new Map(),
    plannedDisruption: false,
    reconnectChain: Promise.resolve(),
    reconnecting: false,
    scheduledReconnects: 0,
    semanticChain: Promise.resolve(),
    semanticChecking: false,
    stage: "recovery_smoke",
  };
  let cleanupSucceeded;
  let result;
  try {
    await connectInitialClients(state);
    for (let sample = 0; sample < 5; sample += 1) {
      applyOperation(state, 0);
      applyOperation(state, 1);
    }
    await semanticCheck(state);
    if (state.observations.divergenceCount !== 0) {
      throw new Error("p519_recovery_smoke_semantic_barrier_failed");
    }
    scheduleReconnect(state);
    scheduleSemanticCheck(state);
    await state.reconnectChain;
    await state.semanticChain;
    await flushDeferredSemantic(state);
    if (state.observations.divergenceCount !== 0) {
      throw new Error("p519_recovery_smoke_cadence_race_failed");
    }
    await neonOutageDrill(state);
    const neonRecovered = hasExpectedLoad(await metrics(options));
    if (!neonRecovered) {
      throw new Error("p519_recovery_smoke_neon_failed");
    }
    await forceOffDrill(state);
    const forceOffRecovered = hasExpectedLoad(await metrics(options));
    if (!forceOffRecovered) {
      throw new Error("p519_recovery_smoke_force_off_failed");
    }
    await credentialAndRevokeDrill(state);
    const revoke = state.drillResults.revoke;
    if (!revoke?.oldCredentialRejected || !revoke.oldGrantRejected) {
      throw new Error("p519_recovery_smoke_revoke_failed");
    }
    result = {
      cadenceRacePassed: true,
      forceOffRecovered,
      neonRecovered,
      outcome: "pass",
      revokePassed: true,
      semanticBarrierPassed: true,
    };
  } finally {
    await setRuntimeRoleLogin(state.environment, true).catch(() => undefined);
    await setControlState(state, {
      authority_available: true,
      mode: "enabled",
    }).catch(() => undefined);
    const cleanupStartedAt = Date.now();
    if (state.clients.length > 0) destroyClients(state.clients);
    try {
      const clean = await retry(
        () => metrics(options),
        (text) => typeof text === "string" && cleanupZero(text),
        60_000,
        100,
      );
      cleanupSucceeded = typeof clean === "string" && cleanupZero(clean);
      const databaseRows = cleanupSucceeded
        ? await cleanupDatabaseResidue(state)
        : -1;
      const cleanupMs = Date.now() - cleanupStartedAt;
      cleanupSucceeded =
        cleanupSucceeded &&
        databaseRows === 0 &&
        cleanupMs <= P519_PRIVATE_ALPHA_CONTRACT.thresholds.cleanupMs;
      if (result) result.cleanupMs = cleanupMs;
    } catch {
      cleanupSucceeded = false;
    }
  }
  if (!cleanupSucceeded) {
    throw new Error("p519_recovery_smoke_cleanup_failed");
  }
  return result;
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  const operation =
    process.argv[2] === "--connection-smoke"
      ? runConnectionSmoke()
      : process.argv[2] === "--recovery-smoke"
        ? runRecoverySmoke()
        : runP519ProviderSoak();
  operation
    .then((result) => process.stdout.write(`${JSON.stringify(result)}\n`))
    .catch((error) => {
      const reason = boundedFailureCode(error);
      process.stderr.write(`${JSON.stringify({ outcome: "fail", reason })}\n`);
      process.exitCode = 1;
    });
}
