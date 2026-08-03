import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  createDirectConversation,
  ensureClassConversation,
  getConversation,
  listConversations,
  rotateCSRFToken,
  type Conversation,
  type CreateDirectConversationRequest,
  type TenantCapabilities,
} from "@tutorhub/api-client";
import { invalidateTenantAudit } from "./audit";
import {
  invalidateTenantCapabilities,
  tenantOperationAvailability,
  type TenantOperationAvailability,
} from "./tenantCapabilities";

export const conversationPageSize = 25;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const conversationQueryKeys = {
  all: ["conversations"] as const,
  tenant: (tenantID: string) => ["conversations", tenantID] as const,
  lists: (tenantID: string) => ["conversations", tenantID, "list"] as const,
  list: (tenantID: string) =>
    ["conversations", tenantID, "list", "all"] as const,
  detail: (tenantID: string, conversationID: string) =>
    ["conversations", tenantID, "detail", conversationID] as const,
};

export function conversationCreationAvailability(query: {
  data: TenantCapabilities | undefined;
  isError: boolean;
  isPending: boolean;
}): TenantOperationAvailability {
  const operation = tenantOperationAvailability(query, "create_conversation");
  if (!operation.available) {
    return operation;
  }
  if (query.data?.features.conversations?.enabled !== true) {
    return { available: false, reason: "feature_disabled" };
  }
  return operation;
}

function shouldRetryConversationQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useConversations(tenantID: string | undefined, enabled = true) {
  return useInfiniteQuery({
    queryKey: conversationQueryKeys.list(tenantID ?? "inactive"),
    queryFn: ({ pageParam, signal }) =>
      listConversations(
        tenantID ?? "",
        {
          cursor: pageParam ?? undefined,
          limit: conversationPageSize,
        },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(tenantID),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryConversationQuery,
    staleTime: 15_000,
  });
}

export function useConversation(
  tenantID: string | undefined,
  conversationID: string | undefined,
) {
  return useQuery({
    queryKey: conversationQueryKeys.detail(
      tenantID ?? "inactive",
      conversationID ?? "invalid",
    ),
    queryFn: ({ signal }) =>
      getConversation(tenantID ?? "", conversationID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: Boolean(tenantID && conversationID),
    retry: shouldRetryConversationQuery,
    staleTime: 15_000,
  });
}

async function synchronizeConversation(
  queryClient: QueryClient,
  tenantID: string,
  conversation: Conversation,
) {
  await queryClient.cancelQueries({
    exact: true,
    queryKey: conversationQueryKeys.detail(tenantID, conversation.id),
  });
  queryClient.setQueryData(
    conversationQueryKeys.detail(tenantID, conversation.id),
    conversation,
  );
  await queryClient.invalidateQueries({
    queryKey: conversationQueryKeys.lists(tenantID),
  });
}

export function invalidateTenantConversations(
  queryClient: QueryClient,
  tenantID: string | undefined,
) {
  if (!tenantID) {
    return Promise.resolve();
  }
  return queryClient.invalidateQueries({
    queryKey: conversationQueryKeys.tenant(tenantID),
  });
}

export function useCreateDirectConversation(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: CreateDirectConversationRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createDirectConversation(tenantID ?? "", input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (conversation) =>
      tenantID
        ? synchronizeConversation(queryClient, tenantID, conversation)
        : Promise.resolve(),
    onSettled: () =>
      Promise.all([
        invalidateTenantAudit(queryClient, tenantID),
        invalidateTenantCapabilities(queryClient, tenantID),
      ]),
    retry: false,
  });
}

export function useEnsureClassConversation(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (classID: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return ensureClassConversation(tenantID ?? "", classID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (conversation) =>
      tenantID
        ? synchronizeConversation(queryClient, tenantID, conversation)
        : Promise.resolve(),
    onSettled: () =>
      Promise.all([
        invalidateTenantAudit(queryClient, tenantID),
        invalidateTenantCapabilities(queryClient, tenantID),
      ]),
    retry: false,
  });
}
