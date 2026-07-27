export const calendarViews = [
  "day",
  "work_week",
  "week",
  "month",
  "agenda",
] as const;

export type CalendarView = (typeof calendarViews)[number];
export type CalendarDensity = "comfortable" | "compact";
export type CalendarHourCycle = "h12" | "h23";
export type CalendarTimeScale = 15 | 30 | 60;

export interface CalendarFilters {
  search: string;
  classIDs: readonly string[];
  statuses: readonly string[];
  types: readonly string[];
}

export interface CalendarRouteState {
  date: string;
  filters: CalendarFilters;
  view: CalendarView;
}

export interface CalendarRange {
  from: string;
  to: string;
}

export interface CalendarItemViewModel {
  id: string;
  sourceType: string;
  sourceID: string;
  seriesID: string | null;
  occurrenceKey: string | null;
  title: string;
  startsAt: string;
  endsAt: string;
  allDay: boolean;
  displayTimezone: string;
  classID: string | null;
  classTitle: string | null;
  colorToken: string;
  status: string;
  version: number;
  canView: boolean;
  canEdit: boolean;
  canCancel: boolean;
  canReschedule: boolean;
}

export interface CalendarDisplayPreferenceViewModel {
  viewerTimezone: string;
  locale: string;
  hourCycle: CalendarHourCycle;
  weekStartsOn: 0 | 1;
  defaultView: CalendarView;
  density: CalendarDensity;
  timeScaleMinutes: CalendarTimeScale;
  secondaryTimezone: string | null;
  version: number;
  updatedAt: string;
}

export type CalendarDisplayPreferenceDraft = Omit<
  CalendarDisplayPreferenceViewModel,
  "updatedAt"
>;

export const emptyCalendarFilters: CalendarFilters = {
  search: "",
  classIDs: [],
  statuses: [],
  types: ["class_session"],
};

export function isCalendarView(value: string | null): value is CalendarView {
  return calendarViews.includes(value as CalendarView);
}

export function hasActiveCalendarFilters(filters: CalendarFilters) {
  return (
    filters.search.length > 0 ||
    filters.classIDs.length > 0 ||
    filters.statuses.length > 0 ||
    (filters.types.length > 0 &&
      !(filters.types.length === 1 && filters.types[0] === "class_session"))
  );
}

export function stableCalendarItemOrder(
  left: CalendarItemViewModel,
  right: CalendarItemViewModel,
) {
  return (
    left.startsAt.localeCompare(right.startsAt) ||
    left.sourceType.localeCompare(right.sourceType) ||
    (left.occurrenceKey ?? left.sourceID).localeCompare(
      right.occurrenceKey ?? right.sourceID,
    )
  );
}
