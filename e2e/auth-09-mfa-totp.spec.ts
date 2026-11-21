import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Browser,
  type Page,
  type Request,
  type Response,
} from "@playwright/test";
import { createHmac, randomBytes } from "node:crypto";

/**
 * AUTH-09B: authenticator-app (TOTP) two-step verification end to end —
 * enrollment with a locally drawn QR code, one-time recovery codes, the
 * second sign-in step after a password, an OAuth callback and a password
 * recovery, recovery-code regeneration and turning the app off — against the
 * loopback stack only. Requires `node scripts/e2e-local.mjs serve
 * --native-auth`, which turns on AUTH_MFA_TOTP_ENABLED (and keeps
 * AUTH_REQUIRE_ADMIN_MFA off).
 *
 * TOTP codes are computed here (RFC 6238, SHA-1, 6 digits, 30 s) from the
 * seed the enrollment returns. The server only accepts a step newer than the
 * last one it accepted (`step > last_used_step`), so a code is never reused:
 * the spec waits for the next 30-second step when it needs another code and
 * uses that very rule to prove replay protection. Each scenario uses its own
 * account so the per-account limit (5 attempts / 300 s) is never reached.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://localhost:5173";
const googleOrigin = "http://127.0.0.1:9092";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);
/** Every origin this stack may call; anything else would leak the seed. */
const allowedOrigins = new Set([
  webOrigin,
  apiOrigin,
  "http://localhost:8080",
  googleOrigin,
]);

const labels = {
  signUp: /^(?:Đăng ký|Sign Up)$/,
  signUpForm: /^(?:Tạo tài khoản mới|Create an Account)$/,
  fullName: /^(?:Họ và tên|Full Name)$/,
  email: /^(?:Địa chỉ email|Email Address)$/,
  getCode: /^(?:Lấy mã|Get Code)$/,
  otp: /^(?:Mã OTP|OTP code)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  google: /^(?:Đăng nhập bằng Google|Sign in with Google)$/,
  forgotPassword: /^(?:Quên mật khẩu|Forgot password)$/,
  recoverDialog: /^(?:Khôi phục mật khẩu|Recover Password)$/,
  recoverOtp: /^(?:Mã xác thực \(OTP\)|Authentication Code \(OTP\))$/,
  newPassword: /^(?:Mật khẩu mới|New Password)$/,
  resetPassword: /^(?:Đặt lại mật khẩu|Reset Password)$/,
  passwordResetDone:
    /(?:Mật khẩu đã được đặt lại|Your password has been reset)/,
  identityCard: /^(?:Danh tính đăng nhập|Sign-in identities)$/,
  identityLinkGoogle: /^(?:Liên kết Google|Link Google)$/,
  identityLinked:
    /^(?:Đã liên kết danh tính đăng nhập\.|Sign-in identity linked\.)$/,
  totpCard: /^(?:Ứng dụng xác thực|Authenticator app)$/,
  setupTotp: /^(?:Thiết lập ứng dụng xác thực|Set up authenticator app)$/,
  qr: /^(?:Mã QR để thêm TutorHub vào ứng dụng xác thực|QR code that adds TutorHub to your authenticator app)$/,
  confirmCode: /^(?:Mã 6 chữ số từ ứng dụng|6-digit code from the app)$/,
  confirmTotp: /^(?:Xác nhận và bật|Verify and turn on)$/,
  codesDialog: /^(?:Lưu mã khôi phục của bạn|Save your recovery codes)$/,
  codesList: /^(?:Mã khôi phục|Recovery codes)$/,
  codesAck:
    /^(?:Tôi đã lưu các mã khôi phục ở nơi an toàn|I have saved these recovery codes somewhere safe)$/,
  codesDone: /^(?:Hoàn tất|Done)$/,
  remaining: (count: number) =>
    new RegExp(
      `^(?:Còn ${count}/10 mã khôi phục\\.|${count} of 10 recovery codes left\\.)$`,
    ),
  statusOff: /^(?:Chưa bật|Off)$/,
  regenerate: /^(?:Tạo bộ mã khôi phục mới|Generate new recovery codes)$/,
  regenerateDialog:
    /^(?:Tạo bộ mã khôi phục mới\?|Generate new recovery codes\?)$/,
  regenerateConfirm: /^(?:Tạo mã mới|Generate new codes)$/,
  disable: /^(?:Tắt ứng dụng xác thực|Turn off authenticator app)$/,
  disableDialog:
    /^(?:Tắt ứng dụng xác thực\?|Turn off the authenticator app\?)$/,
  disableConfirm: /^(?:Tắt xác minh hai bước|Turn off two-step verification)$/,
  disabledDone:
    /(?:Đã tắt ứng dụng xác thực|The authenticator app was turned off)/,
  mfaStep: /^(?:Xác minh hai bước|Two-step verification)$/,
  mfaRecoveryStep: /^(?:Xác minh đó là bạn|Verify it's you)$/,
  mfaTotpField:
    /^(?:Mã 6 chữ số từ ứng dụng xác thực|6-digit code from your authenticator app)$/,
  mfaRecoveryMethod: /^(?:Mã khôi phục|Recovery code)$/,
  mfaRecoveryField: /^(?:Nhập một mã khôi phục|Enter a recovery code)$/,
  mfaVerify: /^(?:Xác minh|Verify)$/,
  mfaBack: /^(?:Quay lại đăng nhập|Back to sign in)$/,
  totpInvalid:
    /(?:Mã không đúng hoặc đã được dùng|That code is wrong or was already used)/,
  recoveryInvalid:
    /(?:Mã khôi phục không đúng hoặc đã được dùng|That recovery code is wrong or was already used)/,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpMethods: /^(?:Cách xác nhận|Confirmation method)$/,
  stepUpTotp: /^(?:Mã từ ứng dụng xác thực|Authenticator code)$/,
  stepUpConfirm: /^(?:Xác nhận|Confirm)$/,
  stepUpPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
} as const;

/* ---------- RFC 6238 TOTP (SHA-1, 6 digits, 30-second steps) ---------- */

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function base32Decode(secret: string) {
  const clean = secret.replace(/[\s=-]+/g, "").toUpperCase();
  let bits = 0;
  let value = 0;
  const bytes: number[] = [];
  for (const character of clean) {
    const index = BASE32_ALPHABET.indexOf(character);
    if (index < 0) throw new Error("The setup key is not Base32.");
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff);
      bits -= 8;
    }
  }
  return Buffer.from(bytes);
}

function totpAt(secret: string, step: number) {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(step));
  const digest = createHmac("sha1", base32Decode(secret))
    .update(counter)
    .digest();
  const offset = (digest[digest.length - 1] ?? 0) & 0x0f;
  const binary =
    (((digest[offset] ?? 0) & 0x7f) << 24) |
    ((digest[offset + 1] ?? 0) << 16) |
    ((digest[offset + 2] ?? 0) << 8) |
    (digest[offset + 3] ?? 0);
  return String(binary % 1_000_000).padStart(6, "0");
}

function currentStep() {
  return Math.floor(Date.now() / 30_000);
}

/** A code from a step the server has not accepted yet (waits ≤ 30 s). */
async function freshTotp(page: Page, secret: string, lastUsedStep: number) {
  while (currentStep() <= lastUsedStep) {
    const untilNextStep = 30_000 - (Date.now() % 30_000) + 250;
    await page.waitForTimeout(untilNextStep);
  }
  const step = currentStep();
  return { code: totpAt(secret, step), step };
}

/* ---------- Helpers ---------- */

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  purpose: string;
}

interface Account {
  displayName: string;
  email: string;
  password: string;
}

interface Enrollment {
  codes: string[];
  secret: string;
  /** TOTP step the confirmation used (the server's `last_used_step`). */
  step: number;
  /** The code sent with the confirmation. */
  usedCode: string;
}

function newAccount(label: string): Account {
  const suffix = randomBytes(6).toString("hex");
  return {
    displayName: `AUTH09 ${label} ${suffix.slice(0, 4)}`,
    email: `auth09.${label}.${suffix}@example.test`,
    password: `Harbor-${randomBytes(9).toString("base64url")}-Quartz7`,
  };
}

function apiPath(url: string) {
  return new URL(url).pathname;
}

function responseTo(page: Page, method: string, suffix: string | RegExp) {
  return page.waitForResponse(
    (response) =>
      response.request().method() === method &&
      (typeof suffix === "string"
        ? apiPath(response.url()).endsWith(suffix)
        : suffix.test(apiPath(response.url()))),
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
        message: `expected the dev inbox to deliver ${purpose}`,
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
  account: Account,
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
  const challengeId = await readChallengeId(await started);
  const code = await waitForDevInboxCode(request, challengeId, "signup_email");
  await form.getByRole("textbox", { name: labels.otp }).fill(code);
  await form.getByLabel(labels.password).fill(account.password);
  await form.getByRole("checkbox", { name: labels.terms }).check();
  const complete = responseTo(page, "POST", "/v1/auth/signup/email/complete");
  await form.getByRole("button", { name: labels.createAccount }).click();
  expect((await complete).status()).toBe(201);
  await expect(page).toHaveURL(/\/app\/home$/);
  return code;
}

async function signOut(page: Page) {
  await page.goto("/app/settings");
  await page.getByRole("button", { name: labels.signOut }).first().click();
  await expect(page).toHaveURL(/\/signed-out$/);
}

async function logInWithPassword(page: Page, email: string, password: string) {
  await page.goto("/sign-in");
  await page.getByRole("textbox", { name: labels.identifier }).fill(email);
  await page.getByLabel(labels.password).fill(password);
  const login = responseTo(page, "POST", "/v1/auth/password/login");
  await page.getByRole("button", { name: labels.logIn }).click();
  const response = await login;
  expect(response.status()).toBe(200);
  expect(await response.json()).toMatchObject({ status: "mfa_required" });
  const step = page.getByRole("region", { name: labels.mfaStep });
  await expect(step).toBeVisible();
  return step;
}

async function meStatus(page: Page) {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  return response.status();
}

/**
 * Settings → set up the authenticator app → confirm with the current code →
 * acknowledge the ten one-time recovery codes.
 */
async function enableTotp(page: Page): Promise<Enrollment> {
  await page.goto("/app/settings");
  const card = page.getByRole("region", { name: labels.totpCard });
  await expect(card.getByText(labels.statusOff)).toBeVisible();
  const enrolled = responseTo(page, "POST", "/v1/me/mfa/totp/enroll");
  await card.getByRole("button", { name: labels.setupTotp }).click();
  const enrollResponse = await enrolled;
  expect(enrollResponse.status()).toBe(200);
  const { secret } = (await enrollResponse.json()) as { secret: string };
  await expect(card.getByRole("img", { name: labels.qr })).toBeVisible();

  const { code, step } = await freshTotp(page, secret, 0);
  await card.getByRole("textbox", { name: labels.confirmCode }).fill(code);
  const confirmed = responseTo(page, "POST", "/v1/me/mfa/totp/confirm");
  await card.getByRole("button", { name: labels.confirmTotp }).click();
  const confirmResponse = await confirmed;
  expect(confirmResponse.status()).toBe(200);
  const { recovery_codes: codes } = (await confirmResponse.json()) as {
    recovery_codes: string[];
  };
  expect(codes).toHaveLength(10);

  const dialog = page.getByRole("dialog", { name: labels.codesDialog });
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("list", { name: labels.codesList }).getByRole("listitem"),
  ).toHaveCount(10);
  await dialog.getByRole("checkbox", { name: labels.codesAck }).check();
  await dialog.getByRole("button", { name: labels.codesDone }).click();
  await expect(dialog).toBeHidden();
  await expect(card.getByText(labels.remaining(10))).toBeVisible();
  return { codes, secret, step, usedCode: code };
}

async function expectBrowserStorageWithoutSecrets(
  page: Page,
  secrets: readonly string[],
) {
  const stored = await page.evaluate(() =>
    JSON.stringify({
      cookie: document.cookie,
      local: { ...window.localStorage },
      session: { ...window.sessionStorage },
    }),
  );
  expect(stored).not.toMatch(/auth_tx|tutorhub_session|csrf|otpauth/i);
  for (const secret of secrets) {
    if (secret) expect(stored).not.toContain(secret);
  }
}

/* ---------- Scenarios ---------- */

test.describe("AUTH-09 authenticator app two-step verification", () => {
  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "TOTP 2FA runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      mfa_totp_enabled: true,
      password_enabled: true,
      signup_enabled: true,
    });
  });

  test("enrolls with a local QR code, then signs in with replay protection and one-time recovery codes", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newAccount("enroll");
    const secrets: string[] = [account.password];
    const { context, page } = await openDevice(browser);
    const requests: Request[] = [];
    page.on("request", (sent) => requests.push(sent));

    try {
      let enrollment: Enrollment | undefined;
      await test.step("sign up, open Settings without a workspace and enroll", async () => {
        secrets.push(await signUp(page, request, account));
        await page.goto("/app/settings");
        const card = page.getByRole("region", { name: labels.totpCard });
        await expect(card).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          'section[aria-labelledby="security-mfa-title"]',
          "authenticator card (off)",
        );
        requests.length = 0;
        const enrolled = responseTo(page, "POST", "/v1/me/mfa/totp/enroll");
        await card.getByRole("button", { name: labels.setupTotp }).click();
        const body = (await (await enrolled).json()) as {
          otpauth_uri: string;
          secret: string;
        };
        secrets.push(body.secret, body.otpauth_uri);
        const qr = card.getByRole("img", { name: labels.qr });
        await expect(qr).toBeVisible();
        expect(await qr.evaluate((node) => node.tagName.toLowerCase())).toBe(
          "svg",
        );
        expect(await qr.getAttribute("aria-label")).not.toContain(body.secret);
        // The grouped setup key matches the seed.
        const grouped = body.secret.match(/.{1,4}/g)?.join(" ") ?? "";
        await expect(card.getByText(grouped, { exact: true })).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          'section[aria-labelledby="security-mfa-title"]',
          "authenticator enrollment",
        );
        // Drawing the QR code made no request outside this stack.
        const foreign = requests
          .map((sent) => new URL(sent.url()).origin)
          .filter((origin) => !allowedOrigins.has(origin));
        expect(foreign).toEqual([]);
        expect(requests.some((sent) => sent.url().includes(body.secret))).toBe(
          false,
        );

        const { code, step } = await freshTotp(page, body.secret, 0);
        await card
          .getByRole("textbox", { name: labels.confirmCode })
          .fill(code);
        const confirmed = responseTo(page, "POST", "/v1/me/mfa/totp/confirm");
        await card.getByRole("button", { name: labels.confirmTotp }).click();
        const confirmResponse = await confirmed;
        expect(confirmResponse.status()).toBe(200);
        const { recovery_codes: codes } = (await confirmResponse.json()) as {
          recovery_codes: string[];
        };
        expect(codes).toHaveLength(10);
        secrets.push(...codes);

        const dialog = page.getByRole("dialog", { name: labels.codesDialog });
        await expect(dialog).toBeVisible();
        const items = dialog
          .getByRole("list", { name: labels.codesList })
          .getByRole("listitem");
        await expect(items).toHaveCount(10);
        await expect(items.first()).toHaveText(codes[0] ?? "");
        await expectNoBlockingAxeViolations(
          page,
          '[role="dialog"]',
          "recovery codes dialog",
        );
        const done = dialog.getByRole("button", { name: labels.codesDone });
        await expect(done).toBeDisabled();
        await dialog.getByRole("checkbox", { name: labels.codesAck }).check();
        await done.click();
        await expect(dialog).toBeHidden();
        await expect(card.getByText(labels.remaining(10))).toBeVisible();
        // The codes left memory with the dialog.
        await expect(page.getByText(codes[0] ?? "")).toHaveCount(0);
        enrollment = { codes, secret: body.secret, step, usedCode: code };
        await expectBrowserStorageWithoutSecrets(page, secrets);
      });
      if (!enrollment) throw new Error("Enrollment did not finish.");
      const { codes, usedCode } = enrollment;

      await test.step("sign in again: the confirmation code is refused as a replay, a recovery code works", async () => {
        await signOut(page);
        const step = await logInWithPassword(
          page,
          account.email,
          account.password,
        );
        await expect(
          step.getByRole("heading", { name: labels.mfaStep }),
        ).toBeFocused();
        await expectNoBlockingAxeViolations(page, "main", "two-step sign-in");

        await step
          .getByRole("textbox", { name: labels.mfaTotpField })
          .fill(usedCode);
        const replay = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        const replayed = await replay;
        expect(replayed.status()).toBe(401);
        expect(await replayed.json()).toMatchObject({
          code: "auth_invalid_verification_code",
        });
        await expect(step.getByText(labels.totpInvalid)).toBeVisible();
        expect(await meStatus(page)).toBe(401);

        await step
          .getByRole("button", { name: labels.mfaRecoveryMethod })
          .click();
        await step
          .getByRole("textbox", { name: labels.mfaRecoveryField })
          .fill((codes[0] ?? "").toLowerCase());
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        expect((await verified).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await meStatus(page)).toBe(200);

        await page.goto("/app/settings");
        await expect(
          page
            .getByRole("region", { name: labels.totpCard })
            .getByText(labels.remaining(9)),
        ).toBeVisible();
      });

      // Attempts so far on this account: confirm, replay, recovery code. The
      // server allows 5 per 300 s, so this scenario stops at 4.
      await test.step("a used recovery code never works twice", async () => {
        await signOut(page);
        const step = await logInWithPassword(
          page,
          account.email,
          account.password,
        );
        await step
          .getByRole("button", { name: labels.mfaRecoveryMethod })
          .click();
        await step
          .getByRole("textbox", { name: labels.mfaRecoveryField })
          .fill(codes[0] ?? "");
        const reused = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        expect((await reused).status()).toBe(401);
        await expect(step.getByText(labels.recoveryInvalid)).toBeVisible();
        expect(await meStatus(page)).toBe(401);

        // Leaving the second step returns to the first factor.
        await step.getByRole("button", { name: labels.mfaBack }).click();
        await expect(
          page.getByRole("textbox", { name: labels.identifier }),
        ).toBeFocused();
        await expectBrowserStorageWithoutSecrets(page, secrets);
      });
    } finally {
      await context.close();
    }
  });

  test("finishes a Google sign-in on /sign-in?mfa=pending and keeps the return path", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newAccount("google");
    const googleEmail = `auth09.google.${randomBytes(6).toString("hex")}@example.test`;
    const { context, page } = await openDevice(browser);
    try {
      await signUp(page, request, account);
      const enrollment = await enableTotp(page);

      await test.step("link Google from Settings with the AAL2 session", async () => {
        const identityCard = page.getByRole("region", {
          name: labels.identityCard,
        });
        await identityCard
          .getByRole("button", { name: labels.identityLinkGoogle })
          .click();
        await page.waitForURL(
          (url) => url.origin === googleOrigin && url.pathname === "/authorize",
        );
        await page
          .getByRole("textbox", { name: "Test email" })
          .fill(googleEmail);
        await page
          .getByRole("button", { name: "Sign in with custom account" })
          .click();
        await expect(
          identityCard.getByText(labels.identityLinked),
        ).toBeVisible();
      });

      await test.step("Google sign-in stops at the second step and returns to the calendar", async () => {
        await signOut(page);
        await page.goto("/app/calendar");
        await expect(page).toHaveURL(/\/sign-in$/);
        await page.getByRole("button", { name: labels.google }).click();
        await page.waitForURL(
          (url) => url.origin === googleOrigin && url.pathname === "/authorize",
        );
        await page
          .getByRole("textbox", { name: "Test email" })
          .fill(googleEmail);
        const pending = responseTo(page, "GET", "/v1/auth/mfa");
        await page
          .getByRole("button", { name: "Sign in with custom account" })
          .click();
        const state = await pending;
        expect(state.status()).toBe(200);
        expect(await state.json()).toMatchObject({
          methods: expect.arrayContaining(["totp", "recovery_code"]),
        });
        // The transient flag left the address bar.
        await expect(page).toHaveURL(`${webOrigin}/sign-in`);
        expect(await meStatus(page)).toBe(401);

        const step = page.getByRole("region", { name: labels.mfaStep });
        const { code } = await freshTotp(
          page,
          enrollment.secret,
          enrollment.step,
        );
        await step
          .getByRole("textbox", { name: labels.mfaTotpField })
          .fill(code);
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        const response = await verified;
        expect(response.status()).toBe(200);
        expect(await response.json()).toMatchObject({
          return_to: "/app/calendar",
          status: "authenticated",
        });
        await expect(page).toHaveURL(`${webOrigin}/app/calendar`);
        expect(await meStatus(page)).toBe(200);
      });
    } finally {
      await context.close();
    }
  });

  test("password recovery waits for the second step and never signs in", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newAccount("recovery");
    const recoveredPassword = `Recovered-${randomBytes(9).toString("base64url")}-Pine3`;
    const { context, page } = await openDevice(browser);
    try {
      await signUp(page, request, account);
      const enrollment = await enableTotp(page);
      await signOut(page);

      await test.step("the emailed code leads to the second step, then password_reset", async () => {
        await page.goto("/sign-in");
        await page.getByRole("button", { name: labels.forgotPassword }).click();
        const dialog = page.getByRole("dialog", { name: labels.recoverDialog });
        await dialog
          .getByRole("textbox", { name: labels.email })
          .fill(account.email);
        const started = responseTo(
          page,
          "POST",
          "/v1/auth/password/recovery/start",
        );
        await dialog.getByRole("button", { name: labels.getCode }).click();
        const challengeId = await readChallengeId(await started);
        const code = await waitForDevInboxCode(
          request,
          challengeId,
          "password_recovery",
        );
        await dialog
          .getByRole("textbox", { name: labels.recoverOtp })
          .fill(code);
        await dialog.getByLabel(labels.newPassword).fill(recoveredPassword);
        const complete = responseTo(
          page,
          "POST",
          "/v1/auth/password/recovery/complete",
        );
        await dialog
          .getByRole("button", { name: labels.resetPassword })
          .click();
        const completed = await complete;
        expect(completed.status()).toBe(200);
        expect(await completed.json()).toMatchObject({
          status: "mfa_required",
        });

        const mfaDialog = page.getByRole("dialog", {
          name: labels.mfaRecoveryStep,
        });
        await expect(mfaDialog).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          '[role="dialog"]',
          "recovery second step",
        );
        await mfaDialog
          .getByRole("button", { name: labels.mfaRecoveryMethod })
          .click();
        await mfaDialog
          .getByRole("textbox", { name: labels.mfaRecoveryField })
          .fill(enrollment.codes[1] ?? "");
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await mfaDialog.getByRole("button", { name: labels.mfaVerify }).click();
        const response = await verified;
        expect(response.status()).toBe(200);
        expect(await response.json()).toEqual({ status: "password_reset" });
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(page.getByText(labels.passwordResetDone)).toBeVisible();
        await expect(page).toHaveURL(/\/sign-in$/);
        expect(await meStatus(page)).toBe(401);
      });

      await test.step("the new password still needs the second step", async () => {
        const step = await logInWithPassword(
          page,
          account.email,
          recoveredPassword,
        );
        const { code } = await freshTotp(
          page,
          enrollment.secret,
          enrollment.step,
        );
        await step
          .getByRole("textbox", { name: labels.mfaTotpField })
          .fill(code);
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        expect((await verified).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        await expectBrowserStorageWithoutSecrets(page, [
          recoveredPassword,
          enrollment.secret,
          ...enrollment.codes,
        ]);
      });
    } finally {
      await context.close();
    }
  });

  test("regenerates recovery codes and turns the app off after a TOTP step-up", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newAccount("manage");
    const { context, page } = await openDevice(browser);
    try {
      await signUp(page, request, account);
      const enrollment = await enableTotp(page);
      const card = page.getByRole("region", { name: labels.totpCard });

      await test.step("regenerating replaces every recovery code", async () => {
        await card.getByRole("button", { name: labels.regenerate }).click();
        const confirm = page.getByRole("dialog", {
          name: labels.regenerateDialog,
        });
        const regenerated = responseTo(
          page,
          "POST",
          "/v1/me/mfa/recovery-codes",
        );
        await confirm
          .getByRole("button", { name: labels.regenerateConfirm })
          .click();
        const response = await regenerated;
        expect(response.status()).toBe(200);
        const { recovery_codes: fresh } = (await response.json()) as {
          recovery_codes: string[];
        };
        expect(fresh).toHaveLength(10);
        expect(fresh).not.toContain(enrollment.codes[0]);
        const dialog = page.getByRole("dialog", { name: labels.codesDialog });
        await dialog.getByRole("checkbox", { name: labels.codesAck }).check();
        await dialog.getByRole("button", { name: labels.codesDone }).click();
        await expect(card.getByText(labels.remaining(10))).toBeVisible();
      });

      await test.step("a stale session steps up with the authenticator app, then turns it off", async () => {
        // The session is younger than the 10-minute window: inject one 403.
        let injected = false;
        await page.route("**/api/v1/me/mfa/totp", async (route) => {
          if (injected || route.request().method() !== "DELETE") {
            await route.fallback();
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
        });
        await card.getByRole("button", { name: labels.disable }).click();
        const confirm = page.getByRole("dialog", {
          name: labels.disableDialog,
        });
        await confirm
          .getByRole("button", { name: labels.disableConfirm })
          .click();

        const stepUp = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(stepUp).toBeVisible();
        expect(injected).toBe(true);
        // An AAL2 session with TOTP re-authenticates only with the app.
        await expect(
          stepUp.getByRole("group", { name: labels.stepUpMethods }),
        ).toHaveCount(0);
        await expect(stepUp.getByLabel(labels.stepUpPassword)).toHaveCount(0);
        const field = stepUp.getByRole("textbox", { name: labels.stepUpTotp });
        await expect(field).toBeFocused();
        await expectNoBlockingAxeViolations(
          page,
          '[role="dialog"]',
          "TOTP step-up",
        );

        const { code } = await freshTotp(
          page,
          enrollment.secret,
          enrollment.step,
        );
        await field.fill(code);
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const disabled = responseTo(page, "DELETE", "/v1/me/mfa/totp");
        await stepUp
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        const reauthResponse = await reauth;
        expect(reauthResponse.status()).toBe(200);
        expect(reauthResponse.request().postDataJSON()).toEqual({
          code,
          method: "totp",
        });
        expect((await disabled).status()).toBe(204);
        await expect(stepUp).toBeHidden();
        await expect(card.getByText(labels.disabledDone)).toBeVisible();
        await expect(
          card.getByRole("button", { name: labels.setupTotp }),
        ).toBeVisible();
        await page.unroute("**/api/v1/me/mfa/totp");
      });

      await test.step("sign-in no longer asks for a second step", async () => {
        await signOut(page);
        await page.goto("/sign-in");
        await page
          .getByRole("textbox", { name: labels.identifier })
          .fill(account.email);
        await page.getByLabel(labels.password).fill(account.password);
        const login = responseTo(page, "POST", "/v1/auth/password/login");
        await page.getByRole("button", { name: labels.logIn }).click();
        expect(await (await login).json()).toEqual({ status: "authenticated" });
        await expect(page).toHaveURL(/\/app\/home$/);
      });
    } finally {
      await context.close();
    }
  });
});
