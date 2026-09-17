import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { parseEnvFile } from "./run-p507-disposable.mjs";
import {
  createP519ExpectedBinding,
  sanitizeP519Output,
  validateP519Environment,
  writeP519TrustedBinding,
} from "./run-p519-disposable.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const DEFAULT_ENV_FILE = ".env.p5-collab-19-disposable.local";
const DEFAULT_STATE_FILE = "tmp/p5-collab-19/render-deploy.json";
const DEFAULT_BINDING_FILE = "tmp/p5-collab-19/run-binding.json";
const API_BASE = "https://api.render.com/v1";
const EXPECTED_ORIGIN = "https://p519-private-alpha.invalid";
const EXPECTED_DOCUMENTS =
  "wb_p519_private_alpha_document_01,wb_p519_private_alpha_document_02";
const TERMINAL_FAILURES = new Set([
  "build_failed",
  "canceled",
  "cancelled",
  "deactivated",
  "pre_deploy_failed",
  "update_failed",
]);

export const P519_RENDER_SERVICES = Object.freeze({
  control: Object.freeze({
    name: "tutorhub-p5-p519-control-bs-20260825",
    url: "https://tutorhub-p5-p519-control-bs-20260825.onrender.com",
  }),
  runtime: Object.freeze({
    name: "tutorhub-p5-p519-runtime-bs-20260825",
    url: "https://tutorhub-p5-p519-runtime-bs-20260825.onrender.com",
  }),
});

function requiredValue(values, names, code, minimumLength = 1) {
  for (const name of names) {
    const value = values.get(name)?.trim() ?? "";
    if (value.length >= minimumLength) return value;
  }
  throw new Error(code);
}

function currentCommitSha() {
  const value = execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: ROOT,
    encoding: "utf8",
    windowsHide: true,
  }).trim();
  if (!/^[a-f0-9]{40}$/u.test(value)) {
    throw new Error("p519_render_commit_invalid");
  }
  return value;
}

function utcRunId(now = new Date()) {
  return `p519-private-alpha-${now.toISOString().replace(/[-:.]/gu, "")}`;
}

export function createP519RenderConfiguration(
  values,
  { commitSha = currentCommitSha(), now = new Date() } = {},
) {
  const disposable = validateP519Environment(values);
  const apiKey = requiredValue(
    values,
    ["P5_COLLAB_19_RENDER_API_KEY", "RENDER_API_KEY"],
    "p519_render_api_key_required",
    20,
  );
  const controlToken = requiredValue(
    values,
    ["P5_COLLAB_19_CONTROL_TOKEN_CURRENT"],
    "p519_control_token_required",
    20,
  );
  const configuredRuntimeToken =
    values.get("COLLAB_CONTROL_PLANE_TOKEN")?.trim() ?? "";
  if (configuredRuntimeToken && configuredRuntimeToken !== controlToken) {
    throw new Error("p519_control_tokens_must_match");
  }
  const adminToken = requiredValue(
    values,
    ["P5_COLLAB_19_CONTROL_ADMIN_TOKEN"],
    "p519_control_admin_token_required",
    20,
  );
  const metricsToken = requiredValue(
    values,
    ["COLLAB_METRICS_TOKEN"],
    "p519_metrics_token_required",
    20,
  );
  const configuredControlUrl = requiredValue(
    values,
    ["P5_COLLAB_19_CONTROL_URL"],
    "p519_control_url_required",
  ).replace(/\/$/u, "");
  const configuredRuntimeUrl = requiredValue(
    values,
    ["P5_COLLAB_19_RUNTIME_URL"],
    "p519_runtime_url_required",
  ).replace(/\/$/u, "");
  if (
    configuredControlUrl !== P519_RENDER_SERVICES.control.url ||
    configuredRuntimeUrl !== P519_RENDER_SERVICES.runtime.url ||
    values.get("P5_COLLAB_19_ALLOWED_ORIGIN") !== EXPECTED_ORIGIN ||
    values.get("P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES") !== EXPECTED_DOCUMENTS
  ) {
    throw new Error("p519_render_target_contract_mismatch");
  }
  return {
    apiKey,
    commitSha,
    disposable,
    health: {
      adminToken,
      controlUrl: configuredControlUrl,
      metricsToken,
      runtimeUrl: configuredRuntimeUrl,
    },
    runId: utcRunId(now),
    serviceEnvironment: {
      control: {
        P5_COLLAB_19_ALLOWED_ORIGIN: EXPECTED_ORIGIN,
        P5_COLLAB_19_CONTROL_ADMIN_TOKEN: adminToken,
        P5_COLLAB_19_CONTROL_TOKEN_CURRENT: controlToken,
        P5_COLLAB_19_DISPOSABLE_CONFIRM:
          "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
        P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES: EXPECTED_DOCUMENTS,
      },
      runtime: {
        B2_APPLICATION_KEY: disposable.B2_APPLICATION_KEY,
        B2_BUCKET: disposable.B2_BUCKET,
        B2_ENDPOINT: disposable.B2_ENDPOINT,
        B2_KEY_ID: disposable.B2_KEY_ID,
        B2_REGION: disposable.B2_REGION,
        COLLAB_ALLOWED_ORIGINS: EXPECTED_ORIGIN,
        COLLAB_BUILD_ID: commitSha,
        COLLAB_CONTROL_PLANE_TOKEN: controlToken,
        COLLAB_CONTROL_PLANE_URL: P519_RENDER_SERVICES.control.url,
        COLLAB_DRAIN_TIMEOUT_MS: "25000",
        COLLAB_INSTANCE_COUNT: "1",
        COLLAB_METRICS_TOKEN: metricsToken,
        COLLAB_PROBE_TIMEOUT_MS: "5000",
        COLLAB_RUNTIME_PROFILE: "FREE_PRIVATE_ALPHA",
        DATABASE_COLLABORATION_URL: disposable.DATABASE_COLLABORATION_URL,
      },
    },
  };
}

function serviceDetails(service) {
  return service.serviceDetails ?? service.service_details ?? {};
}

export function validateP519RenderService(service, expectedName) {
  const details = serviceDetails(service);
  if (
    !service ||
    service.name !== expectedName ||
    service.type !== "web_service" ||
    service.branch !== "main" ||
    !new Set(["no", "off", false]).has(service.autoDeploy) ||
    (service.region ?? details.region) !== "singapore" ||
    details.plan !== "free"
  ) {
    throw new Error("p519_render_service_contract_mismatch");
  }
  if (typeof service.id !== "string" || !/^srv-[a-z0-9]+$/u.test(service.id)) {
    throw new Error("p519_render_service_id_invalid");
  }
  return service;
}

async function renderRequest(fetchImpl, apiKey, path, init = {}) {
  const response = await fetchImpl(`${API_BASE}${path}`, {
    ...init,
    headers: {
      accept: "application/json",
      authorization: `Bearer ${apiKey}`,
      ...(init.body ? { "content-type": "application/json" } : {}),
    },
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok) {
    throw new Error(`p519_render_api_${response.status}`);
  }
  if (response.status === 204) return undefined;
  const body = await response.text();
  if (body.trim() === "") return undefined;
  try {
    return JSON.parse(body);
  } catch {
    throw new Error("p519_render_api_response_invalid");
  }
}

async function findExactService(fetchImpl, apiKey, expectedName) {
  const query = new URLSearchParams({ limit: "100", name: expectedName });
  const result = await renderRequest(
    fetchImpl,
    apiKey,
    `/services?${query.toString()}`,
  );
  const matches = Array.isArray(result)
    ? result
        .map((entry) => entry?.service ?? entry)
        .filter((service) => service?.name === expectedName)
    : [];
  if (matches.length !== 1) {
    throw new Error("p519_render_service_cardinality_invalid");
  }
  return validateP519RenderService(matches[0], expectedName);
}

async function syncEnvironment(fetchImpl, configuration, service, values) {
  for (const [key, value] of Object.entries(values)) {
    await renderRequest(
      fetchImpl,
      configuration.apiKey,
      `/services/${encodeURIComponent(service.id)}/env-vars/${encodeURIComponent(key)}`,
      { body: JSON.stringify({ value }), method: "PUT" },
    );
  }
}

async function triggerDeploy(fetchImpl, configuration, service) {
  const deploy = await renderRequest(
    fetchImpl,
    configuration.apiKey,
    `/services/${encodeURIComponent(service.id)}/deploys`,
    {
      body: JSON.stringify({
        clearCache: "do_not_clear",
        commitId: configuration.commitSha,
      }),
      method: "POST",
    },
  );
  if (typeof deploy?.id !== "string" || !/^dep-[a-z0-9]+$/u.test(deploy.id)) {
    throw new Error("p519_render_deploy_id_invalid");
  }
  return deploy.id;
}

async function waitForDeploy(
  fetchImpl,
  configuration,
  service,
  deployId,
  {
    delay = (milliseconds) =>
      new Promise((resolve) => setTimeout(resolve, milliseconds)),
    timeoutMs = 30 * 60_000,
  } = {},
) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const deploy = await renderRequest(
      fetchImpl,
      configuration.apiKey,
      `/services/${encodeURIComponent(service.id)}/deploys/${encodeURIComponent(deployId)}`,
    );
    if (deploy?.status === "live") {
      const deployedCommit = deploy.commit?.id ?? deploy.commitId;
      if (deployedCommit && deployedCommit !== configuration.commitSha) {
        throw new Error("p519_render_deployed_commit_mismatch");
      }
      return;
    }
    if (TERMINAL_FAILURES.has(deploy?.status)) {
      throw new Error("p519_render_deploy_failed");
    }
    await delay(10_000);
  }
  throw new Error("p519_render_deploy_timeout");
}

async function waitForHttp(fetchImpl, url, expected, headers = {}) {
  const deadline = Date.now() + 180_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetchImpl(url, {
        headers,
        signal: AbortSignal.timeout(15_000),
      });
      if (response.status === expected) return;
    } catch {
      // Render Free may briefly refuse connections while the service wakes.
    }
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new Error("p519_render_health_timeout");
}

function writePrivateJson(filePath, value) {
  const outputPath = resolve(ROOT, filePath);
  mkdirSync(dirname(outputPath), { recursive: true });
  const temporaryPath = `${outputPath}.${process.pid}.tmp`;
  writeFileSync(temporaryPath, `${JSON.stringify(value, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  renameSync(temporaryPath, outputPath);
  return outputPath;
}

export async function runP519RenderProvider(
  values,
  {
    bindingFile = DEFAULT_BINDING_FILE,
    fetchImpl = fetch,
    stateFile = DEFAULT_STATE_FILE,
    waitOptions,
    configuration = createP519RenderConfiguration(values),
  } = {},
) {
  const control = await findExactService(
    fetchImpl,
    configuration.apiKey,
    P519_RENDER_SERVICES.control.name,
  );
  const runtime = await findExactService(
    fetchImpl,
    configuration.apiKey,
    P519_RENDER_SERVICES.runtime.name,
  );
  await syncEnvironment(
    fetchImpl,
    configuration,
    control,
    configuration.serviceEnvironment.control,
  );
  await syncEnvironment(
    fetchImpl,
    configuration,
    runtime,
    configuration.serviceEnvironment.runtime,
  );

  const controlDeployId = await triggerDeploy(
    fetchImpl,
    configuration,
    control,
  );
  await waitForDeploy(
    fetchImpl,
    configuration,
    control,
    controlDeployId,
    waitOptions,
  );
  await waitForHttp(fetchImpl, `${configuration.health.controlUrl}/livez`, 200);

  const runtimeDeployId = await triggerDeploy(
    fetchImpl,
    configuration,
    runtime,
  );
  await waitForDeploy(
    fetchImpl,
    configuration,
    runtime,
    runtimeDeployId,
    waitOptions,
  );
  await waitForHttp(
    fetchImpl,
    `${configuration.health.runtimeUrl}/readyz`,
    200,
  );
  await waitForHttp(
    fetchImpl,
    `${configuration.health.controlUrl}/p519/v1/status`,
    200,
    { authorization: `Bearer ${configuration.health.adminToken}` },
  );

  const deployId = `${controlDeployId}.${runtimeDeployId}`;
  const bindingEnvironment = {
    ...configuration.disposable,
    P5_COLLAB_19_DEPLOY_ID: deployId,
    P5_COLLAB_19_RUN_ID: configuration.runId,
  };
  const expected = createP519ExpectedBinding(bindingEnvironment);
  writeP519TrustedBinding(bindingFile, expected.binding);
  const state = {
    bindingFile,
    commitSha: configuration.commitSha,
    control: { deployId: controlDeployId, serviceId: control.id },
    deployId,
    generatedAt: new Date().toISOString(),
    runId: configuration.runId,
    runtime: { deployId: runtimeDeployId, serviceId: runtime.id },
  };
  writePrivateJson(stateFile, state);
  return state;
}

async function main(args = process.argv.slice(2)) {
  const envFile = resolve(ROOT, args[0] ?? DEFAULT_ENV_FILE);
  let values;
  try {
    values = parseEnvFile(readFileSync(envFile, "utf8"));
    const state = await runP519RenderProvider(values);
    process.stdout.write(
      `${JSON.stringify({
        commit: state.commitSha,
        controlDeploy: state.control.deployId,
        outcome: "pass",
        runtimeDeploy: state.runtime.deployId,
        services: 2,
      })}\n`,
    );
    return 0;
  } catch (error) {
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
    return 1;
  }
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  process.exitCode = await main();
}
