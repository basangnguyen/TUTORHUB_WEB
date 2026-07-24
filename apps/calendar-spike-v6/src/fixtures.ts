import type { EventInput } from "@fullcalendar/core";

const FIXTURE_COUNT_OPTIONS = new Set([500, 1000, 2000]);

export function requestedFixtureCount(search: string): number {
  const raw = new URLSearchParams(search).get("events");
  const count = Number(raw);
  return FIXTURE_COUNT_OPTIONS.has(count) ? count : 48;
}

export function makeFixtureEvents(count: number): EventInput[] {
  const start = Date.parse("2026-07-20T07:00:00.000Z");
  return Array.from({ length: count }, (_, index) => {
    const dayOffset = index % 7;
    const slot = Math.floor(index / 7) % 12;
    const startsAt = start + dayOffset * 86_400_000 + slot * 1_800_000;
    return {
      id: `fixture-${String(index).padStart(4, "0")}`,
      title: `Lớp học mẫu ${index + 1}`,
      start: new Date(startsAt).toISOString(),
      end: new Date(startsAt + 1_800_000).toISOString(),
    };
  });
}
