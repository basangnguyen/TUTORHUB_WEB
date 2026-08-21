import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const DEFAULT_ENV_FILE = ".env.p5-collab-03-disposable.local";
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_03_DISPOSABLE_ONLY";

export function parseEnvFile(content) {
  const values = new Map();
  for (const rawLine of content.replace(/^\uFEFF/, "").split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (line === "" || line.startsWith("#")) {
      continue;
    }
    const separator = line.indexOf("=");
    if (separator < 1) {
      throw new Error("invalid env-file line");
    }
    const name = line.slice(0, separator).trim();
    let value = line.slice(separator + 1).trim();
    if (!/^[A-Z][A-Z0-9_]*$/u.test(name)) {
      throw new Error("invalid env-file variable name");
    }
    if (
      value.length >= 2 &&
      ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'")))
    ) {
      value = value.slice(1, -1);
    }
    if (values.has(name)) {
      throw new Error(`duplicate env-file variable: ${name}`);
    }
    values.set(name, value);
  }
  return values;
}

function parsePostgresURL(name, value, expectedUser, pooled) {
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`${name} must be a valid PostgreSQL URL`);
  }
  if (!new Set(["postgres:", "postgresql:"]).has(parsed.protocol)) {
    throw new Error(`${name} must use the PostgreSQL protocol`);
  }
  if (parsed.username !== expectedUser || parsed.password === "") {
    throw new Error(
      `${name} must use the expected dedicated role with a password`,
    );
  }
  const isPooled = parsed.hostname.includes("-pooler.");
  if (isPooled !== pooled) {
    throw new Error(`${name} has the wrong direct/pooled endpoint type`);
  }
  return parsed;
}

export function validateP503Environment(values) {
  const confirmation = values.get("P5_COLLAB_03_DISPOSABLE_CONFIRM") ?? "";
  if (confirmation !== EXACT_CONFIRMATION) {
    throw new Error(
      "P5-COLLAB-03 disposable confirmation is missing or invalid",
    );
  }
  const migration = parsePostgresURL(
    "DATABASE_MIGRATION_URL",
    values.get("DATABASE_MIGRATION_URL") ?? "",
    "neondb_owner",
    false,
  );
  const runtime = parsePostgresURL(
    "DATABASE_POOL_URL",
    values.get("DATABASE_POOL_URL") ?? "",
    "tutorhub_runtime",
    true,
  );
  if (
    migration.hostname.replace("-pooler.", ".") !==
      runtime.hostname.replace("-pooler.", ".") ||
    migration.pathname !== runtime.pathname
  ) {
    throw new Error(
      "P5-COLLAB-03 URLs must target the same disposable branch and database",
    );
  }
  return {
    DATABASE_MIGRATION_URL: migration.toString(),
    DATABASE_POOL_URL: runtime.toString(),
    P5_COLLAB_03_DISPOSABLE_CONFIRM: confirmation,
  };
}

export function main(argv = process.argv.slice(2)) {
  const file = resolve(argv[0] ?? DEFAULT_ENV_FILE);
  let safeEnvironment;
  try {
    safeEnvironment = validateP503Environment(
      parseEnvFile(readFileSync(file, "utf8")),
    );
  } catch (error) {
    process.stderr.write(
      `P5-COLLAB-03 disposable preflight failed: ${error.message}.\n`,
    );
    return 2;
  }

  process.stdout.write(
    "P5-COLLAB-03 disposable preflight passed; running PostgreSQL gate without printing credentials.\n",
  );
  const result = spawnSync(
    "go",
    [
      "test",
      "-count=1",
      "-tags=integration",
      "-run",
      "^TestP503PostgresRepositoryLifecycleAndRestore$",
      "./services/core-api/internal/modules/collaboration",
    ],
    {
      cwd: process.cwd(),
      env: { ...process.env, ...safeEnvironment },
      stdio: "inherit",
    },
  );
  if (result.error) {
    process.stderr.write("P5-COLLAB-03 disposable gate could not start Go.\n");
    return 1;
  }
  return result.status ?? 1;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  process.exitCode = main();
}
