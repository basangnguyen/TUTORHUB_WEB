import AxeBuilder from "@axe-core/playwright";
import {
  expect,
  test,
  type APIRequestContext,
  type Browser,
  type BrowserContext,
  type Locator,
  type Page,
  type Response,
} from "@playwright/test";
import { randomBytes } from "node:crypto";
import { createSocket, type Socket } from "node:dgram";

/**
 * AUTH-12B: school domain SSO end to end on the loopback stack only
 * (`node scripts/e2e-local.mjs serve --native-auth`, started by
 * playwright.auth.config.ts). Google is the second loopback fake issuer on
 * 127.0.0.1:9092, which signs the Workspace `hd` claim of a custom account;
 * no real identity provider is called.
 *
 * DNS TXT verification never reaches public DNS: the API runs Go's own DNS
 * client against AUTH_E2E_DNS_UDP_ADDR=127.0.0.1:9095 (accepted only with
 * APP_ENV=test), and this spec answers those queries from an in-memory record
 * set, so "publishing" a TXT record is a Map write.
 *
 * Every run uses its own random `.test` school domain and addresses, so the
 * spec does not depend on earlier runs against the same database.
 */

const apiOrigin = "http://127.0.0.1:8080";
const webOrigin = "http://localhost:5173";
const googleOrigin = "http://127.0.0.1:9092";
const dnsAddress = { host: "127.0.0.1", port: 9095 } as const;
const nativeAuthProject = "local-chromium-native-auth";
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
  signUpGetCode: /^(?:Lấy mã|Get code)$/,
  signUpOtp: /^(?:Vui lòng nhập mã xác minh|Please enter verification code)$/,
  continue: /^(?:Tiếp tục|Continue)$/,
  identifier: /^(?:Email hoặc tên đăng nhập|Email or username)$/,
  schoolSSO: /^(?:Đăng nhập bằng SSO trường học|Sign in with school SSO)$/,
  continueGoogle:
    /^(?:Tiếp tục với Google của trường|Continue with school Google)$/,
  ssoRequiredAlert:
    /(?:Email này phải đăng nhập qua SSO của trường|This email address must sign in through your school's SSO)/,
  callbackSsoRequired:
    /(?:tên miền email này yêu cầu đăng nhập qua SSO của trường|this email domain requires your school's SSO)/,
  panel: /^(?:SSO trường học|School SSO)$/,
  newConnection: /^(?:Tạo kết nối mới|New connection)$/,
  hdField:
    /^(?:Tên miền tổ chức Google Workspace \(hd\)|Google Workspace organization domains \(hd\))$/,
  createConnection: /^(?:Tạo kết nối|Create connection)$/,
  domainField: /^(?:Tên miền email của trường|School email domain)$/,
  addDomain: /^(?:Khai báo tên miền|Add domain)$/,
  txtName: /^(?:Tên bản ghi \(Host\)|Record name \(host\))$/,
  txtValue: /^(?:Giá trị TXT|TXT value)$/,
  copyName: /^(?:Sao chép tên bản ghi|Copy record name)$/,
  copyValue: /^(?:Sao chép giá trị TXT|Copy TXT value)$/,
  verified: /^(?:Đã xác minh|Verified)$/,
  enable: /^(?:Bật kết nối|Enable connection)$/,
  enabled: /^(?:Đang bật|Enabled)$/,
  enforce:
    /^(?:Bắt buộc đăng nhập qua SSO trường học cho các tên miền đã xác minh|Require school SSO sign-in for verified domains)$/,
  enforceDialog: /^(?:Bắt buộc SSO trường học\?|Require school SSO\?)$/,
  enforceConfirm: /^(?:Bắt buộc SSO|Require SSO)$/,
  enforcedBadge: /^(?:Bắt buộc SSO|SSO required)$/,
  lockout: /(?:Để không tự khóa tài khoản|To avoid locking yourself out)/,
  joinPolicy:
    /^(?:Khi người mới đăng nhập qua SSO|When someone new signs in through SSO)$/,
  joinStudent: /^(?:Tự động thêm làm Học sinh|Add automatically as a student)$/,
  saveJoinPolicy: /^(?:Lưu chính sách|Save policy)$/,
  auditTable:
    /^(?:Danh sách sự kiện kiểm toán của workspace|Workspace audit event list)$/,
  auditVerify: /^(?:Xác minh tên miền SSO|Verify SSO domain)$/,
  auditConnection: /^(?:Tạo kết nối SSO|Create SSO connection)$/,
  identityCard: /^(?:Danh tính đăng nhập|Sign-in identities)$/,
  identityLinkGoogle: /^(?:Liên kết Google|Link Google)$/,
  identityLinked:
    /^(?:Đã liên kết danh tính đăng nhập\.|Sign-in identity linked\.)$/,
} as const;

// ---------------------------------------------------------------- DNS TXT

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

function startDNSResponder() {
  return new Promise<Socket>((resolve, reject) => {
    const socket = createSocket("udp4");
    socket.on("message", (message, peer) => {
      const reply = dnsAnswer(message);
      if (reply) socket.send(reply, peer.port, peer.address);
    });
    socket.once("error", reject);
    socket.bind(dnsAddress.port, dnsAddress.host, () => {
      socket.off("error", reject);
      resolve(socket);
    });
  });
}

function publishTXT(name: string, value: string) {
  txtRecords.set(name.toLowerCase().replace(/\.$/, ""), [value]);
}

// --------------------------------------------------------------- helpers

interface DevInboxMessage {
  challenge_id: string;
  code?: string;
  purpose: string;
}

interface SchoolAccount {
  displayName: string;
  email: string;
  password: string;
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

/** Web Storage and script-visible cookies never hold TXT values or emails. */
async function expectNoSecretsInBrowser(page: Page, secrets: string[]) {
  const stored = await page.evaluate(() =>
    JSON.stringify({
      cookie: document.cookie,
      local: { ...window.localStorage },
      session: { ...window.sessionStorage },
    }),
  );
  expect(stored).not.toMatch(/tutorhub-site-verification|tutorhub_session/i);
  for (const secret of secrets) {
    expect(stored).not.toContain(secret);
  }
}

async function openDevice(browser: Browser) {
  const context = await browser.newContext({
    baseURL: webOrigin,
    locale: "vi-VN",
    reducedMotion: "reduce",
  });
  return { context, page: await context.newPage() };
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
  const code = found?.code ?? "";
  expect(code).toMatch(/^\d{6}$/);
  return code;
}

async function signUp(
  page: Page,
  request: APIRequestContext,
  account: SchoolAccount,
) {
  await page.goto("/sign-in");
  await page.getByRole("button", { name: labels.signUp }).click();
  const form = page.getByRole("form", { name: labels.signUpForm });
  // Step 1: email + consent -> "Get code".
  await form.getByRole("textbox", { name: labels.email }).fill(account.email);
  await form.getByRole("checkbox", { name: labels.terms }).check();
  const started = responseTo(page, "POST", "/v1/auth/signup/email/start");
  await form.getByRole("button", { name: labels.signUpGetCode }).click();
  const startResponse = await started;
  expect(startResponse.status()).toBe(202);
  const { challenge_id: challengeId } = (await startResponse.json()) as {
    challenge_id: string;
  };
  const code = await waitForDevInboxCode(request, challengeId);
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
    .fill(`AUTH12 School ${suffix}`);
  await page
    .getByLabel(/^(?:Địa chỉ workspace|Workspace address)$/)
    .fill(`auth12-${suffix}`);
  const created = responseTo(page, "POST", "/v1/tenants");
  await page
    .getByRole("button", { name: /^(?:Tạo workspace|Create workspace)$/ })
    .click();
  expect((await created).status()).toBe(201);
  await expect(onboarding).toBeHidden();
}

interface MeSnapshot {
  activeTenantID?: string;
  email?: string;
  role?: string;
  status: number;
}

async function me(page: Page): Promise<MeSnapshot> {
  const response = await page.request.get("/api/v1/me", {
    headers: { Accept: "application/json" },
  });
  if (response.status() !== 200) return { status: response.status() };
  const body = (await response.json()) as {
    active_tenant?: { id?: string; role?: string } | null;
    user?: { email?: string };
  };
  return {
    activeTenantID: body.active_tenant?.id,
    email: body.user?.email,
    role: body.active_tenant?.role,
    status: 200,
  };
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

/** Leaves for the school's Google with login_hint and domain. */
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
  // A top-level navigation, never a script fetch (RFC 9700 §4.4).
  expect(request.isNavigationRequest()).toBe(true);
  const url = new URL(request.url());
  expect(url.searchParams.get("login_hint")).toBe(email);
  expect(url.searchParams.get("domain")).toBe(domain);
  expect(url.searchParams.get("intent")).toBe("login");
  // The server adds Google's `hd` hint for the verified school domain.
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

// ---------------------------------------------------------------- scenario

const runID = randomBytes(5).toString("hex");
const schoolDomain = `auth12-${runID}.test`;
const txtOwner = `_tutorhub-challenge.${schoolDomain}.`;
const admin: SchoolAccount = {
  displayName: `AUTH12 Admin ${runID.slice(0, 4)}`,
  email: `auth12.admin.${runID}@${schoolDomain}`,
  password: `Harbor-${randomBytes(9).toString("base64url")}-Lantern7`,
};
const studentEmail = `auth12.student.${runID}@${schoolDomain}`;
const intruderEmail = `auth12.intruder.${runID}@${schoolDomain}`;
let schoolTenantID = "";
let txtValue = "";

test.describe.configure({ mode: "serial" });

test.describe("AUTH-12 school domain SSO", () => {
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
      "School SSO runs only through `pnpm e2e:auth` (serve --native-auth).",
    );
    const providers = await request.get(`${apiOrigin}/api/v1/auth/providers`, {
      headers: { Accept: "application/json" },
    });
    expect(providers.status()).toBe(200);
    expect(await providers.json()).toMatchObject({
      oauth_providers: expect.arrayContaining(["google"]),
      password_enabled: true,
      signup_enabled: true,
      sso_domains_enabled: true,
    });
  });

  test("an org_admin verifies a school domain over DNS and enforces SSO without locking out", async ({
    browser,
    request,
  }) => {
    test.setTimeout(420_000);
    const contexts: BrowserContext[] = [];
    try {
      const owner = await openDevice(browser);
      contexts.push(owner.context);
      const page = owner.page;

      await test.step("the administrator signs up with a password on the school domain", async () => {
        await signUp(page, request, admin);
        await createWorkspace(page, runID);
        const snapshot = await me(page);
        expect(snapshot).toMatchObject({ role: "org_admin", status: 200 });
        schoolTenantID = snapshot.activeTenantID ?? "";
        expect(schoolTenantID).not.toBe("");
      });

      const panel = panelOf(page);
      await test.step("the workspace page offers school SSO and a Google Workspace connection", async () => {
        await page.goto("/app/workspace");
        await expect(panel).toBeVisible();
        const form = panel.getByRole("form", { name: labels.newConnection });
        await form
          .getByRole("textbox", { name: labels.hdField })
          .fill(schoolDomain);
        const created = responseTo(page, "POST", "/sso-connections");
        await form
          .getByRole("button", { name: labels.createConnection })
          .click();
        const response = await created;
        expect(response.status()).toBe(201);
        expect(await response.json()).toMatchObject({
          allowed_org_ids: [schoolDomain],
          auto_join_role: null,
          enforce_sso: false,
          kind: "google_workspace",
          status: "disabled",
        });
        await expect(connectionCard(panel)).toHaveCount(1);
        await expectNoBlockingAxeViolations(
          page,
          "school SSO panel",
          "section.school-sso",
        );
      });

      await test.step("declaring the domain shows the one-time TXT record", async () => {
        await panel
          .getByRole("textbox", { name: labels.domainField })
          .fill(schoolDomain);
        const created = responseTo(page, "POST", "/sso-domains");
        await panel.getByRole("button", { name: labels.addDomain }).click();
        expect((await created).status()).toBe(201);
        const record = panel.getByRole("group", {
          name: new RegExp(`${schoolDomain.replace(/\./g, "\\.")}$`),
        });
        await expect(record).toBeVisible();
        await expect(
          record.getByRole("textbox", { name: labels.txtName }),
        ).toHaveValue(txtOwner);
        txtValue = await record
          .getByRole("textbox", { name: labels.txtValue })
          .inputValue();
        expect(txtValue).toMatch(
          /^tutorhub-site-verification=[A-Za-z0-9_-]{20,}$/,
        );
        await expect(
          record.getByRole("button", { name: labels.copyName }),
        ).toBeVisible();
        await expect(
          record.getByRole("button", { name: labels.copyValue }),
        ).toBeVisible();
        await expectNoBlockingAxeViolations(
          page,
          "TXT record",
          "section.school-sso",
        );
        await expectNoSecretsInBrowser(page, [txtValue]);
      });

      const verifyButton = panel.getByRole("button", {
        name: new RegExp(
          `^(?:Xác minh DNS|Verify DNS) ${schoolDomain.replace(/\./g, "\\.")}$`,
        ),
      });
      await test.step("verification fails with auth_sso_dns_mismatch before the record exists", async () => {
        const verified = responseTo(page, "POST", "/verify");
        await verifyButton.click();
        const response = await verified;
        expect(response.status()).toBe(409);
        expect(await problemCode(response)).toBe("auth_sso_dns_mismatch");
        await expect(panel.getByRole("alert")).toContainText(txtOwner);
      });

      await test.step("publishing the record on the loopback DNS verifies the domain", async () => {
        publishTXT(txtOwner, txtValue);
        const verified = responseTo(page, "POST", "/verify");
        await verifyButton.click();
        const response = await verified;
        expect(response.status()).toBe(200);
        expect(await response.json()).toMatchObject({
          domain: schoolDomain,
          status: "verified",
        });
        await expect(
          panel.getByText(labels.verified, { exact: true }),
        ).toBeVisible();
        // The one-time value leaves the page once the domain is verified.
        await expect(
          panel.getByRole("textbox", { name: labels.txtValue }),
        ).toHaveCount(0);
        await expectNoSecretsInBrowser(page, [txtValue]);
      });

      await test.step("the connection is enabled", async () => {
        const updated = responseTo(page, "PATCH", /\/sso-connections\/[^/]+$/);
        await panel.getByRole("button", { name: labels.enable }).click();
        expect((await updated).status()).toBe(200);
        await expect(
          connectionCard(panel).getByText(labels.enabled, { exact: true }),
        ).toBeVisible();
      });

      await test.step("requiring SSO from a password session is refused by the lockout guard", async () => {
        await panel.getByRole("switch", { name: labels.enforce }).click();
        const dialog = page.getByRole("dialog", { name: labels.enforceDialog });
        await expect(dialog).toContainText(schoolDomain);
        const updated = responseTo(page, "PATCH", /\/sso-connections\/[^/]+$/);
        await dialog
          .getByRole("button", { name: labels.enforceConfirm })
          .click();
        const response = await updated;
        expect(response.status()).toBe(409);
        expect(await problemCode(response)).toBe("auth_sso_lockout");
        expect(JSON.parse(response.request().postData() ?? "{}")).toEqual({
          enforce_sso: true,
        });
        await expect(panel.getByRole("alert")).toContainText(labels.lockout);
        await expect(
          panel.getByRole("switch", { name: labels.enforce }),
        ).not.toBeChecked();
      });

      await test.step("the audit log lists the school SSO events", async () => {
        await page.goto("/app/workspace/audit");
        const table = page.getByRole("table", { name: labels.auditTable });
        await expect(table).toBeVisible();
        await expect(
          table.getByRole("cell", { name: labels.auditVerify }).first(),
        ).toBeVisible();
        await expect(
          table.getByRole("cell", { name: labels.auditConnection }).first(),
        ).toBeVisible();
      });

      await test.step("the administrator links the school Google account in Settings", async () => {
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
      });

      const schoolDevice = await openDevice(browser);
      contexts.push(schoolDevice.context);
      const schoolPage = schoolDevice.page;
      await test.step("HRD offers the optional school route; signing in through it proves the organization", async () => {
        const answer = await discoverSchool(schoolPage, admin.email);
        expect(answer).toEqual({
          kind: "email",
          sso: {
            available: true,
            domain: schoolDomain,
            provider: "google",
            required: false,
          },
        });
        await expect(
          schoolPage.getByRole("textbox", { name: labels.identifier }),
        ).toBeEnabled();
        await continueWithSchoolGoogle(schoolPage, admin.email, schoolDomain);
        await chooseFakeGoogleAccount(schoolPage, admin.email, schoolDomain);
        await expect(schoolPage).toHaveURL(/\/app\/home$/);
        expect(await me(schoolPage)).toMatchObject({
          activeTenantID: schoolTenantID,
          email: admin.email,
          role: "org_admin",
        });
      });

      await test.step("from the school session SSO is enforced and new students join automatically", async () => {
        await schoolPage.goto("/app/workspace");
        const schoolPanel = panelOf(schoolPage);
        await schoolPanel.getByRole("switch", { name: labels.enforce }).click();
        const dialog = schoolPage.getByRole("dialog", {
          name: labels.enforceDialog,
        });
        const enforced = responseTo(
          schoolPage,
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
          schoolPanel.getByRole("switch", { name: labels.enforce }),
        ).toBeChecked();
        await expect(
          schoolPanel.getByText(labels.enforcedBadge, { exact: true }),
        ).toBeVisible();

        await schoolPanel
          .getByRole("combobox", { name: labels.joinPolicy })
          .click();
        await schoolPage
          .getByRole("option", { name: labels.joinStudent })
          .click();
        const joined = responseTo(
          schoolPage,
          "PATCH",
          /\/sso-connections\/[^/]+$/,
        );
        await schoolPanel
          .getByRole("button", { name: labels.saveJoinPolicy })
          .click();
        const joinResponse = await joined;
        expect(joinResponse.status()).toBe(200);
        expect(JSON.parse(joinResponse.request().postData() ?? "{}")).toEqual({
          auto_join_role: "student",
        });
        expect(await joinResponse.json()).toMatchObject({
          auto_join_role: "student",
          enforce_sso: true,
          status: "enabled",
        });
      });
    } finally {
      for (const context of contexts) await context.close();
    }
  });

  test("/sign-in routes school addresses to the school's Google and enforces the organization", async ({
    browser,
  }) => {
    test.setTimeout(300_000);
    expect(schoolTenantID).not.toBe("");
    const contexts: BrowserContext[] = [];
    try {
      const device = await openDevice(browser);
      contexts.push(device.context);
      const page = device.page;

      const expectedRoute = {
        kind: "email",
        sso: {
          available: true,
          domain: schoolDomain,
          provider: "google",
          required: true,
        },
      };
      await test.step("HRD answers the same route for an existing account and an unknown student", async () => {
        expect(await discoverSchool(page, admin.email)).toEqual(expectedRoute);
        expect(await discoverSchool(page, studentEmail)).toEqual(expectedRoute);
        await expect(
          page.getByRole("button", { name: labels.continueGoogle }),
        ).toBeVisible();
        await expectNoBlockingAxeViolations(page, "/sign-in school SSO route");
      });

      for (const email of [admin.email, studentEmail]) {
        await test.step(`a password sign-in is refused with auth_sso_required (${email === admin.email ? "existing" : "new"} address)`, async () => {
          await page.goto("/sign-in");
          await page
            .getByRole("textbox", { name: labels.identifier })
            .fill(email);
          await page.getByLabel(labels.password).fill(admin.password);
          const login = responseTo(page, "POST", "/v1/auth/password/login");
          await page.getByRole("button", { name: labels.logIn }).click();
          const response = await login;
          expect(response.status()).toBe(403);
          expect(await problemCode(response)).toBe("auth_sso_required");
          await expect(page.getByRole("alert")).toContainText(
            labels.ssoRequiredAlert,
          );
          await expect(page.getByLabel(labels.password)).toHaveValue("");
          await expect(
            page.getByRole("button", { name: labels.continueGoogle }),
          ).toBeVisible();
          expect((await me(page)).status).toBe(401);
        });
      }

      await test.step("a new student signs in with the school's Google and joins as a student", async () => {
        await continueWithSchoolGoogle(page, studentEmail, schoolDomain);
        await chooseFakeGoogleAccount(page, studentEmail, schoolDomain);
        await expect(page).toHaveURL(/\/app\/home$/);
        expect(await me(page)).toEqual({
          activeTenantID: schoolTenantID,
          email: studentEmail,
          role: "student",
          status: 200,
        });
        await expectNoSecretsInBrowser(page, [studentEmail, txtValue]);
      });

      const intruder = await openDevice(browser);
      contexts.push(intruder.context);
      await test.step("a Google account from another organization is sent back with auth_sso_required", async () => {
        const answer = await discoverSchool(intruder.page, intruderEmail);
        expect(answer).toEqual(expectedRoute);
        await continueWithSchoolGoogle(
          intruder.page,
          intruderEmail,
          schoolDomain,
        );
        // The SPA strips the parameter at once, so watch the redirect itself.
        const redirected = intruder.page.waitForRequest(
          (request) =>
            request.isNavigationRequest() &&
            request.url() === `${webOrigin}/sign-in?error=auth_sso_required`,
        );
        await chooseFakeGoogleAccount(
          intruder.page,
          intruderEmail,
          `other-${runID}.test`,
        );
        await redirected;
        await expect(intruder.page.getByRole("alert")).toContainText(
          labels.callbackSsoRequired,
        );
        await expect(intruder.page).toHaveURL(`${webOrigin}/sign-in`);
        expect((await me(intruder.page)).status).toBe(401);
        await expectNoBlockingAxeViolations(
          intruder.page,
          "/sign-in?error=auth_sso_required",
        );
        // The callback rolled back before any account, session or membership
        // existed (covered at the database level by the AUTH-12A suites).
        await expectNoSecretsInBrowser(intruder.page, [
          intruderEmail,
          txtValue,
        ]);
      });
    } finally {
      for (const context of contexts) await context.close();
    }
  });
});
