import { spawn, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
import process from "node:process";

const scriptPath = fileURLToPath(import.meta.url);
const repositoryRoot = path.resolve(path.dirname(scriptPath), "..");
const databaseURL =
  "postgresql://tutorhub:tutorhub_local@localhost:5432/tutorhub?sslmode=disable";

const cloudConfigurationKeys = [
  "SESSION_SECRET",
  "OIDC_ISSUER_URL",
  "OIDC_CLIENT_ID",
  "OIDC_CLIENT_SECRET",
  "B2_ENDPOINT",
  "B2_REGION",
  "B2_BUCKET",
  "B2_KEY_ID",
  "B2_APPLICATION_KEY",
  "LIVEKIT_URL",
  "LIVEKIT_API_KEY",
  "LIVEKIT_API_SECRET",
];

export function buildLocalEnvironment(baseEnvironment = {}) {
  const localEnvironment = {
    ...baseEnvironment,
    APP_ENV: "development",
    PORT: "8080",
    PUBLIC_WEB_ORIGIN: "http://localhost:5173",
    PUBLIC_API_ORIGIN: "http://localhost:8080",
    VITE_API_BASE_URL: "/api",
    DATABASE_POOL_URL: databaseURL,
    DATABASE_MIGRATION_URL: databaseURL,
    REDIS_URL: "redis://localhost:6379/0",
    SESSION_COOKIE_SECURE: "false",
  };

  for (const key of cloudConfigurationKeys) {
    localEnvironment[key] = "";
  }

  return localEnvironment;
}

export function parseArguments(argumentsList) {
  const command = argumentsList[0] ?? "help";
  const allowedCommands = new Set([
    "setup",
    "dev",
    "status",
    "down",
    "reset",
    "help",
  ]);

  if (!allowedCommands.has(command)) {
    throw new Error(`Unknown local environment command: ${command}`);
  }
  if (command === "reset" && !argumentsList.includes("--yes")) {
    throw new Error(
      "Reset removes all local TutorHub database and Redis data. Re-run with --yes.",
    );
  }

  return { command };
}

function executable(name) {
  return process.platform === "win32" ? `${name}.exe` : name;
}

function run(
  command,
  argumentsList,
  environment = buildLocalEnvironment(process.env),
) {
  const result = spawnSync(command, argumentsList, {
    cwd: repositoryRoot,
    env: environment,
    stdio: "inherit",
  });

  if (result.error) {
    throw new Error(`Could not run ${command}: ${result.error.message}`);
  }
  if (result.status !== 0) {
    throw new Error(`${command} exited with status ${result.status}`);
  }
}

function runCompose(argumentsList, environment) {
  run(executable("docker"), ["compose", ...argumentsList], environment);
}

export function setupLocalEnvironment(
  environment = buildLocalEnvironment(process.env),
) {
  runCompose(["up", "--detach", "--wait", "--remove-orphans"], environment);
  run(
    executable("go"),
    ["run", "./services/core-api/cmd/migrate", "up"],
    environment,
  );
  run(executable("go"), ["run", "./services/core-api/cmd/seed"], environment);
}

function stopChildren(children) {
  for (const child of children) {
    if (!child.killed) {
      child.kill("SIGTERM");
    }
  }
}

async function runDevelopmentServers(environment) {
  setupLocalEnvironment(environment);

  const children = [
    spawn(executable("go"), ["run", "./services/core-api/cmd/api"], {
      cwd: repositoryRoot,
      env: environment,
      stdio: "inherit",
    }),
    spawn(
      process.platform === "win32" ? "corepack.cmd" : "corepack",
      ["pnpm", "--filter", "@tutorhub/web", "dev"],
      {
        cwd: repositoryRoot,
        env: environment,
        stdio: "inherit",
      },
    ),
  ];

  const stop = () => stopChildren(children);
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);

  const exitCode = await Promise.race(
    children.map(
      (child) =>
        new Promise((resolve, reject) => {
          child.once("error", reject);
          child.once("exit", (code) => resolve(code ?? 1));
        }),
    ),
  );

  stop();
  process.removeListener("SIGINT", stop);
  process.removeListener("SIGTERM", stop);

  if (exitCode !== 0) {
    throw new Error(`Local development server exited with status ${exitCode}`);
  }
}

function printHelp() {
  process.stdout.write(`TutorHub local environment\n\n`);
  process.stdout.write(`  setup       Start dependencies, migrate, and seed\n`);
  process.stdout.write(`  dev         Run setup, Core API, and web app\n`);
  process.stdout.write(`  status      Show dependency status\n`);
  process.stdout.write(`  down        Stop local dependencies\n`);
  process.stdout.write(`  reset --yes Delete local data, then recreate it\n`);
}

export async function main(argumentsList) {
  const { command } = parseArguments(argumentsList);
  const environment = buildLocalEnvironment(process.env);

  switch (command) {
    case "setup":
      setupLocalEnvironment(environment);
      break;
    case "dev":
      await runDevelopmentServers(environment);
      break;
    case "status":
      runCompose(["ps"], environment);
      break;
    case "down":
      runCompose(["down", "--remove-orphans"], environment);
      break;
    case "reset":
      runCompose(["down", "--volumes", "--remove-orphans"], environment);
      setupLocalEnvironment(environment);
      break;
    case "help":
      printHelp();
      break;
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === scriptPath) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
