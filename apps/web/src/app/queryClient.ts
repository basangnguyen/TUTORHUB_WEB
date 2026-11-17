import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { APIRequestError } from "@tutorhub/api-client";
import { clearClientDrafts } from "./clientDrafts";

const currentUserQueryKey = ["auth", "me"] as const;
const principalGenerations = new WeakMap<QueryClient, number>();

export function currentPrincipalGeneration(queryClient: QueryClient) {
  return principalGenerations.get(queryClient) ?? 0;
}

export function advancePrincipalGeneration(queryClient: QueryClient) {
  const generation = currentPrincipalGeneration(queryClient) + 1;
  principalGenerations.set(queryClient, generation);
  return generation;
}

function isCurrentUserQuery(queryKey: readonly unknown[]) {
  return (
    queryKey.length === currentUserQueryKey.length &&
    queryKey.every((part, index) => part === currentUserQueryKey[index])
  );
}

/**
 * 401 responses that carry these problem codes reject a credential the user
 * typed (for example the current password on /me/password) while the session
 * itself stays valid, so they must not purge caches or re-check `/me`.
 */
const credentialRejectionCodes = new Set(["auth_invalid_current_password"]);

function isUnauthorized(error: unknown) {
  return (
    error instanceof APIRequestError &&
    error.status === 401 &&
    !credentialRejectionCodes.has(error.problem?.code ?? "")
  );
}

function purgePrivateCaches(queryClient: QueryClient) {
  advancePrincipalGeneration(queryClient);
  clearClientDrafts();
  void queryClient.cancelQueries({
    predicate: (query) => !isCurrentUserQuery(query.queryKey),
  });
  queryClient.removeQueries({
    predicate: (query) =>
      !isCurrentUserQuery(query.queryKey) && !query.isActive(),
  });
  queryClient.getMutationCache().clear();
}

export function clearExpiredSessionCaches(queryClient: QueryClient) {
  advancePrincipalGeneration(queryClient);
  clearClientDrafts();
  queryClient.removeQueries({
    predicate: (query) => !isCurrentUserQuery(query.queryKey),
  });
  queryClient.getMutationCache().clear();
}

export function createTutorHubQueryClient() {
  let handlingUnauthorized = false;

  const handleUnauthorized = () => {
    if (handlingUnauthorized) {
      return;
    }
    handlingUnauthorized = true;
    purgePrivateCaches(queryClient);
    void queryClient
      .resetQueries({
        exact: true,
        queryKey: currentUserQueryKey,
      })
      .finally(() => {
        // Local mutation callbacks may run after the global error hook.
        purgePrivateCaches(queryClient);
        handlingUnauthorized = false;
      });
  };

  const queryClient = new QueryClient({
    mutationCache: new MutationCache({
      onError: (error) => {
        if (isUnauthorized(error)) {
          handleUnauthorized();
        }
      },
    }),
    queryCache: new QueryCache({
      onError: (error, query) => {
        if (isUnauthorized(error) && !isCurrentUserQuery(query.queryKey)) {
          handleUnauthorized();
        }
      },
    }),
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        retry: (failureCount, error) =>
          failureCount < 1 &&
          !(
            error instanceof APIRequestError &&
            [401, 403, 404].includes(error.status)
          ),
        retryDelay: 350,
        refetchOnWindowFocus: false,
      },
    },
  });

  return queryClient;
}
