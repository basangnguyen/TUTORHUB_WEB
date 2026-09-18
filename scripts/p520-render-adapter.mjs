import { metricValue } from "../services/whiteboard-runtime/p519-provider-preflight.mjs";
import {
  P519_RENDER_SERVICES,
  validateP519RenderService,
} from "./p519-render-sync.mjs";
import { P520_EXECUTOR_CONTRACT } from "./p520-ramp-executor.mjs";

const API_BASE = "https://api.render.com/v1";
const DEPLOY_PATTERN = /^dep-[a-z0-9]+$/u;
const SHA_PATTERN = /^[a-f0-9]{40}$/u;
const SHA256_PATTERN = /^[a-f0-9]{64}$/u;
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const MODES = new Set(["enabled", "read_only", "off"]);
const TERMINAL_FAILURES = new Set([
  "build_failed",
  "canceled",
  "cancelled",
  "deactivated",
  "pre_deploy_failed",
  "update_failed",
]);

function requireSecret(value, code) {
  if (typeof value !== "string" || value.length < 20) throw new Error(code);
  return value;
}

function exactKeys(value, expected, code) {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).sort().join(",") !== expected.slice().sort().join(",")
  ) {
    throw new Error(code);
  }
  return value;
}

export function validateP520RenderEnvironment(environment) {
  const control = exactKeys(
    environment?.control,
    P520_EXECUTOR_CONTRACT.controlEnvironmentKeys,
    "p520_render_control_environment_invalid",
  );
  const runtime = exactKeys(
    environment?.runtime,
    P520_EXECUTOR_CONTRACT.runtimeEnvironmentKeys,
    "p520_render_runtime_environment_invalid",
  );
  const tenants = control.P5_COLLAB_20_TENANT_IDS.split(",");
  if (
    control.P5_COLLAB_20_INITIAL_MODE !== "off" ||
    tenants.length !== 2 ||
    new Set(tenants).size !== 2 ||
    tenants.some(
      (tenantId) =>
        tenantId !== tenantId.trim() ||
        tenantId !== tenantId.toLowerCase() ||
        !UUID_PATTERN.test(tenantId),
    ) ||
    !SHA_PATTERN.test(runtime.COLLAB_BUILD_ID)
  ) {
    throw new Error("p520_render_environment_value_invalid");
  }
  return environment;
}

async function responseJson(response, code) {
  if (!response.ok) throw new Error(code);
  if (response.status === 204) return undefined;
  const body = await response.text();
  if (body.trim() === "") return undefined;
  try {
    return JSON.parse(body);
  } catch {
    throw new Error(`${code}_response_invalid`);
  }
}

export function createP520RenderAdapter({
  adminToken,
  apiKey,
  controlUrl = P519_RENDER_SERVICES.control.url,
  delay = (milliseconds) =>
    new Promise((resolve) => setTimeout(resolve, milliseconds)),
  expectedTargetFingerprintSha256,
  fetchImpl = fetch,
  metricsToken,
  runtimeUrl = P519_RENDER_SERVICES.runtime.url,
  timeoutMs = 30 * 60_000,
}) {
  const credentials = {
    adminToken: requireSecret(adminToken, "p520_render_admin_token_required"),
    apiKey: requireSecret(apiKey, "p520_render_api_key_required"),
    metricsToken: requireSecret(
      metricsToken,
      "p520_render_metrics_token_required",
    ),
  };
  if (!SHA256_PATTERN.test(expectedTargetFingerprintSha256 ?? "")) {
    throw new Error("p520_render_target_fingerprint_required");
  }
  if (
    controlUrl !== P519_RENDER_SERVICES.control.url ||
    runtimeUrl !== P519_RENDER_SERVICES.runtime.url
  ) {
    throw new Error("p520_render_url_contract_mismatch");
  }

  let services;

  async function renderRequest(path, init = {}) {
    const response = await fetchImpl(`${API_BASE}${path}`, {
      ...init,
      headers: {
        accept: "application/json",
        authorization: `Bearer ${credentials.apiKey}`,
        ...(init.body ? { "content-type": "application/json" } : {}),
      },
      signal: AbortSignal.timeout(30_000),
    });
    return responseJson(response, `p520_render_api_${response.status}`);
  }

  async function findExactService(expectedName) {
    const query = new URLSearchParams({ limit: "100", name: expectedName });
    const result = await renderRequest(`/services?${query.toString()}`);
    const matches = Array.isArray(result)
      ? result
          .map((entry) => entry?.service ?? entry)
          .filter((service) => service?.name === expectedName)
      : [];
    if (matches.length !== 1) {
      throw new Error("p520_render_service_cardinality_invalid");
    }
    return validateP519RenderService(matches[0], expectedName);
  }

  async function ensureServices() {
    if (!services) {
      services = {
        control: await findExactService(P519_RENDER_SERVICES.control.name),
        runtime: await findExactService(P519_RENDER_SERVICES.runtime.name),
      };
    }
    return services;
  }

  async function updateEnvironment(service, values) {
    for (const [key, value] of Object.entries(values)) {
      await renderRequest(
        `/services/${encodeURIComponent(service.id)}/env-vars/${encodeURIComponent(key)}`,
        { body: JSON.stringify({ value }), method: "PUT" },
      );
    }
  }

  async function triggerDeploy(service, candidateSha) {
    const deploy = await renderRequest(
      `/services/${encodeURIComponent(service.id)}/deploys`,
      {
        body: JSON.stringify({
          clearCache: "do_not_clear",
          commitId: candidateSha,
        }),
        method: "POST",
      },
    );
    if (!DEPLOY_PATTERN.test(deploy?.id ?? "")) {
      throw new Error("p520_render_deploy_id_invalid");
    }
    return deploy.id;
  }

  async function waitForDeploy(service, deployId, candidateSha) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const deploy = await renderRequest(
        `/services/${encodeURIComponent(service.id)}/deploys/${encodeURIComponent(deployId)}`,
      );
      if (deploy?.status === "live") {
        const deployedCommit = deploy.commit?.id ?? deploy.commitId;
        if (deployedCommit !== candidateSha) {
          throw new Error("p520_render_deployed_commit_mismatch");
        }
        return;
      }
      if (TERMINAL_FAILURES.has(deploy?.status)) {
        throw new Error("p520_render_deploy_failed");
      }
      await delay(10_000);
    }
    throw new Error("p520_render_deploy_timeout");
  }

  async function controlRequest(path, init = {}) {
    const response = await fetchImpl(`${controlUrl}${path}`, {
      ...init,
      headers: {
        authorization: `Bearer ${credentials.adminToken}`,
        ...(init.body ? { "content-type": "application/json" } : {}),
      },
      signal: AbortSignal.timeout(20_000),
    });
    return responseJson(response, "p520_control_request_failed");
  }

  async function waitForRuntimeStatus(expectedStatus) {
    const deadline = Date.now() + Math.min(timeoutMs, 180_000);
    while (Date.now() < deadline) {
      try {
        const response = await fetchImpl(`${runtimeUrl}/readyz`, {
          signal: AbortSignal.timeout(15_000),
        });
        if (response.status === expectedStatus) return;
      } catch {
        // Render Free may briefly refuse connections while waking or draining.
      }
      await delay(1_000);
    }
    throw new Error("p520_runtime_status_timeout");
  }

  return Object.freeze({
    async assertExactTarget(target) {
      if (
        target?.services?.control?.name !== P519_RENDER_SERVICES.control.name ||
        target?.services?.control?.url !== P519_RENDER_SERVICES.control.url ||
        target?.services?.runtime?.name !== P519_RENDER_SERVICES.runtime.name ||
        target?.services?.runtime?.url !== P519_RENDER_SERVICES.runtime.url ||
        !SHA_PATTERN.test(target?.candidateSha ?? "") ||
        target?.targetFingerprintSha256 !== expectedTargetFingerprintSha256
      ) {
        throw new Error("p520_render_target_invalid");
      }
      await ensureServices();
      return { serviceCount: 2 };
    },

    async syncEnvironment(environment) {
      validateP520RenderEnvironment(environment);
      const targetServices = await ensureServices();
      await updateEnvironment(targetServices.control, environment.control);
      await updateEnvironment(targetServices.runtime, environment.runtime);
    },

    async deployCandidate(candidateSha) {
      if (!SHA_PATTERN.test(candidateSha ?? "")) {
        throw new Error("p520_render_candidate_invalid");
      }
      const targetServices = await ensureServices();
      const controlDeployId = await triggerDeploy(
        targetServices.control,
        candidateSha,
      );
      await waitForDeploy(
        targetServices.control,
        controlDeployId,
        candidateSha,
      );
      const runtimeDeployId = await triggerDeploy(
        targetServices.runtime,
        candidateSha,
      );
      await waitForDeploy(
        targetServices.runtime,
        runtimeDeployId,
        candidateSha,
      );
      return { controlDeployId, runtimeDeployId };
    },

    async setMode(mode) {
      if (!MODES.has(mode)) throw new Error("p520_render_mode_invalid");
      const state = await controlRequest("/p519/v1/state", {
        body: JSON.stringify({ authority_available: true, mode }),
        method: "PUT",
      });
      if (state?.authority_available !== true || state?.mode !== mode) {
        throw new Error("p520_render_mode_not_applied");
      }
    },

    async verifyMode(mode) {
      if (!MODES.has(mode)) throw new Error("p520_render_mode_invalid");
      const readyStatus = mode === "off" ? 503 : 200;
      await waitForRuntimeStatus(readyStatus);
      const [status, metricsResponse] = await Promise.all([
        controlRequest("/p519/v1/status"),
        fetchImpl(`${runtimeUrl}/metrics`, {
          headers: {
            authorization: `Bearer ${credentials.metricsToken}`,
          },
          signal: AbortSignal.timeout(15_000),
        }),
      ]);
      if (!metricsResponse.ok) {
        throw new Error("p520_render_metrics_unavailable");
      }
      const metrics = await metricsResponse.text();
      const documents = metricValue(metrics, "collab_documents_current");
      const editConnections = metricValue(
        metrics,
        "collab_connections_current",
        'capability="edit"',
      );
      if (
        status?.authority_available !== true ||
        status?.mode !== mode ||
        status?.tenant_count !== 2 ||
        (mode === "off" && (documents !== 0 || editConnections !== 0)) ||
        (mode === "read_only" && editConnections !== 0)
      ) {
        throw new Error("p520_render_mode_verification_failed");
      }
      return {
        authorityAvailable: true,
        documents,
        editConnections,
        mode,
        runtimeReady: readyStatus === 200,
      };
    },
  });
}
