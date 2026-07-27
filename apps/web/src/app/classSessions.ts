import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  cancelClassSession as requestClassSessionCancel,
  createClassSession,
  createClassSessionSeries,
  getClassSessionSeries,
  getClassSession,
  listClassSessions,
  previewClassSessionSeriesMutation,
  rotateCSRFToken,
  cancelClassSessionSeriesOccurrence,
  updateClassSessionSeriesOccurrence,
  updateClassSession as requestClassSessionUpdate,
  type CancelClassSessionRequest,
  type ClassSessionOccurrenceMutationRequest,
  type ClassSessionSeriesMutationResponse,
  type ClassSessionSeriesScopePreview,
  type CreateClassSessionSeriesRequest,
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
  series: (tenantID: string, classID: string, seriesID: string) =>
    ["class-session-series", tenantID, classID, seriesID] as const,
  seriesPreview: (
    tenantID: string,
    classID: string,
    seriesID: string,
    input: ClassSessionOccurrenceMutationRequest,
  ) =>
    [
      "class-session-series",
      tenantID,
      classID,
      seriesID,
      "preview",
      input,
    ] as const,
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

export function useClassSessionDetail(
  tenantID: string | undefined,
  classID: string | undefined,
  sessionID: string | undefined,
) {
  return useQuery({
    queryKey: classSessionQueryKeys.detail(
      tenantID ?? "inactive",
      classID ?? "invalid",
      sessionID ?? "invalid",
    ),
    queryFn: ({ signal }) =>
      getClassSession(classID ?? "", sessionID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: Boolean(tenantID && classID && sessionID),
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

async function invalidateCalendarAndSeries(
  queryClient: QueryClient,
  tenantID: string | undefined,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ["calendar"] }),
    invalidateTenantAudit(queryClient, tenantID),
    invalidateTenantCapabilities(queryClient, tenantID),
  ]);
}

export function useClassSessionSeriesDetail(
  tenantID: string | undefined,
  classID: string | undefined,
  seriesID: string | undefined,
) {
  return useQuery({
    queryKey: classSessionQueryKeys.series(
      tenantID ?? "inactive",
      classID ?? "invalid",
      seriesID ?? "invalid",
    ),
    queryFn: ({ signal }) =>
      getClassSessionSeries(classID ?? "", seriesID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: Boolean(tenantID && classID && seriesID),
    retry: shouldRetrySessionQuery,
    staleTime: 20_000,
  });
}

export function useCreateClassSessionSeries(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      classID,
      input,
    }: {
      classID: string;
      input: CreateClassSessionSeriesRequest;
    }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createClassSessionSeries(classID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (series) => {
      queryClient.setQueryData(
        classSessionQueryKeys.series(
          tenantID ?? "inactive",
          series.class_id,
          series.id,
        ),
        series,
      );
    },
    onSettled: () => invalidateCalendarAndSeries(queryClient, tenantID),
    retry: false,
  });
}

export function useClassSessionSeriesMutationPreview(
  tenantID: string | undefined,
  classID: string | undefined,
  seriesID: string | undefined,
  input: ClassSessionOccurrenceMutationRequest | null,
) {
  return useQuery<ClassSessionSeriesScopePreview>({
    queryKey: classSessionQueryKeys.seriesPreview(
      tenantID ?? "inactive",
      classID ?? "invalid",
      seriesID ?? "invalid",
      input ?? {
        expected_version: 1,
        idempotency_key: "inactive-preview-key",
        occurrence_key: "inactive",
        scope: "this_occurrence",
      },
    ),
    queryFn: async ({ signal }) => {
      if (!input) {
        throw new Error("Recurring mutation preview is inactive.");
      }
      const csrf = await rotateCSRFToken({
        baseUrl: getApiBaseUrl(),
        signal,
      });
      return previewClassSessionSeriesMutation(
        classID ?? "",
        seriesID ?? "",
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl(), signal },
      );
    },
    enabled: Boolean(tenantID && classID && seriesID && input),
    refetchOnWindowFocus: false,
    retry: false,
    staleTime: 0,
  });
}

function useSeriesMutation(
  tenantID: string | undefined,
  operation: "update" | "cancel",
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      classID,
      seriesID,
      input,
    }: {
      classID: string;
      seriesID: string;
      input: ClassSessionOccurrenceMutationRequest;
    }): Promise<ClassSessionSeriesMutationResponse> => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return operation === "update"
        ? updateClassSessionSeriesOccurrence(
            classID,
            seriesID,
            input,
            csrf.csrf_token,
            { baseUrl: getApiBaseUrl() },
          )
        : cancelClassSessionSeriesOccurrence(
            classID,
            seriesID,
            input,
            csrf.csrf_token,
            { baseUrl: getApiBaseUrl() },
          );
    },
    onSuccess: (result, variables) => {
      queryClient.setQueryData(
        classSessionQueryKeys.series(
          tenantID ?? "inactive",
          variables.classID,
          result.series.id,
        ),
        result.series,
      );
    },
    onSettled: () => invalidateCalendarAndSeries(queryClient, tenantID),
    retry: false,
  });
}

export function useUpdateClassSessionSeries(tenantID: string | undefined) {
  return useSeriesMutation(tenantID, "update");
}

export function useCancelClassSessionSeries(tenantID: string | undefined) {
  return useSeriesMutation(tenantID, "cancel");
}
