import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type {
  ClassRosterPage,
  ClassroomClass,
  CurrentUser,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { intersectRosterBulkChoices } from "../app/classRosterCapabilities";
import { SessionProvider } from "../app/session";
import { ClassRosterPanel } from "./ClassRosterPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const ownerID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const studentID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";

const currentUser: CurrentUser = {
  user: {
    id: ownerID,
    email: "owner@example.test",
    display_name: "Owner Teacher",
    locale: "en",
    timezone: "UTC",
  },
  active_tenant: {
    id: tenantID,
    slug: "test",
    name: "Test workspace",
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
  title: "Security",
  description: "Class roster test",
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
    can_join_room: true,
    can_publish_media: true,
    can_leave: false,
  },
  created_at: "2026-07-19T01:00:00Z",
  updated_at: "2026-07-19T02:00:00Z",
  archived_at: null,
};

const enrollment = {
  id: "63af7268-58db-4d40-a96f-c4f473a92350",
  class_id: classID,
  user_id: studentID,
  class_role: "student" as const,
  status: "active" as const,
  enrolled_by: ownerID,
  joined_at: "2026-07-19T03:00:00Z",
  suspended_at: null,
  left_at: null,
  removed_at: null,
  created_at: "2026-07-19T03:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
};

const rosterPage: ClassRosterPage = {
  class_owner: {
    user: {
      id: ownerID,
      display_name: "Owner Teacher",
      email: "owner@example.test",
    },
    class_role: "owner",
  },
  items: [
    {
      user: {
        id: studentID,
        display_name: "Student One",
        email: "student@example.test",
      },
      enrollment,
      actions: {
        assignable_roles: ["teaching_assistant"],
        can_suspend: true,
        can_remove: true,
      },
    },
  ],
  next_cursor: null,
};
const rosterMember = rosterPage.items[0]!;

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderPanel(
  fetchMock: ReturnType<typeof vi.fn>,
  value: ClassroomClass = classroom,
) {
  vi.stubGlobal("fetch", fetchMock);
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <ClassRosterPanel classroom={value} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function requestFrom(call: unknown[]) {
  return call[0] as Request;
}

describe("ClassRosterPanel P2-06", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("pins the implicit owner, searches, and confirms a role update", async () => {
    const updatedEnrollment = {
      ...enrollment,
      class_role: "teaching_assistant" as const,
      updated_at: "2026-07-19T04:00:00Z",
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "roster-csrf" }));
      }
      if (
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/roster/${studentID}`,
        ) &&
        request.method === "PATCH"
      ) {
        return Promise.resolve(
          jsonResponse({ outcome: "updated", enrollment: updatedEnrollment }),
        );
      }
      if (
        url.pathname.endsWith(`/api/v1/classes/${classID}/roster`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(rosterPage));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPanel(fetchMock);

    expect(await screen.findByText("Pinned class owner")).toBeInTheDocument();
    expect(screen.getByText("Owner Teacher")).toBeInTheDocument();
    expect(screen.getByText("Student One")).toBeInTheDocument();

    fireEvent.change(screen.getByRole("searchbox", { name: "Find a member" }), {
      target: { value: "  STUDENT   ONE  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    await waitFor(() => {
      expect(
        fetchMock.mock.calls
          .map(requestFrom)
          .some(
            (request) =>
              new URL(request.url).searchParams.get("search") === "student one",
          ),
      ).toBe(true);
    });

    const roleSelect = await screen.findByRole("combobox", {
      name: "Change the class role for Student One",
    });
    fireEvent.keyDown(roleSelect, { key: "ArrowDown" });
    fireEvent.click(
      await screen.findByRole("option", { name: "Teaching assistant" }),
    );
    expect(
      screen.getByRole("dialog", { name: "Confirm roster change" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await screen.findByText("The class role was updated.");
    const patchRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find((request) => request.method === "PATCH");
    expect(patchRequest?.headers.get("X-CSRF-Token")).toBe("roster-csrf");
    await expect(patchRequest?.clone().json()).resolves.toEqual({
      class_role: "teaching_assistant",
    });
  });

  it("sends a bounded bulk action and announces partial failures", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "bulk-csrf" }));
      }
      if (
        url.pathname.endsWith(`/api/v1/classes/${classID}/roster/bulk`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          jsonResponse({
            action: "suspend",
            items: [
              {
                user_id: studentID,
                outcome: "failed",
                enrollment: null,
                failure: { code: "conflict", detail: "State changed." },
              },
            ],
            requested_count: 1,
            updated_count: 0,
            unchanged_count: 0,
            failed_count: 1,
          }),
        );
      }
      if (
        url.pathname.endsWith(`/api/v1/classes/${classID}/roster`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(rosterPage));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPanel(fetchMock);

    fireEvent.click(
      await screen.findByRole("checkbox", { name: "Select Student One" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    expect(
      await screen.findByText("Result: 0 updated, 0 unchanged, 1 failed."),
    ).toHaveAttribute("role", "alert");
    const bulkRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find(
        (request) =>
          request.method === "POST" && request.url.endsWith("/roster/bulk"),
      );
    expect(bulkRequest?.headers.get("X-CSRF-Token")).toBe("bulk-csrf");
    await expect(bulkRequest?.clone().json()).resolves.toEqual({
      action: "suspend",
      user_ids: [studentID],
    });
  });

  it("offers only the intersection of server-derived target capabilities", () => {
    const secondMember: ClassRosterPage["items"][number] = {
      ...rosterMember,
      user: {
        id: "9f45639d-ac6f-4f82-8465-7679dfcd4018",
        display_name: "Student Two",
        email: "student-two@example.test",
      },
      actions: {
        assignable_roles: ["co_teacher"],
        can_remove: true,
        can_suspend: false,
      },
    };

    expect(intersectRosterBulkChoices([rosterMember, secondMember])).toEqual([
      "remove",
    ]);
  });

  it("disables every row mutation for an archived class", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(rosterPage));
    renderPanel(fetchMock, {
      ...classroom,
      archived_at: "2026-07-19T04:00:00Z",
      status: "archived",
    });

    expect(
      await screen.findByRole("checkbox", { name: "Select Student One" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("combobox", {
        name: "Change the class role for Student One",
      }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Suspend" })).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Remove from class" }),
    ).toBeDisabled();
  });

  it.each([401, 403, 404])(
    "hides cached roster PII and controls after a refreshed %s",
    async (status) => {
      let reads = 0;
      const fetchMock = vi.fn().mockImplementation((request: Request) => {
        if (
          new URL(request.url).pathname.endsWith(
            `/api/v1/classes/${classID}/roster`,
          )
        ) {
          reads += 1;
          return Promise.resolve(
            reads === 1
              ? jsonResponse(rosterPage)
              : jsonResponse(
                  {
                    type: "urn:tutorhub:problem:access-boundary",
                    title: "Roster unavailable",
                    status,
                  },
                  status,
                ),
          );
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      });
      const queryClient = renderPanel(fetchMock);

      expect(await screen.findByText("Student One")).toBeInTheDocument();
      await queryClient.refetchQueries({
        queryKey: ["classes", tenantID, "detail", classID, "roster"],
      });

      await waitFor(() =>
        expect(screen.queryByText("Student One")).not.toBeInTheDocument(),
      );
      expect(screen.queryByText("Owner Teacher")).not.toBeInTheDocument();
      expect(screen.queryByText("owner@example.test")).not.toBeInTheDocument();
      expect(
        screen.queryByRole("searchbox", { name: "Find a member" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Suspend" }),
      ).not.toBeInTheDocument();
      if (status === 403) {
        expect(
          screen.getByRole("heading", {
            name: "You can no longer view this roster",
          }),
        ).toBeInTheDocument();
      } else {
        expect(
          screen.getByRole("button", { name: "Try again" }),
        ).toBeInTheDocument();
      }
    },
  );
});
