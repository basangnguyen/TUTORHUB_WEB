import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  getClassSessionAudience,
  getClassSessionSeriesAudience,
  getClassSessionSeriesOccurrenceAudience,
  listClassRoster,
  replaceClassSessionAudience,
  replaceClassSessionSeriesAudience,
  replaceClassSessionSeriesOccurrenceAudience,
  respondToClassSession,
  respondToClassSessionSeries,
  respondToClassSessionSeriesOccurrence,
  rotateCSRFToken,
  type ClassRosterPage,
  type ReplaceSessionAudienceRequest,
  type RespondToClassSessionRequest,
  type SessionAudience,
} from "@tutorhub/api-client";

const audienceRosterPageSize = 25;

export type ClassSessionParticipationSource =
  | { kind: "session"; sessionID: string }
  | { kind: "series"; seriesID: string }
  | {
      kind: "occurrence";
      occurrenceKey: string;
      seriesID: string;
    };

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const classSessionParticipationQueryKeys = {
  audience: (
    tenantID: string,
    userID: string,
    classID: string,
    source: ClassSessionParticipationSource | undefined,
  ) =>
    [
      "class-session-participation",
      tenantID,
      userID,
      classID,
      participationSourceFingerprint(source),
    ] as const,
  roster: (tenantID: string, userID: string, classID: string, search: string) =>
    [
      "class-session-participation",
      tenantID,
      userID,
      classID,
      "active-roster",
      search,
    ] as const,
};

export function participationSourceFingerprint(
  source: ClassSessionParticipationSource | undefined,
) {
  if (!source) {
    return "invalid";
  }
  switch (source.kind) {
    case "session":
      return `session:${source.sessionID}`;
    case "series":
      return `series:${source.seriesID}`;
    case "occurrence":
      return `occurrence:${source.seriesID}:${source.occurrenceKey}`;
  }
}

function isValidParticipationSource(
  source: ClassSessionParticipationSource | undefined,
): source is ClassSessionParticipationSource {
  if (!source) {
    return false;
  }
  switch (source.kind) {
    case "session":
      return Boolean(source.sessionID);
    case "series":
      return Boolean(source.seriesID);
    case "occurrence":
      return Boolean(source.seriesID && source.occurrenceKey);
  }
}

function getParticipationAudience(
  tenantID: string,
  classID: string,
  source: ClassSessionParticipationSource,
  signal?: AbortSignal,
) {
  const options = { baseUrl: getApiBaseUrl(), signal };
  switch (source.kind) {
    case "session":
      return getClassSessionAudience(
        tenantID,
        classID,
        source.sessionID,
        options,
      );
    case "series":
      return getClassSessionSeriesAudience(
        tenantID,
        classID,
        source.seriesID,
        options,
      );
    case "occurrence":
      return getClassSessionSeriesOccurrenceAudience(
        tenantID,
        classID,
        source.seriesID,
        source.occurrenceKey,
        options,
      );
  }
}

function shouldRetryParticipationQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function participationIdempotencyKey(operation: "audience" | "rsvp") {
  return `${operation}:${crypto.randomUUID()}`;
}

export function useParticipationAudience(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  source: ClassSessionParticipationSource | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: classSessionParticipationQueryKeys.audience(
      tenantID ?? "inactive",
      userID ?? "anonymous",
      classID ?? "invalid",
      source,
    ),
    queryFn: ({ signal }) => {
      if (!isValidParticipationSource(source)) {
        throw new Error("A valid participation source is required.");
      }
      return getParticipationAudience(
        tenantID ?? "",
        classID ?? "",
        source,
        signal,
      );
    },
    enabled:
      enabled &&
      Boolean(tenantID && userID && classID) &&
      isValidParticipationSource(source),
    retry: shouldRetryParticipationQuery,
    staleTime: 10_000,
  });
}

function normalizeAudienceRosterSearch(value: string) {
  return value.normalize("NFC").trim().replace(/\s+/gu, " ").toLowerCase();
}

export function useActiveClassRosterForAudience(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  search: string,
  enabled: boolean,
) {
  const normalizedSearch = normalizeAudienceRosterSearch(search);
  return useInfiniteQuery<ClassRosterPage>({
    queryKey: classSessionParticipationQueryKeys.roster(
      tenantID ?? "inactive",
      userID ?? "anonymous",
      classID ?? "invalid",
      normalizedSearch,
    ),
    queryFn: ({ pageParam, signal }) =>
      listClassRoster(
        classID ?? "",
        {
          cursor: pageParam as string | undefined,
          limit: audienceRosterPageSize,
          search: normalizedSearch || undefined,
          status: "active",
        },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(tenantID && userID && classID),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryParticipationQuery,
    staleTime: 10_000,
  });
}

interface ReplaceAudienceVariables {
  input: ReplaceSessionAudienceRequest;
}

async function replaceParticipationAudience(
  tenantID: string,
  classID: string,
  source: ClassSessionParticipationSource,
  input: ReplaceSessionAudienceRequest,
  csrfToken: string,
) {
  const options = { baseUrl: getApiBaseUrl() };
  switch (source.kind) {
    case "session":
      return replaceClassSessionAudience(
        tenantID,
        classID,
        source.sessionID,
        input,
        csrfToken,
        options,
      );
    case "series":
      return replaceClassSessionSeriesAudience(
        tenantID,
        classID,
        source.seriesID,
        input,
        csrfToken,
        options,
      );
    case "occurrence":
      return replaceClassSessionSeriesOccurrenceAudience(
        tenantID,
        classID,
        source.seriesID,
        source.occurrenceKey,
        input,
        csrfToken,
        options,
      );
  }
}

export function useReplaceParticipationAudience(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  source: ClassSessionParticipationSource | undefined,
) {
  const queryClient = useQueryClient();
  const queryKey = classSessionParticipationQueryKeys.audience(
    tenantID ?? "inactive",
    userID ?? "anonymous",
    classID ?? "invalid",
    source,
  );

  return useMutation({
    mutationFn: async ({ input }: ReplaceAudienceVariables) => {
      if (!isValidParticipationSource(source)) {
        throw new Error("A valid participation source is required.");
      }
      const mutationQueryKey = queryKey;
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const response = await replaceParticipationAudience(
        tenantID ?? "",
        classID ?? "",
        source,
        input,
        csrf.csrf_token,
      );
      return { mutationQueryKey, response };
    },
    onSuccess: ({ mutationQueryKey, response }) => {
      queryClient.setQueryData<SessionAudience>(
        mutationQueryKey,
        response.audience,
      );
    },
    retry: false,
  });
}

interface RespondVariables {
  input: RespondToClassSessionRequest;
}

async function respondToParticipationSource(
  tenantID: string,
  classID: string,
  source: ClassSessionParticipationSource,
  input: RespondToClassSessionRequest,
  csrfToken: string,
) {
  const options = { baseUrl: getApiBaseUrl() };
  switch (source.kind) {
    case "session":
      return respondToClassSession(
        tenantID,
        classID,
        source.sessionID,
        input,
        csrfToken,
        options,
      );
    case "series":
      return respondToClassSessionSeries(
        tenantID,
        classID,
        source.seriesID,
        input,
        csrfToken,
        options,
      );
    case "occurrence":
      return respondToClassSessionSeriesOccurrence(
        tenantID,
        classID,
        source.seriesID,
        source.occurrenceKey,
        input,
        csrfToken,
        options,
      );
  }
}

export function useRespondToParticipation(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  source: ClassSessionParticipationSource | undefined,
) {
  const queryClient = useQueryClient();
  const queryKey = classSessionParticipationQueryKeys.audience(
    tenantID ?? "inactive",
    userID ?? "anonymous",
    classID ?? "invalid",
    source,
  );

  return useMutation({
    mutationFn: async ({ input }: RespondVariables) => {
      if (!isValidParticipationSource(source)) {
        throw new Error("A valid participation source is required.");
      }
      const mutationQueryKey = queryKey;
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const response = await respondToParticipationSource(
        tenantID ?? "",
        classID ?? "",
        source,
        input,
        csrf.csrf_token,
      );
      return { mutationQueryKey, response };
    },
    onSuccess: async ({ mutationQueryKey, response }) => {
      queryClient.setQueryData<SessionAudience>(mutationQueryKey, (current) => {
        if (!current) {
          return current;
        }
        return {
          ...current,
          attendees: current.attendees.map((attendee) =>
            attendee.id === response.attendee.id ||
            (attendee.is_self && response.attendee.is_self)
              ? response.attendee
              : attendee,
          ),
        };
      });
      await queryClient.invalidateQueries({
        exact: true,
        queryKey: mutationQueryKey,
      });
    },
    retry: false,
  });
}
