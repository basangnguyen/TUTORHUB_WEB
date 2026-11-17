import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

export const AUTH_DISABLED_FLAGS = Object.freeze([
  "AUTH_SIGNUP_ENABLED",
  "AUTH_METHOD_PASSWORD_ENABLED",
  "AUTH_METHOD_EMAIL_CODE_ENABLED",
  "AUTH_METHOD_PHONE_ENABLED",
  "AUTH_PASSKEYS_ENABLED",
  "AUTH_MFA_TOTP_ENABLED",
  "AUTH_DEVICE_MANAGEMENT_ENABLED",
  "AUTH_SSO_DOMAINS_ENABLED",
  "AUTH_PROVIDER_GOOGLE_ENABLED",
  "AUTH_PROVIDER_FACEBOOK_ENABLED",
  "AUTH_PROVIDER_MICROSOFT_ENABLED",
  "AUTH_PROVIDER_APPLE_ENABLED",
]);

const PAID_SENDERS = new Set(["ses", "zns", "sms_brandname", "twilio"]);

// Only inspect repository examples or explicitly supplied configuration. Never
// discover or load .env*.local files, and never include configuration values in errors.
export function parseAuthEnvironment(source) {
  const environment = {};
  const issues = [];
  for (const line of source.split(/\r?\n/)) {
    const match = /^\s*(?:export\s+)?([A-Z][A-Z0-9_]*)\s*=\s*(.*?)\s*$/.exec(
      line,
    );
    if (!match) continue;
    const [, key, raw] = match;
    if (
      !key.startsWith("AUTH_") &&
      ![
        "APP_ENV",
        "PASSWORD_MIN_LENGTH",
        "PASSWORD_HASH_MAX_CONCURRENCY",
        "SESSION_PERSISTENT_ABSOLUTE_TTL",
      ].includes(key)
    )
      continue;
    if (Object.hasOwn(environment, key)) issues.push(`${key} is duplicated`);
    const value =
      raw.startsWith('"') || raw.startsWith("'")
        ? /^(["'])(.*?)\1\s*(?:#.*)?$/.exec(raw)?.[2]
        : raw.replace(/\s+#.*$/, "").trim();
    if (value === undefined) issues.push(`${key} has an invalid quoted value`);
    environment[key] = value ?? "";
  }
  return { environment, issues };
}

export function validateAuthDeliveryEnvironment(
  environment,
  { safeDefaults = false } = {},
) {
  const issues = [];
  const appEnv = environment.APP_ENV ?? "development";
  if (!["development", "test", "staging", "production"].includes(appEnv))
    issues.push("APP_ENV is unsupported");
  const local = appEnv === "development" || appEnv === "test";
  const hashConcurrency = environment.PASSWORD_HASH_MAX_CONCURRENCY ?? "2";
  if (!/^(?:[1-9]|[12][0-9]|3[0-2])$/.test(hashConcurrency))
    issues.push("PASSWORD_HASH_MAX_CONCURRENCY must be between 1 and 32");
  if (
    !["offline", "hibp", "off"].includes(
      environment.AUTH_PASSWORD_BREACH_CHECK ?? "offline",
    )
  )
    issues.push("AUTH_PASSWORD_BREACH_CHECK must be offline, hibp or off");
  const termsVersion = (
    environment.AUTH_LEGAL_TERMS_VERSION ?? "2026-11-16-v2.0"
  ).trim();
  if ([...termsVersion].length < 1 || [...termsVersion].length > 64)
    issues.push("AUTH_LEGAL_TERMS_VERSION must contain 1 to 64 characters");
  for (const [key, allowed] of [
    ["AUTH_EMAIL_SENDER", ["disabled", "devinbox", "smtp", "ses"]],
    [
      "AUTH_PHONE_SENDER",
      ["disabled", "devinbox", "zns", "sms_brandname", "twilio"],
    ],
  ]) {
    const sender = environment[key] ?? "disabled";
    if (!allowed.includes(sender)) issues.push(`${key} is unsupported`);
    if (!local && ["devinbox", "smtp"].includes(sender))
      issues.push(`${key} local adapters require development or test`);
    if (
      PAID_SENDERS.has(sender) &&
      environment.AUTH_DELIVERY_BUDGET_APPROVED !== "true"
    )
      issues.push(`${key} requires AUTH_DELIVERY_BUDGET_APPROVED=true`);
    if (safeDefaults && sender !== "disabled")
      issues.push(`${key} must default to disabled`);
  }
  if (!local && environment.AUTH_SMTP_ADDR)
    issues.push("AUTH_SMTP_ADDR is only allowed in development or test");
  if (environment.AUTH_EMAIL_SENDER === "smtp" && !environment.AUTH_SMTP_ADDR)
    issues.push("AUTH_SMTP_ADDR is required for smtp");
  if (safeDefaults) {
    for (const key of AUTH_DISABLED_FLAGS) {
      if (environment[key] !== "false")
        issues.push(`${key} must explicitly default to false`);
    }
    for (const [key, value] of Object.entries(environment)) {
      if (
        key.startsWith("AUTH_") &&
        key.endsWith("_ENABLED") &&
        key !== "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED" &&
        value !== "false"
      )
        issues.push(`${key} must default to false`);
    }
    for (const [key, expected] of Object.entries({
      AUTH_PROVIDER_OIDC_DEFAULT_ENABLED: "true",
      AUTH_DELIVERY_BUDGET_APPROVED: "false",
      AUTH_EMAIL_SENDER: "disabled",
      AUTH_PHONE_SENDER: "disabled",
      AUTH_SMTP_ADDR: "",
      AUTH_EMAIL_DAILY_QUOTA: "200",
      AUTH_PHONE_OTP_DAILY_QUOTA: "50",
      PASSWORD_MIN_LENGTH: "8",
      PASSWORD_HASH_MAX_CONCURRENCY: "2",
      AUTH_PASSWORD_BREACH_CHECK: "offline",
      AUTH_LEGAL_TERMS_VERSION: "2026-11-16-v2.0",
      SESSION_PERSISTENT_ABSOLUTE_TTL: "720h",
      AUTH_PHONE_ALLOWED_COUNTRIES: "VN",
    })) {
      if (environment[key] !== expected)
        issues.push(`${key} differs from the approved default`);
    }
  }
  return [...new Set(issues)];
}

export async function checkAuthDeliveryGuard(repositoryRoot) {
  const source = await readFile(
    resolve(repositoryRoot, ".env.example"),
    "utf8",
  );
  const { environment, issues } = parseAuthEnvironment(source);
  return {
    issues: [
      ...issues,
      ...validateAuthDeliveryEnvironment(environment, { safeDefaults: true }),
    ],
  };
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const result = await checkAuthDeliveryGuard(process.cwd());
  for (const issue of result.issues) console.error(issue);
  if (result.issues.length) process.exitCode = 1;
  else console.log("Authentication delivery defaults are safe.");
}
