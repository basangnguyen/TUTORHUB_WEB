import type { CurrentUser, NotificationPreference } from "@tutorhub/api-client";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import {
  useNotificationPreference,
  useUpdateNotificationPreference,
} from "../app/notifications";
import { SessionProvider } from "../app/session";
import { NotificationPreferencesPage } from "./NotificationPreferencesPage";

vi.mock("../app/notifications", () => ({
  useNotificationPreference: vi.fn(),
  useUpdateNotificationPreference: vi.fn(),
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

const preference: NotificationPreference = {
  email_enabled: false,
  in_app_enabled: true,
  quiet_hours_enabled: false,
  quiet_hours_end: null,
  quiet_hours_start: null,
  quiet_hours_timezone: "Asia/Ho_Chi_Minh",
  reminder_offset_minutes: 120,
  updated_at: "2026-07-25T08:00:00Z",
  version: 3,
};

function renderPage() {
  const mutate = vi.fn((...args: unknown[]) => {
    const options = args[1] as
      { onSuccess?: (data: NotificationPreference) => void } | undefined;
    options?.onSuccess?.(preference);
  });

  vi.mocked(useNotificationPreference).mockReturnValue({
    data: preference,
    error: null,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  } as unknown as ReturnType<typeof useNotificationPreference>);
  vi.mocked(useUpdateNotificationPreference).mockReturnValue({
    error: null,
    isError: false,
    isPending: false,
    mutate,
    reset: vi.fn(),
  } as unknown as ReturnType<typeof useUpdateNotificationPreference>);

  render(
    <I18nProvider initialLanguage="en">
      <SessionProvider mode={{ kind: "static", currentUser: session }}>
        <MemoryRouter>
          <NotificationPreferencesPage />
        </MemoryRouter>
      </SessionProvider>
    </I18nProvider>,
  );

  return { mutate };
}

describe("NotificationPreferencesPage", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("keeps a stored reminder offset selectable when it is not a preset", () => {
    renderPage();

    expect(
      screen.getByRole("combobox", { name: "Class reminder" }),
    ).toHaveTextContent("120 minutes before");
  });

  it("clears the saved confirmation after the user edits the form", () => {
    const { mutate } = renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
    expect(mutate).toHaveBeenCalledOnce();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Notification preferences saved.",
    );

    fireEvent.click(
      screen.getByRole("checkbox", { name: "In-app notifications" }),
    );
    expect(
      screen.queryByText("Notification preferences saved."),
    ).not.toBeInTheDocument();
  });
});
