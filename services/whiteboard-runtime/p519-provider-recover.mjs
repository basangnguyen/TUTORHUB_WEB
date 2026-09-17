import { Client } from "pg";

import { validateP519ProviderEnvironment } from "./p519-provider-preflight.mjs";

const options = validateP519ProviderEnvironment(process.env);
const owner = new Client({
  connectionString: process.env.DATABASE_MIGRATION_URL,
  connectionTimeoutMillis: 10_000,
});
try {
  await owner.connect();
  await owner.query("ALTER ROLE tutorhub_collab_worker LOGIN");
} finally {
  await owner.end().catch(() => undefined);
}
const response = await fetch(`${options.controlUrl}/p519/v1/state`, {
  body: JSON.stringify({ authority_available: true, mode: "enabled" }),
  headers: {
    authorization: `Bearer ${options.adminToken}`,
    "content-type": "application/json",
  },
  method: "PUT",
  signal: AbortSignal.timeout(20_000),
});
if (!response.ok) throw new Error("p519_recovery_request_failed");
const statusResponse = await fetch(`${options.controlUrl}/p519/v1/status`, {
  headers: { authorization: `Bearer ${options.adminToken}` },
  signal: AbortSignal.timeout(20_000),
});
if (!statusResponse.ok) throw new Error("p519_recovery_status_failed");
const status = await statusResponse.json();
if (status.authority_available !== true || status.mode !== "enabled") {
  throw new Error("p519_recovery_not_applied");
}
process.stdout.write(
  `${JSON.stringify({
    authorityAvailable: true,
    mode: "enabled",
    outcome: "pass",
    runtimeRoleLogin: true,
  })}\n`,
);
