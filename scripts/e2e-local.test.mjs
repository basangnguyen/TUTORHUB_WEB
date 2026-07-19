import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  buildWebServerCommand,
  buildWindowsTreeKillCommand,
  buildE2EEnvironment,
  createRuntimeSecrets,
  localAccounts,
  parseArguments,
  validateDatabaseConfiguration,
} from "./e2e-local.mjs";

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
  assert.deepEqual(parseArguments(["serve"]), { command: "serve" });
  assert.deepEqual(parseArguments(["prepare"]), { command: "prepare" });
  assert.deepEqual(parseArguments([]), { command: "help" });
  assert.throws(() => parseArguments(["reset"]), /Unknown E2E/);

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
