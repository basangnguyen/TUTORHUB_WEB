import { spawn, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import process from "node:process";

const scriptPath = fileURLToPath(import.meta.url);
const repositoryRoot = path.resolve(path.dirname(scriptPath), "..");
const e2eDatabaseName = "tutorhub_e2e";
const defaultDatabaseURL =
  "postgresql://tutorhub:tutorhub_local@127.0.0.1:5432/tutorhub_e2e?sslmode=disable";
const childShutdownGraceMilliseconds = 5_000;
const childShutdownForceMilliseconds = 2_000;

const localOrigins = Object.freeze({
  api: "http://127.0.0.1:8080",
  issuer: "http://127.0.0.1:9091",
  // AUTH-05: a second fake issuer stands in for Google (native auth only).
  googleIssuer: "http://127.0.0.1:9092",
  web: "http://127.0.0.1:5173",
});

/** Fixture client ID for the loopback fake Google; never a real client. */
export const googleFixtureClientID = "tutorhub-e2e-google";

const clearedConfigurationKeys = [
  "B2_ENDPOINT",
  "B2_REGION",
  "B2_BUCKET",
  "B2_KEY_ID",
  "B2_APPLICATION_KEY",
  "LIVEKIT_URL",
  "LIVEKIT_API_KEY",
  "LIVEKIT_API_SECRET",
];

export const localAccounts = Object.freeze({
  admin: Object.freeze({
    id: "admin",
    email: "admin.e2e@tutorhub.local",
    displayName: "E2E Administrator",
  }),
  teacher: Object.freeze({
    id: "teacher",
    email: "teacher.e2e@tutorhub.local",
    displayName: "E2E Teacher",
  }),
  student: Object.freeze({
    id: "student",
    email: "student.e2e@tutorhub.local",
    displayName: "E2E Student",
  }),
});

/**
 * AUTH-02/03/04 native sign-up, password login, email-code login, password
 * recovery, step-up re-authentication and session/device management, plus
 * AUTH-05 Google federation against a loopback fake issuer, for the loopback
 * stack only. Applied solely when E2E_AUTH_NATIVE=true (or `--native-auth`),
 * so the default `pnpm e2e` environment stays byte-for-byte unchanged. The
 * Google client secret is generated per run (see createRuntimeSecrets).
 */
export const nativeAuthConfiguration = Object.freeze({
  AUTH_SIGNUP_ENABLED: "true",
  AUTH_METHOD_PASSWORD_ENABLED: "true",
  // Also enables password recovery (password AND email code on the server).
  AUTH_METHOD_EMAIL_CODE_ENABLED: "true",
  AUTH_EMAIL_SENDER: "devinbox",
  // Keep the run hermetic: never call the HIBP range API from E2E.
  AUTH_PASSWORD_BREACH_CHECK: "offline",
  // AUTH-04: /me/security, /me/login-methods, /me/sessions and /auth/reauth.
  AUTH_DEVICE_MANAGEMENT_ENABLED: "true",
  // AUTH-05: Google federation against the fake issuer (APP_ENV=test only).
  AUTH_PROVIDER_GOOGLE_ENABLED: "true",
  GOOGLE_CLIENT_ID: googleFixtureClientID,
  GOOGLE_ISSUER_URL: localOrigins.googleIssuer,
});

const commandOptions = Object.freeze({
  "--native-auth": "nativeAuth",
});

export function parseArguments(argumentsList) {
  const [command = "help", ...optionsList] = argumentsList;
  if (!new Set(["serve", "prepare", "help"]).has(command)) {
    throw new Error(`Unknown E2E environment command: ${command}`);
  }
  const options = { nativeAuth: false };
  for (const option of optionsList) {
    const name = Object.hasOwn(commandOptions, option)
      ? commandOptions[option]
      : undefined;
    if (!name) {
      throw new Error(`Unknown E2E environment option: ${option}`);
    }
    options[name] = true;
  }
  return { command, ...options };
}

/** Maps CLI flags to environment toggles without shell-specific syntax. */
export function resolveBaseEnvironment(
  { nativeAuth = false } = {},
  baseEnvironment = process.env,
) {
  return nativeAuth
    ? { ...baseEnvironment, E2E_AUTH_NATIVE: "true" }
    : baseEnvironment;
}

export function createRuntimeSecrets() {
  return {
    clientSecret: randomBytes(32).toString("base64url"),
    googleClientSecret: randomBytes(32).toString("base64url"),
    sessionSecret: randomBytes(48).toString("base64"),
  };
}

export function validateDatabaseConfiguration(baseEnvironment = {}) {
  const mode = baseEnvironment.E2E_DATABASE_MODE?.trim() || "managed";
  if (mode !== "managed" && mode !== "external") {
    throw new Error("E2E_DATABASE_MODE must be either managed or external.");
  }
  const databaseURL =
    baseEnvironment.E2E_DATABASE_URL?.trim() || defaultDatabaseURL;
  let parsed;
  try {
    parsed = new URL(databaseURL);
  } catch {
    throw new Error("E2E_DATABASE_URL must be a valid PostgreSQL URL.");
  }
  if (
    !["postgres:", "postgresql:"].includes(parsed.protocol) ||
    !isLoopbackHost(parsed.hostname) ||
    parsed.pathname !== `/${e2eDatabaseName}`
  ) {
    throw new Error(
      `E2E_DATABASE_URL must target the loopback ${e2eDatabaseName} database.`,
    );
  }
  const queryParameters = [...parsed.searchParams.entries()];
  if (
    queryParameters.length > 1 ||
    (queryParameters.length === 1 &&
      (queryParameters[0][0] !== "sslmode" ||
        queryParameters[0][1] !== "disable"))
  ) {
    throw new Error(
      "E2E_DATABASE_URL query parameters may only contain sslmode=disable.",
    );
  }
  return { databaseURL, mode };
}

export function buildE2EEnvironment(
  baseEnvironment = {},
  runtimeSecrets = createRuntimeSecrets(),
) {
  const { databaseURL } = validateDatabaseConfiguration(baseEnvironment);
  const environment = {
    ...baseEnvironment,
    APP_ENV: "test",
    PORT: "8080",
    HTTP_LISTEN_HOST: "127.0.0.1",
    PUBLIC_WEB_ORIGIN: localOrigins.web,
    PUBLIC_API_ORIGIN: localOrigins.api,
    LOG_LEVEL: "warn",
    VITE_API_BASE_URL: "/api",
    DATABASE_POOL_URL: databaseURL,
    DATABASE_MIGRATION_URL: databaseURL,
    REDIS_URL: "redis://127.0.0.1:6379/1",
    SESSION_SECRET: runtimeSecrets.sessionSecret,
    SESSION_COOKIE_SECURE: "false",
    OIDC_ISSUER_URL: localOrigins.issuer,
    OIDC_CLIENT_ID: "tutorhub-e2e",
    OIDC_CLIENT_SECRET: runtimeSecrets.clientSecret,
    OIDC_CALLBACK_URL: `${localOrigins.api}/api/v1/auth/callback`,
    OIDC_POST_LOGOUT_URL: `${localOrigins.web}/signed-out`,
    OIDC_SCOPES: "openid profile email",
    E2E_OIDC_ADDRESS: "127.0.0.1:9091",
    E2E_ADMIN_EMAIL: localAccounts.admin.email,
    E2E_TEACHER_EMAIL: localAccounts.teacher.email,
    E2E_STUDENT_EMAIL: localAccounts.student.email,
  };

  for (const key of clearedConfigurationKeys) {
    environment[key] = "";
  }
  if (baseEnvironment.E2E_AUTH_NATIVE === "true") {
    if (!runtimeSecrets.googleClientSecret) {
      throw new Error("Native auth E2E needs an ephemeral Google secret.");
    }
    Object.assign(environment, nativeAuthConfiguration, {
      GOOGLE_CLIENT_SECRET: runtimeSecrets.googleClientSecret,
    });
  }
  return environment;
}

/** True only for the native-auth stack that points Google at the fake. */
export function usesFakeGoogle(environment = {}) {
  return (
    environment.E2E_AUTH_NATIVE === "true" &&
    environment.AUTH_PROVIDER_GOOGLE_ENABLED === "true" &&
    environment.GOOGLE_ISSUER_URL === localOrigins.googleIssuer
  );
}

/**
 * The second e2e-oidc process reads the same OIDC_* keys as the first one,
 * so it gets its own copy with Google's issuer, client and callback, plus
 * per-scenario custom test accounts.
 */
export function buildFakeGoogleEnvironment(environment) {
  if (!usesFakeGoogle(environment) || !environment.GOOGLE_CLIENT_SECRET) {
    throw new Error("The fake Google issuer requires the native-auth stack.");
  }
  return {
    ...environment,
    E2E_OIDC_ADDRESS: new URL(localOrigins.googleIssuer).host,
    E2E_OIDC_CUSTOM_ACCOUNTS: "true",
    OIDC_ISSUER_URL: localOrigins.googleIssuer,
    OIDC_CLIENT_ID: environment.GOOGLE_CLIENT_ID,
    OIDC_CLIENT_SECRET: environment.GOOGLE_CLIENT_SECRET,
    OIDC_CALLBACK_URL: `${localOrigins.api}/api/v1/auth/oauth/google/callback`,
    OIDC_POST_LOGOUT_URL: `${localOrigins.web}/signed-out`,
  };
}

function isLoopbackHost(hostname) {
  const normalized = hostname.toLowerCase().replace(/^\[|\]$/g, "");
  return (
    normalized === "localhost" ||
    normalized === "127.0.0.1" ||
    normalized === "::1"
  );
}

function executable(name) {
  return process.platform === "win32" ? `${name}.exe` : name;
}

function goExecutable() {
  const bundled = path.join(
    repositoryRoot,
    ".tools",
    "go",
    "bin",
    process.platform === "win32" ? "go.exe" : "go",
  );
  return existsSync(bundled) ? bundled : executable("go");
}

export function buildWebServerCommand({
  nodeExecutable = process.execPath,
  platform = process.platform,
  repositoryDirectory = repositoryRoot,
} = {}) {
  const pathImplementation = platform === "win32" ? path.win32 : path.posix;
  const webDirectory = pathImplementation.join(
    repositoryDirectory,
    "apps",
    "web",
  );
  return {
    argumentsList: [
      pathImplementation.join(
        webDirectory,
        "node_modules",
        "vite",
        "bin",
        "vite.js",
      ),
      "--host",
      "127.0.0.1",
      "--strictPort",
    ],
    command: nodeExecutable,
    cwd: webDirectory,
  };
}

export function buildWindowsTreeKillCommand(
  processID,
  {
    force = false,
    windowsDirectory = process.env.SystemRoot || "C:\\Windows",
  } = {},
) {
  if (!Number.isSafeInteger(processID) || processID <= 0) {
    throw new Error("A positive integer child process ID is required.");
  }
  return {
    argumentsList: ["/PID", String(processID), "/T", ...(force ? ["/F"] : [])],
    command: path.win32.join(windowsDirectory, "System32", "taskkill.exe"),
  };
}

function run(command, argumentsList, environment) {
  const result = spawnSync(command, argumentsList, {
    cwd: repositoryRoot,
    env: environment,
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) {
    throw new Error(`Could not run ${path.basename(command)}.`);
  }
  if (result.status !== 0) {
    throw new Error(
      `${path.basename(command)} exited with status ${result.status}.`,
    );
  }
}

function runCompose(argumentsList, environment) {
  run(executable("docker"), ["compose", ...argumentsList], environment);
}

function resetManagedDatabase(environment) {
  runCompose(
    ["up", "--detach", "--wait", "--remove-orphans", "postgres"],
    environment,
  );
  runCompose(
    [
      "exec",
      "-T",
      "postgres",
      "dropdb",
      "--username",
      "tutorhub",
      "--if-exists",
      "--force",
      e2eDatabaseName,
    ],
    environment,
  );
  runCompose(
    [
      "exec",
      "-T",
      "postgres",
      "createdb",
      "--username",
      "tutorhub",
      e2eDatabaseName,
    ],
    environment,
  );
}

export function prepareE2EDatabase(
  environment = buildE2EEnvironment(process.env),
) {
  const { mode } = validateDatabaseConfiguration(environment);
  if (mode === "managed") {
    resetManagedDatabase(environment);
  }
  run(
    goExecutable(),
    ["run", "./services/core-api/cmd/migrate", "up"],
    environment,
  );
}

function buildServiceBinaries(environment) {
  const extension = process.platform === "win32" ? ".exe" : "";
  const outputDirectory = path.join(repositoryRoot, ".tools", "bin");
  mkdirSync(outputDirectory, { recursive: true });
  const oidcBinary = path.join(
    outputDirectory,
    `tutorhub-e2e-oidc${extension}`,
  );
  const apiBinary = path.join(
    outputDirectory,
    `tutorhub-core-api-e2e${extension}`,
  );
  run(
    goExecutable(),
    ["build", "-o", oidcBinary, "./services/core-api/cmd/e2e-oidc"],
    environment,
  );
  run(
    goExecutable(),
    ["build", "-o", apiBinary, "./services/core-api/cmd/api"],
    environment,
  );
  return { apiBinary, oidcBinary };
}

function startChild(
  command,
  argumentsList,
  environment,
  workingDirectory = repositoryRoot,
) {
  return spawn(command, argumentsList, {
    cwd: workingDirectory,
    detached: process.platform !== "win32",
    env: environment,
    shell: false,
    stdio: "inherit",
    windowsHide: true,
  });
}

async function waitForEndpoint(target, child, timeoutMilliseconds = 60_000) {
  const deadline = Date.now() + timeoutMilliseconds;
  while (Date.now() < deadline) {
    if (childHasExited(child)) {
      throw new Error(
        `${path.basename(child.spawnfile)} stopped before becoming ready.`,
      );
    }
    try {
      const response = await fetch(target, {
        redirect: "manual",
        signal: AbortSignal.timeout(2_000),
      });
      await response.body?.cancel();
      if (response.status >= 200 && response.status < 500) {
        return;
      }
    } catch {
      // The process is still starting. Retry until the bounded deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`Timed out waiting for ${new URL(target).origin}.`);
}

function childHasExited(child) {
  return child.exitCode !== null || child.signalCode !== null;
}

function waitForChildExit(child, timeoutMilliseconds) {
  if (childHasExited(child)) {
    return Promise.resolve(true);
  }
  return new Promise((resolve) => {
    const onExit = () => {
      clearTimeout(timeout);
      resolve(true);
    };
    const timeout = setTimeout(() => {
      child.removeListener("exit", onExit);
      resolve(childHasExited(child));
    }, timeoutMilliseconds);
    child.once("exit", onExit);
  });
}

function terminateChildTree(child, force) {
  if (childHasExited(child)) {
    return;
  }
  const processID = child.pid;
  const signal = force ? "SIGKILL" : "SIGTERM";
  if (process.platform === "win32" && processID) {
    const termination = buildWindowsTreeKillCommand(processID, { force });
    const result = spawnSync(termination.command, termination.argumentsList, {
      stdio: "ignore",
      windowsHide: true,
    });
    if (result.error && !childHasExited(child)) {
      child.kill(signal);
    }
    return;
  }
  if (processID) {
    try {
      process.kill(-processID, signal);
      return;
    } catch (error) {
      if (error?.code !== "ESRCH") {
        throw error;
      }
    }
  }
  if (!childHasExited(child)) {
    child.kill(signal);
  }
}

async function stopChildren(children) {
  const runningChildren = children.filter((child) => !childHasExited(child));
  for (const child of runningChildren) {
    terminateChildTree(child, false);
  }
  await Promise.all(
    runningChildren.map((child) =>
      waitForChildExit(child, childShutdownGraceMilliseconds),
    ),
  );

  const remainingChildren = runningChildren.filter(
    (child) => !childHasExited(child),
  );
  for (const child of remainingChildren) {
    terminateChildTree(child, true);
  }
  await Promise.all(
    remainingChildren.map((child) =>
      waitForChildExit(child, childShutdownForceMilliseconds),
    ),
  );

  const orphanedProcessIDs = remainingChildren
    .filter((child) => !childHasExited(child))
    .map((child) => child.pid)
    .filter(Boolean);
  if (orphanedProcessIDs.length > 0) {
    throw new Error(
      `Could not stop E2E child process tree(s): ${orphanedProcessIDs.join(", ")}.`,
    );
  }
}

async function runServers(environment) {
  prepareE2EDatabase(environment);
  const binaries = buildServiceBinaries(environment);
  const children = [];
  let signalStopHandler;
  let stopping = false;
  let stopPromise;
  const stop = () => {
    stopping = true;
    stopPromise ??= stopChildren(children);
    return stopPromise;
  };
  let resolveSignalStop;
  const signalStopPromise = new Promise((resolve) => {
    resolveSignalStop = resolve;
  });
  signalStopHandler = () => {
    // The finalizer awaits the same promise and surfaces cleanup failures.
    void stop().then(resolveSignalStop, resolveSignalStop);
  };
  process.once("SIGINT", signalStopHandler);
  process.once("SIGTERM", signalStopHandler);
  try {
    const oidc = startChild(binaries.oidcBinary, [], environment);
    children.push(oidc);
    await waitForEndpoint(`${localOrigins.issuer}/healthz`, oidc);
    if (stopping) {
      return;
    }

    if (usesFakeGoogle(environment)) {
      // The API discovers Google at startup, so the fake must be ready first.
      const google = startChild(
        binaries.oidcBinary,
        [],
        buildFakeGoogleEnvironment(environment),
      );
      children.push(google);
      await waitForEndpoint(`${localOrigins.googleIssuer}/healthz`, google);
      if (stopping) {
        return;
      }
    }

    const api = startChild(binaries.apiBinary, [], environment);
    children.push(api);
    await waitForEndpoint(`${localOrigins.api}/ready`, api);
    if (stopping) {
      return;
    }

    const webCommand = buildWebServerCommand();
    const web = startChild(
      webCommand.command,
      webCommand.argumentsList,
      environment,
      webCommand.cwd,
    );
    children.push(web);
    await waitForEndpoint(`${localOrigins.web}/sign-in`, web);
    if (stopping) {
      return;
    }
    process.stdout.write("TutorHub loopback E2E services are ready.\n");

    await new Promise((resolve, reject) => {
      void signalStopPromise.then(resolve);
      for (const child of children) {
        child.once("error", reject);
        child.once("exit", (code) => {
          if (!stopping) {
            reject(
              new Error(
                `${path.basename(child.spawnfile)} exited with status ${code ?? 1}.`,
              ),
            );
          }
        });
      }
    });
  } finally {
    if (signalStopHandler) {
      process.removeListener("SIGINT", signalStopHandler);
      process.removeListener("SIGTERM", signalStopHandler);
    }
    await stop();
  }
}

function printHelp() {
  process.stdout.write(`TutorHub loopback E2E environment\n\n`);
  process.stdout.write(
    `  prepare  Recreate the isolated database and migrate\n`,
  );
  process.stdout.write(
    `  serve    Prepare and run fake OIDC, Core API, and web\n\n`,
  );
  process.stdout.write(
    `  --native-auth  Also enable email sign-up, password and email-code login, password recovery, step-up and device management, the loopback devinbox sender, and Google federation against a second loopback fake issuer\n`,
  );
}

export async function main(argumentsList, baseEnvironment = process.env) {
  const { command, nativeAuth } = parseArguments(argumentsList);
  const environment = buildE2EEnvironment(
    resolveBaseEnvironment({ nativeAuth }, baseEnvironment),
  );
  switch (command) {
    case "prepare":
      prepareE2EDatabase(environment);
      break;
    case "serve":
      await runServers(environment);
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
