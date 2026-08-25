import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const blueprintPath = new URL(
  "../infrastructure/render/p5-collab-19-private-alpha.render.yaml",
  import.meta.url,
);

const EXPECTED_DOCUMENTS =
  "wb_p519_private_alpha_document_01,wb_p519_private_alpha_document_02";
const EXPECTED_ORIGIN = "https://p519-private-alpha.invalid";

const SECRET_KEYS = new Set([
  "B2_APPLICATION_KEY",
  "B2_BUCKET",
  "B2_ENDPOINT",
  "B2_KEY_ID",
  "B2_REGION",
  "COLLAB_BUILD_ID",
  "COLLAB_CONTROL_PLANE_TOKEN",
  "COLLAB_METRICS_TOKEN",
  "DATABASE_COLLABORATION_URL",
  "P5_COLLAB_19_CONTROL_ADMIN_TOKEN",
  "P5_COLLAB_19_CONTROL_TOKEN_CURRENT",
]);

function parseBlueprint(text) {
  const services = [];
  let service;
  let env;
  for (const rawLine of text.split(/\r?\n/u)) {
    const serviceStart = rawLine.match(/^ {2}- type: (\S+)$/u);
    if (serviceStart) {
      service = { env: new Map(), type: serviceStart[1] };
      services.push(service);
      env = undefined;
      continue;
    }
    if (!service) continue;
    const envStart = rawLine.match(/^ {6}- key: (\S+)$/u);
    if (envStart) {
      env = {};
      service.env.set(envStart[1], env);
      continue;
    }
    const envProperty = rawLine.match(/^ {8}(sync|value): (.+)$/u);
    if (env && envProperty) {
      env[envProperty[1]] = envProperty[2];
      continue;
    }
    const property = rawLine.match(/^ {4}([A-Za-z][A-Za-z]+): (.+)$/u);
    if (property) {
      service[property[1]] = property[2];
      env = undefined;
    }
  }
  return services;
}

function requireEnv(service, key, expected = undefined) {
  const entry = service.env.get(key);
  assert.ok(entry, `${service.name} must define ${key}`);
  if (expected !== undefined) assert.equal(entry.value, expected);
  return entry;
}

export function checkP519RenderCandidateText(text) {
  assert.doesNotMatch(text, /maxShutdownDelaySeconds/u);
  assert.doesNotMatch(text, /(?:^|[:@])latest(?:\s|$)/imu);
  const services = parseBlueprint(text);
  assert.equal(
    services.length,
    2,
    "P519 must use exactly two isolated services",
  );

  const control = services.find(
    (item) => item.name === "tutorhub-p5-p519-control-bs-20260825",
  );
  const runtime = services.find(
    (item) => item.name === "tutorhub-p5-p519-runtime-bs-20260825",
  );
  assert.ok(control, "isolated P519 control service is required");
  assert.ok(runtime, "isolated P519 runtime service is required");

  for (const service of services) {
    assert.equal(service.type, "web");
    assert.equal(service.plan, "free");
    assert.equal(service.region, "singapore");
    assert.equal(service.branch, "main");
    assert.equal(service.autoDeployTrigger, "off");
    assert.equal(service.healthCheckPath, "/livez");
  }

  assert.equal(control.runtime, "node");
  assert.equal(control.buildCommand, "node --version");
  assert.equal(control.startCommand, "node scripts/p519-live-control.mjs");
  requireEnv(
    control,
    "P5_COLLAB_19_DISPOSABLE_CONFIRM",
    "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
  );
  requireEnv(control, "P5_COLLAB_19_ALLOWED_ORIGIN", EXPECTED_ORIGIN);
  requireEnv(
    control,
    "P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES",
    EXPECTED_DOCUMENTS,
  );

  assert.equal(runtime.runtime, "docker");
  assert.equal(
    runtime.dockerfilePath,
    "./services/whiteboard-runtime/Dockerfile",
  );
  assert.equal(runtime.dockerContext, ".");
  requireEnv(runtime, "COLLAB_RUNTIME_PROFILE", "FREE_PRIVATE_ALPHA");
  requireEnv(runtime, "COLLAB_INSTANCE_COUNT", '"1"');
  requireEnv(runtime, "COLLAB_ALLOWED_ORIGINS", EXPECTED_ORIGIN);
  requireEnv(
    runtime,
    "COLLAB_CONTROL_PLANE_URL",
    "https://tutorhub-p5-p519-control-bs-20260825.onrender.com",
  );
  requireEnv(runtime, "COLLAB_DRAIN_TIMEOUT_MS", '"25000"');
  requireEnv(runtime, "COLLAB_PROBE_TIMEOUT_MS", '"5000"');

  for (const service of services) {
    for (const key of SECRET_KEYS) {
      const entry = service.env.get(key);
      if (!entry) continue;
      assert.equal(entry.sync, "false", `${key} must be dashboard-supplied`);
      assert.equal(entry.value, undefined, `${key} must not contain plaintext`);
    }
  }

  for (const key of [
    "P5_COLLAB_19_CONTROL_ADMIN_TOKEN",
    "P5_COLLAB_19_CONTROL_TOKEN_CURRENT",
  ]) {
    requireEnv(control, key);
  }
  for (const key of [
    "B2_APPLICATION_KEY",
    "B2_BUCKET",
    "B2_ENDPOINT",
    "B2_KEY_ID",
    "B2_REGION",
    "COLLAB_BUILD_ID",
    "COLLAB_CONTROL_PLANE_TOKEN",
    "COLLAB_METRICS_TOKEN",
    "DATABASE_COLLABORATION_URL",
  ]) {
    requireEnv(runtime, key);
  }

  return { serviceCount: services.length, secretCount: SECRET_KEYS.size };
}

export async function checkP519RenderCandidate() {
  return checkP519RenderCandidateText(await readFile(blueprintPath, "utf8"));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  checkP519RenderCandidate()
    .then((result) => {
      process.stdout.write(
        `P519 Render candidate static gate passed (${result.serviceCount} isolated free services).\n`,
      );
    })
    .catch(() => {
      process.stderr.write("P519 Render candidate static gate failed.\n");
      process.exitCode = 1;
    });
}
