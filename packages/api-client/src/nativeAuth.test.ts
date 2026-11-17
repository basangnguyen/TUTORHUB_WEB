import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  completeEmailSignup,
  getCurrentCSRFToken,
  loginWithPassword,
  resendAuthChallenge,
  rotateCSRFToken,
  startEmailSignup,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

const challenge = {
  challenge_id: "d7b43df4-1b91-4d06-b106-bd34bdc806a5",
  cooldown_seconds: 60,
  expires_in_seconds: 600,
};

function reply(body: unknown, status = 200, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

describe("native authentication client", () => {
  it("starts sign-up with JSON and browser credentials without a session-CSRF lookup", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(challenge, 202));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("location", { origin: "https://app.example" });
    await expect(
      startEmailSignup({ email: "student@example.test" }),
    ).resolves.toEqual(challenge);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/signup/email/start",
    );
    expect(request.method).toBe("POST");
    expect(request.credentials).toBe("include");
    expect(request.cache).toBe("no-store");
    expect(request.referrerPolicy).toBe("no-referrer");
    expect(request.headers.get("Origin")).toBe("https://app.example");
    expect(request.headers.get("Content-Type")).toBe("application/json");
    expect(request.headers.has("X-CSRF-Token")).toBe(false);
    expect(await request.json()).toEqual({ email: "student@example.test" });
  });

  it("resends an empty JSON object so the browser cannot replace the bound destination", async () => {
    const fetchMock = vi.fn().mockResolvedValue(reply(challenge, 202));
    vi.stubGlobal("fetch", fetchMock);
    await expect(resendAuthChallenge()).resolves.toEqual(challenge);
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/challenges/resend",
    );
    expect(await request.json()).toEqual({});
    expect(request.credentials).toBe("include");
  });

  it("completes signup with legal acceptance and clears a stale in-memory CSRF token", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "previous-session-csrf" }))
      .mockResolvedValueOnce(reply({ status: "authenticated" }, 201));
    vi.stubGlobal("fetch", fetchMock);
    await rotateCSRFToken();
    expect(getCurrentCSRFToken()).toBe("previous-session-csrf");
    const input = {
      challenge_id: challenge.challenge_id,
      code: "123456",
      display_name: "Student",
      password: "private password",
      terms_version: "2026-11-16-v2.0",
      keep_signed_in: true,
    };
    await expect(completeEmailSignup(input)).resolves.toEqual({
      status: "authenticated",
    });
    const request = fetchMock.mock.calls[1]?.[0] as Request;
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/auth/signup/email/complete",
    );
    expect(await request.json()).toEqual(input);
    expect(getCurrentCSRFToken()).toBeNull();
  });

  it("supports authenticated and future MFA responses without putting a password in the URL", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ status: "authenticated" }))
      .mockResolvedValueOnce(
        reply({ status: "mfa_required", methods: ["totp"] }),
      );
    vi.stubGlobal("fetch", fetchMock);
    const input = {
      identifier: "student@example.test",
      password: "private password",
      keep_signed_in: true,
    };
    await expect(loginWithPassword(input)).resolves.toEqual({
      status: "authenticated",
    });
    const continuation = await loginWithPassword(input);
    expect(continuation.status).toBe("mfa_required");
    if (continuation.status === "mfa_required")
      expect(continuation.methods).toEqual(["totp"]);
    for (const call of fetchMock.mock.calls) {
      const request = call[0] as Request;
      expect(new URL(request.url).pathname).toBe("/api/v1/auth/password/login");
      expect(request.url).not.toContain("private");
      expect(request.url).not.toContain("student");
      expect(await request.json()).toEqual(input);
    }
  });

  it("preserves legal-version errors and password-capacity Retry-After", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        reply(
          {
            type: "urn:tutorhub:problem:http-400",
            status: 400,
            title: "Terms have changed",
            code: "auth_terms_version_changed",
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
          { "Retry-After": "2" },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);
    await expect(
      completeEmailSignup({
        challenge_id: challenge.challenge_id,
        code: "123456",
        display_name: "Student",
        password: "private password",
        terms_version: "old",
      }),
    ).rejects.toMatchObject({
      status: 400,
      problem: { code: "auth_terms_version_changed" },
    });
    const error = await loginWithPassword({
      identifier: "student@example.test",
      password: "private password",
    }).catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(APIRequestError);
    expect(error).toMatchObject({ status: 503, retryAfterSeconds: 2 });
  });

  it("propagates cancellation to signup requests", async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      expect(request.signal.aborted).toBe(true);
      return Promise.reject(new DOMException("Aborted", "AbortError"));
    });
    vi.stubGlobal("fetch", fetchMock);
    controller.abort();
    await expect(
      startEmailSignup(
        { email: "student@example.test" },
        { signal: controller.signal },
      ),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
