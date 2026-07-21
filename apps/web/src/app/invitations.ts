import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  acceptMembershipInvitation,
  createMembershipInvitation,
  listMembershipInvitations,
  previewMembershipInvitation,
  revokeMembershipInvitation,
  rotateCSRFToken,
  type CreateMembershipInvitationResponse,
  type InvitableOrganizationRole,
  type MembershipInvitation,
  type MembershipInvitationAcceptResponse,
  type MembershipInvitationListResponse,
  type MembershipInvitationPreview,
} from "@tutorhub/api-client";
import { invalidateTenantAudit } from "./audit";
import { useSession } from "./session";
import { invalidateTenantCapabilities } from "./tenantCapabilities";

export type { InvitableOrganizationRole } from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const membershipInvitationQueryKeys = {
  tenantList: (tenantID: string) =>
    ["tenants", tenantID, "membership-invitations"] as const,
  preview: ["public-membership-invitation", "preview"] as const,
};

function shouldRetryInvitationQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useMembershipInvitationList(tenantID: string | undefined) {
  return useQuery<MembershipInvitationListResponse>({
    queryKey: membershipInvitationQueryKeys.tenantList(tenantID ?? "inactive"),
    queryFn: ({ signal }) =>
      listMembershipInvitations(tenantID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: Boolean(tenantID),
    retry: shouldRetryInvitationQuery,
    staleTime: 15_000,
  });
}

interface CreateMembershipInvitationVariables {
  email: string;
  intendedRole: InvitableOrganizationRole;
  tenantID: string;
}

export function useCreateMembershipInvitation() {
  const queryClient = useQueryClient();

  return useMutation<
    CreateMembershipInvitationResponse,
    Error,
    CreateMembershipInvitationVariables
  >({
    gcTime: 0,
    mutationFn: async ({ email, intendedRole, tenantID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createMembershipInvitation(
        tenantID,
        { email, intended_role: intendedRole },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: async ({ invitation }, { tenantID }) => {
      const queryKey = membershipInvitationQueryKeys.tenantList(tenantID);
      await queryClient.cancelQueries({ exact: true, queryKey });
      queryClient.setQueryData<MembershipInvitationListResponse>(
        queryKey,
        (current) => ({
          ...current,
          items: [
            invitation,
            ...(current?.items ?? []).filter(
              (item) => item.id !== invitation.id,
            ),
          ],
        }),
      );
      await queryClient.invalidateQueries({ exact: true, queryKey });
    },
    onSettled: async (_response, error, variables) => {
      await Promise.all([
        error
          ? queryClient.invalidateQueries({
              exact: true,
              queryKey: membershipInvitationQueryKeys.tenantList(
                variables.tenantID,
              ),
            })
          : Promise.resolve(),
        invalidateTenantAudit(queryClient, variables.tenantID),
        invalidateTenantCapabilities(queryClient, variables.tenantID),
      ]);
    },
    retry: false,
  });
}

interface RevokeMembershipInvitationVariables {
  invitationID: string;
  tenantID: string;
}

export function useRevokeMembershipInvitation() {
  const queryClient = useQueryClient();

  return useMutation<
    MembershipInvitation,
    Error,
    RevokeMembershipInvitationVariables
  >({
    mutationFn: async ({ invitationID, tenantID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return revokeMembershipInvitation(
        tenantID,
        invitationID,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: async (invitation, { tenantID }) => {
      const queryKey = membershipInvitationQueryKeys.tenantList(tenantID);
      await queryClient.cancelQueries({ exact: true, queryKey });
      queryClient.setQueryData<MembershipInvitationListResponse>(
        queryKey,
        (current) =>
          current
            ? {
                ...current,
                items: current.items.map((item) =>
                  item.id === invitation.id ? invitation : item,
                ),
              }
            : { items: [invitation] },
      );
      await queryClient.invalidateQueries({ exact: true, queryKey });
    },
    onSettled: async (_invitation, error, variables) => {
      await Promise.all([
        error
          ? queryClient.invalidateQueries({
              exact: true,
              queryKey: membershipInvitationQueryKeys.tenantList(
                variables.tenantID,
              ),
            })
          : Promise.resolve(),
        invalidateTenantAudit(queryClient, variables.tenantID),
      ]);
    },
    retry: false,
  });
}

export function useMembershipInvitationPreview(
  token: string | null,
  enabled = true,
) {
  return useQuery<MembershipInvitationPreview>({
    queryKey: membershipInvitationQueryKeys.preview,
    queryFn: ({ signal }) =>
      previewMembershipInvitation(
        { token: token ?? "" },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(token),
    gcTime: 0,
    retry: shouldRetryInvitationQuery,
    staleTime: 0,
  });
}

export function useAcceptMembershipInvitation(token: string | null) {
  const queryClient = useQueryClient();
  const session = useSession();

  return useMutation<MembershipInvitationAcceptResponse, Error, void>({
    gcTime: 0,
    mutationFn: async () => {
      if (!token) {
        throw new Error("The membership invitation token is unavailable.");
      }
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return acceptMembershipInvitation({ token }, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async ({ current_user: currentUser }) => {
      await Promise.all([
        queryClient.cancelQueries({ queryKey: ["auth", "me"], exact: true }),
        queryClient.cancelQueries({ queryKey: ["tenants"] }),
      ]);
      queryClient.removeQueries({ queryKey: ["tenants"] });
      session.replaceCurrentUser(currentUser);
    },
    onSettled: (response) =>
      Promise.all([
        invalidateTenantAudit(queryClient, response?.invitation.tenant_id),
        invalidateTenantCapabilities(
          queryClient,
          response?.invitation.tenant_id,
        ),
      ]),
    retry: false,
  });
}
