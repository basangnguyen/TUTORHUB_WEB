import { useMutation } from "@tanstack/react-query";
import {
  createTenant,
  rotateCSRFToken,
  switchActiveTenant,
  type CreateTenantRequest,
  type CurrentUser,
} from "@tutorhub/api-client";
import { useSession } from "./session";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export function useWorkspaceActions() {
  const session = useSession();

  const applyPrincipal = async (currentUser: CurrentUser) => {
    session.replaceCurrentUser(currentUser);
    await session.refresh();
  };

  const createWorkspace = useMutation({
    mutationFn: async (input: CreateTenantRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createTenant(input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: applyPrincipal,
  });

  const switchWorkspace = useMutation({
    mutationFn: async (tenantID: string) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return switchActiveTenant(tenantID, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: applyPrincipal,
  });

  return { createWorkspace, switchWorkspace };
}
