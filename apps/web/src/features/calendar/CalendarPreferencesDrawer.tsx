import { APIRequestError } from "@tutorhub/api-client";
import {
  Button,
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
  SelectField,
  TextField,
} from "@tutorhub/ui";
import { Save } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useI18n, type TranslationKey } from "../../app/i18n";
import {
  calendarViews,
  type CalendarDisplayPreferenceDraft,
  type CalendarDisplayPreferenceViewModel,
  type CalendarView,
} from "./model";

const viewKeys: Record<CalendarView, TranslationKey> = {
  agenda: "calendar.view.agenda",
  day: "calendar.view.day",
  month: "calendar.view.month",
  week: "calendar.view.week",
  work_week: "calendar.view.work_week",
};

function isValidTimezone(value: string) {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: value }).format();
    return true;
  } catch {
    return false;
  }
}

interface CalendarPreferencesDrawerProps {
  error: Error | null;
  open: boolean;
  pending: boolean;
  preference: CalendarDisplayPreferenceViewModel;
  onOpenChange: (open: boolean) => void;
  onReload: () => Promise<CalendarDisplayPreferenceViewModel | undefined>;
  onSave: (
    preference: CalendarDisplayPreferenceDraft,
  ) => Promise<CalendarDisplayPreferenceViewModel>;
}

export function CalendarPreferencesDrawer({
  error,
  onOpenChange,
  onReload,
  onSave,
  open,
  pending,
  preference,
}: CalendarPreferencesDrawerProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<CalendarDisplayPreferenceDraft>(() => ({
    ...preference,
  }));
  const [submitted, setSubmitted] = useState(false);
  const [saved, setSaved] = useState(false);
  const latestPreference = useRef(preference);
  const viewerTimezoneInvalid = !isValidTimezone(draft.viewerTimezone);
  const secondaryTimezoneInvalid = Boolean(
    draft.secondaryTimezone && !isValidTimezone(draft.secondaryTimezone),
  );
  const conflict = error instanceof APIRequestError && error.status === 409;

  useEffect(() => {
    latestPreference.current = preference;
  }, [preference]);

  useEffect(() => {
    if (open) {
      setDraft({ ...latestPreference.current });
      setSubmitted(false);
      setSaved(false);
    }
  }, [open]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitted(true);
    setSaved(false);
    if (viewerTimezoneInvalid || secondaryTimezoneInvalid || pending) {
      return;
    }
    try {
      const updated = await onSave({
        ...draft,
        secondaryTimezone: draft.secondaryTimezone?.trim() || null,
        viewerTimezone: draft.viewerTimezone.trim(),
      });
      setDraft({ ...updated });
      setSaved(true);
    } catch {
      // The mutation error is rendered from the owner hook.
    }
  };

  return (
    <Drawer onOpenChange={onOpenChange} open={open}>
      <DrawerContent
        className="calendar-preferences"
        closeLabel={t("calendar.closePreferences")}
      >
        <DrawerTitle>{t("calendar.preferencesTitle")}</DrawerTitle>
        <DrawerDescription>
          {t("calendar.preferencesDescription")}
        </DrawerDescription>
        <form
          className="calendar-preferences__form"
          onSubmit={(event) => void submit(event)}
        >
          <TextField
            error={
              submitted && viewerTimezoneInvalid
                ? t("calendar.timezoneInvalid")
                : undefined
            }
            label={t("calendar.viewerTimezoneLabel")}
            maxLength={100}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                viewerTimezone: event.target.value,
              }))
            }
            required
            value={draft.viewerTimezone}
          />
          <SelectField
            ariaLabel={t("calendar.localeLabel")}
            label={t("calendar.localeLabel")}
            onValueChange={(locale) =>
              setDraft((current) => ({ ...current, locale }))
            }
            options={[
              { label: "Tiếng Việt (Việt Nam)", value: "vi-VN" },
              { label: "English (United States)", value: "en-US" },
            ]}
            value={draft.locale}
          />
          <SelectField
            ariaLabel={t("calendar.timeFormatLabel")}
            label={t("calendar.timeFormatLabel")}
            onValueChange={(hourCycle) =>
              setDraft((current) => ({
                ...current,
                hourCycle: hourCycle as "h12" | "h23",
              }))
            }
            options={[
              { label: t("calendar.timeFormat12"), value: "h12" },
              { label: t("calendar.timeFormat24"), value: "h23" },
            ]}
            value={draft.hourCycle}
          />
          <SelectField
            ariaLabel={t("calendar.weekStartLabel")}
            label={t("calendar.weekStartLabel")}
            onValueChange={(weekStart) =>
              setDraft((current) => ({
                ...current,
                weekStartsOn: weekStart === "sunday" ? 0 : 1,
              }))
            }
            options={[
              {
                label: t("calendar.weekStartMonday"),
                value: "monday",
              },
              {
                label: t("calendar.weekStartSunday"),
                value: "sunday",
              },
            ]}
            value={draft.weekStartsOn === 0 ? "sunday" : "monday"}
          />
          <SelectField
            ariaLabel={t("calendar.defaultViewLabel")}
            label={t("calendar.defaultViewLabel")}
            onValueChange={(defaultView) =>
              setDraft((current) => ({
                ...current,
                defaultView: defaultView as CalendarView,
              }))
            }
            options={calendarViews.map((value) => ({
              label: t(viewKeys[value]),
              value,
            }))}
            value={draft.defaultView}
          />
          <SelectField
            ariaLabel={t("calendar.densityLabel")}
            label={t("calendar.densityLabel")}
            onValueChange={(density) =>
              setDraft((current) => ({
                ...current,
                density: density as "comfortable" | "compact",
              }))
            }
            options={[
              {
                label: t("calendar.densityComfortable"),
                value: "comfortable",
              },
              {
                label: t("calendar.densityCompact"),
                value: "compact",
              },
            ]}
            value={draft.density}
          />
          <SelectField
            ariaLabel={t("calendar.timeScaleLabel")}
            label={t("calendar.timeScaleLabel")}
            onValueChange={(timeScale) =>
              setDraft((current) => ({
                ...current,
                timeScaleMinutes: Number(timeScale) as 15 | 30 | 60,
              }))
            }
            options={([15, 30, 60] as const).map((minutes) => ({
              label: t("calendar.timeScaleMinutes", { count: minutes }),
              value: String(minutes),
            }))}
            value={String(draft.timeScaleMinutes)}
          />
          <TextField
            error={
              submitted && secondaryTimezoneInvalid
                ? t("calendar.timezoneInvalid")
                : undefined
            }
            hint={t("calendar.secondaryTimezoneHint")}
            label={t("calendar.secondaryTimezoneLabel")}
            maxLength={100}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                secondaryTimezone: event.target.value,
              }))
            }
            value={draft.secondaryTimezone ?? ""}
          />

          {error && (
            <div className="calendar-preferences__feedback" role="alert">
              <span>
                {conflict
                  ? t("calendar.preferencesConflict")
                  : t("calendar.preferencesError")}
              </span>
              {conflict && (
                <Button
                  onClick={() => {
                    void onReload().then((reloaded) => {
                      if (reloaded) {
                        setDraft({ ...reloaded });
                        setSubmitted(false);
                        setSaved(false);
                      }
                    });
                  }}
                  size="sm"
                  variant="secondary"
                >
                  {t("calendar.preferencesReload")}
                </Button>
              )}
            </div>
          )}
          {saved && (
            <p className="calendar-preferences__saved" role="status">
              {t("calendar.preferencesSaved")}
            </p>
          )}

          <div className="calendar-preferences__actions">
            <DrawerClose asChild>
              <Button disabled={pending} variant="secondary">
                {t("calendar.preferencesCancel")}
              </Button>
            </DrawerClose>
            <Button
              leadingIcon={<Save />}
              loading={pending}
              loadingLabel={t("calendar.preferencesSaving")}
              type="submit"
            >
              {t("calendar.preferencesSave")}
            </Button>
          </div>
        </form>
      </DrawerContent>
    </Drawer>
  );
}
