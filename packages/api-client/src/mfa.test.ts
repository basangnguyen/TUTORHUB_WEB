import { afterEach, expect, it, vi } from "vitest";
import {
  confirmTOTP,
  disableTOTP,
  enrollTOTP,
  getCurrentCSRFToken,
  getPendingMFA,
  reauthenticate,
  regenerateRecoveryCodes,
  rotateCSRFToken,
  startMFAPasskey,
  verifyMFA,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

function reply(body: unknown, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

it("reads MFA state and requests UV options using only the bound cookie", async () => {
  const state = { methods: ["totp", "passkey"], expires_in_seconds: 219 };
  const options = {
    challenge: "fresh",
    userVerification: "required",
    allowCredentials: [],
  };
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(reply(state))
    .mockResolvedValueOnce(reply(options));
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("location", { origin: "https://app.example" });
  await expect(getPendingMFA()).resolves.toEqual(state);
  await expect(startMFAPasskey()).resolves.toEqual(options);
  const get = fetchMock.mock.calls[0]?.[0] as Request;
  const post = fetchMock.mock.calls[1]?.[0] as Request;
  expect(new URL(get.url).pathname).toBe("/api/v1/auth/mfa");
  expect(get.cache).toBe("no-store");
  expect(get.referrerPolicy).toBe("no-referrer");
  expect(post.headers.get("Origin")).toBe("https://app.example");
  expect(post.credentials).toBe("include");
  expect(new URL(post.url).pathname).toBe("/api/v1/auth/mfa/passkey/options");
  expect(await post.json()).toEqual({});
});

it.each([
  [
    () => confirmTOTP("123456", "existing"),
    "/api/v1/me/mfa/totp/confirm",
    { code: "123456" },
    { recovery_codes: ["one-time"] },
  ],
  [
    () => verifyMFA({ method: "totp", code: "123456" }),
    "/api/v1/auth/mfa/verify",
    { method: "totp", code: "123456" },
    { status: "authenticated", return_to: "/app/classes" },
  ],
  [
    () => verifyMFA({ method: "recovery_code", code: "sample" }),
    "/api/v1/auth/mfa/verify",
    { method: "recovery_code", code: "sample" },
    { status: "password_reset" },
  ],
  [
    () => reauthenticate({ method: "totp", code: "123456" }, "existing"),
    "/api/v1/auth/reauth",
    { method: "totp", code: "123456" },
    { recent_auth_expires_at: "2026-11-19T01:00:00Z" },
  ],
] as const)(
  "clears stale CSRF only after successful proof at %s",
  async (verify, path, body, result) => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "existing" }))
      .mockResolvedValueOnce(
        reply({ title: "Invalid code", status: 401, type: "about:blank" }, 401),
      )
      .mockResolvedValueOnce(reply(result));
    vi.stubGlobal("fetch", fetchMock);
    await rotateCSRFToken();
    await expect(verify()).rejects.toMatchObject({ status: 401 });
    expect(getCurrentCSRFToken()).toBe("existing");
    await expect(verify()).resolves.toEqual(result);
    const req = fetchMock.mock.calls[2]?.[0] as Request;
    expect(new URL(req.url).pathname).toBe(path);
    expect(await req.json()).toEqual(body);
    expect(getCurrentCSRFToken()).toBeNull();
  },
);

it("sends CSRF on every management mutation and never caches enrollment data", async () => {
  const enrollment = {
    secret: "private",
    otpauth_uri: "otpauth://totp/Example",
    issuer: "Example",
    account_name: "account",
  };
  const codes = { recovery_codes: ["replacement"] };
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(reply(enrollment))
    .mockResolvedValueOnce(reply(codes))
    .mockResolvedValueOnce(reply(null, 204));
  vi.stubGlobal("fetch", fetchMock);
  await expect(enrollTOTP("csrf")).resolves.toEqual(enrollment);
  await expect(regenerateRecoveryCodes("csrf")).resolves.toEqual(codes);
  await expect(disableTOTP("csrf")).resolves.toBeUndefined();
  const requests = fetchMock.mock.calls.map(([r]) => r as Request);
  expect(requests.map((r) => [r.method, new URL(r.url).pathname])).toEqual([
    ["POST", "/api/v1/me/mfa/totp/enroll"],
    ["POST", "/api/v1/me/mfa/recovery-codes"],
    ["DELETE", "/api/v1/me/mfa/totp"],
  ]);
  for (const r of requests) {
    expect(r.cache).toBe("no-store");
    expect(r.credentials).toBe("include");
    expect(r.headers.get("X-CSRF-Token")).toBe("csrf");
  }
});
