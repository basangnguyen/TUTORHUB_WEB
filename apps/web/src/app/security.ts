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
  getMySecurity,
  listMyLoginMethods,
  listMySessions,
  reauthenticate,
  revokeMySession,
  revokeOtherSessions,
  rotateCSRFToken,
  startReauthEmailCode,
  type ReauthenticateRequest,
} from "@tutorhub/api-client";
import { useCallback, useRef, useState } from "react";
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
 * The variables hold the typed password or code: `gcTime: 0` lets the
 * mutation cache drop them as soon as the caller resets the observer.
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

export type StepUpRetry = () => unknown;

export interface StepUpDialogControl {
  onOpenChange: (open: boolean) => void;
  onVerified: () => void;
  open: boolean;
}

/**
 * Owns one shared step-up dialog: `request(retry)` opens it and remembers
 * the blocked action, which runs once after a successful re-authentication
 * (never after a cancel). The caller keeps its own form state, so the user
 * does not have to re-enter it.
 */
export function useStepUpRequest() {
  const [open, setOpen] = useState(false);
  const retryRef = useRef<StepUpRetry | null>(null);

  const request = useCallback((retry?: StepUpRetry) => {
    retryRef.current = retry ?? null;
    setOpen(true);
  }, []);

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

  const dialog: StepUpDialogControl = { onOpenChange, onVerified, open };
  return { dialog, request };
}
