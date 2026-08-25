import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  P519_PRIVATE_ALPHA_CONTRACT,
  createRedactedP519Summary,
  evaluateP519PrivateAlphaReport,
  redactSensitiveText,
} from "./p519-private-alpha-contract.mjs";
import {
  parseEnvFile,
  validateP507Environment,
} from "./run-p507-disposable.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const RUNTIME_ROOT = resolve(ROOT, "services/whiteboard-runtime");
const DEFAULT_ENV_FILE = ".env.p5-collab-19-disposable.local";
const DEFAULT_REDACTED_REPORT =
  "tmp/p5-collab-19-private-alpha-report.redacted.json";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY";
const EXACT_B2_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_19_B2_DISPOSABLE_ONLY";
const P507_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_07_DISPOSABLE_ONLY";
const MODES = new Set([
  "preflight",
  "binding",
  "soak",
  "drills",
  "cleanup",
  "all",
]);
export const P519_ACL_QUERY = `
  select
    r.rolname,
    c.relname,
    has_table_privilege(r.oid, c.oid, 'SELECT') as can_select,
    has_table_privilege(r.oid, c.oid, 'INSERT') as can_insert,
    has_table_privilege(r.oid, c.oid, 'UPDATE') as can_update,
    has_table_privilege(r.oid, c.oid, 'DELETE') as can_delete
  from pg_roles r
  cross join pg_class c
  join pg_namespace n on n.oid = c.relnamespace
  where r.rolname in (
    'tutorhub_runtime',
    'tutorhub_collab_worker',
    'tutorhub_poll_maintenance'
  )
    and n.nspname = 'tutorhub'
    and c.relkind in ('r', 'p')
    and c.relname ~ '^whiteboard_'
  order by r.rolname, c.relname
`;

const { createHash } = await import("node:crypto");
const SAFE_IDENTIFIER_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/u;

function sha256(value) {
  return createHash("sha256").update(String(value), "utf8").digest("hex");
}

function stableJson(value) {
  if (Array.isArray(value)) return "[" + value.map(stableJson).join(",") + "]";
  if (value && typeof value === "object") {
    return (
      "{" +
      Object.keys(value)
        .sort()
        .map((key) => JSON.stringify(key) + ":" + stableJson(value[key]))
        .join(",") +
      "}"
    );
  }
  return JSON.stringify(value);
}

export function normalizeP519Arguments(args) {
  const [envFile = DEFAULT_ENV_FILE, mode = "preflight", reportFile] = args;
  if (!MODES.has(mode)) {
    throw new Error(
      "mode must be one of preflight|soak|drills|cleanup|all|binding",
    );
  }
  if (mode !== "preflight" && !reportFile) {
    throw new Error(
      mode === "binding"
        ? "binding requires a trusted binding JSON output path"
        : mode + " requires a provider-observed report JSON path",
    );
  }
  return { envFile, mode, reportFile };
}

export function resolveP519BindingOutputPath(filePath) {
  const outputRoot = resolve(ROOT, "tmp/p5-collab-19");
  const outputPath = resolve(ROOT, filePath);
  const relativePath = relative(outputRoot, outputPath);
  if (
    !filePath.endsWith(".json") ||
    relativePath === "" ||
    relativePath.startsWith("..") ||
    isAbsolute(relativePath)
  ) {
    throw new Error(
      "trusted binding output must be a JSON file under tmp/p5-collab-19",
    );
  }
  return outputPath;
}

export function validateP519Environment(values) {
  if (
    (values.get("P5_COLLAB_19_DISPOSABLE_CONFIRM") ?? "") !== EXACT_CONFIRMATION
  ) {
    throw new Error(
      "P5-COLLAB-19 disposable confirmation is missing or invalid",
    );
  }
  const delegated = new Map(values);
  delegated.set("P5_COLLAB_07_DISPOSABLE_CONFIRM", P507_CONFIRMATION);
  const environment = validateP507Environment(delegated);
  const worker = new URL(environment.DATABASE_COLLABORATION_URL);
  if (worker.username !== "tutorhub_collab_worker") {
    throw new Error(
      "DATABASE_COLLABORATION_URL must use tutorhub_collab_worker",
    );
  }
  const explicitB2Confirmation =
    (values.get("P5_COLLAB_19_B2_DISPOSABLE_CONFIRM") ?? "") ===
    EXACT_B2_CONFIRMATION;
  if (!/disposable/iu.test(environment.B2_BUCKET) && !explicitB2Confirmation) {
    throw new Error("B2_BUCKET must be explicitly disposable");
  }
  return {
    ...environment,
    P5_COLLAB_19_B2_DISPOSABLE_CONFIRM: explicitB2Confirmation
      ? EXACT_B2_CONFIRMATION
      : "",
    P5_COLLAB_19_RUN_ID: values.get("P5_COLLAB_19_RUN_ID") ?? "",
    P5_COLLAB_19_DEPLOY_ID: values.get("P5_COLLAB_19_DEPLOY_ID") ?? "",
    P5_COLLAB_19_DISPOSABLE_CONFIRM: EXACT_CONFIRMATION,
  };
}

function secretValues(environment) {
  return Object.entries(environment)
    .filter(([name, value]) => {
      return (
        typeof value === "string" &&
        value.length >= 8 &&
        /(?:URL|TOKEN|KEY|PASSWORD|SECRET|CREDENTIAL)/u.test(name)
      );
    })
    .map(([, value]) => value)
    .sort((left, right) => right.length - left.length);
}

export function sanitizeP519Output(value, environment = {}) {
  let result = String(value);
  for (const secret of secretValues(environment)) {
    result = result.split(secret).join("[REDACTED]");
  }
  return redactSensitiveText(result);
}

function run(command, args, environment) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  const result = spawnSync(command, args, {
    cwd: ROOT,
    env: { ...process.env, ...environment },
    encoding: "utf8",
    shell: windowsCommand,
    windowsHide: true,
  });
  const stdout = sanitizeP519Output(result.stdout ?? "", environment);
  const stderr = sanitizeP519Output(result.stderr ?? "", environment);
  if (stdout) process.stdout.write(stdout);
  if (stderr) process.stderr.write(stderr);
  if (result.error) {
    throw new Error(sanitizeP519Output(result.error.message, environment));
  }
  return result.status ?? 1;
}

export function validateP519LedgerRows(rows) {
  if (
    !Array.isArray(rows) ||
    rows.length !== 1 ||
    Number(rows[0]?.version) !== 42 ||
    rows[0]?.dirty !== false
  ) {
    throw new Error(
      "Neon disposable ledger must contain exactly one row: 42 false",
    );
  }
  return [{ version: 42, dirty: false }];
}

export function exactDatabaseLedger(environment) {
  const script = [
    "const{Client}=require('pg');",
    "(async()=>{",
    "const c=new Client({connectionString:process.env.DATABASE_MIGRATION_URL});",
    "await c.connect();",
    "const r=await c.query('select version::int as version, dirty from public.tutorhub_schema_migrations order by version');",
    "console.log(JSON.stringify(r.rows));",
    "await c.end();",
    "})().catch(()=>process.exit(1));",
  ].join("");
  const result = spawnSync(process.execPath, ["-e", script], {
    cwd: RUNTIME_ROOT,
    env: { ...process.env, ...environment },
    encoding: "utf8",
    windowsHide: true,
  });
  if (result.status !== 0)
    throw new Error("Neon disposable ledger probe failed");
  let rows;
  try {
    rows = JSON.parse(result.stdout ?? "");
  } catch {
    throw new Error("Neon disposable ledger probe returned invalid data");
  }
  return validateP519LedgerRows(rows);
}

export function databaseAclSnapshot(environment) {
  const script = [
    "const{Client}=require('pg');",
    "(async()=>{",
    "const c=new Client({connectionString:process.env.DATABASE_MIGRATION_URL});",
    "await c.connect();",
    `const q=${JSON.stringify(P519_ACL_QUERY)};`,
    "const r=await c.query(q);",
    "console.log(JSON.stringify(r.rows));",
    "await c.end();",
    "})().catch(()=>process.exit(1));",
  ].join("");
  const result = spawnSync(process.execPath, ["-e", script], {
    cwd: RUNTIME_ROOT,
    env: { ...process.env, ...environment },
    encoding: "utf8",
    windowsHide: true,
  });
  if (result.status !== 0) throw new Error("Neon disposable ACL probe failed");
  try {
    const rows = JSON.parse(result.stdout ?? "");
    if (!Array.isArray(rows) || rows.length === 0) {
      throw new Error("empty");
    }
    return rows;
  } catch {
    throw new Error("Neon disposable ACL probe returned invalid data");
  }
}

export function validateP519RunMetadata(environment) {
  const runId = environment.P5_COLLAB_19_RUN_ID ?? "";
  const deployId = environment.P5_COLLAB_19_DEPLOY_ID ?? "";
  if (!SAFE_IDENTIFIER_PATTERN.test(runId)) {
    throw new Error(
      "P5_COLLAB_19_RUN_ID must be a safe provider-run identifier",
    );
  }
  if (!SAFE_IDENTIFIER_PATTERN.test(deployId)) {
    throw new Error(
      "P5_COLLAB_19_DEPLOY_ID must be a safe provider deployment identifier",
    );
  }
  return { runId, deployId };
}

function currentCommitSha() {
  const result = spawnSync("git", ["rev-parse", "HEAD"], {
    cwd: ROOT,
    encoding: "utf8",
    windowsHide: true,
  });
  const commitSha = (result.stdout ?? "").trim();
  if (result.status !== 0 || !/^[a-f0-9]{40}$/u.test(commitSha)) {
    throw new Error("Unable to resolve the trusted git commit");
  }
  return commitSha;
}

export function buildExpectedBinding(environment, ledgerRows, aclRows) {
  const metadata = validateP519RunMetadata(environment);
  const target = {
    migrationHost: new URL(environment.DATABASE_MIGRATION_URL).hostname,
    runtimeHost: new URL(environment.DATABASE_POOL_URL).hostname,
    workerHost: new URL(environment.DATABASE_COLLABORATION_URL).hostname,
    maintenanceHost: new URL(environment.DATABASE_POLL_MAINTENANCE_URL)
      .hostname,
    b2Endpoint: new URL(environment.B2_ENDPOINT).hostname,
    b2Region: environment.B2_REGION,
    b2Bucket: environment.B2_BUCKET,
  };
  return {
    schemaVersion: P519_PRIVATE_ALPHA_CONTRACT.bindingSchemaVersion,
    manifestSha256: sha256(stableJson(P519_PRIVATE_ALPHA_CONTRACT)),
    runId: metadata.runId,
    targetFingerprint: sha256(stableJson(target)),
    commitSha: currentCommitSha(),
    deployId: metadata.deployId,
    syntheticPrefix: P519_PRIVATE_ALPHA_CONTRACT.syntheticPrefix,
    ledgerProbeSha256: sha256(stableJson(ledgerRows)),
    aclProbeSha256: sha256(stableJson(aclRows)),
  };
}

export function createP519ExpectedBinding(environment) {
  const ledgerRows = exactDatabaseLedger(environment);
  const aclRows = databaseAclSnapshot(environment);
  return {
    binding: {
      ...buildExpectedBinding(environment, ledgerRows, aclRows),
      generatedAt: new Date().toISOString(),
    },
    ledgerRows,
    aclRows,
  };
}

export function writeP519TrustedBinding(filePath, binding) {
  const outputPath = resolveP519BindingOutputPath(filePath);
  mkdirSync(dirname(outputPath), { recursive: true });
  const temporaryPath = outputPath + "." + process.pid + ".tmp";
  writeFileSync(temporaryPath, JSON.stringify({ binding }, null, 2) + "\n", {
    encoding: "utf8",
    mode: 0o600,
  });
  renameSync(temporaryPath, outputPath);
  return outputPath;
}

function validateProviderReport(reportPath, expectedBinding) {
  let report;
  try {
    report = JSON.parse(readFileSync(reportPath, "utf8"));
  } catch {
    throw new Error("provider-observed report must be valid JSON");
  }
  const evaluation = evaluateP519PrivateAlphaReport(report, {
    expectedBinding,
    nowMs: Date.now(),
  });
  const summary = createRedactedP519Summary(report, evaluation);
  const outputPath = resolve(ROOT, DEFAULT_REDACTED_REPORT);
  mkdirSync(dirname(outputPath), { recursive: true });
  writeFileSync(outputPath, JSON.stringify(summary, null, 2) + "\n", {
    encoding: "utf8",
    mode: 0o600,
  });
  if (!evaluation.ok) {
    for (const error of evaluation.errors) {
      console.error("[P5-COLLAB-19] " + redactSensitiveText(error));
    }
    return 1;
  }
  console.log("[P5-COLLAB-19] provider-observed 60-minute report: PASS");
  return 0;
}

function runCleanup(environment) {
  console.log(
    "[P5-COLLAB-19] cleanup verification (database/runtime/B2 current, versions and multipart)",
  );
  return run(
    process.platform === "win32" ? "pnpm.cmd" : "pnpm",
    [
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/artifactLifecycle.integration.test.ts",
      "--maxWorkers=1",
    ],
    environment,
  );
}

function runDrillRegression(environment) {
  return run(process.execPath, ["scripts/run-p516-local.mjs"], environment);
}

export function main(args = process.argv.slice(2)) {
  const { envFile, mode, reportFile } = normalizeP519Arguments(args);
  let values;
  try {
    values = parseEnvFile(readFileSync(resolve(ROOT, envFile), "utf8"));
  } catch {
    console.error("[P5-COLLAB-19] unable to load disposable environment");
    return 1;
  }

  let environment;
  try {
    environment = validateP519Environment(values);
  } catch (error) {
    console.error("[P5-COLLAB-19] " + sanitizeP519Output(error.message, {}));
    return 1;
  }
  environment.GOCACHE = resolve(ROOT, ".tmp-gocache");

  console.log(
    "[P5-COLLAB-19] " +
      mode +
      " on validated disposable providers (no migration, rollback or shared staging)",
  );
  try {
    if (
      run(
        process.execPath,
        ["scripts/check-p519-private-alpha.mjs"],
        environment,
      ) !== 0
    ) {
      return 1;
    }
    const ledgerRows = exactDatabaseLedger(environment);
    const aclRows = databaseAclSnapshot(environment);
    if (mode === "preflight") {
      console.log("[P5-COLLAB-19] disposable preflight: PASS");
      return 0;
    }

    const expectedBinding = buildExpectedBinding(
      environment,
      ledgerRows,
      aclRows,
    );
    if (mode === "binding") {
      writeP519TrustedBinding(reportFile, {
        ...expectedBinding,
        generatedAt: new Date().toISOString(),
      });
      console.log("[P5-COLLAB-19] trusted run binding: PASS");
      return 0;
    }
    let status = 0;
    try {
      status = validateProviderReport(
        resolve(ROOT, reportFile),
        expectedBinding,
      );
      if (status === 0 && (mode === "drills" || mode === "all")) {
        status = runDrillRegression(environment);
      }
      if (status === 0 && mode === "cleanup") {
        status = runCleanup(environment);
      }
    } finally {
      if (mode !== "cleanup") {
        const cleanupStatus = runCleanup(environment);
        if (status === 0) status = cleanupStatus;
      }
    }
    exactDatabaseLedger(environment);
    if (status === 0) {
      console.log("[P5-COLLAB-19] " + mode + ": PASS");
    }
    return status;
  } catch (error) {
    console.error(
      "[P5-COLLAB-19] " + sanitizeP519Output(error.message, environment),
    );
    return 1;
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  process.exitCode = main();
}
