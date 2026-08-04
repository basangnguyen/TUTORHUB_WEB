import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { useNotificationUnreadCount } from "../app/notifications";
import { NotificationBell } from "./NotificationBell";

vi.mock("../app/notifications", () => ({
  useNotificationUnreadCount: vi.fn(),
}));

const unreadQuery = vi.mocked(useNotificationUnreadCount);

function renderBell(initialLanguage: "en" | "vi" = "vi") {
  return render(
    <I18nProvider initialLanguage={initialLanguage}>
      <MemoryRouter>
        <NotificationBell tenantID="tenant-a" />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("NotificationBell", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("shows a bounded unread badge and an accessible count", () => {
    unreadQuery.mockReturnValue({
      data: { count: 142, is_capped: false },
      isSuccess: true,
    } as ReturnType<typeof useNotificationUnreadCount>);

    renderBell();

    expect(
      screen.getByRole("link", {
        name: "Mở trung tâm thông báo, 142 chưa đọc",
      }),
    ).toHaveAttribute("href", "/app/notifications");
    expect(screen.getByText("99+")).toBeInTheDocument();
  });

  it("does not claim a false zero when the count request fails", () => {
    unreadQuery.mockReturnValue({
      data: undefined,
      isSuccess: false,
    } as ReturnType<typeof useNotificationUnreadCount>);

    renderBell();

    expect(
      screen.getByRole("link", { name: "Mở trung tâm thông báo" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });

  it("announces a capped unread count without claiming an exact total", () => {
    unreadQuery.mockReturnValue({
      data: { count: 1000, is_capped: true },
      isSuccess: true,
    } as ReturnType<typeof useNotificationUnreadCount>);

    renderBell("en");

    expect(
      screen.getByRole("link", {
        name: "Open notification center, at least 1000 unread",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("99+")).toBeInTheDocument();
  });
});
