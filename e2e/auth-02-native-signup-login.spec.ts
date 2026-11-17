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
  getCode: /^(?:Lấy mã|Get Code)$/,
  otp: /^(?:Mã OTP|OTP code)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  keepSignedIn: /^(?:Duy trì đăng nhập|Keep me signed in)$/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  codeSent:
    /(?:mã xác thực 6 chữ số đã được gửi|a 6-digit verification code has been sent)/,
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

async function requestSignupCode(page: Page, form: Locator) {
  const startResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      apiPath(response.url()).endsWith("/v1/auth/signup/email/start"),
  );
  await form.getByRole("button", { name: labels.getCode }).click();
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
  await expect(form.getByText(labels.codeSent)).toBeVisible();
  await expect(form.getByRole("button", { name: /^\d+s$/ })).toBeDisabled();
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
    await expect(
      page.getByRole("checkbox", { name: labels.keepSignedIn }),
    ).toBeEnabled();
    await expectNoBlockingAxeViolations(page, "login view");

    // 1. Sign up: name + email -> code from the dev inbox -> terms -> password.
    await page.getByRole("button", { name: labels.signUp }).click();
    const signUpForm = page.getByRole("form", { name: labels.signUpForm });
    await signUpForm
      .getByRole("textbox", { name: labels.fullName })
      .fill(displayName);
    await signUpForm.getByRole("textbox", { name: labels.email }).fill(email);
    const challengeId = await requestSignupCode(page, signUpForm);
    const delivered = await readDevInbox(request, challengeId);
    expect(delivered.kind).toBe("challenge");
    expect(delivered.purpose).toBe("signup_email");
    const code = delivered.code ?? "";
    expect(code).toMatch(/^\d{6}$/);

    await signUpForm.getByRole("textbox", { name: labels.otp }).fill(code);
    await signUpForm.getByLabel(labels.password).fill(password);
    await signUpForm.getByRole("checkbox", { name: labels.terms }).check();
    await expectNoBlockingAxeViolations(page, "sign-up view");

    const completeResponse = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        apiPath(response.url()).endsWith("/v1/auth/signup/email/complete"),
    );
    await signUpForm
      .getByRole("button", { name: labels.createAccount })
      .click();
    expect((await completeResponse).status()).toBe(201);
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
    await repeatForm
      .getByRole("textbox", { name: labels.fullName })
      .fill(displayName);
    await repeatForm.getByRole("textbox", { name: labels.email }).fill(email);
    const repeatChallengeId = await requestSignupCode(page, repeatForm);
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
    await page.getByLabel(labels.password).fill("not-the-right-password");
    await page.getByRole("button", { name: labels.logIn }).click();

    await expect(page.getByRole("alert")).toHaveText(
      /^(?:Email hoặc mật khẩu không đúng\.|Incorrect email or password\.)$/,
    );
    await expect(page).toHaveURL(/\/sign-in$/);
    await expect(page.getByLabel(labels.password)).toHaveValue("");
    await expectNoBlockingAxeViolations(page, "login error");
  });
});
