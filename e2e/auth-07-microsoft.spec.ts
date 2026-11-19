import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Page,
  type Response,
} from "@playwright/test";
import { randomBytes, randomUUID } from "node:crypto";

/**
 * AUTH-07B: Sign in with Microsoft (work/school and personal accounts),
 * pending registration (email + OTP) when Microsoft gives no `xms_edov`
 * proof, explicit linking, tenant isolation, and linking Microsoft from
 * Settings, against the loopback stack only. Requires
 * `node scripts/e2e-local.mjs serve --native-auth` (started by
 * playwright.auth.config.ts), which also starts a fourth fake on
 * 127.0.0.1:9094 that serves the Microsoft identity platform v2.0 surface —
 * Microsoft itself is never called. Its ID tokens carry the canonical
 * `https://login.microsoftonline.com/{tid}/v2.0` issuer, `tid`, `oid` and
 * the chosen `xms_edov`, exactly what TutorHub's Microsoft adapter verifies.
 *
 * Rate-limit budget (one database for AUTH-02..07, per IP): this file makes
 * exactly ONE sign-up start (scenario 2's pending registration) and one OTP
 * send. The owner account is created by Microsoft itself (trusted email) and
 * gets a password in Settings, as AUTH-06 does, so no native sign-up is used.
 * Settings step-up is triggered by injecting one 403
 * `auth_recent_authentication_required`, like AUTH-05; everything else is
 * real. The scenarios share that account and run in order.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://127.0.0.1:5173";
const microsoftOrigin = "http://127.0.0.1:9094";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);
/** Tenant of every personal Microsoft account (MSA). */
const personalTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad";

const labels = {
  microsoft: /^(?:Đăng nhập bằng Microsoft|Sign in with Microsoft)$/,
  consent:
    /^(?:Bằng việc tiếp tục, bạn đồng ý với|By continuing, you agree to the)/,
  completeTitle:
    /^(?:Hoàn tất đăng ký bằng Microsoft|Finish signing up with Microsoft)$/,
  hint: /(?:Gợi ý từ Microsoft|Suggested by Microsoft)/,
  email: /^(?:Địa chỉ email|Email address)$/,
  sendCode: /^(?:Gửi mã|Send code)$/,
  codeHeading:
    /^(?:Bước 2: Nhập mã xác minh|Step 2: Enter the verification code)$/,
  neutralNotice:
    /(?:Nếu địa chỉ hợp lệ, mã 6 chữ số đã được gửi|If the address is valid, a 6-digit code has been sent)/,
  otp: /^(?:Mã xác minh|Verification code)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  finish: /^(?:Hoàn tất đăng ký|Finish sign-up)$/,
  linkTitle:
    /^(?:Liên kết Microsoft với tài khoản TutorHub|Link Microsoft to your TutorHub account)$/,
  linkConfirmHeading: /^(?:Xác nhận liên kết|Confirm linking)$/,
  linkConfirm: /^(?:Liên kết Microsoft|Link Microsoft)$/,
  linkCancel: /^(?:Hủy|Cancel)$/,
  loginForm: /^(?:Đăng nhập vào TutorHub|Log in to TutorHub)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  password: /^(?:Mật khẩu|Password)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  passwordCard: /^(?:Mật khẩu|Password)$/,
  newPassword: /^(?:Mật khẩu mới|New password)$/,
  confirmPassword: /^(?:Nhập lại mật khẩu mới|Confirm new password)$/,
  setPassword: /^(?:Đặt mật khẩu|Set password)$/,
  passwordSet:
    /^(?:Đã đặt mật khẩu\. Các phiên đăng nhập khác đã bị đăng xuất\.|Password set\. Your other sessions have been signed out\.)$/,
  identityCard: /^(?:Danh tính đăng nhập|Sign-in identities)$/,
  identityLinkMicrosoft: /^(?:Liên kết Microsoft|Link Microsoft)$/,
  identityLinked:
    /^(?:Đã liên kết danh tính đăng nhập\.|Sign-in identity linked\.)$/,
  guidanceTitle:
    /^(?:Chưa liên kết được danh tính|The sign-in identity wasn't linked)$/,
  guidanceMicrosoft:
    /(?:Với tài khoản Microsoft: hãy đăng xuất|For a Microsoft account: sign out)/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  securityCard: /^(?:Bảo mật tài khoản|Account security)$/,
  microsoftMethod: /^(?:Tài khoản Microsoft|Microsoft account)$/,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
  stepUpConfirm: /^(?:Xác nhận|Confirm)$/,
} as const;

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  purpose: string;
}

interface MicrosoftAccount {
  /** Omitted: the ID token carries no email claim (object ID required). */
  email?: string;
  tenantID: string;
  /** `xms_edov`: "true", "false" or "" (claim not sent). */
  domainVerified: "true" | "false" | "";
}

function apiPath(url: string) {
  return new URL(url).pathname;
}

function responseTo(page: Page, method: string, suffix: string) {
  return page.waitForResponse(
    (response) =>
      response.request().method() === method &&
      apiPath(response.url()).endsWith(suffix),
  );
}

async function expectNoBlockingAxeViolations(page: Page, label: string) {
  const results = await new AxeBuilder({ page }).include("main").analyze();
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
  challengeId: string,
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
      { message: "expected the registration code", timeout: 15_000 },
    )
    .toBe("delivered");
  expect(found?.purpose).toBe("signup_email");
  const code = found?.code ?? "";
  expect(code).toMatch(/^\d{6}$/);
  return code;
}

async function readChallengeId(response: Response) {
  // Account-neutral: always 202, whether or not the email has an owner.
  expect(response.status()).toBe(202);
  const body = (await response.json()) as { challenge_id?: unknown };
  if (typeof body.challenge_id !== "string") {
    throw new Error("The challenge response is malformed.");
  }
  return body.challenge_id;
}

function uniqueEmail(label: string) {
  return `auth07.${label}.${randomBytes(6).toString("hex")}@example.test`;
}

/** The server shows the first character of the local part, then `***@`. */
function maskedEmail(email: string) {
  const [local = "", domain = ""] = email.split("@");
  return `${local.length > 1 ? local[0] : ""}***@${domain}`;
}

async function openDevice(browser: Browser) {
  const context = await browser.newContext({
    baseURL: webOrigin,
    locale: "vi-VN",
  });
  return { context, page: await context.newPage() };
}

/** Fills the custom-account form of the fake Microsoft authority. */
async function chooseFakeMicrosoftAccount(
  page: Page,
  account: MicrosoftAccount,
) {
  await page.waitForURL(
    (url) =>
      url.origin === microsoftOrigin &&
      url.pathname === "/common/oauth2/v2.0/authorize",
  );
  if (account.email) {
    await page.getByRole("textbox", { name: "Test email" }).fill(account.email);
  }
  await page
    .getByRole("textbox", { name: "Tenant ID (tid)" })
    .fill(account.tenantID);
  await page
    .getByRole("combobox", { name: "xms_edov claim" })
    .selectOption(account.domainVerified);
  await page
    .getByRole("button", { name: "Sign in with custom account" })
    .click();
}

/** Clicks Microsoft on /sign-in (a full-page navigation) and picks an account. */
async function signInWithMicrosoft(page: Page, account: MicrosoftAccount) {
  await page.goto("/sign-in");
  const started = page.waitForRequest(
    (request) =>
      apiPath(request.url()) === "/api/v1/auth/oauth/microsoft/start",
  );
  await page.getByRole("button", { name: labels.microsoft }).click();
  // A top-level navigation, never a script fetch (RFC 9700 §4.4).
  expect((await started).isNavigationRequest()).toBe(true);
  await chooseFakeMicrosoftAccount(page, account);
}

async function me(page: Page) {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  const body =
    response.status() === 200
      ? ((await response.json()) as { user?: { email?: string } })
      : null;
  return { email: body?.user?.email, status: response.status() };
}

async function cookieNamed(context: BrowserContext, pattern: RegExp) {
  return (await context.cookies()).find((cookie) => pattern.test(cookie.name));
}

async function expectSessionCookiesHttpOnly(context: BrowserContext) {
  const cookies = (await context.cookies()).filter((cookie) =>
    /session/i.test(cookie.name),
  );
  expect(cookies.length).toBeGreaterThan(0);
  for (const cookie of cookies) {
    expect(cookie.httpOnly).toBe(true);
  }
}

/** Web Storage and script-visible cookies never hold tokens or emails. */
async function expectNoSecretsInBrowser(page: Page, secrets: string[]) {
  const stored = await page.evaluate(() =>
    JSON.stringify({
      cookie: document.cookie,
      local: { ...window.localStorage },
      session: { ...window.sessionStorage },
    }),
  );
  expect(stored).not.toMatch(
    /registration_tx|link_tx|tutorhub_session|csrf|id_token|access_token|xms_edov/i,
  );
  for (const secret of secrets) {
    expect(stored).not.toContain(secret);
  }
}

async function createWorkspace(page: Page, suffix: string) {
  const onboarding = page.getByRole("heading", {
    name: /^(?:Tạo workspace đầu tiên|Create your first workspace)$/,
  });
  await expect(onboarding).toBeVisible();
  await page
    .getByLabel(
      /^(?:Tên tổ chức hoặc nhóm học|Organization or learning group name)$/,
    )
    .fill(`AUTH07 Workspace ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth07-${suffix}`);
  const created = responseTo(page, "POST", "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

async function logInWithPassword(page: Page, email: string, password: string) {
  const form = page.getByRole("form", { name: labels.loginForm });
  await form.getByRole("textbox", { name: labels.identifier }).fill(email);
  await form.getByLabel(labels.password).fill(password);
  const login = responseTo(page, "POST", "/v1/auth/password/login");
  await form.getByRole("button", { name: labels.logIn }).click();
  expect((await login).status()).toBe(200);
}

/** Shared by the ordered scenarios below (see the file comment). */
const run = {
  /** Owner email, trusted by tenant A's xms_edov=true. */
  email: uniqueEmail("owner"),
  password: `Meadow-${randomBytes(9).toString("base64url")}-Compass7`,
  /** Work/school tenants of this run (fresh per run). */
  tenantA: randomUUID(),
  tenantB: randomUUID(),
  tenantC: randomUUID(),
  tenantD: randomUUID(),
  tenantE: randomUUID(),
  /** Personal account that proves its email with an OTP. */
  personalEmail: uniqueEmail("personal"),
  /** Email of the identity linked from Settings. */
  settingsEmail: uniqueEmail("settings"),
};

test.describe("AUTH-07 Microsoft federation, pending registration and tenant isolation", () => {
  test.describe.configure({ mode: "serial" });

  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "Microsoft federation runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      oauth_providers: expect.arrayContaining(["microsoft"]),
      password_enabled: true,
      signup_enabled: true,
    });
  });

  test("1. a work account with xms_edov=true and a new email goes straight into the app", async ({
    browser,
  }) => {
    test.setTimeout(300_000);
    const { context, page } = await openDevice(browser);
    try {
      await test.step("the sign-in page offers Microsoft with the consent line", async () => {
        await page.goto("/sign-in");
        await expect(
          page.getByRole("button", { name: labels.microsoft }),
        ).toBeEnabled();
        await expect(page.getByText(labels.consent)).toBeVisible();
        await expectNoBlockingAxeViolations(page, "/sign-in");
      });

      await test.step("a trusted new email creates the account and an HttpOnly session", async () => {
        await signInWithMicrosoft(page, {
          domainVerified: "true",
          email: run.email,
          tenantID: run.tenantA,
        });
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({ email: run.email, status: 200 });
        await expectSessionCookiesHttpOnly(context);
        expect(await cookieNamed(context, /link_tx/)).toBeUndefined();
        expect(await cookieNamed(context, /registration_tx/)).toBeUndefined();
        await expectNoSecretsInBrowser(page, [run.email, run.tenantA]);
      });

      await test.step("a password is set so the account can sign in natively", async () => {
        await createWorkspace(page, randomBytes(4).toString("hex"));
        await page.goto("/app/settings");
        const card = page.getByRole("region", { name: labels.passwordCard });
        await card.getByLabel(labels.newPassword).fill(run.password);
        await card.getByLabel(labels.confirmPassword).fill(run.password);
        const saved = responseTo(page, "POST", "/v1/me/password");
        await card.getByRole("button", { name: labels.setPassword }).click();
        expect((await saved).status()).toBe(200);
        await expect(card.getByText(labels.passwordSet)).toBeVisible();
      });
    } finally {
      await context.close();
    }
  });

  test("2. a personal account without xms_edov verifies an email with an OTP and lands in the app", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const { context, page } = await openDevice(browser);
    try {
      await test.step("the personal account parks a pending registration", async () => {
        await signInWithMicrosoft(page, {
          domainVerified: "",
          email: run.personalEmail,
          tenantID: personalTenantID,
        });
        await expect(page).toHaveURL(
          `${webOrigin}/complete-registration?provider=microsoft`,
        );
        await expect(
          page.getByRole("heading", { level: 1, name: labels.completeTitle }),
        ).toBeVisible();
        // The untrusted email is only a masked hint and never prefilled.
        await expect(page.getByText(labels.hint)).toContainText(
          maskedEmail(run.personalEmail),
        );
        await expect(page.locator("main")).not.toContainText(run.personalEmail);
        await expect(
          page.getByRole("textbox", { name: labels.email }),
        ).toHaveValue("");
        expect((await me(page)).status).toBe(401);
        const cookie = await cookieNamed(context, /registration_tx/);
        expect(cookie?.httpOnly).toBe(true);
        expect(cookie?.sameSite).toBe("Strict");
        await expectNoBlockingAxeViolations(page, "/complete-registration");
      });

      let code = "";
      await test.step("email + neutral 202 + OTP + terms create the account", async () => {
        // The only sign-up start of this file (see the budget above).
        const started = responseTo(
          page,
          "POST",
          "/v1/auth/pending-registration/start",
        );
        await page
          .getByRole("textbox", { name: labels.email })
          .fill(run.personalEmail);
        await page.getByRole("button", { name: labels.sendCode }).click();
        code = await waitForDevInboxCode(
          request,
          await readChallengeId(await started),
        );
        await expect(
          page.getByRole("heading", { name: labels.codeHeading }),
        ).toBeFocused();
        await expect(page.getByText(labels.neutralNotice)).toBeVisible();
        const otp = page.getByRole("textbox", { name: labels.otp });
        await expect(otp).toHaveAttribute("autocomplete", "one-time-code");
        await expect(otp).toHaveAttribute("inputmode", "numeric");
        await expectNoBlockingAxeViolations(
          page,
          "/complete-registration code step",
        );
        await otp.fill(code);
        await page.getByRole("checkbox", { name: labels.terms }).check();
        const completed = responseTo(
          page,
          "POST",
          "/v1/auth/pending-registration/complete",
        );
        await page.getByRole("button", { name: labels.finish }).click();
        const response = await completed;
        expect(response.status()).toBe(200);
        expect(((await response.json()) as { status?: string }).status).toBe(
          "authenticated",
        );
      });

      await test.step("the new account is signed in with HttpOnly cookies only", async () => {
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({
          email: run.personalEmail,
          status: 200,
        });
        await expectSessionCookiesHttpOnly(context);
        expect(await cookieNamed(context, /registration_tx/)).toBeUndefined();
        await expectNoSecretsInBrowser(page, [run.personalEmail, code]);
      });
    } finally {
      await context.close();
    }
  });

  test("3. a trusted email of an existing password account links on /account-link after signing in", async ({
    browser,
  }) => {
    test.setTimeout(300_000);
    const contexts: BrowserContext[] = [];
    try {
      const linking = await openDevice(browser);
      contexts.push(linking.context);
      const page = linking.page;

      await test.step("the owner's trusted email from tenant C parks a link intent", async () => {
        await signInWithMicrosoft(page, {
          domainVerified: "true",
          email: run.email,
          tenantID: run.tenantC,
        });
        await expect(page).toHaveURL(
          `${webOrigin}/account-link?provider=microsoft`,
        );
        await expect(
          page.getByRole("heading", { level: 1, name: labels.linkTitle }),
        ).toBeVisible();
        await expect(
          page.getByText(maskedEmail(run.email), { exact: false }),
        ).toBeVisible();
        await expect(page.locator("main")).not.toContainText(run.email);
        // No session: email is never proof of ownership.
        expect((await me(page)).status).toBe(401);
        const cookie = await cookieNamed(linking.context, /link_tx/);
        expect(cookie?.httpOnly).toBe(true);
        expect(cookie?.sameSite).toBe("Strict");
        await expect(
          page.getByRole("button", { name: labels.microsoft }),
        ).toHaveCount(0);
        await expectNoBlockingAxeViolations(page, "/account-link signed out");
      });

      await test.step("signing in with the password and confirming links Microsoft", async () => {
        await logInWithPassword(page, run.email, run.password);
        await expect(
          page.getByRole("heading", { name: labels.linkConfirmHeading }),
        ).toBeFocused();
        await expectNoBlockingAxeViolations(page, "/account-link signed in");
        const confirmed = responseTo(
          page,
          "POST",
          "/v1/auth/link-intent/confirm",
        );
        await page.getByRole("button", { name: labels.linkConfirm }).click();
        expect((await confirmed).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({ email: run.email, status: 200 });
        expect(await cookieNamed(linking.context, /link_tx/)).toBeUndefined();
        await expectNoSecretsInBrowser(page, [run.email, run.password]);
      });

      await test.step("the next tenant C sign-in goes straight into the same account", async () => {
        const next = await openDevice(browser);
        contexts.push(next.context);
        await signInWithMicrosoft(next.page, {
          domainVerified: "true",
          email: run.email,
          tenantID: run.tenantC,
        });
        await expect(next.page).toHaveURL(/\/app(?:\/home)?$/);
        expect(await me(next.page)).toEqual({ email: run.email, status: 200 });
        await expectSessionCookiesHttpOnly(next.context);
      });
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("4. the same trusted email from another tenant is never merged automatically", async ({
    browser,
  }) => {
    const { context, page } = await openDevice(browser);
    try {
      await test.step("tenant B parks a link intent instead of entering tenant A's account", async () => {
        await signInWithMicrosoft(page, {
          domainVerified: "true",
          email: run.email,
          tenantID: run.tenantB,
        });
        await expect(page).toHaveURL(
          `${webOrigin}/account-link?provider=microsoft`,
        );
        expect((await me(page)).status).toBe(401);
        expect(
          (await context.cookies()).filter(
            (cookie) => /session/i.test(cookie.name) && cookie.value !== "",
          ),
        ).toEqual([]);
        expect((await cookieNamed(context, /link_tx/))?.httpOnly).toBe(true);
      });

      await test.step("cancelling removes the request without linking tenant B", async () => {
        const cancelled = responseTo(
          page,
          "POST",
          "/v1/auth/link-intent/cancel",
        );
        await page.getByRole("button", { name: labels.linkCancel }).click();
        expect((await cancelled).status()).toBe(204);
        await expect(page).toHaveURL(`${webOrigin}/sign-in`);
        expect(await cookieNamed(context, /link_tx/)).toBeUndefined();
        expect((await me(page)).status).toBe(401);
      });
    } finally {
      await context.close();
    }
  });

  test("5. links Microsoft from Settings after a step-up and explains an untrusted email", async ({
    browser,
  }) => {
    test.setTimeout(300_000);
    const { context, page } = await openDevice(browser);
    try {
      await page.goto("/sign-in");
      await logInWithPassword(page, run.email, run.password);
      await expect(page).toHaveURL(/\/app\/home$/);
      await page.goto("/app/settings");
      const identityCard = page.getByRole("region", {
        name: labels.identityCard,
      });

      await test.step("a stale recent-auth window opens the shared step-up, without Microsoft", async () => {
        let injected = false;
        await page.route(
          (url) => url.pathname === "/api/v1/me/identities/link",
          async (route) => {
            if (injected) {
              await route.continue();
              return;
            }
            injected = true;
            await route.fulfill({
              body: JSON.stringify({
                code: "auth_recent_authentication_required",
                status: 403,
                title: "Recent authentication required",
                type: "about:blank",
              }),
              contentType: "application/problem+json",
              status: 403,
            });
          },
        );
        await identityCard
          .getByRole("button", { name: labels.identityLinkMicrosoft })
          .click();
        const dialog = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(dialog).toBeVisible();
        // Microsoft is never a re-authentication method.
        await expect(dialog).not.toContainText("Microsoft");
        await dialog.getByLabel(labels.stepUpPassword).fill(run.password);
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const retried = page.waitForResponse(
          (response) =>
            apiPath(response.url()) === "/api/v1/me/identities/link" &&
            response.status() === 200,
        );
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        expect((await reauth).status()).toBe(200);
        // Exactly one automatic retry, which the server accepts.
        const body = JSON.parse((await retried).request().postData() ?? "{}");
        expect(body).toEqual({ provider: "microsoft" });
      });

      await test.step("an account without xms_edov is not linked and Settings explains why", async () => {
        const redirected = page.waitForRequest(
          (request) =>
            request.isNavigationRequest() &&
            request.url() === `${webOrigin}/sign-in?error=email_unverified`,
        );
        await chooseFakeMicrosoftAccount(page, {
          domainVerified: "",
          email: run.settingsEmail,
          tenantID: run.tenantD,
        });
        await redirected;
        await expect(page).toHaveURL(`${webOrigin}/app/settings`);
        const notice = identityCard.getByRole("alert");
        await expect(notice.getByText(labels.guidanceTitle)).toBeVisible();
        await expect(notice.getByText(labels.guidanceMicrosoft)).toBeVisible();
        await expect(
          notice.getByRole("button", { name: labels.signOut }),
        ).toBeVisible();
        expect(await me(page)).toEqual({ email: run.email, status: 200 });
        await expect(identityCard).not.toContainText(run.settingsEmail);
        await expectNoBlockingAxeViolations(page, "/app/settings guidance");
      });

      await test.step("a trusted Microsoft account links and returns to Settings", async () => {
        const started = page.waitForResponse(
          (response) =>
            apiPath(response.url()) === "/api/v1/me/identities/link" &&
            response.status() === 200,
        );
        await identityCard
          .getByRole("button", { name: labels.identityLinkMicrosoft })
          .click();
        await started;
        await chooseFakeMicrosoftAccount(page, {
          domainVerified: "true",
          email: run.settingsEmail,
          tenantID: run.tenantE,
        });
        await expect(
          identityCard.getByText(labels.identityLinked),
        ).toBeVisible();
        await expect(page).toHaveURL(`${webOrigin}/app/settings`);
        await expect(identityCard.getByText(labels.guidanceTitle)).toHaveCount(
          0,
        );
        await expect(identityCard).toContainText(run.settingsEmail);
        await expect(
          page
            .getByRole("region", { name: labels.securityCard })
            .getByText(labels.microsoftMethod)
            .first(),
        ).toBeVisible();
        await expectSessionCookiesHttpOnly(context);
        await expectNoSecretsInBrowser(page, [
          run.password,
          run.settingsEmail,
          run.tenantE,
        ]);
      });
    } finally {
      await context.close();
    }
  });

  test("6. tenant A still signs in directly; Microsoft screens stay accessible and Web Storage clean", async ({
    browser,
  }) => {
    const { context, page } = await openDevice(browser);
    try {
      await signInWithMicrosoft(page, {
        domainVerified: "true",
        email: run.email,
        tenantID: run.tenantA,
      });
      await expect(page).toHaveURL(/\/app(?:\/home)?$/);
      expect(await me(page)).toEqual({ email: run.email, status: 200 });

      await page.goto("/app/settings");
      const identityCard = page.getByRole("region", {
        name: labels.identityCard,
      });
      // Tenants A, C and E are three identities; B and D were never linked.
      await expect(
        identityCard.locator(".identity-row__title strong", {
          hasText: /^Microsoft$/,
        }),
      ).toHaveCount(3);
      await expect(identityCard).not.toContainText("login.microsoftonline.com");
      await expectNoBlockingAxeViolations(page, "/app/settings identities");

      const storage = await page.evaluate(() => ({
        local: Object.keys(window.localStorage),
        session: Object.keys(window.sessionStorage),
      }));
      for (const key of [...storage.local, ...storage.session]) {
        expect(key).not.toMatch(/token|session|csrf|link|registration/i);
      }
      await expectSessionCookiesHttpOnly(context);
      await expectNoSecretsInBrowser(page, [
        run.email,
        run.password,
        run.personalEmail,
        run.settingsEmail,
        run.tenantA,
        run.tenantB,
        run.tenantC,
      ]);
    } finally {
      await context.close();
    }
  });
});
