import { Button, StatusBadge } from "@tutorhub/ui";
import { CalendarClock, ExternalLink, Eye } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { Temporal } from "temporal-polyfill";
import { useI18n, type TranslationKey } from "../../app/i18n";
import {
  stableCalendarItemOrder,
  type CalendarHourCycle,
  type CalendarItemViewModel,
} from "./model";

const initialVisibleItems = 24;

function dateKey(instant: string, timezone: string) {
  try {
    return Temporal.Instant.from(instant)
      .toZonedDateTimeISO(timezone)
      .toPlainDate()
      .toString();
  } catch {
    return instant.slice(0, 10);
  }
}

function statusTranslation(status: string): TranslationKey {
  if (status === "scheduled") {
    return "calendar.status.scheduled";
  }
  if (status === "cancelled") {
    return "calendar.status.cancelled";
  }
  return "calendar.status.other";
}

interface CalendarAgendaProps {
  hourCycle: CalendarHourCycle;
  items: readonly CalendarItemViewModel[];
  locale: string;
  onOpenItem?: (item: CalendarItemViewModel) => void;
  secondaryTimezone: string | null;
  timezone: string;
}

export function CalendarAgenda({
  hourCycle,
  items,
  locale,
  onOpenItem,
  secondaryTimezone,
  timezone,
}: CalendarAgendaProps) {
  const { t } = useI18n();
  const itemsKey = useMemo(
    () => items.map((item) => `${item.id}:${item.version}`).join("|"),
    [items],
  );
  const [pagination, setPagination] = useState({
    itemsKey: "",
    visibleCount: initialVisibleItems,
  });
  const visibleCount =
    pagination.itemsKey === itemsKey
      ? pagination.visibleCount
      : initialVisibleItems;
  const visibleItems = useMemo(
    () => [...items].sort(stableCalendarItemOrder).slice(0, visibleCount),
    [items, visibleCount],
  );
  const groups = useMemo(() => {
    const result = new Map<string, CalendarItemViewModel[]>();
    for (const item of visibleItems) {
      const key = dateKey(item.startsAt, timezone);
      result.set(key, [...(result.get(key) ?? []), item]);
    }
    return [...result];
  }, [timezone, visibleItems]);
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        dateStyle: "full",
        // Group keys are already viewer-local PlainDate values. Format them in
        // UTC so extreme positive/negative offsets cannot move the label to a
        // neighbouring civil date.
        timeZone: "UTC",
      }),
    [locale],
  );
  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        hour: "numeric",
        hour12: hourCycle === "h12",
        minute: "2-digit",
        timeZone: timezone,
      }),
    [hourCycle, locale, timezone],
  );
  const secondaryFormatter = useMemo(
    () =>
      secondaryTimezone
        ? new Intl.DateTimeFormat(locale, {
            hour: "numeric",
            hour12: hourCycle === "h12",
            minute: "2-digit",
            timeZone: secondaryTimezone,
          })
        : null,
    [hourCycle, locale, secondaryTimezone],
  );

  return (
    <section
      aria-labelledby="calendar-agenda-heading"
      className="calendar-agenda"
    >
      <div className="calendar-agenda__heading">
        <h2 id="calendar-agenda-heading">{t("calendar.agendaTitle")}</h2>
        <p aria-live="polite">
          {t("calendar.agendaCount", { count: visibleItems.length })}
        </p>
      </div>

      <p className="calendar-agenda__timezone">
        {t("calendar.viewerTimezone", { timezone })}
      </p>

      {groups.map(([date, dateItems]) => (
        <section className="calendar-agenda__day" key={date}>
          <h3>{dateFormatter.format(new Date(`${date}T12:00:00Z`))}</h3>
          <ol>
            {dateItems.map((item) => {
              const cancelled = item.status === "cancelled";
              const secondaryTime = secondaryFormatter
                ? `${secondaryFormatter.format(new Date(item.startsAt))} – ${secondaryFormatter.format(
                    new Date(item.endsAt),
                  )}`
                : null;
              const classLink = item.classID
                ? `/app/classrooms/${item.classID}#class-session-heading`
                : null;
              return (
                <li
                  className={`calendar-agenda-item${
                    cancelled ? " calendar-agenda-item--cancelled" : ""
                  }`}
                  key={item.id}
                >
                  <span
                    aria-hidden="true"
                    className="calendar-agenda-item__marker"
                    data-color-token={item.colorToken}
                  />
                  <div className="calendar-agenda-item__time">
                    <time dateTime={item.startsAt}>
                      {timeFormatter.format(new Date(item.startsAt))}
                    </time>
                    <span aria-hidden="true">–</span>
                    <time dateTime={item.endsAt}>
                      {timeFormatter.format(new Date(item.endsAt))}
                    </time>
                  </div>
                  <div className="calendar-agenda-item__body">
                    <div className="calendar-agenda-item__title-row">
                      <h4>{item.title}</h4>
                      <StatusBadge tone={cancelled ? "danger" : "info"}>
                        {t(statusTranslation(item.status), {
                          status: item.status,
                        })}
                      </StatusBadge>
                    </div>
                    <p>
                      <CalendarClock aria-hidden="true" />
                      {t("calendar.sessionType")} ·{" "}
                      {item.classTitle || t("calendar.classFallback")}
                    </p>
                    {secondaryTime && secondaryTimezone && (
                      <small>
                        {t("calendar.secondaryTimezone", {
                          time: secondaryTime,
                          timezone: secondaryTimezone,
                        })}
                      </small>
                    )}
                  </div>
                  {((item.canView && onOpenItem) || classLink) && (
                    <div className="calendar-agenda-item__actions">
                      {item.canView && onOpenItem && (
                        <Button
                          leadingIcon={<Eye />}
                          onClick={() => onOpenItem(item)}
                          size="sm"
                          variant="secondary"
                        >
                          {t("calendar.openSessionDetails")}
                        </Button>
                      )}
                      {classLink && (
                        <Link to={classLink}>
                          <ExternalLink aria-hidden="true" />
                          {item.canEdit || item.canCancel
                            ? t("calendar.manageInClass")
                            : t("calendar.openClass")}
                        </Link>
                      )}
                    </div>
                  )}
                </li>
              );
            })}
          </ol>
        </section>
      ))}

      {visibleCount < items.length && (
        <Button
          onClick={() =>
            setPagination({
              itemsKey,
              visibleCount: visibleCount + initialVisibleItems,
            })
          }
          variant="secondary"
        >
          {t("calendar.loadMore")}
        </Button>
      )}
    </section>
  );
}
