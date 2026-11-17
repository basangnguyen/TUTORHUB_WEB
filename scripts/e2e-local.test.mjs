import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  buildWebServerCommand,
  buildWindowsTreeKillCommand,
  buildE2EEnvironment,
  createRuntimeSecrets,
  localAccounts,
  nativeAuthConfiguration,
  parseArguments,
  resolveBaseEnvironment,
  validateDatabaseConfiguration,
} from "./e2e-local.mjs";

const fixedSecrets = Object.freeze({
  clientSecret: "ephemeral-client-secret-value",
  sessionSecret: "ZXBoZW1lcmFsLXNlc3Npb24tc2VjcmV0LXdpdGgtZW5vdWdoLWJ5dGVz",
});

test("local E2E environment removes inherited cloud credentials", () => {
  const environment = buildE2EEnvironment(
    {
      APP_ENV: "production",
      DATABASE_POOL_URL: "postgresql://production.example.invalid/tutorhub",
      B2_APPLICATION_KEY: "must-not-survive",
      E2E_ADMIN_EMAIL: "inherited-admin@example.invalid",
      E2E_STUDENT_EMAIL: "inherited-student@example.invalid",
      E2E_TEACHER_EMAIL: "inherited-teacher@example.invalid",
      HTTP_LISTEN_HOST: "0.0.0.0",
      LIVEKIT_API_SECRET: "must-not-survive",
      OIDC_CLIENT_SECRET: "must-not-survive",
      SESSION_SECRET: "must-not-survive",
    },
    {
      clientSecret: "ephemeral-client-secret-value",
      sessionSecret: "ZXBoZW1lcmFsLXNlc3Npb24tc2VjcmV0LXdpdGgtZW5vdWdoLWJ5dGVz",
    },
  );

  assert.equal(environment.APP_ENV, "test");
  assert.equal(environment.HTTP_LISTEN_HOST, "127.0.0.1");
  assert.equal(environment.B2_APPLICATION_KEY, "");
  assert.equal(environment.LIVEKIT_API_SECRET, "");
  assert.equal(environment.OIDC_CLIENT_SECRET, "ephemeral-client-secret-value");
  assert.equal(environment.E2E_ADMIN_EMAIL, localAccounts.admin.email);
  assert.equal(environment.E2E_TEACHER_EMAIL, localAccounts.teacher.email);
  assert.equal(environment.E2E_STUDENT_EMAIL, localAccounts.student.email);
  assert.equal(
    environment.SESSION_SECRET,
    "ZXBoZW1lcmFsLXNlc3Npb24tc2VjcmV0LXdpdGgtZW5vdWdoLWJ5dGVz",
  );
  assert.match(environment.OIDC_ISSUER_URL, /^http:\/\/127\.0\.0\.1:/);
  assert.match(environment.PUBLIC_API_ORIGIN, /^http:\/\/127\.0\.0\.1:/);
  assert.match(environment.PUBLIC_WEB_ORIGIN, /^http:\/\/127\.0\.0\.1:/);
  assert.equal(
    environment.DATABASE_POOL_URL,
    environment.DATABASE_MIGRATION_URL,
  );
  assert.match(environment.DATABASE_POOL_URL, /\/tutorhub_e2e\?/);
});

test("runtime credentials are fresh and have sufficient entropy", () => {
  const first = createRuntimeSecrets();
  const second = createRuntimeSecrets();

  assert.notEqual(first.clientSecret, second.clientSecret);
  assert.notEqual(first.sessionSecret, second.sessionSecret);
  assert.ok(Buffer.from(first.clientSecret, "base64url").byteLength >= 32);
  assert.ok(Buffer.from(first.sessionSecret, "base64").byteLength >= 32);
});

test("database boundary accepts only the isolated loopback database", () => {
  assert.deepEqual(validateDatabaseConfiguration({}), {
    databaseURL:
      "postgresql://tutorhub:tutorhub_local@127.0.0.1:5432/tutorhub_e2e?sslmode=disable",
    mode: "managed",
  });
  assert.deepEqual(
    validateDatabaseConfiguration({
      E2E_DATABASE_MODE: "external",
      E2E_DATABASE_URL:
        "postgresql://ci-user:ci-password@localhost:5432/tutorhub_e2e?sslmode=disable",
    }),
    {
      databaseURL:
        "postgresql://ci-user:ci-password@localhost:5432/tutorhub_e2e?sslmode=disable",
      mode: "external",
    },
  );

  for (const databaseURL of [
    "postgresql://user:password@database.example/tutorhub_e2e",
    "postgresql://user:password@127.0.0.1/tutorhub",
    "mysql://user:password@127.0.0.1/tutorhub_e2e",
  ]) {
    assert.throws(
      () => validateDatabaseConfiguration({ E2E_DATABASE_URL: databaseURL }),
      /loopback tutorhub_e2e database/,
    );
  }
  for (const databaseURL of [
    "postgresql://user:password@127.0.0.1/tutorhub_e2e?host=database.example",
    "postgresql://user:password@127.0.0.1/tutorhub_e2e?database=tutorhub",
    "postgresql://user:password@127.0.0.1/tutorhub_e2e?sslmode=disable&host=database.example",
  ]) {
    assert.throws(
      () => validateDatabaseConfiguration({ E2E_DATABASE_URL: databaseURL }),
      /query parameters may only contain sslmode=disable/,
    );
  }
  assert.throws(
    () =>
      validateDatabaseConfiguration({
        E2E_DATABASE_MODE: "production",
      }),
    /managed or external/,
  );
});

test("Windows web command bypasses cmd shims and keeps arguments separate", () => {
  assert.deepEqual(
    buildWebServerCommand({
      nodeExecutable: "C:\\Program Files\\nodejs\\node.exe",
      platform: "win32",
      repositoryDirectory: "D:\\TutorHub_V2",
    }),
    {
      argumentsList: [
        "D:\\TutorHub_V2\\apps\\web\\node_modules\\vite\\bin\\vite.js",
        "--host",
        "127.0.0.1",
        "--strictPort",
      ],
      command: "C:\\Program Files\\nodejs\\node.exe",
      cwd: "D:\\TutorHub_V2\\apps\\web",
    },
  );
});

test("Windows cleanup scopes taskkill to a validated spawned PID", () => {
  assert.deepEqual(
    buildWindowsTreeKillCommand(1234, {
      force: true,
      windowsDirectory: "C:\\Windows",
    }),
    {
      argumentsList: ["/PID", "1234", "/T", "/F"],
      command: "C:\\Windows\\System32\\taskkill.exe",
    },
  );
  assert.throws(
    () => buildWindowsTreeKillCommand("1234 & calc"),
    /positive integer child process ID/,
  );
  assert.throws(
    () => buildWindowsTreeKillCommand(0),
    /positive integer child process ID/,
  );
});

test("commands and local account contract stay explicit", () => {
  assert.deepEqual(parseArguments(["serve"]), {
    command: "serve",
    nativeAuth: false,
  });
  assert.deepEqual(parseArguments(["prepare"]), {
    command: "prepare",
    nativeAuth: false,
  });
  assert.deepEqual(parseArguments([]), { command: "help", nativeAuth: false });
  assert.deepEqual(parseArguments(["serve", "--native-auth"]), {
    command: "serve",
    nativeAuth: true,
  });
  assert.throws(() => parseArguments(["reset"]), /Unknown E2E/);
  assert.throws(
    () => parseArguments(["--native-auth"]),
    /Unknown E2E environment command/,
  );
  for (const option of [
    "--native",
    "--nativeAuth",
    "native-auth",
    "toString",
  ]) {
    assert.throws(
      () => parseArguments(["serve", option]),
      /Unknown E2E environment option/,
    );
  }

  assert.equal(localAccounts.admin.email, "admin.e2e@tutorhub.local");
  assert.equal(localAccounts.teacher.email, "teacher.e2e@tutorhub.local");
  assert.equal(localAccounts.student.email, "student.e2e@tutorhub.local");
});

test("Vite proxies API requests to the same IPv4 loopback boundary", () => {
  const viteConfig = readFileSync(
    new URL("../apps/web/vite.config.ts", import.meta.url),
    "utf8",
  );

  assert.match(viteConfig, /target:\s*"http:\/\/127\.0\.0\.1:8080"/);
  assert.doesNotMatch(viteConfig, /target:\s*"http:\/\/localhost:8080"/);
});

test("native auth stays off by default and inherited auth settings pass through unchanged", () => {
  const standard = buildE2EEnvironment({}, fixedSecrets);
  for (const key of Object.keys(nativeAuthConfiguration)) {
    assert.equal(standard[key], undefined, `${key} must not be set by default`);
  }

  const inherited = {
    AUTH_EMAIL_SENDER: "disabled",
    AUTH_SIGNUP_ENABLED: "false",
    E2E_AUTH_NATIVE: "1",
  };
  const untouched = buildE2EEnvironment(inherited, fixedSecrets);
  assert.equal(untouched.AUTH_SIGNUP_ENABLED, "false");
  assert.equal(untouched.AUTH_EMAIL_SENDER, "disabled");
  assert.equal(untouched.AUTH_METHOD_PASSWORD_ENABLED, undefined);

  const plainBase = { PATH: "/usr/bin" };
  assert.equal(
    resolveBaseEnvironment(parseArguments(["serve"]), plainBase),
    plainBase,
  );
});

test("native auth enables sign-up, password login and the loopback devinbox only on request", () => {
  const fromEnvironment = buildE2EEnvironment(
    {
      AUTH_EMAIL_SENDER: "ses",
      AUTH_PASSWORD_BREACH_CHECK: "hibp",
      AUTH_SIGNUP_ENABLED: "false",
      E2E_AUTH_NATIVE: "true",
    },
    fixedSecrets,
  );
  const fromFlag = buildE2EEnvironment(
    resolveBaseEnvironment(parseArguments(["serve", "--native-auth"]), {
      PATH: "/usr/bin",
    }),
    fixedSecrets,
  );

  for (const environment of [fromEnvironment, fromFlag]) {
    assert.equal(environment.AUTH_SIGNUP_ENABLED, "true");
    assert.equal(environment.AUTH_METHOD_PASSWORD_ENABLED, "true");
    assert.equal(environment.AUTH_EMAIL_SENDER, "devinbox");
    assert.equal(environment.AUTH_PASSWORD_BREACH_CHECK, "offline");
    // The loopback/test boundary that devinbox requires is unchanged.
    assert.equal(environment.APP_ENV, "test");
    assert.equal(environment.HTTP_LISTEN_HOST, "127.0.0.1");
    assert.equal(environment.OIDC_ISSUER_URL, "http://127.0.0.1:9091");
    assert.equal(environment.B2_APPLICATION_KEY, "");
  }
  assert.equal(fromFlag.PATH, "/usr/bin");
});

test("native auth specs run only through the dedicated Playwright config", () => {
  const defaultConfig = readFileSync(
    new URL("../playwright.config.ts", import.meta.url),
    "utf8",
  );
  const authConfig = readFileSync(
    new URL("../playwright.auth.config.ts", import.meta.url),
    "utf8",
  );
  const packageJSON = JSON.parse(
    readFileSync(new URL("../package.json", import.meta.url), "utf8"),
  );

  assert.match(
    defaultConfig,
    /testIgnore:\s*\[\s*"\*\*\/auth-02-native-\*\.spec\.ts",?\s*\]/,
  );
  assert.match(
    defaultConfig,
    /command:\s*"node scripts\/e2e-local\.mjs serve"/,
  );
  assert.match(
    authConfig,
    /testMatch:\s*\[\s*"\*\*\/auth-02-native-\*\.spec\.ts",?\s*\]/,
  );
  assert.match(
    authConfig,
    /command:\s*"node scripts\/e2e-local\.mjs serve --native-auth"/,
  );
  assert.match(
    authConfig,
    /gracefulShutdown:\s*{\s*signal:\s*"SIGTERM",\s*timeout:\s*15_000,?\s*}/,
  );
  assert.match(authConfig, /reuseExistingServer:\s*false/);
  assert.equal(
    packageJSON.scripts["e2e:auth"],
    "playwright test --config playwright.auth.config.ts",
  );
});

test("Playwright gives the local orchestrator a graceful shutdown window", () => {
  const playwrightConfig = readFileSync(
    new URL("../playwright.config.ts", import.meta.url),
    "utf8",
  );

  assert.match(
    playwrightConfig,
    /gracefulShutdown:\s*{\s*signal:\s*"SIGTERM",\s*timeout:\s*15_000,?\s*}/,
  );
});
