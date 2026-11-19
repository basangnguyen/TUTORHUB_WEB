import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

import {
  buildFakeFacebookEnvironment,
  buildFakeGoogleEnvironment,
  buildFakeMicrosoftEnvironment,
  buildWebServerCommand,
  buildWindowsTreeKillCommand,
  buildE2EEnvironment,
  createRuntimeSecrets,
  facebookFixtureClientID,
  facebookSecretHandoffKey,
  googleFixtureClientID,
  localAccounts,
  microsoftFixtureClientID,
  nativeAuthConfiguration,
  nativeAuthOrigins,
  parseArguments,
  publicOriginsFor,
  resolveBaseEnvironment,
  usesFakeFacebook,
  usesFakeGoogle,
  usesFakeMicrosoft,
  validateDatabaseConfiguration,
} from "./e2e-local.mjs";

const fixedSecrets = Object.freeze({
  clientSecret: "ephemeral-client-secret-value",
  facebookClientSecret: "ephemeral-facebook-app-secret-value",
  googleClientSecret: "ephemeral-google-client-secret-value",
  microsoftClientSecret: "ephemeral-microsoft-client-secret-value",
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
  assert.notEqual(first.googleClientSecret, second.googleClientSecret);
  assert.notEqual(first.clientSecret, first.googleClientSecret);
  assert.notEqual(first.facebookClientSecret, second.facebookClientSecret);
  assert.notEqual(first.facebookClientSecret, first.googleClientSecret);
  assert.notEqual(first.microsoftClientSecret, second.microsoftClientSecret);
  assert.notEqual(first.microsoftClientSecret, first.googleClientSecret);
  assert.notEqual(first.microsoftClientSecret, first.facebookClientSecret);
  assert.ok(
    Buffer.from(first.microsoftClientSecret, "base64url").byteLength >= 32,
  );
  assert.notEqual(first.sessionSecret, second.sessionSecret);
  assert.ok(
    Buffer.from(first.facebookClientSecret, "base64url").byteLength >= 32,
  );
  assert.ok(Buffer.from(first.clientSecret, "base64url").byteLength >= 32);
  assert.ok(
    Buffer.from(first.googleClientSecret, "base64url").byteLength >= 32,
  );
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
  // The default stack keeps its IPv4 loopback origins and no passkeys.
  assert.equal(standard.PUBLIC_WEB_ORIGIN, "http://127.0.0.1:5173");
  assert.equal(standard.PUBLIC_API_ORIGIN, "http://127.0.0.1:8080");
  assert.equal(
    standard.OIDC_CALLBACK_URL,
    "http://127.0.0.1:8080/api/v1/auth/callback",
  );
  assert.equal(
    standard.OIDC_POST_LOGOUT_URL,
    "http://127.0.0.1:5173/signed-out",
  );
  assert.equal(standard.AUTH_PASSKEYS_ENABLED, undefined);
  assert.deepEqual(publicOriginsFor({}), {
    api: "http://127.0.0.1:8080",
    web: "http://127.0.0.1:5173",
  });
  assert.deepEqual(publicOriginsFor({ E2E_AUTH_NATIVE: "1" }), {
    api: "http://127.0.0.1:8080",
    web: "http://127.0.0.1:5173",
  });
  assert.equal(standard.GOOGLE_CLIENT_SECRET, undefined);
  assert.equal(standard.FACEBOOK_CLIENT_SECRET, undefined);
  assert.equal(standard.MICROSOFT_CLIENT_SECRET, undefined);
  assert.equal(usesFakeGoogle(standard), false);
  assert.equal(usesFakeFacebook(standard), false);
  assert.equal(usesFakeMicrosoft(standard), false);
  assert.throws(
    () => buildFakeMicrosoftEnvironment(standard),
    /requires the native-auth stack/,
  );
  assert.throws(
    () => buildFakeFacebookEnvironment(standard),
    /requires the native-auth stack/,
  );
  assert.throws(
    () => buildFakeGoogleEnvironment(standard),
    /requires the native-auth stack/,
  );

  const inherited = {
    AUTH_EMAIL_SENDER: "disabled",
    AUTH_SIGNUP_ENABLED: "false",
    E2E_AUTH_NATIVE: "1",
  };
  const untouched = buildE2EEnvironment(inherited, fixedSecrets);
  assert.equal(untouched.AUTH_SIGNUP_ENABLED, "false");
  assert.equal(untouched.AUTH_EMAIL_SENDER, "disabled");
  assert.equal(untouched.AUTH_METHOD_PASSWORD_ENABLED, undefined);
  assert.equal(untouched.AUTH_METHOD_EMAIL_CODE_ENABLED, undefined);
  assert.equal(untouched.AUTH_DEVICE_MANAGEMENT_ENABLED, undefined);

  const plainBase = { PATH: "/usr/bin" };
  assert.equal(
    resolveBaseEnvironment(parseArguments(["serve"]), plainBase),
    plainBase,
  );
});

test("native auth enables sign-up, password and email-code login, device management and the loopback devinbox only on request", () => {
  const fromEnvironment = buildE2EEnvironment(
    {
      AUTH_DEVICE_MANAGEMENT_ENABLED: "false",
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
    assert.equal(environment.AUTH_METHOD_EMAIL_CODE_ENABLED, "true");
    assert.equal(environment.AUTH_EMAIL_SENDER, "devinbox");
    assert.equal(environment.AUTH_PASSWORD_BREACH_CHECK, "offline");
    assert.equal(environment.AUTH_DEVICE_MANAGEMENT_ENABLED, "true");
    // Test-only: per-IP auth buckets scale so auth-02..07 fit one run.
    assert.equal(environment.AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER, "10");
    assert.equal(environment.APP_ENV, "test");
    // AUTH-05: Google points at the loopback fake with fixture credentials.
    assert.equal(environment.AUTH_PROVIDER_GOOGLE_ENABLED, "true");
    assert.equal(environment.GOOGLE_CLIENT_ID, googleFixtureClientID);
    assert.equal(
      environment.GOOGLE_CLIENT_SECRET,
      fixedSecrets.googleClientSecret,
    );
    assert.equal(environment.GOOGLE_ISSUER_URL, "http://127.0.0.1:9092");
    assert.equal(usesFakeGoogle(environment), true);
    // AUTH-06: Facebook points at the third loopback fake.
    assert.equal(environment.AUTH_PROVIDER_FACEBOOK_ENABLED, "true");
    assert.equal(environment.FACEBOOK_CLIENT_ID, facebookFixtureClientID);
    assert.equal(
      environment.FACEBOOK_CLIENT_SECRET,
      fixedSecrets.facebookClientSecret,
    );
    assert.equal(environment.FACEBOOK_ISSUER_URL, "http://127.0.0.1:9093");
    assert.equal(usesFakeFacebook(environment), true);
    // AUTH-07: Microsoft points at the fourth loopback fake authority.
    assert.equal(environment.AUTH_PROVIDER_MICROSOFT_ENABLED, "true");
    assert.equal(environment.MICROSOFT_CLIENT_ID, microsoftFixtureClientID);
    assert.equal(
      environment.MICROSOFT_CLIENT_SECRET,
      fixedSecrets.microsoftClientSecret,
    );
    assert.equal(environment.MICROSOFT_AUTHORITY_URL, "http://127.0.0.1:9094");
    assert.equal(environment.MICROSOFT_TENANT_MODE, undefined);
    assert.equal(usesFakeMicrosoft(environment), true);
    // AUTH-08: passkeys need a domain RP, so the browser-facing origins move
    // to localhost while every process still binds 127.0.0.1.
    assert.equal(environment.AUTH_PASSKEYS_ENABLED, "true");
    assert.equal(environment.WEBAUTHN_RP_ID, "localhost");
    assert.equal(environment.WEBAUTHN_RP_ORIGINS, "http://localhost:5173");
    assert.equal(environment.PUBLIC_WEB_ORIGIN, nativeAuthOrigins.web);
    assert.equal(environment.PUBLIC_API_ORIGIN, nativeAuthOrigins.api);
    assert.equal(
      environment.OIDC_CALLBACK_URL,
      "http://localhost:8080/api/v1/auth/callback",
    );
    assert.equal(
      environment.OIDC_POST_LOGOUT_URL,
      "http://localhost:5173/signed-out",
    );
    // The loopback/test boundary that devinbox requires is unchanged.
    assert.equal(environment.APP_ENV, "test");
    assert.equal(environment.HTTP_LISTEN_HOST, "127.0.0.1");
    assert.equal(environment.OIDC_ISSUER_URL, "http://127.0.0.1:9091");
    assert.equal(environment.B2_APPLICATION_KEY, "");
  }
  assert.equal(fromFlag.PATH, "/usr/bin");
});

test("native auth refuses to start Google without an ephemeral secret", () => {
  assert.throws(
    () =>
      buildE2EEnvironment(
        { E2E_AUTH_NATIVE: "true" },
        {
          clientSecret: fixedSecrets.clientSecret,
          sessionSecret: fixedSecrets.sessionSecret,
        },
      ),
    /ephemeral Google secret/,
  );
});

test("native auth refuses to start Facebook without an ephemeral secret", () => {
  assert.throws(
    () =>
      buildE2EEnvironment(
        { E2E_AUTH_NATIVE: "true" },
        {
          clientSecret: fixedSecrets.clientSecret,
          googleClientSecret: fixedSecrets.googleClientSecret,
          sessionSecret: fixedSecrets.sessionSecret,
        },
      ),
    /ephemeral Facebook secret/,
  );
});

test("native auth refuses to start Microsoft without an ephemeral secret", () => {
  assert.throws(
    () =>
      buildE2EEnvironment(
        { E2E_AUTH_NATIVE: "true" },
        {
          clientSecret: fixedSecrets.clientSecret,
          facebookClientSecret: fixedSecrets.facebookClientSecret,
          googleClientSecret: fixedSecrets.googleClientSecret,
          sessionSecret: fixedSecrets.sessionSecret,
        },
      ),
    /ephemeral Microsoft secret/,
  );
});

test("the fake Microsoft authority is a separate loopback client with the v2.0 surface", () => {
  const environment = buildE2EEnvironment(
    {
      AUTH_PROVIDER_MICROSOFT_ENABLED: "false",
      E2E_AUTH_NATIVE: "true",
      MICROSOFT_AUTHORITY_URL: "https://login.microsoftonline.com",
      MICROSOFT_CLIENT_ID: "00000000-inherited-entra-application",
      MICROSOFT_CLIENT_SECRET: "inherited-entra-secret",
    },
    fixedSecrets,
  );
  // The native-auth stack never inherits a real Entra application.
  assert.equal(environment.AUTH_PROVIDER_MICROSOFT_ENABLED, "true");
  assert.equal(environment.MICROSOFT_CLIENT_ID, microsoftFixtureClientID);
  assert.equal(
    environment.MICROSOFT_CLIENT_SECRET,
    fixedSecrets.microsoftClientSecret,
  );
  assert.equal(environment.MICROSOFT_AUTHORITY_URL, "http://127.0.0.1:9094");

  const microsoft = buildFakeMicrosoftEnvironment(environment);
  assert.equal(microsoft.APP_ENV, "test");
  assert.equal(microsoft.E2E_OIDC_ADDRESS, "127.0.0.1:9094");
  assert.equal(microsoft.E2E_OIDC_CUSTOM_ACCOUNTS, "true");
  assert.equal(microsoft.E2E_OIDC_FLAVOR, "microsoft");
  assert.equal(microsoft.OIDC_ISSUER_URL, "http://127.0.0.1:9094");
  assert.equal(microsoft.OIDC_CLIENT_ID, microsoftFixtureClientID);
  assert.equal(
    microsoft.OIDC_CLIENT_SECRET,
    fixedSecrets.microsoftClientSecret,
  );
  assert.equal(
    microsoft.OIDC_CALLBACK_URL,
    "http://localhost:8080/api/v1/auth/oauth/microsoft/callback",
  );
  assert.equal(
    microsoft.OIDC_POST_LOGOUT_URL,
    "http://localhost:5173/signed-out",
  );
  // The other fakes and the API keep their own values.
  assert.equal(environment.E2E_OIDC_FLAVOR, undefined);
  assert.equal(environment.OIDC_ISSUER_URL, "http://127.0.0.1:9091");
  assert.equal(environment.E2E_OIDC_ADDRESS, "127.0.0.1:9091");
  assert.equal(
    buildFakeFacebookEnvironment(environment).E2E_OIDC_FLAVOR,
    "facebook",
  );
  assert.equal(
    buildFakeGoogleEnvironment(environment).E2E_OIDC_FLAVOR,
    undefined,
  );
  // A real authority (or a disabled flag) never starts the fake.
  assert.equal(
    usesFakeMicrosoft({
      ...environment,
      MICROSOFT_AUTHORITY_URL: "https://login.microsoftonline.com",
    }),
    false,
  );
  assert.equal(
    usesFakeMicrosoft({
      ...environment,
      AUTH_PROVIDER_MICROSOFT_ENABLED: "false",
    }),
    false,
  );
});

test("the Playwright process may hand down the Facebook secret, never to children by name", () => {
  const handedDown = Buffer.alloc(32, 7).toString("base64url");
  const secrets = createRuntimeSecrets({
    [facebookSecretHandoffKey]: handedDown,
  });
  assert.equal(secrets.facebookClientSecret, handedDown);
  assert.throws(
    () => createRuntimeSecrets({ [facebookSecretHandoffKey]: "short" }),
    /at least 32 bytes/,
  );

  const environment = buildE2EEnvironment({
    E2E_AUTH_NATIVE: "true",
    [facebookSecretHandoffKey]: handedDown,
  });
  assert.equal(environment.FACEBOOK_CLIENT_SECRET, handedDown);
  assert.equal(environment[facebookSecretHandoffKey], undefined);
  const standard = buildE2EEnvironment({
    [facebookSecretHandoffKey]: handedDown,
  });
  assert.equal(standard.FACEBOOK_CLIENT_SECRET, undefined);
  assert.equal(standard[facebookSecretHandoffKey], undefined);
});

test("the fake Facebook issuer is a separate loopback client with Meta paths", () => {
  const environment = buildE2EEnvironment(
    {
      AUTH_PROVIDER_FACEBOOK_ENABLED: "false",
      E2E_AUTH_NATIVE: "true",
      FACEBOOK_CLIENT_ID: "inherited-meta-app-id",
      FACEBOOK_ISSUER_URL: "https://www.facebook.com",
    },
    fixedSecrets,
  );
  // The native-auth stack never inherits a real Meta app.
  assert.equal(environment.AUTH_PROVIDER_FACEBOOK_ENABLED, "true");
  assert.equal(environment.FACEBOOK_CLIENT_ID, facebookFixtureClientID);
  assert.equal(environment.FACEBOOK_ISSUER_URL, "http://127.0.0.1:9093");

  const facebook = buildFakeFacebookEnvironment(environment);
  assert.equal(facebook.APP_ENV, "test");
  assert.equal(facebook.E2E_OIDC_ADDRESS, "127.0.0.1:9093");
  assert.equal(facebook.E2E_OIDC_CUSTOM_ACCOUNTS, "true");
  assert.equal(facebook.E2E_OIDC_FLAVOR, "facebook");
  assert.equal(facebook.OIDC_ISSUER_URL, "http://127.0.0.1:9093");
  assert.equal(facebook.OIDC_CLIENT_ID, facebookFixtureClientID);
  assert.equal(
    facebook.OIDC_CALLBACK_URL,
    "http://localhost:8080/api/v1/auth/oauth/facebook/callback",
  );
  // The other fakes and the API keep their own values.
  assert.equal(environment.E2E_OIDC_FLAVOR, undefined);
  assert.equal(environment.OIDC_ISSUER_URL, "http://127.0.0.1:9091");
  assert.equal(
    buildFakeGoogleEnvironment(environment).E2E_OIDC_FLAVOR,
    undefined,
  );
});

test("the fake Google issuer is a separate loopback client with custom accounts", () => {
  const environment = buildE2EEnvironment(
    {
      AUTH_PROVIDER_GOOGLE_ENABLED: "false",
      E2E_AUTH_NATIVE: "true",
      GOOGLE_CLIENT_ID: "inherited.apps.googleusercontent.com",
      GOOGLE_ISSUER_URL: "https://accounts.google.com",
    },
    fixedSecrets,
  );
  // The native-auth stack never inherits a real Google client.
  assert.equal(environment.AUTH_PROVIDER_GOOGLE_ENABLED, "true");
  assert.equal(environment.GOOGLE_CLIENT_ID, googleFixtureClientID);
  assert.equal(environment.GOOGLE_ISSUER_URL, "http://127.0.0.1:9092");

  const google = buildFakeGoogleEnvironment(environment);
  assert.equal(google.APP_ENV, "test");
  assert.equal(google.E2E_OIDC_ADDRESS, "127.0.0.1:9092");
  assert.equal(google.E2E_OIDC_CUSTOM_ACCOUNTS, "true");
  assert.equal(google.OIDC_ISSUER_URL, "http://127.0.0.1:9092");
  assert.equal(google.OIDC_CLIENT_ID, googleFixtureClientID);
  assert.equal(google.OIDC_CLIENT_SECRET, fixedSecrets.googleClientSecret);
  assert.equal(
    google.OIDC_CALLBACK_URL,
    "http://localhost:8080/api/v1/auth/oauth/google/callback",
  );
  assert.equal(google.OIDC_POST_LOGOUT_URL, "http://localhost:5173/signed-out");
  // The default fake OIDC portal and the API keep their own values.
  assert.equal(environment.OIDC_ISSUER_URL, "http://127.0.0.1:9091");
  assert.equal(environment.OIDC_CLIENT_SECRET, fixedSecrets.clientSecret);
  assert.equal(environment.E2E_OIDC_ADDRESS, "127.0.0.1:9091");
  assert.equal(environment.E2E_OIDC_CUSTOM_ACCOUNTS, undefined);
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

  const nativeSpecPatterns =
    /\[\s*"\*\*\/auth-0\[23\]-native-\*\.spec\.ts",\s*"\*\*\/auth-04-\*\.spec\.ts",\s*"\*\*\/auth-05-\*\.spec\.ts",\s*"\*\*\/auth-06-\*\.spec\.ts",\s*"\*\*\/auth-07-\*\.spec\.ts",\s*"\*\*\/auth-08-\*\.spec\.ts",?\s*\]/;
  assert.match(
    defaultConfig,
    new RegExp(`testIgnore:\\s*${nativeSpecPatterns.source}`),
  );
  assert.match(
    defaultConfig,
    /command:\s*"node scripts\/e2e-local\.mjs serve"/,
  );
  assert.match(
    authConfig,
    new RegExp(`testMatch:\\s*${nativeSpecPatterns.source}`),
  );
  // Every auth-0x spec on disk must fall under these shared patterns.
  const nativeSpecs = readdirSync(new URL("../e2e/", import.meta.url)).filter(
    (name) => /^auth-0\d-.*\.spec\.ts$/.test(name),
  );
  assert.ok(nativeSpecs.includes("auth-04-device-sessions-stepup.spec.ts"));
  assert.ok(nativeSpecs.includes("auth-05-google-federation.spec.ts"));
  assert.ok(nativeSpecs.includes("auth-06-facebook.spec.ts"));
  assert.ok(nativeSpecs.includes("auth-07-microsoft.spec.ts"));
  assert.ok(nativeSpecs.includes("auth-08-passkeys.spec.ts"));
  for (const name of nativeSpecs) {
    assert.ok(
      /^auth-0[23]-native-.*\.spec\.ts$/.test(name) ||
        /^auth-0[45678]-.*\.spec\.ts$/.test(name),
      `${name} must match the native auth Playwright patterns`,
    );
  }
  assert.match(
    authConfig,
    /command:\s*"node scripts\/e2e-local\.mjs serve --native-auth"/,
  );
  assert.match(
    authConfig,
    /gracefulShutdown:\s*{\s*signal:\s*"SIGTERM",\s*timeout:\s*15_000,?\s*}/,
  );
  assert.match(authConfig, /reuseExistingServer:\s*false/);
  // AUTH-08: the browser uses the localhost RP origin; readiness and the
  // Chromium resolver stay on the bound IPv4 loopback address.
  assert.match(authConfig, /const baseURL = "http:\/\/localhost:5173";/);
  assert.match(
    authConfig,
    /const boundWebOrigin = "http:\/\/127\.0\.0\.1:5173";/,
  );
  assert.match(authConfig, /url:\s*`\$\{boundWebOrigin\}\/sign-in`/);
  assert.match(
    authConfig,
    /"--host-resolver-rules=MAP localhost 127\.0\.0\.1"/,
  );
  assert.match(
    defaultConfig,
    /mode === "staging" \? stagingBaseURL\(\) : "http:\/\/127\.0\.0\.1:5173"/,
  );
  // The per-run Facebook secret is generated once and shared with workers.
  assert.match(
    authConfig,
    /process\.env\.E2E_FACEBOOK_CLIENT_SECRET\s*\?\?=\s*randomBytes\(32\)\.toString\("base64url"\)/,
  );
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
