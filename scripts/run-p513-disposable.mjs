import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import {
  parseEnvFile,
  validateP507Environment,
} from "./run-p507-disposable.mjs";

const DEFAULT_ENV_FILE = ".env.p5-collab-13-disposable.local";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_13_DISPOSABLE_ONLY";
const ACL_CONFIRMATION =
  "I_UNDERSTAND_P5_COLLAB_13_ACL_PROVISION_DISPOSABLE_ONLY";
const P507_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_07_DISPOSABLE_ONLY";

export function validateP513Environment(values) {
  if (
    (values.get("P5_COLLAB_13_DISPOSABLE_CONFIRM") ?? "") !== EXACT_CONFIRMATION
  ) {
    throw new Error(
      "P5-COLLAB-13 disposable confirmation is missing or invalid",
    );
  }
  const delegated = new Map(values);
  delegated.set("P5_COLLAB_07_DISPOSABLE_CONFIRM", P507_CONFIRMATION);
  const environment = validateP507Environment(delegated);
  return {
    ...environment,
    P5_COLLAB_13_ACL_PROVISION_CONFIRM: ACL_CONFIRMATION,
    P5_COLLAB_13_DISPOSABLE_CONFIRM: EXACT_CONFIRMATION,
  };
}

function run(command, args, environment, silent = false) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  return spawnSync(
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
}

function databaseVersion(environment) {
  const result = run(
    "go",
    ["run", "./services/core-api/cmd/migrate", "version"],
    environment,
    true,
  );
  const match = result.stdout?.trim().match(/^(\d+) (true|false)$/u);
  if (result.status !== 0 || !match) return undefined;
  return { dirty: match[2] === "true", number: Number(match[1]) };
}

function goTest(pattern, environment) {
  return (
    run(
      "go",
      [
        "test",
        "-count=1",
        "-tags=integration",
        "-run",
        pattern,
        "./services/core-api/internal/modules/collaboration",
      ],
      environment,
    ).status ?? 1
  );
}

export function main(argv = process.argv.slice(2)) {
  const file = resolve(argv[0] ?? DEFAULT_ENV_FILE);
  const gate = argv[1] ?? "all";
  if (
    !new Set(["preflight", "migration", "database", "provider", "all"]).has(
      gate,
    )
  ) {
    process.stderr.write(
      "P5-COLLAB-13 gate must be preflight, migration, database, provider or all.\n",
    );
    return 2;
  }

  let environment;
  try {
    environment = validateP513Environment(
      parseEnvFile(readFileSync(file, "utf8")),
    );
  } catch (error) {
    process.stderr.write(
      `P5-COLLAB-13 disposable preflight failed: ${error.message}.\n`,
    );
    return 2;
  }

  const version = databaseVersion(environment);
  const allowedVersions =
    gate === "migration" ? new Set([37, 41]) : new Set([41]);
  if (!version || version.dirty || !allowedVersions.has(version.number)) {
    const observed = version
      ? `${version.number} ${version.dirty}`
      : "unavailable";
    process.stderr.write(
      `P5-COLLAB-13 requires a clean disposable database at ${gate === "migration" ? "37 or 41" : "41"} false; observed ${observed}; no migration was applied.\n`,
    );
    return 1;
  }
  process.stdout.write(
    `P5-COLLAB-13 disposable preflight passed at ${version.number} false; credentials remain hidden.\n`,
  );
  if (gate === "preflight") return 0;

  if (gate === "migration") {
    if (
      (run(
        "go",
        ["run", "./services/core-api/cmd/migrate", "up"],
        environment,
        true,
      ).status ?? 1) !== 0
    ) {
      process.stderr.write(
        "P5-COLLAB-13 disposable forward migration failed.\n",
      );
      return 1;
    }
    const migrated = databaseVersion(environment);
    if (!migrated || migrated.dirty || migrated.number !== 41) {
      process.stderr.write(
        "P5-COLLAB-13 disposable forward migration did not finish at 41 false.\n",
      );
      return 1;
    }
    if (
      (run(
        "go",
        ["run", "./services/core-api/cmd/migrate", "up"],
        environment,
        true,
      ).status ?? 1) !== 0
    ) {
      process.stderr.write(
        "P5-COLLAB-13 disposable idempotent migration rerun failed.\n",
      );
      return 1;
    }
    const idempotent = databaseVersion(environment);
    if (!idempotent || idempotent.dirty || idempotent.number !== 41) {
      process.stderr.write(
        "P5-COLLAB-13 disposable idempotent migration changed the ledger.\n",
      );
      return 1;
    }
    process.stdout.write(
      `P5-COLLAB-13 disposable migration passed: ${version.number} false -> 41 false -> 41 false.\n`,
    );
    return 0;
  }

  if (gate === "database" || gate === "all") {
    if (
      goTest(
        "^TestProvisionP513WhiteboardArtifactWorkerExactACL$",
        environment,
      ) !== 0
    ) {
      return 1;
    }
    if (
      goTest("^TestWhiteboardControlPlanePostgresGates$", environment) !== 0
    ) {
      return 1;
    }
    if (gate === "database") return 0;
  }

  if (gate === "provider" || gate === "all") {
    const status =
      run(
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
      ).status ?? 1;
    if (status !== 0) return status;
  }

  const after = databaseVersion(environment);
  if (!after || after.dirty || after.number !== 41) {
    process.stderr.write(
      "P5-COLLAB-13 postflight failed; disposable migration ledger changed.\n",
    );
    return 1;
  }
  process.stdout.write(
    "P5-COLLAB-13 disposable snapshot/restore gates passed; final ledger remains 41 false.\n",
  );
  return 0;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  process.exitCode = main();
}
