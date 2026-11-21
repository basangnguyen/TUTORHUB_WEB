import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  buildOAuthStartURL,
  createSchoolSSOConnection,
  createSchoolSSODomain,
  createTutorHubClient,
  disableSchoolSSODomain,
  discoverAuthIdentifier,
  getAuthProviders,
  listSchoolSSOConnections,
  listSchoolSSODomains,
  rotateSchoolSSODomain,
  updateSchoolSSOConnection,
  verifySchoolSSODomain,
  type AuthProvidersResponse,
  type SSOChallenge,
  type SSOConnection,
  type SSODomain,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

const TENANT = "8a4f0c7e-0d55-4c2d-9a3e-5f3d2b1c0a11";
const CONNECTION = "2f6a9b1c-3d4e-4f50-8a61-7b8c9d0e1f23";
const DOMAIN_ID = "6c1d2e3f-4a5b-4c6d-8e7f-90a1b2c3d4e5";

const connection: SSOConnection = {
  id: CONNECTION,
  tenant_id: TENANT,
  kind: "google_workspace",
  status: "disabled",
  allowed_org_ids: ["school.example"],
  enforce_sso: false,
  auto_join_role: null,
  created_at: "2026-11-21T00:00:00Z",
  updated_at: "2026-11-21T00:00:00Z",
};

const domain: SSODomain = {
  id: DOMAIN_ID,
  tenant_id: TENANT,
  connection_id: CONNECTION,
  domain: "school.example",
  status: "pending",
  verified_at: null,
  created_at: "2026-11-21T00:00:00Z",
  updated_at: "2026-11-21T00:00:00Z",
};

const challenge: SSOChallenge = {
  domain,
  txt_name: "_tutorhub-challenge.school.example.",
  txt_value: "tutorhub-site-verification=one-time-value",
};

function problem(status: number, code: string, headers: HeadersInit = {}) {
  return new Response(
    JSON.stringify({ type: "about:blank", title: "Rejected", status, code }),
    {
      status,
      headers: { "Content-Type": "application/problem+json", ...headers },
    },
  );
}

describe("school SSO administration client", () => {
  function stubFetch(body: unknown, status = 200) {
    vi.stubGlobal("location", { origin: "https://app.example" });
    const fetchMock = vi
      .fn()
      .mockResolvedValue(Response.json(body, { status: status }));
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  function sentRequest(fetchMock: ReturnType<typeof vi.fn>) {
    return fetchMock.mock.calls[0]?.[0] as Request;
  }

  it("lists connections and domains with credentials, no cache and no CSRF", async () => {
    const connections = stubFetch({ items: [connection] });
    await expect(listSchoolSSOConnections(TENANT)).resolves.toEqual({
      items: [connection],
    });
    let request = sentRequest(connections);
    expect(request.method).toBe("GET");
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/tenants/${TENANT}/sso-connections`,
    );
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
    expect(request.headers.has("X-CSRF-Token")).toBe(false);

    const domains = stubFetch({ items: [domain] });
    await expect(listSchoolSSODomains(TENANT)).resolves.toEqual({
      items: [domain],
    });
    request = sentRequest(domains);
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/tenants/${TENANT}/sso-domains`,
    );
    expect(request.cache).toBe("no-store");
  });

  it("refuses an empty tenant scope before any request", async () => {
    const fetchMock = stubFetch({ items: [] });
    await expect(listSchoolSSOConnections(" ")).rejects.toThrow(TypeError);
    await expect(
      createSchoolSSODomain(
        "",
        { connection_id: CONNECTION, domain: "school.example" },
        "csrf",
      ),
    ).rejects.toThrow(TypeError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("creates a connection with CSRF, Origin and the exact JSON body", async () => {
    const fetchMock = stubFetch(connection, 201);
    await expect(
      createSchoolSSOConnection(
        TENANT,
        { kind: "google_workspace", allowed_org_ids: ["school.example"] },
        "csrf-1",
      ),
    ).resolves.toEqual(connection);
    const request = sentRequest(fetchMock);
    expect(request.method).toBe("POST");
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/tenants/${TENANT}/sso-connections`,
    );
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-1");
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.cache).toBe("no-store");
    expect(request.referrerPolicy).toBe("no-referrer");
    expect(await request.json()).toEqual({
      kind: "google_workspace",
      allowed_org_ids: ["school.example"],
    });
  });

  it("patches only the given policy keys and keeps an explicit null JIT role", async () => {
    const fetchMock = stubFetch({
      ...connection,
      enforce_sso: true,
      status: "enabled",
    });
    await updateSchoolSSOConnection(
      TENANT,
      CONNECTION,
      { enforce_sso: true, auto_join_role: null },
      "csrf-2",
    );
    const request = sentRequest(fetchMock);
    expect(request.method).toBe("PATCH");
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/tenants/${TENANT}/sso-connections/${CONNECTION}`,
    );
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-2");
    expect(await request.json()).toEqual({
      enforce_sso: true,
      auto_join_role: null,
    });
  });

  it("returns the one-time TXT challenge for create and rotate", async () => {
    let fetchMock = stubFetch(challenge, 201);
    await expect(
      createSchoolSSODomain(
        TENANT,
        { connection_id: CONNECTION, domain: "school.example" },
        "csrf-3",
      ),
    ).resolves.toEqual(challenge);
    let request = sentRequest(fetchMock);
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/tenants/${TENANT}/sso-domains`,
    );
    expect(await request.json()).toEqual({
      connection_id: CONNECTION,
      domain: "school.example",
    });

    fetchMock = stubFetch(challenge);
    await expect(
      rotateSchoolSSODomain(TENANT, DOMAIN_ID, "csrf-4"),
    ).resolves.toEqual(challenge);
    request = sentRequest(fetchMock);
    expect(request.method).toBe("POST");
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/tenants/${TENANT}/sso-domains/${DOMAIN_ID}/rotate`,
    );
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-4");
    expect(await request.json()).toEqual({});
  });

  it("verifies and disables a domain with an empty JSON body", async () => {
    for (const [call, verb] of [
      [verifySchoolSSODomain, "verify"],
      [disableSchoolSSODomain, "disable"],
    ] as const) {
      const fetchMock = stubFetch({ ...domain, status: "verified" });
      await expect(call(TENANT, DOMAIN_ID, `csrf-${verb}`)).resolves.toEqual({
        ...domain,
        status: "verified",
      });
      const request = sentRequest(fetchMock);
      expect(new URL(request.url).pathname).toBe(
        `/api/v1/tenants/${TENANT}/sso-domains/${DOMAIN_ID}/${verb}`,
      );
      expect(request.headers.get("X-CSRF-Token")).toBe(`csrf-${verb}`);
      expect(request.headers.get("Origin")).toBe("https://app.example");
      expect(await request.json()).toEqual({});
    }
  });

  it("surfaces coded problems for step-up, lockout, DNS and rate limits", async () => {
    vi.stubGlobal("location", { origin: "https://app.example" });
    for (const [status, code, headers] of [
      [403, "auth_recent_authentication_required", {}],
      [403, "auth_assurance_required", {}],
      [409, "auth_sso_lockout", {}],
      [409, "auth_sso_dns_mismatch", {}],
      [429, "rate_limited", { "Retry-After": "30" }],
    ] as const) {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(problem(status, code, headers)),
      );
      const error = await verifySchoolSSODomain(
        TENANT,
        DOMAIN_ID,
        "csrf",
      ).catch((cause: unknown) => cause);
      expect(error).toBeInstanceOf(APIRequestError);
      expect(error).toMatchObject({ status, problem: { code } });
      if (status === 429) {
        expect(error).toMatchObject({ retryAfterSeconds: 30 });
      }
    }
  });
});

describe("authentication foundation client", () => {
  it("routes school sign-in using the public discovery response", async () => {
    vi.stubGlobal("location", { origin: "https://app.example" });
    const email = "anna+class@school.example";
    const sso = {
      required: true,
      available: true,
      provider: "google",
      domain: "school.example",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ kind: "email", sso })),
    );
    const result = await discoverAuthIdentifier({ identifier: email });
    expect(result.sso).toEqual(sso);
    if (!result.sso.provider) throw new Error("missing school provider");
    const url = new URL(
      buildOAuthStartURL(result.sso.provider, {
        loginHint: email,
        domain: result.sso.domain,
      }),
    );
    expect(url.pathname).toBe("/api/v1/auth/oauth/google/start");
    expect(url.searchParams.get("login_hint")).toBe(email);
    expect(url.searchParams.get("domain")).toBe("school.example");
  });

  it("sends a tenant-scoped policy change and preserves an explicit null JIT role", async () => {
    vi.stubGlobal("location", { origin: "https://app.example" });
    const fetch = vi
      .fn()
      .mockResolvedValue(Response.json({ auto_join_role: null }));
    const client = createTutorHubClient({ fetch });
    const response = await client.PATCH(
      "/api/v1/tenants/{tenant_id}/sso-connections/{connection_id}",
      {
        params: {
          path: { tenant_id: "school-a", connection_id: "connection-a" },
          header: {
            Origin: "https://app.example",
            "X-CSRF-Token": "test-csrf",
          },
        },
        body: { auto_join_role: null },
      },
    );
    expect(response.data?.auto_join_role).toBeNull();
    const request = fetch.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/tenants/school-a/sso-connections/connection-a",
    );
    expect(request.headers.get("X-CSRF-Token")).toBe("test-csrf");
    expect(await request.json()).toEqual({ auto_join_role: null });
  });

  it("reads public capabilities with credentials and no cache", async () => {
    const payload: AuthProvidersResponse = {
      signup_enabled: false,
      password_enabled: false,
      email_code_enabled: false,
      phone_enabled: false,
      passkeys_enabled: false,
      mfa_totp_enabled: false,
      oidc_default_enabled: true,
      sso_domains_enabled: false,
      oauth_providers: [],
      password_min_length: 8,
      phone_allowed_countries: ["VN"],
      legal_terms_version: "2026-11-16-v2.0",
    };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(payload), {
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await expect(getAuthProviders()).resolves.toEqual(payload);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe("/api/v1/auth/providers");
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
  });

  it("posts the identifier as JSON without a session-CSRF preflight", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ kind: "email", sso: { required: false } }),
          { headers: { "Content-Type": "application/json" } },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("location", { origin: "https://app.example" });
    await expect(
      discoverAuthIdentifier({ identifier: "student@example.test" }),
    ).resolves.toEqual({ kind: "email", sso: { required: false } });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.method).toBe("POST");
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/identifier/discover",
    );
    expect(request.headers.get("Content-Type")).toBe("application/json");
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.headers.has("X-CSRF-Token")).toBe(false);
    expect(await request.json()).toEqual({
      identifier: "student@example.test",
    });
    expect(request.cache).toBe("no-store");
    expect(request.referrerPolicy).toBe("no-referrer");
  });

  it("preserves structured rate limit errors and Retry-After", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            type: "urn:tutorhub:problem:http-429",
            title: "Too many requests",
            status: 429,
          }),
          {
            status: 429,
            headers: {
              "Content-Type": "application/problem+json",
              "Retry-After": "60",
            },
          },
        ),
      ),
    );
    const error = await discoverAuthIdentifier({
      identifier: "+84912345678",
    }).catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(APIRequestError);
    expect(error).toMatchObject({ status: 429, retryAfterSeconds: 60 });
  });
});
