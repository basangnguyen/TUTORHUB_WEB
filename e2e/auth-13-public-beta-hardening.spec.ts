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
import { createSocket, type Socket } from "node:dgram";

/**
 * AUTH-13B: the public-beta cross-method gate, on the loopback stack only
 * (`node scripts/e2e-local.mjs serve --native-auth`, started by
 * playwright.auth.config.ts). One school member owns every first-party
 * method — password, passkey, a +84 phone and an authenticator app — before
 * the school's org_admin turns `enforce_sso` on. Then:
 *
 * - a passkey or a phone code on another device is refused with 403
 *   `auth_sso_required` (AUTH-13A) and the page routes to school SSO;
 * - school SSO links the account (no /account-link detour), revokes the
 *   older sessions (`identity_linked`) and still asks for the second step
 *   before an AAL2 session exists, which then passes a Settings step-up;
 * - every alert, the second step and Settings pass axe (no serious or
 *   critical violation) and nothing sensitive reaches Web Storage,
 *   script-visible cookies or the address bar.
 *
 * Google is the loopback fake issuer on 127.0.0.1:9092 (it signs the
 * Workspace `hd` claim of a custom account). DNS TXT lookups go to
 * AUTH_E2E_DNS_UDP_ADDR=127.0.0.1:9095, answered here from memory; the
 * socket is released in `afterAll` so auth-12 and auth-13 share one worker.
 * SMS and email codes come from `/api/v1/dev/auth-inbox`. Passkeys live in
 * Chromium CDP virtual authenticators; the second device receives a copy of
 * the first device's credential. Each run uses its own random `.test`
 * domain, addresses and 0912xxxxxx number.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://localhost:5173";
const googleOrigin = "http://127.0.0.1:9092";
const dnsAddress = { host: "127.0.0.1", port: 9095 } as const;
const nativeAuthProject = "local-chromium-native-auth";
const blockingImpacts = new Set(["serious", "critical"]);

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
  // School SSO (/sign-in and the workspace panel).
  schoolSSO: /^(?:Đăng nhập bằng SSO trường học|Sign in with school SSO)$/,
  continueGoogle:
    /^(?:Tiếp tục với Google của trường|Continue with school Google)$/,
  ssoRequiredAccount:
    /(?:Tài khoản này thuộc trường học bắt buộc đăng nhập qua SSO trường học|This account belongs to a school that requires school SSO)/,
  panel: /^(?:SSO trường học|School SSO)$/,
  newConnection: /^(?:Tạo kết nối mới|New connection)$/,
  hdField:
    /^(?:Tên miền tổ chức Google Workspace \(hd\)|Google Workspace organization domains \(hd\))$/,
  createConnection: /^(?:Tạo kết nối|Create connection)$/,
  domainField: /^(?:Tên miền email của trường|School email domain)$/,
  addDomain: /^(?:Khai báo tên miền|Add domain)$/,
  txtValue: /^(?:Giá trị TXT|TXT value)$/,
  verified: /^(?:Đã xác minh|Verified)$/,
  enable: /^(?:Bật kết nối|Enable connection)$/,
  enabled: /^(?:Đang bật|Enabled)$/,
  enforce:
    /^(?:Bắt buộc đăng nhập qua SSO trường học cho các tên miền đã xác minh|Require school SSO sign-in for verified domains)$/,
  enforceDialog: /^(?:Bắt buộc SSO trường học\?|Require school SSO\?)$/,
  enforceConfirm: /^(?:Bắt buộc SSO|Require SSO)$/,
  enforcedBadge: /^(?:Bắt buộc SSO|SSO required)$/,
  identityCard: /^(?:Danh tính đăng nhập|Sign-in identities)$/,
  identityLinkGoogle: /^(?:Liên kết Google|Link Google)$/,
  identityLinked:
    /^(?:Đã liên kết danh tính đăng nhập\.|Sign-in identity linked\.)$/,
  // Passkeys.
  passkeySignIn: /^(?:Đăng nhập bằng Passkey|Sign in with a passkey)$/,
  passkeysCard: /^(?:Passkey|Passkeys)$/,
  addPasskey: /^(?:Thêm passkey|Add a passkey)$/,
  addPasskeyForm: /^(?:Thêm passkey mới|Add a new passkey)$/,
  passkeyName: /^(?:Tên passkey|Passkey name)$/,
  createPasskey: /^(?:Tạo passkey|Create passkey)$/,
  // Phone (Settings card and sign-in form).
  usePhone: /^(?:Đăng nhập bằng số điện thoại|Log in with a phone number)$/,
  phoneForm:
    /^(?:Đăng nhập bằng mã gửi tới số điện thoại|Log in with a code sent to your phone)$/,
  phoneCountry: /^(?:Mã quốc gia|Country code)$/,
  phoneNumber: /^(?:Số điện thoại|Phone number)$/,
  phoneSendCode: /^(?:Gửi mã|Send code)$/,
  phoneCode: /^(?:Mã gồm 6 chữ số|6-digit code)$/,
  phoneVerify: /^(?:Xác minh và đăng nhập|Verify and log in)$/,
  phoneCard: /^(?:Số điện thoại|Phone number)$/,
  phoneEmpty: /^(?:Chưa có số điện thoại|No phone number yet)$/,
  phoneAdd: /^(?:Thêm số điện thoại|Add phone number)$/,
  phoneAddForm: /^(?:Thêm số điện thoại mới|Add a new phone number)$/,
  phoneMobile: /^(?:Số di động|Mobile number)$/,
  phoneSendVerification: /^(?:Gửi mã xác minh|Send verification code)$/,
  phoneSettingsCode: /^(?:Mã xác minh 6 số|6-digit verification code)$/,
  phoneVerifyNumber: /^(?:Xác minh số|Verify number)$/,
  phoneVerified: /^(?:Đã xác minh|Verified)$/,
  // Authenticator app (TOTP) and recovery codes.
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
  regenerate: /^(?:Tạo bộ mã khôi phục mới|Generate new recovery codes)$/,
  regenerateDialog:
    /^(?:Tạo bộ mã khôi phục mới\?|Generate new recovery codes\?)$/,
  regenerateConfirm: /^(?:Tạo mã mới|Generate new codes)$/,
  // Second step and step-up.
  mfaStep: /^(?:Xác minh hai bước|Two-step verification)$/,
  mfaTotpField:
    /^(?:Mã 6 chữ số từ ứng dụng xác thực|6-digit code from your authenticator app)$/,
  mfaVerify: /^(?:Xác minh|Verify)$/,
  stepUpDialog: /^(?:Xác nhận đó là bạn|Confirm it's you)$/,
  stepUpPasskeyConfirm: /^(?:Xác nhận bằng passkey|Confirm with passkey)$/,
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

/* ---------------------------------------------------------- DNS TXT */

/** Published TXT records by lower-case owner name without the root dot. */
const txtRecords = new Map<string, string[]>();
let dnsServer: Socket | undefined;

function readQuestion(message: Buffer) {
  if (message.length < 12 || message.readUInt16BE(4) !== 1) return null;
  const labelsRead: string[] = [];
  let offset = 12;
  for (;;) {
    const length = message[offset];
    if (length === undefined || length > 63) return null;
    offset += 1;
    if (length === 0) break;
    labelsRead.push(
      message.subarray(offset, offset + length).toString("latin1"),
    );
    offset += length;
  }
  if (offset + 4 > message.length) return null;
  return {
    name: labelsRead.join(".").toLowerCase(),
    raw: message.subarray(12, offset + 4),
    type: message.readUInt16BE(offset),
  };
}

/** NXDOMAIN for an unpublished name; TXT answers otherwise (AA, RA set). */
function dnsAnswer(query: Buffer) {
  const question = readQuestion(query);
  if (!question) return null;
  const values = txtRecords.get(question.name);
  const answers = values && question.type === 16 ? values : [];
  const header = Buffer.alloc(12);
  header.writeUInt16BE(query.readUInt16BE(0), 0);
  const recursionDesired = query.readUInt16BE(2) & 0x0100;
  header.writeUInt16BE(
    0x8000 | 0x0400 | recursionDesired | 0x0080 | (values ? 0 : 3),
    2,
  );
  header.writeUInt16BE(1, 4);
  header.writeUInt16BE(answers.length, 6);
  const records = answers.map((value) => {
    const text = Buffer.from(value, "utf8");
    const resource = Buffer.alloc(12);
    resource.writeUInt16BE(0xc00c, 0); // pointer to the question name
    resource.writeUInt16BE(16, 2); // TXT
    resource.writeUInt16BE(1, 4); // IN
    resource.writeUInt32BE(1, 6); // TTL
    resource.writeUInt16BE(text.length + 1, 10);
    return Buffer.concat([resource, Buffer.from([text.length]), text]);
  });
  return Buffer.concat([header, question.raw, ...records]);
}

function bindDNSResponder() {
  return new Promise<Socket>((resolve, reject) => {
    const socket = createSocket("udp4");
    socket.on("message", (message, peer) => {
      const reply = dnsAnswer(message);
      if (reply) socket.send(reply, peer.port, peer.address);
    });
    socket.once("error", (error) => {
      socket.close();
      reject(error);
    });
    socket.bind(dnsAddress.port, dnsAddress.host, () => {
      socket.removeAllListeners("error");
      resolve(socket);
    });
  });
}

/**
 * Binds 127.0.0.1:9095. auth-12 releases the port in its own `afterAll`;
 * a short retry covers the moment between the two files in one worker.
 */
async function startDNSResponder() {
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await bindDNSResponder();
    } catch (error) {
      const code = (error as NodeJS.ErrnoException).code;
      if (code !== "EADDRINUSE" || attempt >= 40) throw error;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
}

function publishTXT(name: string, value: string) {
  txtRecords.set(name.toLowerCase().replace(/\.$/, ""), [value]);
}

/* ---------------------------------------------------------- helpers */

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
  compact: string;
  display: string;
  e164: string;
  masked: string;
}

interface VirtualAuthenticator {
  authenticatorId: string;
  cdp: CDPSession;
}

interface PasskeyProbe {
  gets: { mediation: string; outcome: string }[];
}

declare global {
  interface Window {
    __auth13PasskeyProbe?: PasskeyProbe;
  }
}

/** Records `navigator.credentials.get` calls (mediation + outcome) only. */
function installPasskeyProbe() {
  const probe: PasskeyProbe = { gets: [] };
  window.__auth13PasskeyProbe = probe;
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
}

async function pendingAutofillRequests(page: Page) {
  const probe = await page.evaluate(
    () => window.__auth13PasskeyProbe ?? { gets: [] },
  );
  return probe.gets.filter(
    (call) => call.mediation === "conditional" && call.outcome === "pending",
  ).length;
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

/** Every spelling of a number that must never reach the browser's stores. */
function phoneSecrets(phone: TestPhone) {
  return [phone.compact, phone.e164, phone.display, phone.e164.slice(1)];
}

function apiPath(url: string) {
  return new URL(url).pathname;
}

function responseTo(page: Page, method: string, match: string | RegExp) {
  return page.waitForResponse((response) => {
    if (response.request().method() !== method) return false;
    const pathname = apiPath(response.url());
    return typeof match === "string"
      ? pathname.endsWith(match)
      : match.test(pathname);
  });
}

async function problemCode(response: Response) {
  const body = (await response.json()) as { code?: unknown };
  return typeof body.code === "string" ? body.code : undefined;
}

async function expectNoBlockingAxeViolations(
  page: Page,
  label: string,
  include = "main",
) {
  const results = await new AxeBuilder({ page }).include(include).analyze();
  const blocking = results.violations
    .filter((violation) => blockingImpacts.has(violation.impact ?? ""))
    .map(
      (violation) =>
        `${label}: ${violation.id} (${violation.impact}) on ${violation.nodes.length} node(s)`,
    );
  expect(blocking).toEqual([]);
}

async function sessionCookieValues(context: BrowserContext) {
  return (await context.cookies())
    .filter((cookie) => /session|csrf/i.test(cookie.name))
    .map((cookie) => cookie.value)
    .filter(Boolean);
}

/** HttpOnly transaction cookies the server must have cleared. */
async function transactionCookieNames(context: BrowserContext) {
  return (await context.cookies())
    .filter((cookie) => /auth_tx/i.test(cookie.name))
    .map((cookie) => cookie.name);
}

/**
 * Web Storage, script-visible cookies and the address bar hold none of the
 * secrets (OTPs, numbers, school emails, TOTP seed, recovery codes, TXT
 * value, passwords) nor a session or CSRF token.
 */
async function expectBrowserPrivacy(page: Page, secrets: readonly string[]) {
  const stored = await page.evaluate(() =>
    JSON.stringify({
      cookie: document.cookie,
      local: { ...window.localStorage },
      session: { ...window.sessionStorage },
    }),
  );
  expect(stored).not.toMatch(
    /auth_tx|link_tx|registration_tx|tutorhub_session|csrf|otpauth|tutorhub-site-verification/i,
  );
  const tokens = await sessionCookieValues(page.context());
  const address = new URL(page.url());
  expect(address.search).toBe("");
  expect(address.hash).toBe("");
  for (const secret of [...secrets, ...tokens]) {
    if (!secret) continue;
    expect(stored).not.toContain(secret);
    expect(page.url()).not.toContain(secret);
    expect(page.url()).not.toContain(encodeURIComponent(secret));
  }
}

async function openDevice(browser: Browser, { probePasskeys = false } = {}) {
  const context = await browser.newContext({
    baseURL: webOrigin,
    locale: "vi-VN",
    reducedMotion: "reduce",
  });
  if (probePasskeys) await context.addInitScript(installPasskeyProbe);
  return { context, page: await context.newPage() };
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
  const { challengeId } = await readChallenge(await started);
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
    .fill(`AUTH13 School ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth13-${suffix}`);
  const created = responseTo(page, "POST", "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

interface MeSnapshot {
  email?: string;
  status: number;
}

async function me(page: Page): Promise<MeSnapshot> {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  if (response.status() !== 200) return { status: response.status() };
  const body = (await response.json()) as { user?: { email?: string } };
  return { email: body.user?.email, status: 200 };
}

/** Settings → add the mobile number → verify the SMS code. */
async function addPhoneInSettings(
  page: Page,
  request: APIRequestContext,
  phone: TestPhone,
) {
  await page.goto("/app/settings");
  const card = page.getByRole("region", { name: labels.phoneCard });
  await expect(card.getByText(labels.phoneEmpty)).toBeVisible();
  await card.getByRole("button", { name: labels.phoneAdd }).click();
  const form = card.getByRole("form", { name: labels.phoneAddForm });
  await form
    .getByRole("textbox", { name: labels.phoneMobile })
    .fill(phone.display);
  const started = responseTo(page, "POST", "/v1/me/phone/start");
  await form
    .getByRole("button", { name: labels.phoneSendVerification })
    .click();
  const { challengeId } = await readChallenge(await started);
  const code = await waitForDevInboxCode(
    request,
    challengeId,
    "phone_verify",
    phone.e164,
  );
  await form
    .getByRole("textbox", { name: labels.phoneSettingsCode })
    .fill(code);
  const verified = responseTo(page, "POST", "/v1/me/phone/verify");
  await form.getByRole("button", { name: labels.phoneVerifyNumber }).click();
  expect((await verified).status()).toBe(200);
  await expect(card.getByText(labels.phoneVerified)).toBeVisible();
  return code;
}

/**
 * Settings → authenticator app → confirm with a fresh code → keep the ten
 * recovery codes. The confirmation rotates this session to AAL2.
 */
async function enableTotp(page: Page) {
  await page.goto("/app/settings");
  const card = page.getByRole("region", { name: labels.totpCard });
  await expect(card.getByText(labels.statusOff)).toBeVisible();
  const enrolled = responseTo(page, "POST", "/v1/me/mfa/totp/enroll");
  await card.getByRole("button", { name: labels.setupTotp }).click();
  const enrollResponse = await enrolled;
  expect(enrollResponse.status()).toBe(200);
  const { otpauth_uri: otpauthURI, secret } = (await enrollResponse.json()) as {
    otpauth_uri: string;
    secret: string;
  };
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
  return { code, codes, otpauthURI, secret, step };
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
 * Opens /sign-in with automatic presence off, so the autofill (conditional)
 * request stays pending; presence comes back on before the button is used.
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
  await expect.poll(() => pendingAutofillRequests(page)).toBe(1);
  await setPresence(authenticator, true);
}

/** Signs in on the fake Google chooser as a custom (Workspace) account. */
async function chooseFakeGoogleAccount(
  page: Page,
  email: string,
  hostedDomain: string,
) {
  await page.waitForURL(
    (url) => url.origin === googleOrigin && url.pathname === "/authorize",
  );
  await page.getByRole("textbox", { name: "Test email" }).fill(email);
  await page
    .getByRole("textbox", { name: "Hosted domain (hd, optional)" })
    .fill(hostedDomain);
  await page
    .getByRole("button", { name: "Sign in with custom account" })
    .click();
}

/** Runs HRD from /sign-in for `email` and returns the discovery answer. */
async function discoverSchool(page: Page, email: string) {
  await page.goto("/sign-in");
  await page.getByRole("textbox", { name: labels.identifier }).fill(email);
  const discovered = responseTo(page, "POST", "/v1/auth/identifier/discover");
  await page.getByRole("button", { name: labels.schoolSSO }).click();
  const response = await discovered;
  expect(response.status()).toBe(200);
  return (await response.json()) as unknown;
}

/** Leaves for the school's Google with login_hint, domain and `hd`. */
async function continueWithSchoolGoogle(
  page: Page,
  email: string,
  domain: string,
) {
  const started = page.waitForRequest(
    (request) => apiPath(request.url()) === "/api/v1/auth/oauth/google/start",
  );
  await page.getByRole("button", { name: labels.continueGoogle }).click();
  const request = await started;
  expect(request.isNavigationRequest()).toBe(true);
  const url = new URL(request.url());
  expect(url.searchParams.get("login_hint")).toBe(email);
  expect(url.searchParams.get("domain")).toBe(domain);
  expect(url.searchParams.get("intent")).toBe("login");
  await page.waitForURL(
    (target) =>
      target.origin === googleOrigin && target.pathname === "/authorize",
  );
  expect(new URL(page.url()).searchParams.get("hd")).toBe(domain);
}

function panelOf(page: Page) {
  return page.getByRole("region", { name: labels.panel });
}

function connectionCard(panel: Locator) {
  return panel.getByRole("article");
}

function recentAuthRequired() {
  return JSON.stringify({
    type: "about:blank",
    title: "Recent authentication required",
    status: 403,
    code: "auth_recent_authentication_required",
    detail: "Verify your identity before changing recovery codes.",
  });
}

/* ---------------------------------------------------------- scenario */

const runID = randomBytes(5).toString("hex");
const schoolDomain = `auth13-${runID}.test`;
const txtOwner = `_tutorhub-challenge.${schoolDomain}.`;
const admin: Account = {
  displayName: `AUTH13 Admin ${runID.slice(0, 4)}`,
  email: `auth13.admin.${runID}@${schoolDomain}`,
  password: `Harbor-${randomBytes(9).toString("base64url")}-Lantern7`,
};
const member: Account = {
  displayName: `AUTH13 Teacher ${runID.slice(0, 4)}`,
  email: `auth13.teacher.${runID}@${schoolDomain}`,
  password: `Harbor-${randomBytes(9).toString("base64url")}-Quartz7`,
};
const phone = newPhone();

test.describe.configure({ mode: "serial" });

test.describe("AUTH-13 public beta hardening across sign-in methods", () => {
  test.beforeAll(async () => {
    if (test.info().project.name !== nativeAuthProject) return;
    dnsServer = await startDNSResponder();
  });

  test.afterAll(async () => {
    const server = dnsServer;
    dnsServer = undefined;
    if (server) await new Promise<void>((resolve) => server.close(resolve));
    txtRecords.clear();
  });

  test.beforeEach(async ({ request }, testInfo) => {
    test.skip(
      testInfo.project.name !== nativeAuthProject,
      "AUTH-13 runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      device_management_enabled: true,
      mfa_totp_enabled: true,
      oauth_providers: expect.arrayContaining(["google"]),
      passkeys_enabled: true,
      password_enabled: true,
      phone_allowed_countries: ["VN"],
      phone_enabled: true,
      signup_enabled: true,
      sso_domains_enabled: true,
    });
  });

  test("a password + passkey + phone + TOTP account moves to enforced school SSO", async ({
    browser,
    request,
  }) => {
    test.setTimeout(900_000);
    const contexts: BrowserContext[] = [];
    const secrets: string[] = [
      member.email,
      member.password,
      admin.email,
      admin.password,
      ...phoneSecrets(phone),
    ];
    try {
      /* ----- Step 1: one account, every first-party method (Device A) ----- */
      const deviceA = await openDevice(browser);
      contexts.push(deviceA.context);
      const pageA = deviceA.page;
      const authenticatorA = await addVirtualAuthenticator(pageA);
      let totpSecret = "";
      let lastStep = 0;

      await test.step("Device A: sign up on the school domain, then add a phone, TOTP and a passkey", async () => {
        secrets.push(await signUp(pageA, request, member));
        // Phone first: AAL1 may add it while no passkey or TOTP exists.
        secrets.push(await addPhoneInSettings(pageA, request, phone));
        // TOTP confirmation rotates this session to AAL2 ...
        const enrollment = await enableTotp(pageA);
        totpSecret = enrollment.secret;
        lastStep = enrollment.step;
        secrets.push(
          enrollment.secret,
          enrollment.otpauthURI,
          enrollment.code,
          ...enrollment.codes,
        );
        // ... which a passkey registration needs once TOTP is active.
        await addPasskey(pageA, "AUTH13 key");
        expect(await me(pageA)).toEqual({ email: member.email, status: 200 });
        await expectBrowserPrivacy(pageA, secrets);
      });

      const { credentials } = await authenticatorA.cdp.send(
        "WebAuthn.getCredentials",
        { authenticatorId: authenticatorA.authenticatorId },
      );
      expect(credentials).toHaveLength(1);
      const memberCredential = credentials[0];
      if (!memberCredential) throw new Error("No passkey was registered.");

      /* ----- Step 2: the org_admin verifies the domain and enforces SSO ----- */
      let txtValue = "";
      await test.step("the org_admin verifies the domain over DNS, links the school account and enforces SSO", async () => {
        const owner = await openDevice(browser);
        contexts.push(owner.context);
        const page = owner.page;
        secrets.push(await signUp(page, request, admin));
        await createWorkspace(page, runID);

        await page.goto("/app/workspace");
        const panel = panelOf(page);
        await expect(panel).toBeVisible();
        const form = panel.getByRole("form", { name: labels.newConnection });
        await form
          .getByRole("textbox", { name: labels.hdField })
          .fill(schoolDomain);
        const created = responseTo(page, "POST", "/sso-connections");
        await form
          .getByRole("button", { name: labels.createConnection })
          .click();
        expect((await created).status()).toBe(201);
        await expect(connectionCard(panel)).toHaveCount(1);

        await panel
          .getByRole("textbox", { name: labels.domainField })
          .fill(schoolDomain);
        const declared = responseTo(page, "POST", "/sso-domains");
        await panel.getByRole("button", { name: labels.addDomain }).click();
        expect((await declared).status()).toBe(201);
        txtValue = await panel
          .getByRole("textbox", { name: labels.txtValue })
          .inputValue();
        expect(txtValue).toMatch(
          /^tutorhub-site-verification=[A-Za-z0-9_-]{20,}$/,
        );
        secrets.push(txtValue);

        publishTXT(txtOwner, txtValue);
        const verified = responseTo(page, "POST", "/verify");
        await panel
          .getByRole("button", {
            name: new RegExp(
              `^(?:Xác minh DNS|Verify DNS) ${schoolDomain.replace(/\./g, "\\.")}$`,
            ),
          })
          .click();
        const verifyResponse = await verified;
        expect(verifyResponse.status()).toBe(200);
        expect(await verifyResponse.json()).toMatchObject({
          domain: schoolDomain,
          status: "verified",
        });
        await expect(
          panel.getByText(labels.verified, { exact: true }),
        ).toBeVisible();

        const enabled = responseTo(page, "PATCH", /\/sso-connections\/[^/]+$/);
        await panel.getByRole("button", { name: labels.enable }).click();
        expect((await enabled).status()).toBe(200);
        await expect(
          connectionCard(panel).getByText(labels.enabled, { exact: true }),
        ).toBeVisible();

        // The lockout guard wants the admin's own school session first.
        await page.goto("/app/settings");
        const identityCard = page.getByRole("region", {
          name: labels.identityCard,
        });
        await identityCard
          .getByRole("button", { name: labels.identityLinkGoogle })
          .click();
        await chooseFakeGoogleAccount(page, admin.email, schoolDomain);
        await expect(
          identityCard.getByText(labels.identityLinked),
        ).toBeVisible();

        const school = await openDevice(browser);
        contexts.push(school.context);
        await discoverSchool(school.page, admin.email);
        await continueWithSchoolGoogle(school.page, admin.email, schoolDomain);
        await chooseFakeGoogleAccount(school.page, admin.email, schoolDomain);
        await expect(school.page).toHaveURL(/\/app\/home$/);

        await school.page.goto("/app/workspace");
        const schoolPanel = panelOf(school.page);
        await schoolPanel.getByRole("switch", { name: labels.enforce }).click();
        const dialog = school.page.getByRole("dialog", {
          name: labels.enforceDialog,
        });
        const enforced = responseTo(
          school.page,
          "PATCH",
          /\/sso-connections\/[^/]+$/,
        );
        await dialog
          .getByRole("button", { name: labels.enforceConfirm })
          .click();
        const enforceResponse = await enforced;
        expect(enforceResponse.status()).toBe(200);
        expect(await enforceResponse.json()).toMatchObject({
          enforce_sso: true,
        });
        await expect(
          schoolPanel.getByText(labels.enforcedBadge, { exact: true }),
        ).toBeVisible();
        await owner.context.close();
        await school.context.close();
      });

      // The member's Device A session predates enforcement and still works.
      expect((await me(pageA)).status).toBe(200);

      /* ----- Step 3: no first-factor bypass on Device B ----- */
      const deviceB = await openDevice(browser, { probePasskeys: true });
      contexts.push(deviceB.context);
      const pageB = deviceB.page;
      await pageB.goto("/signed-out");
      const authenticatorB = await addVirtualAuthenticator(pageB);
      await setPresence(authenticatorB, false);
      await authenticatorB.cdp.send("WebAuthn.addCredential", {
        authenticatorId: authenticatorB.authenticatorId,
        credential: {
          credentialId: memberCredential.credentialId,
          isResidentCredential: true,
          privateKey: memberCredential.privateKey,
          rpId: memberCredential.rpId ?? "localhost",
          signCount: memberCredential.signCount,
          userHandle: memberCredential.userHandle,
        },
      });

      await test.step("Device B: a passkey sign-in is refused with auth_sso_required and routes to school SSO", async () => {
        await openSignInWithPendingAutofill(pageB, authenticatorB);
        const verify = responseTo(
          pageB,
          "POST",
          "/v1/auth/passkeys/assertion/verify",
        );
        await pageB.getByRole("button", { name: labels.passkeySignIn }).click();
        const refused = await verify;
        expect(refused.status()).toBe(403);
        const body = await refused.text();
        expect(JSON.parse(body)).toMatchObject({ code: "auth_sso_required" });
        expect(body).not.toContain(member.email);

        const alert = pageB.getByRole("alert");
        await expect(alert).toHaveCount(1);
        await expect(alert).toContainText(labels.ssoRequiredAccount);
        await expect(alert).not.toContainText(member.email);
        await expect(
          pageB.getByRole("textbox", { name: labels.identifier }),
        ).toBeFocused();
        await expect(
          pageB.getByRole("button", { name: labels.schoolSSO }),
        ).toBeVisible();
        expect((await me(pageB)).status).toBe(401);
        expect(await transactionCookieNames(deviceB.context)).toEqual([]);
        // Autofill is not re-armed against the same refusal.
        expect(await pendingAutofillRequests(pageB)).toBe(0);
        await expectNoBlockingAxeViolations(pageB, "passkey auth_sso_required");
        await expectBrowserPrivacy(pageB, secrets);
      });

      await test.step("Device B: phone/start stays neutral (202), phone/verify refuses with auth_sso_required", async () => {
        // A fresh /sign-in; presence off keeps autofill pending.
        await setPresence(authenticatorB, false);
        await pageB.goto("/sign-in");
        await pageB.getByRole("button", { name: labels.usePhone }).click();
        const form = pageB.getByRole("form", { name: labels.phoneForm });
        await expect(
          form.getByRole("combobox", { name: labels.phoneCountry }),
        ).toHaveValue("VN");
        await form
          .getByRole("textbox", { name: labels.phoneNumber })
          .fill(phone.display);
        const started = responseTo(pageB, "POST", "/v1/auth/phone/start");
        await form.getByRole("button", { name: labels.phoneSendCode }).click();
        const { challengeId, maskedDestination } = await readChallenge(
          await started,
        );
        expect(maskedDestination).toBe(phone.masked);
        const code = await waitForDevInboxCode(
          request,
          challengeId,
          "phone_login",
          phone.e164,
        );
        secrets.push(code);
        await form.getByRole("textbox", { name: labels.phoneCode }).fill(code);
        const verified = responseTo(pageB, "POST", "/v1/auth/phone/verify");
        await form.getByRole("button", { name: labels.phoneVerify }).click();
        const refused = await verified;
        expect(refused.status()).toBe(403);
        expect(await problemCode(refused)).toBe("auth_sso_required");

        // The code step is gone; the identifier and school SSO are back.
        await expect(form).toHaveCount(0);
        await expect(
          pageB.getByRole("textbox", { name: labels.phoneCode }),
        ).toHaveCount(0);
        const alert = pageB.getByRole("alert");
        await expect(alert).toHaveCount(1);
        await expect(alert).toContainText(labels.ssoRequiredAccount);
        await expect(
          pageB.getByRole("textbox", { name: labels.identifier }),
        ).toBeFocused();
        await expect(
          pageB.getByRole("button", { name: labels.schoolSSO }),
        ).toBeVisible();
        expect((await me(pageB)).status).toBe(401);
        expect(await transactionCookieNames(deviceB.context)).toEqual([]);
        await expectNoBlockingAxeViolations(pageB, "phone auth_sso_required");
        await expectBrowserPrivacy(pageB, secrets);
      });

      /* ----- Step 4: school SSO links, revokes, asks for MFA, steps up ----- */
      await test.step("Device B: school SSO links the account, revokes Device A and stops at /sign-in?mfa=pending", async () => {
        await setPresence(authenticatorB, false);
        expect(await discoverSchool(pageB, member.email)).toEqual({
          kind: "email",
          sso: {
            available: true,
            domain: schoolDomain,
            provider: "google",
            required: true,
          },
        });
        await continueWithSchoolGoogle(pageB, member.email, schoolDomain);

        const visited: string[] = [];
        pageB.on("framenavigated", (frame) => {
          if (frame === pageB.mainFrame()) visited.push(frame.url());
        });
        // The SPA strips `?mfa=pending` at once, so watch the redirect itself.
        const redirected = pageB.waitForRequest(
          (sent) =>
            sent.isNavigationRequest() &&
            sent.url() === `${webOrigin}/sign-in?mfa=pending`,
        );
        const pending = responseTo(pageB, "GET", "/v1/auth/mfa");
        await chooseFakeGoogleAccount(pageB, member.email, schoolDomain);
        await redirected;
        const state = await pending;
        expect(state.status()).toBe(200);
        expect(await state.json()).toMatchObject({
          methods: expect.arrayContaining(["totp", "passkey", "recovery_code"]),
        });
        await expect(pageB).toHaveURL(`${webOrigin}/sign-in`);
        // Linked in place: never the /account-link confirmation.
        expect(
          visited.some((url) => new URL(url).pathname === "/account-link"),
        ).toBe(false);
        // No session before the second step; Device A was revoked
        // (`identity_linked`) by the callback itself.
        expect((await me(pageB)).status).toBe(401);
        expect((await me(pageA)).status).toBe(401);

        const step = pageB.getByRole("region", { name: labels.mfaStep });
        await expect(step).toBeVisible();
        await expectNoBlockingAxeViolations(
          pageB,
          "/sign-in?mfa=pending after school SSO",
        );
        await expectBrowserPrivacy(pageB, secrets);
      });

      await test.step("Device B: the authenticator code finishes an AAL2 session in /app", async () => {
        const step = pageB.getByRole("region", { name: labels.mfaStep });
        const totp = await freshTotp(pageB, totpSecret, lastStep);
        lastStep = totp.step;
        secrets.push(totp.code);
        await step
          .getByRole("textbox", { name: labels.mfaTotpField })
          .fill(totp.code);
        const verified = responseTo(pageB, "POST", "/v1/auth/mfa/verify");
        await step.getByRole("button", { name: labels.mfaVerify }).click();
        const response = await verified;
        expect(response.status()).toBe(200);
        expect(await response.json()).toMatchObject({
          status: "authenticated",
        });
        await expect(pageB).toHaveURL(/\/app\/home$/);
        expect(await me(pageB)).toEqual({ email: member.email, status: 200 });
        // The pre-SSO session on Device A stays revoked.
        expect((await me(pageA)).status).toBe(401);
      });

      await test.step("Device B: a Settings action that needs a step-up succeeds with the passkey", async () => {
        await pageB.goto("/app/settings");
        const card = pageB.getByRole("region", { name: labels.totpCard });
        await expect(card).toBeVisible();
        await expectNoBlockingAxeViolations(pageB, "/app/settings");

        // The session is younger than the 10-minute window: inject one 403.
        let injected = false;
        await pageB.route("**/api/v1/me/mfa/recovery-codes", async (route) => {
          if (injected || route.request().method() !== "POST") {
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
        await card.getByRole("button", { name: labels.regenerate }).click();
        await pageB
          .getByRole("dialog", { name: labels.regenerateDialog })
          .getByRole("button", { name: labels.regenerateConfirm })
          .click();
        const stepUp = pageB.getByRole("dialog", { name: labels.stepUpDialog });
        await expect(stepUp).toBeVisible();
        expect(injected).toBe(true);
        await expectNoBlockingAxeViolations(
          pageB,
          "step-up dialog",
          '[role="dialog"]',
        );

        await setPresence(authenticatorB, true);
        const reauth = responseTo(pageB, "POST", "/v1/auth/reauth");
        const regenerated = responseTo(
          pageB,
          "POST",
          "/v1/me/mfa/recovery-codes",
        );
        await stepUp
          .getByRole("button", { name: labels.stepUpPasskeyConfirm })
          .click();
        const reauthResponse = await reauth;
        expect(reauthResponse.status()).toBe(200);
        expect(reauthResponse.request().postDataJSON()).toMatchObject({
          method: "passkey",
        });
        // Regenerating requires AAL2 on the server: the SSO + TOTP session.
        const regenerateResponse = await regenerated;
        expect(regenerateResponse.status()).toBe(200);
        const { recovery_codes: fresh } = (await regenerateResponse.json()) as {
          recovery_codes: string[];
        };
        expect(fresh).toHaveLength(10);
        secrets.push(...fresh);
        await expect(stepUp).toBeHidden();
        const codes = pageB.getByRole("dialog", { name: labels.codesDialog });
        await codes.getByRole("checkbox", { name: labels.codesAck }).check();
        await codes.getByRole("button", { name: labels.codesDone }).click();
        await expect(codes).toBeHidden();
        await pageB.unroute("**/api/v1/me/mfa/recovery-codes");
        expect(await me(pageB)).toEqual({ email: member.email, status: 200 });
      });

      /* ----- Step 5: browser privacy gate on both devices ----- */
      await test.step("nothing sensitive reached Web Storage, script cookies or the address bar", async () => {
        await expectBrowserPrivacy(pageB, secrets);
        // Device A (revoked) on a page without passkey autofill.
        await setPresence(authenticatorA, false);
        await pageA.goto("/signed-out");
        await expectBrowserPrivacy(pageA, secrets);
      });
    } finally {
      for (const context of contexts) {
        await context.close().catch(() => undefined);
      }
    }
  });
});
