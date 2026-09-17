import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { parseEnvFile } from "./run-p507-disposable.mjs";
import {
  createP519ExpectedBinding,
  sanitizeP519Output,
  writeP519TrustedBinding,
} from "./run-p519-disposable.mjs";
import {
  P519_RENDER_SERVICES,
  createP519RenderConfiguration,
  validateP519RenderService,
} from "./p519-render-sync.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const require = createRequire(import.meta.url);
const { Client } = require(
  resolve(ROOT, "services/whiteboard-runtime/node_modules/pg"),
);
const API_BASE = "https://api.render.com/v1";
const ENV_FILE = ".env.p5-collab-19-disposable.local";
const BINDING_FILE = "tmp/p5-collab-19/run-binding.json";
const STATE_FILE = "tmp/p5-collab-19/render-deploy.json";
const AUTHORITY_LOCK_NAMESPACE = 0x5455_4842;
const AUTHORITY_LOCK_KEY = 0x5742_5244;

const delay = (milliseconds) =>
  new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds));

async function request(apiKey, path, method = "GET", body) {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      accept: "application/json",
      authorization: `Bearer ${apiKey}`,
      ...(body ? { "content-type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
    method,
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok)
    throw new Error(`p519_render_resume_api_${response.status}`);
  const responseBody = await response.text();
  return responseBody.trim() === "" ? undefined : JSON.parse(responseBody);
}

async function waitForDeploy(configuration, id, deployId) {
  const terminalFailures = new Set([
    "build_failed",
    "canceled",
    "pre_deploy_failed",
    "update_failed",
  ]);
  const deadline = Date.now() + 15 * 60_000;
  while (Date.now() < deadline) {
    const deploy = await request(
      configuration.apiKey,
      `/services/${encodeURIComponent(id)}/deploys/${encodeURIComponent(deployId)}`,
    );
    if (deploy?.status === "live") {
      const commit = deploy.commit?.id ?? deploy.commitId;
      if (commit && commit !== configuration.commitSha) {
        throw new Error("p519_runtime_deployed_commit_mismatch");
      }
      return deploy;
    }
    if (terminalFailures.has(deploy?.status)) {
      throw new Error("p519_runtime_singleton_deploy_failed");
    }
    await delay(2_000);
  }
  throw new Error("p519_runtime_singleton_deploy_timeout");
}

async function exactService(configuration, expected) {
  const query = new URLSearchParams({ limit: "100", name: expected.name });
  const entries = await request(
    configuration.apiKey,
    `/services?${query.toString()}`,
  );
  const matches = entries
    .map((entry) => entry.service ?? entry)
    .filter((service) => service.name === expected.name);
  if (matches.length !== 1) throw new Error("p519_render_service_cardinality");
  return validateP519RenderService(matches[0], expected.name);
}

async function service(configuration, id) {
  return request(configuration.apiKey, `/services/${encodeURIComponent(id)}`);
}

async function waitForSuspension(configuration, id, expected) {
  const deadline = Date.now() + 5 * 60_000;
  while (Date.now() < deadline) {
    const current = await service(configuration, id);
    const suspended = current.suspended === "suspended";
    if (suspended === expected) return;
    await delay(2_000);
  }
  throw new Error("p519_render_suspension_transition_timeout");
}

async function latestLiveDeploy(configuration, id) {
  const entries = await request(
    configuration.apiKey,
    `/services/${encodeURIComponent(id)}/deploys?limit=10`,
  );
  return entries
    .map((entry) => entry.deploy ?? entry)
    .find(
      (deploy) =>
        deploy.status === "live" &&
        (deploy.commit?.id ?? deploy.commitId) === configuration.commitSha,
    );
}

async function withMigrationClient(configuration, operation) {
  const client = new Client({
    application_name: "p519-render-singleton-handoff",
    connectionString: configuration.disposable.DATABASE_MIGRATION_URL,
    connectionTimeoutMillis: 5_000,
    query_timeout: 5_000,
    statement_timeout: 5_000,
  });
  try {
    await client.connect();
    return await operation(client);
  } finally {
    await client.end().catch(() => undefined);
  }
}

async function captureRuntimeAuthority(configuration) {
  return withMigrationClient(configuration, async (client) => {
    const result = await client.query(
      `SELECT activity.pid
       FROM pg_catalog.pg_stat_activity AS activity
       JOIN pg_catalog.pg_locks AS authority_lock
         ON authority_lock.pid = activity.pid
       WHERE activity.datname = current_database()
         AND activity.usename = 'tutorhub_collab_worker'
         AND activity.application_name = 'tutorhub-whiteboard-authority'
         AND authority_lock.locktype = 'advisory'
         AND authority_lock.classid = $1::oid
         AND authority_lock.objid = $2::oid
         AND authority_lock.objsubid = 2
         AND authority_lock.mode = 'ExclusiveLock'
         AND authority_lock.granted`,
      [AUTHORITY_LOCK_NAMESPACE, AUTHORITY_LOCK_KEY],
    );
    if (result.rows.length !== 1 || !Number.isSafeInteger(result.rows[0].pid)) {
      throw new Error("p519_runtime_authority_cardinality_invalid");
    }
    return result.rows[0].pid;
  });
}

async function releaseCapturedAuthority(configuration, backendPid) {
  return withMigrationClient(configuration, async (client) => {
    const result = await client.query(
      `SELECT CASE
         WHEN EXISTS (
           SELECT 1
           FROM pg_catalog.pg_stat_activity AS activity
           JOIN pg_catalog.pg_locks AS authority_lock
             ON authority_lock.pid = activity.pid
           WHERE activity.pid = $1
             AND activity.datname = current_database()
             AND activity.usename = 'tutorhub_collab_worker'
             AND activity.application_name = 'tutorhub-whiteboard-authority'
             AND authority_lock.locktype = 'advisory'
             AND authority_lock.classid = $2::oid
             AND authority_lock.objid = $3::oid
             AND authority_lock.objsubid = 2
             AND authority_lock.mode = 'ExclusiveLock'
             AND authority_lock.granted
         ) THEN pg_catalog.pg_terminate_backend($1)
         WHEN NOT EXISTS (
           SELECT 1
           FROM pg_catalog.pg_locks AS authority_lock
           WHERE authority_lock.database = (
             SELECT oid FROM pg_catalog.pg_database
             WHERE datname = current_database()
           )
             AND authority_lock.locktype = 'advisory'
             AND authority_lock.classid = $2::oid
             AND authority_lock.objid = $3::oid
             AND authority_lock.objsubid = 2
             AND authority_lock.mode = 'ExclusiveLock'
             AND authority_lock.granted
         ) THEN true
         ELSE false
       END AS released`,
      [backendPid, AUTHORITY_LOCK_NAMESPACE, AUTHORITY_LOCK_KEY],
    );
    if (result.rows.length !== 1 || result.rows[0].released !== true) {
      throw new Error("p519_runtime_authority_handoff_failed");
    }
  });
}

async function restartRuntime(configuration, id) {
  await request(
    configuration.apiKey,
    `/services/${encodeURIComponent(id)}/suspend`,
    "POST",
  );
  await waitForSuspension(configuration, id, true);
  await request(
    configuration.apiKey,
    `/services/${encodeURIComponent(id)}/resume`,
    "POST",
  );
  await waitForSuspension(configuration, id, false);
  await waitForReady(configuration);
}

async function waitForReady(configuration) {
  const deadline = Date.now() + 15 * 60_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(
        `${configuration.health.runtimeUrl}/readyz`,
        {
          signal: AbortSignal.timeout(15_000),
        },
      );
      if (response.status === 200) return;
    } catch {
      // Expected while the singleton is starting after resume.
    }
    await delay(2_000);
  }
  throw new Error("p519_render_resume_readiness_timeout");
}

async function main() {
  let values;
  let runtime;
  let authorityReleased = false;
  try {
    values = parseEnvFile(
      readFileSync(resolve(ROOT, process.argv[2] ?? ENV_FILE), "utf8"),
    );
    const configuration = createP519RenderConfiguration(values);
    const control = await exactService(
      configuration,
      P519_RENDER_SERVICES.control,
    );
    runtime = await exactService(configuration, P519_RENDER_SERVICES.runtime);
    const controlDeploy = await latestLiveDeploy(configuration, control.id);
    if (!controlDeploy) throw new Error("p519_control_live_deploy_missing");

    await waitForReady(configuration);
    const authorityBackendPid = await captureRuntimeAuthority(configuration);
    const pendingDeploy = await request(
      configuration.apiKey,
      `/services/${encodeURIComponent(runtime.id)}/deploys`,
      "POST",
      { deployMode: "deploy_only" },
    );
    if (
      typeof pendingDeploy?.id !== "string" ||
      !/^dep-[a-z0-9]+$/u.test(pendingDeploy.id)
    ) {
      throw new Error("p519_runtime_singleton_deploy_id_invalid");
    }
    await releaseCapturedAuthority(configuration, authorityBackendPid);
    authorityReleased = true;
    const runtimeDeploy = await waitForDeploy(
      configuration,
      runtime.id,
      pendingDeploy.id,
    );
    await waitForReady(configuration);
    authorityReleased = false;

    const deployId = `${controlDeploy.id}.${runtimeDeploy.id}`;
    const expected = createP519ExpectedBinding({
      ...configuration.disposable,
      P5_COLLAB_19_DEPLOY_ID: deployId,
      P5_COLLAB_19_RUN_ID: configuration.runId,
    });
    writeP519TrustedBinding(BINDING_FILE, expected.binding);
    const state = {
      bindingFile: BINDING_FILE,
      commitSha: configuration.commitSha,
      control: { deployId: controlDeploy.id, serviceId: control.id },
      deployId,
      generatedAt: new Date().toISOString(),
      runId: configuration.runId,
      runtime: { deployId: runtimeDeploy.id, serviceId: runtime.id },
      singletonResume: true,
    };
    const output = resolve(ROOT, STATE_FILE);
    const temporary = `${output}.${process.pid}.tmp`;
    const { mkdirSync, renameSync, writeFileSync } = await import("node:fs");
    const { dirname } = await import("node:path");
    mkdirSync(dirname(output), { recursive: true });
    writeFileSync(temporary, `${JSON.stringify(state, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
    });
    renameSync(temporary, output);
    process.stdout.write(
      `${JSON.stringify({
        commit: configuration.commitSha,
        controlDeploy: controlDeploy.id,
        outcome: "pass",
        runtimeDeploy: runtimeDeploy.id,
        singletonResume: true,
      })}\n`,
    );
  } catch (error) {
    if (authorityReleased && runtime && values) {
      const configuration = createP519RenderConfiguration(values);
      await restartRuntime(configuration, runtime.id).catch(() => undefined);
    }
    const environment = values
      ? Object.fromEntries(values.entries())
      : Object.create(null);
    const message = error instanceof Error ? error.message : "unknown";
    process.stderr.write(
      `${JSON.stringify({
        outcome: "fail",
        reason: sanitizeP519Output(message, environment),
      })}\n`,
    );
    process.exitCode = 1;
  }
}

await main();
