import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { validateGateF3Environment } from "./require-p5-collab-01-gate-f3-confirm.mjs";

const requireFromRuntime = createRequire(
  new URL("../services/whiteboard-runtime/package.json", import.meta.url),
);
const { Pool } = requireFromRuntime("pg");

const EXACT_RUNTIME_ROLE = "tutorhub_collab_f3";
const EXACT_OUTAGE_CONFIRMATION =
  "I_UNDERSTAND_P5_F3_NEON_ROLE_WILL_BE_UNAVAILABLE_FOR_600_SECONDS";
let boundedFailureStage = "startup";

export function validateNeonOutageEnvironment(env = process.env) {
  const validated = validateGateF3Environment(env);
  const runtimeRole = decodeURIComponent(validated.runtimeDatabase.username);
  if (runtimeRole !== EXACT_RUNTIME_ROLE) {
    throw new Error("gate_f3_neon_runtime_role_not_exact");
  }
  if (env.P5_F3_OUTAGE_TARGET !== "neon") {
    throw new Error("gate_f3_neon_outage_target_invalid");
  }
  if (env.P5_F3_OUTAGE_SECONDS !== "600") {
    throw new Error("gate_f3_neon_outage_duration_invalid");
  }
  if (env.P5_F3_OUTAGE_CONFIRM !== EXACT_OUTAGE_CONFIRMATION) {
    throw new Error("gate_f3_neon_outage_confirmation_missing");
  }
  return { ...validated, runtimeRole };
}

export async function runGateF3NeonOutage(env = process.env) {
  boundedFailureStage = "validate";
  const validated = validateNeonOutageEnvironment(env);
  boundedFailureStage = "runtime_liveness_preflight";
  await waitForStatus(`${validated.runtimeUrl}/livez`, 200, 120_000);
  boundedFailureStage = "runtime_role_preflight";
  const loginRepaired = await ensureRuntimeRoleLogin(
    validated.ownerDatabase,
    validated.runtimeRole,
  );
  boundedFailureStage = "runtime_database_preflight";
  if (!(await databaseAcceptsConnection(validated.runtimeDatabase))) {
    throw new Error("gate_f3_neon_runtime_preflight_failed");
  }
  boundedFailureStage = "runtime_readiness_preflight";
  await waitForStatus(`${validated.runtimeUrl}/readyz`, 200, 120_000);
  console.log(
    JSON.stringify({
      preflight_login_repaired: loginRepaired,
      target: "neon",
    }),
  );

  let loginDisabled = false;
  try {
    boundedFailureStage = "disable_login";
    await setRuntimeRoleLogin(
      validated.ownerDatabase,
      validated.runtimeRole,
      false,
    );
    loginDisabled = true;
    boundedFailureStage = "terminate_connections";
    await terminateRuntimeRoleConnections(
      validated.ownerDatabase,
      validated.runtimeRole,
    );
    if (await databaseAcceptsConnection(validated.runtimeDatabase)) {
      throw new Error("gate_f3_neon_role_failed_open");
    }
    boundedFailureStage = "wait_fail_closed";
    await waitForStatus(`${validated.runtimeUrl}/readyz`, 503, 30_000);

    for (let elapsed = 60; elapsed <= 600; elapsed += 60) {
      boundedFailureStage = `hold_${elapsed}`;
      await delay(60_000);
      await assertHttpStatus(`${validated.runtimeUrl}/livez`, 200);
      await assertHttpStatus(`${validated.runtimeUrl}/readyz`, 503);
      console.log(
        JSON.stringify({
          elapsed_seconds: elapsed,
          fail_closed: true,
          liveness_preserved: true,
          target: "neon",
        }),
      );
    }
  } finally {
    if (loginDisabled) {
      boundedFailureStage = "restore_login";
      await retryOperation(
        () =>
          setRuntimeRoleLogin(
            validated.ownerDatabase,
            validated.runtimeRole,
            true,
          ),
        30_000,
      );
      boundedFailureStage = "wait_database_recovery";
      await waitForDatabase(validated.runtimeDatabase, 30_000);
      boundedFailureStage = "wait_readiness_recovery";
      await waitForStatus(`${validated.runtimeUrl}/readyz`, 200, 60_000);
    }
  }

  boundedFailureStage = "complete";
  console.log(
    JSON.stringify({
      duration_seconds: 600,
      fail_closed: true,
      liveness_preserved: true,
      outcome: "pass",
      recovered: true,
      target: "neon",
    }),
  );
}

async function ensureRuntimeRoleLogin(ownerDatabase, runtimeRole) {
  const canLogin = await withPool(ownerDatabase, async (pool) => {
    const result = await pool.query(
      `
        SELECT rolcanlogin AS can_login
        FROM pg_roles
        WHERE rolname = $1
      `,
      [runtimeRole],
    );
    if (result.rows.length !== 1) {
      throw new Error("gate_f3_neon_runtime_role_preflight_failed");
    }
    return result.rows[0]?.can_login === true;
  });
  if (canLogin) return false;
  await setRuntimeRoleLogin(ownerDatabase, runtimeRole, true);
  return true;
}

async function setRuntimeRoleLogin(ownerDatabase, runtimeRole, enabled) {
  const role = quoteIdentifier(runtimeRole);
  await withPool(ownerDatabase, (pool) =>
    pool.query(`ALTER ROLE ${role} ${enabled ? "LOGIN" : "NOLOGIN"}`),
  );
}

async function terminateRuntimeRoleConnections(ownerDatabase, runtimeRole) {
  await withPool(ownerDatabase, async (pool) => {
    await pool.query(
      `
        SELECT pg_terminate_backend(pid)
        FROM pg_stat_activity
        WHERE datname = current_database()
          AND usename = $1
          AND pid <> pg_backend_pid()
      `,
      [runtimeRole],
    );
    const remaining = await pool.query(
      `
        SELECT NOT EXISTS (
          SELECT 1
          FROM pg_stat_activity
          WHERE datname = current_database()
            AND usename = $1
            AND pid <> pg_backend_pid()
        ) AS cleared
      `,
      [runtimeRole],
    );
    if (remaining.rows[0]?.cleared !== true) {
      throw new Error("gate_f3_neon_runtime_connections_not_cleared");
    }
  });
}

async function databaseAcceptsConnection(databaseUrl) {
  try {
    await withPool(databaseUrl, (pool) => pool.query("SELECT 1"));
    return true;
  } catch {
    return false;
  }
}

async function waitForDatabase(databaseUrl, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await databaseAcceptsConnection(databaseUrl)) return;
    await delay(500);
  }
  throw new Error("gate_f3_neon_runtime_database_recovery_timeout");
}

async function withPool(databaseUrl, operation) {
  const pool = new Pool({
    allowExitOnIdle: true,
    connectionString: databaseUrl.toString(),
    connectionTimeoutMillis: 5_000,
    idleTimeoutMillis: 5_000,
    max: 1,
    statement_timeout: 10_000,
  });
  try {
    return await operation(pool);
  } finally {
    await pool.end().catch(() => undefined);
  }
}

async function assertHttpStatus(url, expected) {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
    if (response.status !== expected) {
      throw new Error("unexpected_status");
    }
  } catch {
    throw new Error("gate_f3_neon_http_probe_failed");
  }
}

async function waitForStatus(url, expected, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
      if (response.status === expected) return;
    } catch {
      // A sleeping disposable service can briefly refuse connections.
    }
    await delay(500);
  }
  throw new Error("gate_f3_neon_readiness_transition_timeout");
}

async function retryOperation(operation, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      await operation();
      return;
    } catch {
      await delay(500);
    }
  }
  throw new Error("gate_f3_neon_restore_failed");
}

function quoteIdentifier(value) {
  return `"${value.replaceAll('"', '""')}"`;
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  runGateF3NeonOutage().catch(() => {
    console.error(
      JSON.stringify({
        outcome: "fail",
        reason: "bounded_neon_outage_gate_failure",
        stage: boundedFailureStage,
      }),
    );
    process.exitCode = 1;
  });
}
