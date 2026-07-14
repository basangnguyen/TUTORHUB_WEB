import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createClass,
  getClass,
  listClasses,
  rotateCSRFToken,
  type CreateClassRequest,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const classQueryKeys = {
  list: (tenantID: string) => ["classes", tenantID, "list"] as const,
  detail: (tenantID: string, classID: string) =>
    ["classes", tenantID, "detail", classID] as const,
};

export function useClassList(tenantID: string | undefined) {
  return useQuery({
    queryKey: classQueryKeys.list(tenantID ?? "inactive"),
    queryFn: ({ signal }) =>
      listClasses(50, { baseUrl: getApiBaseUrl(), signal }),
    enabled: Boolean(tenantID),
    staleTime: 20_000,
  });
}

export function useClassDetail(
  tenantID: string | undefined,
  classID: string | undefined,
) {
  return useQuery({
    queryKey: classQueryKeys.detail(
      tenantID ?? "inactive",
      classID ?? "invalid",
    ),
    queryFn: ({ signal }) =>
      getClass(classID ?? "", { baseUrl: getApiBaseUrl(), signal }),
    enabled: Boolean(tenantID && classID),
    staleTime: 20_000,
  });
}

export function useCreateClass(tenantID: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (input: CreateClassRequest) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return createClass(input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    onSuccess: async (created) => {
      if (!tenantID) {
        return;
      }
      queryClient.setQueryData(
        classQueryKeys.detail(tenantID, created.id),
        created,
      );
      await queryClient.invalidateQueries({
        queryKey: classQueryKeys.list(tenantID),
      });
    },
  });
}
