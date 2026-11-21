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
import { createHash, createHmac, randomBytes } from "node:crypto";

/**
 * AUTH-06B: Facebook sign-in, pending registration (email + OTP), explicit
 * linking to an existing account, and Meta's data deletion / deauthorize
 * callbacks, against the loopback stack only. Requires
 * `node scripts/e2e-local.mjs serve --native-auth` (started by
 * playwright.auth.config.ts), which also starts a third fake issuer on
 * 127.0.0.1:9093 that serves Meta's fixed Login paths — Meta itself is never
 * called. The per-run app secret (E2E_FACEBOOK_CLIENT_SECRET) comes from the
 * Playwright config so this spec can sign `signed_request` callbacks.
 *
 * The scenarios share one account and run in order: the stack allows only
 * ten sign-up starts per IP per day and AUTH-02..05 already use eight, so this
 * file starts exactly two (one per pending registration) and builds the
 * "password account" by setting a password on the first one in Settings.
 */

const apiOrigin = "http://127.0.0.1:8080";
// AUTH-08: the native-auth stack serves the SPA on the localhost RP origin.
const webOrigin = "http://localhost:5173";
const facebookOrigin = "http://127.0.0.1:9093";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);

const labels = {
  facebook: /^(?:Tiếp tục với Facebook|Continue with Facebook)$/,
  completeTitle:
    /^(?:Hoàn tất đăng ký bằng Facebook|Finish signing up with Facebook)$/,
  hint: /(?:Gợi ý từ Facebook|Suggested by Facebook)/,
  email: /^(?:Địa chỉ email|Email address)$/,
  sendCode: /^(?:Gửi mã|Send code)$/,
  codeHeading:
    /^(?:Bước 2: Nhập mã xác minh|Step 2: Enter the verification code)$/,
  neutralNotice:
    /(?:Nếu địa chỉ hợp lệ, mã 6 chữ số đã được gửi|If the address is valid, a 6-digit code has been sent)/,
  otp: /^(?:Mã xác minh|Verification code)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  finish: /^(?:Hoàn tất đăng ký|Finish sign-up)$/,
  cancel: /^(?:Hủy đăng ký|Cancel sign-up)$/,
  registrationUnavailable:
    /^(?:Yêu cầu đăng ký không còn hiệu lực|This sign-up request is no longer valid)$/,
  linkTitle:
    /^(?:Liên kết Facebook với tài khoản TutorHub|Link Facebook to your TutorHub account)$/,
  linkConfirmHeading: /^(?:Xác nhận liên kết|Confirm linking)$/,
  linkConfirm: /^(?:Liên kết Facebook|Link Facebook)$/,
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
  deletionTitle:
    /^(?:Trạng thái yêu cầu xóa dữ liệu Facebook|Facebook data deletion status)$/,
  deletionCompleted: /^(?:Đã hoàn tất|Completed)$/,
  deletionNotFound: /^(?:Không tìm thấy mã|Code not found)$/,
  deletionGuide: /^(?:Cách yêu cầu xóa dữ liệu|How to request deletion)$/,
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

/** Same derivation as e2eoidc.FacebookTestASID (Go test pins the vectors). */
function facebookTestASID(seed: string) {
  const digest = createHash("sha256").update(seed).digest("hex");
  const value = BigInt(`0x${digest.slice(0, 16)}`) % 10n ** 17n;
  return `1${value.toString().padStart(17, "0")}`;
}

function facebookAppSecret() {
  const secret = process.env.E2E_FACEBOOK_CLIENT_SECRET?.trim();
  if (!secret) {
    throw new Error(
      "E2E_FACEBOOK_CLIENT_SECRET is missing: run through playwright.auth.config.ts.",
    );
  }
  return secret;
}

/** Meta's `signed_request`: base64url(HMAC-SHA256(secret, payload)).payload */
function signedRequest(asid: string) {
  const payload = Buffer.from(
    JSON.stringify({
      algorithm: "HMAC-SHA256",
      issued_at: Math.floor(Date.now() / 1000),
      user_id: asid,
    }),
  ).toString("base64url");
  const signature = createHmac("sha256", facebookAppSecret())
    .update(payload)
    .digest("base64url");
  return `${signature}.${payload}`;
}

function uniqueEmail(label: string) {
  return `auth06.${label}.${randomBytes(6).toString("hex")}@example.test`;
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
    reducedMotion: "reduce",
  });
  return { context, page: await context.newPage() };
}

/** Clicks Facebook on /sign-in and chooses a custom account on the fake. */
async function signInWithFacebook(
  page: Page,
  seed: string,
  { shareEmail }: { shareEmail: boolean },
) {
  await page.goto("/sign-in");
  const started = page.waitForRequest(
    (request) => apiPath(request.url()) === "/api/v1/auth/oauth/facebook/start",
  );
  await page.getByRole("button", { name: labels.facebook }).click();
  // A top-level navigation, never a script fetch (RFC 9700 §4.4).
  expect((await started).isNavigationRequest()).toBe(true);
  await page.waitForURL(
    (url) =>
      url.origin === facebookOrigin && url.pathname === "/v27.0/dialog/oauth",
  );
  await page.getByRole("textbox", { name: "Test email" }).fill(seed);
  if (!shareEmail) {
    await page
      .getByRole("checkbox", { name: "Do not share email with the app" })
      .check();
  }
  await page
    .getByRole("button", { name: "Sign in with custom account" })
    .click();
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
  expect(stored).not.toMatch(/registration_tx|link_tx|tutorhub_session|csrf/i);
  for (const secret of secrets) {
    expect(stored).not.toContain(secret);
  }
}

/**
 * Runs the /complete-registration form: email → neutral 202 → devinbox code
 * → terms → complete. Returns the complete response and the code used.
 */
async function completeRegistration(
  page: Page,
  request: APIRequestContext,
  email: string,
  { axeLabel }: { axeLabel?: string } = {},
) {
  const started = responseTo(
    page,
    "POST",
    "/v1/auth/pending-registration/start",
  );
  await page.getByRole("textbox", { name: labels.email }).fill(email);
  await page.getByRole("button", { name: labels.sendCode }).click();
  const code = await waitForDevInboxCode(
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
  if (axeLabel) await expectNoBlockingAxeViolations(page, axeLabel);
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
  return {
    body: (await response.json()) as { status?: string },
    code,
  };
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
    .fill(`AUTH06 Workspace ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth06-${suffix}`);
  const created = responseTo(page, "POST", "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

/** Shared by the ordered scenarios below (see the file comment). */
const run = {
  /** Facebook seed (and thus ASID) used to create the account. */
  firstSeed: `auth06.first.${randomBytes(6).toString("hex")}@facebook.test`,
  /** Facebook seed that later links to the same account by email. */
  secondSeed: "",
  email: uniqueEmail("owner"),
  password: `Harbor-${randomBytes(9).toString("base64url")}-Lantern7`,
};
run.secondSeed = run.email;

test.describe("AUTH-06 Facebook federation, pending registration and Meta callbacks", () => {
  test.describe.configure({ mode: "serial" });

  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "Facebook federation runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      oauth_providers: expect.arrayContaining(["facebook"]),
      password_enabled: true,
      signup_enabled: true,
    });
  });

  test("1. a Facebook account without an email verifies one and lands in the app", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const { context, page } = await openDevice(browser);
    try {
      await test.step("the sign-in page offers Facebook", async () => {
        await page.goto("/sign-in");
        await expect(
          page.getByRole("button", { name: labels.facebook }),
        ).toBeEnabled();
        await expectNoBlockingAxeViolations(page, "/sign-in");
      });

      await test.step("a new subject parks a pending registration", async () => {
        await signInWithFacebook(page, run.firstSeed, { shareEmail: false });
        await expect(page).toHaveURL(
          `${webOrigin}/complete-registration?provider=facebook`,
        );
        await expect(
          page.getByRole("heading", { level: 1, name: labels.completeTitle }),
        ).toBeVisible();
        // Facebook shared no email: no hint, and nothing is prefilled.
        await expect(page.getByText(labels.hint)).toHaveCount(0);
        await expect(
          page.getByRole("textbox", { name: labels.email }),
        ).toHaveValue("");
        expect((await me(page)).status).toBe(401);
        const cookie = await cookieNamed(context, /registration_tx/);
        expect(cookie?.httpOnly).toBe(true);
        expect(cookie?.sameSite).toBe("Strict");
        await expectNoBlockingAxeViolations(page, "/complete-registration");
      });

      await test.step("email + OTP + terms create the account and a session", async () => {
        const { body, code } = await completeRegistration(
          page,
          request,
          run.email,
          { axeLabel: "/complete-registration code step" },
        );
        expect(body.status).toBe("authenticated");
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({ email: run.email, status: 200 });
        await expectSessionCookiesHttpOnly(context);
        expect(await cookieNamed(context, /registration_tx/)).toBeUndefined();
        await expectNoSecretsInBrowser(page, [run.email, code]);
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

  test("2. a Facebook email that already has an owner links after password sign-in", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const contexts: BrowserContext[] = [];
    try {
      const linking = await openDevice(browser);
      contexts.push(linking.context);
      const page = linking.page;

      await test.step("Facebook shares the owner's email as a masked hint only", async () => {
        await signInWithFacebook(page, run.secondSeed, { shareEmail: true });
        await expect(page).toHaveURL(
          `${webOrigin}/complete-registration?provider=facebook`,
        );
        await expect(
          page.getByText(maskedEmail(run.email), { exact: false }),
        ).toBeVisible();
        await expect(page.locator("main")).not.toContainText(run.email);
      });

      await test.step("a correct code turns into a link intent, not a session", async () => {
        const { body } = await completeRegistration(page, request, run.email);
        expect(body.status).toBe("link_pending");
        await expect(page).toHaveURL(
          `${webOrigin}/account-link?provider=facebook`,
        );
        await expect(
          page.getByRole("heading", { level: 1, name: labels.linkTitle }),
        ).toBeVisible();
        expect((await me(page)).status).toBe(401);
        expect((await cookieNamed(linking.context, /link_tx/))?.httpOnly).toBe(
          true,
        );
      });

      await test.step("signing in with the password and confirming links Facebook", async () => {
        const form = page.getByRole("form", { name: labels.loginForm });
        await form
          .getByRole("textbox", { name: labels.identifier })
          .fill(run.email);
        await form.getByLabel(labels.password).fill(run.password);
        const login = responseTo(page, "POST", "/v1/auth/password/login");
        await form.getByRole("button", { name: labels.logIn }).click();
        expect((await login).status()).toBe(200);
        await expect(
          page.getByRole("heading", { name: labels.linkConfirmHeading }),
        ).toBeFocused();
        const confirmed = responseTo(
          page,
          "POST",
          "/v1/auth/link-intent/confirm",
        );
        await page.getByRole("button", { name: labels.linkConfirm }).click();
        expect((await confirmed).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({ email: run.email, status: 200 });
        await expectNoSecretsInBrowser(page, [run.email, run.password]);
      });

      await test.step("the next Facebook sign-in goes straight into the app", async () => {
        const next = await openDevice(browser);
        contexts.push(next.context);
        await signInWithFacebook(next.page, run.secondSeed, {
          shareEmail: true,
        });
        await expect(next.page).toHaveURL(/\/app(?:\/home)?$/);
        expect(await me(next.page)).toEqual({ email: run.email, status: 200 });
        await expectSessionCookiesHttpOnly(next.context);
      });
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("3. data deletion erases the first ASID and issues a public receipt", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const { context, page } = await openDevice(browser);
    try {
      let receiptURL = "";
      await test.step("the signed callback returns a confirmation code", async () => {
        const response = await request.post(
          `${apiOrigin}/api/v1/auth/oauth/facebook/data-deletion`,
          {
            form: {
              signed_request: signedRequest(facebookTestASID(run.firstSeed)),
            },
          },
        );
        expect(response.status()).toBe(200);
        const body = (await response.json()) as {
          confirmation_code?: string;
          url?: string;
        };
        expect(body.confirmation_code).toMatch(/^[0-9a-f]{32}$/);
        expect(body.url).toBe(
          `${webOrigin}/legal/data-deletion?code=${body.confirmation_code}`,
        );
        receiptURL = body.url ?? "";
      });

      await test.step("an unsigned or tampered callback is rejected", async () => {
        const tampered = signedRequest(facebookTestASID(run.firstSeed)).replace(
          /^./,
          (character) => (character === "A" ? "B" : "A"),
        );
        const response = await request.post(
          `${apiOrigin}/api/v1/auth/oauth/facebook/data-deletion`,
          { form: { signed_request: tampered } },
        );
        expect(response.status()).toBe(400);
      });

      await test.step("the receipt page shows the coarse status only", async () => {
        const status = page.waitForResponse((response) =>
          apiPath(response.url()).startsWith(
            "/api/v1/auth/oauth/facebook/data-deletion/",
          ),
        );
        await page.goto(receiptURL);
        expect((await status).status()).toBe(200);
        await expect(
          page.getByRole("heading", { level: 1, name: labels.deletionTitle }),
        ).toBeVisible();
        await expect(page.getByText(labels.deletionCompleted)).toBeVisible();
        await expect(
          page.getByRole("heading", { name: labels.deletionGuide }),
        ).toBeVisible();
        await expect(page.locator("main")).not.toContainText(run.email);
        await expectNoBlockingAxeViolations(page, "/legal/data-deletion");
        await expectNoSecretsInBrowser(page, [run.email]);
      });

      await test.step("the erased ASID starts a new pending registration", async () => {
        await signInWithFacebook(page, run.firstSeed, { shareEmail: false });
        await expect(page).toHaveURL(
          `${webOrigin}/complete-registration?provider=facebook`,
        );
        expect((await me(page)).status).toBe(401);
      });

      await test.step("5. cancelling removes the pending registration", async () => {
        const cancelled = responseTo(
          page,
          "POST",
          "/v1/auth/pending-registration/cancel",
        );
        await page.getByRole("button", { name: labels.cancel }).click();
        expect((await cancelled).status()).toBe(204);
        await expect(page).toHaveURL(`${webOrigin}/sign-in`);
        expect(await cookieNamed(context, /registration_tx/)).toBeUndefined();

        const preview = responseTo(
          page,
          "GET",
          "/v1/auth/pending-registration",
        );
        await page.goto("/complete-registration?provider=facebook");
        expect((await preview).status()).toBe(404);
        await expect(
          page.getByRole("heading", { name: labels.registrationUnavailable }),
        ).toBeVisible();
      });

      await test.step("5. an unknown receipt code is reported, a malformed one is not sent", async () => {
        const unknown = randomBytes(16).toString("hex");
        const lookup = page.waitForResponse((response) =>
          apiPath(response.url()).endsWith(`/data-deletion/${unknown}`),
        );
        await page.goto(`/legal/data-deletion?code=${unknown}`);
        expect((await lookup).status()).toBe(404);
        await expect(
          page.getByRole("heading", { name: labels.deletionNotFound }),
        ).toBeVisible();

        let called = false;
        page.on("request", (request) => {
          if (apiPath(request.url()).includes("/data-deletion/")) called = true;
        });
        await page.goto(
          "/legal/data-deletion?code=%3Cb%3Enot-a-code%3C%2Fb%3E",
        );
        await expect(
          page.getByRole("heading", { name: labels.deletionNotFound }),
        ).toBeVisible();
        expect(called).toBe(false);
        expect(await page.content()).not.toContain("not-a-code");
      });
    } finally {
      await context.close();
    }
  });

  test("4. deauthorize unlinks Facebook and revokes the sessions it created", async ({
    browser,
    request,
  }) => {
    const { context, page } = await openDevice(browser);
    try {
      await signInWithFacebook(page, run.secondSeed, { shareEmail: true });
      await expect(page).toHaveURL(/\/app(?:\/home)?$/);
      expect(await me(page)).toEqual({ email: run.email, status: 200 });

      const response = await request.post(
        `${apiOrigin}/api/v1/auth/oauth/facebook/deauthorize`,
        {
          form: {
            signed_request: signedRequest(facebookTestASID(run.secondSeed)),
          },
        },
      );
      expect(response.status()).toBe(204);
      expect((await me(page)).status).toBe(401);
    } finally {
      await context.close();
    }
  });
});
