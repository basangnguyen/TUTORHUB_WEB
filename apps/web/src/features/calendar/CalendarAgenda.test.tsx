import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import { I18nProvider } from "../../app/i18n";
import type { CalendarItemViewModel } from "./model";
import { CalendarAgenda } from "./CalendarAgenda";

const item: CalendarItemViewModel = {
  allDay: false,
  canCancel: false,
  canEdit: true,
  canReschedule: true,
  canView: true,
  classID: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
  classTitle: "Advanced Mathematics",
  colorToken: "class-accent-1",
  displayTimezone: "Asia/Ho_Chi_Minh",
  endsAt: "2026-07-27T03:00:00Z",
  id: "class-session:session-1:2026-07-27T02:00:00Z",
  occurrenceKey: "2026-07-27T02:00:00Z",
  sourceID: "session-1",
  sourceType: "class_session",
  startsAt: "2026-07-27T02:00:00Z",
  status: "scheduled",
  title: "Linear algebra",
  version: 3,
};

describe("CalendarAgenda", () => {
  afterEach(cleanup);

  it("announces semantic status, class and secondary timezone", () => {
    render(
      <MemoryRouter>
        <I18nProvider initialLanguage="en">
          <CalendarAgenda
            hourCycle="h23"
            items={[item]}
            locale="en-US"
            secondaryTimezone="Europe/London"
            timezone="Asia/Ho_Chi_Minh"
          />
        </I18nProvider>
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: "Linear algebra" }),
    ).toBeVisible();
    expect(screen.getByText("Scheduled")).toBeVisible();
    expect(screen.getByText(/Advanced Mathematics/)).toBeVisible();
    expect(screen.getByText(/Secondary Europe\/London/)).toBeVisible();
    expect(
      screen.getByRole("link", { name: /Manage in class/ }),
    ).toHaveAttribute(
      "href",
      `/app/classrooms/${item.classID}#class-session-heading`,
    );
  });

  it("uses text and decoration semantics for cancelled sessions", () => {
    render(
      <MemoryRouter>
        <I18nProvider initialLanguage="en">
          <CalendarAgenda
            hourCycle="h12"
            items={[{ ...item, canEdit: false, status: "cancelled" }]}
            locale="en-US"
            secondaryTimezone={null}
            timezone="Asia/Ho_Chi_Minh"
          />
        </I18nProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText("Cancelled")).toBeVisible();
    expect(screen.getByRole("listitem")).toHaveClass(
      "calendar-agenda-item--cancelled",
    );
  });

  it("keeps the viewer-local civil date for extreme positive offsets", () => {
    render(
      <MemoryRouter>
        <I18nProvider initialLanguage="en">
          <CalendarAgenda
            hourCycle="h23"
            items={[
              {
                ...item,
                endsAt: "2026-07-31T13:30:00Z",
                startsAt: "2026-07-31T12:30:00Z",
              },
            ]}
            locale="en-US"
            secondaryTimezone={null}
            timezone="Pacific/Kiritimati"
          />
        </I18nProvider>
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("heading", { name: /August 1, 2026/ }),
    ).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: /August 2, 2026/ }),
    ).not.toBeInTheDocument();
  });
});
