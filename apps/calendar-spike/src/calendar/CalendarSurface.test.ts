import { describe, expect, it } from "vitest";
import { mutationFromSurfaceEvent, toSurfaceEvents } from "./CalendarSurface";

describe("renderer adapter boundary", () => {
  it("keeps FullCalendar event shape at the adapter boundary", () => {
    const events = toSurfaceEvents([
      {
        id: "session-1",
        title: "Lớp mẫu",
        startsAt: "2026-07-23T01:00:00.000Z",
        endsAt: "2026-07-23T02:00:00.000Z",
        timeZone: "Asia/Ho_Chi_Minh",
        category: "class",
        status: "scheduled",
        version: 3,
      },
    ]);
    expect(events[0]).toMatchObject({
      id: "session-1",
      start: "2026-07-23T01:00:00.000Z",
      extendedProps: { timeZone: "Asia/Ho_Chi_Minh", version: 3 },
    });
  });

  it("rejects an incomplete event instead of inventing an end time", () => {
    expect(
      mutationFromSurfaceEvent(
        {
          id: "missing-end",
          start: new Date("2026-07-23T01:00:00.000Z"),
          end: null,
          extendedProps: { timeZone: "Asia/Ho_Chi_Minh", version: 1 },
        },
        "resize",
      ),
    ).toBeNull();
  });
});
