import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import {
  parseEnvFile,
  validateP507Environment,
} from "./run-p507-disposable.mjs";

const DEFAULT_ENV_FILE = ".env.p5-collab-08-disposable.local";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_08_DISPOSABLE_ONLY";
const P507_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_07_DISPOSABLE_ONLY";

export function validateP508Environment(values) {
  if (
    (values.get("P5_COLLAB_08_DISPOSABLE_CONFIRM") ?? "") !== EXACT_CONFIRMATION
  ) {
    throw new Error(
      "P5-COLLAB-08 disposable confirmation is missing or invalid",
    );
  }
  const delegated = new Map(values);
  delegated.set("P5_COLLAB_07_DISPOSABLE_CONFIRM", P507_CONFIRMATION);
  const environment = validateP507Environment(delegated);
  return {
    ...environment,
    P5_COLLAB_08_DISPOSABLE_CONFIRM: EXACT_CONFIRMATION,
  };
}

function run(command, args, environment) {
  const windowsCommand =
    process.platform === "win32" && command.toLowerCase().endsWith(".cmd");
  const executable = windowsCommand
    ? (process.env.ComSpec ?? "cmd.exe")
    : command;
  const executableArgs = windowsCommand
    ? ["/d", "/s", "/c", command, ...args]
    : args;
  const result = spawnSync(executable, executableArgs, {
    cwd: process.cwd(),
    env: { ...process.env, ...environment },
    stdio: "inherit",
    windowsHide: true,
  });
  return result.status ?? 1;
}

function databaseVersion(environment) {
  const result = spawnSync(
    "go",
    ["run", "./services/core-api/cmd/migrate", "version"],
    {
      cwd: process.cwd(),
      encoding: "utf8",
      env: { ...process.env, ...environment },
      stdio: "pipe",
      windowsHide: true,
    },
  );
  const match = result.stdout?.trim().match(/^(\d+) (true|false)$/u);
  if (result.status !== 0 || !match) return undefined;
  return { dirty: match[2] === "true", number: Number(match[1]) };
}

export function main(argv = process.argv.slice(2)) {
  const file = resolve(argv[0] ?? DEFAULT_ENV_FILE);
  const gate = argv[1] ?? "all";
  if (!new Set(["preflight", "recovery", "all"]).has(gate)) {
    process.stderr.write(
      "P5-COLLAB-08 gate must be preflight, recovery or all.\n",
    );
    return 2;
  }

  let environment;
  try {
    environment = validateP508Environment(
      parseEnvFile(readFileSync(file, "utf8")),
    );
  } catch (error) {
    process.stderr.write(
      `P5-COLLAB-08 disposable preflight failed: ${error.message}.\n`,
    );
    return 2;
  }

  const version = databaseVersion(environment);
  if (!version || version.dirty || version.number !== 40) {
    process.stderr.write(
      "P5-COLLAB-08 preflight failed; expected clean migration version 40.\n",
    );
    return 1;
  }
  process.stdout.write(
    "P5-COLLAB-08 disposable preflight passed at 40 false; credentials remain hidden.\n",
  );
  if (gate === "preflight") return 0;

  const status = run(
    process.platform === "win32" ? "pnpm.cmd" : "pnpm",
    [
      "--filter",
      "@tutorhub/whiteboard-runtime",
      "exec",
      "vitest",
      "run",
      "src/artifactLifecycle.integration.test.ts",
    ],
    environment,
  );
  if (status !== 0) return status;

  const after = databaseVersion(environment);
  if (!after || after.dirty || after.number !== 40) {
    process.stderr.write(
      "P5-COLLAB-08 recovery postflight failed; migration ledger changed.\n",
    );
    return 1;
  }
  process.stdout.write(
    "P5-COLLAB-08 recovery gate passed; final ledger remains 40 false.\n",
  );
  return 0;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  process.exitCode = main();
}
