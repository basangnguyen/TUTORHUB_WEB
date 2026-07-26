import {
  emptyCalendarFilters,
  isCalendarView,
  type CalendarFilters,
  type CalendarRouteState,
  type CalendarView,
} from "./model";
import { Temporal } from "temporal-polyfill";

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

function isCalendarDate(value: string) {
  if (!datePattern.test(value)) {
    return false;
  }
  try {
    Temporal.PlainDate.from(value);
    return true;
  } catch {
    return false;
  }
}

function parseList(value: string | null) {
  if (!value) {
    return [];
  }
  return [
    ...new Set(
      value
        .split(",")
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  ].sort((left, right) => left.localeCompare(right));
}

function setList(
  params: URLSearchParams,
  name: string,
  values: readonly string[],
) {
  const normalized = [...new Set(values.filter(Boolean))].sort((left, right) =>
    left.localeCompare(right),
  );
  if (normalized.length > 0) {
    params.set(name, normalized.join(","));
  } else {
    params.delete(name);
  }
}

export interface CalendarRouteDefaults {
  date: string;
  view: CalendarView;
}

export function parseCalendarRouteState(
  params: URLSearchParams,
  defaults: CalendarRouteDefaults,
): CalendarRouteState {
  const requestedView = params.get("view");
  const requestedDate = params.get("date");
  return {
    date:
      requestedDate && isCalendarDate(requestedDate)
        ? requestedDate
        : defaults.date,
    filters: {
      search: (params.get("search") ?? "").trim().slice(0, 200),
      classIDs: parseList(params.get("classes")),
      statuses: parseList(params.get("statuses")),
      types: (() => {
        const types = parseList(params.get("types"));
        return types.length > 0 ? types : emptyCalendarFilters.types;
      })(),
    },
    view: isCalendarView(requestedView) ? requestedView : defaults.view,
  };
}

export function calendarRouteSearch(state: CalendarRouteState) {
  const params = new URLSearchParams();
  params.set("view", state.view);
  params.set("date", state.date);
  if (state.filters.search) {
    params.set("search", state.filters.search);
  }
  setList(params, "classes", state.filters.classIDs);
  setList(params, "statuses", state.filters.statuses);
  if (
    state.filters.types.length !== 1 ||
    state.filters.types[0] !== "class_session"
  ) {
    setList(params, "types", state.filters.types);
  }
  return params;
}

export function withCalendarFilters(
  state: CalendarRouteState,
  filters: Partial<CalendarFilters>,
): CalendarRouteState {
  return { ...state, filters: { ...state.filters, ...filters } };
}
