import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { parseEnvFile } from "./run-p507-disposable.mjs";

const DEFAULT_ENV_FILE = ".env.p5-collab-09-disposable.local";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_09_DISPOSABLE_ONLY";
const ACCEPTED_FORWARD_BASELINES = new Set([37, 38, 39, 40, 41]);

function postgresURL(name, raw, expectedUser, pooled) {
  let value;
  try {
    value = new URL(raw);
  } catch {
    throw new Error(`${name} must be a valid PostgreSQL URL`);
  }
  if (
    !new Set(["postgres:", "postgresql:"]).has(value.protocol) ||
    !value.hostname ||
    !value.pathname.slice(1) ||
    value.username !== expectedUser ||
    !value.password ||
    value.searchParams.get("sslmode") !== "require"
  ) {
    throw new Error(
      `${name} must use the expected password-authenticated TLS role`,
    );
  }
  if (value.hostname.includes("-pooler.") !== pooled) {
    throw new Error(`${name} has the wrong direct/pooled endpoint type`);
  }
  return value;
}

export function validateP509Environment(values) {
  if (
    (values.get("P5_COLLAB_09_DISPOSABLE_CONFIRM") ?? "") !== EXACT_CONFIRMATION
  ) {
    throw new Error(
      "P5-COLLAB-09 disposable confirmation is missing or invalid",
    );
  }
  const migration = postgresURL(
    "DATABASE_MIGRATION_URL",
    values.get("DATABASE_MIGRATION_URL") ?? "",
    "neondb_owner",
    false,
  );
  const runtime = postgresURL(
    "DATABASE_POOL_URL",
    values.get("DATABASE_POOL_URL") ?? "",
    "tutorhub_runtime",
    true,
  );
  const worker = postgresURL(
    "DATABASE_COLLABORATION_URL",
    values.get("DATABASE_COLLABORATION_URL") ?? "",
    "tutorhub_collab_worker",
    false,
  );
  const urls = [migration, runtime, worker];
  const hosts = new Set(
    urls.map((value) => value.hostname.replace("-pooler.", ".")),
  );
  const databases = new Set(urls.map((value) => value.pathname));
  const roles = new Set(urls.map((value) => value.username));
  if (hosts.size !== 1 || databases.size !== 1 || roles.size !== 3) {
    throw new Error(
      "P5-COLLAB-09 requires three distinct intended roles on one disposable branch and database",
    );
  }
  return {
    DATABASE_COLLABORATION_URL: worker.toString(),
    DATABASE_MIGRATION_URL: migration.toString(),
    DATABASE_POOL_URL: runtime.toString(),
    P5_COLLAB_09_DISPOSABLE_CONFIRM: EXACT_CONFIRMATION,
  };
}

function spawn(command, args, environment, silent = false) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  const result = spawnSync(
    windowsCommand ? (process.env.ComSpec ?? "cmd.exe") : command,
    windowsCommand ? ["/d", "/s", "/c", command, ...args] : args,
    {
      cwd: process.cwd(),
      encoding: silent ? "utf8" : undefined,
      env: { ...process.env, ...environment },
      stdio: silent ? "pipe" : "inherit",
      windowsHide: true,
    },
  );
  return result;
}

function run(command, args, environment) {
  return spawn(command, args, environment).status ?? 1;
}

function databaseVersion(environment) {
  const result = spawn(
    "go",
    ["run", "./services/core-api/cmd/migrate", "version"],
    environment,
    true,
  );
  const match = result.stdout?.trim().match(/^(\d+) (true|false)$/u);
  if (result.status !== 0 || !match) return undefined;
  return { dirty: match[2] === "true", number: Number(match[1]) };
}

function migrate(environment) {
  const before = databaseVersion(environment);
  if (
    !before ||
    before.dirty ||
    !ACCEPTED_FORWARD_BASELINES.has(before.number)
  ) {
    process.stderr.write(
      "P5-COLLAB-09 migration preflight failed; expected a clean forward-only P5 baseline from 37 through 41.\n",
    );
    return 1;
  }
  for (const label of ["forward", "idempotent rerun"]) {
    const result = spawn(
      "go",
      ["run", "./services/core-api/cmd/migrate", "up"],
      environment,
      true,
    );
    if (result.status !== 0) {
      process.stderr.write(`P5-COLLAB-09 ${label} migration failed.\n`);
      return 1;
    }
    const after = databaseVersion(environment);
    if (!after || after.dirty || after.number !== 41) {
      process.stderr.write(
        `P5-COLLAB-09 ${label} migration postflight failed.\n`,
      );
      return 1;
    }
  }
  process.stdout.write(
    `P5-COLLAB-09 migration gate passed: ${before.number} false -> 41 false -> 41 false.\n`,
  );
  return 0;
}

function runGoIntegration(testName, environment) {
  return run(
    "go",
    [
      "test",
      "-count=1",
      "-tags=integration",
      "-run",
      `^${testName}$`,
      "./services/core-api/internal/modules/collaboration",
    ],
    environment,
  );
}

export function main(argv = process.argv.slice(2)) {
  const file = resolve(argv[0] ?? DEFAULT_ENV_FILE);
  const gate = argv[1] ?? "all";
  if (
    !new Set(["preflight", "migration", "database", "runtime", "all"]).has(gate)
  ) {
    process.stderr.write(
      "P5-COLLAB-09 gate must be preflight, migration, database, runtime or all.\n",
    );
    return 2;
  }

  let environment;
  try {
    environment = validateP509Environment(
      parseEnvFile(readFileSync(file, "utf8")),
    );
  } catch (error) {
    process.stderr.write(
      `P5-COLLAB-09 disposable preflight failed: ${error.message}.\n`,
    );
    return 2;
  }
  if (runGoIntegration("TestP509RolePreflight", environment) !== 0) return 1;
  const version = databaseVersion(environment);
  if (!version) {
    process.stderr.write(
      "P5-COLLAB-09 preflight failed; migration ledger could not be read.\n",
    );
    return 1;
  }
  if (version.dirty || !ACCEPTED_FORWARD_BASELINES.has(version.number)) {
    process.stderr.write(
      `P5-COLLAB-09 preflight failed; ledger is ${version.number} ${version.dirty}, expected a clean forward-only P5 baseline from 37 through 41.\n`,
    );
    return 1;
  }
  process.stdout.write(
    `P5-COLLAB-09 disposable preflight passed at ${version.number} false; credentials remain hidden.\n`,
  );
  if (gate === "preflight") return 0;

  if ((gate === "migration" || gate === "all") && migrate(environment) !== 0) {
    return 1;
  }
  if (gate === "migration") return 0;

  if (gate === "database" || gate === "all") {
    if (
      runGoIntegration(
        "TestP509FeatureQuotaOperationsPostgres",
        environment,
      ) !== 0
    ) {
      return 1;
    }
    if (gate === "database") return 0;
  }

  if (gate === "runtime" || gate === "all") {
    if (
      run(
        "go",
        [
          "test",
          "-count=1",
          "-run",
          "^(TestServiceReadOnlyModePreservesReadExportAndClampsGrant|TestServiceOffModeConcealsAllPublicWhiteboardReadsAndWrites)$",
          "./services/core-api/internal/modules/collaboration",
        ],
        environment,
      ) !== 0
    ) {
      return 1;
    }
    if (
      run(
        process.platform === "win32" ? "pnpm.cmd" : "pnpm",
        [
          "--filter",
          "@tutorhub/whiteboard-runtime",
          "exec",
          "vitest",
          "run",
          "src/runtimePolicy.test.ts",
          "src/runtimeServer.test.ts",
          "src/telemetry.test.ts",
        ],
        environment,
      ) !== 0
    ) {
      return 1;
    }
  }

  const finalVersion = databaseVersion(environment);
  if (!finalVersion || finalVersion.dirty || finalVersion.number !== 41) {
    process.stderr.write(
      "P5-COLLAB-09 postflight failed; final ledger is not 41 false.\n",
    );
    return 1;
  }
  process.stdout.write(
    "P5-COLLAB-09 disposable gate passed; final ledger remains 41 false.\n",
  );
  return 0;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  process.exitCode = main();
}
