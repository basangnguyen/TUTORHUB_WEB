import type {
  CalendarAvailabilityQueryRequest,
  CalendarAvailabilityQueryResponse,
  CalendarAvailabilityStatus,
  SessionAudience,
} from "@tutorhub/api-client";
import { Button } from "@tutorhub/ui";
import { RefreshCw, Search } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Temporal } from "temporal-polyfill";
import { useI18n, type TranslationKey } from "../../app/i18n";
import { useCalendarAvailabilityQuery } from "./queries";

type AvailabilityParticipant =
  CalendarAvailabilityQueryResponse["participants"][number];
type AvailabilitySuggestion =
  CalendarAvailabilityQueryResponse["suggestions"][number];
type SearchHorizon = 7 | 14 | 31;
type SearchStep = 15 | 30 | 60;

interface SchedulingAssistantPanelProps {
  audience: SessionAudience;
  classID: string;
  endsAt: string;
  hourCycle: "h12" | "h23";
  locale: string;
  onUseSuggestion?: (startsAt: string, endsAt: string) => void;
  primaryTimezone: string;
  secondaryTimezone: string | null;
  startsAt: string;
  tenantID: string;
  userID: string;
}

const availabilityStatuses: readonly CalendarAvailabilityStatus[] = [
  "free",
  "tentative",
  "unknown",
  "busy",
  "out_of_office",
];

const statusKeys: Record<CalendarAvailabilityStatus, TranslationKey> = {
  busy: "calendar.schedulingAssistant.status.busy",
  free: "calendar.schedulingAssistant.status.free",
  out_of_office: "calendar.schedulingAssistant.status.outOfOffice",
  tentative: "calendar.schedulingAssistant.status.tentative",
  unknown: "calendar.schedulingAssistant.status.unknown",
};

const reasonKeys = {
  optional_busy: "calendar.schedulingAssistant.reason.optionalBusy",
  optional_out_of_office:
    "calendar.schedulingAssistant.reason.optionalOutOfOffice",
  optional_outside_working:
    "calendar.schedulingAssistant.reason.optionalOutsideWorking",
  optional_tentative: "calendar.schedulingAssistant.reason.optionalTentative",
  optional_unknown: "calendar.schedulingAssistant.reason.optionalUnknown",
  required_busy: "calendar.schedulingAssistant.reason.requiredBusy",
  required_out_of_office:
    "calendar.schedulingAssistant.reason.requiredOutOfOffice",
  required_outside_working:
    "calendar.schedulingAssistant.reason.requiredOutsideWorking",
  required_tentative: "calendar.schedulingAssistant.reason.requiredTentative",
  required_unknown: "calendar.schedulingAssistant.reason.requiredUnknown",
} as const satisfies Record<
  keyof AvailabilitySuggestion["reason_breakdown"],
  TranslationKey
>;

function abbreviatedUserID(userID: string) {
  return userID.slice(-8);
}

function sessionDurationMinutes(startsAt: string, endsAt: string) {
  const duration = Math.ceil(
    (Date.parse(endsAt) - Date.parse(startsAt)) / 60_000,
  );
  return Number.isFinite(duration) ? duration : 0;
}

function searchRange(
  startsAt: string,
  timezone: string,
  horizon: SearchHorizon,
) {
  try {
    const start = Temporal.Instant.from(startsAt)
      .toZonedDateTimeISO(timezone)
      .startOfDay();
    return {
      from: start.toInstant().toString(),
      to: start.add({ days: horizon }).toInstant().toString(),
    };
  } catch {
    return null;
  }
}

function participantLabel(
  participant: AvailabilityParticipant,
  userID: string,
  t: ReturnType<typeof useI18n>["t"],
) {
  if (participant.participant.id === userID) {
    return t("calendar.participation.you");
  }
  return t("calendar.participation.internalUser", {
    id: abbreviatedUserID(participant.participant.id),
  });
}

function participantStatusCounts(participant: AvailabilityParticipant) {
  const counts = new Map<CalendarAvailabilityStatus, number>();
  participant.intervals.forEach((interval) => {
    counts.set(interval.status, (counts.get(interval.status) ?? 0) + 1);
  });
  return counts;
}

function TimeValue({
  endsAt,
  formatter,
  startsAt,
}: {
  endsAt: string;
  formatter: Intl.DateTimeFormat;
  startsAt: string;
}) {
  return (
    <>
      <time dateTime={startsAt}>{formatter.format(new Date(startsAt))}</time>
      <span aria-hidden="true"> – </span>
      <time dateTime={endsAt}>{formatter.format(new Date(endsAt))}</time>
    </>
  );
}

export function SchedulingAssistantPanel({
  audience,
  classID,
  endsAt,
  hourCycle,
  locale,
  onUseSuggestion,
  primaryTimezone,
  secondaryTimezone,
  startsAt,
  tenantID,
  userID,
}: SchedulingAssistantPanelProps) {
  const { t } = useI18n();
  const [horizon, setHorizon] = useState<SearchHorizon>(7);
  const [step, setStep] = useState<SearchStep>(30);
  const availability = useCalendarAvailabilityQuery(tenantID);
  const resetAvailability = availability.reset;
  const durationMinutes = useMemo(
    () => sessionDurationMinutes(startsAt, endsAt),
    [endsAt, startsAt],
  );
  const participants = audience.attendees;
  const range = useMemo(
    () => searchRange(startsAt, primaryTimezone, horizon),
    [horizon, primaryTimezone, startsAt],
  );
  const primaryFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        dateStyle: "medium",
        hour12: hourCycle === "h12",
        timeStyle: "short",
        timeZone: primaryTimezone,
      }),
    [hourCycle, locale, primaryTimezone],
  );
  const secondaryFormatter = useMemo(
    () =>
      secondaryTimezone
        ? new Intl.DateTimeFormat(locale, {
            dateStyle: "medium",
            hour12: hourCycle === "h12",
            timeStyle: "short",
            timeZone: secondaryTimezone,
          })
        : null,
    [hourCycle, locale, secondaryTimezone],
  );
  const request = useMemo<CalendarAvailabilityQueryRequest | null>(() => {
    if (
      !range ||
      durationMinutes < 15 ||
      durationMinutes > 480 ||
      participants.length === 0
    ) {
      return null;
    }
    return {
      class_id: classID,
      duration_minutes: durationMinutes,
      from: range.from,
      max_candidates: 10,
      optional: participants
        .filter((attendee) => attendee.participation_role === "optional")
        .map((attendee) => ({
          id: attendee.user_id,
          kind: "internal_user" as const,
        })),
      required: participants
        .filter((attendee) => attendee.participation_role === "required")
        .map((attendee) => ({
          id: attendee.user_id,
          kind: "internal_user" as const,
        })),
      step_minutes: step,
      timezone: primaryTimezone,
      to: range.to,
    };
  }, [classID, durationMinutes, participants, primaryTimezone, range, step]);

  useEffect(() => {
    resetAvailability();
  }, [
    audience.audience_revision,
    classID,
    endsAt,
    horizon,
    primaryTimezone,
    resetAvailability,
    step,
    startsAt,
    tenantID,
    userID,
  ]);

  if (!audience.viewer_access.can_manage_attendees) {
    return null;
  }

  const runSearch = (event?: FormEvent) => {
    event?.preventDefault();
    if (request) {
      availability.mutate(request);
    }
  };

  return (
    <section
      aria-labelledby="calendar-scheduling-assistant-title"
      className="calendar-scheduling-assistant"
    >
      <div className="calendar-scheduling-assistant__heading">
        <div>
          <h4 id="calendar-scheduling-assistant-title">
            {t("calendar.schedulingAssistant.title")}
          </h4>
          <p>{t("calendar.schedulingAssistant.description")}</p>
        </div>
        <span>{t("calendar.schedulingAssistant.private")}</span>
      </div>

      <form
        aria-label={t("calendar.schedulingAssistant.searchForm")}
        className="calendar-scheduling-assistant__controls"
        onSubmit={runSearch}
      >
        <label>
          {t("calendar.schedulingAssistant.range")}
          <select
            onChange={(event) => {
              const nextHorizon = Number(event.target.value) as SearchHorizon;
              setHorizon(nextHorizon);
              if (nextHorizon === 31 && step === 15) {
                setStep(30);
              }
            }}
            value={horizon}
          >
            <option value={7}>
              {t("calendar.schedulingAssistant.rangeDays", { count: 7 })}
            </option>
            <option value={14}>
              {t("calendar.schedulingAssistant.rangeDays", { count: 14 })}
            </option>
            <option value={31}>
              {t("calendar.schedulingAssistant.rangeDays", { count: 31 })}
            </option>
          </select>
        </label>
        <label>
          {t("calendar.schedulingAssistant.step")}
          <select
            onChange={(event) =>
              setStep(Number(event.target.value) as SearchStep)
            }
            value={step}
          >
            <option disabled={horizon === 31} value={15}>
              {t("calendar.schedulingAssistant.minutes", { count: 15 })}
            </option>
            <option value={30}>
              {t("calendar.schedulingAssistant.minutes", { count: 30 })}
            </option>
            <option value={60}>
              {t("calendar.schedulingAssistant.minutes", { count: 60 })}
            </option>
          </select>
        </label>
        <div className="calendar-scheduling-assistant__search">
          <span>
            {t("calendar.schedulingAssistant.duration", {
              count: durationMinutes,
            })}
          </span>
          <Button
            disabled={!request}
            leadingIcon={<Search />}
            loading={availability.isPending}
            loadingLabel={t("calendar.schedulingAssistant.searching")}
            size="sm"
            type="submit"
          >
            {t("calendar.schedulingAssistant.search")}
          </Button>
        </div>
      </form>

      {!request && (
        <p className="calendar-scheduling-assistant__notice" role="status">
          {participants.length === 0
            ? t("calendar.schedulingAssistant.noParticipants")
            : t("calendar.schedulingAssistant.invalidDuration")}
        </p>
      )}

      {availability.isPending && (
        <p aria-live="polite" className="calendar-scheduling-assistant__notice">
          {t("calendar.schedulingAssistant.searching")}
        </p>
      )}

      {availability.isError && (
        <div className="calendar-scheduling-assistant__feedback" role="alert">
          <p>{t("calendar.schedulingAssistant.error")}</p>
          <Button
            leadingIcon={<RefreshCw />}
            onClick={() => runSearch()}
            size="sm"
            variant="secondary"
          >
            {t("calendar.retry")}
          </Button>
        </div>
      )}

      {availability.isSuccess && (
        <div className="calendar-scheduling-assistant__results">
          <p aria-live="polite" className="visually-hidden">
            {t("calendar.schedulingAssistant.resultCount", {
              count: availability.data.suggestions.length,
            })}
          </p>

          <div className="calendar-scheduling-assistant__timezone">
            <strong>{t("calendar.schedulingAssistant.primaryTimezone")}</strong>
            <span>{primaryTimezone}</span>
            {secondaryTimezone && (
              <>
                <strong>
                  {t("calendar.schedulingAssistant.secondaryTimezone")}
                </strong>
                <span>{secondaryTimezone}</span>
              </>
            )}
          </div>

          <details>
            <summary>
              {t("calendar.schedulingAssistant.participantSummary", {
                count: availability.data.participants.length,
              })}
            </summary>
            <div className="calendar-scheduling-assistant__table-scroll">
              <table>
                <caption className="visually-hidden">
                  {t("calendar.schedulingAssistant.participantTable")}
                </caption>
                <thead>
                  <tr>
                    <th scope="col">
                      {t("calendar.schedulingAssistant.participant")}
                    </th>
                    <th scope="col">
                      {t("calendar.schedulingAssistant.role")}
                    </th>
                    <th scope="col">
                      {t("calendar.schedulingAssistant.availability")}
                    </th>
                    <th scope="col">
                      {t("calendar.schedulingAssistant.workingHours")}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {availability.data.participants.map((participant) => {
                    const statusCounts = participantStatusCounts(participant);
                    return (
                      <tr
                        key={`${participant.participant.kind}:${participant.participant.id}`}
                      >
                        <th scope="row">
                          {participantLabel(participant, userID, t)}
                        </th>
                        <td>
                          {participant.role === "required"
                            ? t("calendar.participation.role.required")
                            : t("calendar.participation.role.optional")}
                        </td>
                        <td>
                          <ul className="calendar-scheduling-assistant__statuses">
                            {statusCounts.size === 0 ? (
                              <li className="calendar-scheduling-assistant__status calendar-scheduling-assistant__status--free">
                                {t(
                                  "calendar.schedulingAssistant.noBlockingIntervals",
                                )}
                              </li>
                            ) : (
                              availabilityStatuses
                                .filter((status) => statusCounts.has(status))
                                .map((status) => (
                                  <li
                                    className={`calendar-scheduling-assistant__status calendar-scheduling-assistant__status--${status}`}
                                    key={status}
                                  >
                                    {t(
                                      "calendar.schedulingAssistant.statusCount",
                                      {
                                        count: statusCounts.get(status) ?? 0,
                                        status: t(statusKeys[status]),
                                      },
                                    )}
                                  </li>
                                ))
                            )}
                          </ul>
                        </td>
                        <td>
                          {participant.working_intervals.length > 0 ? (
                            <details className="calendar-scheduling-assistant__working-details">
                              <summary>
                                {t(
                                  "calendar.schedulingAssistant.workingIntervalCount",
                                  {
                                    count: participant.working_intervals.length,
                                  },
                                )}
                              </summary>
                              <ul>
                                {participant.working_intervals.map(
                                  (interval) => (
                                    <li
                                      key={`${interval.starts_at}:${interval.ends_at}`}
                                    >
                                      <TimeValue
                                        endsAt={interval.ends_at}
                                        formatter={primaryFormatter}
                                        startsAt={interval.starts_at}
                                      />
                                    </li>
                                  ),
                                )}
                              </ul>
                            </details>
                          ) : (
                            t("calendar.schedulingAssistant.noWorkingIntervals")
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </details>

          {availability.data.suggestions.length === 0 ? (
            <div className="calendar-scheduling-assistant__empty" role="status">
              <strong>{t("calendar.schedulingAssistant.emptyTitle")}</strong>
              <p>{t("calendar.schedulingAssistant.emptyDescription")}</p>
            </div>
          ) : (
            <div className="calendar-scheduling-assistant__table-scroll">
              <table>
                <caption>
                  {t("calendar.schedulingAssistant.suggestionTable")}
                </caption>
                <thead>
                  <tr>
                    <th scope="col">
                      {t("calendar.schedulingAssistant.primaryTime")}
                    </th>
                    {secondaryFormatter && secondaryTimezone && (
                      <th scope="col">
                        {t("calendar.schedulingAssistant.secondaryTime")}
                      </th>
                    )}
                    <th scope="col">
                      {t("calendar.schedulingAssistant.reasons")}
                    </th>
                    {onUseSuggestion && (
                      <th scope="col">
                        <span className="visually-hidden">
                          {t("calendar.schedulingAssistant.actions")}
                        </span>
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody>
                  {availability.data.suggestions.map((suggestion) => {
                    const reasons = (
                      Object.entries(suggestion.reason_breakdown) as [
                        keyof AvailabilitySuggestion["reason_breakdown"],
                        number,
                      ][]
                    ).filter(([, count]) => count > 0);
                    return (
                      <tr key={suggestion.stable_slot_key}>
                        <td>
                          <TimeValue
                            endsAt={suggestion.ends_at}
                            formatter={primaryFormatter}
                            startsAt={suggestion.starts_at}
                          />
                          <small>{primaryTimezone}</small>
                        </td>
                        {secondaryFormatter && secondaryTimezone && (
                          <td>
                            <TimeValue
                              endsAt={suggestion.ends_at}
                              formatter={secondaryFormatter}
                              startsAt={suggestion.starts_at}
                            />
                            <small>{secondaryTimezone}</small>
                          </td>
                        )}
                        <td>
                          {reasons.length === 0 ? (
                            <span className="calendar-scheduling-assistant__clear">
                              {t("calendar.schedulingAssistant.noConflicts")}
                            </span>
                          ) : (
                            <ul className="calendar-scheduling-assistant__reasons">
                              {reasons.map(([reason, count]) => (
                                <li key={reason}>
                                  {t(reasonKeys[reason], { count })}
                                </li>
                              ))}
                            </ul>
                          )}
                        </td>
                        {onUseSuggestion && (
                          <td>
                            <Button
                              onClick={() =>
                                onUseSuggestion(
                                  suggestion.starts_at,
                                  suggestion.ends_at,
                                )
                              }
                              size="sm"
                              variant="secondary"
                            >
                              {t("calendar.schedulingAssistant.useTime")}
                            </Button>
                          </td>
                        )}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
