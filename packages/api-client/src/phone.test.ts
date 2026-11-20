import { afterEach, expect, it, vi } from "vitest";
import {
  getCurrentCSRFToken,
  getMyPhone,
  removeMyPhone,
  resendAuthChallenge,
  resendMyPhoneChallenge,
  rotateCSRFToken,
  startMyPhone,
  startPhoneLogin,
  verifyMyPhone,
  verifyPhoneLogin,
} from "./index";

const input = { phone_number: "0912 345 678", country_code: "VN" };
const proof = {
  challenge_id: "76aeb342-b3e1-4dbe-95b1-d725cf01eba2",
  code: "081023",
};
const challenge = {
  challenge_id: proof.challenge_id,
  cooldown_seconds: 60,
  expires_in_seconds: 300,
  masked_destination: "+84 ***5678",
  resend_after_seconds: 60,
};

function reply(body: unknown, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

it("sends login and resend without CSRF and invalidates CSRF only after session creation", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(reply({ csrf_token: "prior" }))
    .mockResolvedValueOnce(reply(challenge, 202))
    .mockResolvedValueOnce(reply(challenge, 202))
    .mockResolvedValueOnce(
      reply({ status: "mfa_required", methods: ["passkey"] }),
    )
    .mockResolvedValueOnce(reply({ status: "authenticated" }));
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("location", { origin: "https://app.example" });
  await rotateCSRFToken();
  await expect(startPhoneLogin(input)).resolves.toEqual(challenge);
  await expect(resendAuthChallenge()).resolves.toEqual(challenge);
  await expect(verifyPhoneLogin(proof)).resolves.toEqual({
    status: "mfa_required",
    methods: ["passkey"],
  });
  expect(getCurrentCSRFToken()).toBe("prior");
  await expect(verifyPhoneLogin(proof)).resolves.toEqual({
    status: "authenticated",
  });
  expect(getCurrentCSRFToken()).toBeNull();
  const requests = fetchMock.mock.calls.slice(1).map(([r]) => r as Request);
  expect(requests.map((r) => new URL(r.url).pathname)).toEqual([
    "/api/v1/auth/phone/start",
    "/api/v1/auth/challenges/resend",
    "/api/v1/auth/phone/verify",
    "/api/v1/auth/phone/verify",
  ]);
  expect(await Promise.all(requests.map((r) => r.json()))).toEqual([
    input,
    {},
    proof,
    proof,
  ]);
  for (const r of requests) {
    expect(r.headers.get("Origin")).toBe("https://app.example");
    expect(r.headers.has("X-CSRF-Token")).toBe(false);
    expect(r.cache).toBe("no-store");
    expect(new URL(r.url).search).toBe("");
    expect(r.credentials).toBe("include");
  }
});

it("reads a nullable binding, rotates CSRF on every management mutation and clears it after removal", async () => {
  const verified = {
    phone: {
      id: "8ce61ff9-c97e-4d5e-9cdc-c2b4ea60ff2b",
      masked_number: "+84 ***5678",
      calling_code: 84,
      country_code: "VN",
      last4: "5678",
      stale: false,
      verified_at: "2026-11-20T00:00:00Z",
      last_used_at: "2026-11-20T00:00:00Z",
    },
  };
  const fetchMock = vi.fn().mockResolvedValueOnce(reply({ phone: null }));
  for (const [i, result] of [
    reply(challenge, 202),
    reply(challenge, 202),
    reply(verified),
    reply(null, 204),
  ].entries()) {
    fetchMock
      .mockResolvedValueOnce(reply({ csrf_token: `fresh-${i}` }))
      .mockResolvedValueOnce(result);
  }
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("location", { origin: "https://app.example" });
  await expect(getMyPhone()).resolves.toEqual({ phone: null });
  await expect(startMyPhone(input)).resolves.toEqual(challenge);
  await expect(resendMyPhoneChallenge()).resolves.toEqual(challenge);
  await expect(verifyMyPhone(proof)).resolves.toEqual(verified);
  expect(getCurrentCSRFToken()).toBe("fresh-2");
  await expect(removeMyPhone()).resolves.toBeUndefined();
  expect(getCurrentCSRFToken()).toBeNull();
  const requests = fetchMock.mock.calls.map(([r]) => r as Request);
  const read = requests[0]!;
  expect([read.method, new URL(read.url).pathname, read.cache]).toEqual([
    "GET",
    "/api/v1/me/phone",
    "no-store",
  ]);
  const mutations = requests.slice(2).filter((_, i) => i % 2 === 0);
  expect(mutations.map((r) => [r.method, new URL(r.url).pathname])).toEqual([
    ["POST", "/api/v1/me/phone/start"],
    ["POST", "/api/v1/auth/challenges/resend"],
    ["POST", "/api/v1/me/phone/verify"],
    ["DELETE", "/api/v1/me/phone"],
  ]);
  expect(await Promise.all(mutations.slice(0, 3).map((r) => r.json()))).toEqual(
    [input, {}, proof],
  );
  for (const [i, r] of mutations.entries()) {
    expect(new URL(requests[i * 2 + 1]!.url).pathname).toBe(
      "/api/v1/auth/csrf",
    );
    expect(r.headers.get("X-CSRF-Token")).toBe(`fresh-${i}`);
    expect(r.headers.get("Origin")).toBe("https://app.example");
    expect(r.cache).toBe("no-store");
    expect(r.referrerPolicy).toBe("no-referrer");
    expect(r.credentials).toBe("include");
    expect(new URL(r.url).search).toBe("");
  }
});

it("propagates a required second factor and preserves the existing CSRF token on failure", async () => {
  const problem = {
    status: 403,
    type: "about:blank",
    title: "Additional verification required",
    code: "auth_phone_second_factor_required",
  };
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(reply({ csrf_token: "existing" }))
      .mockResolvedValueOnce(reply(problem, 403)),
  );
  await rotateCSRFToken();
  await expect(verifyPhoneLogin(proof)).rejects.toMatchObject({
    status: 403,
    problem,
  });
  expect(getCurrentCSRFToken()).toBe("existing");
});
