import type { TenantCapabilities } from "@tutorhub/api-client";
import {
  tenantFeatureKeys,
  tenantQuotaKeys,
  type TenantFeatureKey,
  type TenantQuotaKey,
} from "./featureControlCatalog";

const draftPrefix = "tutorhub:draft:v1:";
const draftSchemaVersion = 1;
const draftTimeToLiveMilliseconds = 8 * 60 * 60 * 1000;
const maximumDraftBytes = 16 * 1024;

export interface TenantFeatureControlDraft {
  base: {
    tenantID: string;
    version: number;
  };
  features: Record<TenantFeatureKey, boolean>;
  quotas: Record<TenantQuotaKey, string>;
}

export interface TenantFeatureControlDraftScope {
  actorID: string;
  tenantID: string;
}

interface StoredTenantFeatureControlDraft {
  actor_id: string;
  expires_at: number;
  kind: "tenant_feature_controls";
  schema_version: 1;
  tenant_id: string;
  value: TenantFeatureControlDraft;
}

function draftKey(scope: TenantFeatureControlDraftScope) {
  return `${draftPrefix}tenant_feature_controls:${encodeURIComponent(scope.actorID)}:${encodeURIComponent(scope.tenantID)}`;
}

function activeSessionStorage() {
  try {
    return globalThis.sessionStorage;
  } catch {
    return undefined;
  }
}

function removeDraft(storage: Storage, key: string) {
  try {
    storage.removeItem(key);
  } catch {
    // Storage is advisory. The in-memory form remains authoritative for this render.
  }
}

export function readTenantFeatureControlDraft(
  scope: TenantFeatureControlDraftScope,
  now = Date.now(),
): TenantFeatureControlDraft | null {
  const storage = activeSessionStorage();
  const key = draftKey(scope);
  if (!storage || !scope.actorID || !scope.tenantID) {
    return null;
  }

  let serialized: string | null;
  try {
    serialized = storage.getItem(key);
  } catch {
    return null;
  }
  if (!serialized) {
    return null;
  }
  if (new TextEncoder().encode(serialized).length > maximumDraftBytes) {
    removeDraft(storage, key);
    return null;
  }

  try {
    const candidate = JSON.parse(serialized) as unknown;
    if (!isStoredDraft(candidate, scope, now)) {
      removeDraft(storage, key);
      return null;
    }
    return candidate.value;
  } catch {
    removeDraft(storage, key);
    return null;
  }
}

export function writeTenantFeatureControlDraft(
  scope: TenantFeatureControlDraftScope,
  value: TenantFeatureControlDraft,
  now = Date.now(),
) {
  const storage = activeSessionStorage();
  if (!storage || !scope.actorID || !scope.tenantID || !isDraftValue(value)) {
    return false;
  }
  const record: StoredTenantFeatureControlDraft = {
    actor_id: scope.actorID,
    expires_at: now + draftTimeToLiveMilliseconds,
    kind: "tenant_feature_controls",
    schema_version: draftSchemaVersion,
    tenant_id: scope.tenantID,
    value,
  };
  const serialized = JSON.stringify(record);
  if (new TextEncoder().encode(serialized).length > maximumDraftBytes) {
    return false;
  }
  try {
    storage.setItem(draftKey(scope), serialized);
    return true;
  } catch {
    return false;
  }
}

export function removeTenantFeatureControlDraft(
  scope: TenantFeatureControlDraftScope,
) {
  const storage = activeSessionStorage();
  if (storage && scope.actorID && scope.tenantID) {
    removeDraft(storage, draftKey(scope));
  }
}

export function clearClientDrafts() {
  const storage = activeSessionStorage();
  if (!storage) {
    return;
  }
  try {
    const keys: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key?.startsWith(draftPrefix)) {
        keys.push(key);
      }
    }
    for (const key of keys) {
      removeDraft(storage, key);
    }
  } catch {
    // Storage cleanup is best effort; scoped keys still prevent cross-principal reads.
  }
}

function isStoredDraft(
  value: unknown,
  scope: TenantFeatureControlDraftScope,
  now: number,
): value is StoredTenantFeatureControlDraft {
  if (!isRecord(value)) {
    return false;
  }
  return (
    value.schema_version === draftSchemaVersion &&
    value.kind === "tenant_feature_controls" &&
    value.actor_id === scope.actorID &&
    value.tenant_id === scope.tenantID &&
    typeof value.expires_at === "number" &&
    Number.isSafeInteger(value.expires_at) &&
    value.expires_at > now &&
    value.expires_at <= now + draftTimeToLiveMilliseconds &&
    isDraftValue(value.value) &&
    value.value.base.tenantID === scope.tenantID
  );
}

function isDraftValue(value: unknown): value is TenantFeatureControlDraft {
  if (!isRecord(value) || !isRecord(value.base)) {
    return false;
  }
  const features = value.features;
  const quotas = value.quotas;
  if (
    typeof value.base.tenantID !== "string" ||
    !value.base.tenantID ||
    !Number.isSafeInteger(value.base.version) ||
    Number(value.base.version) < 0 ||
    !isRecord(features) ||
    !isRecord(quotas)
  ) {
    return false;
  }
  if (
    Object.keys(features).length !== tenantFeatureKeys.length ||
    Object.keys(quotas).length !== tenantQuotaKeys.length
  ) {
    return false;
  }
  return (
    tenantFeatureKeys.every((key) => typeof features[key] === "boolean") &&
    tenantQuotaKeys.every((key) => {
      const candidate = quotas[key];
      return typeof candidate === "string" && /^\d{1,20}$/.test(candidate);
    })
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// Compile-time guard: draft keys remain exactly aligned with the generated capability contract.
type _FeatureDraftContract =
  TenantFeatureControlDraft["features"] extends Record<
    keyof TenantCapabilities["features"],
    boolean
  >
    ? true
    : never;
type _QuotaDraftContract =
  TenantFeatureControlDraft["quotas"] extends Record<
    keyof TenantCapabilities["quotas"],
    string
  >
    ? true
    : never;
void (true satisfies _FeatureDraftContract);
void (true satisfies _QuotaDraftContract);
