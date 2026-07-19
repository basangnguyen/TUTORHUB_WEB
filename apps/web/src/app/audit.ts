import { useInfiniteQuery, type QueryClient } from "@tanstack/react-query";
import {
  APIRequestError,
  listAuditEvents,
  type AuditAction,
  type AuditOutcome,
} from "@tutorhub/api-client";

const auditPageSize = 25;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export interface AuditFilters {
  action?: AuditAction;
  occurredFrom?: string;
  occurredTo?: string;
  outcome?: AuditOutcome;
  resourceID?: string;
  resourceType?: string;
}

export function normalizeAuditFilters(filters: AuditFilters): AuditFilters {
  const resourceType = filters.resourceType?.trim().toLowerCase();
  const resourceID = filters.resourceID?.trim().toLowerCase();

  return {
    action: filters.action,
    occurredFrom: filters.occurredFrom,
    occurredTo: filters.occurredTo,
    outcome: filters.outcome,
    resourceID: resourceID || undefined,
    resourceType: resourceType || undefined,
  };
}

export const auditQueryKeys = {
  all: ["audit"] as const,
  tenant: (tenantID: string) => ["audit", tenantID] as const,
  list: (tenantID: string, filters: AuditFilters) => {
    const normalized = normalizeAuditFilters(filters);
    return [
      "audit",
      tenantID,
      "list",
      normalized.occurredFrom ?? "",
      normalized.occurredTo ?? "",
      normalized.action ?? "all",
      normalized.resourceType ?? "all",
      normalized.resourceID ?? "",
      normalized.outcome ?? "all",
    ] as const;
  },
};

export function invalidateTenantAudit(
  queryClient: QueryClient,
  tenantID: string | undefined,
) {
  if (!tenantID) {
    return Promise.resolve();
  }
  return queryClient.invalidateQueries({
    queryKey: auditQueryKeys.tenant(tenantID),
  });
}

function shouldRetryAuditQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useAuditEvents(
  tenantID: string | undefined,
  filters: AuditFilters,
  enabled: boolean,
) {
  const normalized = normalizeAuditFilters(filters);

  return useInfiniteQuery({
    queryKey: auditQueryKeys.list(tenantID ?? "inactive", normalized),
    queryFn: ({ pageParam, signal }) =>
      listAuditEvents(
        tenantID ?? "",
        {
          ...normalized,
          cursor: pageParam ?? undefined,
          limit: auditPageSize,
        },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: enabled && Boolean(tenantID),
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryAuditQuery,
    staleTime: 10_000,
  });
}
