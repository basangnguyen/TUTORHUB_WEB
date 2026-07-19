import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  archiveTenant as requestTenantArchive,
  createTenant,
  getTenant,
  listTenants,
  rotateCSRFToken,
  switchActiveTenant,
  updateTenant as requestTenantUpdate,
  type ArchiveTenantRequest,
  type CreateTenantRequest,
  type CurrentUser,
  type Tenant,
  type TenantListResponse,
  type UpdateTenantRequest,
} from "@tutorhub/api-client";
import { useEffect, useRef } from "react";
import { useSession } from "./session";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

const tenantScopedQueryRoots = new Set([
  "audit",
  "classes",
  "media",
  "tenants",
]);

export const tenantQueryKeys = {
  all: ["tenants"] as const,
  list: ["tenants", "list"] as const,
  detail: (tenantID: string) => ["tenants", "detail", tenantID] as const,
};

function isTenantScopedQuery(query: { queryKey: readonly unknown[] }) {
  return tenantScopedQueryRoots.has(String(query.queryKey[0] ?? ""));
}

async function cancelWorkspaceReaders(queryClient: QueryClient) {
  await Promise.all([
    queryClient.cancelQueries({ queryKey: ["auth", "me"], exact: true }),
    queryClient.cancelQueries({ predicate: isTenantScopedQuery }),
  ]);
}

interface WorkspaceActionOptions {
  onSwitchSuccess?: (currentUser: CurrentUser) => void | Promise<void>;
}

function useTenantBoundaryPrincipal() {
  const session = useSession();
  const queryClient = useQueryClient();

  return async (
    currentUser: CurrentUser,
    isCurrent: () => boolean = () => true,
  ) => {
    if (!isCurrent()) {
      return false;
    }
    await cancelWorkspaceReaders(queryClient);
    if (!isCurrent()) {
      return false;
    }
    queryClient.removeQueries({ predicate: isTenantScopedQuery });
    session.replaceCurrentUser(currentUser);
    return true;
  };
}

function shouldRetryTenantQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError && [401, 403, 404].includes(error.status)
    )
  );
}

function tenantMembership(tenant: Tenant) {
  return {
    id: tenant.id,
    is_active: tenant.is_active,
    name: tenant.name,
    role: tenant.role,
    slug: tenant.slug,
    status: tenant.status,
    version: tenant.version,
  };
}

function principalWithTenant(
  currentUser: CurrentUser,
  tenant: Tenant,
): CurrentUser {
  const membership = tenantMembership(tenant);
  return {
    ...currentUser,
    active_tenant:
      currentUser.active_tenant?.id === tenant.id
        ? membership
        : currentUser.active_tenant,
    memberships: currentUser.memberships.map((item) =>
      item.id === tenant.id ? membership : item,
    ),
  };
}

export function useWorkspaceActions(options: WorkspaceActionOptions = {}) {
  const applyPrincipal = useTenantBoundaryPrincipal();
  const latestSwitchGeneration = useRef(0);
  const highestAppliedSwitchGeneration = useRef(0);
  const switchOutcomes = useRef(
    new Map<
      number,
      | { status: "pending" }
      | { status: "failed" }
      | { currentUser: CurrentUser; status: "succeeded" }
    >(),
  );
  const switchReconciliation = useRef<Promise<void>>(Promise.resolve());

  const isWinningSwitch = (generation: number) => {
    if (
      generation <= highestAppliedSwitchGeneration.current ||
      switchOutcomes.current.get(generation)?.status !== "succeeded"
    ) {
      return false;
    }
    for (
      let newerGeneration = latestSwitchGeneration.current;
      newerGeneration > generation;
      newerGeneration -= 1
    ) {
      if (switchOutcomes.current.get(newerGeneration)?.status !== "failed") {
        return false;
      }
    }
    return true;
  };

  const reconcileSwitchOutcomes = () => {
    const reconcile = async () => {
      let candidate:
        { currentUser: CurrentUser; generation: number } | undefined;
      for (
        let generation = latestSwitchGeneration.current;
        generation > highestAppliedSwitchGeneration.current;
        generation -= 1
      ) {
        const outcome = switchOutcomes.current.get(generation);
        if (!outcome || outcome.status === "pending") {
          return;
        }
        if (outcome.status === "succeeded") {
          candidate = { currentUser: outcome.currentUser, generation };
          break;
        }
      }
      if (!candidate) {
        return;
      }

      const applied = await applyPrincipal(candidate.currentUser, () =>
        isWinningSwitch(candidate.generation),
      );
      if (
        applied &&
        candidate.generation > highestAppliedSwitchGeneration.current
      ) {
        highestAppliedSwitchGeneration.current = candidate.generation;
        for (const generation of switchOutcomes.current.keys()) {
          if (generation <= latestSwitchGeneration.current) {
            switchOutcomes.current.delete(generation);
          }
        }
        await options.onSwitchSuccess?.(candidate.currentUser);
      }
    };

    const queued = switchReconciliation.current.then(reconcile, reconcile);
    switchReconciliation.current = queued.catch(() => undefined);
    return queued;
  };

  const createWorkspace = useMutation({
    mutationFn: async (input: CreateTenantRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createTenant(input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (currentUser) => applyPrincipal(currentUser),
  });

  const switchWorkspace = useMutation({
    onMutate: () => {
      const generation = ++latestSwitchGeneration.current;
      switchOutcomes.current.set(generation, { status: "pending" });
      return { generation };
    },
    mutationFn: async (tenantID: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return switchActiveTenant(tenantID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async (currentUser, _tenantID, context) => {
      switchOutcomes.current.set(context.generation, {
        currentUser,
        status: "succeeded",
      });
      await reconcileSwitchOutcomes();
    },
    onError: async (_error, _tenantID, context) => {
      if (!context) {
        return;
      }
      switchOutcomes.current.set(context.generation, { status: "failed" });
      await reconcileSwitchOutcomes();
    },
  });

  return { createWorkspace, switchWorkspace };
}

export function useTenantList() {
  return useQuery({
    queryKey: tenantQueryKeys.list,
    queryFn: ({ signal }) => listTenants({ baseUrl: getApiBaseUrl(), signal }),
    retry: shouldRetryTenantQuery,
    staleTime: 20_000,
  });
}

export function useTenantDetail(tenantID: string | undefined) {
  return useQuery({
    queryKey: tenantQueryKeys.detail(tenantID ?? "inactive"),
    queryFn: ({ signal }) =>
      getTenant(tenantID ?? "", { baseUrl: getApiBaseUrl(), signal }),
    enabled: Boolean(tenantID),
    retry: shouldRetryTenantQuery,
    staleTime: 20_000,
  });
}

interface UpdateTenantVariables {
  input: UpdateTenantRequest;
  tenantID: string;
}

export function useUpdateTenant() {
  const session = useSession();
  const queryClient = useQueryClient();
  const currentUser = useRef(session.currentUser);
  useEffect(() => {
    currentUser.current = session.currentUser;
  }, [session.currentUser]);

  return useMutation({
    mutationFn: async ({ input, tenantID }: UpdateTenantVariables) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return requestTenantUpdate(tenantID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async (tenant) => {
      const shouldReloadTenantList =
        queryClient.getQueryData(tenantQueryKeys.list) === undefined;
      await cancelWorkspaceReaders(queryClient);
      if (currentUser.current?.active_tenant?.id !== tenant.id) {
        return;
      }
      queryClient.setQueryData(tenantQueryKeys.detail(tenant.id), tenant);
      queryClient.setQueryData<TenantListResponse>(
        tenantQueryKeys.list,
        (current) =>
          current
            ? {
                items: current.items.map((item) =>
                  item.id === tenant.id ? tenant : item,
                ),
              }
            : current,
      );
      if (currentUser.current) {
        session.replaceCurrentUser(
          principalWithTenant(currentUser.current, tenant),
        );
      }
      if (shouldReloadTenantList) {
        void queryClient.invalidateQueries({
          queryKey: tenantQueryKeys.list,
          exact: true,
        });
      }
    },
  });
}

interface ArchiveTenantVariables {
  input: ArchiveTenantRequest;
  tenantID: string;
}

export function useArchiveTenant() {
  const applyPrincipal = useTenantBoundaryPrincipal();

  return useMutation({
    mutationFn: async ({ input, tenantID }: ArchiveTenantVariables) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return requestTenantArchive(tenantID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: (currentUser) => applyPrincipal(currentUser),
  });
}
