import {
  APIRequestError,
  type CalendarWorkingSchedule,
} from "@tutorhub/api-client";
import {
  Button,
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
  TextField,
} from "@tutorhub/ui";
import { Plus, Save, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useI18n, type TranslationKey } from "../../app/i18n";

type WorkingInterval = CalendarWorkingSchedule["weekly_intervals"][number];
type Weekday = WorkingInterval["weekday"];
type ScheduleException = CalendarWorkingSchedule["exceptions"][number];

const weekdays: readonly Weekday[] = [
  "monday",
  "tuesday",
  "wednesday",
  "thursday",
  "friday",
  "saturday",
  "sunday",
];

const weekdayKeys: Record<Weekday, TranslationKey> = {
  friday: "calendar.workingSchedule.weekday.friday",
  monday: "calendar.workingSchedule.weekday.monday",
  saturday: "calendar.workingSchedule.weekday.saturday",
  sunday: "calendar.workingSchedule.weekday.sunday",
  thursday: "calendar.workingSchedule.weekday.thursday",
  tuesday: "calendar.workingSchedule.weekday.tuesday",
  wednesday: "calendar.workingSchedule.weekday.wednesday",
};

interface CivilIntervalDraft {
  ends_at: string;
  starts_at: string;
}

interface ExceptionDraft {
  date: string;
  intervals: CivilIntervalDraft[];
  kind: "holiday" | "out_of_office" | "special_hours";
}

interface WorkingScheduleDraft {
  exceptions: ExceptionDraft[];
  timezone: string;
  version: number;
  weekly: Record<Weekday, CivilIntervalDraft[]>;
}

function emptyWeekly(): Record<Weekday, CivilIntervalDraft[]> {
  return {
    friday: [],
    monday: [],
    saturday: [],
    sunday: [],
    thursday: [],
    tuesday: [],
    wednesday: [],
  };
}

function toDraft(schedule: CalendarWorkingSchedule): WorkingScheduleDraft {
  const weekly = emptyWeekly();
  for (const interval of schedule.weekly_intervals) {
    weekly[interval.weekday].push({
      ends_at: interval.ends_at,
      starts_at: interval.starts_at,
    });
  }
  return {
    exceptions: schedule.exceptions.map((exception) => ({
      date: exception.date,
      intervals: exception.intervals.map((interval) => ({ ...interval })),
      kind: exception.kind,
    })),
    timezone: schedule.timezone,
    version: schedule.version,
    weekly,
  };
}

function validTimezone(value: string) {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: value }).format();
    return true;
  } catch {
    return false;
  }
}

function intervalsValid(intervals: readonly CivilIntervalDraft[]) {
  const ordered = [...intervals].sort((left, right) =>
    left.starts_at.localeCompare(right.starts_at),
  );
  return ordered.every((interval, index) => {
    const previous = ordered[index - 1];
    return (
      /^\d{2}:\d{2}$/.test(interval.starts_at) &&
      /^\d{2}:\d{2}$/.test(interval.ends_at) &&
      interval.starts_at < interval.ends_at &&
      (previous === undefined || previous.ends_at <= interval.starts_at)
    );
  });
}

function draftValid(draft: WorkingScheduleDraft) {
  const exceptionDates = new Set<string>();
  return (
    validTimezone(draft.timezone.trim()) &&
    weekdays.every(
      (weekday) =>
        draft.weekly[weekday].length <= 8 &&
        intervalsValid(draft.weekly[weekday]),
    ) &&
    draft.exceptions.length <= 366 &&
    draft.exceptions.every((exception) => {
      if (!/^\d{4}-\d{2}-\d{2}$/.test(exception.date)) {
        return false;
      }
      if (exceptionDates.has(exception.date)) {
        return false;
      }
      exceptionDates.add(exception.date);
      return exception.kind === "special_hours"
        ? exception.intervals.length > 0 &&
            exception.intervals.length <= 8 &&
            intervalsValid(exception.intervals)
        : exception.intervals.length === 0;
    })
  );
}

interface WorkingScheduleDrawerProps {
  error: Error | null;
  loading: boolean;
  open: boolean;
  pending: boolean;
  schedule: CalendarWorkingSchedule | undefined;
  onOpenChange: (open: boolean) => void;
  onReload: () => Promise<CalendarWorkingSchedule | undefined>;
  onSave: (input: {
    exceptions: readonly ScheduleException[];
    expected_version: number;
    timezone: string;
    weekly_intervals: readonly WorkingInterval[];
  }) => Promise<CalendarWorkingSchedule>;
}

export function WorkingScheduleDrawer({
  error,
  loading,
  onOpenChange,
  onReload,
  onSave,
  open,
  pending,
  schedule,
}: WorkingScheduleDrawerProps) {
  const { t } = useI18n();
  const latestSchedule = useRef(schedule);
  const draftInitializedForOpen = useRef(false);
  const [draft, setDraft] = useState<WorkingScheduleDraft | null>(() =>
    schedule ? toDraft(schedule) : null,
  );
  const [submitted, setSubmitted] = useState(false);
  const [saved, setSaved] = useState(false);
  const conflict = error instanceof APIRequestError && error.status === 409;
  const valid = useMemo(() => (draft ? draftValid(draft) : false), [draft]);

  useEffect(() => {
    latestSchedule.current = schedule;
  }, [schedule]);

  useEffect(() => {
    if (!open) {
      draftInitializedForOpen.current = false;
      return;
    }
    if (draftInitializedForOpen.current) return;

    const currentSchedule = latestSchedule.current;
    setDraft(currentSchedule ? toDraft(currentSchedule) : null);
    setSubmitted(false);
    setSaved(false);
    draftInitializedForOpen.current = currentSchedule !== undefined;
  }, [open, schedule]);

  const updateInterval = (
    weekday: Weekday,
    index: number,
    field: keyof CivilIntervalDraft,
    value: string,
  ) => {
    setDraft((current) => {
      if (!current) return current;
      const intervals = current.weekly[weekday].map(
        (interval, intervalIndex) =>
          intervalIndex === index ? { ...interval, [field]: value } : interval,
      );
      return {
        ...current,
        weekly: { ...current.weekly, [weekday]: intervals },
      };
    });
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitted(true);
    setSaved(false);
    if (!draft || !valid || pending) return;
    try {
      const updated = await onSave({
        exceptions: draft.exceptions,
        expected_version: draft.version,
        timezone: draft.timezone.trim(),
        weekly_intervals: weekdays.flatMap((weekday) =>
          draft.weekly[weekday].map((interval) => ({ ...interval, weekday })),
        ),
      });
      setDraft(toDraft(updated));
      setSaved(true);
    } catch {
      // The mutation error is rendered below.
    }
  };

  return (
    <Drawer onOpenChange={onOpenChange} open={open}>
      <DrawerContent
        className="calendar-working-schedule"
        closeLabel={t("calendar.workingSchedule.close")}
      >
        <DrawerTitle>{t("calendar.workingSchedule.title")}</DrawerTitle>
        <DrawerDescription>
          {t("calendar.workingSchedule.description")}
        </DrawerDescription>

        {loading && (
          <p aria-live="polite">{t("calendar.workingSchedule.loading")}</p>
        )}
        {!loading && !draft && (
          <div role="alert">
            <p>{t("calendar.workingSchedule.loadError")}</p>
            <Button
              onClick={() => void onReload()}
              size="sm"
              variant="secondary"
            >
              {t("calendar.retry")}
            </Button>
          </div>
        )}

        {draft && (
          <form
            className="calendar-working-schedule__form"
            onSubmit={(event) => void submit(event)}
          >
            <TextField
              error={
                submitted && !validTimezone(draft.timezone.trim())
                  ? t("calendar.timezoneInvalid")
                  : undefined
              }
              label={t("calendar.workingSchedule.timezone")}
              maxLength={100}
              onChange={(event) =>
                setDraft((current) =>
                  current
                    ? { ...current, timezone: event.target.value }
                    : current,
                )
              }
              required
              value={draft.timezone}
            />

            <section aria-labelledby="calendar-working-weekly-title">
              <h3 id="calendar-working-weekly-title">
                {t("calendar.workingSchedule.weekly")}
              </h3>
              <p>{t("calendar.workingSchedule.weeklyHint")}</p>
              <div className="calendar-working-schedule__days">
                {weekdays.map((weekday) => (
                  <fieldset key={weekday}>
                    <legend>{t(weekdayKeys[weekday])}</legend>
                    {draft.weekly[weekday].length === 0 && (
                      <p>{t("calendar.workingSchedule.closed")}</p>
                    )}
                    {draft.weekly[weekday].map((interval, index) => (
                      <div
                        className="calendar-working-schedule__interval"
                        key={`${weekday}-${index}`}
                      >
                        <label>
                          <span>{t("calendar.workingSchedule.starts")}</span>
                          <input
                            aria-label={`${t(weekdayKeys[weekday])} ${t("calendar.workingSchedule.starts")}`}
                            onChange={(event) =>
                              updateInterval(
                                weekday,
                                index,
                                "starts_at",
                                event.target.value,
                              )
                            }
                            required
                            type="time"
                            value={interval.starts_at}
                          />
                        </label>
                        <label>
                          <span>{t("calendar.workingSchedule.ends")}</span>
                          <input
                            aria-label={`${t(weekdayKeys[weekday])} ${t("calendar.workingSchedule.ends")}`}
                            onChange={(event) =>
                              updateInterval(
                                weekday,
                                index,
                                "ends_at",
                                event.target.value,
                              )
                            }
                            required
                            type="time"
                            value={interval.ends_at}
                          />
                        </label>
                        <Button
                          aria-label={t(
                            "calendar.workingSchedule.removeInterval",
                            {
                              day: t(weekdayKeys[weekday]),
                            },
                          )}
                          leadingIcon={<Trash2 />}
                          onClick={() =>
                            setDraft((current) =>
                              current
                                ? {
                                    ...current,
                                    weekly: {
                                      ...current.weekly,
                                      [weekday]: current.weekly[weekday].filter(
                                        (_, intervalIndex) =>
                                          intervalIndex !== index,
                                      ),
                                    },
                                  }
                                : current,
                            )
                          }
                          size="sm"
                          type="button"
                          variant="quiet"
                        >
                          {t("calendar.workingSchedule.remove")}
                        </Button>
                      </div>
                    ))}
                    <Button
                      disabled={draft.weekly[weekday].length >= 8}
                      leadingIcon={<Plus />}
                      onClick={() =>
                        setDraft((current) =>
                          current
                            ? {
                                ...current,
                                weekly: {
                                  ...current.weekly,
                                  [weekday]: [
                                    ...current.weekly[weekday],
                                    { ends_at: "17:00", starts_at: "09:00" },
                                  ],
                                },
                              }
                            : current,
                        )
                      }
                      size="sm"
                      type="button"
                      variant="secondary"
                    >
                      {t("calendar.workingSchedule.addInterval")}
                    </Button>
                  </fieldset>
                ))}
              </div>
            </section>

            <section aria-labelledby="calendar-working-exceptions-title">
              <div className="calendar-working-schedule__section-heading">
                <div>
                  <h3 id="calendar-working-exceptions-title">
                    {t("calendar.workingSchedule.exceptions")}
                  </h3>
                  <p>{t("calendar.workingSchedule.exceptionsHint")}</p>
                </div>
                <Button
                  disabled={draft.exceptions.length >= 366}
                  leadingIcon={<Plus />}
                  onClick={() =>
                    setDraft((current) =>
                      current
                        ? {
                            ...current,
                            exceptions: [
                              ...current.exceptions,
                              { date: "", intervals: [], kind: "holiday" },
                            ],
                          }
                        : current,
                    )
                  }
                  size="sm"
                  type="button"
                  variant="secondary"
                >
                  {t("calendar.workingSchedule.addException")}
                </Button>
              </div>
              {draft.exceptions.map((exception, index) => (
                <div
                  className="calendar-working-schedule__exception"
                  key={`${exception.date}-${index}`}
                >
                  <label>
                    <span>{t("calendar.workingSchedule.date")}</span>
                    <input
                      aria-label={t("calendar.workingSchedule.date")}
                      onChange={(event) =>
                        setDraft((current) =>
                          current
                            ? {
                                ...current,
                                exceptions: current.exceptions.map(
                                  (value, valueIndex) =>
                                    valueIndex === index
                                      ? { ...value, date: event.target.value }
                                      : value,
                                ),
                              }
                            : current,
                        )
                      }
                      required
                      type="date"
                      value={exception.date}
                    />
                  </label>
                  <label>
                    <span>{t("calendar.workingSchedule.kind")}</span>
                    <select
                      aria-label={t("calendar.workingSchedule.kind")}
                      onChange={(event) => {
                        const kind = event.target
                          .value as ExceptionDraft["kind"];
                        setDraft((current) =>
                          current
                            ? {
                                ...current,
                                exceptions: current.exceptions.map(
                                  (value, valueIndex) =>
                                    valueIndex === index
                                      ? {
                                          ...value,
                                          intervals:
                                            kind === "special_hours"
                                              ? value.intervals.length > 0
                                                ? value.intervals
                                                : [
                                                    {
                                                      ends_at: "17:00",
                                                      starts_at: "09:00",
                                                    },
                                                  ]
                                              : [],
                                          kind,
                                        }
                                      : value,
                                ),
                              }
                            : current,
                        );
                      }}
                      value={exception.kind}
                    >
                      <option value="holiday">
                        {t("calendar.workingSchedule.kind.holiday")}
                      </option>
                      <option value="out_of_office">
                        {t("calendar.workingSchedule.kind.outOfOffice")}
                      </option>
                      <option value="special_hours">
                        {t("calendar.workingSchedule.kind.specialHours")}
                      </option>
                    </select>
                  </label>
                  {exception.kind === "special_hours" && (
                    <div className="calendar-working-schedule__special-hours">
                      {exception.intervals.map((interval, intervalIndex) => (
                        <div
                          className="calendar-working-schedule__interval"
                          key={intervalIndex}
                        >
                          <input
                            aria-label={t("calendar.workingSchedule.starts")}
                            onChange={(event) =>
                              setDraft((current) =>
                                current
                                  ? {
                                      ...current,
                                      exceptions: current.exceptions.map(
                                        (value, valueIndex) =>
                                          valueIndex === index
                                            ? {
                                                ...value,
                                                intervals: value.intervals.map(
                                                  (civil, civilIndex) =>
                                                    civilIndex === intervalIndex
                                                      ? {
                                                          ...civil,
                                                          starts_at:
                                                            event.target.value,
                                                        }
                                                      : civil,
                                                ),
                                              }
                                            : value,
                                      ),
                                    }
                                  : current,
                              )
                            }
                            required
                            type="time"
                            value={interval.starts_at}
                          />
                          <input
                            aria-label={t("calendar.workingSchedule.ends")}
                            onChange={(event) =>
                              setDraft((current) =>
                                current
                                  ? {
                                      ...current,
                                      exceptions: current.exceptions.map(
                                        (value, valueIndex) =>
                                          valueIndex === index
                                            ? {
                                                ...value,
                                                intervals: value.intervals.map(
                                                  (civil, civilIndex) =>
                                                    civilIndex === intervalIndex
                                                      ? {
                                                          ...civil,
                                                          ends_at:
                                                            event.target.value,
                                                        }
                                                      : civil,
                                                ),
                                              }
                                            : value,
                                      ),
                                    }
                                  : current,
                              )
                            }
                            required
                            type="time"
                            value={interval.ends_at}
                          />
                          <Button
                            aria-label={t("calendar.workingSchedule.remove")}
                            leadingIcon={<Trash2 />}
                            onClick={() =>
                              setDraft((current) =>
                                current
                                  ? {
                                      ...current,
                                      exceptions: current.exceptions.map(
                                        (value, valueIndex) =>
                                          valueIndex === index
                                            ? {
                                                ...value,
                                                intervals:
                                                  value.intervals.filter(
                                                    (_, civilIndex) =>
                                                      civilIndex !==
                                                      intervalIndex,
                                                  ),
                                              }
                                            : value,
                                      ),
                                    }
                                  : current,
                              )
                            }
                            size="sm"
                            type="button"
                            variant="quiet"
                          >
                            {t("calendar.workingSchedule.remove")}
                          </Button>
                        </div>
                      ))}
                      <Button
                        disabled={exception.intervals.length >= 8}
                        leadingIcon={<Plus />}
                        onClick={() =>
                          setDraft((current) =>
                            current
                              ? {
                                  ...current,
                                  exceptions: current.exceptions.map(
                                    (value, valueIndex) =>
                                      valueIndex === index
                                        ? {
                                            ...value,
                                            intervals: [
                                              ...value.intervals,
                                              {
                                                ends_at: "17:00",
                                                starts_at: "09:00",
                                              },
                                            ],
                                          }
                                        : value,
                                  ),
                                }
                              : current,
                          )
                        }
                        size="sm"
                        type="button"
                        variant="secondary"
                      >
                        {t("calendar.workingSchedule.addInterval")}
                      </Button>
                    </div>
                  )}
                  <Button
                    aria-label={t("calendar.workingSchedule.removeException")}
                    leadingIcon={<Trash2 />}
                    onClick={() =>
                      setDraft((current) =>
                        current
                          ? {
                              ...current,
                              exceptions: current.exceptions.filter(
                                (_, valueIndex) => valueIndex !== index,
                              ),
                            }
                          : current,
                      )
                    }
                    size="sm"
                    type="button"
                    variant="quiet"
                  >
                    {t("calendar.workingSchedule.remove")}
                  </Button>
                </div>
              ))}
            </section>

            {submitted && !valid && (
              <p className="calendar-working-schedule__feedback" role="alert">
                {t("calendar.workingSchedule.validation")}
              </p>
            )}
            {error && (
              <div className="calendar-working-schedule__feedback" role="alert">
                <span>
                  {conflict
                    ? t("calendar.workingSchedule.conflict")
                    : t("calendar.workingSchedule.saveError")}
                </span>
                {conflict && (
                  <Button
                    onClick={() =>
                      void onReload().then((reloaded) => {
                        if (reloaded) {
                          setDraft(toDraft(reloaded));
                          setSubmitted(false);
                        }
                      })
                    }
                    size="sm"
                    variant="secondary"
                  >
                    {t("calendar.preferencesReload")}
                  </Button>
                )}
              </div>
            )}
            {saved && (
              <p className="calendar-working-schedule__saved" role="status">
                {t("calendar.workingSchedule.saved")}
              </p>
            )}

            <div className="calendar-working-schedule__actions">
              <DrawerClose asChild>
                <Button disabled={pending} variant="secondary">
                  {t("calendar.preferencesCancel")}
                </Button>
              </DrawerClose>
              <Button
                leadingIcon={<Save />}
                loading={pending}
                loadingLabel={t("calendar.workingSchedule.saving")}
                type="submit"
              >
                {t("calendar.workingSchedule.save")}
              </Button>
            </div>
          </form>
        )}
      </DrawerContent>
    </Drawer>
  );
}
