import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
  type Response,
} from "@playwright/test";
import { randomBytes } from "node:crypto";

/**
 * AUTH-03B: email-code login, password recovery and set/change password on
 * the loopback stack. Requires `node scripts/e2e-local.mjs serve --native-auth`
 * (started by playwright.auth.config.ts), which enables sign-up, password and
 * email-code login, recovery and the loopback-only `devinbox` email sender.
 * Under the default `pnpm e2e` config the native methods are off, so the spec
 * skips itself there instead of failing.
 */

const apiOrigin = "http://127.0.0.1:8080";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);

const labels = {
  signUp: /^(?:Đăng ký|Sign Up)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  signUpForm: /^(?:Tạo tài khoản mới|Create an Account)$/,
  fullName: /^(?:Họ và tên|Full Name)$/,
  email: /^(?:Địa chỉ email|Email Address)$/,
  getCode: /^(?:Lấy mã|Get Code)$/,
  cooldown: /^\d+s$/,
  otp: /^(?:Mã OTP|OTP code)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  // UI-AUTH-07: three-step sign-up (email + terms -> code -> name + password).
  signUpGetCode: /^(?:Lấy mã|Get code)$/,
  signUpOtp: /^(?:Vui lòng nhập mã xác minh|Please enter verification code)$/,
  signUpCodeSent:
    /(?:Mã xác minh 6 chữ số đã được gửi đến|A 6-digit verification code has been sent to)/,
  resendIn: /^(?:Gửi lại sau \d+s|Resend in \d+s)$/,
  continue: /^(?:Tiếp tục|Continue)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  codeSent:
    /(?:mã xác thực 6 chữ số đã được gửi|a 6-digit verification code has been sent)/,
  useOtp: /^(?:Đăng nhập với OTP|Log in with OTP)$/,
  otpChannels: /^(?:Nhận mã OTP qua|Receive the OTP by)$/,
  emailChannel: /^Email$/,
  emailCodeForm:
    /^(?:Đăng nhập bằng mã gửi tới email|Log in with a code sent to your email)$/,
  forgotPassword: /^(?:Quên mật khẩu|Forgot password)$/,
  recoverDialog: /^(?:Khôi phục mật khẩu|Recover Password)$/,
  recoverOtp: /^(?:Mã xác thực \(OTP\)|Authentication Code \(OTP\))$/,
  newPassword: /^(?:Mật khẩu mới|New Password)$/,
  resetPassword: /^(?:Đặt lại mật khẩu|Reset Password)$/,
  resetDone:
    /(?:Mật khẩu đã được đặt lại và mọi phiên đăng nhập|Your password has been reset and all your sessions)/,
  loginInvalid:
    /^(?:Email hoặc mật khẩu không đúng\.|Incorrect email or password\.)$/,
  passwordCard: /^(?:Mật khẩu|Password)$/,
  currentPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
  settingsNewPassword: /^(?:Mật khẩu mới|New password)$/,
  confirmPassword: /^(?:Nhập lại mật khẩu mới|Confirm new password)$/,
  changePassword: /^(?:Đổi mật khẩu|Change password)$/,
  invalidCurrent:
    /^(?:Mật khẩu hiện tại không đúng\.|The current password is incorrect\.)$/,
  passwordChanged:
    /^(?:Đã đổi mật khẩu\. Các phiên đăng nhập khác đã bị đăng xuất\.|Password changed\. Your other sessions have been signed out\.)$/,
  saveProfile: /^(?:Lưu thay đổi|Save changes)$/,
  profileSaved: /^(?:Đã cập nhật hồ sơ\.|Profile updated\.)$/,
} as const;

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  destination?: string;
  kind?: string;
  notice_kind?: string;
  purpose: string;
}

interface ChallengeBody {
  challenge_id?: unknown;
  cooldown_seconds?: unknown;
  expires_in_seconds?: unknown;
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

async function listDevInbox(request: APIRequestContext) {
  const response = await request.get(`${apiOrigin}/api/v1/dev/auth-inbox`, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok()) {
    throw new Error(`The dev inbox answered HTTP ${response.status()}.`);
  }
  const body = (await response.json()) as { items?: DevInboxMessage[] };
  return body.items ?? [];
}

async function waitForDevInbox(
  request: APIRequestContext,
  description: string,
  match: (message: DevInboxMessage) => boolean,
): Promise<DevInboxMessage> {
  let found: DevInboxMessage | undefined;
  await expect
    .poll(
      async () => {
        found = (await listDevInbox(request)).find(match);
        return found ? "delivered" : "pending";
      },
      {
        message: `expected the dev inbox to receive ${description}`,
        timeout: 15_000,
      },
    )
    .toBe("delivered");
  if (!found) {
    throw new Error(`The dev inbox message (${description}) disappeared.`);
  }
  return found;
}

async function readChallenge(response: Response) {
  // Uniform anti-enumeration contract: always 202 with the same body shape.
  expect(response.status()).toBe(202);
  const body = (await response.json()) as ChallengeBody;
  expect(Object.keys(body).sort()).toEqual([
    "challenge_id",
    "cooldown_seconds",
    "expires_in_seconds",
  ]);
  expect(body.cooldown_seconds).toBe(60);
  if (
    typeof body.challenge_id !== "string" ||
    typeof body.expires_in_seconds !== "number"
  ) {
    throw new Error("The challenge response is malformed.");
  }
  return {
    challengeId: body.challenge_id,
    expiresInSeconds: body.expires_in_seconds,
  };
}

/**
 * Requests a code inside `scope` and returns what the user can observe, so a
 * registered and an unknown address can be compared for equality.
 */
async function requestCode(
  page: Page,
  scope: Locator,
  endpoint: string,
  email: string,
) {
  await scope.getByRole("textbox", { name: labels.email }).fill(email);
  const started = postTo(page, endpoint);
  await scope.getByRole("button", { name: labels.getCode }).click();
  const challenge = await readChallenge(await started);
  const notice = scope.getByText(labels.codeSent);
  await expect(notice).toBeVisible();
  const cooldown = scope.getByRole("button", { name: labels.cooldown });
  await expect(cooldown).toBeDisabled();
  return {
    ...challenge,
    observed: {
      cooldownDisabled: await cooldown.isDisabled(),
      expiresInSeconds: challenge.expiresInSeconds,
      notice: ((await notice.textContent()) ?? "").trim(),
    },
  };
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

async function expectSessionCookiesHttpOnly(page: Page) {
  const sessionCookies = (await page.context().cookies()).filter((cookie) =>
    /session/i.test(cookie.name),
  );
  expect(sessionCookies.length).toBeGreaterThan(0);
  for (const cookie of sessionCookies) {
    expect(cookie.httpOnly).toBe(true);
  }
}

async function expectSignedInAs(page: Page, email: string) {
  await expect(page).toHaveURL(/\/app\/home$/);
  const me = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  expect(me.status()).toBe(200);
  const body = (await me.json()) as { user?: { email?: string } };
  expect(body.user?.email).toBe(email);
}

async function signOut(page: Page) {
  await page.getByRole("button", { name: labels.signOut }).click();
  await expect(page).toHaveURL(/\/signed-out$/);
}

async function logInWithPassword(page: Page, email: string, password: string) {
  await page.goto("/sign-in");
  await page.getByRole("textbox", { name: labels.identifier }).fill(email);
  await page.getByLabel(labels.password).fill(password);
  const login = postTo(page, "/v1/auth/password/login");
  await page.getByRole("button", { name: labels.logIn }).click();
  return login;
}

async function fillPasswordCard(
  card: Locator,
  values: { confirm: string; current: string; next: string },
) {
  await card.getByLabel(labels.currentPassword).fill(values.current);
  await card.getByLabel(labels.settingsNewPassword).fill(values.next);
  await card.getByLabel(labels.confirmPassword).fill(values.confirm);
}

test.describe("AUTH-03 native email-code login, recovery and password settings", () => {
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
      email_code_enabled: true,
      password_enabled: true,
      password_recovery_enabled: true,
      signup_enabled: true,
    });
  });

  test("logs in with an emailed code, recovers the password without signing in and changes it in settings", async ({
    page,
    request,
  }) => {
    const suffix = randomBytes(6).toString("hex");
    const email = `auth03.${suffix}@example.test`;
    const unknownEmail = `auth03.unknown.${suffix}@example.test`;
    const displayName = `AUTH03 Learner ${suffix.slice(0, 4)}`;
    const signupPassword = `Lantern-${randomBytes(9).toString("base64url")}-Harbor7`;
    const recoveredPassword = `Compass-${randomBytes(9).toString("base64url")}-Meadow4`;
    const changedPassword = `Orchard-${randomBytes(9).toString("base64url")}-Summit2`;
    const codes: string[] = [];

    await test.step("sign up with an emailed code", async () => {
      await page.goto("/sign-in");
      await page.getByRole("button", { name: labels.signUp }).click();
      const form = page.getByRole("form", { name: labels.signUpForm });
      // Step 1: email + consent -> "Get code".
      await form.getByRole("textbox", { name: labels.email }).fill(email);
      await form.getByRole("checkbox", { name: labels.terms }).check();
      const started = postTo(page, "/v1/auth/signup/email/start");
      await form.getByRole("button", { name: labels.signUpGetCode }).click();
      const { challengeId } = await readChallenge(await started);
      // Step 2: neutral notice; Resend counts down inside the code field.
      await expect(form.getByText(labels.signUpCodeSent)).toBeVisible();
      await expect(
        form.getByRole("button", { name: labels.resendIn }),
      ).toBeDisabled();
      const delivered = await waitForDevInbox(
        request,
        "the sign-up code",
        (message) => message.challenge_id === challengeId,
      );
      const code = delivered.code ?? "";
      expect(code).toMatch(/^\d{6}$/);
      codes.push(code);
      await form.getByRole("textbox", { name: labels.signUpOtp }).fill(code);
      await form.getByRole("button", { name: labels.continue }).click();
      // Step 3: name + password -> "Create Account".
      await form
        .getByRole("textbox", { name: labels.fullName })
        .fill(displayName);
      await form.getByLabel(labels.password).fill(signupPassword);
      const complete = postTo(page, "/v1/auth/signup/email/complete");
      await form.getByRole("button", { name: labels.createAccount }).click();
      expect((await complete).status()).toBe(201);
      await expectSignedInAs(page, email);
      await signOut(page);
    });

    await test.step("log in with an email code; an unknown address looks identical", async () => {
      await page.goto("/sign-in");
      await page.getByRole("button", { name: labels.useOtp }).click();
      // Email and phone codes are both on: the email channel opens first.
      await expect(
        page
          .getByRole("group", { name: labels.otpChannels })
          .getByRole("button", { name: labels.emailChannel }),
      ).toHaveAttribute("aria-pressed", "true");
      const form = page.getByRole("form", { name: labels.emailCodeForm });
      await expect(
        form.getByRole("textbox", { name: labels.email }),
      ).toBeFocused();

      const unknown = await requestCode(
        page,
        form,
        "/v1/auth/email-code/start",
        unknownEmail,
      );
      // Editing the address drops the pending code and re-enables Get Code.
      const known = await requestCode(
        page,
        form,
        "/v1/auth/email-code/start",
        email,
      );
      expect(known.observed).toEqual(unknown.observed);
      await expectNoBlockingAxeViolations(
        page,
        ".sign-in__panel",
        "email-code login",
      );

      const delivered = await waitForDevInbox(
        request,
        "the login code",
        (message) => message.challenge_id === known.challengeId,
      );
      expect(delivered.purpose).toBe("login_code");
      const code = delivered.code ?? "";
      expect(code).toMatch(/^\d{6}$/);
      codes.push(code);
      // An address without an account never receives anything.
      expect(
        (await listDevInbox(request)).some(
          (message) => message.challenge_id === unknown.challengeId,
        ),
      ).toBe(false);

      await form.getByRole("textbox", { name: labels.otp }).fill(code);
      // UI-AUTH-04: no checkbox; the code sign-in is always persistent.
      await expect(form.getByRole("checkbox")).toHaveCount(0);
      const verify = postTo(page, "/v1/auth/email-code/verify");
      await form.getByRole("button", { name: labels.logIn }).click();
      const verified = await verify;
      expect(verified.status()).toBe(200);
      expect(verified.request().postDataJSON()).toMatchObject({
        keep_signed_in: true,
      });
      expect(await verified.json()).toEqual({ status: "authenticated" });
      await expectSignedInAs(page, email);
      await expectSessionCookiesHttpOnly(page);
      await expectBrowserStorageWithoutSecrets(page, [
        signupPassword,
        ...codes,
      ]);
      await signOut(page);
    });

    await test.step("recover the password: no automatic sign-in, notice without a code", async () => {
      await page.goto("/sign-in");
      await page.getByRole("button", { name: labels.forgotPassword }).click();
      const dialog = page.getByRole("dialog", { name: labels.recoverDialog });
      await expect(dialog).toBeVisible();

      const unknown = await requestCode(
        page,
        dialog,
        "/v1/auth/password/recovery/start",
        unknownEmail,
      );
      const known = await requestCode(
        page,
        dialog,
        "/v1/auth/password/recovery/start",
        email,
      );
      expect(known.observed).toEqual(unknown.observed);

      const delivered = await waitForDevInbox(
        request,
        "the recovery code",
        (message) => message.challenge_id === known.challengeId,
      );
      expect(delivered.purpose).toBe("password_recovery");
      const code = delivered.code ?? "";
      expect(code).toMatch(/^\d{6}$/);
      codes.push(code);
      expect(
        (await listDevInbox(request)).some(
          (message) => message.challenge_id === unknown.challengeId,
        ),
      ).toBe(false);

      await dialog.getByRole("textbox", { name: labels.recoverOtp }).fill(code);
      await dialog.getByLabel(labels.newPassword).fill(recoveredPassword);
      await expectNoBlockingAxeViolations(
        page,
        '[role="dialog"]',
        "recovery dialog",
      );
      const complete = postTo(page, "/v1/auth/password/recovery/complete");
      await dialog.getByRole("button", { name: labels.resetPassword }).click();
      const completed = await complete;
      expect(completed.status()).toBe(200);
      expect(await completed.json()).toEqual({ status: "password_reset" });

      await expect(dialog).toBeHidden();
      await expect(page.getByText(labels.resetDone)).toBeVisible();
      await expect(page).toHaveURL(/\/sign-in$/);
      await expect(
        page.getByRole("textbox", { name: labels.identifier }),
      ).toBeFocused();
      // The reset never signs anyone in.
      const me = await page.request.get("/api/v1/me", {
        headers: { Accept: "application/json" },
      });
      expect(me.status()).toBe(401);

      const notice = await waitForDevInbox(
        request,
        "the password_changed notice",
        (message) =>
          message.notice_kind === "password_changed" &&
          message.destination === email,
      );
      expect(notice.kind).toBe("notice");
      expect(notice.code).toBeUndefined();
      await expectBrowserStorageWithoutSecrets(page, [
        signupPassword,
        recoveredPassword,
        ...codes,
      ]);
    });

    await test.step("the old password is rejected and the new one signs in", async () => {
      const rejected = await logInWithPassword(page, email, signupPassword);
      expect((await rejected).status()).toBe(401);
      await expect(page.getByRole("alert")).toHaveText(labels.loginInvalid);

      const accepted = await logInWithPassword(page, email, recoveredPassword);
      expect((await accepted).status()).toBe(200);
      await expectSignedInAs(page, email);
    });

    await test.step("change the password in settings and keep the session", async () => {
      // A brand-new account has no workspace; /app routes render onboarding
      // until one exists, so create it through the real UI first.
      const onboarding = page.getByRole("heading", {
        name: /^(?:Tạo workspace đầu tiên|Create your first workspace)$/,
      });
      await expect(onboarding).toBeVisible();
      await page
        .getByLabel(
          /^(?:Tên tổ chức hoặc nhóm học|Organization or learning group name)$/,
        )
        .fill(`AUTH03 Workspace ${suffix}`);
      await page
        .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
        .fill(`auth03-${suffix}`);
      const tenantCreated = postTo(page, "/v1/tenants");
      await page
        .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
        .click();
      expect((await tenantCreated).status()).toBe(201);
      await expect(onboarding).toBeHidden();

      await page.goto("/app/settings");
      const card = page.getByRole("region", { name: labels.passwordCard });
      await expect(card.getByLabel(labels.currentPassword)).toBeVisible();
      await expectNoBlockingAxeViolations(
        page,
        'section[aria-labelledby="profile-password-title"]',
        "password card",
      );

      await fillPasswordCard(card, {
        confirm: changedPassword,
        current: `Wrong-${randomBytes(6).toString("hex")}`,
        next: changedPassword,
      });
      const wrong = postTo(page, "/v1/me/password");
      await card.getByRole("button", { name: labels.changePassword }).click();
      expect((await wrong).status()).toBe(401);
      await expect(card.getByText(labels.invalidCurrent)).toBeVisible();
      await expect(card.getByLabel(labels.currentPassword)).toHaveValue("");

      await fillPasswordCard(card, {
        confirm: changedPassword,
        current: recoveredPassword,
        next: changedPassword,
      });
      const changed = postTo(page, "/v1/me/password");
      await card.getByRole("button", { name: labels.changePassword }).click();
      expect((await changed).status()).toBe(200);
      await expect(card.getByText(labels.passwordChanged)).toBeVisible();
      await expect(page).toHaveURL(/\/app\/settings$/);
      await expect(card.getByLabel(labels.settingsNewPassword)).toHaveValue("");

      // The rotated session and CSRF cookie still authorize the next mutation.
      const me = await page.request.get("/api/v1/me", {
        headers: { Accept: "application/json" },
      });
      expect(me.status()).toBe(200);
      const saved = page.waitForResponse(
        (response) =>
          response.request().method() === "PATCH" &&
          apiPath(response.url()).endsWith("/v1/me/profile"),
      );
      await page.getByRole("button", { name: labels.saveProfile }).click();
      expect((await saved).status()).toBe(200);
      await expect(page.getByText(labels.profileSaved)).toBeVisible();
      await expectSessionCookiesHttpOnly(page);
      await expectBrowserStorageWithoutSecrets(page, [
        signupPassword,
        recoveredPassword,
        changedPassword,
        ...codes,
      ]);
      await signOut(page);
    });

    await test.step("only the changed password signs in afterwards", async () => {
      const previous = await logInWithPassword(page, email, recoveredPassword);
      expect((await previous).status()).toBe(401);
      const current = await logInWithPassword(page, email, changedPassword);
      expect((await current).status()).toBe(200);
      await expectSignedInAs(page, email);
    });
  });
});
