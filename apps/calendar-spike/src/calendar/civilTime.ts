import { Temporal } from "temporal-polyfill";

export type CivilDisambiguation = "compatible" | "earlier" | "later" | "reject";

export interface CivilDateTimeInput {
  local: string;
  timeZone: string;
  disambiguation?: CivilDisambiguation;
}

export interface ResolvedCivilDateTime {
  instant: string;
  offset: string;
  timeZone: string;
  local: string;
}

/**
 * Resolves a local wall-clock value without ever relying on the host machine's
 * timezone. `reject` is used by the API boundary for DST gaps; `earlier` and
 * `later` make an overlap choice explicit.
 */
export function resolveCivilDateTime(
  input: CivilDateTimeInput,
): ResolvedCivilDateTime {
  const disambiguation = input.disambiguation ?? "reject";
  const zoned = Temporal.ZonedDateTime.from(
    `${input.local}[${input.timeZone}]`,
    { disambiguation },
  );

  return {
    instant: zoned.toInstant().toString(),
    offset: zoned.offset,
    timeZone: zoned.timeZoneId,
    local: zoned.toPlainDateTime().toString(),
  };
}

export function addMinutesToInstant(instant: string, minutes: number): string {
  return Temporal.Instant.from(instant).add({ minutes }).toString();
}
