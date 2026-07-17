import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  beginIdentityLink,
  getProfile,
  listIdentities,
  rotateCSRFToken,
  unlinkIdentity,
  updateProfile,
  type ProfileUpdateRequest,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const profileQueryKeys = {
  detail: ["profile", "detail"] as const,
  identities: ["profile", "identities"] as const,
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
