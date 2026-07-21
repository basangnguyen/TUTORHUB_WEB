import { Button } from "@tutorhub/ui";
import { RefreshCw } from "lucide-react";
import { useI18n, type TranslationKey } from "../app/i18n";
import type {
  TenantOperationAvailability,
  TenantOperationReason,
} from "../app/tenantCapabilities";

function tenantOperationReasonKey(
  reason: TenantOperationReason,
): TranslationKey {
  if (reason === "feature_disabled") {
    return "capabilities.reasonFeatureDisabled";
  }
  if (reason === "quota_exhausted") {
    return "capabilities.reasonQuotaExhausted";
  }
  if (reason === "rate_limited") {
    return "capabilities.reasonRateLimited";
  }
  if (reason === "capabilities_loading") {
    return "capabilities.reasonLoading";
  }
  return "capabilities.reasonUnavailable";
}

export function TenantOperationNotice({
  availability,
  label,
  onRetry,
}: {
  availability: TenantOperationAvailability;
  label?: string;
  onRetry?: () => void;
}) {
  const { t } = useI18n();
  if (availability.available) {
    return null;
  }

  return (
    <div
      className="tenant-operation-notice"
      role={availability.reason === "capabilities_loading" ? "status" : "alert"}
    >
      <span>
        {label && <strong>{label}: </strong>}
        {t(tenantOperationReasonKey(availability.reason))}
      </span>
      {availability.reason !== "capabilities_loading" && onRetry && (
        <Button
          leadingIcon={<RefreshCw />}
          onClick={onRetry}
          size="sm"
          variant="secondary"
        >
          {t("state.retry")}
        </Button>
      )}
    </div>
  );
}
