import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type { CalendarWorkingSchedule } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import { WorkingScheduleDrawer } from "./WorkingScheduleDrawer";

const schedule: CalendarWorkingSchedule = {
  exceptions: [],
  source: "user_override",
  timezone: "Asia/Ho_Chi_Minh",
  updated_at: "2026-07-28T08:00:00Z",
  version: 4,
  weekly_intervals: [
    { ends_at: "12:00", starts_at: "08:00", weekday: "monday" },
    { ends_at: "17:00", starts_at: "13:00", weekday: "monday" },
  ],
};

describe("WorkingScheduleDrawer", () => {
  afterEach(cleanup);

  it("submits a full CAS replacement with multiple intervals", async () => {
    const onSave = vi.fn().mockResolvedValue({ ...schedule, version: 5 });
    render(
      <I18nProvider initialLanguage="en">
        <WorkingScheduleDrawer
          error={null}
          loading={false}
          onOpenChange={vi.fn()}
          onReload={vi.fn()}
          onSave={onSave}
          open
          pending={false}
          schedule={schedule}
        />
      </I18nProvider>,
    );

    const monday = screen.getByRole("group", { name: "Monday" });
    const starts = within(monday).getAllByLabelText("Monday Starts");
    const secondStart = starts.at(1);
    if (!secondStart) {
      throw new Error("expected the second Monday working interval");
    }
    fireEvent.change(secondStart, { target: { value: "13:30" } });
    fireEvent.click(screen.getByRole("button", { name: "Add exception" }));
    fireEvent.change(screen.getByLabelText("Date"), {
      target: { value: "2026-09-02" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save working hours" }));

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        expected_version: 4,
        timezone: "Asia/Ho_Chi_Minh",
        exceptions: [{ date: "2026-09-02", intervals: [], kind: "holiday" }],
        weekly_intervals: [
          { ends_at: "12:00", starts_at: "08:00", weekday: "monday" },
          { ends_at: "17:00", starts_at: "13:30", weekday: "monday" },
        ],
      }),
    );
    expect(await screen.findByText("Working hours saved.")).toBeVisible();
  });

  it("rejects overlapping intervals before calling the API", () => {
    const onSave = vi.fn();
    render(
      <I18nProvider initialLanguage="en">
        <WorkingScheduleDrawer
          error={null}
          loading={false}
          onOpenChange={vi.fn()}
          onReload={vi.fn()}
          onSave={onSave}
          open
          pending={false}
          schedule={{
            ...schedule,
            weekly_intervals: [
              { ends_at: "12:00", starts_at: "08:00", weekday: "monday" },
              { ends_at: "13:00", starts_at: "11:00", weekday: "monday" },
            ],
          }}
        />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Save working hours" }));

    expect(onSave).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        "Check the timezone, dates, time order, and overlapping intervals.",
      ),
    ).toBeVisible();
  });
});
