import { createHash } from "node:crypto";
import {
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  realpathSync,
  renameSync,
  writeFileSync,
} from "node:fs";
import {
  basename,
  dirname,
  extname,
  isAbsolute,
  relative,
  resolve,
} from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  P520_RAMP_EXIT_CONTRACT,
  containsP520SecretMaterial,
  evaluateP520RampExitPlan,
} from "./p520-ramp-exit-contract.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const PRIVATE_TMP_ROOT = resolve(ROOT, "tmp", "p5-collab-20");
const MAX_INPUT_BYTES = 64 * 1024;
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const MANIFEST_SCHEMA = "p5-collab-20-tenant-allowlist-v1";
const HASH_DOMAIN = `${MANIFEST_SCHEMA}\n`;

function digestTenantIds(tenantIds) {
  return createHash("sha256")
    .update(`${HASH_DOMAIN}${tenantIds.join("\n")}\n`)
    .digest("hex");
}

function assertPrivateJsonPath(inputPath, label) {
  if (typeof inputPath !== "string" || inputPath.trim() === "") {
    throw new Error(`p520_${label}_path_required`);
  }
  const absolutePath = resolve(ROOT, inputPath);
  const relativePath = relative(PRIVATE_TMP_ROOT, absolutePath);
  if (
    relativePath === "" ||
    relativePath.startsWith("..") ||
    isAbsolute(relativePath) ||
    extname(absolutePath).toLowerCase() !== ".json" ||
    basename(absolutePath).toLowerCase().startsWith(".env")
  ) {
    throw new Error(`p520_${label}_path_outside_private_tmp`);
  }
  return absolutePath;
}

export function resolveP520TenantInput(inputPath) {
  return assertPrivateJsonPath(inputPath, "tenant_input");
}

export function resolveP520TenantOutput(outputPath) {
  return assertPrivateJsonPath(outputPath, "tenant_output");
}

export function assertP520TenantOutputAvailable(path, exists = existsSync) {
  if (exists(path)) throw new Error("p520_tenant_output_exists");
  return path;
}

function parseBoundedPrivateJson(path) {
  const information = lstatSync(path);
  const realPrivateRoot = realpathSync(PRIVATE_TMP_ROOT);
  const realInput = realpathSync(path);
  const inputFromPrivateRoot = relative(realPrivateRoot, realInput);
  if (
    !information.isFile() ||
    information.isSymbolicLink() ||
    inputFromPrivateRoot.startsWith("..") ||
    isAbsolute(inputFromPrivateRoot)
  ) {
    throw new Error("p520_tenant_input_realpath_invalid");
  }
  const raw = readFileSync(path);
  if (raw.byteLength === 0 || raw.byteLength > MAX_INPUT_BYTES) {
    throw new Error("p520_tenant_input_size_invalid");
  }
  try {
    return JSON.parse(raw.toString("utf8"));
  } catch {
    throw new Error("p520_tenant_input_json_invalid");
  }
}

export function canonicalizeP520TenantManifest(manifest) {
  if (
    !manifest ||
    typeof manifest !== "object" ||
    Array.isArray(manifest) ||
    Object.keys(manifest).sort().join(",") !== "schemaVersion,tenantIds" ||
    manifest.schemaVersion !== MANIFEST_SCHEMA ||
    !Array.isArray(manifest.tenantIds) ||
    manifest.tenantIds.length !== P520_RAMP_EXIT_CONTRACT.initialRampTenantCount
  ) {
    throw new Error("p520_tenant_manifest_invalid");
  }
  const tenantIds = manifest.tenantIds.map((tenantId) => {
    if (
      typeof tenantId !== "string" ||
      tenantId !== tenantId.trim() ||
      tenantId !== tenantId.toLowerCase() ||
      !UUID_PATTERN.test(tenantId)
    ) {
      throw new Error("p520_tenant_uuid_invalid");
    }
    return tenantId;
  });
  if (new Set(tenantIds).size !== tenantIds.length) {
    throw new Error("p520_tenant_uuid_duplicate");
  }
  tenantIds.sort();
  return {
    tenantCount: tenantIds.length,
    tenantAllowlistSha256: digestTenantIds(tenantIds),
  };
}

export function bindP520TenantAllowlist(packet, manifest) {
  const evaluation = evaluateP520RampExitPlan(packet);
  if (
    !evaluation.ok ||
    evaluation.liveRampAllowed ||
    packet?.status !== "preparation-only" ||
    packet?.preparation?.proposedTarget?.requiredTenantCount !==
      P520_RAMP_EXIT_CONTRACT.initialRampTenantCount ||
    packet?.preparation?.proposedTarget?.tenantAllowlistSha256 !== null ||
    packet?.preparation?.proposedTarget?.tenantCount !== null
  ) {
    throw new Error("p520_tenant_packet_not_bindable");
  }
  const binding = canonicalizeP520TenantManifest(manifest);
  const boundPacket = structuredClone(packet);
  boundPacket.preparation.proposedTarget.tenantAllowlistSha256 =
    binding.tenantAllowlistSha256;
  boundPacket.preparation.proposedTarget.tenantCount = binding.tenantCount;
  const boundEvaluation = evaluateP520RampExitPlan(boundPacket);
  if (
    !boundEvaluation.ok ||
    boundEvaluation.liveRampAllowed ||
    containsP520SecretMaterial(boundPacket)
  ) {
    throw new Error("p520_tenant_bound_packet_invalid");
  }
  return { boundPacket, binding };
}

function writePrivatePacket(path, packet) {
  assertP520TenantOutputAvailable(path);
  mkdirSync(dirname(path), { recursive: true });
  const realWorkspace = realpathSync(ROOT);
  const realPrivateRoot = realpathSync(PRIVATE_TMP_ROOT);
  const realOutputDirectory = realpathSync(dirname(path));
  const privateRootFromWorkspace = relative(realWorkspace, realPrivateRoot);
  const outputFromPrivateRoot = relative(realPrivateRoot, realOutputDirectory);
  if (
    privateRootFromWorkspace.startsWith("..") ||
    isAbsolute(privateRootFromWorkspace) ||
    outputFromPrivateRoot.startsWith("..") ||
    isAbsolute(outputFromPrivateRoot)
  ) {
    throw new Error("p520_tenant_output_realpath_invalid");
  }
  const temporaryPath = `${path}.${process.pid}.tmp`;
  writeFileSync(temporaryPath, `${JSON.stringify(packet, null, 2)}\n`, {
    encoding: "utf8",
    mode: 0o600,
    flag: "wx",
  });
  renameSync(temporaryPath, path);
}

export function parseP520TenantBindArgs(args) {
  const values = new Map();
  for (let index = 0; index < args.length; index += 1) {
    const key = args[index];
    if (key === "--execute" || key === "--live") {
      throw new Error("p520_tenant_live_execution_not_implemented");
    }
    if (!new Set(["--packet", "--tenants", "--output"]).has(key)) {
      throw new Error("p520_tenant_bind_usage_invalid");
    }
    const value = args[index + 1];
    if (!value || value.startsWith("--") || values.has(key)) {
      throw new Error("p520_tenant_bind_usage_invalid");
    }
    values.set(key, value);
    index += 1;
  }
  if (
    !values.has("--packet") ||
    !values.has("--tenants") ||
    !values.has("--output")
  ) {
    throw new Error("p520_tenant_bind_usage_invalid");
  }
  return {
    packetPath: resolveP520TenantInput(values.get("--packet")),
    tenantPath: resolveP520TenantInput(values.get("--tenants")),
    outputPath: resolveP520TenantOutput(values.get("--output")),
  };
}

export function runP520TenantBindCli(args = process.argv.slice(2)) {
  const paths = parseP520TenantBindArgs(args);
  const packet = parseBoundedPrivateJson(paths.packetPath);
  const manifest = parseBoundedPrivateJson(paths.tenantPath);
  const { boundPacket, binding } = bindP520TenantAllowlist(packet, manifest);
  writePrivatePacket(paths.outputPath, boundPacket);
  process.stdout.write(
    `${JSON.stringify({
      outcome: "pass",
      status: boundPacket.status,
      tenantAllowlistBound: true,
      tenantCount: binding.tenantCount,
      liveRampAllowed: false,
      providerMutationAuthorized: false,
      file: relative(ROOT, paths.outputPath).replaceAll("\\", "/"),
    })}\n`,
  );
  return 0;
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  try {
    process.exitCode = runP520TenantBindCli();
  } catch {
    process.stderr.write(
      `${JSON.stringify({
        outcome: "blocked",
        reason: "p520_tenant_binding_failed",
      })}\n`,
    );
    process.exitCode = 1;
  }
}
