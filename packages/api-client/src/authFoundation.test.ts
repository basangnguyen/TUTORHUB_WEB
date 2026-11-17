import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  discoverAuthIdentifier,
  getAuthProviders,
  type AuthProvidersResponse,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

describe("authentication foundation client", () => {
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
