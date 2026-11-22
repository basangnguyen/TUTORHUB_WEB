import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
} from "@playwright/test";
import { randomBytes } from "node:crypto";

/**
 * AUTH-02B: native email sign-up (OTP + terms) and password login on the
 * loopback stack. Requires `node scripts/e2e-local.mjs serve --native-auth`
 * (started by playwright.auth.config.ts), which enables sign-up, password
 * login and the loopback-only `devinbox` email sender.
 */

const apiOrigin = "http://127.0.0.1:8080";
const blockingImpacts = new Set(["serious", "critical"]);

const labels = {
  signUp: /^(?:Đăng ký|Sign Up)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  signUpForm: /^(?:Tạo tài khoản mới|Create an Account)$/,
  fullName: /^(?:Họ và tên|Full Name)$/,
  email: /^(?:Địa chỉ email|Email Address)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  // UI-AUTH-07: three-step sign-up (email + terms -> code -> name + password).
  emailStep: /^Email$/,
  accountStep: /^(?:Thiết lập tài khoản|Set up your account)$/,
  signUpGetCode: /^(?:Lấy mã|Get code)$/,
  signUpOtp: /^(?:Vui lòng nhập mã xác minh|Please enter verification code)$/,
  resendIn: /^(?:Gửi lại sau \d+s|Resend in \d+s)$/,
  continue: /^(?:Tiếp tục|Continue)$/,
  backToEmail: /^(?:Quay lại nhập email|Back to email)$/,
  otherSignInMethods:
    /(?:Google|Microsoft|Facebook|[Pp]asskey|SSO trường học|school SSO)/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  holdToShowPassword:
    /^(?:Nhấn giữ để hiện mật khẩu|Press and hold to show password)$/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  codeSent:
    /(?:Mã xác minh 6 chữ số đã được gửi đến|A 6-digit verification code has been sent to)/,
} as const;

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  kind?: string;
  notice_kind?: string;
  purpose: string;
}

function apiPath(url: string) {
  return new URL(url).pathname;
}

async function expectNoBlockingAxeViolations(page: Page, label: string) {
  const results = await new AxeBuilder({ page })
    .include(".sign-in__panel")
    .analyze();
  const blocking = results.violations
    .filter((violation) => blockingImpacts.has(violation.impact ?? ""))
    .map(
      (violation) =>
        `${label}: ${violation.id} (${violation.impact}) on ${violation.nodes.length} node(s)`,
    );
  expect(blocking).toEqual([]);
}

async function openSignUpForm(page: Page) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: labels.signUp }).click();
  return page.getByRole("form", { name: labels.signUpForm });
}

/** Step 1 -> step 2: "Get code" with the email and consent already filled. */
async function requestSignupCode(page: Page, form: Locator) {
  const startResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      apiPath(response.url()).endsWith("/v1/auth/signup/email/start"),
  );
  await form.getByRole("button", { name: labels.signUpGetCode }).click();
  const response = await startResponse;
  // Uniform anti-enumeration contract: always 202 with the same body shape.
  expect(response.status()).toBe(202);
  const body = (await response.json()) as {
    challenge_id?: unknown;
    cooldown_seconds?: unknown;
    expires_in_seconds?: unknown;
  };
  expect(body.cooldown_seconds).toBe(60);
  expect(typeof body.expires_in_seconds).toBe("number");
  if (typeof body.challenge_id !== "string") {
    throw new Error("The sign-up start response has no challenge_id.");
  }
  // Step 2: neutral notice, Resend counts down inside the code field.
  await expect(form.getByText(labels.codeSent)).toBeVisible();
  await expect(
    form.getByRole("textbox", { name: labels.signUpOtp }),
  ).toBeFocused();
  await expect(
    form.getByRole("button", { name: labels.resendIn }),
  ).toBeDisabled();
  return body.challenge_id;
}

async function readDevInbox(
  request: APIRequestContext,
  challengeId: string,
): Promise<DevInboxMessage> {
  let message: DevInboxMessage | undefined;
  await expect
    .poll(
      async () => {
        const response = await request.get(
          `${apiOrigin}/api/v1/dev/auth-inbox`,
          { headers: { Accept: "application/json" } },
        );
        if (!response.ok()) {
          return `HTTP ${response.status()}`;
        }
        const body = (await response.json()) as { items?: DevInboxMessage[] };
        message = body.items?.find((item) => item.challenge_id === challengeId);
        return message ? "delivered" : "pending";
      },
      {
        message: "expected the loopback dev inbox to receive the message",
        timeout: 15_000,
      },
    )
    .toBe("delivered");
  if (!message) {
    throw new Error("The dev inbox message disappeared.");
  }
  return message;
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

async function expectSignedInAs(
  page: Page,
  email: string,
  displayName: string,
) {
  await expect(page).toHaveURL(/\/app\/home$/);
  const me = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  expect(me.status()).toBe(200);
  const body = (await me.json()) as {
    user?: { display_name?: string; email?: string };
  };
  expect(body.user?.email).toBe(email);
  expect(body.user?.display_name).toBe(displayName);
  await expect(
    page.getByText(displayName, { exact: true }).first(),
  ).toBeVisible();
}

test.describe("AUTH-02 native email sign-up and password login", () => {
  test("signs up with an emailed code, signs out and logs back in with the password", async ({
    context,
    page,
    request,
  }) => {
    const suffix = randomBytes(6).toString("hex");
    const email = `auth02.${suffix}@example.test`;
    const displayName = `AUTH02 Learner ${suffix.slice(0, 4)}`;
    const password = `Lantern-${randomBytes(9).toString("base64url")}-Harbor7`;

    await page.goto("/sign-in");
    await expect(
      page.getByRole("button", { name: labels.logIn }),
    ).toBeEnabled();
    // UI-AUTH-04 (option A): no "Keep me signed in" / "Remember Me".
    await expect(
      page.locator(".sign-in__panel").getByRole("checkbox"),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: labels.holdToShowPassword }),
    ).toHaveAttribute("aria-pressed", "false");
    await expectNoBlockingAxeViolations(page, "login view");

    // 1. Sign up in three steps (UI-AUTH-07): email + terms -> code from the
    //    dev inbox -> name + password.
    await page.getByRole("button", { name: labels.signUp }).click();
    const signUpForm = page.getByRole("form", { name: labels.signUpForm });
    await expect(
      signUpForm.getByRole("heading", { level: 2, name: labels.emailStep }),
    ).toBeVisible();
    const emailField = signUpForm.getByRole("textbox", { name: labels.email });
    await expect(emailField).toBeFocused();
    // The brand row's Log In stays; no social, passkey or school SSO entry.
    await expect(
      page.getByRole("button", { name: labels.logIn }),
    ).toBeVisible();
    await expect(
      page
        .locator(".sign-in__panel")
        .getByRole("button", { name: labels.otherSignInMethods }),
    ).toHaveCount(0);
    await expectNoBlockingAxeViolations(page, "sign-up step 1 (incomplete)");
    await emailField.fill(email);
    await signUpForm.getByRole("checkbox", { name: labels.terms }).check();
    await expectNoBlockingAxeViolations(page, "sign-up step 1 (ready)");
    const challengeId = await requestSignupCode(page, signUpForm);
    await expectNoBlockingAxeViolations(page, "sign-up step 2 (code)");
    const delivered = await readDevInbox(request, challengeId);
    expect(delivered.kind).toBe("challenge");
    expect(delivered.purpose).toBe("signup_email");
    const code = delivered.code ?? "";
    expect(code).toMatch(/^\d{6}$/);

    await signUpForm
      .getByRole("textbox", { name: labels.signUpOtp })
      .fill(code);
    await signUpForm.getByRole("button", { name: labels.continue }).click();
    await expect(
      signUpForm.getByRole("heading", { level: 2, name: labels.accountStep }),
    ).toBeVisible();
    const fullNameField = signUpForm.getByRole("textbox", {
      name: labels.fullName,
    });
    await expect(fullNameField).toBeFocused();
    await fullNameField.fill(displayName);
    await signUpForm.getByLabel(labels.password).fill(password);
    await expectNoBlockingAxeViolations(page, "sign-up step 3 (account)");

    const completeResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        apiPath(response.url()).endsWith("/v1/auth/signup/email/complete"),
    );
    await signUpForm
      .getByRole("button", { name: labels.createAccount })
      .click();
    const complete = await completeResponse;
    expect(complete.status()).toBe(201);
    // UI-AUTH-07: sign-up on /sign-in asks for a persistent session too.
    expect(complete.request().postDataJSON()).toMatchObject({
      keep_signed_in: true,
    });
    await expectSignedInAs(page, email, displayName);

    // The session lives only in HttpOnly cookies; no secret reaches web storage.
    const sessionCookies = (await context.cookies()).filter((cookie) =>
      /session/i.test(cookie.name),
    );
    expect(sessionCookies.length).toBeGreaterThan(0);
    for (const cookie of sessionCookies) {
      expect(cookie.httpOnly).toBe(true);
    }
    await expectBrowserStorageWithoutSecrets(page, [password, code]);

    // 2. Sign out through the CSRF-protected BFF logout.
    await page.getByRole("button", { name: labels.signOut }).click();
    await expect(page).toHaveURL(/\/signed-out$/);

    // 3. Requesting a code for the now-registered email looks identical in the
    //    UI, but only an "account exists" notice without a code is delivered.
    const repeatForm = await openSignUpForm(page);
    await repeatForm.getByRole("textbox", { name: labels.email }).fill(email);
    await repeatForm.getByRole("checkbox", { name: labels.terms }).check();
    const repeatChallengeId = await requestSignupCode(page, repeatForm);
    // "<" returns to the email step with the address and consent kept.
    await repeatForm.getByRole("button", { name: labels.backToEmail }).click();
    await expect(
      repeatForm.getByRole("textbox", { name: labels.email }),
    ).toHaveValue(email);
    await expect(
      repeatForm.getByRole("checkbox", { name: labels.terms }),
    ).toBeChecked();
    const notice = await readDevInbox(request, repeatChallengeId);
    expect(notice.kind).toBe("notice");
    expect(notice.notice_kind).toBe("account_exists");
    expect(notice.code).toBeUndefined();

    // 4. Log back in with the password that was just created.
    await page.goto("/sign-in");
    await page.getByRole("textbox", { name: labels.identifier }).fill(email);
    await page.getByLabel(labels.password).fill(password);
    const loginResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        apiPath(response.url()).endsWith("/v1/auth/password/login"),
    );
    await page.getByRole("button", { name: labels.logIn }).click();
    const login = await loginResponse;
    expect(login.status()).toBe(200);
    // UI-AUTH-04: /sign-in always asks for a persistent session.
    expect(login.request().postDataJSON()).toMatchObject({
      keep_signed_in: true,
    });
    expect(await login.json()).toEqual({ status: "authenticated" });
    await expectSignedInAs(page, email, displayName);
    await expectBrowserStorageWithoutSecrets(page, [password, code]);
  });

  test("rejects a wrong password with one generic message", async ({
    page,
  }) => {
    await page.goto("/sign-in");
    await page
      .getByRole("textbox", { name: labels.identifier })
      .fill(`unknown.${randomBytes(4).toString("hex")}@example.test`);
    const passwordField = page.getByLabel(labels.password);
    await passwordField.fill("not-the-right-password");
    // Holding the eye button reveals the password; releasing hides it again.
    const eye = page.getByRole("button", { name: labels.holdToShowPassword });
    await eye.hover();
    await page.mouse.down();
    await expect(passwordField).toHaveAttribute("type", "text");
    await expect(eye).toHaveAttribute("aria-pressed", "true");
    await expectNoBlockingAxeViolations(page, "login password shown");
    await page.mouse.up();
    await expect(passwordField).toHaveAttribute("type", "password");
    await expect(eye).toHaveAttribute("aria-pressed", "false");
    await page.getByRole("button", { name: labels.logIn }).click();
    await expect(passwordField).toHaveAttribute("type", "password");

    await expect(page.getByRole("alert")).toHaveText(
      /^(?:Email hoặc mật khẩu không đúng\.|Incorrect email or password\.)$/,
    );
    await expect(page).toHaveURL(/\/sign-in$/);
    await expect(page.getByLabel(labels.password)).toHaveValue("");
    await expectNoBlockingAxeViolations(page, "login error");
  });
});
