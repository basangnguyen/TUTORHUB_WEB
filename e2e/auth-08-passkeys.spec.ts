import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type CDPSession,
  type Page,
  type Response,
} from "@playwright/test";
import { generateKeyPairSync, randomBytes } from "node:crypto";

/**
 * AUTH-08B: passkey registration, rename and removal in Settings, passwordless
 * sign-in (button and autofill), passkey step-up and the Signal API, against
 * the loopback stack only. Requires `node scripts/e2e-local.mjs serve
 * --native-auth` (started by playwright.auth.config.ts), which turns on
 * AUTH_PASSKEYS_ENABLED with the `localhost` relying party — WebAuthn rejects
 * an IP address as RP ID — and serves the SPA on http://localhost:5173.
 *
 * Passkeys live in a Chromium CDP virtual authenticator (CTAP2, internal,
 * resident keys, user verification). A virtual authenticator answers an
 * autofill (conditional) request at once, so the button scenarios first turn
 * automatic presence off: the autofill request then stays pending, which
 * proves the page aborts it before opening the modal prompt.
 *
 * Every session here is younger than the server's 10-minute recent-auth
 * window, so the "needs a step-up" signal (403
 * `auth_recent_authentication_required`) is injected once per scenario with
 * `page.route`; re-authentication, the retried action and every passkey
 * ceremony run against the real Core API. A small init script only records
 * `navigator.credentials.get` calls and Signal API calls (it forwards both
 * unchanged) so the spec can assert the autofill lifecycle.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://localhost:5173";
const googleOrigin = "http://127.0.0.1:9092";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

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
  keepSignedIn: /^(?:Duy trì đăng nhập|Keep me signed in)$/,
  google: /^(?:Đăng nhập bằng Google|Sign in with Google)$/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  passkeySignIn: /^(?:Đăng nhập bằng Passkey|Sign in with a passkey)$/,
  passkeyCancelled:
    /^(?:Đã hủy xác thực passkey\.|Passkey sign-in was cancelled\.)$/,
  passkeyInvalid:
    /(?:Không thể xác minh passkey này|We couldn't verify this passkey)/,
  passkeysCard: /^(?:Passkey|Passkeys)$/,
  addPasskey: /^(?:Thêm passkey|Add a passkey)$/,
  addPasskeyForm: /^(?:Thêm passkey mới|Add a new passkey)$/,
  passkeyName: /^(?:Tên passkey|Passkey name)$/,
  createPasskey: /^(?:Tạo passkey|Create passkey)$/,
  passkeyAdded: (label: string) =>
    new RegExp(
      `^(?:Đã thêm passkey|Passkey) "${escapeRegExp(label)}"(?: added)?\\.$`,
    ),
  renamePasskey: (label: string) =>
    new RegExp(`^(?:Đổi tên passkey|Rename passkey) ${escapeRegExp(label)}$`),
  removePasskey: (label: string) =>
    new RegExp(`^(?:Xóa passkey|Remove passkey) ${escapeRegExp(label)}$`),
  saveName: /^(?:Lưu tên|Save name)$/,
  renamed: /^(?:Đã đổi tên passkey\.|Passkey renamed\.)$/,
  removeDialog: /^(?:Xóa passkey này\?|Remove this passkey\?)$/,
  removeConfirm: /^(?:Xóa passkey|Remove passkey)$/,
  removed: /^(?:Đã xóa passkey\.|Passkey removed\.)$/,
  emptyPasskeys: /^(?:Chưa có passkey nào|No passkeys yet)$/,
  deviceBound: /^(?:Trên thiết bị|Device-bound)$/,
  lastMethod:
    /(?:phương thức đăng nhập cuối cùng|it's your last sign-in method)/,
  identityCard: /^(?:Danh tính đăng nhập|Sign-in identities)$/,
  unlink: /^(?:Hủy liên kết|Unlink)$/,
  passwordCard: /^(?:Mật khẩu|Password)$/,
  newPassword: /^(?:Mật khẩu mới|New password)$/,
  confirmPassword: /^(?:Nhập lại mật khẩu mới|Confirm new password)$/,
  setPassword: /^(?:Đặt mật khẩu|Set password)$/,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpMethods: /^(?:Cách xác nhận|Confirmation method)$/,
  stepUpPasskeyMethod: /^Passkey$/,
  stepUpPasswordMethod: /^(?:Mật khẩu|Password)$/,
  stepUpEmailMethod: /^(?:Mã gửi qua email|Email code)$/,
  stepUpPassword: /^(?:Mật khẩu hiện tại|Current password)$/,
  stepUpConfirm: /^(?:Xác nhận|Confirm)$/,
  stepUpPasskeyConfirm: /^(?:Xác nhận bằng passkey|Confirm with passkey)$/,
  stepUpPasskeyOnly:
    /(?:chỉ có thể xác nhận lại bằng passkey|can only be confirmed with a passkey)/,
  stepUpPasskeyCancelled:
    /(?:Đã hủy xác thực passkey|Passkey verification was cancelled)/,
  stepUpSendCode: /^(?:Gửi mã|Send code)$/,
  stepUpCode: /^(?:Mã xác nhận|Verification code)$/,
} as const;

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  purpose: string;
}

interface PasskeyProbe {
  gets: { mediation: string; outcome: string }[];
  signals: { credentialId: string; rpId: string }[];
}

declare global {
  interface Window {
    __passkeyProbe?: PasskeyProbe;
  }
}

/**
 * Records WebAuthn calls without changing them: `navigator.credentials.get`
 * (mediation + outcome) and `PublicKeyCredential.signalUnknownCredential`.
 */
function installPasskeyProbe() {
  const probe: PasskeyProbe = { gets: [], signals: [] };
  window.__passkeyProbe = probe;
  const credentials = navigator.credentials;
  if (credentials && typeof credentials.get === "function") {
    const originalGet = credentials.get.bind(credentials);
    credentials.get = (options?: CredentialRequestOptions) => {
      const entry = {
        mediation: options?.mediation ?? "optional",
        outcome: "pending",
      };
      probe.gets.push(entry);
      return originalGet(options).then(
        (credential) => {
          entry.outcome = "resolved";
          return credential;
        },
        (error: unknown) => {
          entry.outcome =
            error instanceof DOMException ? error.name : "rejected";
          throw error;
        },
      );
    };
  }
  const statics = window.PublicKeyCredential as unknown as
    | {
        signalUnknownCredential?: (options: {
          credentialId: string;
          rpId: string;
        }) => Promise<void>;
      }
    | undefined;
  const originalSignal = statics?.signalUnknownCredential;
  if (statics && typeof originalSignal === "function") {
    statics.signalUnknownCredential = (options) => {
      probe.signals.push({
        credentialId: options.credentialId,
        rpId: options.rpId,
      });
      return originalSignal.call(statics, options);
    };
  }
}

async function readProbe(page: Page): Promise<PasskeyProbe> {
  return page.evaluate(
    () => window.__passkeyProbe ?? { gets: [], signals: [] },
  );
}

interface VirtualAuthenticator {
  authenticatorId: string;
  cdp: CDPSession;
}

/** A platform-style CTAP2 authenticator with resident keys and UV. */
async function addVirtualAuthenticator(
  page: Page,
): Promise<VirtualAuthenticator> {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("WebAuthn.enable", { enableUI: false });
  const { authenticatorId } = await cdp.send(
    "WebAuthn.addVirtualAuthenticator",
    {
      options: {
        automaticPresenceSimulation: true,
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        protocol: "ctap2",
        transport: "internal",
      },
    },
  );
  return { authenticatorId, cdp };
}

async function setPresence(
  authenticator: VirtualAuthenticator,
  enabled: boolean,
) {
  await authenticator.cdp.send("WebAuthn.setAutomaticPresenceSimulation", {
    authenticatorId: authenticator.authenticatorId,
    enabled,
  });
}

async function setUserVerified(
  authenticator: VirtualAuthenticator,
  isUserVerified: boolean,
) {
  await authenticator.cdp.send("WebAuthn.setUserVerified", {
    authenticatorId: authenticator.authenticatorId,
    isUserVerified,
  });
}

async function storedCredentials(authenticator: VirtualAuthenticator) {
  const { credentials } = await authenticator.cdp.send(
    "WebAuthn.getCredentials",
    { authenticatorId: authenticator.authenticatorId },
  );
  return credentials;
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
  await context.addInitScript(installPasskeyProbe);
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

async function createWorkspace(page: Page, suffix: string) {
  const onboarding = page.getByRole("heading", {
    name: /^(?:Tạo workspace đầu tiên|Create your first workspace)$/,
  });
  await expect(onboarding).toBeVisible();
  await page
    .getByLabel(
      /^(?:Tên tổ chức hoặc nhóm học|Organization or learning group name)$/,
    )
    .fill(`AUTH08 Workspace ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth08-${suffix}`);
  const created = responseTo(page, "POST", "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

async function signOut(page: Page) {
  await page.goto("/app/home");
  await page.getByRole("button", { name: labels.signOut }).click();
  await expect(page).toHaveURL(/\/signed-out$/);
}

async function meStatus(page: Page) {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  return response.status();
}

async function sessionCookies(context: BrowserContext) {
  return (await context.cookies()).filter((cookie) =>
    /session/i.test(cookie.name),
  );
}

/**
 * Opens /sign-in with automatic presence off, so the autofill (conditional)
 * request the page starts stays pending; presence comes back on before the
 * caller clicks the passkey button.
 */
async function openSignInWithPendingAutofill(
  page: Page,
  authenticator: VirtualAuthenticator,
) {
  await setPresence(authenticator, false);
  const autofillOptions = responseTo(
    page,
    "POST",
    "/v1/auth/passkeys/assertion/options",
  );
  await page.goto("/sign-in");
  expect((await autofillOptions).status()).toBe(200);
  await expect
    .poll(
      async () =>
        (await readProbe(page)).gets.filter(
          (call) =>
            call.mediation === "conditional" && call.outcome === "pending",
        ).length,
    )
    .toBe(1);
  await setPresence(authenticator, true);
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
  expect(stored).not.toMatch(/link_tx|auth_tx|tutorhub_session|csrf/i);
  for (const secret of secrets) {
    if (secret) expect(stored).not.toContain(secret);
  }
}

function recentAuthRequired() {
  return JSON.stringify({
    type: "about:blank",
    title: "Recent authentication required",
    status: 403,
    code: "auth_recent_authentication_required",
    detail: "Verify your identity before changing passkeys.",
  });
}

/** Answers the next matching request like a session past its window. */
async function injectRecentAuthOnce(
  page: Page,
  pattern: string,
  method: string,
) {
  let injected = false;
  await page.route(pattern, async (route) => {
    if (injected || route.request().method() !== method) {
      await route.fallback();
      return;
    }
    injected = true;
    await route.fulfill({
      body: recentAuthRequired(),
      contentType: "application/problem+json",
      status: 403,
    });
  });
  return () => injected;
}

test.describe("AUTH-08 passkeys", () => {
  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "Passkeys run only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      passkeys_enabled: true,
      password_enabled: true,
      signup_enabled: true,
    });
  });

  test("registers and renames a passkey, signs in without a password and steps up with it", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const suffix = randomBytes(6).toString("hex");
    const email = `auth08.${suffix}@example.test`;
    const password = `Harbor-${randomBytes(9).toString("base64url")}-Maple7`;
    const secrets: string[] = [password];
    const { context, page } = await openDevice(browser);
    const passkeysCard = page.getByRole("region", {
      name: labels.passkeysCard,
    });

    try {
      await test.step("sign up with a password and create a workspace", async () => {
        secrets.push(
          await signUp(page, request, {
            displayName: `AUTH08 Owner ${suffix.slice(0, 4)}`,
            email,
            password,
          }),
        );
        await createWorkspace(page, suffix);
      });

      const authenticator = await addVirtualAuthenticator(page);

      await test.step("Settings lists no passkeys yet", async () => {
        await page.goto("/app/settings");
        await expect(
          passkeysCard.getByRole("heading", { name: labels.emptyPasskeys }),
        ).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          'section[aria-labelledby="security-passkeys-title"]',
          "passkeys card (empty)",
        );
      });

      await test.step("adding a passkey steps up first, then creates it with an edited name", async () => {
        const wasInjected = await injectRecentAuthOnce(
          page,
          "**/api/v1/me/passkeys/registration/options",
          "POST",
        );
        await passkeysCard
          .getByRole("button", { name: labels.addPasskey })
          .click();
        const form = passkeysCard.getByRole("form", {
          name: labels.addPasskeyForm,
        });
        const name = form.getByRole("textbox", { name: labels.passkeyName });
        // Playwright's Desktop Chrome profile reports Windows.
        await expect(name).toHaveValue("Windows · Chrome");
        await name.fill("E2E laptop");
        await form.getByRole("button", { name: labels.createPasskey }).click();

        const dialog = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(dialog).toBeVisible();
        expect(wasInjected()).toBe(true);
        await page.unroute("**/api/v1/me/passkeys/registration/options");
        // No passkey exists yet: the password comes first.
        await expect(
          dialog.getByRole("button", { name: labels.stepUpPasskeyMethod }),
        ).toHaveCount(0);
        await dialog.getByLabel(labels.stepUpPassword).fill(password);
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const options = responseTo(
          page,
          "POST",
          "/v1/me/passkeys/registration/options",
        );
        const verify = responseTo(
          page,
          "POST",
          "/v1/me/passkeys/registration/verify",
        );
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        expect((await reauth).status()).toBe(200);
        expect((await options).status()).toBe(200);
        const created = await verify;
        expect(created.status()).toBe(201);
        expect(await created.json()).toMatchObject({ label: "E2E laptop" });
        await expect(dialog).toBeHidden();
        await expect(
          passkeysCard.getByText(labels.passkeyAdded("E2E laptop")),
        ).toBeVisible();
        const row = passkeysCard.locator("li.passkey-row", {
          hasText: "E2E laptop",
        });
        await expect(row.getByText(labels.deviceBound)).toBeVisible();
        expect(await storedCredentials(authenticator)).toHaveLength(1);
      });

      await test.step("rename the passkey in place", async () => {
        await passkeysCard
          .getByRole("button", { name: labels.renamePasskey("E2E laptop") })
          .click();
        const form = passkeysCard.getByRole("form", {
          name: labels.renamePasskey("E2E laptop"),
        });
        await form
          .getByRole("textbox", { name: labels.passkeyName })
          .fill("E2E work laptop");
        const renamed = responseTo(page, "PATCH", /\/v1\/me\/passkeys\/[^/]+$/);
        await form.getByRole("button", { name: labels.saveName }).click();
        expect((await renamed).status()).toBe(200);
        await expect(passkeysCard.getByText(labels.renamed)).toBeVisible();
        await expect(
          passkeysCard.locator("li.passkey-row strong", {
            hasText: "E2E work laptop",
          }),
        ).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          'section[aria-labelledby="security-passkeys-title"]',
          "passkeys card",
        );
      });

      await test.step("an AAL1 step-up can move from the passkey to an emailed code", async () => {
        const wasInjected = await injectRecentAuthOnce(
          page,
          "**/api/v1/me/passkeys/*",
          "PATCH",
        );
        await passkeysCard
          .getByRole("button", {
            name: labels.renamePasskey("E2E work laptop"),
          })
          .click();
        const form = passkeysCard.getByRole("form", {
          name: labels.renamePasskey("E2E work laptop"),
        });
        await form
          .getByRole("textbox", { name: labels.passkeyName })
          .fill("E2E key");
        await form.getByRole("button", { name: labels.saveName }).click();

        const dialog = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(dialog).toBeVisible();
        expect(wasInjected()).toBe(true);
        await page.unroute("**/api/v1/me/passkeys/*");
        const methods = dialog.getByRole("group", {
          name: labels.stepUpMethods,
        });
        await expect(methods.getByRole("button")).toHaveText([
          labels.stepUpPasskeyMethod,
          labels.stepUpPasswordMethod,
          labels.stepUpEmailMethod,
        ]);
        await expect(
          methods.getByRole("button", { name: labels.stepUpPasskeyMethod }),
        ).toHaveAttribute("aria-pressed", "true");

        // The device refuses user verification: a quiet cancel, no error.
        await setUserVerified(authenticator, false);
        const passkeyOptions = responseTo(
          page,
          "POST",
          "/v1/auth/reauth/passkey/options",
        );
        await dialog
          .getByRole("button", { name: labels.stepUpPasskeyConfirm })
          .click();
        expect((await passkeyOptions).status()).toBe(200);
        await expect(
          dialog.getByText(labels.stepUpPasskeyCancelled),
        ).toBeVisible();
        await expect(dialog.getByRole("alert")).toHaveCount(0);
        await setUserVerified(authenticator, true);

        await methods
          .getByRole("button", { name: labels.stepUpEmailMethod })
          .click();
        const started = responseTo(
          page,
          "POST",
          "/v1/auth/reauth/email-code/start",
        );
        await dialog
          .getByRole("button", { name: labels.stepUpSendCode })
          .click();
        const challengeId = await readChallengeId(await started);
        const code = await waitForDevInboxCode(request, challengeId, "reauth");
        secrets.push(code);
        await dialog
          .getByRole("textbox", { name: labels.stepUpCode })
          .fill(code);
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const resumed = responseTo(page, "PATCH", /\/v1\/me\/passkeys\/[^/]+$/);
        await dialog
          .getByRole("button", { name: labels.stepUpConfirm })
          .click();
        expect((await reauth).status()).toBe(200);
        expect((await resumed).status()).toBe(200);
        await expect(dialog).toBeHidden();
        await expect(passkeysCard.getByText(labels.renamed)).toBeVisible();
        await expect(
          passkeysCard.locator("li.passkey-row strong", { hasText: "E2E key" }),
        ).toBeVisible();
        expect(await meStatus(page)).toBe(200);
      });

      await test.step("sign out, then sign in with the passkey button (autofill is aborted first)", async () => {
        await signOut(page);
        await openSignInWithPendingAutofill(page, authenticator);
        await expect(
          page.getByRole("textbox", { name: labels.identifier }),
        ).toHaveAttribute("autocomplete", "username webauthn");
        await expectNoBlockingAxeViolations(page, "main", "/sign-in");

        await page.getByRole("checkbox", { name: labels.keepSignedIn }).check();
        const options = responseTo(
          page,
          "POST",
          "/v1/auth/passkeys/assertion/options",
        );
        const verify = responseTo(
          page,
          "POST",
          "/v1/auth/passkeys/assertion/verify",
        );
        await page.getByRole("button", { name: labels.passkeySignIn }).click();
        const optionsResponse = await options;
        expect(optionsResponse.status()).toBe(200);
        const { challenge } = (await optionsResponse.json()) as {
          challenge: string;
        };
        // The challenge must never reach Web Storage (checked below).
        secrets.push(challenge);
        const verified = await verify;
        expect(verified.status()).toBe(200);
        expect(verified.request().postDataJSON()).toMatchObject({
          keep_signed_in: true,
        });
        await expect(page).toHaveURL(/\/app\/home$/);

        const calls = (await readProbe(page)).gets;
        expect(calls.map((call) => call.mediation)).toEqual([
          "conditional",
          "optional",
        ]);
        expect(calls[0]?.outcome).toBe("AbortError");
        expect(calls[1]?.outcome).toBe("resolved");

        const cookies = await sessionCookies(context);
        expect(cookies.length).toBeGreaterThan(0);
        for (const cookie of cookies) {
          expect(cookie.httpOnly).toBe(true);
          // "Keep me signed in": a persistent cookie, not a browser session one.
          expect(cookie.expires).toBeGreaterThan(Date.now() / 1000 + 3600);
        }
        // A user-verified passkey session is AAL2: only a passkey re-authenticates.
        const security = await page.request.get("/api/v1/me/security", {
          headers: { Accept: "application/json" },
        });
        expect(security.status()).toBe(200);
        expect(await security.json()).toMatchObject({
          passkey_count: 1,
          reauth_methods: ["passkey"],
        });
      });

      await test.step("an AAL2 session steps up with the passkey and resumes the rename", async () => {
        await page.goto("/app/settings");
        const before = (await sessionCookies(context)).map(
          (cookie) => cookie.value,
        );
        const wasInjected = await injectRecentAuthOnce(
          page,
          "**/api/v1/me/passkeys/*",
          "PATCH",
        );
        await passkeysCard
          .getByRole("button", { name: labels.renamePasskey("E2E key") })
          .click();
        const form = passkeysCard.getByRole("form", {
          name: labels.renamePasskey("E2E key"),
        });
        await form
          .getByRole("textbox", { name: labels.passkeyName })
          .fill("E2E key AAL2");
        await form.getByRole("button", { name: labels.saveName }).click();

        const dialog = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(dialog).toBeVisible();
        expect(wasInjected()).toBe(true);
        await page.unroute("**/api/v1/me/passkeys/*");
        await expect(dialog.getByText(labels.stepUpPasskeyOnly)).toBeVisible();
        await expect(
          dialog.getByRole("group", { name: labels.stepUpMethods }),
        ).toHaveCount(0);
        await expectNoBlockingAxeViolations(
          page,
          '[role="dialog"]',
          "passkey step-up",
        );

        const options = responseTo(
          page,
          "POST",
          "/v1/auth/reauth/passkey/options",
        );
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const resumed = responseTo(page, "PATCH", /\/v1\/me\/passkeys\/[^/]+$/);
        await dialog
          .getByRole("button", { name: labels.stepUpPasskeyConfirm })
          .click();
        expect((await options).status()).toBe(200);
        const reauthenticated = await reauth;
        expect(reauthenticated.status()).toBe(200);
        expect(reauthenticated.request().postDataJSON()).toMatchObject({
          method: "passkey",
        });
        expect((await resumed).status()).toBe(200);
        await expect(dialog).toBeHidden();
        await expect(
          passkeysCard.locator("li.passkey-row strong", {
            hasText: "E2E key AAL2",
          }),
        ).toBeVisible();
        const after = (await sessionCookies(context)).map(
          (cookie) => cookie.value,
        );
        expect(after).not.toEqual(before);
        expect(await meStatus(page)).toBe(200);
      });

      await test.step("passkey autofill signs in without a click", async () => {
        await signOut(page);
        const verify = responseTo(
          page,
          "POST",
          "/v1/auth/passkeys/assertion/verify",
        );
        await page.goto("/sign-in");
        expect((await verify).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        const calls = (await readProbe(page)).gets;
        expect(calls).toEqual([
          { mediation: "conditional", outcome: "resolved" },
        ]);
        expect(await meStatus(page)).toBe(200);
      });

      await test.step("no credential ID, challenge or token reaches Web Storage", async () => {
        const credentialIds = (await storedCredentials(authenticator)).map(
          (credential) =>
            Buffer.from(credential.credentialId, "base64").toString(
              "base64url",
            ),
        );
        const tokens = (await sessionCookies(context)).map(
          (cookie) => cookie.value,
        );
        await expectBrowserStorageWithoutSecrets(page, [
          ...secrets,
          ...credentialIds,
          ...tokens,
        ]);
      });
    } finally {
      await context.close();
    }
  });

  test("keeps the last sign-in method, removes the passkey once a password exists and rejects it afterwards", async ({
    browser,
  }) => {
    test.setTimeout(240_000);
    const suffix = randomBytes(6).toString("hex");
    const email = `auth08.google.${suffix}@example.test`;
    const password = `Willow-${randomBytes(9).toString("base64url")}-Cedar4`;
    const { context, page } = await openDevice(browser);
    const passkeysCard = page.getByRole("region", {
      name: labels.passkeysCard,
    });

    try {
      await test.step("sign up with Google only and create a workspace", async () => {
        await page.goto("/sign-in");
        await page.getByRole("button", { name: labels.google }).click();
        await page.waitForURL(
          (url) => url.origin === googleOrigin && url.pathname === "/authorize",
        );
        await page.getByRole("textbox", { name: "Test email" }).fill(email);
        await page
          .getByRole("button", { name: "Sign in with custom account" })
          .click();
        await expect(page).toHaveURL(/\/app\/home$/);
        await createWorkspace(page, suffix);
      });

      const authenticator = await addVirtualAuthenticator(page);

      await test.step("add a passkey, then unlink Google so the passkey is the only method", async () => {
        await page.goto("/app/settings");
        await passkeysCard
          .getByRole("button", { name: labels.addPasskey })
          .click();
        const form = passkeysCard.getByRole("form", {
          name: labels.addPasskeyForm,
        });
        await form
          .getByRole("textbox", { name: labels.passkeyName })
          .fill("Only key");
        const verify = responseTo(
          page,
          "POST",
          "/v1/me/passkeys/registration/verify",
        );
        await form.getByRole("button", { name: labels.createPasskey }).click();
        expect((await verify).status()).toBe(201);
        await expect(
          passkeysCard.getByText(labels.passkeyAdded("Only key")),
        ).toBeVisible();

        const identityCard = page.getByRole("region", {
          name: labels.identityCard,
        });
        const unlink = identityCard.getByRole("button", {
          name: labels.unlink,
        });
        await expect(unlink).toBeEnabled();
        const unlinked = responseTo(
          page,
          "DELETE",
          /\/v1\/me\/identities\/[^/]+$/,
        );
        await unlink.click();
        expect((await unlinked).status()).toBe(204);
        await expect(
          identityCard.getByRole("button", { name: labels.unlink }),
        ).toHaveCount(0);
      });

      await test.step("removing the last sign-in method steps up to AAL2, then is refused (409 auth_last_identity)", async () => {
        await passkeysCard
          .getByRole("button", { name: labels.removePasskey("Only key") })
          .click();
        const dialog = page.getByRole("dialog", { name: labels.removeDialog });
        await expect(dialog).toBeVisible();
        // AUTH-09: with AUTH_MFA_TOTP_ENABLED the passkey is a second factor,
        // so this AAL1 Google session must reach AAL2 before changing it.
        const assurance = responseTo(
          page,
          "DELETE",
          /\/v1\/me\/passkeys\/[^/]+$/,
        );
        await dialog
          .getByRole("button", { name: labels.removeConfirm })
          .click();
        const needsAal2 = await assurance;
        expect(needsAal2.status()).toBe(403);
        expect(await needsAal2.json()).toMatchObject({
          code: "auth_assurance_required",
        });
        const stepUp = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(stepUp).toBeVisible();
        // Only AAL2 factors are offered: no emailed code for this refusal.
        await expect(
          stepUp.getByRole("button", { name: labels.stepUpEmailMethod }),
        ).toHaveCount(0);
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const refused = responseTo(
          page,
          "DELETE",
          /\/v1\/me\/passkeys\/[^/]+$/,
        );
        await stepUp
          .getByRole("button", { name: labels.stepUpPasskeyConfirm })
          .click();
        expect((await reauth).status()).toBe(200);
        const response = await refused;
        expect(response.status()).toBe(409);
        expect(await response.json()).toMatchObject({
          code: "auth_last_identity",
        });
        await expect(stepUp).toBeHidden();
        await expect(dialog).toBeHidden();
        await expect(passkeysCard.getByRole("alert")).toContainText(
          labels.lastMethod,
        );
        await expect(passkeysCard.locator("li.passkey-row")).toHaveCount(1);
      });

      // The AAL2 session from the passkey step-up may set the first password.
      await test.step("with a password set, the passkey can be removed", async () => {
        const card = page.getByRole("region", { name: labels.passwordCard });
        await card.getByLabel(labels.newPassword).fill(password);
        await card.getByLabel(labels.confirmPassword).fill(password);
        const saved = responseTo(page, "POST", "/v1/me/password");
        await card.getByRole("button", { name: labels.setPassword }).click();
        expect((await saved).status()).toBe(200);

        await passkeysCard
          .getByRole("button", { name: labels.removePasskey("Only key") })
          .click();
        const dialog = page.getByRole("dialog", { name: labels.removeDialog });
        const removed = responseTo(
          page,
          "DELETE",
          /\/v1\/me\/passkeys\/[^/]+$/,
        );
        await dialog
          .getByRole("button", { name: labels.removeConfirm })
          .click();
        expect((await removed).status()).toBe(204);
        await expect(passkeysCard.getByText(labels.removed)).toBeVisible();
        await expect(
          passkeysCard.getByRole("heading", { name: labels.emptyPasskeys }),
        ).toBeVisible();
      });

      await test.step("the removed credential no longer signs in and is never signalled", async () => {
        await signOut(page);
        // The authenticator still holds the credential the server revoked.
        expect(await storedCredentials(authenticator)).toHaveLength(1);
        await openSignInWithPendingAutofill(page, authenticator);
        const verify = responseTo(
          page,
          "POST",
          "/v1/auth/passkeys/assertion/verify",
        );
        await page.getByRole("button", { name: labels.passkeySignIn }).click();
        const rejected = await verify;
        expect(rejected.status()).toBe(401);
        const body = (await rejected.json()) as Record<string, unknown>;
        expect(body).toMatchObject({ code: "auth_invalid_passkey" });
        expect(body.unknown_credential).toBeUndefined();
        await expect(page.getByRole("alert")).toContainText(
          labels.passkeyInvalid,
        );
        await expect(page).toHaveURL(/\/sign-in$/);
        expect((await readProbe(page)).signals).toEqual([]);
        expect(await meStatus(page)).toBe(401);
      });
    } finally {
      await context.close();
    }
  });

  test("signals an unknown credential to the authenticator and keeps a cancel quiet", async ({
    browser,
  }) => {
    const { context, page } = await openDevice(browser);
    try {
      await page.goto("/signed-out");
      const authenticator = await addVirtualAuthenticator(page);
      // A discoverable credential this server never issued.
      const credentialId = randomBytes(32);
      const { privateKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });
      await authenticator.cdp.send("WebAuthn.addCredential", {
        authenticatorId: authenticator.authenticatorId,
        credential: {
          credentialId: credentialId.toString("base64"),
          isResidentCredential: true,
          privateKey: privateKey
            .export({ format: "der", type: "pkcs8" })
            .toString("base64"),
          rpId: "localhost",
          signCount: 0,
          userHandle: randomBytes(64).toString("base64"),
        },
      });

      await test.step("the server reports the credential as unknown and the page signals it", async () => {
        await openSignInWithPendingAutofill(page, authenticator);
        const verify = responseTo(
          page,
          "POST",
          "/v1/auth/passkeys/assertion/verify",
        );
        await page.getByRole("button", { name: labels.passkeySignIn }).click();
        const rejected = await verify;
        expect(rejected.status()).toBe(401);
        expect(await rejected.json()).toMatchObject({
          code: "auth_invalid_passkey",
          unknown_credential: true,
        });
        await expect(page.getByRole("alert")).toContainText(
          labels.passkeyInvalid,
        );
        await expect
          .poll(async () => (await readProbe(page)).signals)
          .toEqual([
            {
              credentialId: credentialId.toString("base64url"),
              rpId: "localhost",
            },
          ]);
        // Chromium's Signal API removed it from the authenticator.
        await expect
          .poll(async () => (await storedCredentials(authenticator)).length)
          .toBe(0);
      });

      await test.step("with no passkey left the prompt is a quiet cancel, never signalled", async () => {
        await page.getByRole("button", { name: labels.passkeySignIn }).click();
        await expect(page.getByText(labels.passkeyCancelled)).toBeVisible();
        await expect(page.getByRole("alert")).toHaveCount(0);
        expect((await readProbe(page)).signals).toHaveLength(1);
        expect(await meStatus(page)).toBe(401);
        await expectBrowserStorageWithoutSecrets(page, [
          credentialId.toString("base64url"),
        ]);
      });
    } finally {
      await context.close();
    }
  });
});
