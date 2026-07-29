import { useMutation, useQuery } from "@tanstack/react-query";
import {
  APIRequestError,
  resolveExternalCalendarRSVP,
  respondExternalCalendarRSVP,
  type ExternalCalendarRSVPMutationResponse,
  type ExternalCalendarRSVPProjection,
  type SessionSelfRSVPState,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function shouldRetryPublicRSVP(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useExternalCalendarRSVP(
  resolveToken: string | null,
  enabled = true,
) {
  return useQuery<ExternalCalendarRSVPProjection>({
    // A capability is deliberately excluded from query keys and persisted
    // caches. Only one public RSVP page can be active in this router.
    queryKey: ["public-calendar-rsvp"],
    queryFn: ({ signal }) =>
      resolveExternalCalendarRSVP(
        { token: resolveToken ?? "" },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(resolveToken),
    gcTime: 0,
    retry: shouldRetryPublicRSVP,
    staleTime: 0,
  });
}

interface ExternalRSVPVariables {
  expectedAttendeeVersion: number;
  idempotencyKey: string;
  note: string;
  state: SessionSelfRSVPState;
}

export function useRespondExternalCalendarRSVP(respondToken: string | null) {
  return useMutation<
    ExternalCalendarRSVPMutationResponse,
    Error,
    ExternalRSVPVariables
  >({
    gcTime: 0,
    mutationFn: ({ expectedAttendeeVersion, idempotencyKey, note, state }) => {
      if (!respondToken) {
        throw new Error(
          "The external calendar RSVP capability is unavailable.",
        );
      }
      return respondExternalCalendarRSVP(
        {
          expected_attendee_version: expectedAttendeeVersion,
          idempotency_key: idempotencyKey,
          note,
          state,
          token: respondToken,
        },
        { baseUrl: getApiBaseUrl() },
      );
    },
    retry: false,
  });
}
