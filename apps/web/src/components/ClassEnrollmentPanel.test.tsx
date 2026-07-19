import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type {
  ClassEnrollment,
  ClassInviteCode,
  ClassroomClass,
  CurrentUser,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { classEnrollmentQueryKeys } from "../app/classEnrollments";
import { ClassEnrollmentPanel } from "./ClassEnrollmentPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const ownerID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const studentID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";

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
  description: "Class enrollment management.",
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
  created_at: "2026-07-18T02:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
  archived_at: null,
};

const enrollment: ClassEnrollment = {
  id: "63af7268-58db-4d40-a96f-c4f473a92350",
  class_id: classID,
  user_id: studentID,
  class_role: "student",
  status: "active",
  enrolled_by: ownerID,
  joined_at: "2026-07-19T03:00:00Z",
  suspended_at: null,
  left_at: null,
  removed_at: null,
  created_at: "2026-07-19T03:00:00Z",
  updated_at: "2026-07-19T03:00:00Z",
};

const inviteCode: ClassInviteCode = {
  id: "72299f70-e556-4878-b66d-c2dab2a2492f",
  class_id: classID,
  status: "active",
  expires_at: "2026-07-26T02:00:00Z",
  usage_limit: 12,
  usage_count: 0,
  created_by: ownerID,
  revoked_at: null,
  created_at: "2026-07-19T02:00:00Z",
  updated_at: "2026-07-19T02:00:00Z",
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

function renderPanel(
  fetchMock: ReturnType<typeof vi.fn>,
  value: ClassroomClass = classroom,
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
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <ClassEnrollmentPanel classroom={value} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("ClassEnrollmentPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("stays hidden and does not fetch codes without class-scoped management access", () => {
    const fetchMock = vi.fn();

    renderPanel(fetchMock, {
      ...classroom,
      viewer_access: {
        ...classroom.viewer_access,
        can_manage_enrollments: false,
      },
    });

    expect(
      screen.queryByRole("heading", { name: "Members and invite links" }),
    ).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("directly enrolls by normalized email and creates a one-time bounded link", async () => {
    const token = `thciv1_${"C".repeat(43)}`;
    const joinURL = `https://web.example/class-invite#token=${token}`;
    let csrfRequests = 0;
    let wasCreated = false;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(
          jsonResponse({ items: wasCreated ? [inviteCode] : [] }),
        );
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        csrfRequests += 1;
        return Promise.resolve(
          jsonResponse({ csrf_token: `mutation-csrf-${csrfRequests}` }),
        );
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/enrollments`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(jsonResponse(enrollment, 201));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
        request.method === "POST"
      ) {
        wasCreated = true;
        return Promise.resolve(
          jsonResponse({ invite_code: inviteCode, join_url: joinURL }, 201),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPanel(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "No class join links" }),
    ).toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "Member email" }), {
      target: { value: "  Student@Example.com  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add to class" }));

    expect(
      await screen.findByText("The learner was added to the class."),
    ).toBeInTheDocument();
    const enrollmentRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/enrollments"));
    expect(enrollmentRequest?.headers.get("X-CSRF-Token")).toBe(
      "mutation-csrf-1",
    );
    await expect(enrollmentRequest?.clone().json()).resolves.toEqual({
      member_email: "student@example.com",
    });

    fireEvent.click(screen.getByRole("button", { name: "Create link" }));
    const dialog = screen.getByRole("dialog", {
      name: "Create a class join link",
    });
    fireEvent.change(
      within(dialog).getByRole("spinbutton", { name: "Maximum uses" }),
      {
        target: { value: "12" },
      },
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Create link" }),
    );

    expect(await screen.findByDisplayValue(joinURL)).toBeInTheDocument();
    const createRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find(
        (request) =>
          request.url.endsWith("/invite-codes") && request.method === "POST",
      );
    expect(createRequest?.headers.get("X-CSRF-Token")).toBe("mutation-csrf-2");
    await expect(createRequest?.clone().json()).resolves.toEqual({
      expires_in_seconds: 604800,
      usage_limit: 12,
    });
    expect(
      fetchMock.mock.calls.every(
        (call) => !(call[0] as Request).url.includes(token),
      ),
    ).toBe(true);
    expect(
      queryClient.getQueryData(
        classEnrollmentQueryKeys.inviteCodes(tenantID, classID),
      ),
    ).toEqual({ items: [inviteCode] });
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

  it("initializes invite metadata while the first list request is still pending", async () => {
    const joinURL = `https://web.example/class-invite#token=thciv1_${"D".repeat(43)}`;
    let reads = 0;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
        request.method === "GET"
      ) {
        reads += 1;
        if (reads === 1) {
          return new Promise<Response>((resolve) => {
            request.signal.addEventListener(
              "abort",
              () => resolve(jsonResponse({ items: [] })),
              { once: true },
            );
          });
        }
        return Promise.resolve(jsonResponse({ items: [inviteCode] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "create-csrf" }));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          jsonResponse({ invite_code: inviteCode, join_url: joinURL }, 201),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPanel(fetchMock);

    await waitFor(() => expect(reads).toBe(1));
    fireEvent.click(screen.getByRole("button", { name: "Create link" }));
    fireEvent.click(
      within(
        screen.getByRole("dialog", {
          name: "Create a class join link",
        }),
      ).getByRole("button", { name: "Create link" }),
    );

    expect(await screen.findByDisplayValue(joinURL)).toBeInTheDocument();
    expect(reads).toBeGreaterThanOrEqual(2);
    expect(
      queryClient.getQueryData(
        classEnrollmentQueryKeys.inviteCodes(tenantID, classID),
      ),
    ).toEqual({ items: [inviteCode] });
  });

  it("confirms revocation and replaces the cached invite-code metadata", async () => {
    const revokedCode: ClassInviteCode = {
      ...inviteCode,
      status: "revoked",
      revoked_at: "2026-07-19T04:00:00Z",
      updated_at: "2026-07-19T04:00:00Z",
    };
    let currentCode = inviteCode;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse({ items: [currentCode] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "revoke-csrf" }));
      }
      if (
        request.url.endsWith(
          `/api/v1/classes/${classID}/invite-codes/${inviteCode.id}/revoke`,
        ) &&
        request.method === "POST"
      ) {
        currentCode = revokedCode;
        return Promise.resolve(jsonResponse(revokedCode));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPanel(fetchMock);

    expect(await screen.findByText("0/12 uses consumed")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: /^Revoke link expiring at /,
      }),
    );
    const dialog = screen.getByRole("dialog", {
      name: "Revoke this link?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm revoke" }),
    );

    expect(
      await screen.findByText("The class join link was revoked."),
    ).toBeInTheDocument();
    expect(screen.getByText("Revoked")).toBeInTheDocument();
    expect(
      queryClient.getQueryData(
        classEnrollmentQueryKeys.inviteCodes(tenantID, classID),
      ),
    ).toEqual({ items: [revokedCode] });
    const revokeRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/revoke"));
    expect(revokeRequest?.headers.get("X-CSRF-Token")).toBe("revoke-csrf");
  });

  it.each([401, 403, 404])(
    "hides cached invite data and all mutation controls after a refreshed %s",
    async (status) => {
      let reads = 0;
      const fetchMock = vi.fn().mockImplementation((request: Request) => {
        if (
          request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
          request.method === "GET"
        ) {
          reads += 1;
          return Promise.resolve(
            reads === 1
              ? jsonResponse({ items: [inviteCode] })
              : jsonResponse(
                  {
                    type: "urn:tutorhub:problem:access-boundary",
                    title: "Invite codes unavailable",
                    status,
                  },
                  status,
                ),
          );
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      });
      const queryClient = renderPanel(fetchMock);

      expect(await screen.findByText("0/12 uses consumed")).toBeInTheDocument();
      await queryClient.refetchQueries({
        exact: true,
        queryKey: classEnrollmentQueryKeys.inviteCodes(tenantID, classID),
      });

      await waitFor(() =>
        expect(
          screen.queryByText("0/12 uses consumed"),
        ).not.toBeInTheDocument(),
      );
      expect(
        screen.queryByRole("button", { name: "Create link" }),
      ).not.toBeInTheDocument();
      expect(
        screen.queryByRole("textbox", { name: "Member email" }),
      ).not.toBeInTheDocument();
      if (status === 403) {
        expect(
          screen.getByRole("heading", {
            name: "You can no longer manage this class",
          }),
        ).toBeInTheDocument();
      } else {
        expect(
          screen.getByRole("button", { name: "Try again" }),
        ).toBeInTheDocument();
      }
    },
  );

  it("refetches protected data after direct enrollment loses permission", async () => {
    let inviteReads = 0;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/invite-codes`) &&
        request.method === "GET"
      ) {
        inviteReads += 1;
        return Promise.resolve(
          inviteReads === 1
            ? jsonResponse({ items: [inviteCode] })
            : jsonResponse(
                {
                  type: "urn:tutorhub:problem:http-403",
                  title: "Class enrollment access denied",
                  status: 403,
                },
                403,
              ),
        );
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "stale-csrf" }));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/enrollments`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:http-403",
              title: "Class enrollment access denied",
              status: 403,
            },
            403,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPanel(fetchMock);

    expect(await screen.findByText("0/12 uses consumed")).toBeInTheDocument();
    fireEvent.change(screen.getByRole("textbox", { name: "Member email" }), {
      target: { value: "student@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add to class" }));

    expect(
      await screen.findByRole("heading", {
        name: "You can no longer manage this class",
      }),
    ).toBeInTheDocument();
    expect(inviteReads).toBeGreaterThanOrEqual(2);
    expect(screen.queryByText("0/12 uses consumed")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "Member email" }),
    ).not.toBeInTheDocument();
  });

  it("keeps revocation available while archived creation and direct enrollment stay locked", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ items: [inviteCode] }));
    renderPanel(fetchMock, {
      ...classroom,
      archived_at: "2026-07-19T04:00:00Z",
      status: "archived",
    });

    expect(
      await screen.findByRole("button", {
        name: /^Revoke link expiring at /,
      }),
    ).toBeEnabled();
    expect(screen.getByRole("button", { name: "Create link" })).toBeDisabled();
    expect(
      screen.getByRole("textbox", { name: "Member email" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Add to class" })).toBeDisabled();
  });
});
