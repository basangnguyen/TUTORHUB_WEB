import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  AUTH_DISABLED_FLAGS,
  checkAuthDeliveryGuard,
  parseAuthEnvironment,
  validateAuthDeliveryEnvironment,
} from "./check-auth-delivery-guard.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));

test("repository authentication defaults keep all new methods and delivery disabled", async () => {
  assert.deepEqual((await checkAuthDeliveryGuard(root)).issues, []);
});

test("every new flag is checked when missing or enabled", async () => {
  const source = await readFile(
    new URL("../.env.example", import.meta.url),
    "utf8",
  );
  const { environment } = parseAuthEnvironment(source);
  for (const key of AUTH_DISABLED_FLAGS) {
    for (const value of [undefined, "true"]) {
      assert.match(
        validateAuthDeliveryEnvironment(
          { ...environment, [key]: value },
          { safeDefaults: true },
        ).join("\n"),
        new RegExp(key),
      );
    }
  }
  assert.match(
    validateAuthDeliveryEnvironment(
      { ...environment, AUTH_FUTURE_METHOD_ENABLED: "true" },
      { safeDefaults: true },
    ).join("\n"),
    /AUTH_FUTURE_METHOD_ENABLED/,
  );
  assert.match(
    validateAuthDeliveryEnvironment(
      { ...environment, AUTH_PROVIDER_OIDC_DEFAULT_ENABLED: "false" },
      { safeDefaults: true },
    ).join("\n"),
    /AUTH_PROVIDER_OIDC_DEFAULT_ENABLED/,
  );
});

test("paid delivery requires explicit budget approval in every environment", () => {
  for (const APP_ENV of ["development", "test", "staging", "production"]) {
    for (const [key, sender] of [
      ["AUTH_EMAIL_SENDER", "ses"],
      ["AUTH_PHONE_SENDER", "zns"],
      ["AUTH_PHONE_SENDER", "sms_brandname"],
      ["AUTH_PHONE_SENDER", "twilio"],
    ]) {
      for (const approval of [undefined, "false", "yes", "TRUE"]) {
        assert.match(
          validateAuthDeliveryEnvironment({
            APP_ENV,
            [key]: sender,
            AUTH_DELIVERY_BUDGET_APPROVED: approval,
          }).join("\n"),
          /requires AUTH_DELIVERY_BUDGET_APPROVED=true/,
        );
      }
      assert.deepEqual(
        validateAuthDeliveryEnvironment({
          APP_ENV,
          [key]: sender,
          AUTH_DELIVERY_BUDGET_APPROVED: "true",
        }),
        [],
      );
    }
  }
});

test("local inbox and unauthenticated smtp cannot be enabled in staging or production", () => {
  for (const APP_ENV of ["staging", "production"]) {
    for (const AUTH_EMAIL_SENDER of ["devinbox", "smtp"]) {
      assert.match(
        validateAuthDeliveryEnvironment({
          APP_ENV,
          AUTH_EMAIL_SENDER,
          AUTH_SMTP_ADDR: "127.0.0.1:1025",
        }).join("\n"),
        /require development or test/,
      );
    }
    assert.match(
      validateAuthDeliveryEnvironment({
        APP_ENV,
        AUTH_PHONE_SENDER: "devinbox",
      }).join("\n"),
      /require development or test/,
    );
    assert.match(
      validateAuthDeliveryEnvironment({
        APP_ENV,
        AUTH_SMTP_ADDR: "127.0.0.1:1025",
      }).join("\n"),
      /AUTH_SMTP_ADDR/,
    );
  }
  for (const APP_ENV of ["development", "test"]) {
    assert.deepEqual(
      validateAuthDeliveryEnvironment({
        APP_ENV,
        AUTH_EMAIL_SENDER: "devinbox",
        AUTH_PHONE_SENDER: "devinbox",
      }),
      [],
    );
    assert.deepEqual(
      validateAuthDeliveryEnvironment({
        APP_ENV,
        AUTH_EMAIL_SENDER: "smtp",
        AUTH_SMTP_ADDR: "127.0.0.1:1025",
      }),
      [],
    );
  }
  assert.match(
    validateAuthDeliveryEnvironment({ APP_ENV: "local" }).join("\n"),
    /APP_ENV/,
  );
});

test("parser ignores comments and detects duplicate guard settings without reflecting values", () => {
  const parsed = parseAuthEnvironment(
    "# AUTH_EMAIL_SENDER=ses\nAUTH_EMAIL_SENDER=\"disabled\" # safe\nAUTH_EMAIL_SENDER=private-sensitive-value\nIGNORED_SECRET=secret-value\nAUTH_SIGNUP_ENABLED='false'",
  );
  assert.equal(parsed.environment.AUTH_SIGNUP_ENABLED, "false");
  assert.deepEqual(parsed.issues, ["AUTH_EMAIL_SENDER is duplicated"]);
  assert.equal(Object.hasOwn(parsed.environment, "IGNORED_SECRET"), false);
  const issues = validateAuthDeliveryEnvironment(parsed.environment).join("\n");
  assert.match(issues, /AUTH_EMAIL_SENDER is unsupported/);
  assert.doesNotMatch(issues, /private-sensitive-value|secret-value/);
});
