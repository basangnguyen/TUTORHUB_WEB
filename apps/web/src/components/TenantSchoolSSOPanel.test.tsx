import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type {
  AuthProvidersResponse,
  CurrentUser,
  SSOConnection,
  SSODomain,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { isPlausibleDomain, parseOrgIDs } from "../app/schoolSSO";
import { SessionProvider } from "../app/session";
import { TenantSchoolSSOPanel } from "./TenantSchoolSSOPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const connectionID = "1d4c9e52-6d0b-4a3f-9a43-6c2e0f7b8a11";
const domainID = "7f3b2a10-9c4d-4e5f-8a6b-1c2d3e4f5a6b";
const TXT_VALUE = "tutorhub-site-verification=one-time-secret-value";
const TXT_NAME = "_tutorhub-challenge.school.example.";

function jsonResponse(body: unknown, status = 200, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
      ...headers,
    },
  });
}

function problem(status: number, code?: string, headers = {}) {
  return jsonResponse(
    { type: "about:blank", title: "Rejected", status, code },
    status,
    headers,
  );
}

function session(
  role: "org_admin" | "teacher" = "org_admin",
  permissions: CurrentUser["permissions"] = ["tenant.manage"],
): CurrentUser {
  return {
    user: {
      id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      email: "admin@school.example",
      display_name: "School Admin",
      locale: "en",
      timezone: "Asia/Ho_Chi_Minh",
    },
    active_tenant: {
      id: tenantID,
      slug: "school",
      name: "School",
      role,
      status: "active",
      version: 1,
      is_active: true,
    },
    memberships: [],
    permissions,
  };
}

const providers: AuthProvidersResponse = {
  device_management_enabled: true,
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: true,
  oauth_providers: ["google"],
  oidc_default_enabled: false,
  passkeys_enabled: false,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: true,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: true,
};

function connection(overrides: Partial<SSOConnection> = {}): SSOConnection {
  return {
    id: connectionID,
    tenant_id: tenantID,
    kind: "google_workspace",
    status: "disabled",
    allowed_org_ids: ["school.example"],
    enforce_sso: false,
    auto_join_role: null,
    created_at: "2026-11-21T01:00:00Z",
    updated_at: "2026-11-21T01:00:00Z",
    ...overrides,
  };
}

function domain(overrides: Partial<SSODomain> = {}): SSODomain {
  return {
    id: domainID,
    tenant_id: tenantID,
    connection_id: connectionID,
    domain: "school.example",
    status: "pending",
    verified_at: null,
    created_at: "2026-11-21T01:00:00Z",
    updated_at: "2026-11-21T01:00:00Z",
    ...overrides,
  };
}

interface ServerState {
  connections: SSOConnection[];
  domains: SSODomain[];
}

type Handler = (
  request: Request,
  state: ServerState,
) => Response | Promise<Response> | undefined;

interface Sent {
  body: unknown;
  csrf: string | null;
  method: string;
  path: string;
}

/** A stateful fake Core API; `handler` can override any request. */
function fakeServer(initial: Partial<ServerState>, handler?: Handler) {
  const state: ServerState = {
    connections: initial.connections ?? [],
    domains: initial.domains ?? [],
  };
  const sent: Sent[] = [];
  const base = `/api/v1/tenants/${tenantID}`;
  const fetchMock = vi.fn(async (request: Request) => {
    const url = new URL(request.url);
    const path = url.pathname;
    if (request.method !== "GET") {
      const text = await request.clone().text();
      sent.push({
        body: text ? (JSON.parse(text) as unknown) : undefined,
        csrf: request.headers.get("X-CSRF-Token"),
        method: request.method,
        path,
      });
    }
    const custom = handler?.(request, state);
    if (custom) return custom;
    if (path === "/api/v1/auth/csrf") {
      return jsonResponse({ csrf_token: "csrf-test" });
    }
    if (path === "/api/v1/auth/providers") return jsonResponse(providers);
    if (path === "/api/v1/me/security") {
      return jsonResponse({
        email_verified: true,
        has_password: true,
        mfa_enabled: false,
        reauth_methods: ["password"],
        recent_auth_expires_at: null,
      });
    }
    if (path === "/api/v1/auth/reauth") {
      return jsonResponse({ recent_auth_expires_at: "2026-11-21T01:10:00Z" });
    }
    if (path === `${base}/sso-connections` && request.method === "GET") {
      return jsonResponse({ items: state.connections });
    }
    if (path === `${base}/sso-domains` && request.method === "GET") {
      return jsonResponse({ items: state.domains });
    }
    if (path === `${base}/sso-connections` && request.method === "POST") {
      const body = (await request.json()) as Pick<
        SSOConnection,
        "kind" | "allowed_org_ids"
      >;
      const created = connection(body);
      state.connections.push(created);
      return jsonResponse(created, 201);
    }
    if (
      path === `${base}/sso-connections/${connectionID}` &&
      request.method === "PATCH"
    ) {
      const patch = (await request.json()) as Partial<SSOConnection>;
      const current = state.connections[0] ?? connection();
      const updated = {
        ...current,
        ...patch,
        updated_at: new Date().toISOString(),
      };
      state.connections = [updated];
      return jsonResponse(updated);
    }
    if (path === `${base}/sso-domains` && request.method === "POST") {
      const body = (await request.json()) as {
        connection_id: string;
        domain: string;
      };
      const created = domain({ domain: body.domain });
      state.domains.push(created);
      return jsonResponse(
        { domain: created, txt_name: TXT_NAME, txt_value: TXT_VALUE },
        201,
      );
    }
    if (path === `${base}/sso-domains/${domainID}/verify`) {
      const verified = domain({
        status: "verified",
        verified_at: "2026-11-21T01:05:00Z",
      });
      state.domains = [verified];
      return jsonResponse(verified);
    }
    if (path === `${base}/sso-domains/${domainID}/rotate`) {
      return jsonResponse({
        domain: domain(),
        txt_name: TXT_NAME,
        txt_value: `${TXT_VALUE}-rotated`,
      });
    }
    if (path === `${base}/sso-domains/${domainID}/disable`) {
      const disabled = domain({ status: "disabled" });
      state.domains = [disabled];
      return jsonResponse(disabled);
    }
    throw new Error(`Unexpected request: ${request.method} ${path}`);
  });
  return { fetchMock, sent, state };
}

function renderPanel(
  fetchMock: ReturnType<typeof vi.fn>,
  currentUser: CurrentUser = session(),
  language: Language = "en",
) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage={language}>
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <TenantSchoolSSOPanel tenantID={tenantID} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function panel() {
  return screen.getByRole("region", { name: "School SSO" });
}

function mutations(sent: Sent[], suffix: string) {
  return sent.filter((entry) => entry.path.endsWith(suffix));
}

async function confirmPasswordStepUp() {
  const dialog = await screen.findByRole("dialog", {
    name: "Confirm it's you",
  });
  fireEvent.change(await within(dialog).findByLabelText("Current password"), {
    target: { value: "Harbor-Lantern-77" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "Confirm it's you" }),
    ).not.toBeInTheDocument(),
  );
}

describe("school SSO client validation", () => {
  it("accepts domains and tenant UUIDs and explains invalid lists", () => {
    expect(isPlausibleDomain("truong.edu.vn")).toBe(true);
    expect(isPlausibleDomain("trường.vn")).toBe(true);
    for (const value of [
      "",
      "localhost",
      "a@b.vn",
      "https://school.vn",
      "school.vn/path",
      "1.2.3.4",
      "-bad.vn",
      "school.vn.",
    ]) {
      expect(isPlausibleDomain(value), value).toBe(false);
    }
    expect(
      parseOrgIDs("google_workspace", " School.Example , b.example\n"),
    ).toEqual({
      ids: ["school.example", "b.example"],
    });
    expect(parseOrgIDs("google_workspace", "").error).toBe(
      "schoolSSO.orgIDsRequired",
    );
    expect(parseOrgIDs("google_workspace", "a.vn a.vn").error).toBe(
      "schoolSSO.orgIDsTooMany",
    );
    expect(parseOrgIDs("entra", "not-a-uuid").error).toBe(
      "schoolSSO.orgIDsInvalidEntra",
    );
    expect(
      parseOrgIDs("entra", "5B3C1F0E-8D2A-4E61-9C7B-2A4F6D8E0C13"),
    ).toEqual({ ids: ["5b3c1f0e-8d2a-4e61-9c7b-2a4f6d8e0c13"] });
  });
});

describe("TenantSchoolSSOPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("is forbidden for a non-administrator and never calls the SSO API", async () => {
    const { fetchMock } = fakeServer({});
    renderPanel(fetchMock, session("teacher", ["tenant.manage"]));
    expect(
      await within(panel()).findByText("School SSO management unavailable"),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();

    cleanup();
    const second = fakeServer({});
    renderPanel(second.fetchMock, session("org_admin", []));
    expect(
      await within(panel()).findByText("School SSO management unavailable"),
    ).toBeInTheDocument();
    expect(second.fetchMock).not.toHaveBeenCalled();
  });

  it("shows loading, then the empty state, then creates a Google Workspace connection", async () => {
    const { fetchMock, sent } = fakeServer({});
    renderPanel(fetchMock);
    expect(
      within(panel()).getByLabelText("Loading school SSO settings"),
    ).toBeInTheDocument();
    expect(
      await within(panel()).findByText("No school connection yet"),
    ).toBeInTheDocument();

    const form = within(panel()).getByRole("form", { name: "New connection" });
    fireEvent.click(
      within(form).getByRole("button", { name: "Create connection" }),
    );
    expect(
      await within(form).findByText("Enter at least one value."),
    ).toBeInTheDocument();

    fireEvent.change(
      within(form).getByLabelText("Google Workspace organization domains (hd)"),
      { target: { value: "School.Example" } },
    );
    fireEvent.click(
      within(form).getByRole("button", { name: "Create connection" }),
    );
    expect(
      await within(panel()).findByText(
        "Connection created. It stays disabled until you enable it.",
      ),
    ).toBeInTheDocument();
    const created = mutations(sent, "/sso-connections");
    expect(created).toHaveLength(1);
    expect(created[0]).toMatchObject({
      body: { kind: "google_workspace", allowed_org_ids: ["school.example"] },
      csrf: "csrf-test",
      method: "POST",
    });
    expect(
      await within(panel()).findByRole("heading", { name: "Google Workspace" }),
    ).toBeInTheDocument();
  });

  it("creates a Microsoft Entra connection from tenant IDs", async () => {
    const { fetchMock, sent } = fakeServer({});
    renderPanel(fetchMock);
    const form = await within(
      await screen.findByRole("region", {
        name: "School SSO",
      }),
    ).findByRole("form", { name: "New connection" });
    const kind = within(form).getByRole("combobox", {
      name: "School identity provider",
    });
    fireEvent.keyDown(kind, { key: "ArrowDown" });
    fireEvent.click(
      await screen.findByRole("option", { name: "Microsoft Entra ID" }),
    );
    const ids = within(form).getByLabelText("Microsoft Entra tenant IDs (tid)");
    fireEvent.change(ids, { target: { value: "school.example" } });
    fireEvent.click(
      within(form).getByRole("button", { name: "Create connection" }),
    );
    expect(
      await within(form).findByText(
        "Each value must be a valid tenant ID (UUID).",
      ),
    ).toBeInTheDocument();
    fireEvent.change(ids, {
      target: { value: "5b3c1f0e-8d2a-4e61-9c7b-2a4f6d8e0c13" },
    });
    fireEvent.click(
      within(form).getByRole("button", { name: "Create connection" }),
    );
    await waitFor(() =>
      expect(mutations(sent, "/sso-connections")).toHaveLength(1),
    );
    expect(mutations(sent, "/sso-connections")[0]?.body).toEqual({
      kind: "entra",
      allowed_org_ids: ["5b3c1f0e-8d2a-4e61-9c7b-2a4f6d8e0c13"],
    });
  });

  it("shows the one-time TXT record, keeps it out of caches, and explains a DNS mismatch", async () => {
    let dnsPublished = false;
    const { fetchMock, sent } = fakeServer(
      { connections: [connection({ status: "enabled" })] },
      (request) =>
        request.url.endsWith(`/sso-domains/${domainID}/verify`) && !dnsPublished
          ? problem(409, "auth_sso_dns_mismatch")
          : undefined,
    );
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    const queryClient = renderPanel(fetchMock);

    const domainField = await within(panel()).findByLabelText(
      "School email domain",
    );
    fireEvent.change(domainField, { target: { value: "school.example" } });
    fireEvent.click(
      within(panel()).getByRole("button", { name: "Add domain" }),
    );

    const record = await within(panel()).findByRole("group", {
      name: "DNS TXT record for school.example",
    });
    expect(within(record).getByLabelText("Record name (host)")).toHaveValue(
      TXT_NAME,
    );
    expect(within(record).getByLabelText("TXT value")).toHaveValue(TXT_VALUE);
    expect(within(record).getByText(/shown only once/)).toBeInTheDocument();
    fireEvent.click(
      within(record).getByRole("button", { name: "Copy TXT value" }),
    );
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(TXT_VALUE));
    expect(await within(record).findByText("Copied.")).toBeInTheDocument();
    fireEvent.click(
      within(record).getByRole("button", { name: "Copy record name" }),
    );
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(TXT_NAME));
    expect(mutations(sent, "/sso-domains")[0]?.body).toEqual({
      connection_id: connectionID,
      domain: "school.example",
    });

    // Neither the query cache nor the mutation cache keeps the secret.
    const cached = JSON.stringify([
      queryClient
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
      queryClient
        .getMutationCache()
        .getAll()
        .map((mutation) => mutation.state.data),
    ]);
    expect(cached).not.toContain(TXT_VALUE);
    expect(
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ).not.toContain(TXT_VALUE);

    fireEvent.click(
      within(panel()).getByRole("button", {
        name: "Verify DNS school.example",
      }),
    );
    const alert = await within(panel()).findByRole("alert");
    expect(alert).toHaveTextContent(
      "No matching TXT record was found yet. Check that _tutorhub-challenge.school.example. holds the latest value",
    );
    expect(within(panel()).getByText("Waiting for DNS")).toBeInTheDocument();

    dnsPublished = true;
    fireEvent.click(
      within(panel()).getByRole("button", {
        name: "Verify DNS school.example",
      }),
    );
    expect(
      await within(panel()).findByText("Domain school.example verified."),
    ).toBeInTheDocument();
    expect(await within(panel()).findByText("Verified")).toBeInTheDocument();
    expect(
      within(panel()).queryByRole("group", {
        name: "DNS TXT record for school.example",
      }),
    ).not.toBeInTheDocument();
  });

  it("rotates a pending code and disables a domain only after confirmation", async () => {
    const { fetchMock, sent } = fakeServer({
      connections: [connection()],
      domains: [domain()],
    });
    renderPanel(fetchMock);
    fireEvent.click(
      await within(panel()).findByRole("button", {
        name: "New verification code school.example",
      }),
    );
    const record = await within(panel()).findByRole("group", {
      name: "DNS TXT record for school.example",
    });
    expect(within(record).getByLabelText("TXT value")).toHaveValue(
      `${TXT_VALUE}-rotated`,
    );
    fireEvent.click(
      within(record).getByRole("button", { name: "I've saved the record" }),
    );
    expect(
      within(panel()).queryByRole("group", {
        name: "DNS TXT record for school.example",
      }),
    ).not.toBeInTheDocument();

    fireEvent.click(
      within(panel()).getByRole("button", { name: "Disable school.example" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Disable the domain school.example?",
    });
    expect(mutations(sent, "/disable")).toHaveLength(0);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Disable domain" }),
    );
    expect(
      await within(panel()).findByText("Domain school.example disabled."),
    ).toBeInTheDocument();
    expect(mutations(sent, "/disable")).toHaveLength(1);
    expect(mutations(sent, "/rotate")[0]?.body).toEqual({});
  });

  it("allows enforcement only for an enabled connection with a verified domain and explains the lockout guard", async () => {
    const { fetchMock, sent } = fakeServer(
      { connections: [connection()], domains: [domain()] },
      (request) =>
        request.method === "PATCH"
          ? problem(409, "auth_sso_lockout")
          : undefined,
    );
    renderPanel(fetchMock);
    const toggle = await within(panel()).findByRole("switch", {
      name: "Require school SSO sign-in for verified domains",
    });
    expect(toggle).toBeDisabled();
    expect(
      within(panel()).getByText(
        "Enable the connection and verify at least one domain before requiring SSO.",
      ),
    ).toBeInTheDocument();

    cleanup();
    const ready = fakeServer(
      {
        connections: [connection({ status: "enabled" })],
        domains: [
          domain({ status: "verified", verified_at: "2026-11-21T01:05:00Z" }),
        ],
      },
      (request) =>
        request.method === "PATCH"
          ? problem(409, "auth_sso_lockout")
          : undefined,
    );
    renderPanel(ready.fetchMock);
    const enabledToggle = await within(panel()).findByRole("switch", {
      name: "Require school SSO sign-in for verified domains",
    });
    expect(enabledToggle).toBeEnabled();
    fireEvent.click(enabledToggle);
    const dialog = await screen.findByRole("dialog", {
      name: "Require school SSO?",
    });
    expect(dialog).toHaveTextContent("school.example");
    expect(dialog).toHaveTextContent("Google");
    expect(
      mutations(ready.sent, `/sso-connections/${connectionID}`),
    ).toHaveLength(0);
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Require SSO" }),
    );
    const alert = await within(panel()).findByRole("alert");
    expect(alert).toHaveTextContent(
      "To avoid locking yourself out, sign in with a school account",
    );
    expect(
      mutations(ready.sent, `/sso-connections/${connectionID}`)[0]?.body,
    ).toEqual({ enforce_sso: true });
    expect(sent).toHaveLength(0);
  });

  it("retries a stale-session mutation exactly once after the shared step-up", async () => {
    let refused = 0;
    const { fetchMock, sent } = fakeServer(
      { connections: [connection()] },
      (request) => {
        if (request.method === "PATCH" && refused === 0) {
          refused += 1;
          return problem(403, "auth_recent_authentication_required");
        }
        return undefined;
      },
    );
    renderPanel(fetchMock);
    fireEvent.click(
      await within(panel()).findByRole("button", { name: "Enable connection" }),
    );
    await confirmPasswordStepUp();
    expect(
      await within(panel()).findByText("Connection enabled."),
    ).toBeInTheDocument();
    const patches = mutations(sent, `/sso-connections/${connectionID}`);
    expect(patches).toHaveLength(2);
    expect(patches.map((entry) => entry.body)).toEqual([
      { status: "enabled" },
      { status: "enabled" },
    ]);
    expect(mutations(sent, "/auth/reauth")).toHaveLength(1);
  });

  it("asks for an AAL2 step-up on an assurance refusal and gives up after one retry", async () => {
    const { fetchMock, sent } = fakeServer(
      { connections: [connection()] },
      (request) =>
        request.method === "PATCH"
          ? problem(403, "auth_assurance_required")
          : undefined,
    );
    renderPanel(fetchMock);
    fireEvent.click(
      await within(panel()).findByRole("button", { name: "Enable connection" }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    expect(dialog).toHaveTextContent("This action needs two-step verification");
    expect(mutations(sent, `/sso-connections/${connectionID}`)).toHaveLength(1);
  });

  it("sends an explicit null JIT role for invitations and student for auto-join", async () => {
    const { fetchMock, sent } = fakeServer({
      connections: [connection({ auto_join_role: null })],
    });
    renderPanel(fetchMock);
    const policy = await within(panel()).findByRole("combobox", {
      name: "When someone new signs in through SSO",
    });
    const save = within(panel()).getByRole("button", { name: "Save policy" });
    expect(save).toBeDisabled();
    fireEvent.keyDown(policy, { key: "ArrowDown" });
    fireEvent.click(
      await screen.findByRole("option", {
        name: "Add automatically as a student",
      }),
    );
    fireEvent.click(save);
    expect(
      await within(panel()).findByText("Join policy saved."),
    ).toBeInTheDocument();

    const reloaded = await within(panel()).findByRole("combobox", {
      name: "When someone new signs in through SSO",
    });
    fireEvent.keyDown(reloaded, { key: "ArrowDown" });
    fireEvent.click(
      await screen.findByRole("option", { name: "Require an invitation" }),
    );
    fireEvent.click(
      within(panel()).getByRole("button", { name: "Save policy" }),
    );
    await waitFor(() =>
      expect(mutations(sent, `/sso-connections/${connectionID}`)).toHaveLength(
        2,
      ),
    );
    expect(
      mutations(sent, `/sso-connections/${connectionID}`).map(
        (entry) => entry.body,
      ),
    ).toEqual([{ auto_join_role: "student" }, { auto_join_role: null }]);
  });

  it("maps conflicts, invalid values, rate limits and outages to clear messages", async () => {
    const answers = [
      problem(409, "auth_sso_conflict"),
      problem(400, "auth_sso_invalid"),
      problem(429, undefined, { "Retry-After": "42" }),
      problem(503),
    ];
    const { fetchMock } = fakeServer(
      { connections: [connection()] },
      (request) => (request.method === "PATCH" ? answers.shift() : undefined),
    );
    renderPanel(fetchMock);
    const button = await within(panel()).findByRole("button", {
      name: "Enable connection",
    });
    for (const text of [
      "This change conflicts with the current state",
      "Some values are invalid.",
      "Too many attempts. Try again in 42 seconds.",
      "School SSO or the DNS lookup is temporarily unavailable.",
    ]) {
      fireEvent.click(button);
      expect(await within(panel()).findByRole("alert")).toHaveTextContent(text);
    }
  });

  it("offers retry on a load error and conceals data on 404", async () => {
    let failures = 1;
    const { fetchMock } = fakeServer(
      { connections: [connection()] },
      (request) => {
        if (request.url.endsWith("/sso-domains") && failures > 0) {
          failures -= 1;
          return problem(503);
        }
        return undefined;
      },
    );
    renderPanel(fetchMock);
    expect(
      await within(panel()).findByText("School SSO settings unavailable"),
    ).toBeInTheDocument();
    fireEvent.click(within(panel()).getByRole("button", { name: "Try again" }));
    expect(
      await within(panel()).findByRole("heading", { name: "Google Workspace" }),
    ).toBeInTheDocument();

    cleanup();
    const hidden = fakeServer({ connections: [connection()] }, (request) =>
      request.url.includes("/sso-") ? problem(404) : undefined,
    );
    renderPanel(hidden.fetchMock);
    expect(
      await within(panel()).findByText("School SSO management unavailable"),
    ).toBeInTheDocument();
    expect(
      within(panel()).queryByText("school.example"),
    ).not.toBeInTheDocument();
  });

  it("renders Vietnamese copy", async () => {
    const { fetchMock } = fakeServer({
      connections: [connection({ status: "enabled", enforce_sso: true })],
      domains: [
        domain({ status: "verified", verified_at: "2026-11-21T01:05:00Z" }),
      ],
    });
    renderPanel(fetchMock, session(), "vi");
    const region = await screen.findByRole("region", {
      name: "SSO trường học",
    });
    expect(await within(region).findByText("Đã xác minh")).toBeInTheDocument();
    expect(within(region).getByText("Bắt buộc SSO")).toBeInTheDocument();
    expect(within(region).getByText("Đang bật")).toBeInTheDocument();
  });
});
