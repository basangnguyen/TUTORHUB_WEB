import {
  getAuthProviders,
  type AuthProvidersResponse,
} from "@tutorhub/api-client";
import { useCallback, useEffect, useState } from "react";

/**
 * Sign-in helpers shared by /sign-in, /account-link and /complete-registration. Kept out of the page
 * component module so both routes can reuse them without breaking fast
 * refresh boundaries.
 */

export const DEFAULT_RETURN_TO = "/app/home";
export const ACCOUNT_LINK_PATH = "/account-link";
export const COMPLETE_REGISTRATION_PATH = "/complete-registration";
/**
 * Where `/sign-in?error=email_unverified` sends a signed-in person: the
 * callback of a provider link started in Settings (AUTH-07B, Microsoft
 * without `xms_edov`) carries no provider, so Settings explains the next step.
 */
export const LINK_EMAIL_UNVERIFIED_TARGET =
  "/app/settings?identity=email_unverified";
export const MIN_PASSWORD_LENGTH = 8;

export function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function hasControlCharacter(value: string) {
  for (const character of value) {
    const code = character.charCodeAt(0);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

function pathnameOf(value: string) {
  return value.split(/[?#]/, 1)[0] ?? value;
}

function isUnderPath(pathname: string, root: string) {
  return pathname === root || pathname.startsWith(`${root}/`);
}

/** Only same-origin SPA paths are accepted; anything else lands on the dashboard. */
export function sanitizeReturnTo(value: string) {
  if (
    !value.startsWith("/") ||
    value.startsWith("//") ||
    value.includes("\\") ||
    hasControlCharacter(value)
  ) {
    return DEFAULT_RETURN_TO;
  }
  if (isUnderPath(pathnameOf(value), "/sign-in")) {
    return DEFAULT_RETURN_TO;
  }
  return value;
}

/**
 * The server already sanitized `return_to`; the client re-checks it and also
 * refuses to send the user back to the (now consumed) linking screen.
 */
export function sanitizeLinkReturnTo(value: string) {
  const sanitized = sanitizeReturnTo(value);
  return isUnderPath(pathnameOf(sanitized), ACCOUNT_LINK_PATH)
    ? DEFAULT_RETURN_TO
    : sanitized;
}

/**
 * After a pending registration completes, never send the user back to the
 * consumed registration screen (or to the linking screen it may lead to).
 */
export function sanitizeRegistrationReturnTo(value: string) {
  const sanitized = sanitizeLinkReturnTo(value);
  return isUnderPath(pathnameOf(sanitized), COMPLETE_REGISTRATION_PATH)
    ? DEFAULT_RETURN_TO
    : sanitized;
}

export function readReturnTo(state: unknown) {
  if (typeof state !== "object" || state === null || !("from" in state)) {
    return DEFAULT_RETURN_TO;
  }
  const from: unknown = state.from;
  if (typeof from !== "object" || from === null) {
    return DEFAULT_RETURN_TO;
  }
  const pathname =
    "pathname" in from && typeof from.pathname === "string"
      ? from.pathname
      : DEFAULT_RETURN_TO;
  const search =
    "search" in from && typeof from.search === "string" ? from.search : "";
  return sanitizeReturnTo(`${pathname}${search}`);
}

/** Fixed codes the OAuth callback appends as `/sign-in?error=<code>`. */
export const CALLBACK_ERROR_CODES = [
  "signup_disabled",
  "email_unverified",
  "provider_unavailable",
  "assurance_required",
  "authentication_failed",
  "temporarily_unavailable",
  "rate_limited",
] as const;

export type CallbackErrorCode = (typeof CALLBACK_ERROR_CODES)[number];

const callbackErrorCodes: ReadonlySet<string> = new Set(CALLBACK_ERROR_CODES);

/**
 * Maps `?error=` to a fixed code. The raw query value is never returned, so
 * it can never be rendered; anything unrecognised becomes "unknown".
 */
export function readCallbackError(
  search: string,
): CallbackErrorCode | "unknown" | null {
  const params = new URLSearchParams(search);
  if (!params.has("error")) return null;
  const values = params.getAll("error");
  const value = values.length === 1 ? values[0] : undefined;
  return value !== undefined && callbackErrorCodes.has(value)
    ? (value as CallbackErrorCode)
    : "unknown";
}

/** Same search string without the `error` parameter (other params are kept). */
export function searchWithoutCallbackError(search: string) {
  const params = new URLSearchParams(search);
  params.delete("error");
  const next = params.toString();
  return next ? `?${next}` : "";
}

export type LinkProvider = "google" | "oidc" | "facebook" | "microsoft";

export function linkProviderFrom(value: string | null): LinkProvider {
  if (value === "oidc" || value === "facebook" || value === "microsoft") {
    return value;
  }
  return "google";
}

/** Providers whose new subjects verify an email before an account exists. */
export type RegistrationProvider = "facebook" | "microsoft";

export function registrationProviderFrom(
  value: string | null,
): RegistrationProvider {
  return value === "microsoft" ? "microsoft" : "facebook";
}

/**
 * `/sign-in?link=pending&provider=<p>` (set by the OAuth callback) moves to the
 * confirmation screen. The HttpOnly link cookie never reaches JavaScript.
 */
export function linkPendingTarget(search: string) {
  const params = new URLSearchParams(search);
  if (params.get("link") !== "pending") return null;
  const provider = linkProviderFrom(params.get("provider"));
  return `${ACCOUNT_LINK_PATH}?provider=${provider}`;
}

/**
 * `/sign-in?registration=pending&provider=<p>` (set by the Facebook or
 * Microsoft callback for a new subject without a trusted email) moves to the
 * registration screen. The provider is allowlisted, never echoed.
 */
export function registrationPendingTarget(search: string) {
  const params = new URLSearchParams(search);
  if (params.get("registration") !== "pending") return null;
  const provider = registrationProviderFrom(params.get("provider"));
  return `${COMPLETE_REGISTRATION_PATH}?provider=${provider}`;
}

export type AuthProvidersState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ready"; providers: AuthProvidersResponse };

/**
 * Loads the deployment's public sign-in capabilities. A reload keeps the last
 * good configuration on screen so the layout never collapses.
 */
export function useAuthProviders() {
  const [state, setState] = useState<AuthProvidersState>({
    status: "loading",
  });
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    getAuthProviders({
      baseUrl: getApiBaseUrl(),
      signal: controller.signal,
    }).then(
      (providers) => {
        if (!controller.signal.aborted) {
          setState({ status: "ready", providers });
        }
      },
      () => {
        if (!controller.signal.aborted) {
          setState((current) =>
            current.status === "ready" ? current : { status: "error" },
          );
        }
      },
    );
    return () => controller.abort();
  }, [generation]);

  const reload = useCallback(() => {
    setState((current) =>
      current.status === "ready" ? current : { status: "loading" },
    );
    setGeneration((value) => value + 1);
  }, []);

  return { reload, state };
}

export type LoginMode = "pending" | "password" | "oidc" | "unavailable";
export type SignUpMode = "pending" | "native" | "oidc" | "unavailable";

export interface SocialAvailability {
  disabled: boolean;
  facebook: boolean;
  /** `oauth_providers` lists Facebook: the button starts the Facebook flow. */
  facebookFederation: boolean;
  google: boolean;
  /** `oauth_providers` lists Google: the button starts the Google flow. */
  googleFederation: boolean;
  /**
   * `oauth_providers` lists Microsoft. Microsoft has no default-portal
   * fallback, so the button exists only while the server enables it.
   */
  microsoft: boolean;
}

export const NO_SOCIAL: SocialAvailability = {
  disabled: false,
  facebook: false,
  facebookFederation: false,
  google: false,
  googleFederation: false,
  microsoft: false,
};

export function loginModeFor(
  providers: AuthProvidersResponse | null,
): LoginMode {
  if (!providers) return "pending";
  if (providers.password_enabled) return "password";
  return providers.oidc_default_enabled ? "oidc" : "unavailable";
}

export function signUpModeFor(
  providers: AuthProvidersResponse | null,
): SignUpMode {
  if (!providers) return "pending";
  if (providers.signup_enabled) return "native";
  return providers.oidc_default_enabled ? "oidc" : "unavailable";
}

/**
 * Google and Facebook start their own provider-bound flow once the server
 * lists them in `oauth_providers`. Otherwise each stays a fallback entry into
 * the default OIDC portal (when that portal is enabled). Microsoft is shown
 * only when the server lists it (AUTH-07B), never as a portal fallback.
 */
export function socialAvailabilityFor(
  providers: AuthProvidersResponse | null,
): SocialAvailability {
  if (!providers) {
    return {
      disabled: true,
      facebook: true,
      facebookFederation: false,
      google: true,
      googleFederation: false,
      microsoft: false,
    };
  }
  const fallback = providers.oidc_default_enabled;
  const facebookFederation = providers.oauth_providers.includes("facebook");
  const googleFederation = providers.oauth_providers.includes("google");
  return {
    disabled: false,
    facebook: fallback || facebookFederation,
    facebookFederation,
    google: fallback || googleFederation,
    googleFederation,
    microsoft: providers.oauth_providers.includes("microsoft"),
  };
}

export function passwordMinLengthFor(providers: AuthProvidersResponse | null) {
  const value = providers?.password_min_length;
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.ceil(value)
    : MIN_PASSWORD_LENGTH;
}
