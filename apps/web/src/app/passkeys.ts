import {
  APIRequestError,
  type PasskeyAssertion,
  type PasskeyAttestation,
  type PasskeyCreationOptions,
  type PasskeyRequestOptions,
} from "@tutorhub/api-client";

/**
 * AUTH-08B browser WebAuthn helpers shared by /sign-in, /account-link, the
 * step-up dialog and the Settings passkey card. Options arrive from the Core
 * API in the WebAuthn JSON format (unpadded base64url binaries) and responses
 * go back the same way. Native `parse*FromJSON` / `toJSON()` are preferred; a
 * plain TypeScript base64url converter covers browsers without them (and
 * jsdom). Nothing here persists credential IDs, challenges or user handles.
 */

/** Matches the server's label rule: 1–80 characters after trimming. */
export const MAX_PASSKEY_LABEL_LENGTH = 80;
export const INVALID_PASSKEY_CODE = "auth_invalid_passkey";

const BASE64URL_PATTERN = /^[A-Za-z0-9_-]*$/;
const TRANSPORTS: ReadonlySet<string> = new Set<AuthenticatorTransport>([
  "ble",
  "hybrid",
  "internal",
  "nfc",
  "usb",
]);

/**
 * The static surface of `PublicKeyCredential` this module feature-detects.
 * Every member is optional: older browsers lack the JSON parsers and the
 * Signal API, and TypeScript's DOM library does not declare the latter.
 */
interface PublicKeyCredentialStatics {
  isConditionalMediationAvailable?: () => Promise<boolean>;
  parseCreationOptionsFromJSON?: (
    options: PublicKeyCredentialCreationOptionsJSON,
  ) => PublicKeyCredentialCreationOptions;
  parseRequestOptionsFromJSON?: (
    options: PublicKeyCredentialRequestOptionsJSON,
  ) => PublicKeyCredentialRequestOptions;
  signalUnknownCredential?: (options: {
    credentialId: string;
    rpId: string;
  }) => Promise<void>;
}

function publicKeyCredentialStatics(): PublicKeyCredentialStatics | null {
  const value: unknown = (globalThis as { PublicKeyCredential?: unknown })
    .PublicKeyCredential;
  return typeof value === "function"
    ? (value as PublicKeyCredentialStatics)
    : null;
}

/** True when this browser exposes WebAuthn (`get` and `create`). */
export function isWebAuthnSupported() {
  if (publicKeyCredentialStatics() === null) return false;
  const credentials: Partial<CredentialsContainer> | undefined =
    typeof navigator === "undefined" ? undefined : navigator.credentials;
  return (
    typeof credentials?.get === "function" &&
    typeof credentials.create === "function"
  );
}

/** Passkey autofill (conditional mediation); any failure counts as "no". */
export async function isConditionalMediationAvailable() {
  const statics = publicKeyCredentialStatics();
  if (
    !statics ||
    !isWebAuthnSupported() ||
    typeof statics.isConditionalMediationAvailable !== "function"
  ) {
    return false;
  }
  try {
    return (await statics.isConditionalMediationAvailable()) === true;
  } catch {
    return false;
  }
}

/* ---------- base64url ---------- */

/** Decodes unpadded base64url; throws a TypeError on anything else. */
export function base64URLToBuffer(value: string): ArrayBuffer {
  if (!BASE64URL_PATTERN.test(value) || value.length % 4 === 1) {
    throw new TypeError("The value is not unpadded base64url.");
  }
  const base64 = value
    .replace(/-/g, "+")
    .replace(/_/g, "/")
    .padEnd(Math.ceil(value.length / 4) * 4, "=");
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes.buffer;
}

export function bufferToBase64URL(value: ArrayBuffer | ArrayBufferView) {
  const bytes = ArrayBuffer.isView(value)
    ? new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
    : new Uint8Array(value);
  let binary = "";
  // Chunked: spreading a large array into fromCharCode can overflow the stack.
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

/* ---------- options: JSON → browser ---------- */

type DescriptorJSON = PasskeyRequestOptions["allowCredentials"][number];

function transportsFrom(values: readonly string[] | undefined) {
  if (!values) return undefined;
  return values.filter((value): value is AuthenticatorTransport =>
    TRANSPORTS.has(value),
  );
}

function descriptorFromJSON(
  descriptor: DescriptorJSON,
): PublicKeyCredentialDescriptor {
  const transports = transportsFrom(descriptor.transports);
  return {
    id: base64URLToBuffer(descriptor.id),
    type: "public-key",
    ...(transports ? { transports } : {}),
  };
}

function manualRequestOptions(
  options: PasskeyRequestOptions,
): PublicKeyCredentialRequestOptions {
  return {
    allowCredentials: options.allowCredentials.map(descriptorFromJSON),
    challenge: base64URLToBuffer(options.challenge),
    rpId: options.rpId,
    userVerification: options.userVerification,
    ...(options.timeout === undefined ? {} : { timeout: options.timeout }),
  };
}

function manualCreationOptions(
  options: PasskeyCreationOptions,
): PublicKeyCredentialCreationOptions {
  return {
    attestation: options.attestation,
    authenticatorSelection: { ...options.authenticatorSelection },
    challenge: base64URLToBuffer(options.challenge),
    excludeCredentials: (options.excludeCredentials ?? []).map(
      descriptorFromJSON,
    ),
    pubKeyCredParams: options.pubKeyCredParams.map((parameter) => ({
      alg: parameter.alg,
      type: "public-key" as const,
    })),
    rp: { id: options.rp.id, name: options.rp.name },
    user: {
      displayName: options.user.displayName,
      id: base64URLToBuffer(options.user.id),
      name: options.user.name,
    },
    ...(options.timeout === undefined ? {} : { timeout: options.timeout }),
  };
}

/** Native `parseRequestOptionsFromJSON` when present, else the fallback. */
export function requestOptionsFromJSON(
  options: PasskeyRequestOptions,
): PublicKeyCredentialRequestOptions {
  const statics = publicKeyCredentialStatics();
  if (typeof statics?.parseRequestOptionsFromJSON === "function") {
    try {
      return statics.parseRequestOptionsFromJSON(options);
    } catch {
      // A stricter or buggy native parser: the fallback re-validates.
    }
  }
  return manualRequestOptions(options);
}

/** Native `parseCreationOptionsFromJSON` when present, else the fallback. */
export function creationOptionsFromJSON(
  options: PasskeyCreationOptions,
): PublicKeyCredentialCreationOptions {
  const statics = publicKeyCredentialStatics();
  if (typeof statics?.parseCreationOptionsFromJSON === "function") {
    try {
      return statics.parseCreationOptionsFromJSON(options);
    } catch {
      // A stricter or buggy native parser: the fallback re-validates.
    }
  }
  return manualCreationOptions(options);
}

/* ---------- credentials: browser → JSON ---------- */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isBase64URL(value: unknown): value is string {
  return typeof value === "string" && BASE64URL_PATTERN.test(value);
}

function isPublicKeyCredential(value: unknown): value is PublicKeyCredential {
  return (
    isRecord(value) &&
    value.type === "public-key" &&
    typeof value.id === "string" &&
    "rawId" in value &&
    isRecord(value.response)
  );
}

/** `toJSON()` is typed `any` and can be replaced by extensions: verify it. */
function nativeJSON(credential: PublicKeyCredential): unknown {
  const candidate = credential as { toJSON?: () => unknown };
  if (typeof candidate.toJSON !== "function") return null;
  try {
    return candidate.toJSON();
  } catch {
    return null;
  }
}

function attachmentOf(value: unknown) {
  return typeof value === "string" ? value : null;
}

function nativeAssertion(value: unknown): PasskeyAssertion | null {
  if (!isRecord(value) || !isRecord(value.response)) return null;
  const response = value.response;
  const userHandle = response.userHandle;
  if (
    value.type !== "public-key" ||
    !isBase64URL(value.id) ||
    !isBase64URL(value.rawId) ||
    !isBase64URL(response.authenticatorData) ||
    !isBase64URL(response.clientDataJSON) ||
    !isBase64URL(response.signature) ||
    !(
      userHandle === undefined ||
      userHandle === null ||
      isBase64URL(userHandle)
    )
  ) {
    return null;
  }
  return {
    authenticatorAttachment: attachmentOf(value.authenticatorAttachment),
    clientExtensionResults: isRecord(value.clientExtensionResults)
      ? value.clientExtensionResults
      : {},
    id: value.id,
    rawId: value.rawId,
    response: {
      authenticatorData: response.authenticatorData,
      clientDataJSON: response.clientDataJSON,
      signature: response.signature,
      userHandle: userHandle ?? null,
    },
    type: "public-key",
  };
}

function nativeAttestation(value: unknown): PasskeyAttestation | null {
  if (!isRecord(value) || !isRecord(value.response)) return null;
  const response = value.response;
  const transports = response.transports;
  if (
    value.type !== "public-key" ||
    !isBase64URL(value.id) ||
    !isBase64URL(value.rawId) ||
    !isBase64URL(response.attestationObject) ||
    !isBase64URL(response.clientDataJSON) ||
    !(
      transports === undefined ||
      (Array.isArray(transports) &&
        transports.every((item) => typeof item === "string"))
    )
  ) {
    return null;
  }
  return {
    authenticatorAttachment: attachmentOf(value.authenticatorAttachment),
    clientExtensionResults: isRecord(value.clientExtensionResults)
      ? value.clientExtensionResults
      : {},
    id: value.id,
    rawId: value.rawId,
    response: {
      attestationObject: response.attestationObject,
      clientDataJSON: response.clientDataJSON,
      ...(isBase64URL(response.authenticatorData)
        ? { authenticatorData: response.authenticatorData }
        : {}),
      ...(isBase64URL(response.publicKey)
        ? { publicKey: response.publicKey }
        : {}),
      ...(typeof response.publicKeyAlgorithm === "number"
        ? { publicKeyAlgorithm: response.publicKeyAlgorithm }
        : {}),
      ...(transports ? { transports: transports as string[] } : {}),
    },
    type: "public-key",
  };
}

export function assertionToJSON(
  credential: PublicKeyCredential,
): PasskeyAssertion {
  const native = nativeAssertion(nativeJSON(credential));
  if (native) return native;
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    authenticatorAttachment: attachmentOf(credential.authenticatorAttachment),
    // No extensions are requested, so there are no results to forward.
    clientExtensionResults: {},
    id: bufferToBase64URL(credential.rawId),
    rawId: bufferToBase64URL(credential.rawId),
    response: {
      authenticatorData: bufferToBase64URL(response.authenticatorData),
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      signature: bufferToBase64URL(response.signature),
      userHandle: response.userHandle
        ? bufferToBase64URL(response.userHandle)
        : null,
    },
    type: "public-key",
  };
}

/** Optional `AuthenticatorAttestationResponse` getters (Level 2+). */
interface AttestationResponseGetters {
  getAuthenticatorData?: () => ArrayBuffer;
  getPublicKey?: () => ArrayBuffer | null;
  getPublicKeyAlgorithm?: () => number;
  getTransports?: () => string[];
}

export function attestationToJSON(
  credential: PublicKeyCredential,
): PasskeyAttestation {
  const native = nativeAttestation(nativeJSON(credential));
  if (native) return native;
  const response = credential.response as AuthenticatorAttestationResponse &
    AttestationResponseGetters;
  const transports =
    typeof response.getTransports === "function"
      ? response.getTransports()
      : undefined;
  const publicKey =
    typeof response.getPublicKey === "function"
      ? response.getPublicKey()
      : null;
  return {
    authenticatorAttachment: attachmentOf(credential.authenticatorAttachment),
    clientExtensionResults: {},
    id: bufferToBase64URL(credential.rawId),
    rawId: bufferToBase64URL(credential.rawId),
    response: {
      attestationObject: bufferToBase64URL(response.attestationObject),
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      ...(typeof response.getAuthenticatorData === "function"
        ? {
            authenticatorData: bufferToBase64URL(
              response.getAuthenticatorData(),
            ),
          }
        : {}),
      ...(publicKey ? { publicKey: bufferToBase64URL(publicKey) } : {}),
      ...(typeof response.getPublicKeyAlgorithm === "function"
        ? { publicKeyAlgorithm: response.getPublicKeyAlgorithm() }
        : {}),
      ...(transports ? { transports } : {}),
    },
    type: "public-key",
  };
}

/* ---------- ceremonies ---------- */

export type PasskeyCeremonyFailure =
  "cancelled" | "exists" | "unsupported" | "security" | "failed";

export class PasskeyCeremonyError extends Error {
  readonly kind: PasskeyCeremonyFailure;

  constructor(kind: PasskeyCeremonyFailure) {
    super(`Passkey ceremony failed: ${kind}`);
    this.name = "PasskeyCeremonyError";
    this.kind = kind;
  }
}

/**
 * Maps a WebAuthn DOMException. `NotAllowedError` (the user closed the
 * system sheet, a timeout, or no matching passkey) and `AbortError` (this
 * app cancelled a background request) are both a quiet cancel.
 */
export function classifyCeremonyError(error: unknown): PasskeyCeremonyFailure {
  if (error instanceof PasskeyCeremonyError) return error.kind;
  const name =
    isRecord(error) && typeof error.name === "string" ? error.name : "";
  switch (name) {
    case "NotAllowedError":
    case "AbortError":
      return "cancelled";
    case "InvalidStateError":
      return "exists";
    case "NotSupportedError":
      return "unsupported";
    case "SecurityError":
      return "security";
    default:
      return "failed";
  }
}

export interface PasskeyCeremonyOptions {
  mediation?: CredentialMediationRequirement;
  signal?: AbortSignal;
}

/** Runs `navigator.credentials.get` and returns the JSON the API expects. */
export async function getPasskeyAssertion(
  options: PasskeyRequestOptions,
  { mediation = "optional", signal }: PasskeyCeremonyOptions = {},
): Promise<PasskeyAssertion> {
  if (!isWebAuthnSupported()) throw new PasskeyCeremonyError("unsupported");
  let publicKey: PublicKeyCredentialRequestOptions;
  try {
    publicKey = requestOptionsFromJSON(options);
  } catch {
    throw new PasskeyCeremonyError("failed");
  }
  const credential = await navigator.credentials.get({
    mediation,
    publicKey,
    ...(signal ? { signal } : {}),
  });
  if (!isPublicKeyCredential(credential)) {
    throw new PasskeyCeremonyError("failed");
  }
  return assertionToJSON(credential);
}

/** Runs `navigator.credentials.create` and returns the attestation JSON. */
export async function createPasskeyAttestation(
  options: PasskeyCreationOptions,
  { signal }: Pick<PasskeyCeremonyOptions, "signal"> = {},
): Promise<PasskeyAttestation> {
  if (!isWebAuthnSupported()) throw new PasskeyCeremonyError("unsupported");
  let publicKey: PublicKeyCredentialCreationOptions;
  try {
    publicKey = creationOptionsFromJSON(options);
  } catch {
    throw new PasskeyCeremonyError("failed");
  }
  const credential = await navigator.credentials.create({
    publicKey,
    ...(signal ? { signal } : {}),
  });
  if (!isPublicKeyCredential(credential)) {
    throw new PasskeyCeremonyError("failed");
  }
  return attestationToJSON(credential);
}

/* ---------- Signal API ---------- */

/**
 * Only a 401 `auth_invalid_passkey` that the server marked
 * `unknown_credential: true` (the credential ID never existed) qualifies. A
 * cancel, a network failure, an expired challenge or a revoked passkey never
 * does.
 */
export function shouldSignalUnknownCredential(error: unknown) {
  return (
    error instanceof APIRequestError &&
    error.status === 401 &&
    error.problem?.code === INVALID_PASSKEY_CODE &&
    error.problem.unknown_credential === true
  );
}

/**
 * Tells the authenticator that the credential it just used is unknown to
 * this relying party, so it can hide or delete it. `rpId` comes from the
 * request options and `credentialId` from the assertion that was sent.
 * Best effort: resolves to whether the browser accepted the signal.
 */
export async function signalUnknownPasskey(
  error: unknown,
  target: { credentialId: string; rpId: string },
) {
  if (!shouldSignalUnknownCredential(error)) return false;
  const statics = publicKeyCredentialStatics();
  if (
    !statics ||
    typeof statics.signalUnknownCredential !== "function" ||
    !target.rpId ||
    !target.credentialId ||
    !BASE64URL_PATTERN.test(target.credentialId)
  ) {
    return false;
  }
  try {
    await statics.signalUnknownCredential({
      credentialId: target.credentialId,
      rpId: target.rpId,
    });
    return true;
  } catch {
    return false;
  }
}

/* ---------- labels ---------- */

const OPERATING_SYSTEMS: readonly (readonly [RegExp, string])[] = [
  [/Windows/, "Windows"],
  [/iPhone|iPad|iPod/, "iOS"],
  [/Android/, "Android"],
  [/CrOS/, "ChromeOS"],
  [/Macintosh|Mac OS X/, "macOS"],
  [/Linux/, "Linux"],
];

const BROWSERS: readonly (readonly [RegExp, string])[] = [
  [/Edg(?:e|A|iOS)?\//, "Edge"],
  [/OPR\/|Opera/, "Opera"],
  [/SamsungBrowser\//, "Samsung Internet"],
  [/Firefox\/|FxiOS\//, "Firefox"],
  [/Chrome\/|CriOS\//, "Chrome"],
  [/Safari\//, "Safari"],
];

function firstMatch(
  value: string,
  table: readonly (readonly [RegExp, string])[],
) {
  return table.find(([pattern]) => pattern.test(value))?.[1];
}

/**
 * A coarse, allowlisted "OS · Browser" suggestion (e.g. "Windows · Chrome")
 * that the user can edit before creating the passkey; never the raw UA.
 */
export function defaultPasskeyLabel(userAgent: string, fallback = "Passkey") {
  const parts = [
    firstMatch(userAgent, OPERATING_SYSTEMS),
    firstMatch(userAgent, BROWSERS),
  ].filter((part): part is string => Boolean(part));
  return parts.length > 0 ? parts.join(" · ") : fallback;
}

export type PasskeyLabelProblem = "required" | "tooLong" | "invalid";

/** Client-side mirror of the server rule; the server stays authoritative. */
export function validatePasskeyLabel(
  label: string,
): PasskeyLabelProblem | null {
  const trimmed = label.trim();
  if (!trimmed) return "required";
  if ([...trimmed].length > MAX_PASSKEY_LABEL_LENGTH) return "tooLong";
  for (const character of trimmed) {
    const code = character.charCodeAt(0);
    if (code < 0x20 || code === 0x7f) return "invalid";
  }
  return null;
}
