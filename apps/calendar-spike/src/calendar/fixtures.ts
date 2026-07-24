import type { CalendarItem } from "./domain";

export const FIXTURE_TIME_ZONE = "Asia/Ho_Chi_Minh";
export const FIXTURE_DATE = "2026-07-23";

const HCM_START = "2026-07-23T08:00:00+07:00";
const HCM_END = "2026-07-23T09:00:00+07:00";

export const DST_FIXTURES: readonly CalendarItem[] = [
  {
    id: "ny-spring-gap",
    title: "New York DST gap (rejected civil time)",
    startsAt: "2026-03-08T07:30:00Z",
    endsAt: "2026-03-08T08:30:00Z",
    timeZone: "America/New_York",
    category: "class",
    status: "scheduled",
    version: 1,
  },
  {
    id: "ny-fall-earlier",
    title: "New York overlap · earlier",
    startsAt: "2026-11-01T05:30:00Z",
    endsAt: "2026-11-01T06:00:00Z",
    timeZone: "America/New_York",
    category: "class",
    status: "scheduled",
    version: 1,
  },
  {
    id: "ny-fall-later",
    title: "New York overlap · later",
    startsAt: "2026-11-01T06:30:00Z",
    endsAt: "2026-11-01T07:00:00Z",
    timeZone: "America/New_York",
    category: "study",
    status: "scheduled",
    version: 1,
  },
];

export function makeFixtureItems(count = 48): CalendarItem[] {
  const items: CalendarItem[] = [
    {
      id: "hcm-class-001",
      title: "Toán · Hà Nội / Hồ Chí Minh",
      startsAt: HCM_START,
      endsAt: HCM_END,
      timeZone: FIXTURE_TIME_ZONE,
      category: "class",
      status: "scheduled",
      version: 1,
    },
    {
      id: "hcm-conflict",
      title: "Mô phỏng 409 conflict (kéo để hoàn tác)",
      startsAt: "2026-07-23T09:30:00+07:00",
      endsAt: "2026-07-23T10:00:00+07:00",
      timeZone: FIXTURE_TIME_ZONE,
      category: "class",
      status: "conflict",
      version: 1,
    },
  ];

  const start = Date.parse("2026-07-20T07:00:00.000Z");
  for (let index = items.length; index < Math.max(2, count); index += 1) {
    const dayOffset = index % 7;
    const slot = Math.floor(index / 7) % 12;
    const startsAt = new Date(
      start + dayOffset * 86_400_000 + slot * 1_800_000,
    ).toISOString();
    const endsAt = new Date(
      start + dayOffset * 86_400_000 + slot * 1_800_000 + 1_800_000,
    ).toISOString();
    items.push({
      id: `fixture-${String(index).padStart(4, "0")}`,
      title: `Lớp học mẫu ${index + 1}`,
      startsAt,
      endsAt,
      timeZone: FIXTURE_TIME_ZONE,
      category: index % 3 === 0 ? "study" : "class",
      status: "scheduled",
      version: 1,
    });
  }
  return items.slice(0, Math.max(2, count));
}

export function requestedFixtureCount(search: string): number {
  const requested = new URLSearchParams(search).get("events");
  if (requested === "500" || requested === "1000" || requested === "2000") {
    return Number(requested);
  }
  return 48;
}
