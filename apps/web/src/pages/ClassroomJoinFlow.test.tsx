import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ClassroomClass, CurrentUser } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useLocation } from "react-router-dom";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { tenantCapabilityQueryKeys } from "../app/tenantCapabilities";
import {
  availableTenantCapabilities,
  withAvailableTenantCapabilities,
} from "../test/tenantCapabilities";
import { ClassroomListPage } from "./ClassroomPages";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const studentID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";

const currentUser: CurrentUser = {
  user: {
    id: studentID,
    email: "student@example.test",
    display_name: "Student One",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "test",
    name: "Test workspace",
    role: "student",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: ["class.view", "session.join"],
};

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
  code: "SEC101",
  title: "Information Security",
  description: "Joined from the class list.",
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
  created_at: "2026-07-19T03:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
  archived_at: null,
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

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.pathname}</output>;
}

describe("class-list invitation journey", () => {
  afterEach(() => {
    cleanup();
    window.localStorage.clear();
    window.sessionStorage.clear();
    vi.unstubAllGlobals();
  });

  it("keeps a pasted token in memory/body only and awaits the joined class refetch", async () => {
    const token = `thciv1_${"A".repeat(43)}`;
    const copiedURL = `https://web.example/class-invite#token=${token}`;
    let joined = false;
    let listReads = 0;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith("/api/v1/classes?limit=20") &&
        request.method === "GET"
      ) {
        listReads += 1;
        return Promise.resolve(
          jsonResponse({
            items: joined ? [classroom] : [],
            next_cursor: null,
          }),
        );
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "join-csrf" }));
      }
      if (
        request.url.endsWith("/api/v1/class-invitations/join") &&
        request.method === "POST"
      ) {
        joined = true;
        return Promise.resolve(
          jsonResponse({
            classroom,
            enrollment: {
              id: "63af7268-58db-4d40-a96f-c4f473a92350",
              class_id: classID,
              user_id: studentID,
              class_role: "student",
              status: "active",
              enrolled_by: studentID,
              joined_at: classroom.updated_at,
              suspended_at: null,
              left_at: null,
              removed_at: null,
              created_at: classroom.updated_at,
              updated_at: classroom.updated_at,
            },
            joined: true,
          }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    queryClient.setQueryData(
      tenantCapabilityQueryKeys.detail(tenantID),
      availableTenantCapabilities(tenantID),
    );
    vi.stubGlobal(
      "fetch",
      withAvailableTenantCapabilities(fetchMock, tenantID),
    );

    render(
      <QueryClientProvider client={queryClient}>
        <I18nProvider initialLanguage="en">
          <SessionProvider mode={{ kind: "static", currentUser }}>
            <MemoryRouter initialEntries={["/app/classrooms"]}>
              <ClassroomListPage />
              <LocationProbe />
            </MemoryRouter>
          </SessionProvider>
        </I18nProvider>
      </QueryClientProvider>,
    );

    await screen.findByText("This workspace has no classes");
    fireEvent.click(screen.getByRole("button", { name: "Join with a code" }));
    const dialog = screen.getByRole("dialog", { name: "Join a class" });
    fireEvent.change(within(dialog).getByLabelText("Join code or link"), {
      target: { value: copiedURL },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Join class" }));

    expect(
      await screen.findByRole("link", { name: "Open the joined class" }),
    ).toHaveAttribute("href", `/app/classrooms/${classID}`);
    expect(screen.getByTestId("location")).toHaveTextContent("/app/classrooms");
    expect(screen.getByText(classroom.code)).toBeInTheDocument();
    expect(listReads).toBeGreaterThanOrEqual(2);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.every((request) => !request.url.includes(token))).toBe(
      true,
    );
    const joinRequest = requests.find((request) =>
      request.url.endsWith("/api/v1/class-invitations/join"),
    );
    expect(joinRequest?.headers.get("X-CSRF-Token")).toBe("join-csrf");
    await expect(joinRequest?.clone().json()).resolves.toEqual({ token });
    expect(window.localStorage).toHaveLength(0);
    expect(window.sessionStorage).toHaveLength(0);
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
});
