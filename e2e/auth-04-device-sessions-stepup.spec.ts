import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Locator,
  type Page,
  type Response,
} from "@playwright/test";
import { randomBytes } from "node:crypto";

/**
 * AUTH-04B: shared step-up dialog, sign-in methods and session/device
 * management on /app/settings, against the loopback stack only. Requires
 * `node scripts/e2e-local.mjs serve --native-auth` (started by
 * playwright.auth.config.ts), which also turns on
 * AUTH_DEVICE_MANAGEMENT_ENABLED and the loopback-only `devinbox` sender.
 * Under the default `pnpm e2e` config the spec skips itself.
 *
 * Every session in this run is younger than the server's fixed 10-minute
 * recent-authentication window, so the "needs a step-up" signal (a 403
 * `auth_recent_authentication_required`, or an empty recent window on
 * /me/security) is injected exactly once per scenario with `page.route`.
 * The re-authentication itself, the retried action, session rotation and
 * revocation all run against the real Core API.
 */

const apiOrigin = "http://127.0.0.1:8080";
// AUTH-08: the native-auth stack serves the SPA on the localhost RP origin.
const webOrigin = "http://localhost:5173";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);

/** User agents map to the server's coarse allowlisted device labels. */
const devices = {
  primary: {
    label: "Chrome · Windows",
    userAgent:
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
  },
  second: {
    label: "Firefox · Linux",
    userAgent:
      "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0",
  },
  third: {
    label: "Safari · macOS",
    userAgent:
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
  },
} as const;

const labels = {
  signUp: /^(?:Đăng ký|Sign Up)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  signUpForm: /^(?:Tạo tài khoản mới|Create an Account)$/,
  fullName: /^(?:Họ và tên|Full Name)$/,
  email: /^(?:Địa chỉ email|Email Address)$/,
  getCode: /^(?:Lấy mã|Get Code)$/,
  otp: /^(?:Mã OTP|OTP code)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  useEmailCode: /^(?:Đăng nhập bằng mã email|Log in with an email code)$/,
  emailCodeForm:
    /^(?:Đăng nhập bằng mã gửi tới email|Log in with a code sent to your email)$/,
  securityCard: /^(?:Bảo mật tài khoản|Account security)$/,
  devicesCard: /^(?:Thiết bị đang đăng nhập|Signed-in devices)$/,
  thisDevice: /(?:Thiết bị này|This device)/,
  emailMethod: /(?:Mã đăng nhập qua email|Email sign-in code)/,
  passwordMethod: /(?:Đổi lần gần nhất|Last changed)/,
  revokeOthers:
    /^(?:Đăng xuất khỏi các thiết bị khác|Sign out of all other devices)$/,
  revokeOthersDialog:
    /^(?:Đăng xuất khỏi các thiết bị khác\?|Sign out of all other devices\?)$/,
  revokeOthersConfirm:
    /^(?:Đăng xuất các thiết bị khác|Sign out other devices)$/,
  revokedOne:
    /^(?:Đã đăng xuất 1 phiên khác\.|Signed out 1 other session\(s\)\.)$/,
  confirmNow: /^(?:Xác nhận ngay|Confirm now)$/,
  recentValid: /^(?:Còn hiệu lực đến|Valid until) /,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
  stepUpConfirm: /^(?:Xác nhận|Confirm)$/,
  stepUpEmailMethod: /^(?:Mã gửi qua email|Email code)$/,
  stepUpSendCode: /^(?:Gửi mã|Send code)$/,
  stepUpCode: /^(?:Mã xác nhận|Verification code)$/,
  stepUpWrongPassword:
    /(?:Mật khẩu không đúng hoặc đang tạm bị khóa|The password is incorrect or temporarily locked)/,
  stepUpWrongCode:
    /(?:Mã không đúng hoặc đã hết hạn|The code is incorrect or expired)/,
  passwordCard: /^(?:Mật khẩu|Password)$/,
  currentPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
  settingsNewPassword: /^(?:Mật khẩu mới|New password)$/,
  confirmPassword: /^(?:Nhập lại mật khẩu mới|Confirm new password)$/,
  changePassword: /^(?:Đổi mật khẩu|Change password)$/,
  passwordChanged:
    /^(?:Đã đổi mật khẩu\. Các phiên đăng nhập khác đã bị đăng xuất\.|Password changed\. Your other sessions have been signed out\.)$/,
  saveProfile: /^(?:Lưu thay đổi|Save changes)$/,
  profileSaved: /^(?:Đã cập nhật hồ sơ\.|Profile updated\.)$/,
} as const;

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  purpose: string;
}

function apiPath(url: string) {
  return new URL(url).pathname;
}

function postTo(page: Page, suffix: string) {
  return page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      apiPath(response.url()).endsWith(suffix),
  );
}

/** POST /me/sessions/{id}/revoke (never the revoke-others endpoint). */
function waitForSessionRevoke(page: Page) {
  return page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      /\/v1\/me\/sessions\/[^/]+\/revoke$/.test(apiPath(response.url())),
  );
}

async function expectNoBlockingAxeViolations(
  page: Page,
  selector: string,
  label: string,
) {
  const results = await new AxeBuilder({ page }).include(selector).analyze();
  const blocking = results.violations
    .filter((violation) => blockingImpacts.has(violation.impact ?? ""))
    .map(
      (violation) =>
        `${label}: ${violation.id} (${violation.impact}) on ${violation.nodes.length} node(s)`,
    );
  expect(blocking).toEqual([]);
}

async function waitForDevInboxCode(
  request: APIRequestContext,
  description: string,
  challengeId: string,
  purpose: string,
) {
  let found: DevInboxMessage | undefined;
  await expect
    .poll(
      async () => {
        const response = await request.get(
          `${apiOrigin}/api/v1/dev/auth-inbox`,
          { headers: { Accept: "application/json" } },
        );
        if (!response.ok()) {
          throw new Error(`The dev inbox answered HTTP ${response.status()}.`);
        }
        const body = (await response.json()) as { items?: DevInboxMessage[] };
        found = (body.items ?? []).find(
          (message) => message.challenge_id === challengeId,
        );
        return found ? "delivered" : "pending";
      },
      {
        message: `expected the dev inbox to receive ${description}`,
        timeout: 15_000,
      },
    )
    .toBe("delivered");
  expect(found?.purpose).toBe(purpose);
  const code = found?.code ?? "";
  expect(code).toMatch(/^\d{6}$/);
  return code;
}

async function readChallengeId(response: Response) {
  expect(response.status()).toBe(202);
  const body = (await response.json()) as { challenge_id?: unknown };
  if (typeof body.challenge_id !== "string") {
    throw new Error("The challenge response is malformed.");
  }
  return body.challenge_id;
}

/** A six-digit code that is guaranteed to differ from `code`. */
function wrongCodeFor(code: string) {
  return code.replace(/\d/g, (digit) => String((Number(digit) + 1) % 10));
}

async function meStatus(page: Page) {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  return response.status();
}

async function sessionCookies(page: Page) {
  return (await page.context().cookies()).filter((cookie) =>
    /session/i.test(cookie.name),
  );
}

async function expectSessionCookiesHttpOnly(page: Page) {
  const cookies = await sessionCookies(page);
  expect(cookies.length).toBeGreaterThan(0);
  for (const cookie of cookies) {
    expect(cookie.httpOnly).toBe(true);
  }
}

async function expectBrowserStorageWithoutSecrets(
  page: Page,
  secrets: readonly string[],
) {
  const stored = await page.evaluate(() =>
    JSON.stringify({
      local: { ...window.localStorage },
      session: { ...window.sessionStorage },
    }),
  );
  for (const secret of secrets) {
    expect(stored).not.toContain(secret);
  }
}

async function openDevice(browser: Browser, userAgent: string) {
  const context = await browser.newContext({
    baseURL: webOrigin,
    locale: "vi-VN",
    userAgent,
  });
  return { context, page: await context.newPage() };
}

async function signUp(
  page: Page,
  request: APIRequestContext,
  account: { displayName: string; email: string; password: string },
) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: labels.signUp }).click();
  const form = page.getByRole("form", { name: labels.signUpForm });
  await form
    .getByRole("textbox", { name: labels.fullName })
    .fill(account.displayName);
  await form.getByRole("textbox", { name: labels.email }).fill(account.email);
  const started = postTo(page, "/v1/auth/signup/email/start");
  await form.getByRole("button", { name: labels.getCode }).click();
  const challengeId = await readChallengeId(await started);
  const code = await waitForDevInboxCode(
    request,
    "the sign-up code",
    challengeId,
    "signup_email",
  );
  await form.getByRole("textbox", { name: labels.otp }).fill(code);
  await form.getByLabel(labels.password).fill(account.password);
  await form.getByRole("checkbox", { name: labels.terms }).check();
  const complete = postTo(page, "/v1/auth/signup/email/complete");
  await form.getByRole("button", { name: labels.createAccount }).click();
  expect((await complete).status()).toBe(201);
  await expect(page).toHaveURL(/\/app\/home$/);
  return code;
}

async function createWorkspace(page: Page, suffix: string) {
  // A brand-new account has no workspace; /app routes render onboarding.
  const onboarding = page.getByRole("heading", {
    name: /^(?:Tạo workspace đầu tiên|Create your first workspace)$/,
  });
  await expect(onboarding).toBeVisible();
  await page
    .getByLabel(
      /^(?:Tên tổ chức hoặc nhóm học|Organization or learning group name)$/,
    )
    .fill(`AUTH04 Workspace ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth04-${suffix}`);
  const created = postTo(page, "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

async function logInWithEmailCode(
  page: Page,
  request: APIRequestContext,
  email: string,
) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: labels.useEmailCode }).click();
  const form = page.getByRole("form", { name: labels.emailCodeForm });
  await form.getByRole("textbox", { name: labels.email }).fill(email);
  const started = postTo(page, "/v1/auth/email-code/start");
  await form.getByRole("button", { name: labels.getCode }).click();
  const challengeId = await readChallengeId(await started);
  const code = await waitForDevInboxCode(
    request,
    "the login code",
    challengeId,
    "login_code",
  );
  await form.getByRole("textbox", { name: labels.otp }).fill(code);
  const verify = postTo(page, "/v1/auth/email-code/verify");
  await form.getByRole("button", { name: labels.logIn }).click();
  expect((await verify).status()).toBe(200);
  await expect(page).toHaveURL(/\/app\/home$/);
  return code;
}

async function logInWithPassword(page: Page, email: string, password: string) {
  await page.goto("/sign-in");
  await page.getByRole("textbox", { name: labels.identifier }).fill(email);
  await page.getByLabel(labels.password).fill(password);
  const login = postTo(page, "/v1/auth/password/login");
  await page.getByRole("button", { name: labels.logIn }).click();
  expect((await login).status()).toBe(200);
  await expect(page).toHaveURL(/\/app\/home$/);
}

function sessionRow(card: Locator, deviceLabel: string) {
  return card.locator("li.session-row", { hasText: deviceLabel });
}

async function expectProfileSaveWorks(page: Page) {
  // A CSRF-protected mutation proves the client resynced the rotated token.
  const saved = page.waitForResponse(
    (response) =>
      response.request().method() === "PATCH" &&
      apiPath(response.url()).endsWith("/v1/me/profile"),
  );
  await page.getByRole("button", { name: labels.saveProfile }).click();
  expect((await saved).status()).toBe(200);
  await expect(page.getByText(labels.profileSaved)).toBeVisible();
}

test.describe("AUTH-04 step-up re-authentication, sign-in methods and devices", () => {
  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "Native auth runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      email_code_enabled: true,
      password_enabled: true,
      signup_enabled: true,
    });
  });

  test("lists and revokes devices, then steps up with a password and an emailed code", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const suffix = randomBytes(6).toString("hex");
    const email = `auth04.${suffix}@example.test`;
    const password = `Beacon-${randomBytes(9).toString("base64url")}-River5`;
    const changedPassword = `Glacier-${randomBytes(9).toString("base64url")}-Forest8`;
    const secrets: string[] = [password, changedPassword];
    const contexts: BrowserContext[] = [];

    try {
      const primary = await openDevice(browser, devices.primary.userAgent);
      contexts.push(primary.context);
      const pageA = primary.page;

      await test.step("sign up on the primary device and create a workspace", async () => {
        secrets.push(
          await signUp(pageA, request, {
            displayName: `AUTH04 Owner ${suffix.slice(0, 4)}`,
            email,
            password,
          }),
        );
        await createWorkspace(pageA, suffix);
      });

      const second = await openDevice(browser, devices.second.userAgent);
      contexts.push(second.context);
      const pageB = second.page;

      await test.step("sign in on a second device with an emailed code", async () => {
        secrets.push(await logInWithEmailCode(pageB, request, email));
        expect(await meStatus(pageB)).toBe(200);
      });

      const devicesCard = pageA.getByRole("region", {
        name: labels.devicesCard,
      });
      const securityCard = pageA.getByRole("region", {
        name: labels.securityCard,
      });

      await test.step("the primary device lists both sessions and marks itself", async () => {
        await pageA.goto("/app/settings");
        const current = sessionRow(devicesCard, devices.primary.label);
        const other = sessionRow(devicesCard, devices.second.label);
        await expect(current).toBeVisible();
        await expect(other).toBeVisible();
        await expect(devicesCard.locator("li.session-row")).toHaveCount(2);
        await expect(current).toContainText(labels.thisDevice);
        await expect(other).not.toContainText(labels.thisDevice);
        // This device can only sign out through the app's own sign-out.
        await expect(current.getByRole("button")).toHaveCount(0);

        await expect(securityCard.getByText(labels.emailMethod)).toBeVisible();
        await expect(securityCard.getByText(email)).toBeVisible();
        await expect(
          securityCard.getByText(labels.passwordMethod),
        ).toBeVisible();
        await expectNoBlockingAxeViolations(
          pageA,
          'section[aria-labelledby="security-sessions-title"]',
          "devices card",
        );
        await expectNoBlockingAxeViolations(
          pageA,
          'section[aria-labelledby="security-overview-title"]',
          "security card",
        );
      });

      await test.step("signing out the second device ends only that session", async () => {
        const revoked = waitForSessionRevoke(pageA);
        await sessionRow(devicesCard, devices.second.label)
          .getByRole("button", {
            name: new RegExp(`^(?:Đăng xuất|Sign out) ${devices.second.label}`),
          })
          .click();
        expect((await revoked).status()).toBe(204);
        await expect(sessionRow(devicesCard, devices.second.label)).toHaveCount(
          0,
        );
        expect(await meStatus(pageB)).toBe(401);
        expect(await meStatus(pageA)).toBe(200);
      });

      await test.step("sign out every other device after an explicit confirmation", async () => {
        const third = await openDevice(browser, devices.third.userAgent);
        contexts.push(third.context);
        await logInWithPassword(third.page, email, password);

        await pageA.reload();
        await expect(
          sessionRow(devicesCard, devices.third.label),
        ).toBeVisible();
        await devicesCard
          .getByRole("button", { name: labels.revokeOthers })
          .click();
        const dialog = pageA.getByRole("dialog", {
          name: labels.revokeOthersDialog,
        });
        await expect(dialog).toBeVisible();
        expect(await meStatus(third.page)).toBe(200);
        const revokedOthers = postTo(pageA, "/v1/me/sessions/revoke-others");
        await dialog
          .getByRole("button", { name: labels.revokeOthersConfirm })
          .click();
        const response = await revokedOthers;
        expect(response.status()).toBe(200);
        expect(await response.json()).toEqual({ revoked_count: 1 });
        await expect(devicesCard.getByText(labels.revokedOne)).toBeVisible();
        await expect(devicesCard.locator("li.session-row")).toHaveCount(1);
        expect(await meStatus(third.page)).toBe(401);
        expect(await meStatus(pageA)).toBe(200);
      });

      await test.step("confirm it's you from the banner: a wrong password keeps the session", async () => {
        // Present this young session as outside the recent window once.
        let staleOverviewServed = false;
        await pageA.route("**/api/v1/me/security", async (route) => {
          if (staleOverviewServed || route.request().method() !== "GET") {
            await route.fallback();
            return;
          }
          staleOverviewServed = true;
          const response = await route.fetch();
          const body = (await response.json()) as Record<string, unknown>;
          await route.fulfill({
            response,
            json: { ...body, recent_auth_expires_at: null },
          });
        });
        await pageA.goto("/app/settings");
        await securityCard
          .getByRole("button", { name: labels.confirmNow })
          .click();
        await pageA.unroute("**/api/v1/me/security");

        const dialog = pageA.getByRole("dialog", { name: labels.stepUpDialog });
        const passwordField = dialog.getByLabel(labels.stepUpPassword);
        await expect(passwordField).toBeFocused();
        await expectNoBlockingAxeViolations(
          pageA,
          '[role="dialog"]',
          "step-up dialog",
        );

        await passwordField.fill(`Wrong-${randomBytes(6).toString("hex")}`);
        const rejected = postTo(pageA, "/v1/auth/reauth");
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        const rejection = await rejected;
        expect(rejection.status()).toBe(401);
        expect(await rejection.json()).toMatchObject({
          code: "auth_invalid_current_password",
        });
        await expect(
          dialog.getByText(labels.stepUpWrongPassword),
        ).toBeVisible();
        await expect(passwordField).toHaveValue("");
        await expect(pageA).toHaveURL(/\/app\/settings$/);
        expect(await meStatus(pageA)).toBe(200);

        const before = await sessionCookies(pageA);
        await passwordField.fill(password);
        const accepted = postTo(pageA, "/v1/auth/reauth");
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        expect((await accepted).status()).toBe(200);
        await expect(dialog).toBeHidden();
        await expect(securityCard.getByText(labels.recentValid)).toBeVisible();
        // The server rotated the session cookie on step-up.
        const after = await sessionCookies(pageA);
        expect(after.map((cookie) => cookie.value)).not.toEqual(
          before.map((cookie) => cookie.value),
        );
        await expectProfileSaveWorks(pageA);
      });

      await test.step("a blocked password change resumes after an emailed-code step-up", async () => {
        // The first change attempt answers like a session past its window.
        let recentAuthInjected = false;
        await pageA.route("**/api/v1/me/password", async (route) => {
          if (recentAuthInjected || route.request().method() !== "POST") {
            await route.fallback();
            return;
          }
          recentAuthInjected = true;
          await route.fulfill({
            status: 403,
            contentType: "application/problem+json",
            body: JSON.stringify({
              type: "about:blank",
              title: "Recent authentication required",
              status: 403,
              code: "auth_recent_authentication_required",
              detail: "Sign in again before changing your password.",
            }),
          });
        });

        const card = pageA.getByRole("region", { name: labels.passwordCard });
        await card.getByLabel(labels.currentPassword).fill(password);
        await card.getByLabel(labels.settingsNewPassword).fill(changedPassword);
        await card.getByLabel(labels.confirmPassword).fill(changedPassword);
        await card.getByRole("button", { name: labels.changePassword }).click();

        const dialog = pageA.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(dialog).toBeVisible();
        expect(recentAuthInjected).toBe(true);
        await pageA.unroute("**/api/v1/me/password");
        await dialog
          .getByRole("button", { name: labels.stepUpEmailMethod })
          .click();
        const started = postTo(pageA, "/v1/auth/reauth/email-code/start");
        await dialog
          .getByRole("button", { name: labels.stepUpSendCode })
          .click();
        const challengeId = await readChallengeId(await started);
        const code = await waitForDevInboxCode(
          request,
          "the step-up code",
          challengeId,
          "reauth",
        );
        secrets.push(code);
        const codeField = dialog.getByRole("textbox", {
          name: labels.stepUpCode,
        });
        await expect(codeField).toBeFocused();
        await expectNoBlockingAxeViolations(
          pageA,
          '[role="dialog"]',
          "step-up dialog (email code)",
        );

        await codeField.fill(wrongCodeFor(code));
        const rejected = postTo(pageA, "/v1/auth/reauth");
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        const rejection = await rejected;
        expect(rejection.status()).toBe(401);
        expect(await rejection.json()).toMatchObject({
          code: "auth_invalid_verification_code",
        });
        await expect(dialog.getByText(labels.stepUpWrongCode)).toBeVisible();
        expect(await meStatus(pageA)).toBe(200);

        await codeField.fill(code);
        const accepted = postTo(pageA, "/v1/auth/reauth");
        const resumed = postTo(pageA, "/v1/me/password");
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        expect((await accepted).status()).toBe(200);
        // The blocked change runs again with the values already typed.
        expect((await resumed).status()).toBe(200);
        await expect(dialog).toBeHidden();
        await expect(card.getByText(labels.passwordChanged)).toBeVisible();
        await expect(card.getByLabel(labels.settingsNewPassword)).toHaveValue(
          "",
        );
        expect(await meStatus(pageA)).toBe(200);
        await expectProfileSaveWorks(pageA);
      });

      await test.step("no password, code or session token reaches Web Storage", async () => {
        await expectSessionCookiesHttpOnly(pageA);
        const tokens = (await sessionCookies(pageA)).map(
          (cookie) => cookie.value,
        );
        await expectBrowserStorageWithoutSecrets(pageA, [
          ...secrets,
          ...tokens,
        ]);
      });
    } finally {
      for (const context of contexts) {
        await context.close();
      }
    }
  });
});
