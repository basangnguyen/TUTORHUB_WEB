import { describe, expect, it } from "vitest";
import {
  calendarRangeForView,
  miniMonthDays,
  moveCalendarDate,
} from "./dateRange";

describe("calendar date ranges", () => {
  it("uses civil boundaries across a daylight-saving transition", () => {
    const range = calendarRangeForView(
      "2026-03-08",
      "day",
      "America/New_York",
      0,
    );

    expect(range).toEqual({
      from: "2026-03-08T05:00:00Z",
      to: "2026-03-09T04:00:00Z",
    });
  });

  it("bounds month queries to a six-week calendar grid", () => {
    const range = calendarRangeForView(
      "2026-07-26",
      "month",
      "Asia/Ho_Chi_Minh",
      1,
    );

    expect(range.from).toBe("2026-06-28T17:00:00Z");
    expect(range.to).toBe("2026-08-09T17:00:00Z");
    expect(miniMonthDays("2026-07-26", 1)).toHaveLength(42);
  });

  it("moves each URL view by its explicit range", () => {
    expect(moveCalendarDate("2026-07-26", "day", 1)).toBe("2026-07-27");
    expect(moveCalendarDate("2026-07-26", "week", -1)).toBe("2026-07-19");
    expect(moveCalendarDate("2026-07-26", "agenda", 1)).toBe("2026-08-23");
    expect(moveCalendarDate("2026-07-26", "month", 1)).toBe("2026-08-26");
  });
});
