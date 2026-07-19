import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  bulkMutateClassRoster,
  listClassRoster,
  rotateCSRFToken,
  updateClassRosterRole,
  type ClassEnrollmentRole,
  type ClassEnrollmentStatus,
  type ClassRosterBulkRequest,
  type ClassRosterBulkResponse,
  type ClassRosterMutationResponse,
} from "@tutorhub/api-client";
import { classEnrollmentQueryKeys } from "./classEnrollments";

const rosterPageSize = 25;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export type RosterStatusFilter = "all" | ClassEnrollmentStatus;

export function normalizeRosterSearch(value: string) {
  return value.normalize("NFC").trim().replace(/\s+/gu, " ").toLowerCase();
}

function shouldRetryRosterQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useClassRoster(
  tenantID: string | undefined,
  classID: string | undefined,
  search: string,
  status: RosterStatusFilter,
  enabled: boolean,
) {
  const normalizedSearch = normalizeRosterSearch(search);
  return useInfiniteQuery({
    queryKey: classEnrollmentQueryKeys.roster(
      tenantID ?? "inactive",
      classID ?? "invalid",
      normalizedSearch,
      status,
    ),
    queryFn: ({ pageParam, signal }) =>
      listClassRoster(
        classID ?? "",
        {
          cursor: pageParam ?? undefined,
          limit: rosterPageSize,
          search: normalizedSearch || undefined,
          status: status === "all" ? undefined : status,
        },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(tenantID && classID),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryRosterQuery,
    staleTime: 15_000,
  });
}

interface UpdateRosterRoleVariables {
  classID: string;
  classRole: ClassEnrollmentRole;
  userID: string;
}

export function useUpdateClassRosterRole(
  tenantID: string | undefined,
  classID: string,
) {
  const queryClient = useQueryClient();
  return useMutation<
    ClassRosterMutationResponse,
    Error,
    UpdateRosterRoleVariables
  >({
    mutationFn: async ({ classID: targetClassID, classRole, userID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return updateClassRosterRole(
        targetClassID,
        userID,
        { class_role: classRole },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSettled: async () => {
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

interface BulkRosterVariables {
  classID: string;
  input: ClassRosterBulkRequest;
}

export function useBulkMutateClassRoster(
  tenantID: string | undefined,
  classID: string,
) {
  const queryClient = useQueryClient();
  return useMutation<ClassRosterBulkResponse, Error, BulkRosterVariables>({
    mutationFn: async ({ classID: targetClassID, input }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return bulkMutateClassRoster(targetClassID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSettled: async () => {
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
