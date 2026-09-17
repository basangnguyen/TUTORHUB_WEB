import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { parseEnvFile } from "./run-p507-disposable.mjs";
import {
  P519_RENDER_SERVICES,
  createP519RenderConfiguration,
} from "./p519-render-sync.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const API_BASE = "https://api.render.com/v1";
const KNOWN_REASON = /"reason_code":"([a-z0-9_]+)"/u;

async function request(apiKey, path) {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      accept: "application/json",
      authorization: `Bearer ${apiKey}`,
    },
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok)
    throw new Error(`p519_render_diagnostic_${response.status}`);
  return response.json();
}

const values = parseEnvFile(
  readFileSync(
    resolve(ROOT, process.argv[2] ?? ".env.p5-collab-19-disposable.local"),
    "utf8",
  ),
);
const configuration = createP519RenderConfiguration(values);
const serviceQuery = new URLSearchParams({
  limit: "100",
  name: P519_RENDER_SERVICES.runtime.name,
});
const serviceEntries = await request(
  configuration.apiKey,
  `/services?${serviceQuery.toString()}`,
);
const services = serviceEntries
  .map((entry) => entry.service ?? entry)
  .filter((service) => service.name === P519_RENDER_SERVICES.runtime.name);
if (services.length !== 1) throw new Error("p519_render_service_cardinality");
const service = services[0];
const ownerId = service.ownerId ?? service.owner?.id;
if (typeof ownerId !== "string" || ownerId.length < 5) {
  throw new Error("p519_render_owner_missing");
}
const logQuery = new URLSearchParams({
  direction: "backward",
  endTime: new Date().toISOString(),
  limit: "100",
  ownerId,
  resource: service.id,
  startTime: new Date(Date.now() - 30 * 60_000).toISOString(),
});
const logResult = await request(
  configuration.apiKey,
  `/logs?${logQuery.toString()}`,
);
const logs = Array.isArray(logResult) ? logResult : (logResult.logs ?? []);
const reasonCodes = [];
const eventCodes = new Map();
let exitFailureObserved = false;
let portFailureObserved = false;
for (const entry of logs) {
  const message = String(entry.message ?? entry.text ?? "");
  const reason = message.match(KNOWN_REASON)?.[1];
  if (reason) reasonCodes.push(reason);
  const event = message.match(/"event_code":"([a-z0-9_]+)"/u)?.[1];
  if (event) eventCodes.set(event, (eventCodes.get(event) ?? 0) + 1);
  if (/exited with status|non-zero exit/iu.test(message)) {
    exitFailureObserved = true;
  }
  if (/no open ports|port scan timeout/iu.test(message)) {
    portFailureObserved = true;
  }
}
process.stdout.write(
  `${JSON.stringify({
    exitFailureObserved,
    eventCodes: Object.fromEntries(
      [...eventCodes.entries()].sort(([left], [right]) =>
        left.localeCompare(right),
      ),
    ),
    logCount: logs.length,
    portFailureObserved,
    reasonCodes: [...new Set(reasonCodes)],
    serviceId: service.id,
  })}\n`,
);
