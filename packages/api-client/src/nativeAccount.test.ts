import { afterEach, describe, expect, it, vi } from "vitest";
import {
  completePasswordRecovery,
  getCurrentCSRFToken,
  getMyPasswordStatus,
  rotateCSRFToken,
  startEmailCodeLogin,
  startPasswordRecovery,
  updateMyPassword,
  verifyEmailCodeLogin,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

const challenge = {
  challenge_id: "d7b43df4-1b91-4d06-b106-bd34bdc806a5",
  cooldown_seconds: 60,
  expires_in_seconds: 600,
};
const updatedAt = "2026-11-17T10:00:00Z";
function reply(body: unknown, status = 200, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

describe("email code and password account client", () => {
  it.each([
    [startEmailCodeLogin, "/api/v1/auth/email-code/start"],
    [startPasswordRecovery, "/api/v1/auth/password/recovery/start"],
  ] as const)(
    "starts with pre-auth JSON and cookies (%s)",
    async (start, path) => {
      const fetchMock = vi.fn().mockResolvedValue(reply(challenge, 202));
      vi.stubGlobal("fetch", fetchMock);
      vi.stubGlobal("location", { origin: "https://app.example" });
      const input = { email: "private@example.test" };
      await expect(start(input)).resolves.toEqual(challenge);
      expect(fetchMock).toHaveBeenCalledTimes(1);
      const request = fetchMock.mock.calls[0]?.[0] as Request;
      expect(new URL(request.url).pathname).toBe(path);
      expect(request.url).not.toContain("private");
      expect(request.credentials).toBe("include");
      expect(request.cache).toBe("no-store");
      expect(request.referrerPolicy).toBe("no-referrer");
      expect(request.headers.get("Origin")).toBe("https://app.example");
      expect(request.headers.has("X-CSRF-Token")).toBe(false);
      expect(await request.json()).toEqual(input);
    },
  );

  it("clears stale CSRF only when email-code verification authenticates", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "old-csrf" }))
      .mockResolvedValueOnce(
        reply({ status: "mfa_required", methods: ["totp"] }),
      )
      .mockResolvedValueOnce(reply({ status: "authenticated" }));
    vi.stubGlobal("fetch", fetchMock);
    await rotateCSRFToken();
    const input = {
      challenge_id: challenge.challenge_id,
      code: "123456",
      keep_signed_in: true,
    };
    const pending = await verifyEmailCodeLogin(input);
    expect(pending.status).toBe("mfa_required");
    if (pending.status === "mfa_required")
      expect(pending.methods).toEqual(["totp"]);
    expect(getCurrentCSRFToken()).toBe("old-csrf");
    await expect(verifyEmailCodeLogin(input)).resolves.toEqual({
      status: "authenticated",
    });
    expect(getCurrentCSRFToken()).toBeNull();
    for (const call of fetchMock.mock.calls.slice(1)) {
      const request = call[0] as Request;
      expect(new URL(request.url).pathname).toBe(
        "/api/v1/auth/email-code/verify",
      );
      expect(await request.json()).toEqual(input);
      expect(request.credentials).toBe("include");
    }
  });

  it("clears CSRF after password reset without performing a login", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "old-session-csrf" }))
      .mockResolvedValueOnce(
        reply({ status: "mfa_required", methods: ["passkey"] }),
      )
      .mockResolvedValueOnce(reply({ status: "password_reset" }));
    vi.stubGlobal("fetch", fetchMock);
    await rotateCSRFToken();
    const input = {
      challenge_id: challenge.challenge_id,
      code: "123456",
      new_password: "new private password",
    };
    const continuation = await completePasswordRecovery(input);
    expect(continuation.status).toBe("mfa_required");
    expect(getCurrentCSRFToken()).toBe("old-session-csrf");
    await expect(completePasswordRecovery(input)).resolves.toEqual({
      status: "password_reset",
    });
    expect(getCurrentCSRFToken()).toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(3);
    const request = fetchMock.mock.calls[2]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/password/recovery/complete",
    );
    expect(await request.json()).toEqual(input);
    expect(request.url).not.toContain("private");
  });

  it("reads password status with no-store even when password auth is disabled", async () => {
    const status = {
      password_enabled: false,
      has_password: true,
      updated_at: updatedAt,
    };
    const fetchMock = vi.fn().mockResolvedValue(reply(status));
    vi.stubGlobal("fetch", fetchMock);
    await expect(getMyPasswordStatus()).resolves.toEqual(status);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe("/api/v1/me/password");
    expect(request.method).toBe("GET");
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
  });

  it.each([undefined, "old private password"])(
    "sets or changes a password and invalidates CSRF after rotation",
    async (currentPassword) => {
      const updated = { has_password: true, updated_at: updatedAt };
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(reply({ csrf_token: "current-csrf" }))
        .mockResolvedValueOnce(reply(updated));
      vi.stubGlobal("fetch", fetchMock);
      await rotateCSRFToken();
      const input = {
        new_password: "new private password",
        ...(currentPassword ? { current_password: currentPassword } : {}),
      };
      await expect(updateMyPassword(input, "current-csrf")).resolves.toEqual(
        updated,
      );
      expect(getCurrentCSRFToken()).toBeNull();
      const request = fetchMock.mock.calls[1]?.[0] as Request;
      expect(new URL(request.url).pathname).toBe("/api/v1/me/password");
      expect(request.method).toBe("POST");
      expect(request.headers.get("X-CSRF-Token")).toBe("current-csrf");
      expect(request.cache).toBe("no-store");
      expect(await request.json()).toEqual(input);
    },
  );

  it("preserves actionable error codes, retry delay and the existing CSRF on failure", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "still-valid" }))
      .mockResolvedValueOnce(
        reply(
          {
            status: 401,
            type: "urn:tutorhub:problem:http-401",
            title: "Invalid current password",
            code: "auth_invalid_current_password",
          },
          401,
        ),
      )
      .mockResolvedValueOnce(
        reply(
          {
            status: 400,
            type: "urn:tutorhub:problem:http-400",
            title: "Invalid password",
            code: "auth_password_policy",
          },
          400,
        ),
      )
      .mockResolvedValueOnce(
        reply(
          {
            type: "urn:tutorhub:problem:http-503",
            status: 503,
            title: "Authentication busy",
          },
          503,
          {
            "Retry-After": "2",
          },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    await rotateCSRFToken();
    await expect(
      updateMyPassword({ new_password: "secret" }, "still-valid"),
    ).rejects.toMatchObject({
      status: 401,
      problem: { code: "auth_invalid_current_password" },
    });
    await expect(
      completePasswordRecovery({
        challenge_id: challenge.challenge_id,
        code: "123456",
        new_password: "secret",
      }),
    ).rejects.toMatchObject({
      status: 400,
      problem: { code: "auth_password_policy" },
    });
    await expect(
      verifyEmailCodeLogin({
        challenge_id: challenge.challenge_id,
        code: "123456",
      }),
    ).rejects.toMatchObject({ status: 503, retryAfterSeconds: 2 });
    expect(getCurrentCSRFToken()).toBe("still-valid");
  });

  it("forwards abort signals without another authentication lookup", async () => {
    const controller = new AbortController();
    controller.abort();
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      expect(request.signal.aborted).toBe(true);
      return Promise.reject(new DOMException("Aborted", "AbortError"));
    });
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      getMyPasswordStatus({ signal: controller.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
