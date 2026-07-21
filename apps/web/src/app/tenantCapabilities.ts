import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import {
  APIRequestError,
  getTenantCapabilities,
  rotateCSRFToken,
  updateTenantFeatureControls,
  type TenantCapabilities,
  type UpdateTenantFeatureControlsRequest,
} from "@tutorhub/api-client";
import { invalidateTenantAudit } from "./audit";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const tenantCapabilityQueryKeys = {
  detail: (tenantID: string) => ["tenants", tenantID, "capabilities"] as const,
};

export type TenantOperationKey = keyof TenantCapabilities["operations"];
export type TenantOperationReason =
  | TenantCapabilities["operations"][TenantOperationKey]["reason"]
  | "capabilities_loading"
  | "capabilities_unavailable";

export interface TenantOperationAvailability {
  available: boolean;
  reason: TenantOperationReason;
}

class TenantCapabilityScopeError extends Error {}

function shouldRetryCapabilitiesQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(error instanceof TenantCapabilityScopeError) &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function requireMatchingTenant(
  tenantID: string,
  capabilities: TenantCapabilities,
) {
  if (capabilities.tenant_id !== tenantID) {
    throw new TenantCapabilityScopeError(
      "Tenant capability response scope did not match the request.",
    );
  }
  return capabilities;
}

export function useTenantCapabilities(
  tenantID: string | undefined,
  enabled = true,
) {
  return useQuery<TenantCapabilities>({
    queryKey: tenantCapabilityQueryKeys.detail(tenantID ?? "inactive"),
    queryFn: async ({ signal }) =>
      requireMatchingTenant(
        tenantID ?? "",
        await getTenantCapabilities(tenantID ?? "", {
          baseUrl: getApiBaseUrl(),
          signal,
        }),
      ),
    enabled: enabled && Boolean(tenantID),
    retry: shouldRetryCapabilitiesQuery,
    staleTime: 15_000,
  });
}

export function tenantOperationAvailability(
  query: Pick<
    UseQueryResult<TenantCapabilities>,
    "data" | "isError" | "isPending"
  >,
  operation: TenantOperationKey,
): TenantOperationAvailability {
  if (query.isPending) {
    return { available: false, reason: "capabilities_loading" };
  }
  if (query.isError || !query.data) {
    return { available: false, reason: "capabilities_unavailable" };
  }
  return query.data.operations[operation];
}

export function invalidateTenantCapabilities(
  queryClient: QueryClient,
  tenantID: string | undefined,
) {
  if (!tenantID) {
    return Promise.resolve();
  }
  return queryClient.invalidateQueries({
    exact: true,
    queryKey: tenantCapabilityQueryKeys.detail(tenantID),
  });
}

interface UpdateTenantFeatureControlsVariables {
  input: UpdateTenantFeatureControlsRequest;
  tenantID: string;
}

export function useUpdateTenantFeatureControls() {
  const queryClient = useQueryClient();

  return useMutation<
    TenantCapabilities,
    Error,
    UpdateTenantFeatureControlsVariables
  >({
    mutationFn: async ({ input, tenantID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return requireMatchingTenant(
        tenantID,
        await updateTenantFeatureControls(tenantID, input, csrf.csrf_token, {
          baseUrl: getApiBaseUrl(),
        }),
      );
    },
    onSuccess: (capabilities, { tenantID }) => {
      queryClient.setQueryData(
        tenantCapabilityQueryKeys.detail(tenantID),
        capabilities,
      );
    },
    onSettled: (_capabilities, error, { tenantID }) =>
      Promise.all([
        error
          ? invalidateTenantCapabilities(queryClient, tenantID)
          : Promise.resolve(),
        invalidateTenantAudit(queryClient, tenantID),
      ]),
    retry: false,
  });
}
