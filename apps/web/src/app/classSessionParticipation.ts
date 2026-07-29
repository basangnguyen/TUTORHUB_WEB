import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  getClassSessionAudience,
  replaceClassSessionAudience,
  respondToClassSession,
  rotateCSRFToken,
  type ReplaceSessionAudienceRequest,
  type RespondToClassSessionRequest,
  type SessionAudience,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const classSessionParticipationQueryKeys = {
  audience: (
    tenantID: string,
    userID: string,
    classID: string,
    sessionID: string,
  ) =>
    [
      "class-session-participation",
      tenantID,
      userID,
      classID,
      sessionID,
    ] as const,
};

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

export function useClassSessionAudience(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  sessionID: string | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: classSessionParticipationQueryKeys.audience(
      tenantID ?? "inactive",
      userID ?? "anonymous",
      classID ?? "invalid",
      sessionID ?? "invalid",
    ),
    queryFn: ({ signal }) =>
      getClassSessionAudience(tenantID ?? "", classID ?? "", sessionID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: enabled && Boolean(tenantID && userID && classID && sessionID),
    retry: shouldRetryParticipationQuery,
    staleTime: 10_000,
  });
}

interface ReplaceAudienceVariables {
  input: ReplaceSessionAudienceRequest;
}

export function useReplaceClassSessionAudience(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  sessionID: string | undefined,
) {
  const queryClient = useQueryClient();
  const queryKey = classSessionParticipationQueryKeys.audience(
    tenantID ?? "inactive",
    userID ?? "anonymous",
    classID ?? "invalid",
    sessionID ?? "invalid",
  );

  return useMutation({
    mutationFn: async ({ input }: ReplaceAudienceVariables) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return replaceClassSessionAudience(
        tenantID ?? "",
        classID ?? "",
        sessionID ?? "",
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (response) => {
      queryClient.setQueryData<SessionAudience>(queryKey, response.audience);
    },
    retry: false,
  });
}

interface RespondVariables {
  input: RespondToClassSessionRequest;
}

export function useRespondToClassSession(
  tenantID: string | undefined,
  userID: string | undefined,
  classID: string | undefined,
  sessionID: string | undefined,
) {
  const queryClient = useQueryClient();
  const queryKey = classSessionParticipationQueryKeys.audience(
    tenantID ?? "inactive",
    userID ?? "anonymous",
    classID ?? "invalid",
    sessionID ?? "invalid",
  );

  return useMutation({
    mutationFn: async ({ input }: RespondVariables) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return respondToClassSession(
        tenantID ?? "",
        classID ?? "",
        sessionID ?? "",
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (response) => {
      queryClient.setQueryData<SessionAudience>(queryKey, (current) => {
        if (!current) {
          return current;
        }
        return {
          ...current,
          attendees: current.attendees.map((attendee) =>
            attendee.id === response.attendee.id ? response.attendee : attendee,
          ),
        };
      });
    },
    retry: false,
  });
}
