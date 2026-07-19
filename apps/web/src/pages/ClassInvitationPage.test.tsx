import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ClassroomClass, CurrentUser } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { ClassInvitationPage } from "./ClassInvitationPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";

const membership = {
  id: tenantID,
  slug: "tutorhub-test",
  name: "TutorHub Test",
  role: "student" as const,
  is_active: true,
  status: "active" as const,
  version: 1,
};

const currentUser: CurrentUser = {
  user: {
    id: userID,
    email: "student@example.com",
    display_name: "TutorHub Student",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: membership,
  memberships: [membership],
  permissions: [],
};

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
  code: "SEC101",
  title: "Information Security",
  description: "Class joined from a secure invitation.",
  timezone: "Asia/Ho_Chi_Minh",
  status: "active",
  version: 2,
  viewer_access: {
    class_role: "student",
    enrollment_status: "active",
    can_update_class: false,
    can_archive_class: false,
    can_transfer_ownership: false,
    can_manage_enrollments: false,
    can_join_room: true,
    can_publish_media: true,
    can_leave: true,
  },
  created_at: "2026-07-18T02:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
  archived_at: null,
};

const enrollment = {
  id: "63af7268-58db-4d40-a96f-c4f473a92350",
  class_id: classID,
  user_id: userID,
  class_role: "student" as const,
  status: "active" as const,
  enrolled_by: userID,
  joined_at: "2026-07-19T03:00:00Z",
  suspended_at: null,
  left_at: null,
  removed_at: null,
  created_at: "2026-07-19T03:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
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

function renderInvitationPage(
  fetchMock: ReturnType<typeof vi.fn>,
  user: CurrentUser | null = currentUser,
) {
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
        <SessionProvider mode={{ kind: "static", currentUser: user }}>
          <MemoryRouter initialEntries={["/class-invite"]}>
            <Routes>
              <Route path="/class-invite" element={<ClassInvitationPage />} />
              <Route
                path="/app/classrooms/:classId"
                element={<h1>Joined class destination</h1>}
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("ClassInvitationPage", () => {
  afterEach(() => {
    cleanup();
    window.history.replaceState(null, "", "/");
    window.localStorage.clear();
    window.sessionStorage.clear();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("consumes the fragment and sends the bearer token only in the join body", async () => {
    const token = `thciv1_${"A".repeat(43)}`;
    window.history.replaceState(null, "", `/class-invite#token=${token}`);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "join-csrf" }));
      }
      if (
        request.url.endsWith("/api/v1/class-invitations/join") &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          jsonResponse({ classroom, enrollment, joined: true }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderInvitationPage(fetchMock);

    expect(window.location.pathname).toBe("/class-invite");
    expect(window.location.hash).toBe("");
    expect(window.localStorage).toHaveLength(0);
    expect(window.sessionStorage).toHaveLength(0);
    expect(fetchMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Join class" }));

    expect(
      await screen.findByRole("heading", { name: "Joined class destination" }),
    ).toBeInTheDocument();
    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.every((request) => !request.url.includes(token))).toBe(
      true,
    );
    const joinRequest = requests.find((request) =>
      request.url.endsWith("/api/v1/class-invitations/join"),
    );
    expect(joinRequest?.headers.get("X-CSRF-Token")).toBe("join-csrf");
    await expect(joinRequest?.clone().json()).resolves.toEqual({ token });
    expect(
      JSON.stringify(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => ({ data: query.state.data, key: query.queryKey })),
      ),
    ).not.toContain(token);
    await waitFor(() =>
      expect(
        JSON.stringify(
          queryClient
            .getMutationCache()
            .getAll()
            .map((mutation) => mutation.state),
        ),
      ).not.toContain(token),
    );
  });

  it("does not call the API when no token is present", () => {
    window.history.replaceState(null, "", "/class-invite");
    const fetchMock = vi.fn();

    renderInvitationPage(fetchMock, null);

    expect(
      screen.getByRole("heading", { name: "Class invitation unavailable" }),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps the consumed token in memory when retrying after an offline state", async () => {
    const token = `thciv1_${"C".repeat(43)}`;
    window.history.replaceState(null, "", `/class-invite#token=${token}`);
    const online = vi
      .spyOn(window.navigator, "onLine", "get")
      .mockReturnValue(false);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "offline-csrf" }));
      }
      if (request.url.endsWith("/api/v1/class-invitations/join")) {
        return Promise.resolve(
          jsonResponse({ classroom, enrollment, joined: true }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderInvitationPage(fetchMock);

    expect(screen.getByText("You are offline")).toBeInTheDocument();
    expect(window.location.hash).toBe("");

    online.mockReturnValue(true);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    fireEvent.click(screen.getByRole("button", { name: "Join class" }));

    expect(
      await screen.findByRole("heading", { name: "Joined class destination" }),
    ).toBeInTheDocument();
    const joinRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) =>
        request.url.endsWith("/api/v1/class-invitations/join"),
      );
    await expect(joinRequest?.clone().json()).resolves.toEqual({ token });
  });

  it("keeps unavailable invite states recoverable without exposing class data", async () => {
    const token = `thciv1_${"B".repeat(43)}`;
    window.history.replaceState(null, "", `/class-invite#token=${token}`);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "join-csrf" }));
      }
      if (request.url.endsWith("/api/v1/class-invitations/join")) {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:class-invitation-unavailable",
              title: "Class invitation unavailable",
              status: 404,
            },
            404,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderInvitationPage(fetchMock);

    fireEvent.click(screen.getByRole("button", { name: "Join class" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The link is invalid, expired, revoked, exhausted, or the class is no longer active.",
    );
    expect(
      screen.getByRole("button", { name: "Retry joining" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(classroom.title)).not.toBeInTheDocument();
    expect(
      fetchMock.mock.calls.every(
        (call) => !(call[0] as Request).url.includes(token),
      ),
    ).toBe(true);
  });
});
