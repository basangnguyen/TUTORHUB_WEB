import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ClassroomClass, CurrentUser } from "@tutorhub/api-client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { ClassroomDetailPage, ClassroomListPage } from "./ClassroomPages";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const ownerID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: ownerID,
  code: "SEC101",
  title: "Cơ sở An toàn thông tin",
  description: "Lớp học kỳ 1",
  timezone: "Asia/Ho_Chi_Minh",
  status: "active",
  version: 3,
  archived_at: null,
  created_at: "2026-07-18T01:00:00Z",
  updated_at: "2026-07-18T02:00:00Z",
  viewer_access: {
    class_role: null,
    enrollment_status: null,
    can_update_class: true,
    can_archive_class: true,
    can_transfer_ownership: true,
    can_manage_enrollments: false,
    can_join_room: true,
    can_publish_media: true,
    can_leave: false,
  },
};

function currentUser({
  role = "teacher",
  userID = ownerID,
}: {
  role?: "org_admin" | "teacher";
  userID?: string;
} = {}): CurrentUser {
  const membership = {
    id: tenantID,
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role,
    is_active: true,
    status: "active" as const,
    version: 1,
  };
  return {
    user: {
      id: userID,
      email: "teacher@example.com",
      display_name: "TutorHub Teacher",
      locale: "vi",
      timezone: "UTC",
    },
    active_tenant: membership,
    memberships: [membership],
    permissions: [
      "class.view",
      "class.create",
      "class.update",
      ...(role === "org_admin" ? (["class.archive"] as const) : []),
      "session.join",
    ],
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderClassRoute(
  path: string,
  fetchMock: ReturnType<typeof vi.fn>,
  session = currentUser(),
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="vi">
        <SessionProvider mode={{ kind: "static", currentUser: session }}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route element={<ClassroomListPage />} path="/app/classrooms" />
              <Route
                element={<ClassroomDetailPage />}
                path="/app/classrooms/:classId"
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function requestFrom(call: unknown[]) {
  return call[0] as Request;
}

describe("ClassroomPages P2-04", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("filters by status and loads cursor pages without losing the first page", async () => {
    const secondClass: ClassroomClass = {
      ...classroom,
      id: "f24d8c4b-b141-475f-b20a-ed0bfe96da6b",
      code: "SEC102",
      title: "An toàn ứng dụng",
      version: 1,
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (!url.pathname.endsWith("/api/v1/classes")) {
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      }
      if (url.searchParams.get("status") === "archived") {
        return Promise.resolve(
          jsonResponse({
            items: [
              {
                ...classroom,
                status: "archived",
                archived_at: classroom.updated_at,
              },
            ],
            next_cursor: null,
          }),
        );
      }
      if (url.searchParams.get("cursor") === "cursor-page-2") {
        return Promise.resolve(
          jsonResponse({ items: [secondClass], next_cursor: null }),
        );
      }
      return Promise.resolve(
        jsonResponse({
          items: [classroom],
          next_cursor: "cursor-page-2",
        }),
      );
    });

    renderClassRoute("/app/classrooms", fetchMock);

    expect(
      await screen.findByText("Cơ sở An toàn thông tin"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Tải thêm lớp" }));
    expect(await screen.findByText("An toàn ứng dụng")).toBeInTheDocument();
    expect(screen.getByText("Cơ sở An toàn thông tin")).toBeInTheDocument();

    const secondPageRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find((request) => request.url.includes("cursor=cursor-page-2"));
    expect(secondPageRequest?.url).toContain("limit=20");

    const statusTrigger = screen.getByRole("combobox", {
      name: "Lọc theo trạng thái",
    });
    fireEvent.keyDown(statusTrigger, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Đã lưu trữ" }));

    await waitFor(() => {
      expect(
        fetchMock.mock.calls
          .map(requestFrom)
          .some((request) => request.url.includes("status=archived")),
      ).toBe(true);
    });
  });

  it("uses the active workspace timezone when creating a draft class", async () => {
    const created = {
      ...classroom,
      status: "draft" as const,
      timezone: "Asia/Bangkok",
      version: 1,
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (
        url.pathname.endsWith(`/api/v1/tenants/${tenantID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse({ timezone: "Asia/Bangkok" }));
      }
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "class-csrf" }));
      }
      if (
        url.pathname.endsWith("/api/v1/classes") &&
        request.method === "POST"
      ) {
        return Promise.resolve(jsonResponse(created, 201));
      }
      if (
        url.pathname.endsWith("/api/v1/classes") &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse({ items: [], next_cursor: null }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderClassRoute("/app/classrooms", fetchMock);

    await screen.findByText("Workspace chưa có lớp học");
    fireEvent.click(screen.getByRole("button", { name: "Tạo lớp học" }));
    const timezoneInput = screen.getByRole("textbox", {
      name: /^Múi giờ lớp học/,
    });
    await waitFor(() => expect(timezoneInput).toHaveValue("Asia/Bangkok"));
    fireEvent.change(screen.getByRole("textbox", { name: /^Mã lớp/ }), {
      target: { value: "sec101" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Tên lớp" }), {
      target: { value: classroom.title },
    });
    fireEvent.click(screen.getByRole("button", { name: "Tạo lớp" }));

    await screen.findByRole("heading", { name: classroom.title });
    const createRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find(
        (request) =>
          request.method === "POST" && request.url.endsWith("/api/v1/classes"),
      );
    expect(createRequest?.headers.get("X-CSRF-Token")).toBe("class-csrf");
    await expect(createRequest?.clone().json()).resolves.toMatchObject({
      timezone: "Asia/Bangkok",
    });
  });

  it("updates metadata with the base version and activates a draft", async () => {
    const draft = { ...classroom, status: "draft" as const };
    const updated = {
      ...draft,
      title: "An toàn thông tin nâng cao",
      status: "active" as const,
      version: 4,
      updated_at: "2026-07-18T03:00:00Z",
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "update-csrf" }));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}`) &&
        request.method === "PATCH"
      ) {
        return Promise.resolve(jsonResponse(updated));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(draft));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderClassRoute(`/app/classrooms/${classID}`, fetchMock);

    const titleInput = await screen.findByRole("textbox", {
      name: "Tên lớp",
    });
    fireEvent.change(titleInput, {
      target: { value: "An toàn thông tin nâng cao" },
    });
    const statusTrigger = screen.getByRole("combobox", {
      name: "Trạng thái lớp",
    });
    fireEvent.keyDown(statusTrigger, { key: "ArrowDown" });
    fireEvent.click(
      await screen.findByRole("option", { name: "Đang hoạt động" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Lưu thay đổi" }));

    expect(
      await screen.findByRole("heading", {
        name: "An toàn thông tin nâng cao",
      }),
    ).toBeInTheDocument();
    const updateRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find((request) => request.method === "PATCH");
    expect(updateRequest?.headers.get("X-CSRF-Token")).toBe("update-csrf");
    await expect(updateRequest?.clone().json()).resolves.toMatchObject({
      expected_version: 3,
      status: "active",
      title: "An toàn thông tin nâng cao",
    });
  });

  it("keeps a conflicting edit until the user reloads the latest version", async () => {
    const latest = {
      ...classroom,
      title: "Tên từ máy chủ",
      version: 4,
      updated_at: "2026-07-18T03:00:00Z",
    };
    let detailReads = 0;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "update-csrf" }));
      }
      if (request.method === "PATCH") {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:class-version-conflict",
              title: "Class changed",
              status: 409,
            },
            409,
          ),
        );
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}`) &&
        request.method === "GET"
      ) {
        detailReads += 1;
        return Promise.resolve(
          jsonResponse(detailReads === 1 ? classroom : latest),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderClassRoute(`/app/classrooms/${classID}`, fetchMock);

    const titleInput = await screen.findByRole("textbox", {
      name: "Tên lớp",
    });
    fireEvent.change(titleInput, { target: { value: "Bản sửa của tôi" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu thay đổi" }));
    expect(
      await screen.findByText(/Lớp đã được thay đổi ở nơi khác/),
    ).toBeInTheDocument();
    expect(titleInput).toHaveValue("Bản sửa của tôi");

    fireEvent.click(screen.getByRole("button", { name: "Tải bản mới nhất" }));
    await waitFor(() => expect(titleInput).toHaveValue("Tên từ máy chủ"));
  });

  it("reports a duplicate code without presenting stale-version recovery", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "update-csrf" }));
      }
      if (request.method === "PATCH") {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:conflict",
              title: "Class code already exists",
              status: 409,
            },
            409,
          ),
        );
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(classroom));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderClassRoute(`/app/classrooms/${classID}`, fetchMock);

    fireEvent.change(await screen.findByRole("textbox", { name: /^Mã lớp/ }), {
      target: { value: "SEC999" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu thay đổi" }));

    expect(
      await screen.findByText(/Mã lớp đã tồn tại trong workspace này/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Tải bản mới nhất" }),
    ).not.toBeInTheDocument();
  });

  it("confirms archive and restore while keeping lifecycle controls owner-scoped", async () => {
    let state = classroom;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "lifecycle-csrf" }));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/archive`) &&
        request.method === "POST"
      ) {
        state = {
          ...state,
          status: "archived",
          version: 4,
          archived_at: "2026-07-18T04:00:00Z",
        };
        return Promise.resolve(jsonResponse(state));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}/restore`) &&
        request.method === "POST"
      ) {
        state = {
          ...state,
          status: "active",
          version: 5,
          archived_at: null,
        };
        return Promise.resolve(jsonResponse(state));
      }
      if (
        request.url.endsWith(`/api/v1/classes/${classID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(state));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderClassRoute(`/app/classrooms/${classID}`, fetchMock);

    expect(
      await screen.findByRole("link", {
        name: "Vào phòng học trực tuyến",
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Lưu trữ lớp" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Xác nhận lưu trữ" }),
    );

    expect(
      await screen.findByRole("button", { name: "Khôi phục lớp" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Vào phòng học trực tuyến" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Chỉnh sửa lớp học" }),
    ).not.toBeInTheDocument();

    const archiveRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find((request) => request.url.endsWith("/archive"));
    await expect(archiveRequest?.clone().json()).resolves.toEqual({
      expected_version: 3,
    });

    fireEvent.click(screen.getByRole("button", { name: "Khôi phục lớp" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Xác nhận khôi phục" }),
    );
    expect(
      await screen.findByRole("button", { name: "Lưu trữ lớp" }),
    ).toBeInTheDocument();

    const restoreRequest = fetchMock.mock.calls
      .map(requestFrom)
      .find((request) => request.url.endsWith("/restore"));
    await expect(restoreRequest?.clone().json()).resolves.toEqual({
      expected_version: 4,
    });
  });

  it("does not expose archive or restore to a non-owner teacher", async () => {
    const teacherView = {
      ...classroom,
      viewer_access: {
        ...classroom.viewer_access,
        can_archive_class: false,
        can_transfer_ownership: false,
      },
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(teacherView));
    renderClassRoute(
      `/app/classrooms/${classID}`,
      fetchMock,
      currentUser({
        role: "teacher",
        userID: "c7e64873-6e2f-48f3-9988-bb2d345401ed",
      }),
    );

    expect(
      await screen.findByRole("heading", { name: classroom.title }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Chỉnh sửa lớp học" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Lưu trữ lớp" }),
    ).not.toBeInTheDocument();
  });

  it("exposes lifecycle controls to an organization admin who is not the owner", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(classroom));
    renderClassRoute(
      `/app/classrooms/${classID}`,
      fetchMock,
      currentUser({
        role: "org_admin",
        userID: "c7e64873-6e2f-48f3-9988-bb2d345401ed",
      }),
    );

    expect(
      await screen.findByRole("button", { name: "Lưu trữ lớp" }),
    ).toBeInTheDocument();
  });
});
