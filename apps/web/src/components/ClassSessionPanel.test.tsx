import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type {
  ClassSession,
  ClassroomClass,
  CurrentUser,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { ClassSessionPanel } from "./ClassSessionPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const ownerID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const sessionID = "9f8bf389-362c-48ca-8e67-53df4e558f4d";
const availableOperation = { available: true, reason: "available" } as const;

const currentUser: CurrentUser = {
  user: {
    id: ownerID,
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
  permissions: [],
};

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: ownerID,
  code: "SEC101",
  title: "Information Security",
  description: "Class session scheduling.",
  timezone: "Asia/Ho_Chi_Minh",
  status: "active",
  version: 2,
  viewer_access: {
    class_role: "owner",
    enrollment_status: null,
    can_update_class: true,
    can_archive_class: true,
    can_transfer_ownership: true,
    can_manage_enrollments: true,
    can_schedule_sessions: true,
    can_join_room: true,
    can_publish_media: true,
    can_leave: false,
  },
  created_at: "2026-07-18T02:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
  archived_at: null,
};

const classSession: ClassSession = {
  id: sessionID,
  class_id: classID,
  title: "P3-01 staging acceptance",
  description: "Regression coverage for the edit dialog.",
  starts_at: "2026-07-29T02:00:00Z",
  ends_at: "2026-07-29T03:30:00Z",
  timezone: "Asia/Ho_Chi_Minh",
  status: "scheduled",
  version: 3,
  viewer_access: {
    can_update: true,
    can_cancel: true,
  },
  created_by: ownerID,
  updated_by: ownerID,
  cancelled_by: null,
  cancelled_at: null,
  created_at: "2026-07-24T02:00:00Z",
  updated_at: "2026-07-24T02:00:00Z",
};

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function renderPanel(fetchMock: ReturnType<typeof vi.fn>) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <ClassSessionPanel
            classroom={classroom}
            schedulingAvailability={availableOperation}
          />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("ClassSessionPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("prefills the edit form when its controlled dialog is opened by the parent", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        new URL(request.url).pathname ===
          `/api/v1/classes/${classID}/sessions` &&
        request.method === "GET"
      ) {
        return Promise.resolve(
          jsonResponse({ items: [classSession], next_cursor: null }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPanel(fetchMock);

    await screen.findByRole("heading", { name: classSession.title });
    fireEvent.click(screen.getByRole("button", { name: "Edit class" }));

    const dialog = await screen.findByRole("dialog", { name: "Edit session" });
    expect(
      within(dialog).getByRole("textbox", { name: "Session title" }),
    ).toHaveValue(classSession.title);
    expect(
      within(dialog).getByRole("textbox", { name: "Description" }),
    ).toHaveValue(classSession.description);
    expect(within(dialog).getByLabelText("Starts")).toHaveValue(
      "2026-07-29T09:00",
    );
    expect(within(dialog).getByLabelText("Ends")).toHaveValue(
      "2026-07-29T10:30",
    );
    expect(
      within(dialog).getByRole("textbox", { name: "Timezone" }),
    ).toHaveValue(classSession.timezone);
  });
});
