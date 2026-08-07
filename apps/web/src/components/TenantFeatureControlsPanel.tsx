import { APIRequestError, type TenantCapabilities } from "@tutorhub/api-client";
import {
  Button,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import { RefreshCw, Save, SlidersHorizontal } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  useTenantCapabilities,
  useUpdateTenantFeatureControls,
} from "../app/tenantCapabilities";

type FeatureKey = keyof TenantCapabilities["features"];
type QuotaKey = keyof TenantCapabilities["quotas"];

const featureKeys = [
  "membership_invitations",
  "class_management",
  "class_invite_links",
  "class_session_scheduling",
  "class_session_recurrence",
  "conversations",
  "file_uploads",
  "in_app_notifications",
  "availability_polls",
] as const satisfies readonly FeatureKey[];

const quotaKeys = [
  "members",
  "active_classes",
  "invite_creations_per_hour",
  "active_availability_polls",
  "availability_poll_range_days",
  "availability_poll_slots",
  "availability_poll_participants",
  "availability_poll_creations_per_hour",
  "availability_poll_capability_creations_per_hour",
  "active_study_meetings",
  "study_meeting_creations_per_hour",
  "messages_per_tenant",
  "message_sends_per_hour",
  "files_per_tenant",
  "file_bytes_per_tenant",
  "single_file_bytes",
  "file_upload_intents_per_hour",
] as const satisfies readonly QuotaKey[];

const featureLabelKeys: Record<FeatureKey, TranslationKey> = {
  availability_polls: "capabilities.featureAvailabilityPolls",
  class_invite_links: "capabilities.featureClassInviteLinks",
  class_session_scheduling: "capabilities.featureClassSessionScheduling",
  class_session_recurrence: "capabilities.featureClassSessionRecurrence",
  class_management: "capabilities.featureClassManagement",
  conversations: "capabilities.featureConversations",
  file_uploads: "capabilities.featureFileUploads",
  in_app_notifications: "capabilities.featureInAppNotifications",
  membership_invitations: "capabilities.featureMembershipInvitations",
};

const quotaLabelKeys: Record<QuotaKey, TranslationKey> = {
  active_availability_polls: "capabilities.quotaActiveAvailabilityPolls",
  active_classes: "capabilities.quotaActiveClasses",
  active_study_meetings: "capabilities.quotaActiveStudyMeetings",
  invite_creations_per_hour: "capabilities.quotaInviteCreations",
  availability_poll_range_days: "capabilities.quotaAvailabilityPollRangeDays",
  availability_poll_slots: "capabilities.quotaAvailabilityPollSlots",
  availability_poll_participants:
    "capabilities.quotaAvailabilityPollParticipants",
  availability_poll_creations_per_hour:
    "capabilities.quotaAvailabilityPollCreations",
  availability_poll_capability_creations_per_hour:
    "capabilities.quotaAvailabilityPollCapabilityCreations",
  study_meeting_creations_per_hour: "capabilities.quotaStudyMeetingCreations",
  messages_per_tenant: "capabilities.quotaMessages",
  message_sends_per_hour: "capabilities.quotaMessageSends",
  files_per_tenant: "capabilities.quotaFiles",
  file_bytes_per_tenant: "capabilities.quotaFileBytes",
  single_file_bytes: "capabilities.quotaSingleFileBytes",
  file_upload_intents_per_hour: "capabilities.quotaFileUploadIntents",
  members: "capabilities.quotaMembers",
};

interface ControlsDraft {
  base: {
    tenantID: string;
    version: number;
  };
  features: Record<FeatureKey, boolean>;
  quotas: Record<QuotaKey, string>;
}

function configuredFeature(capabilities: TenantCapabilities, key: FeatureKey) {
  return (
    capabilities.features[key].configured_enabled ??
    capabilities.features[key].enabled
  );
}

function configuredQuota(capabilities: TenantCapabilities, key: QuotaKey) {
  return (
    capabilities.quotas[key].configured_limit ?? capabilities.quotas[key].limit
  );
}

function controlsDraft(capabilities: TenantCapabilities): ControlsDraft {
  return {
    base: {
      tenantID: capabilities.tenant_id,
      version: capabilities.version,
    },
    features: {
      availability_polls: configuredFeature(capabilities, "availability_polls"),
      class_invite_links: configuredFeature(capabilities, "class_invite_links"),
      class_session_scheduling: configuredFeature(
        capabilities,
        "class_session_scheduling",
      ),
      class_session_recurrence: configuredFeature(
        capabilities,
        "class_session_recurrence",
      ),
      class_management: configuredFeature(capabilities, "class_management"),
      conversations: configuredFeature(capabilities, "conversations"),
      file_uploads: configuredFeature(capabilities, "file_uploads"),
      in_app_notifications: configuredFeature(
        capabilities,
        "in_app_notifications",
      ),
      membership_invitations: configuredFeature(
        capabilities,
        "membership_invitations",
      ),
    },
    quotas: {
      active_availability_polls: String(
        configuredQuota(capabilities, "active_availability_polls"),
      ),
      active_classes: String(configuredQuota(capabilities, "active_classes")),
      active_study_meetings: String(
        configuredQuota(capabilities, "active_study_meetings"),
      ),
      invite_creations_per_hour: String(
        configuredQuota(capabilities, "invite_creations_per_hour"),
      ),
      availability_poll_range_days: String(
        configuredQuota(capabilities, "availability_poll_range_days"),
      ),
      availability_poll_slots: String(
        configuredQuota(capabilities, "availability_poll_slots"),
      ),
      availability_poll_participants: String(
        configuredQuota(capabilities, "availability_poll_participants"),
      ),
      availability_poll_creations_per_hour: String(
        configuredQuota(capabilities, "availability_poll_creations_per_hour"),
      ),
      availability_poll_capability_creations_per_hour: String(
        configuredQuota(
          capabilities,
          "availability_poll_capability_creations_per_hour",
        ),
      ),
      study_meeting_creations_per_hour: String(
        configuredQuota(capabilities, "study_meeting_creations_per_hour"),
      ),
      messages_per_tenant: String(
        configuredQuota(capabilities, "messages_per_tenant"),
      ),
      message_sends_per_hour: String(
        configuredQuota(capabilities, "message_sends_per_hour"),
      ),
      files_per_tenant: String(
        configuredQuota(capabilities, "files_per_tenant"),
      ),
      file_bytes_per_tenant: String(
        configuredQuota(capabilities, "file_bytes_per_tenant"),
      ),
      single_file_bytes: String(
        configuredQuota(capabilities, "single_file_bytes"),
      ),
      file_upload_intents_per_hour: String(
        configuredQuota(capabilities, "file_upload_intents_per_hour"),
      ),
      members: String(configuredQuota(capabilities, "members")),
    },
  };
}

function draftChanged(draft: ControlsDraft, capabilities: TenantCapabilities) {
  return (
    featureKeys.some(
      (key) => draft.features[key] !== configuredFeature(capabilities, key),
    ) ||
    quotaKeys.some(
      (key) => Number(draft.quotas[key]) !== configuredQuota(capabilities, key),
    )
  );
}

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function isConflict(error: Error | null) {
  return error instanceof APIRequestError && error.status === 409;
}

export function TenantFeatureControlsPanel({
  capabilities: capabilitiesQuery,
  tenantID,
}: {
  capabilities: ReturnType<typeof useTenantCapabilities>;
  tenantID: string;
}) {
  const { language, t } = useI18n();
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  if (capabilitiesQuery.isPending) {
    return (
      <section className="workspace-management__panel tenant-controls">
        <SkeletonGroup label={t("capabilities.loading")}>
          <Skeleton height={28} width="46%" />
          <Skeleton height={92} />
          <Skeleton height={160} />
        </SkeletonGroup>
      </section>
    );
  }

  if (capabilitiesQuery.isError || !capabilitiesQuery.data) {
    const State = isForbidden(capabilitiesQuery.error)
      ? ForbiddenState
      : ErrorState;
    return (
      <section className="workspace-management__panel tenant-controls">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void capabilitiesQuery.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={
            isForbidden(capabilitiesQuery.error)
              ? t("capabilities.forbiddenDescription")
              : t("capabilities.errorDescription")
          }
          title={
            isForbidden(capabilitiesQuery.error)
              ? t("capabilities.forbiddenTitle")
              : t("capabilities.errorTitle")
          }
        />
      </section>
    );
  }

  const capabilities = capabilitiesQuery.data;
  if (capabilities.tenant_id !== tenantID) {
    return null;
  }

  return (
    <section
      aria-labelledby="tenant-controls-title"
      className="workspace-management__panel tenant-controls"
    >
      <div className="workspace-management__panel-heading">
        <span aria-hidden="true">
          <SlidersHorizontal />
        </span>
        <div>
          <h2 id="tenant-controls-title">{t("capabilities.title")}</h2>
          <p>{t("capabilities.description")}</p>
        </div>
      </div>

      {capabilitiesQuery.isFetching && (
        <p className="tenant-controls__refreshing" role="status">
          {t("capabilities.refreshing")}
        </p>
      )}

      <div className="tenant-controls__features">
        {featureKeys.map((key) => (
          <div key={key}>
            <span>{t(featureLabelKeys[key])}</span>
            <StatusBadge
              tone={capabilities.features[key].enabled ? "success" : "neutral"}
            >
              {capabilities.features[key].enabled
                ? t("capabilities.enabled")
                : t("capabilities.disabled")}
            </StatusBadge>
          </div>
        ))}
      </div>

      <div className="tenant-controls__quotas">
        {quotaKeys.map((key) => {
          const quota = capabilities.quotas[key];
          return (
            <article key={key}>
              <div>
                <h3>{t(quotaLabelKeys[key])}</h3>
                <strong>
                  {t("capabilities.quotaUsage", {
                    limit: quota.limit,
                    used: quota.used,
                  })}
                </strong>
              </div>
              <meter
                aria-label={t(quotaLabelKeys[key])}
                max={quota.limit}
                value={Math.min(quota.used, quota.limit)}
              />
              <span>
                {t("capabilities.quotaRemaining", {
                  remaining: quota.remaining,
                })}
              </span>
              {quota.reset_at && (
                <small>
                  {t("capabilities.quotaReset", {
                    date: dateFormatter.format(new Date(quota.reset_at)),
                  })}
                </small>
              )}
            </article>
          );
        })}
      </div>

      {capabilities.can_manage_overrides ? (
        <TenantFeatureControlsForm
          capabilities={capabilities}
          onReload={() => capabilitiesQuery.refetch()}
        />
      ) : (
        <p className="tenant-controls__readonly">
          {t("capabilities.readonlyDescription")}
        </p>
      )}
    </section>
  );
}

function TenantFeatureControlsForm({
  capabilities,
  onReload,
}: {
  capabilities: TenantCapabilities;
  onReload: () => Promise<unknown>;
}) {
  const { t } = useI18n();
  const updateControls = useUpdateTenantFeatureControls();
  const [draftOverride, setDraftOverride] = useState<ControlsDraft | null>(
    null,
  );
  const [quotaErrors, setQuotaErrors] = useState<
    Partial<Record<QuotaKey, string>>
  >({});
  const [feedback, setFeedback] = useState<string | null>(null);

  const draft =
    draftOverride?.base.tenantID === capabilities.tenant_id &&
    draftChanged(draftOverride, capabilities)
      ? draftOverride
      : controlsDraft(capabilities);
  const changed = draftChanged(draft, capabilities);

  const updateFeature = (key: FeatureKey, enabled: boolean) => {
    setDraftOverride((current) => {
      const source =
        current?.base.tenantID === capabilities.tenant_id &&
        draftChanged(current, capabilities)
          ? current
          : controlsDraft(capabilities);
      return {
        ...source,
        features: { ...source.features, [key]: enabled },
      };
    });
    setFeedback(null);
    updateControls.reset();
  };

  const updateQuota = (key: QuotaKey, value: string) => {
    setDraftOverride((current) => {
      const source =
        current?.base.tenantID === capabilities.tenant_id &&
        draftChanged(current, capabilities)
          ? current
          : controlsDraft(capabilities);
      return {
        ...source,
        quotas: { ...source.quotas, [key]: value },
      };
    });
    setQuotaErrors((current) => ({ ...current, [key]: undefined }));
    setFeedback(null);
    updateControls.reset();
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const errors: Partial<Record<QuotaKey, string>> = {};
    const quotas = {
      active_availability_polls: Number(draft.quotas.active_availability_polls),
      active_classes: Number(draft.quotas.active_classes),
      active_study_meetings: Number(draft.quotas.active_study_meetings),
      invite_creations_per_hour: Number(draft.quotas.invite_creations_per_hour),
      members: Number(draft.quotas.members),
      availability_poll_range_days: Number(
        draft.quotas.availability_poll_range_days,
      ),
      availability_poll_slots: Number(draft.quotas.availability_poll_slots),
      availability_poll_participants: Number(
        draft.quotas.availability_poll_participants,
      ),
      availability_poll_creations_per_hour: Number(
        draft.quotas.availability_poll_creations_per_hour,
      ),
      availability_poll_capability_creations_per_hour: Number(
        draft.quotas.availability_poll_capability_creations_per_hour,
      ),
      study_meeting_creations_per_hour: Number(
        draft.quotas.study_meeting_creations_per_hour,
      ),
      messages_per_tenant: Number(draft.quotas.messages_per_tenant),
      message_sends_per_hour: Number(draft.quotas.message_sends_per_hour),
      files_per_tenant: Number(draft.quotas.files_per_tenant),
      file_bytes_per_tenant: Number(draft.quotas.file_bytes_per_tenant),
      single_file_bytes: Number(draft.quotas.single_file_bytes),
      file_upload_intents_per_hour: Number(
        draft.quotas.file_upload_intents_per_hour,
      ),
    };
    for (const key of quotaKeys) {
      if (!Number.isSafeInteger(quotas[key]) || quotas[key] < 1) {
        errors[key] = t("capabilities.quotaValidation");
      }
    }
    setQuotaErrors(errors);
    setFeedback(null);
    if (Object.keys(errors).length > 0 || !changed) {
      return;
    }

    updateControls.mutate(
      {
        tenantID: capabilities.tenant_id,
        input: {
          expected_version: draft.base.version,
          features: draft.features,
          quotas,
        },
      },
      {
        onSuccess: () => {
          setDraftOverride(null);
          setFeedback(t("capabilities.updateSuccess"));
        },
      },
    );
  };

  const reload = async () => {
    await onReload();
    setDraftOverride(null);
    setQuotaErrors({});
    setFeedback(null);
    updateControls.reset();
  };

  return (
    <form className="tenant-controls__form" onSubmit={submit}>
      <fieldset disabled={updateControls.isPending}>
        <legend>{t("capabilities.featuresLegend")}</legend>
        {featureKeys.map((key) => (
          <label className="tenant-controls__toggle" key={key}>
            <input
              checked={draft.features[key]}
              onChange={(event) => updateFeature(key, event.target.checked)}
              type="checkbox"
            />
            <span>{t(featureLabelKeys[key])}</span>
          </label>
        ))}
      </fieldset>

      <fieldset
        className="tenant-controls__quota-fields"
        disabled={updateControls.isPending}
      >
        <legend>{t("capabilities.quotasLegend")}</legend>
        {quotaKeys.map((key) => (
          <TextField
            error={quotaErrors[key]}
            inputMode="numeric"
            key={key}
            label={t(quotaLabelKeys[key])}
            min={1}
            onChange={(event) => updateQuota(key, event.target.value)}
            required
            type="number"
            value={draft.quotas[key]}
          />
        ))}
      </fieldset>

      {updateControls.isError && (
        <div className="workspace-management__feedback" role="alert">
          <span>
            {isConflict(updateControls.error)
              ? t("capabilities.updateConflict")
              : isForbidden(updateControls.error)
                ? t("capabilities.updateForbidden")
                : updateControls.error instanceof APIRequestError &&
                    updateControls.error.status === 400
                  ? t("capabilities.updateInvalid")
                  : t("capabilities.updateError")}
          </span>
          {isConflict(updateControls.error) && (
            <Button onClick={() => void reload()} size="sm" variant="secondary">
              {t("capabilities.reloadLatest")}
            </Button>
          )}
        </div>
      )}

      {feedback && (
        <p className="workspace-management__success" role="status">
          {feedback}
        </p>
      )}

      <div className="workspace-management__form-actions">
        <Button
          disabled={!changed || updateControls.isPending}
          leadingIcon={<Save />}
          loading={updateControls.isPending}
          loadingLabel={t("capabilities.updating")}
          type="submit"
        >
          {t("capabilities.updateAction")}
        </Button>
      </div>
    </form>
  );
}
