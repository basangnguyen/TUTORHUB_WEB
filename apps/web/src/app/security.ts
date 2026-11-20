import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  cancelLinkIntent,
  confirmLinkIntent,
  confirmTOTP,
  disableTOTP,
  enrollTOTP,
  getMyPhone,
  getMySecurity,
  listMyLoginMethods,
  listMyPasskeys,
  listMySessions,
  reauthenticate,
  regenerateRecoveryCodes,
  removeMyPhone,
  renameMyPasskey,
  resendMyPhoneChallenge,
  revokeMyPasskey,
  revokeMySession,
  revokeOtherSessions,
  rotateCSRFToken,
  startPasskeyReauthentication,
  startMyPhone,
  startPasskeyRegistration,
  startReauthEmailCode,
  verifyMyPhone,
  verifyPasskeyRegistration,
  type PhoneStartRequest,
  type PhoneVerifyRequest,
  type ReauthenticateRequest,
} from "@tutorhub/api-client";
import { useCallback, useRef, useState } from "react";
import { createPasskeyAttestation } from "./passkeys";
import { profileQueryKeys } from "./profile";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

/**
 * AUTH-04B security overview, login-method inventory and device sessions.
 * The keys never carry passwords, codes or tokens.
 */
export const securityQueryKeys = {
  overview: ["security", "overview"] as const,
  loginMethods: ["security", "login-methods"] as const,
  sessions: ["security", "sessions"] as const,
  passkeys: ["security", "passkeys"] as const,
  /** AUTH-10B: GET /me/phone (masked number only, never the raw number). */
  phone: ["security", "phone"] as const,
};

/** The server's step-up contract: 403 with this exact problem code. */
export const RECENT_AUTHENTICATION_REQUIRED_CODE =
  "auth_recent_authentication_required";

export function isRecentAuthenticationRequired(error: unknown) {
  return (
    error instanceof APIRequestError &&
    error.status === 403 &&
    error.problem?.code === RECENT_AUTHENTICATION_REQUIRED_CODE
  );
}

/**
 * AUTH-09A: the account has a second factor (TOTP or a passkey), so changing
 * factors needs an AAL2 session; a password or email-code step-up keeps the
 * session at AAL1 and cannot clear it.
 */
export const ASSURANCE_REQUIRED_CODE = "auth_assurance_required";
/** AUTH-09A: an `org_admin` workspace needs an AAL2 session. */
export const ADMIN_MFA_REQUIRED_CODE = "auth_admin_mfa_required";

function hasForbiddenCode(error: unknown, code: string) {
  return (
    error instanceof APIRequestError &&
    error.status === 403 &&
    error.problem?.code === code
  );
}

export function isAssuranceRequired(error: unknown) {
  return hasForbiddenCode(error, ASSURANCE_REQUIRED_CODE);
}

export function isAdminMFARequired(error: unknown) {
  return hasForbiddenCode(error, ADMIN_MFA_REQUIRED_CODE);
}

export interface StepUpRequestOptions {
  /**
   * Offer only the factors that raise the session to AAL2 (passkey, TOTP).
   * Set for 403 `auth_assurance_required` and `auth_admin_mfa_required`.
   */
  requireAal2?: boolean;
}

/**
 * Which step-up clears a refused action: any re-authentication for a stale
 * session, an AAL2 factor for an assurance refusal, or `null` when a step-up
 * cannot help.
 */
export function stepUpRequirementFor(
  error: unknown,
): Required<StepUpRequestOptions> | null {
  if (isRecentAuthenticationRequired(error)) return { requireAal2: false };
  if (isAssuranceRequired(error) || isAdminMFARequired(error)) {
    return { requireAal2: true };
  }
  return null;
}

/**
 * Client errors and Retry-After answers are deterministic: retrying them
 * immediately only spends the server's rate limit.
 */
function shouldRetrySecurityQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      (error.status < 500 || error.status === 503)
    )
  );
}

export function useMySecurityQuery(enabled = true) {
  return useQuery({
    queryKey: securityQueryKeys.overview,
    queryFn: ({ signal }) =>
      getMySecurity({ baseUrl: getApiBaseUrl(), signal }),
    enabled,
    retry: shouldRetrySecurityQuery,
    staleTime: 15_000,
  });
}

export function useMyLoginMethodsQuery(enabled = true) {
  return useQuery({
    queryKey: securityQueryKeys.loginMethods,
    queryFn: ({ signal }) =>
      listMyLoginMethods({ baseUrl: getApiBaseUrl(), signal }),
    enabled,
    retry: shouldRetrySecurityQuery,
    staleTime: 30_000,
  });
}

/** AUTH-08B: GET /me/passkeys (only while `passkeys_enabled` is true). */
export function useMyPasskeysQuery(enabled = true) {
  return useQuery({
    queryKey: securityQueryKeys.passkeys,
    queryFn: ({ signal }) =>
      listMyPasskeys({ baseUrl: getApiBaseUrl(), signal }),
    enabled,
    retry: shouldRetrySecurityQuery,
    staleTime: 15_000,
  });
}

export function useMySessionsQuery(enabled = true) {
  return useQuery({
    queryKey: securityQueryKeys.sessions,
    queryFn: ({ signal }) =>
      listMySessions({ baseUrl: getApiBaseUrl(), signal }),
    enabled,
    retry: shouldRetrySecurityQuery,
    staleTime: 15_000,
  });
}

/**
 * Runs after a successful step-up: the server rotated the session and CSRF
 * cookies. Resync the in-memory CSRF token, then reload the principal and the
 * security data so the blocked action runs against the new session.
 */
export async function synchronizeAfterReauthentication(
  queryClient: QueryClient,
) {
  try {
    await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
  } catch {
    // Best effort: every protected mutation rotates the token before it runs.
  }
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["auth", "me"] }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.overview }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.loginMethods }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.sessions }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.passkeys }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.phone }),
    queryClient.invalidateQueries({ queryKey: profileQueryKeys.password }),
  ]);
}

/**
 * AUTH-05: confirming a link intent links the identity, revokes the other
 * sessions and rotates this session and its CSRF token. Resync like a step-up
 * and also reload the linked-identity list.
 */
export async function synchronizeAfterLinkConfirmation(
  queryClient: QueryClient,
) {
  await Promise.all([
    synchronizeAfterReauthentication(queryClient),
    queryClient.invalidateQueries({ queryKey: profileQueryKeys.identities }),
  ]);
}

/** The server requires the session's latest CSRF token and the same Origin. */
export function useConfirmLinkIntent() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return confirmLinkIntent(csrf.csrf_token, { baseUrl: getApiBaseUrl() });
    },
    onSuccess: () => synchronizeAfterLinkConfirmation(queryClient),
  });
}

/** Works signed in or not: only the HttpOnly link cookie is required. */
export function useCancelLinkIntent() {
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: () => cancelLinkIntent({ baseUrl: getApiBaseUrl() }),
  });
}

export function useStartReauthEmailCode() {
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return startReauthEmailCode(csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
  });
}

/**
 * AUTH-08B: step-up options for a passkey. The server binds the challenge to
 * this user, session and context version through the HttpOnly `auth_tx`
 * cookie; the options never leave memory.
 */
export function useStartPasskeyReauthentication() {
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return startPasskeyReauthentication(csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
  });
}

/**
 * The variables hold the typed password or code (or a passkey assertion):
 * `gcTime: 0` lets the mutation cache drop them as soon as the caller resets
 * the observer.
 */
export function useReauthenticate() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (input: ReauthenticateRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return reauthenticate(input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: () => synchronizeAfterReauthentication(queryClient),
  });
}

export function useRevokeMySession() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (sessionID: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      await revokeMySession(sessionID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: securityQueryKeys.sessions }),
  });
}

export function useRevokeOtherSessions() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return revokeOtherSessions(csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: securityQueryKeys.sessions }),
  });
}

/** Reloads every view that shows passkeys or the sign-in methods. */
function invalidatePasskeyViews(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.passkeys }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.overview }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.loginMethods }),
  ]);
}

/**
 * AUTH-08B: options → `navigator.credentials.create` → verify. Each POST
 * rotates the CSRF token first. A 403 `auth_recent_authentication_required`
 * (from the options or the verify step) is the caller's cue to step up.
 */
export function useRegisterPasskey() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (label: string) => {
      const baseUrl = getApiBaseUrl();
      const csrf = await rotateCSRFToken({ baseUrl });
      const options = await startPasskeyRegistration(csrf.csrf_token, {
        baseUrl,
      });
      const attestation = await createPasskeyAttestation(options);
      const verifyCSRF = await rotateCSRFToken({ baseUrl });
      return verifyPasskeyRegistration(
        { attestation, label },
        verifyCSRF.csrf_token,
        { baseUrl },
      );
    },
    onSuccess: () => invalidatePasskeyViews(queryClient),
  });
}

export function useRenameMyPasskey() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async ({ id, label }: { id: string; label: string }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return renameMyPasskey(id, { label }, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: securityQueryKeys.passkeys }),
  });
}

export function useRevokeMyPasskey() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (id: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      await revokeMyPasskey(id, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: () => invalidatePasskeyViews(queryClient),
  });
}

/* ---------- AUTH-09B: authenticator app (TOTP) and recovery codes ---------- */

/**
 * POST /me/mfa/totp/enroll. The result carries the TOTP seed: `gcTime: 0`
 * and the caller's `reset()` keep it out of the mutation cache; it lives
 * only in the enrollment card's state until confirmed or cancelled.
 */
export function useEnrollTOTP() {
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return enrollTOTP(csrf.csrf_token, { baseUrl: getApiBaseUrl() });
    },
  });
}

/**
 * POST /me/mfa/totp/confirm. On success the server raised this session to
 * AAL2, revoked the other AAL1 sessions and rotated the session and CSRF
 * cookies: resync like a step-up before anything else runs.
 */
export function useConfirmTOTP() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (code: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return confirmTOTP(code, csrf.csrf_token, { baseUrl: getApiBaseUrl() });
    },
    onSuccess: () => synchronizeAfterReauthentication(queryClient),
  });
}

/** POST /me/mfa/recovery-codes: the previous set stops working at once. */
export function useRegenerateRecoveryCodes() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return regenerateRecoveryCodes(csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: securityQueryKeys.overview }),
  });
}

/** DELETE /me/mfa/totp: also invalidates every recovery code. */
export function useDisableTOTP() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      await disableTOTP(csrf.csrf_token, { baseUrl: getApiBaseUrl() });
    },
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: securityQueryKeys.overview }),
        queryClient.invalidateQueries({
          queryKey: securityQueryKeys.loginMethods,
        }),
      ]),
  });
}

/* ---------- AUTH-10B: phone number in Settings ---------- */

/**
 * GET /me/phone (only while `phone_enabled` is true). The answer carries the
 * masked number only; the key never holds a number or a code.
 */
export function useMyPhoneQuery(enabled = true) {
  return useQuery({
    queryKey: securityQueryKeys.phone,
    queryFn: ({ signal }) => getMyPhone({ baseUrl: getApiBaseUrl(), signal }),
    enabled,
    retry: shouldRetrySecurityQuery,
    staleTime: 15_000,
  });
}

/**
 * POST /me/phone/start (rotates the CSRF token first). The variables hold
 * the typed number: `gcTime: 0` plus the caller's `reset()` drop them.
 */
export function useStartMyPhone() {
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: (input: PhoneStartRequest) =>
      startMyPhone(input, { baseUrl: getApiBaseUrl() }),
  });
}

/** POST /auth/challenges/resend for the Settings transaction. */
export function useResendMyPhoneChallenge() {
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: () => resendMyPhoneChallenge({ baseUrl: getApiBaseUrl() }),
  });
}

/** Reloads every view that shows the phone number or the sign-in methods. */
function invalidatePhoneViews(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.phone }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.overview }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.loginMethods }),
    queryClient.invalidateQueries({ queryKey: securityQueryKeys.sessions }),
  ]);
}

/**
 * POST /me/phone/verify. The variables hold the typed code (`gcTime: 0`).
 * Changing the number signs out the sessions the old number opened, so the
 * device list reloads as well.
 */
export function useVerifyMyPhone() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: (input: PhoneVerifyRequest) =>
      verifyMyPhone(input, { baseUrl: getApiBaseUrl() }),
    onSuccess: (result) => {
      queryClient.setQueryData(securityQueryKeys.phone, result);
      return invalidatePhoneViews(queryClient);
    },
  });
}

/**
 * DELETE /me/phone. The server signs out every other session and rotates
 * this session and its CSRF token: resync like a step-up.
 */
export function useRemoveMyPhone() {
  const queryClient = useQueryClient();

  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: () => removeMyPhone({ baseUrl: getApiBaseUrl() }),
    onSuccess: async () => {
      queryClient.setQueryData(securityQueryKeys.phone, { phone: null });
      await Promise.all([
        synchronizeAfterReauthentication(queryClient),
        invalidatePhoneViews(queryClient),
      ]);
    },
  });
}

export type StepUpRetry = () => unknown;

export interface StepUpDialogControl {
  onOpenChange: (open: boolean) => void;
  onVerified: () => void;
  open: boolean;
  /** Only AAL2 factors (passkey, TOTP) may clear the refused action. */
  requireAal2: boolean;
}

/**
 * Owns one shared step-up dialog: `request(retry, options)` opens it and
 * remembers the blocked action, which runs once after a successful
 * re-authentication (never after a cancel). `options.requireAal2` limits the
 * dialog to AAL2 factors. The caller keeps its own form state, so the user
 * does not have to re-enter it.
 */
export function useStepUpRequest() {
  const [open, setOpen] = useState(false);
  const [requireAal2, setRequireAal2] = useState(false);
  const retryRef = useRef<StepUpRetry | null>(null);

  const request = useCallback(
    (retry?: StepUpRetry, options?: StepUpRequestOptions) => {
      retryRef.current = retry ?? null;
      setRequireAal2(options?.requireAal2 === true);
      setOpen(true);
    },
    [],
  );

  const onOpenChange = useCallback((nextOpen: boolean) => {
    if (!nextOpen) {
      retryRef.current = null;
    }
    setOpen(nextOpen);
  }, []);

  const onVerified = useCallback(() => {
    const retry = retryRef.current;
    retryRef.current = null;
    setOpen(false);
    if (retry) {
      void retry();
    }
  }, []);

  const dialog: StepUpDialogControl = {
    onOpenChange,
    onVerified,
    open,
    requireAal2,
  };
  return { dialog, request };
}
