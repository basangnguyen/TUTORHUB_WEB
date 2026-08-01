import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  resolvePublicAvailabilityPoll,
  respondPublicAvailabilityPoll,
  type AvailabilityPollAnswerInput,
  type PublicAvailabilityPollExchange,
  type PublicAvailabilityPollMutationResponse,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function shouldRetryPublicPoll(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export const publicAvailabilityPollQueryKey = (publicID: string) =>
  ["public-availability-poll", publicID] as const;

export function usePublicAvailabilityPoll(
  publicID: string,
  capabilityToken: string | null,
) {
  return useQuery<PublicAvailabilityPollExchange>({
    // The bearer capability is deliberately excluded from all query metadata.
    queryKey: publicAvailabilityPollQueryKey(publicID),
    queryFn: ({ signal }) =>
      resolvePublicAvailabilityPoll(
        { public_id: publicID, token: capabilityToken ?? "" },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: Boolean(publicID && capabilityToken),
    gcTime: 0,
    refetchOnWindowFocus: false,
    retry: shouldRetryPublicPoll,
    staleTime: 0,
  });
}

interface PublicAvailabilityPollResponseVariables {
  answers: readonly AvailabilityPollAnswerInput[];
  expectedResponseVersion: number;
  idempotencyKey: string;
}

export function useRespondPublicAvailabilityPoll(
  publicID: string,
  responseToken: string | null,
) {
  const queryClient = useQueryClient();

  return useMutation<
    PublicAvailabilityPollMutationResponse,
    Error,
    PublicAvailabilityPollResponseVariables
  >({
    gcTime: 0,
    mutationFn: ({ answers, expectedResponseVersion, idempotencyKey }) => {
      if (!responseToken) {
        throw new Error("The public poll response capability is unavailable.");
      }

      return respondPublicAvailabilityPoll(
        {
          answers,
          expected_response_version: expectedResponseVersion,
          idempotency_key: idempotencyKey,
          public_id: publicID,
          response_token: responseToken,
        },
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (result) => {
      queryClient.setQueryData<PublicAvailabilityPollExchange>(
        publicAvailabilityPollQueryKey(publicID),
        (exchange) =>
          exchange ? { ...exchange, poll: result.poll } : exchange,
      );
    },
    retry: false,
  });
}
