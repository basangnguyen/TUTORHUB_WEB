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
import { randomBytes } from "node:crypto";

/**
 * AUTH-05B: Google sign-in, explicit account linking (link intent) and
 * linking Google from Settings, against the loopback stack only. Requires
 * `node scripts/e2e-local.mjs serve --native-auth` (started by
 * playwright.auth.config.ts), which also starts a second fake issuer on
 * 127.0.0.1:9092 that stands in for Google — Google itself is never called.
 * Each scenario signs in to the fake with its own custom test email, so the
 * spec is independent of the order and of earlier runs on the same database.
 *
 * Sessions in this run are younger than the 10-minute recent-authentication
 * window, so the Settings step-up is triggered by injecting one 403
 * `auth_recent_authentication_required`; everything else is real.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://127.0.0.1:5173";
const googleOrigin = "http://127.0.0.1:9092";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);

const labels = {
  signUp: /^(?:Đăng ký|Sign Up)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  loginForm: /^(?:Đăng nhập vào TutorHub|Log in to TutorHub)$/,
  signUpForm: /^(?:Tạo tài khoản mới|Create an Account)$/,
  fullName: /^(?:Họ và tên|Full Name)$/,
  email: /^(?:Địa chỉ email|Email Address)$/,
  getCode: /^(?:Lấy mã|Get Code)$/,
  otp: /^(?:Mã OTP|OTP code)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  google: /^(?:Đăng nhập bằng Google|Sign in with Google)$/,
  consent:
    /^(?:Bằng việc tiếp tục, bạn đồng ý với|By continuing, you agree to the)/,
  linkTitle:
    /^(?:Liên kết Google với tài khoản TutorHub|Link Google to your TutorHub account)$/,
  linkSignIn:
    /^(?:Đăng nhập tài khoản TutorHub hiện có|Sign in to your existing TutorHub account)$/,
  linkConfirmHeading: /^(?:Xác nhận liên kết|Confirm linking)$/,
  linkConfirm: /^(?:Liên kết Google|Link Google)$/,
  linkCancel: /^(?:Hủy|Cancel)$/,
  linkUnavailable:
    /^(?:Yêu cầu liên kết không còn hiệu lực|This linking request is no longer valid)$/,
  linkMismatch:
    /(?:Bạn đang đăng nhập một tài khoản khác|You're signed in to a different account)/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  emailUnverified:
    /(?:Email của tài khoản bạn chọn chưa được nhà cung cấp xác minh|The email address of the account you chose isn't verified)/,
  callbackUnknown:
    /^(?:Không thể hoàn tất đăng nhập\. Vui lòng thử lại\.|We couldn't complete that sign-in\. Please try again\.)$/,
  identityCard: /^(?:Danh tính đăng nhập|Sign-in identities)$/,
  identityLinkGoogle: /^(?:Liên kết Google|Link Google)$/,
  identityLinked:
    /^(?:Đã liên kết danh tính đăng nhập\.|Sign-in identity linked\.)$/,
  securityCard: /^(?:Bảo mật tài khoản|Account security)$/,
  googleMethod: /^(?:Tài khoản Google|Google account)$/,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
  stepUpConfirm: /^(?:Xác nhận|Confirm)$/,
} as const;

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  purpose: string;
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
      { message: "expected the sign-up code", timeout: 15_000 },
    )
    .toBe("delivered");
  expect(found?.purpose).toBe("signup_email");
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

interface NativeAccount {
  displayName: string;
  email: string;
  password: string;
}

function newNativeAccount(label: string): NativeAccount {
  const suffix = randomBytes(6).toString("hex");
  return {
    displayName: `AUTH05 ${label} ${suffix.slice(0, 4)}`,
    email: `auth05.${label}.${suffix}@example.test`,
    password: `Harbor-${randomBytes(9).toString("base64url")}-Lantern7`,
  };
}

function newGoogleEmail(label: string) {
  return `auth05.google.${label}.${randomBytes(6).toString("hex")}@example.test`;
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

async function signUp(
  page: Page,
  request: APIRequestContext,
  account: NativeAccount,
) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: labels.signUp }).click();
  const form = page.getByRole("form", { name: labels.signUpForm });
  await form
    .getByRole("textbox", { name: labels.fullName })
    .fill(account.displayName);
  await form.getByRole("textbox", { name: labels.email }).fill(account.email);
  const started = responseTo(page, "POST", "/v1/auth/signup/email/start");
  await form.getByRole("button", { name: labels.getCode }).click();
  const code = await waitForDevInboxCode(
    request,
    await readChallengeId(await started),
  );
  await form.getByRole("textbox", { name: labels.otp }).fill(code);
  await form.getByLabel(labels.password).fill(account.password);
  await form.getByRole("checkbox", { name: labels.terms }).check();
  const complete = responseTo(page, "POST", "/v1/auth/signup/email/complete");
  await form.getByRole("button", { name: labels.createAccount }).click();
  expect((await complete).status()).toBe(201);
  await expect(page).toHaveURL(/\/app\/home$/);
  return code;
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
    .fill(`AUTH05 Workspace ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth05-${suffix}`);
  const created = responseTo(page, "POST", "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

/**
 * Clicks the Google button (or expects to already be on the fake chooser)
 * and signs in with a custom test email on the loopback fake issuer.
 */
async function chooseFakeGoogleAccount(
  page: Page,
  email: string,
  { unverified = false } = {},
) {
  await page.waitForURL(
    (url) => url.origin === googleOrigin && url.pathname === "/authorize",
  );
  await page.getByRole("textbox", { name: "Test email" }).fill(email);
  if (unverified) {
    await page.getByRole("checkbox", { name: "Email is not verified" }).check();
  }
  await page
    .getByRole("button", { name: "Sign in with custom account" })
    .click();
}

async function signInWithGoogle(
  page: Page,
  email: string,
  options: { unverified?: boolean } = {},
) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: labels.google }).click();
  await chooseFakeGoogleAccount(page, email, options);
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

async function expectSessionCookiesHttpOnly(context: BrowserContext) {
  const cookies = (await context.cookies()).filter((cookie) =>
    /session/i.test(cookie.name),
  );
  expect(cookies.length).toBeGreaterThan(0);
  for (const cookie of cookies) {
    expect(cookie.httpOnly).toBe(true);
  }
}

async function linkCookie(context: BrowserContext) {
  return (await context.cookies()).find((cookie) =>
    /link_tx/.test(cookie.name),
  );
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
  expect(stored).not.toMatch(/link_tx|tutorhub_session|csrf/i);
  for (const secret of secrets) {
    expect(stored).not.toContain(secret);
  }
}

test.describe("AUTH-05 Google federation and explicit account linking", () => {
  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "Google federation runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      oauth_providers: expect.arrayContaining(["google"]),
      password_enabled: true,
      signup_enabled: true,
    });
  });

  test("signs up with Google and lands in the app with HttpOnly cookies only", async ({
    browser,
  }) => {
    const email = newGoogleEmail("new");
    const { context, page } = await openDevice(browser);
    try {
      await test.step("the sign-in page offers Google with the consent line", async () => {
        await page.goto("/sign-in");
        await expect(
          page.getByRole("button", { name: labels.google }),
        ).toBeEnabled();
        await expect(page.getByText(labels.consent)).toBeVisible();
        await expectNoBlockingAxeViolations(page, "/sign-in");
      });

      await test.step("a new Google account is created and signed in", async () => {
        const started = page.waitForRequest(
          (request) =>
            apiPath(request.url()) === "/api/v1/auth/oauth/google/start",
        );
        await page.getByRole("button", { name: labels.google }).click();
        // A top-level navigation, never a script fetch (RFC 9700 §4.4).
        expect((await started).isNavigationRequest()).toBe(true);
        await chooseFakeGoogleAccount(page, email);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({ email, status: 200 });
        await expectSessionCookiesHttpOnly(context);
        expect(await linkCookie(context)).toBeUndefined();
        await expectNoSecretsInBrowser(page, [email]);
      });
    } finally {
      await context.close();
    }
  });

  test("links Google to an existing password account on /account-link", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newNativeAccount("link");
    const contexts: BrowserContext[] = [];
    try {
      const owner = await openDevice(browser);
      contexts.push(owner.context);
      const otpCode = await signUp(owner.page, request, account);

      const linking = await openDevice(browser);
      contexts.push(linking.context);
      const page = linking.page;

      await test.step("the same email from Google parks a link intent", async () => {
        await signInWithGoogle(page, account.email);
        await expect(page).toHaveURL(/\/account-link\?provider=google$/);
        await expect(
          page.getByRole("heading", { level: 1, name: labels.linkTitle }),
        ).toBeVisible();
        await expect(
          page.getByText(maskedEmail(account.email), { exact: false }),
        ).toBeVisible();
        await expect(page.locator("main")).not.toContainText(account.email);
        // No session was created and the link cookie is HttpOnly.
        expect((await me(page)).status).toBe(401);
        const cookie = await linkCookie(linking.context);
        expect(cookie?.httpOnly).toBe(true);
        expect(cookie?.sameSite).toBe("Strict");
        await expectNoSecretsInBrowser(page, [account.email]);
        await expectNoBlockingAxeViolations(page, "/account-link signed out");
      });

      await test.step("signing in with the password stays on the page", async () => {
        await expect(
          page.getByRole("heading", { name: labels.linkSignIn }),
        ).toBeVisible();
        await expect(
          page.getByRole("button", { name: labels.google }),
        ).toHaveCount(0);
        const form = page.getByRole("form", { name: labels.loginForm });
        await form
          .getByRole("textbox", { name: labels.identifier })
          .fill(account.email);
        await form.getByLabel(labels.password).fill(account.password);
        const login = responseTo(page, "POST", "/v1/auth/password/login");
        await form.getByRole("button", { name: labels.logIn }).click();
        expect((await login).status()).toBe(200);
        const heading = page.getByRole("heading", {
          name: labels.linkConfirmHeading,
        });
        await expect(heading).toBeFocused();
        await expect(page).toHaveURL(/\/account-link\?provider=google$/);
        await expectNoBlockingAxeViolations(page, "/account-link signed in");
      });

      await test.step("confirming links Google and signs out the other device", async () => {
        const confirmed = responseTo(
          page,
          "POST",
          "/v1/auth/link-intent/confirm",
        );
        await page.getByRole("button", { name: labels.linkConfirm }).click();
        expect((await confirmed).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({ email: account.email, status: 200 });
        expect(await linkCookie(linking.context)).toBeUndefined();
        await expectSessionCookiesHttpOnly(linking.context);
        // The owner device signed up earlier: confirm revoked that session.
        expect((await me(owner.page)).status).toBe(401);
        await expectNoSecretsInBrowser(page, [
          account.email,
          account.password,
          otpCode,
        ]);
      });

      await test.step("the next Google sign-in goes straight into the app", async () => {
        const next = await openDevice(browser);
        contexts.push(next.context);
        await signInWithGoogle(next.page, account.email);
        await expect(next.page).toHaveURL(/\/app\/home$/);
        expect(await me(next.page)).toEqual({
          email: account.email,
          status: 200,
        });
      });
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("cancelling a pending link removes it", async ({ browser, request }) => {
    const account = newNativeAccount("cancel");
    const contexts: BrowserContext[] = [];
    try {
      const owner = await openDevice(browser);
      contexts.push(owner.context);
      await signUp(owner.page, request, account);

      const { context, page } = await openDevice(browser);
      contexts.push(context);
      await signInWithGoogle(page, account.email);
      await expect(page).toHaveURL(/\/account-link\?provider=google$/);

      const cancelled = responseTo(page, "POST", "/v1/auth/link-intent/cancel");
      await page.getByRole("button", { name: labels.linkCancel }).click();
      expect((await cancelled).status()).toBe(204);
      await expect(page).toHaveURL(/\/sign-in$/);
      expect(await linkCookie(context)).toBeUndefined();

      const preview = responseTo(page, "GET", "/v1/auth/link-intent");
      await page.goto("/account-link?provider=google");
      expect((await preview).status()).toBe(404);
      await expect(
        page.getByRole("heading", { name: labels.linkUnavailable }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: labels.linkConfirm }),
      ).toHaveCount(0);
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("refuses to link while signed in to another account and keeps the request", async ({
    browser,
    request,
  }) => {
    const target = newNativeAccount("target");
    const otherGoogleEmail = newGoogleEmail("other");
    const contexts: BrowserContext[] = [];
    try {
      const owner = await openDevice(browser);
      contexts.push(owner.context);
      await signUp(owner.page, request, target);

      const { context, page } = await openDevice(browser);
      contexts.push(context);
      await signInWithGoogle(page, otherGoogleEmail);
      await expect(page).toHaveURL(/\/app\/home$/);

      await signInWithGoogle(page, target.email);
      await expect(page).toHaveURL(/\/account-link\?provider=google$/);
      await expect(page.getByText(otherGoogleEmail)).toBeVisible();

      const confirmed = responseTo(
        page,
        "POST",
        "/v1/auth/link-intent/confirm",
      );
      await page.getByRole("button", { name: labels.linkConfirm }).click();
      const response = await confirmed;
      expect(response.status()).toBe(403);
      expect(await response.json()).toMatchObject({
        code: "auth_link_intent_mismatch",
      });
      const alert = page.getByRole("alert");
      await expect(alert).toContainText(labels.linkMismatch);
      await expect(
        alert.getByRole("button", { name: labels.signOut }),
      ).toBeVisible();
      // A 403 never ends the session, and the request is still pending.
      expect(await me(page)).toEqual({ email: otherGoogleEmail, status: 200 });
      const preview = responseTo(page, "GET", "/v1/auth/link-intent");
      await page.reload();
      expect((await preview).status()).toBe(200);
      await expect(
        page.getByText(maskedEmail(target.email), { exact: false }),
      ).toBeVisible();
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("maps callback errors to fixed messages and cleans the address", async ({
    browser,
  }) => {
    const { context, page } = await openDevice(browser);
    try {
      await test.step("a real unverified Google email ends on ?error=email_unverified", async () => {
        // The SPA strips the parameter at once, so watch the redirect itself.
        const redirected = page.waitForRequest(
          (request) =>
            request.isNavigationRequest() &&
            request.url() === `${webOrigin}/sign-in?error=email_unverified`,
        );
        await signInWithGoogle(page, newGoogleEmail("unverified"), {
          unverified: true,
        });
        await redirected;
        await expect(page.getByRole("alert")).toContainText(
          labels.emailUnverified,
        );
        await expect(page).toHaveURL(`${webOrigin}/sign-in`);
        expect((await me(page)).status).toBe(401);
      });

      await test.step("an unknown code shows the generic message only", async () => {
        await page.goto("/sign-in?error=auth05%3Cb%3Einjected%3C%2Fb%3E");
        await expect(page.getByRole("alert")).toHaveText(
          labels.callbackUnknown,
        );
        await expect(page).toHaveURL(`${webOrigin}/sign-in`);
        expect(await page.content()).not.toContain("injected");
      });
    } finally {
      await context.close();
    }
  });

  test("links Google from Settings after a step-up", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newNativeAccount("settings");
    const googleEmail = newGoogleEmail("settings");
    const { context, page } = await openDevice(browser);
    try {
      await signUp(page, request, account);
      await createWorkspace(page, randomBytes(4).toString("hex"));
      await page.goto("/app/settings");
      const identityCard = page.getByRole("region", {
        name: labels.identityCard,
      });

      await test.step("a stale recent-auth window opens the shared step-up", async () => {
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
          .getByRole("button", { name: labels.identityLinkGoogle })
          .click();
        const dialog = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(dialog).toBeVisible();
        await dialog.getByLabel(labels.stepUpPassword).fill(account.password);
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
        expect(body).toEqual({ provider: "google" });
      });

      await test.step("Google returns to Settings with the identity linked", async () => {
        await chooseFakeGoogleAccount(page, googleEmail);
        await expect(
          identityCard.getByText(labels.identityLinked),
        ).toBeVisible();
        await expect(page).toHaveURL(`${webOrigin}/app/settings`);
        await expect(
          identityCard.locator(".identity-row__title strong", {
            hasText: /^Google$/,
          }),
        ).toBeVisible();
        await expect(identityCard).toContainText(googleEmail);
        await expect(
          page
            .getByRole("region", { name: labels.securityCard })
            .getByText(labels.googleMethod),
        ).toBeVisible();
        await expectSessionCookiesHttpOnly(context);
        await expectNoSecretsInBrowser(page, [account.password, googleEmail]);
      });
    } finally {
      await context.close();
    }
  });
});
