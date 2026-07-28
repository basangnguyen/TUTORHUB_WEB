import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import type { CalendarItemViewModel } from "./model";
import { SessionDetailDrawer } from "./SessionDetailDrawer";

const item: CalendarItemViewModel = {
  allDay: false,
  canCancel: false,
  canEdit: false,
  canReschedule: false,
  canView: true,
  classID: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
  classTitle: "Advanced Mathematics",
  colorToken: "class-accent-1",
  displayTimezone: "Asia/Ho_Chi_Minh",
  endsAt: "2026-07-27T03:00:00Z",
  id: "class-session:session-1:2026-07-27T02:00:00Z",
  occurrenceKey: "2026-07-27T02:00:00Z",
  sourceID: "session-1",
  seriesID: null,
  sourceType: "class_session",
  startsAt: "2026-07-27T02:00:00Z",
  status: "scheduled",
  title: "Linear algebra",
  version: 3,
};

describe("SessionDetailDrawer", () => {
  afterEach(cleanup);

  it("shows projection details without exposing edit to a read-only viewer", () => {
    render(
      <I18nProvider initialLanguage="en">
        <SessionDetailDrawer
          hourCycle="h23"
          item={item}
          locale="en-US"
          onClose={vi.fn()}
          onEdit={vi.fn()}
        />
      </I18nProvider>,
    );

    const drawer = screen.getByRole("dialog", { name: item.title });
    expect(within(drawer).getByText("Advanced Mathematics")).toBeVisible();
    expect(within(drawer).getByText("Scheduled")).toBeVisible();
    expect(
      within(drawer).getByText("Timezone: Asia/Ho_Chi_Minh"),
    ).toBeVisible();
    expect(
      within(drawer).queryByRole("button", { name: "Edit session" }),
    ).not.toBeInTheDocument();
  });

  it("offers edit only when the projection grants edit capability", () => {
    const onEdit = vi.fn();
    const editableItem = { ...item, canEdit: true };
    render(
      <I18nProvider initialLanguage="en">
        <SessionDetailDrawer
          hourCycle="h23"
          item={editableItem}
          locale="en-US"
          onClose={vi.fn()}
          onEdit={onEdit}
        />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Edit session" }));

    expect(onEdit).toHaveBeenCalledWith(editableItem);
  });
});
