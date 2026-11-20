import type { PhoneSummary } from "@tutorhub/api-client";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * AUTH-10B helpers shared by the phone sign-in (`/sign-in`, `/account-link`)
 * and the Settings phone card. The server owns E.164 normalisation, country
 * policy and line-type checks; the client only tidies what the user typed.
 * Nothing here stores a number or a code.
 */

/** Upper bound for client-side countdowns; the server stays authoritative. */
export const MAX_PHONE_COUNTDOWN_SECONDS = 3600;
/** AUTH-10A: a phone challenge lives 5 minutes and resends after 60 s. */
export const DEFAULT_PHONE_CODE_TTL_SECONDS = 300;
export const DEFAULT_PHONE_RESEND_SECONDS = 60;
export const PHONE_CODE_LENGTH = 6;
const PHONE_CODE_PATTERN = /^\d{6}$/;
/** Digits with an optional leading `+`, after separators are removed. */
const PHONE_INPUT_PATTERN = /^\+?\d{6,15}$/;
const COUNTRY_PATTERN = /^[A-Z]{2}$/;
/** Longest input accepted by the field (formatted national or E.164). */
export const PHONE_INPUT_MAX_LENGTH = 24;

/**
 * Calling codes for regions TutorHub is likely to allow. Any other region is
 * shown by its ISO code alone; the server still validates it.
 */
const CALLING_CODES: Readonly<Record<string, number>> = {
  AU: 61,
  CA: 1,
  CN: 86,
  DE: 49,
  FR: 33,
  GB: 44,
  HK: 852,
  ID: 62,
  IN: 91,
  JP: 81,
  KH: 855,
  KR: 82,
  LA: 856,
  MY: 60,
  PH: 63,
  SG: 65,
  TH: 66,
  TW: 886,
  US: 1,
  VN: 84,
};

/** `VN (+84)` for a known region, the ISO code otherwise. */
export function phoneCountryLabel(country: string, callingCode?: number) {
  const iso = country.trim().toUpperCase();
  const code =
    typeof callingCode === "number" && callingCode > 0
      ? callingCode
      : CALLING_CODES[iso];
  return code ? `${iso} (+${code})` : iso;
}

/** Distinct, upper-case ISO regions in server order (first = default). */
export function phoneCountryOptions(allowed: readonly string[] | undefined) {
  const result: string[] = [];
  for (const entry of allowed ?? []) {
    const iso = entry.trim().toUpperCase();
    if (COUNTRY_PATTERN.test(iso) && !result.includes(iso)) result.push(iso);
  }
  return result;
}

/** Drops spaces, dots, dashes and brackets: `0912 345 678` → `0912345678`. */
export function compactPhoneNumber(value: string) {
  return value.trim().replace(/[\s().-]/g, "");
}

/** A syntax check only; the server decides validity and line type. */
export function isPlausiblePhoneNumber(value: string) {
  return PHONE_INPUT_PATTERN.test(compactPhoneNumber(value));
}

/** Keeps at most six digits of a typed or pasted code. */
export function sanitizePhoneCode(value: string) {
  return value.replace(/\D/g, "").slice(0, PHONE_CODE_LENGTH);
}

export function isCompletePhoneCode(value: string) {
  return PHONE_CODE_PATTERN.test(value);
}

export function boundedPhoneSeconds(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), MAX_PHONE_COUNTDOWN_SECONDS)
    : 0;
}

/** `m:ss` for a countdown, e.g. `4:59`. */
export function formatPhoneClock(seconds: number) {
  const safe = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(safe / 60);
  const rest = safe % 60;
  return `${minutes}:${String(rest).padStart(2, "0")}`;
}

export interface SignInMethodInventory {
  /**
   * The account's own email identity can still sign in: with an emailed
   * code, or with a password once that email is verified.
   */
  emailSignIn: boolean;
  /** Enrolled passkeys (a second factor even while passkey sign-in is off). */
  enrolledPasskeys: number;
  /** An active authenticator app (TOTP). */
  hasTotp: boolean;
  /** `mfa_totp_enabled`: the deployment runs the second sign-in step. */
  mfaTotpEnabled: boolean;
  /** Active passkeys that can sign in (`passkeys_enabled`). */
  passkeyCount: number;
  phone: PhoneSummary | null;
}

/**
 * Mirrors the AUTH-10A server rule for a phone number that can still sign in
 * on its own: not stale, and on an account with a passkey or an authenticator
 * app the second step must be able to run. The server stays authoritative.
 */
export function isPhoneUsableSignInMethod(inventory: SignInMethodInventory) {
  const { phone } = inventory;
  if (!phone || phone.stale) return false;
  const secondFactor = inventory.hasTotp || inventory.enrolledPasskeys > 0;
  if (!secondFactor) return true;
  return (
    inventory.mfaTotpEnabled &&
    (inventory.hasTotp || inventory.passkeyCount > 0)
  );
}

/**
 * Whether removing one linked identity still leaves a way to sign in: an
 * active passkey, the account's email (password or emailed code) or a usable
 * phone number.
 */
export function hasAnotherSignInMethod(inventory: SignInMethodInventory) {
  return (
    inventory.passkeyCount > 0 ||
    inventory.emailSignIn ||
    isPhoneUsableSignInMethod(inventory)
  );
}

/**
 * A wall-clock countdown: throttled background timers cannot stretch it.
 * `start(seconds)` restarts it, `reset()` stops it at zero.
 */
export function usePhoneCountdown() {
  const [remaining, setRemaining] = useState(0);
  const [generation, setGeneration] = useState(0);
  const deadlineRef = useRef(0);

  useEffect(() => {
    if (generation === 0) return;
    const timer = window.setInterval(() => {
      const left = Math.max(
        0,
        Math.ceil((deadlineRef.current - Date.now()) / 1000),
      );
      setRemaining(left);
      if (left <= 0) window.clearInterval(timer);
    }, 1000);
    return () => window.clearInterval(timer);
  }, [generation]);

  const start = useCallback((seconds: number) => {
    const bounded = boundedPhoneSeconds(seconds);
    deadlineRef.current = Date.now() + bounded * 1000;
    setRemaining(bounded);
    setGeneration((value) => value + 1);
  }, []);

  const reset = useCallback(() => {
    deadlineRef.current = 0;
    setRemaining(0);
  }, []);

  return { remaining, reset, start };
}
