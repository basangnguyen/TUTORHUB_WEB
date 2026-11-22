import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type CDPSession,
  type Locator,
  type Page,
  type Response,
} from "@playwright/test";
import { createHmac, randomBytes, randomInt } from "node:crypto";

/**
 * AUTH-10B: phone (+84) OTP sign-in on /sign-in and /account-link, the
 * Settings phone card (add, landline refusal, AAL2 step-up, remove, last
 * method) and the device list, against the loopback stack only. Requires
 * `node scripts/e2e-local.mjs serve --native-auth`, which turns on
 * AUTH_METHOD_PHONE_ENABLED with the `devinbox` phone sender (VN only).
 *
 * SMS codes are read from `/api/v1/dev/auth-inbox` (`channel: "sms"`). Each
 * scenario uses its own random 0912xxxxxx mobile number so the per-number
 * limits (5 sends per hour) are never reached. Passkeys live in a Chromium
 * CDP virtual authenticator; automatic presence is turned off whenever
 * /sign-in opens so the background autofill request stays pending instead of
 * signing in by itself. TOTP codes follow RFC 6238 (SHA-1, 6 digits, 30 s)
 * and always use a step newer than the last one the server accepted.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://localhost:5173";
const googleOrigin = "http://127.0.0.1:9092";
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);
const phoneCardSelector = 'section[aria-labelledby="security-phone-title"]';

const labels = {
  signUp: /^(?:Đăng ký|Sign Up)$/,
  signUpForm: /^(?:Tạo tài khoản mới|Create an Account)$/,
  fullName: /^(?:Họ và tên|Full Name)$/,
  email: /^(?:Địa chỉ email|Email Address)$/,
  password: /^(?:Mật khẩu|Password)$/,
  terms: /(?:Thỏa thuận người dùng|User Agreement)/,
  createAccount: /^(?:Tạo tài khoản|Create Account)$/,
  // UI-AUTH-07: three-step sign-up (email + terms -> code -> name + password).
  signUpGetCode: /^(?:Lấy mã|Get code)$/,
  signUpOtp: /^(?:Vui lòng nhập mã xác minh|Please enter verification code)$/,
  continue: /^(?:Tiếp tục|Continue)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  logIn: /^(?:Đăng nhập|Log In)$/,
  keepSignedIn: /^(?:Duy trì đăng nhập|Keep me signed in)$/,
  signOut: /^(?:Đăng xuất|Sign out)$/,
  google: /^(?:Đăng nhập bằng Google|Sign in with Google)$/,
  // Phone sign-in (/sign-in and /account-link).
  useOtp: /^(?:Đăng nhập với OTP|Log in with OTP)$/,
  otpChannels: /^(?:Nhận mã OTP qua|Receive the OTP by)$/,
  phoneChannel: /^(?:Số điện thoại|Phone)$/,
  usePassword: /^(?:Đăng nhập bằng mật khẩu|Log in with password)$/,
  phoneForm:
    /^(?:Đăng nhập bằng mã gửi tới số điện thoại|Log in with a code sent to your phone)$/,
  phoneCountry: /^(?:Mã quốc gia|Country code)$/,
  phoneNumber: /^(?:Số điện thoại|Phone number)$/,
  phoneSendCode: /^(?:Gửi mã|Send code)$/,
  phoneCode: /^(?:Mã gồm 6 chữ số|6-digit code)$/,
  phoneVerify: /^(?:Xác minh và đăng nhập|Verify and log in)$/,
  phoneResend: /^(?:Gửi lại mã|Resend code)(?: \(\d+s\))?$/,
  phoneChange: /^(?:Đổi số điện thoại|Change phone number)$/,
  phoneRequired: /^(?:Hãy nhập số điện thoại\.|Enter your phone number\.)$/,
  phoneCodeInvalid:
    /(?:Mã xác minh không đúng hoặc đã hết hạn|That verification code is incorrect or has expired)/,
  phoneExpiresIn: /^(?:Mã hết hạn sau|The code expires in) \d+:\d{2}\.$/,
  // Settings phone card.
  phoneCard: /^(?:Số điện thoại|Phone number)$/,
  phoneEmpty: /^(?:Chưa có số điện thoại|No phone number yet)$/,
  phoneAdd: /^(?:Thêm số điện thoại|Add phone number)$/,
  phoneAddForm: /^(?:Thêm số điện thoại mới|Add a new phone number)$/,
  phoneMobile: /^(?:Số di động|Mobile number)$/,
  phoneSendVerification: /^(?:Gửi mã xác minh|Send verification code)$/,
  phoneSettingsCode: /^(?:Mã xác minh 6 số|6-digit verification code)$/,
  phoneVerifyNumber: /^(?:Xác minh số|Verify number)$/,
  phoneVerified: /^(?:Đã xác minh|Verified)$/,
  phoneInvalid:
    /(?:Số này không hợp lệ hoặc không được hỗ trợ|This number isn't valid or supported)/,
  phoneAdded: /(?:Đã thêm số điện thoại|Phone number .* added)/,
  phoneRemove: /^(?:Gỡ số điện thoại|Remove phone number)$/,
  phoneRemoveDialog: /^(?:Gỡ số điện thoại này\?|Remove this phone number\?)$/,
  phoneRemoveConfirm: /^(?:Gỡ số|Remove number)$/,
  phoneRemoved: /(?:Đã gỡ số điện thoại|Phone number removed)/,
  phoneLastMethod:
    /(?:phương thức đăng nhập khả dụng cuối cùng|your last available sign-in method)/,
  securityCard: /^(?:Bảo mật tài khoản|Account security)$/,
  devicesCard: /^(?:Thiết bị đang đăng nhập|Signed-in devices)$/,
  thisDevice: /(?:Thiết bị này|This device)/,
  phoneMethod: /(?:Mã SĐT|Phone code)/,
  passkeyMethod: /Passkey/,
  persistent: /(?:Duy trì đăng nhập đến|Stays signed in until)/,
  // Passkeys.
  passkeysCard: /^(?:Passkey|Passkeys)$/,
  addPasskey: /^(?:Thêm passkey|Add a passkey)$/,
  addPasskeyForm: /^(?:Thêm passkey mới|Add a new passkey)$/,
  passkeyName: /^(?:Tên passkey|Passkey name)$/,
  createPasskey: /^(?:Tạo passkey|Create passkey)$/,
  // Second step and step-up.
  mfaStep: /^(?:Xác minh hai bước|Two-step verification)$/,
  mfaPasskey: /^(?:Xác minh bằng passkey|Verify with a passkey)$/,
  mfaTotpField:
    /^(?:Mã 6 chữ số từ ứng dụng xác thực|6-digit code from your authenticator app)$/,
  mfaVerify: /^(?:Xác minh|Verify)$/,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpEmailMethod: /^(?:Mã gửi qua email|Email code)$/,
  stepUpPasskeyConfirm: /^(?:Xác nhận bằng passkey|Confirm with passkey)$/,
  // Authenticator app (TOTP) enrollment.
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
  statusOff: /^(?:Chưa bật|Off)$/,
  // /account-link.
  linkSignIn:
    /^(?:Đăng nhập tài khoản TutorHub hiện có|Sign in to your existing TutorHub account)$/,
  linkConfirmHeading: /^(?:Xác nhận liên kết|Confirm linking)$/,
  linkConfirm: /^(?:Liên kết Google|Link Google)$/,
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
  channel?: string;
  code?: string;
  destination?: string;
  purpose: string;
}

interface Account {
  displayName: string;
  email: string;
  password: string;
}

interface TestPhone {
  /** What the user types in the national format. */
  display: string;
  /** What the form sends (separators removed). */
  compact: string;
  e164: string;
  masked: string;
}

interface VirtualAuthenticator {
  authenticatorId: string;
  cdp: CDPSession;
}

function newAccount(label: string): Account {
  const suffix = randomBytes(6).toString("hex");
  return {
    displayName: `AUTH10 ${label} ${suffix.slice(0, 4)}`,
    email: `auth10.${label}.${suffix}@example.test`,
    password: `Harbor-${randomBytes(9).toString("base64url")}-Quartz7`,
  };
}

/** A random Vinaphone-style mobile number: 0912 xxx xxx (+84 912 xxx xxx). */
function newPhone(): TestPhone {
  const subscriber = String(randomInt(0, 1_000_000)).padStart(6, "0");
  return {
    compact: `0912${subscriber}`,
    display: `0912 ${subscriber.slice(0, 3)} ${subscriber.slice(3)}`,
    e164: `+84912${subscriber}`,
    masked: `+84 ***${subscriber.slice(2)}`,
  };
}

/** Every spelling of a number that must never reach Web Storage. */
function phoneSecrets(phone: TestPhone) {
  return [phone.compact, phone.e164, phone.display, phone.e164.slice(1)];
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
  destination?: string,
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
  if (destination) {
    // Phone codes travel on the inbox's SMS channel to the E.164 number.
    expect(found?.channel).toBe("sms");
    expect(found?.destination).toBe(destination);
  }
  const code = found?.code ?? "";
  expect(code).toMatch(/^\d{6}$/);
  return code;
}

async function readChallenge(response: Response) {
  expect(response.status()).toBe(202);
  const body = (await response.json()) as {
    challenge_id?: unknown;
    masked_destination?: unknown;
  };
  if (typeof body.challenge_id !== "string") {
    throw new Error("The challenge response is malformed.");
  }
  return {
    challengeId: body.challenge_id,
    maskedDestination:
      typeof body.masked_destination === "string"
        ? body.masked_destination
        : "",
  };
}

async function openDevice(browser: Browser) {
  const context = await browser.newContext({
    baseURL: webOrigin,
    locale: "vi-VN",
    reducedMotion: "reduce",
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
  // Step 1: email + consent -> "Get code".
  await form.getByRole("textbox", { name: labels.email }).fill(account.email);
  await form.getByRole("checkbox", { name: labels.terms }).check();
  const started = responseTo(page, "POST", "/v1/auth/signup/email/start");
  await form.getByRole("button", { name: labels.signUpGetCode }).click();
  const { challengeId } = await readChallenge(await started);
  const code = await waitForDevInboxCode(request, challengeId, "signup_email");
  // Step 2: the emailed code -> "Continue".
  await form.getByRole("textbox", { name: labels.signUpOtp }).fill(code);
  await form.getByRole("button", { name: labels.continue }).click();
  // Step 3: name + password -> "Create Account".
  await form
    .getByRole("textbox", { name: labels.fullName })
    .fill(account.displayName);
  await form.getByLabel(labels.password).fill(account.password);
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

async function logInWithPassword(page: Page, account: Account) {
  await page.goto("/sign-in");
  await page
    .getByRole("textbox", { name: labels.identifier })
    .fill(account.email);
  await page.getByLabel(labels.password).fill(account.password);
  const login = responseTo(page, "POST", "/v1/auth/password/login");
  await page.getByRole("button", { name: labels.logIn }).click();
  const response = await login;
  expect(response.status()).toBe(200);
  return (await response.json()) as { status: string; methods?: string[] };
}

async function meStatus(page: Page) {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  return response.status();
}

async function sessionCookieValues(context: BrowserContext) {
  return (await context.cookies())
    .filter((cookie) => /session/i.test(cookie.name))
    .map((cookie) => `${cookie.name}=${cookie.value}`)
    .sort();
}

function phoneCard(page: Page) {
  return page.getByRole("region", { name: labels.phoneCard });
}

function currentSessionRow(page: Page) {
  return page
    .getByRole("region", { name: labels.devicesCard })
    .locator("li.session-row", { hasText: labels.thisDevice });
}

/**
 * Settings → Add phone number → send a code → verify it. Returns the code so
 * the caller can prove it never reached Web Storage.
 */
async function addPhoneInSettings(
  page: Page,
  request: APIRequestContext,
  phone: TestPhone,
) {
  await page.goto("/app/settings");
  const card = phoneCard(page);
  await expect(card.getByText(labels.phoneEmpty)).toBeVisible();
  await card.getByRole("button", { name: labels.phoneAdd }).click();
  const form = card.getByRole("form", { name: labels.phoneAddForm });
  const number = form.getByRole("textbox", { name: labels.phoneMobile });
  await expect(number).toBeFocused();
  await number.fill(phone.display);
  const started = responseTo(page, "POST", "/v1/me/phone/start");
  await form
    .getByRole("button", { name: labels.phoneSendVerification })
    .click();
  const startResponse = await started;
  expect(startResponse.request().postDataJSON()).toMatchObject({
    country_code: "VN",
    phone_number: phone.compact,
  });
  const { challengeId, maskedDestination } = await readChallenge(startResponse);
  expect(maskedDestination).toBe(phone.masked);
  const code = await waitForDevInboxCode(
    request,
    challengeId,
    "phone_verify",
    phone.e164,
  );
  const codeField = form.getByRole("textbox", {
    name: labels.phoneSettingsCode,
  });
  await expect(codeField).toBeFocused();
  await codeField.fill(code);
  const verified = responseTo(page, "POST", "/v1/me/phone/verify");
  await form.getByRole("button", { name: labels.phoneVerifyNumber }).click();
  const response = await verified;
  expect(response.status()).toBe(200);
  expect(await response.json()).toMatchObject({
    phone: { country_code: "VN", masked_number: phone.masked, stale: false },
  });
  await expect(card.locator(".phone-row__number")).toHaveText(phone.masked);
  await expect(card.getByText(labels.phoneVerified)).toBeVisible();
  await expect(card.getByText(labels.phoneAdded)).toBeVisible();
  return code;
}

/**
 * UI-AUTH-04: "Log in with OTP", then the Phone channel (the email code is
 * on too, so the channel chooser shows).
 */
async function openPhoneMethod(page: Page) {
  await page.getByRole("button", { name: labels.useOtp }).click();
  const phoneChannel = page
    .getByRole("group", { name: labels.otpChannels })
    .getByRole("button", { name: labels.phoneChannel });
  await phoneChannel.click();
  await expect(phoneChannel).toHaveAttribute("aria-pressed", "true");
}

/**
 * Switches the visible login form to the phone method and requests a code.
 * Works on /sign-in (always persistent, no checkbox) and inside the
 * /account-link card (`optIn`: its "Keep me signed in" stays unchecked).
 */
async function requestPhoneSignInCode(
  page: Page,
  phone: TestPhone,
  { optIn = false, switchMethod = true } = {},
) {
  if (switchMethod) await openPhoneMethod(page);
  const form = page.getByRole("form", { name: labels.phoneForm });
  const number = form.getByRole("textbox", { name: labels.phoneNumber });
  if (switchMethod) await expect(number).toBeFocused();
  await expect(
    form.getByRole("combobox", { name: labels.phoneCountry }),
  ).toHaveValue("VN");
  // Nothing is remembered on the device; only /account-link keeps a choice.
  await expect(form.getByRole("checkbox")).toHaveCount(optIn ? 1 : 0);
  const keepSignedIn = !optIn;
  if (optIn) {
    await expect(
      form.getByRole("checkbox", { name: labels.keepSignedIn }),
    ).not.toBeChecked();
  }
  await number.fill(phone.display);
  const started = responseTo(page, "POST", "/v1/auth/phone/start");
  await form.getByRole("button", { name: labels.phoneSendCode }).click();
  const response = await started;
  expect(response.request().postDataJSON()).toMatchObject({
    country_code: "VN",
    keep_signed_in: keepSignedIn,
    phone_number: phone.compact,
  });
  const { challengeId, maskedDestination } = await readChallenge(response);
  expect(maskedDestination).toBe(phone.masked);
  await expect(form.getByText(phone.masked, { exact: false })).toBeVisible();
  await expect(form.getByText(labels.phoneExpiresIn)).toBeVisible();
  await expect(
    form.getByRole("textbox", { name: labels.phoneCode }),
  ).toBeFocused();
  // The 60-second resend cooldown starts with the code.
  await expect(
    form.getByRole("button", { name: labels.phoneResend }),
  ).toBeDisabled();
  return { challengeId, form };
}

/** Types the SMS code and submits it; returns the code and the answer. */
async function submitPhoneSignInCode(
  page: Page,
  request: APIRequestContext,
  form: Locator,
  challengeId: string,
  phone: TestPhone,
  { keepSignedIn = true } = {},
) {
  const code = await waitForDevInboxCode(
    request,
    challengeId,
    "phone_login",
    phone.e164,
  );
  await form.getByRole("textbox", { name: labels.phoneCode }).fill(code);
  const verified = responseTo(page, "POST", "/v1/auth/phone/verify");
  await form.getByRole("button", { name: labels.phoneVerify }).click();
  const response = await verified;
  expect(response.status()).toBe(200);
  expect(response.request().postDataJSON()).toMatchObject({
    challenge_id: challengeId,
    keep_signed_in: keepSignedIn,
  });
  const body = (await response.json()) as {
    methods?: string[];
    status: string;
  };
  return { body, code };
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

async function addPasskey(page: Page, label: string) {
  await page.goto("/app/settings");
  const card = page.getByRole("region", { name: labels.passkeysCard });
  await card.getByRole("button", { name: labels.addPasskey }).click();
  const form = card.getByRole("form", { name: labels.addPasskeyForm });
  await form.getByRole("textbox", { name: labels.passkeyName }).fill(label);
  const verify = responseTo(
    page,
    "POST",
    "/v1/me/passkeys/registration/verify",
  );
  await form.getByRole("button", { name: labels.createPasskey }).click();
  expect((await verify).status()).toBe(201);
  await expect(
    card.locator("li.passkey-row", { hasText: label }),
  ).toBeVisible();
}

/**
 * Settings → set up the authenticator app → confirm with the current code →
 * acknowledge the recovery codes. Returns the seed and the step it used.
 */
async function enableTotp(page: Page) {
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
  expect((await confirmed).status()).toBe(200);
  const dialog = page.getByRole("dialog", { name: labels.codesDialog });
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("list", { name: labels.codesList }).getByRole("listitem"),
  ).toHaveCount(10);
  await dialog.getByRole("checkbox", { name: labels.codesAck }).check();
  await dialog.getByRole("button", { name: labels.codesDone }).click();
  await expect(dialog).toBeHidden();
  return { secret, step };
}

async function chooseFakeGoogleAccount(page: Page, email: string) {
  await page.waitForURL(
    (url) => url.origin === googleOrigin && url.pathname === "/authorize",
  );
  await page.getByRole("textbox", { name: "Test email" }).fill(email);
  await page
    .getByRole("button", { name: "Sign in with custom account" })
    .click();
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
  expect(stored).not.toMatch(/auth_tx|link_tx|tutorhub_session|csrf/i);
  for (const secret of secrets) {
    if (secret) expect(stored).not.toContain(secret);
  }
}

function lastMethodProblem() {
  return JSON.stringify({
    type: "about:blank",
    title: "Phone number cannot be removed",
    status: 409,
    code: "auth_last_identity",
    detail: "Keep at least one sign-in method on your account.",
  });
}

/* ---------- Scenarios ---------- */

test.describe("AUTH-10 phone OTP", () => {
  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "Phone OTP runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      mfa_totp_enabled: true,
      passkeys_enabled: true,
      password_enabled: true,
      phone_allowed_countries: ["VN"],
      phone_enabled: true,
      signup_enabled: true,
    });
  });

  test("1. adds a mobile number in Settings (landline refused), then signs in with an SMS code and keep-signed-in", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newAccount("settings");
    const phone = newPhone();
    const secrets: string[] = [account.password, ...phoneSecrets(phone)];
    const { context, page } = await openDevice(browser);
    try {
      await test.step("sign up and open the empty phone card", async () => {
        secrets.push(await signUp(page, request, account));
        await page.goto("/app/settings");
        const card = phoneCard(page);
        await expect(card.getByText(labels.phoneEmpty)).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          phoneCardSelector,
          "phone card (empty)",
        );
      });

      await test.step("a landline is refused with a friendly 400", async () => {
        const card = phoneCard(page);
        await card.getByRole("button", { name: labels.phoneAdd }).click();
        const form = card.getByRole("form", { name: labels.phoneAddForm });
        await form
          .getByRole("textbox", { name: labels.phoneMobile })
          .fill("024 3822 1234");
        const refused = responseTo(page, "POST", "/v1/me/phone/start");
        await form
          .getByRole("button", { name: labels.phoneSendVerification })
          .click();
        const response = await refused;
        expect(response.status()).toBe(400);
        expect(await response.json()).toMatchObject({
          code: "auth_invalid_phone_number",
        });
        await expect(form.getByText(labels.phoneInvalid)).toBeVisible();
        await expect(
          form.getByRole("textbox", { name: labels.phoneMobile }),
        ).toBeFocused();
        await expectNoBlockingAxeViolations(
          page,
          phoneCardSelector,
          "phone card (landline refused)",
        );
        // Start over from the empty card with a mobile number.
        await form.getByRole("button", { name: /^(?:Hủy|Cancel)$/ }).click();
      });

      await test.step("add a mobile number through the SMS code", async () => {
        secrets.push(await addPhoneInSettings(page, request, phone));
        await expectNoBlockingAxeViolations(
          page,
          phoneCardSelector,
          "phone card (verified)",
        );
        // The security overview and the sign-in methods show the number.
        await expect(
          page.getByRole("region", { name: labels.securityCard }),
        ).toContainText(phone.masked);
        await expectBrowserStorageWithoutSecrets(page, secrets);
      });

      await test.step("sign out, then sign in with the phone (always persistent)", async () => {
        await signOut(page);
        await page.goto("/sign-in");
        const { challengeId, form } = await requestPhoneSignInCode(page, phone);
        await expectNoBlockingAxeViolations(
          page,
          ".sign-in__panel",
          "/sign-in phone code step",
        );
        const { body, code } = await submitPhoneSignInCode(
          page,
          request,
          form,
          challengeId,
          phone,
        );
        secrets.push(code);
        expect(body).toMatchObject({ status: "authenticated" });
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await meStatus(page)).toBe(200);
      });

      await test.step("Settings labels this device with the phone method", async () => {
        await page.goto("/app/settings");
        const current = currentSessionRow(page);
        await expect(current).toBeVisible();
        await expect(current).toContainText(labels.phoneMethod);
        await expect(current).toContainText(labels.persistent);
        await expectBrowserStorageWithoutSecrets(page, secrets);
      });
    } finally {
      await context.close();
    }
  });

  test("2. a passkey-only account finishes a phone sign-in with its passkey, and an AAL1 session steps up to AAL2 to remove the number", async ({
    browser,
    request,
  }) => {
    test.setTimeout(300_000);
    const account = newAccount("passkey");
    const phone = newPhone();
    const secrets: string[] = [account.password, ...phoneSecrets(phone)];
    const { context, page } = await openDevice(browser);
    try {
      const authenticator = await addVirtualAuthenticator(page);

      await test.step("sign up, add the number, then register a passkey", async () => {
        secrets.push(await signUp(page, request, account));
        secrets.push(await addPhoneInSettings(page, request, phone));
        await addPasskey(page, "AUTH10 key");
      });

      await test.step("the phone code asks for the passkey (mfa_required: passkey)", async () => {
        // Keep the background autofill pending so it never signs in alone.
        await setPresence(authenticator, false);
        await signOut(page);
        await page.goto("/sign-in");
        const { challengeId, form } = await requestPhoneSignInCode(page, phone);
        const { body, code } = await submitPhoneSignInCode(
          page,
          request,
          form,
          challengeId,
          phone,
        );
        secrets.push(code);
        expect(body).toMatchObject({ status: "mfa_required" });
        // ADR-0039 §4.3: a passkey-only account still needs its passkey.
        expect(body.methods).toEqual(["passkey"]);
        expect(await meStatus(page)).toBe(401);

        const step = page.getByRole("region", { name: labels.mfaStep });
        await expect(step).toBeVisible();
        await setPresence(authenticator, true);
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaPasskey }).click();
        expect((await verified).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await meStatus(page)).toBe(200);
      });

      await test.step("the device list shows the phone code and the passkey", async () => {
        await page.goto("/app/settings");
        const current = currentSessionRow(page);
        await expect(current).toContainText(labels.phoneMethod);
        await expect(current).toContainText(labels.passkeyMethod);
      });

      await test.step("an AAL1 password session must step up with the passkey to remove the number", async () => {
        await setPresence(authenticator, false);
        await signOut(page);
        expect(await logInWithPassword(page, account)).toMatchObject({
          status: "authenticated",
        });
        await expect(page).toHaveURL(/\/app\/home$/);

        await page.goto("/app/settings");
        const card = phoneCard(page);
        await expect(card.locator(".phone-row__number")).toHaveText(
          phone.masked,
        );
        await card.getByRole("button", { name: labels.phoneRemove }).click();
        const dialog = page.getByRole("dialog", {
          name: labels.phoneRemoveDialog,
        });
        await expect(dialog).toBeVisible();
        const refused = responseTo(page, "DELETE", "/v1/me/phone");
        await dialog
          .getByRole("button", { name: labels.phoneRemoveConfirm })
          .click();
        const needsAal2 = await refused;
        expect(needsAal2.status()).toBe(403);
        expect(await needsAal2.json()).toMatchObject({
          code: "auth_assurance_required",
        });

        const stepUp = page.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(stepUp).toBeVisible();
        // Only AAL2 factors are offered, never an SMS or an emailed code.
        await expect(
          stepUp.getByRole("button", { name: labels.stepUpEmailMethod }),
        ).toHaveCount(0);
        await expect(stepUp).not.toContainText(phone.masked);
        await setPresence(authenticator, true);
        const reauth = responseTo(page, "POST", "/v1/auth/reauth");
        const removed = responseTo(page, "DELETE", "/v1/me/phone");
        await stepUp
          .getByRole("button", { name: labels.stepUpPasskeyConfirm })
          .click();
        expect((await reauth).status()).toBe(200);
        expect((await removed).status()).toBe(204);
        await expect(stepUp).toBeHidden();
        await expect(card.getByText(labels.phoneRemoved)).toBeVisible();
        await expect(card.getByText(labels.phoneEmpty)).toBeVisible();
        expect(await meStatus(page)).toBe(200);
        await expectBrowserStorageWithoutSecrets(page, secrets);
      });
    } finally {
      await context.close();
    }
  });

  test("3. a TOTP account signs in by phone on /sign-in and /account-link, then removes the number (last-method guard first)", async ({
    browser,
    request,
  }) => {
    test.setTimeout(360_000);
    const account = newAccount("totp");
    const phone = newPhone();
    const secrets: string[] = [account.password, ...phoneSecrets(phone)];
    const contexts: BrowserContext[] = [];
    try {
      const owner = await openDevice(browser);
      contexts.push(owner.context);
      let lastStep = 0;
      let totpSecret = "";

      await test.step("sign up, add the number and turn on the authenticator app", async () => {
        secrets.push(await signUp(owner.page, request, account));
        secrets.push(await addPhoneInSettings(owner.page, request, phone));
        const enrollment = await enableTotp(owner.page);
        totpSecret = enrollment.secret;
        lastStep = enrollment.step;
        secrets.push(totpSecret);
      });

      await test.step("a phone sign-in continues with the authenticator code", async () => {
        const page = owner.page;
        await signOut(page);
        await page.goto("/sign-in");
        const { challengeId, form } = await requestPhoneSignInCode(page, phone);
        const { body, code } = await submitPhoneSignInCode(
          page,
          request,
          form,
          challengeId,
          phone,
        );
        secrets.push(code);
        expect(body.status).toBe("mfa_required");
        expect(body.methods).toEqual(
          expect.arrayContaining(["totp", "recovery_code"]),
        );
        const step = page.getByRole("region", { name: labels.mfaStep });
        await expect(step).toBeVisible();
        const totp = await freshTotp(page, totpSecret, lastStep);
        lastStep = totp.step;
        await step
          .getByRole("textbox", { name: labels.mfaTotpField })
          .fill(totp.code);
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        expect((await verified).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await meStatus(page)).toBe(200);
      });

      const linking = await openDevice(browser);
      contexts.push(linking.context);
      const page = linking.page;

      await test.step("on /account-link the phone code and its second step stay to confirm the link", async () => {
        await page.goto("/sign-in");
        await page.getByRole("button", { name: labels.google }).click();
        await chooseFakeGoogleAccount(page, account.email);
        await expect(page).toHaveURL(/\/account-link\?provider=google$/);
        await expect(
          page.getByRole("heading", { name: labels.linkSignIn }),
        ).toBeVisible();
        expect(await meStatus(page)).toBe(401);

        const { challengeId, form } = await requestPhoneSignInCode(
          page,
          phone,
          {
            optIn: true,
          },
        );
        await expectNoBlockingAxeViolations(
          page,
          "main",
          "/account-link phone code step",
        );
        const { body, code } = await submitPhoneSignInCode(
          page,
          request,
          form,
          challengeId,
          phone,
          { keepSignedIn: false },
        );
        secrets.push(code);
        expect(body.status).toBe("mfa_required");
        const step = page.getByRole("region", { name: labels.mfaStep });
        await expect(step).toBeVisible();
        const totp = await freshTotp(page, totpSecret, lastStep);
        lastStep = totp.step;
        await step
          .getByRole("textbox", { name: labels.mfaTotpField })
          .fill(totp.code);
        const verified = responseTo(page, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        expect((await verified).status()).toBe(200);

        const heading = page.getByRole("heading", {
          name: labels.linkConfirmHeading,
        });
        await expect(heading).toBeFocused();
        await expect(page).toHaveURL(/\/account-link\?provider=google$/);
        const confirmed = responseTo(
          page,
          "POST",
          "/v1/auth/link-intent/confirm",
        );
        await page.getByRole("button", { name: labels.linkConfirm }).click();
        expect((await confirmed).status()).toBe(200);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await meStatus(page)).toBe(200);
      });

      await test.step("removing the number is refused while it is the last method (409), then rotates the session", async () => {
        await page.goto("/app/settings");
        const card = phoneCard(page);
        await expect(card.locator(".phone-row__number")).toHaveText(
          phone.masked,
        );

        // The UI contract for 409 auth_last_identity (the server rule has
        // its own AUTH-10A tests; this account still has other methods).
        let injected = false;
        await page.route("**/api/v1/me/phone", async (route) => {
          if (injected || route.request().method() !== "DELETE") {
            await route.fallback();
            return;
          }
          injected = true;
          await route.fulfill({
            body: lastMethodProblem(),
            contentType: "application/problem+json",
            status: 409,
          });
        });
        await card.getByRole("button", { name: labels.phoneRemove }).click();
        const dialog = page.getByRole("dialog", {
          name: labels.phoneRemoveDialog,
        });
        await dialog
          .getByRole("button", { name: labels.phoneRemoveConfirm })
          .click();
        await expect(dialog).toBeHidden();
        await expect(card.getByRole("alert")).toContainText(
          labels.phoneLastMethod,
        );
        await expect(card.locator(".phone-row__number")).toHaveText(
          phone.masked,
        );
        expect(injected).toBe(true);
        await page.unroute("**/api/v1/me/phone");

        const before = await sessionCookieValues(linking.context);
        await card.getByRole("button", { name: labels.phoneRemove }).click();
        await expect(dialog).toBeVisible();
        const removed = responseTo(page, "DELETE", "/v1/me/phone");
        await dialog
          .getByRole("button", { name: labels.phoneRemoveConfirm })
          .click();
        expect((await removed).status()).toBe(204);
        await expect(card.getByText(labels.phoneRemoved)).toBeVisible();
        await expect(card.getByText(labels.phoneEmpty)).toBeVisible();
        await expect(card.getByRole("alert")).toHaveCount(0);
        // The server rotated this session (and kept it signed in).
        expect(await meStatus(page)).toBe(200);
        expect(await sessionCookieValues(linking.context)).not.toEqual(before);
        await expectBrowserStorageWithoutSecrets(page, secrets);
      });
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("4. the phone sign-in screens are accessible, account-neutral and keep Web Storage clean", async ({
    browser,
  }) => {
    test.setTimeout(180_000);
    // Never attached to an account: the answers must look the same anyway.
    const phone = newPhone();
    const { context, page } = await openDevice(browser);
    try {
      await page.goto("/sign-in");
      await openPhoneMethod(page);
      const form = page.getByRole("form", { name: labels.phoneForm });
      const number = form.getByRole("textbox", { name: labels.phoneNumber });
      await expect(number).toBeFocused();
      await expect(number).toHaveAttribute("type", "tel");
      await expect(number).toHaveAttribute("inputmode", "tel");
      await expect(number).toHaveAttribute("autocomplete", "tel");
      await expectNoBlockingAxeViolations(
        page,
        ".sign-in__panel",
        "/sign-in phone number step",
      );

      // An empty number is caught before any request.
      await form.getByRole("button", { name: labels.phoneSendCode }).click();
      await expect(form.getByText(labels.phoneRequired)).toBeVisible();
      await expect(number).toBeFocused();
      await expectNoBlockingAxeViolations(
        page,
        ".sign-in__panel",
        "/sign-in phone number error",
      );

      const { form: codeForm } = await requestPhoneSignInCode(page, phone, {
        switchMethod: false,
      });
      const codeField = codeForm.getByRole("textbox", {
        name: labels.phoneCode,
      });
      await expect(codeField).toHaveAttribute("inputmode", "numeric");
      await expect(codeField).toHaveAttribute("autocomplete", "one-time-code");
      await expectNoBlockingAxeViolations(
        page,
        ".sign-in__panel",
        "/sign-in phone code step",
      );

      // Letters are dropped; a wrong code gets one neutral answer.
      await codeField.fill("12ab34");
      await expect(codeField).toHaveValue("1234");
      await codeField.fill("000000");
      const verified = responseTo(page, "POST", "/v1/auth/phone/verify");
      await codeForm.getByRole("button", { name: labels.phoneVerify }).click();
      const response = await verified;
      expect(response.status()).toBe(401);
      expect(await response.json()).toMatchObject({
        code: "auth_invalid_verification_code",
      });
      await expect(codeForm.getByRole("alert")).toContainText(
        labels.phoneCodeInvalid,
      );
      // The submitted code left the field (and the page state) at once.
      await expect(codeField).toHaveValue("");
      await expect(codeField).toBeFocused();
      await expectNoBlockingAxeViolations(
        page,
        ".sign-in__panel",
        "/sign-in phone code refused",
      );
      expect(await meStatus(page)).toBe(401);

      // "Change phone number" goes back with the typed number kept.
      await codeForm.getByRole("button", { name: labels.phoneChange }).click();
      await expect(number).toBeFocused();
      await expect(number).toHaveValue(phone.display);

      // Back to the password form: the identifier field is focused.
      await page.getByRole("button", { name: labels.usePassword }).click();
      await expect(
        page.getByRole("textbox", { name: labels.identifier }),
      ).toBeFocused();
      await expectBrowserStorageWithoutSecrets(page, phoneSecrets(phone));
    } finally {
      await context.close();
    }
  });
});
