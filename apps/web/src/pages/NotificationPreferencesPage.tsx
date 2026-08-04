import {
  APIRequestError,
  type NotificationPreference,
} from "@tutorhub/api-client";
import {
  Button,
  ErrorState,
  ForbiddenState,
  SelectField,
  Skeleton,
  SkeletonGroup,
} from "@tutorhub/ui";
import { RefreshCw, Save } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { useI18n } from "../app/i18n";
import {
  useNotificationPreference,
  useUpdateNotificationPreference,
} from "../app/notifications";
import { useSession } from "../app/session";

const reminderOptions = [5, 10, 15, 30, 60, 1_440] as const;

export function NotificationPreferencesPage() {
  const { t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const preference = useNotificationPreference(tenantID, true);
  const forbidden =
    preference.error instanceof APIRequestError &&
    preference.error.status === 403;

  if (preference.isPending) {
    return (
      <div className="page-content notification-preferences">
        <SkeletonGroup label={t("notificationPreferences.loading")}>
          <Skeleton height={88} />
          <Skeleton height={420} />
        </SkeletonGroup>
      </div>
    );
  }

  if (preference.isError || !preference.data) {
    const State = forbidden ? ForbiddenState : ErrorState;
    return (
      <div className="page-content notification-preferences">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void preference.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={
            forbidden
              ? t("notifications.forbiddenDescription")
              : t("notificationPreferences.errorDescription")
          }
          title={
            forbidden
              ? t("notifications.forbiddenTitle")
              : t("notificationPreferences.errorTitle")
          }
        />
      </div>
    );
  }

  return (
    <NotificationPreferenceForm
      onReload={async () => (await preference.refetch()).data}
      preference={preference.data}
      tenantID={tenantID}
    />
  );
}

function NotificationPreferenceForm({
  onReload,
  preference,
  tenantID,
}: {
  onReload: () => Promise<NotificationPreference | undefined>;
  preference: NotificationPreference;
  tenantID: string | undefined;
}) {
  const { t } = useI18n();
  const update = useUpdateNotificationPreference(tenantID);
  const [inAppEnabled, setInAppEnabled] = useState(preference.in_app_enabled);
  const [emailEnabled, setEmailEnabled] = useState(preference.email_enabled);
  const [reminderOffset, setReminderOffset] = useState(
    preference.reminder_offset_minutes,
  );
  const [quietEnabled, setQuietEnabled] = useState(
    preference.quiet_hours_enabled,
  );
  const [quietStart, setQuietStart] = useState(
    preference.quiet_hours_start ?? "22:00",
  );
  const [quietEnd, setQuietEnd] = useState(
    preference.quiet_hours_end ?? "07:00",
  );
  const [submitted, setSubmitted] = useState(false);
  const [saved, setSaved] = useState(false);
  const selectableReminderOptions = reminderOptions.includes(
    reminderOffset as (typeof reminderOptions)[number],
  )
    ? reminderOptions
    : [...reminderOptions, reminderOffset].sort((left, right) => left - right);

  const quietRangeInvalid =
    quietEnabled && (!quietStart || !quietEnd || quietStart === quietEnd);
  const conflict =
    update.error instanceof APIRequestError && update.error.status === 409;

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitted(true);
    setSaved(false);
    if (quietRangeInvalid || update.isPending) {
      return;
    }
    update.mutate(
      {
        input: {
          email_enabled: emailEnabled,
          expected_version: preference.version,
          in_app_enabled: inAppEnabled,
          quiet_hours_enabled: quietEnabled,
          quiet_hours_end: quietEnabled ? quietEnd : null,
          quiet_hours_start: quietEnabled ? quietStart : null,
          quiet_hours_timezone: preference.quiet_hours_timezone,
          reminder_offset_minutes: reminderOffset,
        },
      },
      { onSuccess: () => setSaved(true) },
    );
  };

  const reload = async () => {
    const freshPreference = await onReload();
    if (freshPreference) {
      setInAppEnabled(freshPreference.in_app_enabled);
      setEmailEnabled(freshPreference.email_enabled);
      setReminderOffset(freshPreference.reminder_offset_minutes);
      setQuietEnabled(freshPreference.quiet_hours_enabled);
      setQuietStart(freshPreference.quiet_hours_start ?? "22:00");
      setQuietEnd(freshPreference.quiet_hours_end ?? "07:00");
      setSubmitted(false);
    }
    update.reset();
    setSaved(false);
  };

  return (
    <div className="page-content notification-preferences">
      <Link className="classroom-back-link" to="/app/notifications">
        {t("notificationPreferences.back")}
      </Link>
      <header className="page-heading notification-preferences__header">
        <div>
          <p>{t("notificationPreferences.kicker")}</p>
          <h1>{t("notificationPreferences.title")}</h1>
          <span>{t("notificationPreferences.description")}</span>
        </div>
      </header>

      <form className="notification-preferences__panel" onSubmit={submit}>
        <PreferenceToggle
          checked={inAppEnabled}
          description={t("notificationPreferences.inAppHint")}
          label={t("notificationPreferences.inAppLabel")}
          onChange={(checked) => {
            setSaved(false);
            setInAppEnabled(checked);
          }}
        />
        <PreferenceToggle
          checked={emailEnabled}
          description={t("notificationPreferences.emailHint")}
          label={t("notificationPreferences.emailLabel")}
          onChange={(checked) => {
            setSaved(false);
            setEmailEnabled(checked);
          }}
        />

        <div className="notification-preferences__field">
          <SelectField
            ariaLabel={t("notificationPreferences.reminderLabel")}
            hint={t("notificationPreferences.reminderHint")}
            label={t("notificationPreferences.reminderLabel")}
            onValueChange={(value) => {
              setSaved(false);
              setReminderOffset(Number(value));
            }}
            options={selectableReminderOptions.map((minutes) => ({
              label:
                minutes === 1_440
                  ? t("notificationPreferences.reminderDay")
                  : t("notificationPreferences.reminderMinutes", {
                      count: minutes,
                    }),
              value: String(minutes),
            }))}
            value={String(reminderOffset)}
          />
        </div>

        <PreferenceToggle
          checked={quietEnabled}
          description={t("notificationPreferences.quietHoursHint")}
          label={t("notificationPreferences.quietHoursLabel")}
          onChange={(checked) => {
            setSaved(false);
            setQuietEnabled(checked);
          }}
        />

        {quietEnabled && (
          <div className="notification-preferences__quiet-hours">
            <label>
              <span>{t("notificationPreferences.quietStartLabel")}</span>
              <input
                aria-invalid={submitted && quietRangeInvalid}
                onChange={(event) => {
                  setSaved(false);
                  setQuietStart(event.target.value);
                }}
                required
                type="time"
                value={quietStart}
              />
            </label>
            <label>
              <span>{t("notificationPreferences.quietEndLabel")}</span>
              <input
                aria-invalid={submitted && quietRangeInvalid}
                onChange={(event) => {
                  setSaved(false);
                  setQuietEnd(event.target.value);
                }}
                required
                type="time"
                value={quietEnd}
              />
            </label>
            <p className="notification-preferences__timezone">
              {t("notificationPreferences.timezone", {
                timezone: preference.quiet_hours_timezone,
              })}
            </p>
            {submitted && quietRangeInvalid && (
              <p className="notification-preferences__validation" role="alert">
                {t("notificationPreferences.quietRangeError")}
              </p>
            )}
          </div>
        )}

        {update.isError && (
          <div className="notification-preferences__feedback" role="alert">
            <span>
              {conflict
                ? t("notificationPreferences.conflict")
                : t("notificationPreferences.saveError")}
            </span>
            {conflict && (
              <Button
                onClick={() => void reload()}
                size="sm"
                variant="secondary"
              >
                {t("notificationPreferences.reload")}
              </Button>
            )}
          </div>
        )}

        {saved && (
          <p className="notification-preferences__success" role="status">
            {t("notificationPreferences.saved")}
          </p>
        )}

        <div className="notification-preferences__actions">
          <Button
            leadingIcon={<Save />}
            loading={update.isPending}
            loadingLabel={t("notificationPreferences.saving")}
            type="submit"
          >
            {t("notificationPreferences.save")}
          </Button>
        </div>
      </form>
    </div>
  );
}

function PreferenceToggle({
  checked,
  description,
  label,
  onChange,
}: {
  checked: boolean;
  description: string;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="notification-preferences__toggle">
      <span>
        <strong>{label}</strong>
        <small>{description}</small>
      </span>
      <input
        aria-label={label}
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        type="checkbox"
      />
    </label>
  );
}
