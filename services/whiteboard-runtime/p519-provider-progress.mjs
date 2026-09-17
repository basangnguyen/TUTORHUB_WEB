import {
  metricValue,
  validateP519ProviderEnvironment,
} from "./p519-provider-preflight.mjs";

const options = validateP519ProviderEnvironment(process.env);
const [metricsResponse, statusResponse] = await Promise.all([
  fetch(`${options.runtimeUrl}/metrics`, {
    headers: { authorization: `Bearer ${options.metricsToken}` },
    signal: AbortSignal.timeout(15_000),
  }),
  fetch(`${options.controlUrl}/p519/v1/status`, {
    headers: { authorization: `Bearer ${options.adminToken}` },
    signal: AbortSignal.timeout(15_000),
  }),
]);
if (!metricsResponse.ok || !statusResponse.ok) {
  throw new Error("p519_progress_probe_unavailable");
}
const metrics = await metricsResponse.text();
const status = await statusResponse.json();
process.stdout.write(
  `${JSON.stringify({
    authorityAvailable: status.authority_available === true,
    activeLeases: status.active_leases,
    checkpointFailed: metricValue(
      metrics,
      "collab_checkpoint_total",
      'outcome="failed"',
    ),
    checkpointLoaded: metricValue(
      metrics,
      "collab_checkpoint_total",
      'outcome="loaded"',
    ),
    checkpointStored: metricValue(
      metrics,
      "collab_checkpoint_total",
      'outcome="stored"',
    ),
    connectionAccepted: metricValue(
      metrics,
      "collab_connection_total",
      'outcome="accepted"',
    ),
    connectionClosed: metricValue(
      metrics,
      "collab_connection_total",
      'outcome="closed"',
    ),
    connectionRejected: metricValue(
      metrics,
      "collab_connection_total",
      'outcome="rejected"',
    ),
    dirtyDocuments: metricValue(metrics, "collab_dirty_documents"),
    documents: metricValue(metrics, "collab_documents_current"),
    editConnections: metricValue(
      metrics,
      "collab_connections_current",
      'capability="edit"',
    ),
    mode: status.mode,
    connectionQuotaRejected: metricValue(
      metrics,
      "collab_policy_rejection_total",
      'reason="connection_quota"',
    ),
    operationQuotaRejected: metricValue(
      metrics,
      "collab_policy_rejection_total",
      'reason="operation_quota"',
    ),
    updateRejected: metricValue(
      metrics,
      "collab_policy_rejection_total",
      'reason="update"',
    ),
  })}\n`,
);
