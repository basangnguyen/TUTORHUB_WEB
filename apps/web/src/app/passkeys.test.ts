import {
  APIRequestError,
  type PasskeyCreationOptions,
  type PasskeyRequestOptions,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  assertionToJSON,
  attestationToJSON,
  base64URLToBuffer,
  bufferToBase64URL,
  classifyCeremonyError,
  createPasskeyAttestation,
  creationOptionsFromJSON,
  defaultPasskeyLabel,
  getPasskeyAssertion,
  isConditionalMediationAvailable,
  isWebAuthnSupported,
  PasskeyCeremonyError,
  requestOptionsFromJSON,
  shouldSignalUnknownCredential,
  signalUnknownPasskey,
  validatePasskeyLabel,
} from "./passkeys";

const requestOptions: PasskeyRequestOptions = {
  allowCredentials: [
    {
      id: "AQID",
      transports: ["internal", "carrier-pigeon"],
      type: "public-key",
    },
  ],
  challenge: "_-8",
  rpId: "localhost",
  timeout: 300000,
  userVerification: "required",
};

const creationOptions: PasskeyCreationOptions = {
  attestation: "none",
  authenticatorSelection: {
    residentKey: "required",
    userVerification: "preferred",
  },
  challenge: "AAEC",
  excludeCredentials: [{ id: "BAUG", type: "public-key" }],
  pubKeyCredParams: [{ alg: -7, type: "public-key" }],
  rp: { id: "localhost", name: "TutorHub" },
  user: { displayName: "Teacher", id: "CQoL", name: "teacher@example.test" },
};

function bytes(buffer: ArrayBuffer | ArrayBufferView | undefined) {
  if (!buffer) return [];
  return Array.from(
    ArrayBuffer.isView(buffer)
      ? new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength)
      : new Uint8Array(buffer),
  );
}

function buffer(...values: number[]) {
  return new Uint8Array(values).buffer;
}

function installCredentials(container: Partial<CredentialsContainer>) {
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: container,
  });
}

function installPublicKeyCredential(statics: Record<string, unknown> = {}) {
  const fake = Object.assign(function PublicKeyCredential() {}, statics);
  vi.stubGlobal("PublicKeyCredential", fake);
  return fake;
}

function fakeAssertion(overrides: Record<string, unknown> = {}) {
  return {
    authenticatorAttachment: "platform",
    id: "AQID",
    rawId: buffer(1, 2, 3),
    response: {
      authenticatorData: buffer(4),
      clientDataJSON: buffer(5),
      signature: buffer(6),
      userHandle: buffer(7, 8),
    },
    type: "public-key",
    ...overrides,
  } as unknown as PublicKeyCredential;
}

function problemError(status: number, problem: Record<string, unknown>) {
  return new APIRequestError(status, {
    status,
    title: "Rejected",
    type: "about:blank",
    ...problem,
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window.navigator, "credentials");
});

describe("base64url", () => {
  it("round-trips unpadded base64url and rejects anything else", () => {
    const value = new Uint8Array([0, 1, 254, 255, 62, 63]);
    const encoded = bufferToBase64URL(value);
    expect(encoded).toBe("AAH-_z4_");
    expect(bytes(base64URLToBuffer(encoded))).toEqual(Array.from(value));
    expect(bufferToBase64URL(new Uint8Array(0))).toBe("");
    expect(() => base64URLToBuffer("AA==")).toThrow(TypeError);
    expect(() => base64URLToBuffer("A+/=")).toThrow(TypeError);
    expect(() => base64URLToBuffer("AAAAA")).toThrow(TypeError);
  });

  it("encodes large buffers without overflowing the call stack", () => {
    const large = new Uint8Array(200_000).fill(65);
    expect(bufferToBase64URL(large)).toHaveLength(266_667);
  });
});

describe("support detection", () => {
  it("needs PublicKeyCredential plus credentials.get and create", async () => {
    expect(isWebAuthnSupported()).toBe(false);
    await expect(isConditionalMediationAvailable()).resolves.toBe(false);

    installPublicKeyCredential({
      isConditionalMediationAvailable: () => Promise.resolve(true),
    });
    expect(isWebAuthnSupported()).toBe(false);
    installCredentials({ create: vi.fn(), get: vi.fn() });
    expect(isWebAuthnSupported()).toBe(true);
    await expect(isConditionalMediationAvailable()).resolves.toBe(true);
  });

  it("treats a missing or failing conditional check as unavailable", async () => {
    installCredentials({ create: vi.fn(), get: vi.fn() });
    installPublicKeyCredential();
    await expect(isConditionalMediationAvailable()).resolves.toBe(false);
    installPublicKeyCredential({
      isConditionalMediationAvailable: () => Promise.reject(new Error("no")),
    });
    await expect(isConditionalMediationAvailable()).resolves.toBe(false);
  });
});

describe("options conversion", () => {
  it("prefers the native JSON parsers", () => {
    const parsedRequest = { challenge: buffer(9) };
    const parsedCreation = { challenge: buffer(8) };
    const statics = installPublicKeyCredential({
      parseCreationOptionsFromJSON: vi.fn(() => parsedCreation),
      parseRequestOptionsFromJSON: vi.fn(() => parsedRequest),
    });
    expect(requestOptionsFromJSON(requestOptions)).toBe(parsedRequest);
    expect(statics.parseRequestOptionsFromJSON).toHaveBeenCalledWith(
      requestOptions,
    );
    expect(creationOptionsFromJSON(creationOptions)).toBe(parsedCreation);
  });

  it("falls back to the base64url converter without native parsers", () => {
    const request = requestOptionsFromJSON(requestOptions);
    expect(bytes(request.challenge as ArrayBuffer)).toEqual([255, 239]);
    expect(request.rpId).toBe("localhost");
    expect(request.userVerification).toBe("required");
    expect(request.timeout).toBe(300000);
    const descriptor = request.allowCredentials?.[0];
    expect(bytes(descriptor?.id as ArrayBuffer)).toEqual([1, 2, 3]);
    // Unknown transports are dropped, never forwarded to the browser.
    expect(descriptor?.transports).toEqual(["internal"]);

    const creation = creationOptionsFromJSON(creationOptions);
    expect(bytes(creation.challenge as ArrayBuffer)).toEqual([0, 1, 2]);
    expect(bytes(creation.user.id as ArrayBuffer)).toEqual([9, 10, 11]);
    expect(bytes(creation.excludeCredentials?.[0]?.id as ArrayBuffer)).toEqual([
      4, 5, 6,
    ]);
    expect(creation.authenticatorSelection).toEqual({
      residentKey: "required",
      userVerification: "preferred",
    });
    expect(creation.rp).toEqual({ id: "localhost", name: "TutorHub" });
  });

  it("falls back when a native parser throws and rejects malformed binaries", () => {
    installPublicKeyCredential({
      parseRequestOptionsFromJSON: () => {
        throw new TypeError("strict");
      },
    });
    expect(
      bytes(requestOptionsFromJSON(requestOptions).challenge as ArrayBuffer),
    ).toEqual([255, 239]);
    expect(() =>
      requestOptionsFromJSON({
        ...requestOptions,
        challenge: "not base64url!",
      }),
    ).toThrow(TypeError);
  });
});

describe("credential serialization", () => {
  it("uses a well-formed native toJSON result", () => {
    const credential = fakeAssertion({
      toJSON: () => ({
        authenticatorAttachment: "cross-platform",
        clientExtensionResults: {},
        id: "native-id",
        rawId: "native-id",
        response: {
          authenticatorData: "AA",
          clientDataJSON: "AQ",
          signature: "Ag",
          userHandle: "Aw",
        },
        type: "public-key",
      }),
    });
    expect(assertionToJSON(credential)).toEqual({
      authenticatorAttachment: "cross-platform",
      clientExtensionResults: {},
      id: "native-id",
      rawId: "native-id",
      response: {
        authenticatorData: "AA",
        clientDataJSON: "AQ",
        signature: "Ag",
        userHandle: "Aw",
      },
      type: "public-key",
    });
  });

  it("serializes manually without toJSON or when it is malformed or throws", () => {
    const expected = {
      authenticatorAttachment: "platform",
      clientExtensionResults: {},
      id: "AQID",
      rawId: "AQID",
      response: {
        authenticatorData: "BA",
        clientDataJSON: "BQ",
        signature: "Bg",
        userHandle: "Bwg",
      },
      type: "public-key",
    };
    expect(assertionToJSON(fakeAssertion())).toEqual(expected);
    expect(
      assertionToJSON(fakeAssertion({ toJSON: () => ({ id: 42 }) })),
    ).toEqual(expected);
    expect(
      assertionToJSON(
        fakeAssertion({
          toJSON: () => {
            throw new Error("extension");
          },
        }),
      ),
    ).toEqual(expected);
    const withoutHandle = fakeAssertion({
      response: {
        authenticatorData: buffer(4),
        clientDataJSON: buffer(5),
        signature: buffer(6),
        userHandle: null,
      },
    });
    expect(assertionToJSON(withoutHandle).response.userHandle).toBeNull();
  });

  it("serializes an attestation with the optional getters", () => {
    const credential = {
      authenticatorAttachment: null,
      id: "AQID",
      rawId: buffer(1, 2, 3),
      response: {
        attestationObject: buffer(10),
        clientDataJSON: buffer(11),
        getAuthenticatorData: () => buffer(12),
        getPublicKey: () => buffer(13),
        getPublicKeyAlgorithm: () => -7,
        getTransports: () => ["internal", "hybrid"],
      },
      type: "public-key",
    } as unknown as PublicKeyCredential;
    expect(attestationToJSON(credential)).toEqual({
      authenticatorAttachment: null,
      clientExtensionResults: {},
      id: "AQID",
      rawId: "AQID",
      response: {
        attestationObject: "Cg",
        authenticatorData: "DA",
        clientDataJSON: "Cw",
        publicKey: "DQ",
        publicKeyAlgorithm: -7,
        transports: ["internal", "hybrid"],
      },
      type: "public-key",
    });
  });
});

describe("ceremonies", () => {
  it("passes mediation and the abort signal to credentials.get", async () => {
    const get = vi.fn(() => Promise.resolve(fakeAssertion()));
    installCredentials({ create: vi.fn(), get });
    installPublicKeyCredential();
    const controller = new AbortController();
    const assertion = await getPasskeyAssertion(requestOptions, {
      mediation: "conditional",
      signal: controller.signal,
    });
    expect(assertion.id).toBe("AQID");
    expect(get).toHaveBeenCalledWith(
      expect.objectContaining({
        mediation: "conditional",
        signal: controller.signal,
      }),
    );
  });

  it("reports unsupported browsers and non-credential results", async () => {
    await expect(getPasskeyAssertion(requestOptions)).rejects.toMatchObject({
      kind: "unsupported",
    });
    installPublicKeyCredential();
    installCredentials({
      create: vi.fn(() => Promise.resolve(null)),
      get: vi.fn(() => Promise.resolve(null)),
    });
    await expect(getPasskeyAssertion(requestOptions)).rejects.toMatchObject({
      kind: "failed",
    });
    await expect(
      createPasskeyAttestation(creationOptions),
    ).rejects.toBeInstanceOf(PasskeyCeremonyError);
  });

  it("maps WebAuthn DOMExceptions to quiet cancels and actionable failures", () => {
    const named = (name: string) => new DOMException("x", name);
    expect(classifyCeremonyError(named("NotAllowedError"))).toBe("cancelled");
    expect(classifyCeremonyError(named("AbortError"))).toBe("cancelled");
    expect(classifyCeremonyError(named("InvalidStateError"))).toBe("exists");
    expect(classifyCeremonyError(named("NotSupportedError"))).toBe(
      "unsupported",
    );
    expect(classifyCeremonyError(named("SecurityError"))).toBe("security");
    expect(classifyCeremonyError(new TypeError("bad"))).toBe("failed");
    expect(classifyCeremonyError(new PasskeyCeremonyError("exists"))).toBe(
      "exists",
    );
  });
});

describe("Signal API", () => {
  const target = { credentialId: "AQID", rpId: "localhost" };

  it("signals only a confirmed unknown credential", async () => {
    const signalUnknownCredential = vi.fn(() => Promise.resolve());
    installPublicKeyCredential({ signalUnknownCredential });
    const unknown = problemError(401, {
      code: "auth_invalid_passkey",
      unknown_credential: true,
    });
    expect(shouldSignalUnknownCredential(unknown)).toBe(true);
    await expect(signalUnknownPasskey(unknown, target)).resolves.toBe(true);
    expect(signalUnknownCredential).toHaveBeenCalledWith({
      credentialId: "AQID",
      rpId: "localhost",
    });

    const notSignalled = [
      problemError(401, { code: "auth_invalid_passkey" }),
      problemError(401, {
        code: "auth_invalid_passkey",
        unknown_credential: false,
      }),
      problemError(401, {
        code: "auth_invalid_current_password",
        unknown_credential: true,
      }),
      problemError(403, { code: "auth_passkey_clone_detected" }),
      problemError(429, {}),
      problemError(503, {}),
      new DOMException("closed", "NotAllowedError"),
      new DOMException("aborted", "AbortError"),
      new TypeError("Failed to fetch"),
    ];
    for (const error of notSignalled) {
      expect(shouldSignalUnknownCredential(error)).toBe(false);
      await expect(signalUnknownPasskey(error, target)).resolves.toBe(false);
    }
    expect(signalUnknownCredential).toHaveBeenCalledTimes(1);
  });

  it("is a no-op without the API or with an unusable target, and swallows rejections", async () => {
    const unknown = problemError(401, {
      code: "auth_invalid_passkey",
      unknown_credential: true,
    });
    installPublicKeyCredential();
    await expect(signalUnknownPasskey(unknown, target)).resolves.toBe(false);

    const signalUnknownCredential = vi.fn(() =>
      Promise.reject(new DOMException("x", "SecurityError")),
    );
    installPublicKeyCredential({ signalUnknownCredential });
    await expect(
      signalUnknownPasskey(unknown, { credentialId: "", rpId: "localhost" }),
    ).resolves.toBe(false);
    await expect(
      signalUnknownPasskey(unknown, { credentialId: "a+b", rpId: "localhost" }),
    ).resolves.toBe(false);
    expect(signalUnknownCredential).not.toHaveBeenCalled();
    await expect(signalUnknownPasskey(unknown, target)).resolves.toBe(false);
    expect(signalUnknownCredential).toHaveBeenCalledTimes(1);
  });
});

describe("labels", () => {
  it("suggests an allowlisted OS · browser name", () => {
    expect(
      defaultPasskeyLabel(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
      ),
    ).toBe("Windows · Chrome");
    expect(
      defaultPasskeyLabel(
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
      ),
    ).toBe("Windows · Edge");
    expect(
      defaultPasskeyLabel(
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
      ),
    ).toBe("macOS · Safari");
    expect(
      defaultPasskeyLabel(
        "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36",
      ),
    ).toBe("Android · Chrome");
    expect(
      defaultPasskeyLabel(
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/128.0 Mobile/15E148 Safari/605.1.15",
      ),
    ).toBe("iOS · Firefox");
    expect(defaultPasskeyLabel("curl/8.0", "Khóa truy cập")).toBe(
      "Khóa truy cập",
    );
  });

  it("validates 1–80 printable characters after trimming", () => {
    expect(validatePasskeyLabel("  ")).toBe("required");
    expect(validatePasskeyLabel("a".repeat(80))).toBeNull();
    expect(validatePasskeyLabel("😀".repeat(80))).toBeNull();
    expect(validatePasskeyLabel("a".repeat(81))).toBe("tooLong");
    expect(validatePasskeyLabel("bad\u0000name")).toBe("invalid");
    expect(validatePasskeyLabel(" Laptop ")).toBeNull();
  });
});
