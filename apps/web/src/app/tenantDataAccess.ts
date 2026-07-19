import { APIRequestError } from "@tutorhub/api-client";

const tenantDataConcealmentStatuses = new Set([401, 403, 404]);

export function shouldConcealTenantScopedData(error: unknown) {
  return (
    error instanceof APIRequestError &&
    tenantDataConcealmentStatuses.has(error.status)
  );
}
