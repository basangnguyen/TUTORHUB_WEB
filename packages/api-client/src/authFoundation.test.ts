import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  buildOAuthStartURL,
  createTutorHubClient,
  discoverAuthIdentifier,
  getAuthProviders,
  type AuthProvidersResponse,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

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
