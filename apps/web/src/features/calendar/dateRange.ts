import { Temporal } from "temporal-polyfill";
import type { CalendarRange, CalendarView } from "./model";

export function calendarToday(timezone: string) {
  try {
    return Temporal.Now.zonedDateTimeISO(timezone).toPlainDate().toString();
  } catch {
    return Temporal.Now.plainDateISO().toString();
  }
}

function parseDate(value: string) {
  try {
    return Temporal.PlainDate.from(value);
  } catch {
    return Temporal.Now.plainDateISO();
  }
}

function normalizedWeekday(firstDayOfWeek: 0 | 1) {
  return firstDayOfWeek === 0 ? 7 : firstDayOfWeek;
}

function startOfWeek(date: Temporal.PlainDate, firstDayOfWeek: 0 | 1) {
  const weekday = normalizedWeekday(firstDayOfWeek);
  return date.subtract({ days: (date.dayOfWeek - weekday + 7) % 7 });
}

function toInstant(date: Temporal.PlainDate, timezone: string) {
  return date.toZonedDateTime({ timeZone: timezone }).toInstant().toString();
}

export function calendarRangeForView(
  dateValue: string,
  view: CalendarView,
  timezone: string,
  firstDayOfWeek: 0 | 1,
): CalendarRange {
  const date = parseDate(dateValue);
  let fromDate = date;
  let toDate = date.add({ days: 1 });

  if (view === "week" || view === "work_week") {
    fromDate = startOfWeek(date, firstDayOfWeek);
    toDate = fromDate.add({ days: 7 });
  } else if (view === "month") {
    fromDate = startOfWeek(date.with({ day: 1 }), firstDayOfWeek);
    toDate = fromDate.add({ days: 42 });
  } else if (view === "agenda") {
    fromDate = date;
    toDate = date.add({ days: 28 });
  }

  return {
    from: toInstant(fromDate, timezone),
    to: toInstant(toDate, timezone),
  };
}

export function moveCalendarDate(
  dateValue: string,
  view: CalendarView,
  direction: -1 | 1,
) {
  const date = parseDate(dateValue);
  if (view === "month") {
    return date.add({ months: direction }).toString();
  }
  if (view === "week" || view === "work_week") {
    return date.add({ days: direction * 7 }).toString();
  }
  if (view === "agenda") {
    return date.add({ days: direction * 28 }).toString();
  }
  return date.add({ days: direction }).toString();
}

export function calendarRangeTitle(
  dateValue: string,
  view: CalendarView,
  timezone: string,
  locale: string,
  firstDayOfWeek: 0 | 1,
) {
  const range = calendarRangeForView(dateValue, view, timezone, firstDayOfWeek);
  const from = new Date(range.from);
  const inclusiveEnd = new Date(
    Temporal.Instant.from(range.to).subtract({ nanoseconds: 1 }).toString(),
  );
  const formatter = new Intl.DateTimeFormat(locale, {
    day: "numeric",
    month: "long",
    timeZone: timezone,
    year: "numeric",
  });
  if (view === "day") {
    return formatter.format(from);
  }
  return `${formatter.format(from)} – ${formatter.format(inclusiveEnd)}`;
}

export function miniMonthDays(dateValue: string, firstDayOfWeek: 0 | 1) {
  const selected = parseDate(dateValue);
  const monthStart = selected.with({ day: 1 });
  const gridStart = startOfWeek(monthStart, firstDayOfWeek);
  return Array.from({ length: 42 }, (_, index) => {
    const date = gridStart.add({ days: index });
    return {
      date: date.toString(),
      day: date.day,
      inMonth: date.month === selected.month && date.year === selected.year,
      selected: date.equals(selected),
    };
  });
}
