import type { MFAStateResponse } from "@tutorhub/api-client";

/**
 * AUTH-09B helpers shared by the sign-in second step and the Settings
 * authenticator card. Nothing here stores a code, a seed or a token.
 */
export type MFAMethod = MFAStateResponse["methods"][number];

/** Server-side lifetime of an MFA transaction (AUTH-09A: 5 minutes). */
export const DEFAULT_MFA_EXPIRES_IN_SECONDS = 300;

/** Server alphabet: Crockford Base32 without I, L, O and U. */
const RECOVERY_CODE_PATTERN = /^[0-9A-HJKMNP-TV-Z]{16}$/;

/** Most convenient first: the app, then a passkey, then a one-time code. */
const METHOD_ORDER: readonly MFAMethod[] = ["totp", "passkey", "recovery_code"];

/**
 * Crockford-normalises a typed recovery code (case, spaces and dashes;
 * O→0, I/L→1). Returns the canonical 16 characters, or `null`.
 */
export function normalizeRecoveryCode(value: string) {
  const compact = value
    .toUpperCase()
    .replace(/[\s-]+/g, "")
    .replace(/O/g, "0")
    .replace(/[IL]/g, "1");
  return RECOVERY_CODE_PATTERN.test(compact) ? compact : null;
}

/** Orders the server's methods; a passkey needs WebAuthn in this browser. */
export function usableMfaMethods(
  methods: readonly MFAMethod[],
  passkeysAvailable: boolean,
) {
  return METHOD_ORDER.filter(
    (method) =>
      methods.includes(method) && (method !== "passkey" || passkeysAvailable),
  );
}

/** Plain-text body of the one-time recovery-code download. */
export function recoveryCodesFileText(
  codes: readonly string[],
  labels: { heading: string; warning: string },
) {
  return [labels.heading, labels.warning, "", ...codes, ""].join("\n");
}
