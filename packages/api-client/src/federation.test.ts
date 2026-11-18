import { afterEach, describe, expect, it, vi } from "vitest";
import {
  beginIdentityLink,
  buildOAuthStartURL,
  cancelLinkIntent,
  confirmLinkIntent,
  getCurrentCSRFToken,
  getLinkIntent,
  linkIdentity,
  rotateCSRFToken,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

function reply(body: unknown, status = 200, headers = {}) {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

describe("federation client", () => {
  it.each(["login", "reauth"] as const)(
    "builds a normalized Google %s navigation URL",
    (intent) => {
      const url = new URL(
        buildOAuthStartURL(
          "google",
          { returnTo: "/app/home?class=one&tab=two", intent },
          { baseUrl: "https://api.example/api" },
        ),
      );
      expect(url.origin).toBe("https://api.example");
      expect(url.pathname).toBe("/api/v1/auth/oauth/google/start");
      expect(url.searchParams.get("return_to")).toBe(
        "/app/home?class=one&tab=two",
      );
      expect(url.searchParams.get("intent")).toBe(intent);
    },
  );

  it("retains the OIDC provider option and login defaults", () => {
    const url = new URL(
      buildOAuthStartURL("oidc", {}, { baseUrl: "https://api.example" }),
    );
    expect(url.pathname).toBe("/api/v1/auth/oauth/oidc/start");
    expect(url.searchParams.get("return_to")).toBe("/app/home");
    expect(url.searchParams.get("intent")).toBe("login");
  });

  it("reads only the masked linking preview without caching", async () => {
    const preview = {
      provider: "google",
      masked_email: "s***@e***.test",
      expires_in_seconds: 600,
    };
    const fetchMock = vi.fn().mockResolvedValue(reply(preview));
    vi.stubGlobal("fetch", fetchMock);
    await expect(getLinkIntent()).resolves.toEqual(preview);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe("/api/v1/auth/link-intent");
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
    expect(request.referrerPolicy).toBe("no-referrer");
    expect(request.headers.has("Cookie")).toBe(false);
  });

  it("confirms with CSRF and Origin and invalidates cached CSRF on success", async () => {
    const result = { status: "linked", return_to: "/app/home" };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "old-csrf" }))
      .mockResolvedValueOnce(reply(result));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("location", { origin: "https://app.example" });
    await rotateCSRFToken();
    await expect(confirmLinkIntent("old-csrf")).resolves.toEqual(result);
    const request = fetchMock.mock.calls[1]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/link-intent/confirm",
    );
    expect(request.method).toBe("POST");
    expect(request.headers.get("X-CSRF-Token")).toBe("old-csrf");
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
    expect(await request.json()).toEqual({});
    expect(getCurrentCSRFToken()).toBeNull();
  });

  it.each([
    [403, "auth_link_intent_mismatch"],
    [403, "auth_assurance_required"],
    [400, "auth_link_intent_unavailable"],
  ] as const)(
    "preserves session state on confirmation error %s/%s",
    async (status, code) => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(reply({ csrf_token: "valid-csrf" }))
        .mockResolvedValueOnce(
          reply(
            { type: "about:blank", title: "Link rejected", status, code },
            status,
          ),
        );
      vi.stubGlobal("fetch", fetchMock);
      await rotateCSRFToken();
      await expect(confirmLinkIntent("valid-csrf")).rejects.toMatchObject({
        status,
        problem: { code },
      });
      expect(getCurrentCSRFToken()).toBe("valid-csrf");
    },
  );

  it("cancels using the browser cookie and Origin without requiring a session", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(null, 204));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("location", { origin: "https://app.example" });
    await expect(cancelLinkIntent()).resolves.toBeUndefined();
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/link-intent/cancel",
    );
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.headers.has("X-CSRF-Token")).toBe(false);
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
    expect(await request.json()).toEqual({});
  });

  it("preserves rate limit information and unavailable preview errors", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        reply(
          {
            type: "about:blank",
            title: "Unavailable",
            status: 404,
            code: "auth_link_intent_unavailable",
          },
          404,
        ),
      )
      .mockResolvedValueOnce(
        reply({ type: "about:blank", title: "Wait", status: 429 }, 429, {
          "Retry-After": "60",
        }),
      );
    vi.stubGlobal("fetch", fetchMock);
    await expect(getLinkIntent()).rejects.toMatchObject({
      status: 404,
      problem: { code: "auth_link_intent_unavailable" },
    });
    await expect(cancelLinkIntent()).rejects.toMatchObject({
      status: 429,
      retryAfterSeconds: 60,
    });
  });

  it.each([{}, { provider: "google" }, { provider: "oidc" }] as const)(
    "starts a settings link with explicit optional provider %j",
    async (input) => {
      const result = {
        authorization_url: "https://provider.example/authorize",
      };
      const fetchMock = vi.fn().mockResolvedValue(reply(result));
      vi.stubGlobal("fetch", fetchMock);
      await expect(linkIdentity(input, "csrf-token")).resolves.toEqual(result);
      const request = fetchMock.mock.calls[0]?.[0] as Request;
      expect(new URL(request.url).pathname).toBe("/api/v1/me/identities/link");
      expect(await request.json()).toEqual(input);
      expect(request.headers.get("X-CSRF-Token")).toBe("csrf-token");
    },
  );

  it("keeps the existing beginIdentityLink helper bodyless", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        reply({ authorization_url: "https://oidc.example/authorize" }),
      );
    vi.stubGlobal("fetch", fetchMock);
    await beginIdentityLink("csrf-token");
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(await request.text()).toBe("");
    expect(request.headers.get("X-CSRF-Token")).toBe("csrf-token");
  });
});
