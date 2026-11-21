import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  createSchoolSSOConnection,
  createSchoolSSODomain,
  disableSchoolSSODomain,
  listSchoolSSOConnections,
  listSchoolSSODomains,
  rotateCSRFToken,
  rotateSchoolSSODomain,
  updateSchoolSSOConnection,
  verifySchoolSSODomain,
  type CreateSSOConnectionRequest,
  type SSOChallenge,
  type SSOConnection,
  type SSOConnectionList,
  type SSODomain,
  type SSODomainList,
  type UpdateSSOConnectionRequest,
} from "@tutorhub/api-client";
import type { TranslationKey } from "./i18n";
import { stepUpRequirementFor } from "./security";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

/**
 * AUTH-12B school domain SSO administration. Query keys and cached data hold
 * only connection/domain state: the one-time TXT value of an `SSOChallenge`
 * is handed to the caller of the mutation and never written to any cache.
 */
export const schoolSSOQueryKeys = {
  all: (tenantID: string) => ["school-sso", tenantID] as const,
  connections: (tenantID: string) =>
    ["school-sso", tenantID, "connections"] as const,
  domains: (tenantID: string) => ["school-sso", tenantID, "domains"] as const,
};

/** Deterministic refusals (4xx, 503 with Retry-After) are not retried. */
function shouldRetrySchoolSSOQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      (error.status < 500 || error.status === 503)
    )
  );
}

export function useSchoolSSOConnections(tenantID: string, enabled: boolean) {
  return useQuery({
    queryKey: schoolSSOQueryKeys.connections(tenantID),
    queryFn: ({ signal }) =>
      listSchoolSSOConnections(tenantID, { baseUrl: getApiBaseUrl(), signal }),
    enabled: enabled && tenantID !== "",
    retry: shouldRetrySchoolSSOQuery,
    staleTime: 15_000,
  });
}

export function useSchoolSSODomains(tenantID: string, enabled: boolean) {
  return useQuery({
    queryKey: schoolSSOQueryKeys.domains(tenantID),
    queryFn: ({ signal }) =>
      listSchoolSSODomains(tenantID, { baseUrl: getApiBaseUrl(), signal }),
    enabled: enabled && tenantID !== "",
    retry: shouldRetrySchoolSSOQuery,
    staleTime: 15_000,
  });
}

async function csrfToken() {
  return (await rotateCSRFToken({ baseUrl: getApiBaseUrl() })).csrf_token;
}

function upsertConnection(
  queryClient: QueryClient,
  tenantID: string,
  connection: SSOConnection,
) {
  queryClient.setQueryData<SSOConnectionList>(
    schoolSSOQueryKeys.connections(tenantID),
    (current) => {
      if (!current) return current;
      const exists = current.items.some((item) => item.id === connection.id);
      return {
        items: exists
          ? current.items.map((item) =>
              item.id === connection.id ? connection : item,
            )
          : [...current.items, connection],
      };
    },
  );
}

/** Only the public `SSODomain` part of a challenge ever reaches the cache. */
function upsertDomain(
  queryClient: QueryClient,
  tenantID: string,
  domain: SSODomain,
) {
  queryClient.setQueryData<SSODomainList>(
    schoolSSOQueryKeys.domains(tenantID),
    (current) => {
      if (!current) return current;
      const exists = current.items.some((item) => item.id === domain.id);
      return {
        items: exists
          ? current.items.map((item) => (item.id === domain.id ? domain : item))
          : [...current.items, domain],
      };
    },
  );
}

/** Every write changes the audit log, and a policy change affects routing. */
function invalidateAfterWrite(queryClient: QueryClient, tenantID: string) {
  return Promise.all([
    queryClient.invalidateQueries({
      queryKey: schoolSSOQueryKeys.all(tenantID),
    }),
    queryClient.invalidateQueries({ queryKey: ["audit", tenantID] }),
  ]);
}

export function useCreateSchoolSSOConnection(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (input: CreateSSOConnectionRequest) =>
      createSchoolSSOConnection(tenantID, input, await csrfToken(), {
        baseUrl: getApiBaseUrl(),
      }),
    onSuccess: (connection) => {
      upsertConnection(queryClient, tenantID, connection);
      return invalidateAfterWrite(queryClient, tenantID);
    },
  });
}

export interface UpdateSchoolSSOConnectionInput {
  connectionID: string;
  patch: UpdateSSOConnectionRequest;
}

export function useUpdateSchoolSSOConnection(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async ({
      connectionID,
      patch,
    }: UpdateSchoolSSOConnectionInput) =>
      updateSchoolSSOConnection(
        tenantID,
        connectionID,
        patch,
        await csrfToken(),
        { baseUrl: getApiBaseUrl() },
      ),
    onSuccess: (connection) => {
      upsertConnection(queryClient, tenantID, connection);
      return invalidateAfterWrite(queryClient, tenantID);
    },
  });
}

export interface CreateSchoolSSODomainInput {
  connectionID: string;
  domain: string;
}

/** Resolves with the one-time challenge; the caller keeps it in local state. */
export function useCreateSchoolSSODomain(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async ({
      connectionID,
      domain,
    }: CreateSchoolSSODomainInput): Promise<SSOChallenge> =>
      createSchoolSSODomain(
        tenantID,
        { connection_id: connectionID, domain },
        await csrfToken(),
        { baseUrl: getApiBaseUrl() },
      ),
    onSuccess: (challenge) => {
      upsertDomain(queryClient, tenantID, challenge.domain);
      return invalidateAfterWrite(queryClient, tenantID);
    },
  });
}

export type SchoolSSODomainAction = "rotate" | "verify" | "disable";

export interface SchoolSSODomainActionInput {
  action: SchoolSSODomainAction;
  domainID: string;
}

export type SchoolSSODomainActionResult =
  | { action: "rotate"; challenge: SSOChallenge }
  | { action: "verify" | "disable"; domain: SSODomain };

export function useSchoolSSODomainAction(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async ({
      action,
      domainID,
    }: SchoolSSODomainActionInput): Promise<SchoolSSODomainActionResult> => {
      const token = await csrfToken();
      const options = { baseUrl: getApiBaseUrl() };
      if (action === "rotate") {
        return {
          action,
          challenge: await rotateSchoolSSODomain(
            tenantID,
            domainID,
            token,
            options,
          ),
        };
      }
      const domain =
        action === "verify"
          ? await verifySchoolSSODomain(tenantID, domainID, token, options)
          : await disableSchoolSSODomain(tenantID, domainID, token, options);
      return { action, domain };
    },
    onSuccess: (result) => {
      upsertDomain(
        queryClient,
        tenantID,
        result.action === "rotate" ? result.challenge.domain : result.domain,
      );
      return invalidateAfterWrite(queryClient, tenantID);
    },
  });
}

/** Context for turning a refusal into actionable copy. */
export interface SchoolSSOErrorContext {
  domain?: string;
}

type ConnectionKind = SSOConnection["kind"];
type Translate = (
  key: TranslationKey,
  values?: Record<string, string | number>,
) => string;

const MAX_ORG_IDS = 64;
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const DNS_LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const MAX_RETRY_AFTER_SECONDS = 3600;

/**
 * Advisory client check only (the server normalizes and decides). Accepts
 * IDN input by letting the URL parser produce its A-label form.
 */
export function isPlausibleDomain(value: string) {
  if (value === "" || /[\s@/:?#\\[\]]/.test(value)) return false;
  let ascii: string;
  try {
    ascii = new URL(`http://${value}`).hostname;
  } catch {
    return false;
  }
  if (ascii.length > 253 || ascii.endsWith(".")) return false;
  const labels = ascii.split(".");
  const top = labels[labels.length - 1] ?? "";
  return (
    labels.length >= 2 &&
    labels.every((label) => DNS_LABEL.test(label)) &&
    /[a-z]/.test(top)
  );
}

export function parseOrgIDs(
  kind: ConnectionKind,
  text: string,
): { ids: string[]; error?: TranslationKey } {
  const ids = text
    .split(/[\s,;]+/)
    .map((value) => value.trim().toLowerCase())
    .filter(Boolean);
  if (ids.length === 0) return { ids, error: "schoolSSO.orgIDsRequired" };
  if (ids.length > MAX_ORG_IDS || new Set(ids).size !== ids.length) {
    return { ids, error: "schoolSSO.orgIDsTooMany" };
  }
  const valid =
    kind === "entra"
      ? ids.every((id) => UUID_PATTERN.test(id))
      : ids.every(isPlausibleDomain);
  if (!valid) {
    return {
      ids,
      error:
        kind === "entra"
          ? "schoolSSO.orgIDsInvalidEntra"
          : "schoolSSO.orgIDsInvalidGoogle",
    };
  }
  return { ids };
}

/** Maps the AUTH-12A problem contract to bilingual, actionable copy. */
export function schoolSSOErrorText(
  error: unknown,
  t: Translate,
  context: SchoolSSOErrorContext = {},
) {
  if (!(error instanceof APIRequestError)) return t("schoolSSO.errorNetwork");
  const code = error.problem?.code;
  switch (error.status) {
    case 400:
      return t("schoolSSO.errorInvalid");
    case 403:
      return stepUpRequirementFor(error)
        ? t("schoolSSO.errorStepUp")
        : t("schoolSSO.errorForbidden");
    case 404:
      return t("schoolSSO.errorNotFound");
    case 409:
      if (code === "auth_sso_lockout") return t("schoolSSO.errorLockout");
      if (code === "auth_sso_dns_mismatch") {
        return t("schoolSSO.errorDNSMismatch", {
          record: context.domain
            ? `_tutorhub-challenge.${context.domain}.`
            : "_tutorhub-challenge.<domain>.",
        });
      }
      return t("schoolSSO.errorConflict");
    case 429: {
      const seconds = error.retryAfterSeconds;
      return seconds
        ? t("schoolSSO.errorRateLimited", {
            seconds: Math.min(seconds, MAX_RETRY_AFTER_SECONDS),
          })
        : t("schoolSSO.errorRateLimitedGeneric");
    }
    case 503:
      return t("schoolSSO.errorUnavailable");
    default:
      return t("schoolSSO.errorGeneric");
  }
}
