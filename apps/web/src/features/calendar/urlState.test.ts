import { describe, expect, it } from "vitest";
import {
  calendarRouteSearch,
  parseCalendarRouteState,
  withCalendarFilters,
} from "./urlState";

const defaults = { date: "2026-07-26", view: "week" as const };

describe("calendar URL state", () => {
  it("uses bounded defaults for invalid view and date values", () => {
    const state = parseCalendarRouteState(
      new URLSearchParams("view=timeline&date=2026-02-31"),
      defaults,
    );

    expect(state).toEqual({
      date: "2026-07-26",
      filters: {
        classIDs: [],
        search: "",
        statuses: [],
        types: ["class_session"],
      },
      view: "week",
    });
  });

  it("normalizes filters into stable query-string order", () => {
    const state = parseCalendarRouteState(
      new URLSearchParams(
        "view=agenda&date=2026-07-27&classes=z,a,z&statuses=scheduled,cancelled&types=class_session&search=%20math%20",
      ),
      defaults,
    );

    expect(calendarRouteSearch(state).toString()).toBe(
      "view=agenda&date=2026-07-27&search=math&classes=a%2Cz&statuses=cancelled%2Cscheduled",
    );
  });

  it("updates filters without mutating the remaining route state", () => {
    const state = parseCalendarRouteState(new URLSearchParams(), defaults);

    expect(withCalendarFilters(state, { search: "physics" })).toEqual({
      ...state,
      filters: { ...state.filters, search: "physics" },
    });
  });
});
