import { P519_PRIVATE_ALPHA_CONTRACT } from "../../scripts/p519-private-alpha-contract.mjs";

export const P519_DOCUMENTS = Object.freeze([
  "wb_p519_private_alpha_document_01",
  "wb_p519_private_alpha_document_02",
]);

export function createP519LivePlan(contract = P519_PRIVATE_ALPHA_CONTRACT) {
  const durationMs = contract.durationSeconds * 1_000;
  const operationIntervalMs =
    60_000 / contract.workload.steadyOperationsPerMinutePerDocument;
  const reconnectMomentsMs = cadenceMoments(
    durationMs,
    contract.cadence.reconnectSeconds * 1_000,
    false,
  );
  const metricsMomentsMs = cadenceMoments(
    durationMs,
    contract.cadence.metricsSeconds * 1_000,
    true,
  );
  const semanticMomentsMs = cadenceMoments(
    durationMs,
    contract.cadence.semanticCheckSeconds * 1_000,
    true,
  );
  return Object.freeze({
    durationMs,
    documents: P519_DOCUMENTS,
    clientsPerDocument: contract.workload.clientsPerDocument,
    totalConnections: contract.workload.totalConnections,
    shapesPerDocument: contract.workload.shapesPerDocument,
    operationIntervalMs,
    operationsPerDocument: Math.floor(durationMs / operationIntervalMs),
    reconnectMomentsMs,
    reconnectEvents:
      reconnectMomentsMs.length * contract.workload.totalConnections,
    metricsMomentsMs,
    semanticMomentsMs,
  });
}

export function cadenceMoments(durationMs, cadenceMs, includeZero) {
  if (!Number.isInteger(durationMs) || durationMs < 1) {
    throw new Error("invalid_duration_ms");
  }
  if (!Number.isInteger(cadenceMs) || cadenceMs < 1) {
    throw new Error("invalid_cadence_ms");
  }
  const moments = [];
  for (let at = includeZero ? 0 : cadenceMs; at < durationMs; at += cadenceMs) {
    moments.push(at);
  }
  return Object.freeze(moments);
}

export function dueCount(moments, elapsedMs) {
  let count = 0;
  while (count < moments.length && moments[count] <= elapsedMs) count += 1;
  return count;
}
