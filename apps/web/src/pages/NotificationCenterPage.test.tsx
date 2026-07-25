import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useNotificationUnreadCount,
  useNotifications,
} from "../app/notifications";
import { SessionProvider } from "../app/session";
import { NotificationCenterPage } from "./NotificationCenterPage";
import type { CurrentUser, Notification } from "@tutorhub/api-client";

vi.mock("../app/notifications", () => ({
  useMarkAllNotificationsRead: vi.fn(),
  useMarkNotificationRead: vi.fn(),
  useNotificationUnreadCount: vi.fn(),
  useNotifications: vi.fn(),
}));

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const session: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role: "teacher",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: ["class.view"],
};

const readNotification: Notification = {
  id: "7f0af0b8-e168-4f37-84fb-2b3f76abcc9c",
  effect_key: "class_session_updated_7f0af0b8",
  template_key: "class_session.updated",
  resource_type: "class_session",
  resource_id: "2b9c25a4-b01b-4b5b-9e87-a782675ed511",
  context: { class_title: "Algorithms" },
  occurred_at: "2026-07-25T08:00:00Z",
  read_at: "2026-07-25T08:05:00Z",
  created_at: "2026-07-25T08:00:01Z",
};

describe("NotificationCenterPage", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("keeps mark-all available when unread rows exist beyond loaded pages", () => {
    const markAll = vi.fn();
    vi.mocked(useNotifications).mockReturnValue({
      data: {
        pages: [{ items: [readNotification], next_cursor: "next-page" }],
        pageParams: [undefined],
      },
      error: null,
      isError: false,
      isPending: false,
      isSuccess: true,
      isFetchNextPageError: false,
      isRefetching: false,
      isFetchingNextPage: false,
      hasNextPage: true,
      refetch: vi.fn(),
      fetchNextPage: vi.fn(),
    } as unknown as ReturnType<typeof useNotifications>);
    vi.mocked(useNotificationUnreadCount).mockReturnValue({
      data: { count: 3, is_capped: false },
    } as ReturnType<typeof useNotificationUnreadCount>);
    vi.mocked(useMarkNotificationRead).mockReturnValue({
      isError: false,
      isPending: false,
      mutate: vi.fn(),
    } as unknown as ReturnType<typeof useMarkNotificationRead>);
    vi.mocked(useMarkAllNotificationsRead).mockReturnValue({
      isError: false,
      isPending: false,
      mutate: markAll,
    } as unknown as ReturnType<typeof useMarkAllNotificationsRead>);

    render(
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: session }}>
          <MemoryRouter>
            <NotificationCenterPage />
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>,
    );

    const button = screen.getByRole("button", { name: "Mark all as read" });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    expect(markAll).toHaveBeenCalledOnce();
  });
});
