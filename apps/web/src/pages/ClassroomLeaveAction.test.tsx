import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type {
  ClassEnrollment,
  ClassroomClass,
  CurrentUser,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { classQueryKeys } from "../app/classes";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { ClassroomDetailPage } from "./ClassroomPages";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const studentID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";

const currentUser: CurrentUser = {
  user: {
    id: studentID,
    email: "student@example.com",
    display_name: "TutorHub Student",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role: "student",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: [],
};

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
  code: "SEC101",
  title: "Information Security",
  description: "An enrolled learner can leave this class.",
  timezone: "Asia/Ho_Chi_Minh",
  status: "active",
  version: 2,
  viewer_access: {
    class_role: "student",
    enrollment_status: "active",
    can_manage_enrollments: false,
    can_join_room: true,
    can_publish_media: true,
    can_leave: true,
  },
  created_at: "2026-07-18T02:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
  archived_at: null,
};

const leftEnrollment: ClassEnrollment = {
  id: "63af7268-58db-4d40-a96f-c4f473a92350",
  class_id: classID,
  user_id: studentID,
  class_role: "student",
  status: "left",
  enrolled_by: studentID,
  joined_at: "2026-07-19T03:00:00Z",
  suspended_at: null,
  left_at: "2026-07-19T04:00:00Z",
  removed_at: null,
  created_at: "2026-07-19T03:00:00Z",
  updated_at: "2026-07-19T04:00:00Z",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderDetail(fetchMock: ReturnType<typeof vi.fn>) {
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
          <MemoryRouter initialEntries={[`/app/classrooms/${classID}`]}>
            <Routes>
              <Route
                path="/app/classrooms/:classId"
                element={<ClassroomDetailPage />}
              />
              <Route
                path="/app/classrooms"
                element={<h1>Class list destination</h1>}
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("Classroom self-leave action", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("uses viewer access, confirms the mutation, and removes private detail cache", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/classes/${classID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(classroom));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "leave-csrf" }));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/leave`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(jsonResponse(leftEnrollment));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderDetail(fetchMock);

    expect(
      await screen.findByRole("heading", { name: classroom.title }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Leave class" }));
    const dialog = screen.getByRole("dialog", {
      name: "Leave this class?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm leave" }),
    );

    expect(
      await screen.findByRole("heading", { name: "Class list destination" }),
    ).toBeInTheDocument();
    const leaveRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/leave"));
    expect(leaveRequest?.headers.get("X-CSRF-Token")).toBe("leave-csrf");
    expect(
      queryClient.getQueryData(classQueryKeys.detail(tenantID, classID)),
    ).toBeUndefined();
  });
});
