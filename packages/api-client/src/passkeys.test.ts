import { afterEach, describe, expect, expectTypeOf, it, vi } from "vitest";
import {
  getCurrentCSRFToken,
  listMyPasskeys,
  reauthenticate,
  renameMyPasskey,
  revokeMyPasskey,
  rotateCSRFToken,
  startPasskeyAssertion,
  startPasskeyReauthentication,
  startPasskeyRegistration,
  verifyPasskeyAssertion,
  verifyPasskeyRegistration,
  type MyPasskey,
  type PasskeyAssertion,
  type PasskeyAttestation,
} from "./index";

afterEach(() => vi.unstubAllGlobals());

const assertion: PasskeyAssertion = {
  id: "Y3JlZA",
  rawId: "Y3JlZA",
  type: "public-key",
  response: {
    authenticatorData: "YXV0aA",
    clientDataJSON: "Y2xpZW50",
    signature: "c2ln",
    userHandle: "aGFuZGxl",
  },
};
const attestation: PasskeyAttestation = {
  id: assertion.id,
  rawId: assertion.rawId,
  type: "public-key",
  response: {
    attestationObject: "YXR0ZXN0",
    clientDataJSON: "Y2xpZW50",
    transports: ["internal"],
  },
};
const passkey: MyPasskey = {
  id: "77dac399-c8be-41dc-ae45-e2f079d5b166",
  label: "Laptop",
  backup_eligible: true,
  backup_state: false,
  transports: ["internal"],
  created_at: "2026-11-19T03:00:00Z",
  last_used_at: null,
};
const csrf = "current-token";
const origin = "https://class.example";

function reply(body: unknown, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function sentRequest(calls: readonly [Request][], index: number) {
  const request = calls[index]?.[0];
  if (!request) throw new Error(`Expected request ${index + 1}`);
  return request;
}

function checkRequest(
  request: Request,
  method: string,
  path: string,
  authenticatedMutation = false,
) {
  expect(request.method).toBe(method);
  expect(new URL(request.url).pathname).toBe(path);
  expect(request.credentials).toBe("include");
  expect(request.cache).toBe("no-store");
  expect(request.referrerPolicy).toBe("no-referrer");
  expect(request.headers.get("X-CSRF-Token")).toBe(
    authenticatedMutation ? csrf : null,
  );
  if (method !== "GET") expect(request.headers.get("Origin")).toBe(origin);
}

describe("passkey client", () => {
  it("returns options accepted by the browser WebAuthn JSON parsers", () => {
    type RequestOptions = Parameters<
      typeof PublicKeyCredential.parseRequestOptionsFromJSON
    >[0];
    type CreationOptions = Parameters<
      typeof PublicKeyCredential.parseCreationOptionsFromJSON
    >[0];
    expectTypeOf(
      startPasskeyAssertion,
    ).returns.resolves.toExtend<RequestOptions>();
    expectTypeOf(
      startPasskeyReauthentication,
    ).returns.resolves.toExtend<RequestOptions>();
    expectTypeOf(
      startPasskeyRegistration,
    ).returns.resolves.toExtend<CreationOptions>();
  });

  it("starts discoverable sign-in without an account identifier and clears cached CSRF on successful login", async () => {
    vi.stubGlobal("location", { origin });
    const options = {
      allowCredentials: [],
      challenge: "dGVzdA",
      rpId: "class.example",
      userVerification: "preferred",
    };
    const fetch = vi
      .fn<(request: Request) => Promise<Response>>()
      .mockResolvedValueOnce(reply({ csrf_token: csrf }))
      .mockResolvedValueOnce(reply(options))
      .mockResolvedValueOnce(reply({ status: "authenticated" }));
    await rotateCSRFToken({ fetch });
    await expect(startPasskeyAssertion({ fetch })).resolves.toEqual(options);
    expect(getCurrentCSRFToken()).toBe(csrf);
    const input = { assertion, keep_signed_in: true };
    await expect(verifyPasskeyAssertion(input, { fetch })).resolves.toEqual({
      status: "authenticated",
    });
    expect(getCurrentCSRFToken()).toBeNull();
    const start = sentRequest(fetch.mock.calls, 1);
    const finish = sentRequest(fetch.mock.calls, 2);
    checkRequest(start, "POST", "/api/v1/auth/passkeys/assertion/options");
    checkRequest(finish, "POST", "/api/v1/auth/passkeys/assertion/verify");
    expect(await start.json()).toEqual({});
    expect(await finish.json()).toEqual(input);
  });

  it.each([true, undefined])(
    "preserves the server's unknown-credential signal %s and the cached CSRF on rejection",
    async (unknownCredential) => {
      const problem = {
        type: "urn:tutorhub:problem:http-401",
        status: 401,
        title: "Invalid passkey",
        code: "auth_invalid_passkey",
        ...(unknownCredential && { unknown_credential: true }),
      };
      const fetch = vi
        .fn()
        .mockResolvedValueOnce(reply({ csrf_token: csrf }))
        .mockResolvedValueOnce(reply(problem, 401));
      await rotateCSRFToken({ fetch });
      await expect(
        verifyPasskeyAssertion({ assertion }, { fetch }),
      ).rejects.toMatchObject({ status: 401, problem });
      expect(getCurrentCSRFToken()).toBe(csrf);
    },
  );

  it("sends session-bound step-up evidence and clears CSRF only after the session rotates", async () => {
    vi.stubGlobal("location", { origin });
    const options = {
      challenge: "dGVzdA",
      rpId: "class.example",
      userVerification: "required",
      allowCredentials: [{ type: "public-key", id: assertion.id }],
    };
    const fetch = vi
      .fn<(request: Request) => Promise<Response>>()
      .mockResolvedValueOnce(reply({ csrf_token: csrf }))
      .mockResolvedValueOnce(reply(options))
      .mockResolvedValueOnce(
        reply({ recent_auth_expires_at: "2026-11-19T03:10:00Z" }),
      );
    await rotateCSRFToken({ fetch });
    await expect(
      startPasskeyReauthentication(csrf, { fetch }),
    ).resolves.toEqual(options);
    expect(getCurrentCSRFToken()).toBe(csrf);
    const proof = { method: "passkey", assertion } as const;
    await reauthenticate(proof, csrf, { fetch });
    expect(getCurrentCSRFToken()).toBeNull();
    const start = sentRequest(fetch.mock.calls, 1);
    const finish = sentRequest(fetch.mock.calls, 2);
    checkRequest(start, "POST", "/api/v1/auth/reauth/passkey/options", true);
    checkRequest(finish, "POST", "/api/v1/auth/reauth", true);
    expect(await start.json()).toEqual({});
    expect(await finish.json()).toEqual(proof);
  });

  it("registers, lists, renames and revokes passkeys without rotating session CSRF", async () => {
    vi.stubGlobal("location", { origin });
    const options = {
      challenge: "dGVzdA",
      user: { id: "aGFuZGxl", name: "account", displayName: "account" },
      rp: { id: "class.example", name: "TutorHub" },
      pubKeyCredParams: [{ type: "public-key", alg: -7 }],
      authenticatorSelection: {
        residentKey: "required",
        userVerification: "preferred",
      },
      attestation: "none",
    };
    const fetch = vi
      .fn<(request: Request) => Promise<Response>>()
      .mockResolvedValueOnce(reply({ csrf_token: csrf }))
      .mockResolvedValueOnce(reply(options))
      .mockResolvedValueOnce(reply(passkey, 201))
      .mockResolvedValueOnce(reply({ passkeys: [passkey] }))
      .mockResolvedValueOnce(reply({ ...passkey, label: "Office" }))
      .mockResolvedValueOnce(reply(null, 204));
    await rotateCSRFToken({ fetch });
    const input = { attestation, label: passkey.label };
    await expect(startPasskeyRegistration(csrf, { fetch })).resolves.toEqual(
      options,
    );
    await expect(
      verifyPasskeyRegistration(input, csrf, { fetch }),
    ).resolves.toEqual(passkey);
    await expect(listMyPasskeys({ fetch })).resolves.toEqual({
      passkeys: [passkey],
    });
    await expect(
      renameMyPasskey(passkey.id, { label: "Office" }, csrf, { fetch }),
    ).resolves.toEqual({ ...passkey, label: "Office" });
    await expect(
      revokeMyPasskey(passkey.id, csrf, { fetch }),
    ).resolves.toBeUndefined();
    expect(getCurrentCSRFToken()).toBe(csrf);
    const start = sentRequest(fetch.mock.calls, 1);
    const verify = sentRequest(fetch.mock.calls, 2);
    const list = sentRequest(fetch.mock.calls, 3);
    const rename = sentRequest(fetch.mock.calls, 4);
    const revoke = sentRequest(fetch.mock.calls, 5);
    checkRequest(
      start,
      "POST",
      "/api/v1/me/passkeys/registration/options",
      true,
    );
    checkRequest(
      verify,
      "POST",
      "/api/v1/me/passkeys/registration/verify",
      true,
    );
    checkRequest(list, "GET", "/api/v1/me/passkeys");
    checkRequest(rename, "PATCH", `/api/v1/me/passkeys/${passkey.id}`, true);
    checkRequest(revoke, "DELETE", `/api/v1/me/passkeys/${passkey.id}`, true);
    expect(await start.json()).toEqual({});
    expect(await verify.json()).toEqual(input);
    expect(await rename.json()).toEqual({ label: "Office" });
    expect(revoke.body).toBeNull();
  });
});
