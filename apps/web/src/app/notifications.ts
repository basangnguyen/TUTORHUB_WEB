import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  getNotificationPreference,
  getNotificationUnreadCount,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  rotateCSRFToken,
  updateNotificationPreference,
  type Notification,
  type NotificationPage,
  type NotificationPreference,
  type NotificationUnreadCount,
  type UpdateNotificationPreferenceRequest,
} from "@tutorhub/api-client";

export const notificationPageSize = 25;
export const notificationPollInterval = 30_000;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const notificationQueryKeys = {
  all: ["notifications"] as const,
  tenant: (tenantID: string) => ["notifications", tenantID] as const,
  lists: (tenantID: string) => ["notifications", tenantID, "list"] as const,
  list: (tenantID: string, unreadOnly: boolean) =>
    ["notifications", tenantID, "list", unreadOnly ? "unread" : "all"] as const,
  unreadCount: (tenantID: string) =>
    ["notifications", tenantID, "unread-count"] as const,
  preference: (tenantID: string) =>
    ["notifications", tenantID, "preference"] as const,
};

function shouldRetryNotificationQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useNotifications(
  tenantID: string | undefined,
  unreadOnly: boolean,
  enabled: boolean,
) {
  return useInfiniteQuery({
    queryKey: notificationQueryKeys.list(tenantID ?? "inactive", unreadOnly),
    queryFn: ({ pageParam, signal }) =>
      listNotifications(
        tenantID ?? "",
        {
          cursor: pageParam ?? undefined,
          limit: notificationPageSize,
          unreadOnly,
        },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(tenantID),
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    initialPageParam: undefined as string | undefined,
    refetchInterval: notificationPollInterval,
    refetchIntervalInBackground: false,
    retry: shouldRetryNotificationQuery,
    staleTime: 10_000,
  });
}

export function useNotificationUnreadCount(
  tenantID: string | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: notificationQueryKeys.unreadCount(tenantID ?? "inactive"),
    queryFn: ({ signal }) =>
      getNotificationUnreadCount(tenantID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: enabled && Boolean(tenantID),
    refetchInterval: notificationPollInterval,
    refetchIntervalInBackground: false,
    retry: shouldRetryNotificationQuery,
    staleTime: 10_000,
  });
}

export function useNotificationPreference(
  tenantID: string | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: notificationQueryKeys.preference(tenantID ?? "inactive"),
    queryFn: ({ signal }) =>
      getNotificationPreference(tenantID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: enabled && Boolean(tenantID),
    retry: shouldRetryNotificationQuery,
    staleTime: 30_000,
  });
}

export function invalidateTenantNotifications(
  queryClient: QueryClient,
  tenantID: string | undefined,
) {
  if (!tenantID) {
    return Promise.resolve();
  }
  return queryClient.invalidateQueries({
    queryKey: notificationQueryKeys.tenant(tenantID),
  });
}

export function useMarkNotificationRead(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (notificationID: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return markNotificationRead(
        tenantID ?? "",
        notificationID,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (notification) => {
      if (!tenantID) {
        return;
      }
      replaceNotificationInLists(queryClient, tenantID, notification);
      queryClient.setQueryData<NotificationUnreadCount>(
        notificationQueryKeys.unreadCount(tenantID),
        (current) =>
          current
            ? { ...current, count: Math.max(0, current.count - 1) }
            : current,
      );
    },
    onSettled: () => invalidateTenantNotifications(queryClient, tenantID),
    retry: false,
  });
}

export function useMarkAllNotificationsRead(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async () => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return markAllNotificationsRead(tenantID ?? "", csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: () => {
      if (!tenantID) {
        return;
      }
      const readAt = new Date().toISOString();
      queryClient.setQueriesData<InfiniteData<NotificationPage>>(
        { queryKey: notificationQueryKeys.lists(tenantID) },
        (current) =>
          mapNotificationPages(current, (item) => ({
            ...item,
            read_at: item.read_at ?? readAt,
          })),
      );
      queryClient.setQueryData<NotificationUnreadCount>(
        notificationQueryKeys.unreadCount(tenantID),
        (current) =>
          current ? { ...current, count: 0, is_capped: false } : current,
      );
    },
    onSettled: () => invalidateTenantNotifications(queryClient, tenantID),
    retry: false,
  });
}

interface UpdateNotificationPreferenceVariables {
  input: UpdateNotificationPreferenceRequest;
}

export function useUpdateNotificationPreference(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation<
    NotificationPreference,
    Error,
    UpdateNotificationPreferenceVariables
  >({
    mutationFn: async ({ input }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return updateNotificationPreference(
        tenantID ?? "",
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: (preference) => {
      if (tenantID) {
        queryClient.setQueryData(
          notificationQueryKeys.preference(tenantID),
          preference,
        );
      }
    },
    onSettled: (_preference, error) =>
      error && tenantID
        ? queryClient.invalidateQueries({
            exact: true,
            queryKey: notificationQueryKeys.preference(tenantID),
          })
        : Promise.resolve(),
    retry: false,
  });
}

function replaceNotificationInLists(
  queryClient: QueryClient,
  tenantID: string,
  notification: Notification,
) {
  queryClient.setQueriesData<InfiniteData<NotificationPage>>(
    { queryKey: notificationQueryKeys.lists(tenantID) },
    (current) =>
      mapNotificationPages(current, (item) =>
        item.id === notification.id ? notification : item,
      ),
  );
}

function mapNotificationPages(
  current: InfiniteData<NotificationPage> | undefined,
  mapItem: (item: Notification) => Notification,
) {
  if (!current) {
    return current;
  }
  return {
    ...current,
    pages: current.pages.map((page) => ({
      ...page,
      items: page.items.map(mapItem),
    })),
  };
}
