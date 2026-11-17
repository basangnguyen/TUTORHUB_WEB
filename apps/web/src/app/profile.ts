import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  beginIdentityLink,
  getAuthProviders,
  getMyPasswordStatus,
  getProfile,
  listIdentities,
  rotateCSRFToken,
  unlinkIdentity,
  updateMyPassword,
  updateProfile,
  type MyPasswordUpdateRequest,
  type ProfileUpdateRequest,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const profileQueryKeys = {
  detail: ["profile", "detail"] as const,
  identities: ["profile", "identities"] as const,
  password: ["profile", "password"] as const,
  authProviders: ["profile", "auth-providers"] as const,
};

export function useProfileQuery() {
  return useQuery({
    queryKey: profileQueryKeys.detail,
    queryFn: ({ signal }) => getProfile({ baseUrl: getApiBaseUrl(), signal }),
    staleTime: 30_000,
  });
}

export function useIdentitiesQuery() {
  return useQuery({
    queryKey: profileQueryKeys.identities,
    queryFn: ({ signal }) =>
      listIdentities({ baseUrl: getApiBaseUrl(), signal }),
    staleTime: 30_000,
  });
}

export function usePasswordStatusQuery() {
  return useQuery({
    queryKey: profileQueryKeys.password,
    queryFn: ({ signal }) =>
      getMyPasswordStatus({ baseUrl: getApiBaseUrl(), signal }),
    staleTime: 30_000,
  });
}

/** Public sign-in capabilities (minimum length, OIDC re-authentication). */
export function useAuthProvidersQuery() {
  return useQuery({
    queryKey: profileQueryKeys.authProviders,
    queryFn: ({ signal }) =>
      getAuthProviders({ baseUrl: getApiBaseUrl(), signal }),
    staleTime: 5 * 60_000,
  });
}

export function useUpdateProfile() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: ProfileUpdateRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return updateProfile(input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (profile) => {
      queryClient.setQueryData(profileQueryKeys.detail, profile);
    },
  });
}

export function useUpdateMyPassword() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: MyPasswordUpdateRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return updateMyPassword(input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async () => {
      // The server rotated the session and CSRF cookie: resync the in-memory
      // token. Best effort only, because every CSRF mutation rotates first and
      // a failure here must not report the completed change as an error.
      try {
        await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      } catch {
        // The next protected action fetches a fresh token before it runs.
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: profileQueryKeys.password }),
        queryClient.invalidateQueries({ queryKey: ["auth", "me"] }),
      ]);
    },
  });
}

export function useBeginIdentityLink() {
  return useMutation({
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return beginIdentityLink(csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
  });
}

export function useUnlinkIdentity() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (identityID: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      await unlinkIdentity(identityID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: profileQueryKeys.identities,
      });
    },
  });
}
