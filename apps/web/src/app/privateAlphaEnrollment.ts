import {
  getPrivateAlphaEnrollment,
  rotateCSRFToken,
  updatePrivateAlphaEnrollment,
  type PrivateAlphaEnrollmentResponse,
  type UpdatePrivateAlphaEnrollmentRequest,
} from "@tutorhub/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

export const PRIVATE_ALPHA_PROGRAM = "classroom_whiteboards" as const;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export function privateAlphaEnrollmentQueryKey(tenantID: string) {
  return ["tenants", tenantID, "private-alpha-enrollment"] as const;
}

export function usePrivateAlphaEnrollment(tenantID: string, enabled = true) {
  return useQuery({
    queryKey: privateAlphaEnrollmentQueryKey(tenantID),
    queryFn: async () => {
      const response = await getPrivateAlphaEnrollment(tenantID, {
        baseUrl: getApiBaseUrl(),
      });
      if (
        response.tenant_id !== tenantID ||
        response.program !== PRIVATE_ALPHA_PROGRAM
      ) {
        throw new Error("private_alpha_enrollment_scope_mismatch");
      }
      return response;
    },
    enabled: enabled && tenantID.length > 0,
    retry: false,
  });
}

export function useUpdatePrivateAlphaEnrollment(tenantID: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: UpdatePrivateAlphaEnrollmentRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const response = await updatePrivateAlphaEnrollment(
        tenantID,
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
      if (
        response.tenant_id !== tenantID ||
        response.program !== PRIVATE_ALPHA_PROGRAM
      ) {
        throw new Error("private_alpha_enrollment_scope_mismatch");
      }
      return response;
    },
    onSuccess: async (response: PrivateAlphaEnrollmentResponse) => {
      queryClient.setQueryData(
        privateAlphaEnrollmentQueryKey(tenantID),
        response,
      );
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["tenants", tenantID, "capabilities"],
        }),
        queryClient.invalidateQueries({
          queryKey: ["tenants", tenantID, "audit-events"],
        }),
      ]);
    },
  });
}
