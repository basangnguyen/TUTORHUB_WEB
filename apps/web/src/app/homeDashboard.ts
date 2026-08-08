import { useQuery } from "@tanstack/react-query";
import {
  APIRequestError,
  getNotificationUnreadCount,
  listCalendarItems,
  listConversations,
  listHomeRecentFiles,
  searchAuthorizedResources,
  type AuthorizedSearchPage,
  type CalendarItem,
  type HomeRecentFile,
} from "@tutorhub/api-client";

const upcomingLimit = 5;
const recentFileLimit = 5;
const conversationLimit = 100;
const searchLimit = 12;

export function isHomeSearchReady(query: string) {
  const normalizedQuery = query.trim();
  return (
    Array.from(normalizedQuery).length >= 2 && normalizedQuery.length <= 100
  );
}

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function shouldRetry(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export const homeQueryKeys = {
  all: ["home"] as const,
  principal: (tenantID: string, userID: string) =>
    ["home", tenantID, userID] as const,
  upcoming: (tenantID: string, userID: string, timezone: string) =>
    ["home", tenantID, userID, "upcoming", timezone] as const,
  notificationUnread: (tenantID: string, userID: string) =>
    ["home", tenantID, userID, "notification-unread"] as const,
  messageUnread: (tenantID: string, userID: string) =>
    ["home", tenantID, userID, "message-unread"] as const,
  recentFiles: (tenantID: string, userID: string) =>
    ["home", tenantID, userID, "recent-files"] as const,
  search: (tenantID: string, userID: string, query: string) =>
    ["home", tenantID, userID, "search", query] as const,
};

interface HomePrincipalInput {
  tenantID: string | undefined;
  userID: string | undefined;
}

export function useHomeUpcomingSessions({
  tenantID,
  timezone,
  userID,
}: HomePrincipalInput & { timezone: string | undefined }) {
  return useQuery({
    queryKey: homeQueryKeys.upcoming(
      tenantID ?? "inactive",
      userID ?? "anonymous",
      timezone ?? "UTC",
    ),
    queryFn: async ({ signal }): Promise<readonly CalendarItem[]> => {
      const from = new Date();
      const to = new Date(from.getTime() + 90 * 24 * 60 * 60 * 1000);
      const page = await listCalendarItems(
        tenantID ?? "",
        {
          from: from.toISOString(),
          limit: upcomingLimit,
          statuses: ["live", "scheduled"],
          to: to.toISOString(),
          types: ["class_session"],
          viewer_timezone: timezone ?? "UTC",
        },
        { baseUrl: getApiBaseUrl(), signal },
      );
      return page.items;
    },
    enabled: Boolean(tenantID && userID && timezone),
    retry: shouldRetry,
    staleTime: 30_000,
  });
}

export function useHomeNotificationUnread({
  tenantID,
  userID,
}: HomePrincipalInput) {
  return useQuery({
    queryKey: homeQueryKeys.notificationUnread(
      tenantID ?? "inactive",
      userID ?? "anonymous",
    ),
    queryFn: ({ signal }) =>
      getNotificationUnreadCount(tenantID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: Boolean(tenantID && userID),
    retry: shouldRetry,
    staleTime: 20_000,
  });
}

export interface HomeMessageUnread {
  capped: boolean;
  count: number;
}

export function useHomeMessageUnread({ tenantID, userID }: HomePrincipalInput) {
  return useQuery({
    queryKey: homeQueryKeys.messageUnread(
      tenantID ?? "inactive",
      userID ?? "anonymous",
    ),
    queryFn: async ({ signal }): Promise<HomeMessageUnread> => {
      const page = await listConversations(
        tenantID ?? "",
        { limit: conversationLimit },
        { baseUrl: getApiBaseUrl(), signal },
      );
      const count = page.items.reduce(
        (total, conversation) => total + conversation.unread_count,
        0,
      );
      return {
        capped:
          Boolean(page.next_cursor) ||
          page.items.some((conversation) => conversation.unread_count_capped) ||
          count > 999,
        count: Math.min(count, 999),
      };
    },
    enabled: Boolean(tenantID && userID),
    retry: shouldRetry,
    staleTime: 15_000,
  });
}

export function useHomeRecentFiles({ tenantID, userID }: HomePrincipalInput) {
  return useQuery({
    queryKey: homeQueryKeys.recentFiles(
      tenantID ?? "inactive",
      userID ?? "anonymous",
    ),
    queryFn: async ({ signal }): Promise<readonly HomeRecentFile[]> => {
      const page = await listHomeRecentFiles(
        tenantID ?? "",
        { limit: recentFileLimit },
        { baseUrl: getApiBaseUrl(), signal },
      );
      return page.items;
    },
    enabled: Boolean(tenantID && userID),
    retry: shouldRetry,
    staleTime: 30_000,
  });
}

export function useHomeSearch({
  query,
  tenantID,
  userID,
}: HomePrincipalInput & { query: string }) {
  const normalizedQuery = query.trim();
  return useQuery({
    queryKey: homeQueryKeys.search(
      tenantID ?? "inactive",
      userID ?? "anonymous",
      normalizedQuery.toLocaleLowerCase(),
    ),
    queryFn: ({ signal }): Promise<AuthorizedSearchPage> =>
      searchAuthorizedResources(
        tenantID ?? "",
        { limit: searchLimit, q: normalizedQuery },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: Boolean(tenantID && userID && isHomeSearchReady(normalizedQuery)),
    retry: shouldRetry,
    staleTime: 30_000,
  });
}
