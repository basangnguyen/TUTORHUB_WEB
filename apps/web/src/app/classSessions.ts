import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  cancelClassSession as requestClassSessionCancel,
  createClassSession,
  listClassSessions,
  rotateCSRFToken,
  updateClassSession as requestClassSessionUpdate,
  type CancelClassSessionRequest,
  type ClassSession,
  type CreateClassSessionRequest,
  type UpdateClassSessionRequest,
} from "@tutorhub/api-client";
import { invalidateTenantAudit } from "./audit";
import { invalidateTenantCapabilities } from "./tenantCapabilities";

const sessionPageSize = 50;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const classSessionQueryKeys = {
  all: ["class-sessions"] as const,
  tenant: (tenantID: string) => ["class-sessions", tenantID] as const,
  lists: (tenantID: string, classID: string) =>
    ["class-sessions", tenantID, classID, "list"] as const,
  list: (
    tenantID: string,
    classID: string,
    rangeStart: string,
    rangeEnd: string,
  ) =>
    [
      "class-sessions",
      tenantID,
      classID,
      "list",
      rangeStart,
      rangeEnd,
    ] as const,
  detail: (tenantID: string, classID: string, sessionID: string) =>
    ["class-sessions", tenantID, classID, "detail", sessionID] as const,
};

function shouldRetrySessionQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useClassSessionList(
  tenantID: string | undefined,
  classID: string | undefined,
  rangeStart: string,
  rangeEnd: string,
) {
  return useInfiniteQuery({
    queryKey: classSessionQueryKeys.list(
      tenantID ?? "inactive",
      classID ?? "invalid",
      rangeStart,
      rangeEnd,
    ),
    queryFn: ({ pageParam, signal }) =>
      listClassSessions(
        classID ?? "",
        {
          cursor: pageParam ?? undefined,
          limit: sessionPageSize,
          range_end: rangeEnd,
          range_start: rangeStart,
        },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: Boolean(tenantID && classID && rangeStart && rangeEnd),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetrySessionQuery,
    staleTime: 20_000,
  });
}

async function synchronizeSession(
  queryClient: QueryClient,
  tenantID: string,
  session: ClassSession,
) {
  await queryClient.cancelQueries({
    queryKey: classSessionQueryKeys.lists(tenantID, session.class_id),
  });
  queryClient.setQueryData(
    classSessionQueryKeys.detail(tenantID, session.class_id, session.id),
    session,
  );
  await queryClient.invalidateQueries({
    queryKey: classSessionQueryKeys.lists(tenantID, session.class_id),
  });
}

export function useCreateClassSession(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      classID,
      input,
    }: {
      classID: string;
      input: CreateClassSessionRequest;
    }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createClassSession(classID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async (session) => {
      if (tenantID) {
        await synchronizeSession(queryClient, tenantID, session);
      }
    },
    onSettled: () =>
      Promise.all([
        invalidateTenantAudit(queryClient, tenantID),
        invalidateTenantCapabilities(queryClient, tenantID),
      ]),
    retry: false,
  });
}

export function useUpdateClassSession(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      classID,
      sessionID,
      input,
    }: {
      classID: string;
      sessionID: string;
      input: UpdateClassSessionRequest;
    }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return requestClassSessionUpdate(
        classID,
        sessionID,
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: async (session) => {
      if (tenantID) {
        await synchronizeSession(queryClient, tenantID, session);
      }
    },
    onSettled: (_session, error, variables) =>
      Promise.all([
        error && tenantID
          ? queryClient.invalidateQueries({
              exact: true,
              queryKey: classSessionQueryKeys.detail(
                tenantID,
                variables.classID,
                variables.sessionID,
              ),
            })
          : Promise.resolve(),
        invalidateTenantAudit(queryClient, tenantID),
      ]),
    retry: false,
  });
}

export function useCancelClassSession(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      classID,
      sessionID,
      input,
    }: {
      classID: string;
      sessionID: string;
      input: CancelClassSessionRequest;
    }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return requestClassSessionCancel(
        classID,
        sessionID,
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: async (session) => {
      if (tenantID) {
        await synchronizeSession(queryClient, tenantID, session);
      }
    },
    onSettled: (_session, error, variables) =>
      Promise.all([
        error && tenantID
          ? queryClient.invalidateQueries({
              exact: true,
              queryKey: classSessionQueryKeys.detail(
                tenantID,
                variables.classID,
                variables.sessionID,
              ),
            })
          : Promise.resolve(),
        invalidateTenantAudit(queryClient, tenantID),
      ]),
    retry: false,
  });
}
