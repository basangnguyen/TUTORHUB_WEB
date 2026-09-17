import {
  metricValue,
  validateP519ProviderEnvironment,
} from "./p519-provider-preflight.mjs";

const options = validateP519ProviderEnvironment(process.env);

async function fetchControl(path, init = {}) {
  return fetch(`${options.controlUrl}${path}`, {
    ...init,
    headers: {
      authorization: `Bearer ${options.adminToken}`,
      "content-type": "application/json",
      ...(init.headers ?? {}),
    },
    signal: AbortSignal.timeout(20_000),
  });
}

const stateResponse = await fetchControl("/p519/v1/state", {
  body: JSON.stringify({ authority_available: true, mode: "off" }),
  method: "PUT",
});
if (!stateResponse.ok) throw new Error("p519_force_off_request_failed");

const deadline = Date.now() + 60_000;
let readyStatus;
while (Date.now() < deadline) {
  try {
    const response = await fetch(`${options.runtimeUrl}/readyz`, {
      signal: AbortSignal.timeout(10_000),
    });
    readyStatus = response.status;
  } catch {
    readyStatus = undefined;
  }
  if (readyStatus === 503) break;
  await new Promise((resolve) => setTimeout(resolve, 500));
}

const [statusResponse, metricsResponse] = await Promise.all([
  fetchControl("/p519/v1/status"),
  fetch(`${options.runtimeUrl}/metrics`, {
    headers: { authorization: `Bearer ${options.metricsToken}` },
    signal: AbortSignal.timeout(15_000),
  }),
]);
if (!statusResponse.ok || !metricsResponse.ok) {
  throw new Error("p519_force_off_verification_unavailable");
}
const status = await statusResponse.json();
const metrics = await metricsResponse.text();
const editConnections = metricValue(
  metrics,
  "collab_connections_current",
  'capability="edit"',
);
const documents = metricValue(metrics, "collab_documents_current");
if (
  status.authority_available !== true ||
  status.mode !== "off" ||
  readyStatus !== 503 ||
  editConnections !== 0 ||
  documents !== 0
) {
  throw new Error("p519_force_off_verification_failed");
}

process.stdout.write(
  `${JSON.stringify({
    authorityAvailable: true,
    documents,
    editConnections,
    mode: "off",
    outcome: "pass",
    runtimeReady: false,
  })}\n`,
);
