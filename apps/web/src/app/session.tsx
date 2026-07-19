/* eslint-disable react-refresh/only-export-components -- This context module intentionally exports its hook and session contract. */

import {
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
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
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from "react";
import { clearExpiredSessionCaches } from "./queryClient";

export type SessionStatus =
  "loading" | "authenticated" | "unauthenticated" | "error";

interface SessionContextValue {
  currentUser: CurrentUser | null;
  error: Error | null;
  status: SessionStatus;
  signIn: (returnTo?: string) => void;
  signOut: () => Promise<void>;
  refresh: () => Promise<void>;
  replaceCurrentUser: (currentUser: CurrentUser) => void;
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

export async function clearPrivateSessionCache(queryClient: QueryClient) {
  await queryClient.cancelQueries();
  queryClient.clear();
}

export function SessionProvider({
  children,
  mode = { kind: "remote" },
}: PropsWithChildren<{ mode?: SessionMode }>) {
  if (mode.kind === "static") {
    return (
      <StaticSessionProvider initialCurrentUser={mode.currentUser}>
        {children}
      </StaticSessionProvider>
    );
  }

  return <RemoteSessionProvider>{children}</RemoteSessionProvider>;
}

function StaticSessionProvider({
  children,
  initialCurrentUser,
}: PropsWithChildren<{ initialCurrentUser: CurrentUser | null }>) {
  const [currentUser, setCurrentUser] = useState(initialCurrentUser);
  const value = useMemo<SessionContextValue>(
    () => ({
      currentUser,
      error: null,
      status: currentUser ? "authenticated" : "unauthenticated",
      signIn: navigateToLogin,
      signOut: async () => undefined,
      refresh: async () => undefined,
      replaceCurrentUser: setCurrentUser,
    }),
    [currentUser],
  );

  return (
    <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
  );
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
      await clearPrivateSessionCache(queryClient);
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

  const refresh = useCallback(async () => {
    const result = await sessionQuery.refetch();
    if (result.error) {
      throw result.error;
    }
  }, [sessionQuery]);

  const replaceCurrentUser = useCallback(
    (currentUser: CurrentUser) => {
      queryClient.setQueryData(["auth", "me"], currentUser);
    },
    [queryClient],
  );

  const value = useMemo<SessionContextValue>(() => {
    if (isSigningOut || sessionQuery.isPending) {
      return {
        currentUser: sessionQuery.data ?? null,
        error: null,
        status: "loading",
        signIn: navigateToLogin,
        signOut,
        refresh,
        replaceCurrentUser,
      };
    }
    if (signOutError) {
      return {
        currentUser: sessionQuery.data ?? null,
        error: signOutError,
        status: "error",
        signIn: navigateToLogin,
        signOut,
        refresh,
        replaceCurrentUser,
      };
    }
    if (sessionQuery.isSuccess) {
      return {
        currentUser: sessionQuery.data,
        error: null,
        status: "authenticated",
        signIn: navigateToLogin,
        signOut,
        refresh,
        replaceCurrentUser,
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
        refresh,
        replaceCurrentUser,
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
      refresh,
      replaceCurrentUser,
    };
  }, [
    isSigningOut,
    sessionQuery.data,
    sessionQuery.error,
    sessionQuery.isPending,
    sessionQuery.isSuccess,
    signOutError,
    signOut,
    refresh,
    replaceCurrentUser,
  ]);

  useEffect(() => {
    if (sessionQuery.isError) {
      clearExpiredSessionCaches(queryClient);
    }
  }, [queryClient, sessionQuery.isError]);

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
