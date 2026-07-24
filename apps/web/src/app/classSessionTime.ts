import { Temporal } from "temporal-polyfill";

export type OverlapChoice = "earlier" | "later";

export type CivilTimeResolution =
  | { kind: "resolved"; value: string }
  | { kind: "overlap"; earlier: string; later: string }
  | { kind: "invalid"; reason: "invalid" | "gap" };

function serialize(zoned: Temporal.ZonedDateTime) {
  return zoned.toString({
    calendarName: "never",
    fractionalSecondDigits: "auto",
    timeZoneName: "never",
  });
}

export function resolveCivilDateTime(
  value: string,
  timezone: string,
  overlapChoice?: OverlapChoice,
): CivilTimeResolution {
  let plain: Temporal.PlainDateTime;
  try {
    plain = Temporal.PlainDateTime.from(value);
  } catch {
    return { kind: "invalid", reason: "invalid" };
  }

  const fields = {
    calendar: plain.calendarId,
    day: plain.day,
    hour: plain.hour,
    microsecond: plain.microsecond,
    millisecond: plain.millisecond,
    minute: plain.minute,
    month: plain.month,
    nanosecond: plain.nanosecond,
    second: plain.second,
    timeZone: timezone,
    year: plain.year,
  };

  try {
    const exact = Temporal.ZonedDateTime.from(fields, {
      disambiguation: "reject",
    });
    return { kind: "resolved", value: serialize(exact) };
  } catch {
    try {
      const earlier = Temporal.ZonedDateTime.from(fields, {
        disambiguation: "earlier",
      });
      const later = Temporal.ZonedDateTime.from(fields, {
        disambiguation: "later",
      });
      if (
        !earlier.toPlainDateTime().equals(plain) ||
        !later.toPlainDateTime().equals(plain)
      ) {
        return { kind: "invalid", reason: "gap" };
      }
      if (Temporal.ZonedDateTime.compare(earlier, later) === 0) {
        return { kind: "invalid", reason: "invalid" };
      }
      if (!overlapChoice) {
        return {
          kind: "overlap",
          earlier: serialize(earlier),
          later: serialize(later),
        };
      }
      return {
        kind: "resolved",
        value: serialize(overlapChoice === "earlier" ? earlier : later),
      };
    } catch {
      return { kind: "invalid", reason: "invalid" };
    }
  }
}

export function isOrderedSessionRange(startsAt: string, endsAt: string) {
  try {
    return (
      Temporal.Instant.compare(
        Temporal.Instant.from(startsAt),
        Temporal.Instant.from(endsAt),
      ) < 0
    );
  } catch {
    return false;
  }
}

export function civilInputFromInstant(instant: string, timezone: string) {
  try {
    return Temporal.Instant.from(instant)
      .toZonedDateTimeISO(timezone)
      .toPlainDateTime()
      .toString({ smallestUnit: "minute" });
  } catch {
    return "";
  }
}
