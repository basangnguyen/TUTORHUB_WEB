import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  createClassEnrollment,
  createClassInviteCode,
  joinClassInvitation,
  leaveClass,
  listClassInviteCodes,
  revokeClassInviteCode,
  rotateCSRFToken,
  type ClassEnrollment,
  type ClassInviteCode,
  type ClassInviteCodeListResponse,
  type CreateClassInviteCodeRequest,
  type CreateClassInviteCodeResponse,
  type JoinClassInvitationResponse,
} from "@tutorhub/api-client";
import { classQueryKeys } from "./classes";
import { useSession } from "./session";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const classEnrollmentQueryKeys = {
  rosters: (tenantID: string, classID: string) =>
    ["classes", tenantID, "detail", classID, "roster"] as const,
  roster: (tenantID: string, classID: string, search: string, status: string) =>
    [
      ...classEnrollmentQueryKeys.rosters(tenantID, classID),
      { search, status },
    ] as const,
  inviteCodes: (tenantID: string, classID: string) =>
    ["classes", tenantID, "detail", classID, "invite-codes"] as const,
};

function shouldRetryEnrollmentQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useClassInviteCodes(
  tenantID: string | undefined,
  classID: string | undefined,
  enabled: boolean,
) {
  return useQuery<ClassInviteCodeListResponse>({
    queryKey: classEnrollmentQueryKeys.inviteCodes(
      tenantID ?? "inactive",
      classID ?? "invalid",
    ),
    queryFn: ({ signal }) =>
      listClassInviteCodes(classID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: enabled && Boolean(tenantID && classID),
    retry: shouldRetryEnrollmentQuery,
    staleTime: 15_000,
  });
}

interface DirectEnrollVariables {
  classID: string;
  memberEmail: string;
}

export function useDirectClassEnrollment(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation<ClassEnrollment, Error, DirectEnrollVariables>({
    mutationFn: async ({ classID, memberEmail }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createClassEnrollment(
        classID,
        { member_email: memberEmail },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: async (_, { classID }) => {
      if (!tenantID) {
        return;
      }
      await queryClient.invalidateQueries({
        queryKey: classEnrollmentQueryKeys.rosters(tenantID, classID),
      });
    },
    retry: false,
  });
}

interface CreateInviteCodeVariables {
  classID: string;
  input: CreateClassInviteCodeRequest;
  tenantID: string;
}

export function useCreateClassInviteCode() {
  const queryClient = useQueryClient();

  return useMutation<
    CreateClassInviteCodeResponse,
    Error,
    CreateInviteCodeVariables
  >({
    gcTime: 0,
    mutationFn: async ({ classID, input }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createClassInviteCode(classID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: ({ invite_code: inviteCode }, { classID, tenantID }) => {
      const queryKey = classEnrollmentQueryKeys.inviteCodes(tenantID, classID);
      queryClient.setQueryData<ClassInviteCodeListResponse>(
        queryKey,
        (current) =>
          current
            ? {
                items: [
                  inviteCode,
                  ...current.items.filter((item) => item.id !== inviteCode.id),
                ],
              }
            : current,
      );
    },
    retry: false,
  });
}

interface RevokeInviteCodeVariables {
  classID: string;
  codeID: string;
  tenantID: string;
}

export function useRevokeClassInviteCode() {
  const queryClient = useQueryClient();

  return useMutation<ClassInviteCode, Error, RevokeInviteCodeVariables>({
    mutationFn: async ({ classID, codeID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return revokeClassInviteCode(classID, codeID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (inviteCode, { classID, tenantID }) => {
      const queryKey = classEnrollmentQueryKeys.inviteCodes(tenantID, classID);
      queryClient.setQueryData<ClassInviteCodeListResponse>(
        queryKey,
        (current) =>
          current
            ? {
                items: current.items.map((item) =>
                  item.id === inviteCode.id ? inviteCode : item,
                ),
              }
            : current,
      );
    },
    retry: false,
  });
}

export function useJoinClassInvitation(token: string | null) {
  const queryClient = useQueryClient();
  const session = useSession();

  return useMutation<JoinClassInvitationResponse, Error, void>({
    gcTime: 0,
    mutationFn: async () => {
      if (!token) {
        throw new Error("The class invitation token is unavailable.");
      }
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return joinClassInvitation({ token }, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async ({ classroom }) => {
      const tenantID = session.currentUser?.active_tenant?.id;
      if (!tenantID) {
        return;
      }
      queryClient.setQueryData(
        classQueryKeys.detail(tenantID, classroom.id),
        classroom,
      );
      await queryClient.invalidateQueries({
        queryKey: classQueryKeys.lists(tenantID),
      });
    },
    retry: false,
  });
}

interface LeaveClassVariables {
  classID: string;
  tenantID: string;
}

export function useLeaveClass() {
  const queryClient = useQueryClient();

  return useMutation<ClassEnrollment, Error, LeaveClassVariables>({
    mutationFn: async ({ classID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return leaveClass(classID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async (_, { classID, tenantID }) => {
      queryClient.removeQueries({
        queryKey: classQueryKeys.detail(tenantID, classID),
        exact: true,
      });
      queryClient.removeQueries({
        queryKey: classEnrollmentQueryKeys.inviteCodes(tenantID, classID),
        exact: true,
      });
      await queryClient.invalidateQueries({
        queryKey: classQueryKeys.lists(tenantID),
      });
    },
    retry: false,
  });
}
