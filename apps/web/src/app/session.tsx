/* eslint-disable react-refresh/only-export-components -- This context module intentionally exports its hook and session contract. */

import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  getCurrentUser,
  getLoginURL,
  logout,
  rotateCSRFToken,
  type CurrentUser,
} from "@tutorhub/api-client";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type PropsWithChildren,
} from "react";

export type SessionStatus =
  "loading" | "authenticated" | "unauthenticated" | "error";

interface SessionContextValue {
  currentUser: CurrentUser | null;
  error: Error | null;
  status: SessionStatus;
  signIn: (returnTo?: string) => void;
  signOut: () => Promise<void>;
}

export type SessionMode =
  { kind: "remote" } | { kind: "static"; currentUser: CurrentUser | null };

const SessionContext = createContext<SessionContextValue | null>(null);

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function navigateToLogin(returnTo = "/app/home") {
  window.location.assign(
    getLoginURL(returnTo, {
      baseUrl: getApiBaseUrl(),
    }),
  );
}

export function SessionProvider({
  children,
  mode = { kind: "remote" },
}: PropsWithChildren<{ mode?: SessionMode }>) {
  if (mode.kind === "static") {
    return (
      <SessionContext.Provider
        value={{
          currentUser: mode.currentUser,
          error: null,
          status: mode.currentUser ? "authenticated" : "unauthenticated",
          signIn: navigateToLogin,
          signOut: async () => undefined,
        }}
      >
        {children}
      </SessionContext.Provider>
    );
  }

  return <RemoteSessionProvider>{children}</RemoteSessionProvider>;
}

function RemoteSessionProvider({ children }: PropsWithChildren) {
  const queryClient = useQueryClient();
  const [isSigningOut, setIsSigningOut] = useState(false);
  const [signOutError, setSignOutError] = useState<Error | null>(null);
  const sessionQuery = useQuery({
    queryKey: ["auth", "me"],
    queryFn: ({ signal }) =>
      getCurrentUser({ baseUrl: getApiBaseUrl(), signal }),
    retry: (failureCount, error) =>
      failureCount < 1 &&
      !(
        error instanceof APIRequestError &&
        [401, 403, 503].includes(error.status)
      ),
    staleTime: 30_000,
  });

  const signOut = useCallback(async () => {
    setIsSigningOut(true);
    setSignOutError(null);
    try {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const result = await logout(csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
      queryClient.removeQueries({ queryKey: ["auth"] });
      window.location.assign(result.logout_url ?? "/signed-out");
    } catch (error) {
      setSignOutError(
        error instanceof Error
          ? error
          : new Error("The sign-out request could not be completed."),
      );
    } finally {
      setIsSigningOut(false);
    }
  }, [queryClient]);

  const value = useMemo<SessionContextValue>(() => {
    if (isSigningOut || sessionQuery.isPending) {
      return {
        currentUser: sessionQuery.data ?? null,
        error: null,
        status: "loading",
        signIn: navigateToLogin,
        signOut,
      };
    }
    if (signOutError) {
      return {
        currentUser: sessionQuery.data ?? null,
        error: signOutError,
        status: "error",
        signIn: navigateToLogin,
        signOut,
      };
    }
    if (sessionQuery.isSuccess) {
      return {
        currentUser: sessionQuery.data,
        error: null,
        status: "authenticated",
        signIn: navigateToLogin,
        signOut,
      };
    }
    if (
      sessionQuery.error instanceof APIRequestError &&
      sessionQuery.error.status === 401
    ) {
      return {
        currentUser: null,
        error: null,
        status: "unauthenticated",
        signIn: navigateToLogin,
        signOut,
      };
    }
    return {
      currentUser: null,
      error:
        sessionQuery.error instanceof Error
          ? sessionQuery.error
          : new Error("Authentication status is unavailable."),
      status: "error",
      signIn: navigateToLogin,
      signOut,
    };
  }, [
    isSigningOut,
    sessionQuery.data,
    sessionQuery.error,
    sessionQuery.isPending,
    sessionQuery.isSuccess,
    signOutError,
    signOut,
  ]);

  return (
    <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
  );
}

export function useSession() {
  const context = useContext(SessionContext);
  if (!context) {
    throw new Error("useSession must be used inside SessionProvider.");
  }
  return context;
}
