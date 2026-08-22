import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  createWhiteboard,
  exchangeWhiteboardGrant,
  resolveWhiteboard,
  rotateCSRFToken,
  transitionWhiteboard,
  type WhiteboardCapability,
  type WhiteboardDocument,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const whiteboardQueryKeys = {
  tool: (tenantID: string, mediaSpaceID: string) =>
    ["whiteboard-tool", tenantID, mediaSpaceID] as const,
};

function shouldRetryWhiteboardQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useWhiteboardTool(
  tenantID: string,
  mediaSpaceID: string,
  enabled: boolean,
) {
  return useQuery({
    queryKey: whiteboardQueryKeys.tool(tenantID, mediaSpaceID),
    queryFn: ({ signal }) =>
      resolveWhiteboard(tenantID, mediaSpaceID, {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled,
    retry: shouldRetryWhiteboardQuery,
    staleTime: 5_000,
  });
}

export function usePrepareWhiteboard(tenantID: string, mediaSpaceID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createWhiteboard(
        tenantID,
        {
          idempotency_key: crypto.randomUUID(),
          media_space_id: mediaSpaceID,
        },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (document) => {
      queryClient.setQueryData(
        whiteboardQueryKeys.tool(tenantID, mediaSpaceID),
        { can_create: false, document },
      );
    },
    retry: false,
  });
}

export function useTransitionWhiteboard(
  tenantID: string,
  mediaSpaceID: string,
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      document,
      operation,
    }: {
      document: WhiteboardDocument;
      operation: "open" | "suspend" | "resume" | "close";
    }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return transitionWhiteboard(
        tenantID,
        document.id,
        operation,
        {
          expected_version: document.version,
          idempotency_key: crypto.randomUUID(),
        },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (document) => {
      queryClient.setQueryData(
        whiteboardQueryKeys.tool(tenantID, mediaSpaceID),
        { can_create: false, document },
      );
    },
    retry: false,
  });
}

export async function requestWhiteboardGrant(
  tenantID: string,
  document: WhiteboardDocument,
  capability: WhiteboardCapability,
  signal?: AbortSignal,
) {
  const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl(), signal });
  return exchangeWhiteboardGrant(
    tenantID,
    document.id,
    {
      capability,
      expected_generation: document.current_generation,
      expected_revoke_generation: document.revoke_generation,
    },
    csrf.csrf_token,
    { baseUrl: getApiBaseUrl(), signal },
  );
}
