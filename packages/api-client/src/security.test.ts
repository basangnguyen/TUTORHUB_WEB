import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getMySecurity,
  listMyLoginMethods,
  listMySessions,
  startReauthEmailCode,
  reauthenticate,
  revokeMySession,
  revokeOtherSessions,
  rotateCSRFToken,
  getCurrentCSRFToken,
  type ReauthenticateRequest,
} from "./index";

afterEach(() => vi.unstubAllGlobals());
const challengeID = "7dd14656-fcfb-4ce9-a11c-b3e412593cf3";
const expires = "2026-11-18T08:10:00Z";
function reply(body: unknown, status = 200, headers = {}) {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

describe("security and device management client", () => {
  it.each([
    [
      getMySecurity,
      "/api/v1/me/security",
      {
        has_password: true,
        email_verified: true,
        mfa_enabled: false,
        reauth_methods: ["password"],
        recent_auth_expires_at: null,
      },
    ],
    [
      listMyLoginMethods,
      "/api/v1/me/login-methods",
      { password: { has_password: false, updated_at: null }, identities: [] },
    ],
    [listMySessions, "/api/v1/me/sessions", { sessions: [] }],
  ] as const)(
    "reads private data without cache (%s)",
    async (read, path, payload) => {
      const fetchMock = vi.fn().mockResolvedValue(reply(payload));
      vi.stubGlobal("fetch", fetchMock);
      await expect(read()).resolves.toEqual(payload);
      const request = fetchMock.mock.calls[0]?.[0] as Request;
      expect(request.method).toBe("GET");
      expect(new URL(request.url).pathname).toBe(path);
      expect(request.credentials).toBe("include");
      expect(request.cache).toBe("no-store");
      expect(request.referrerPolicy).toBe("no-referrer");
    },
  );

  it("starts and resends reauth without accepting a destination", async () => {
    const challenge = {
      challenge_id: challengeID,
      cooldown_seconds: 60,
      expires_in_seconds: 600,
    };
    const fetchMock = vi.fn().mockResolvedValue(reply(challenge, 202));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("location", { origin: "https://app.example" });
    await expect(startReauthEmailCode("current-csrf")).resolves.toEqual(
      challenge,
    );
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/reauth/email-code/start",
    );
    expect(request.method).toBe("POST");
    expect(request.headers.get("X-CSRF-Token")).toBe("current-csrf");
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
    expect(await request.json()).toEqual({});
  });

  it.each<ReauthenticateRequest>([
    { method: "password", password: "private-password" },
    { method: "email_code", challenge_id: challengeID, code: "123456" },
  ])("reauthenticates with $method and clears rotated CSRF", async (input) => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "old-csrf" }))
      .mockResolvedValueOnce(reply({ recent_auth_expires_at: expires }));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("location", { origin: "https://app.example" });
    await rotateCSRFToken();
    await expect(reauthenticate(input, "old-csrf")).resolves.toEqual({
      recent_auth_expires_at: expires,
    });
    expect(getCurrentCSRFToken()).toBeNull();
    const request = fetchMock.mock.calls[1]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe("/api/v1/auth/reauth");
    expect(request.url).not.toContain("private");
    expect(request.url).not.toContain("123456");
    expect(await request.json()).toEqual(input);
    expect(request.headers.get("X-CSRF-Token")).toBe("old-csrf");
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.cache).toBe("no-store");
    expect(request.referrerPolicy).toBe("no-referrer");
  });

  it.each(["auth_invalid_current_password", "auth_invalid_verification_code"])(
    "preserves session CSRF for proof error %s",
    async (code) => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(reply({ csrf_token: "still-valid" }))
        .mockResolvedValueOnce(
          reply(
            {
              title: "Verification failed",
              status: 401,
              type: "about:blank",
              code,
            },
            401,
          ),
        );
      vi.stubGlobal("fetch", fetchMock);
      await rotateCSRFToken();
      await expect(
        reauthenticate(
          { method: "password", password: "wrong" },
          "still-valid",
        ),
      ).rejects.toMatchObject({ status: 401, problem: { code } });
      expect(getCurrentCSRFToken()).toBe("still-valid");
    },
  );

  it("revokes an opaque session ID and handles 204 without resetting current CSRF", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "current-csrf" }))
      .mockResolvedValueOnce(reply(null, 204));
    vi.stubGlobal("fetch", fetchMock);
    await rotateCSRFToken();
    await expect(
      revokeMySession(challengeID, "current-csrf"),
    ).resolves.toBeUndefined();
    const request = fetchMock.mock.calls[1]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/me/sessions/${challengeID}/revoke`,
    );
    expect(request.headers.get("X-CSRF-Token")).toBe("current-csrf");
    expect(await request.json()).toEqual({});
    expect(getCurrentCSRFToken()).toBe("current-csrf");
  });

  it("revokes other sessions and returns the affected count", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply({ revoked_count: 3 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(revokeOtherSessions("current-csrf")).resolves.toEqual({
      revoked_count: 3,
    });
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/me/sessions/revoke-others",
    );
    expect(await request.json()).toEqual({});
  });

  it("keeps the current-session revoke error and rate limit Retry-After", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        reply(
          {
            type: "about:blank",
            title: "Use logout",
            status: 400,
            code: "auth_cannot_revoke_current_session",
          },
          400,
        ),
      )
      .mockResolvedValueOnce(
        reply(
          { type: "about:blank", title: "Too many requests", status: 429 },
          429,
          { "Retry-After": "60" },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    await expect(revokeMySession(challengeID, "csrf")).rejects.toMatchObject({
      status: 400,
      problem: { code: "auth_cannot_revoke_current_session" },
    });
    await expect(revokeOtherSessions("csrf")).rejects.toMatchObject({
      status: 429,
      retryAfterSeconds: 60,
    });
  });

  it("passes AbortSignal and never retries a secret mutation", async () => {
    const controller = new AbortController();
    const fetchMock = vi
      .fn()
      .mockRejectedValue(new DOMException("Aborted", "AbortError"));
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      reauthenticate({ method: "password", password: "private" }, "csrf", {
        signal: controller.signal,
      }),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    controller.abort();
    expect(request.signal.aborted).toBe(true);
  });
});
