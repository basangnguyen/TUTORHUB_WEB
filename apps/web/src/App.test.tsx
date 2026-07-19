import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, useI18n } from "./app/i18n";
import { createAppRoutes, getVisibleNavigationItems } from "./app/routes";
import { SessionProvider } from "./app/session";
import type { CurrentUser, Tenant } from "@tutorhub/api-client";

const activeMembership = {
  id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
  slug: "tutorhub-test",
  name: "TutorHub Test",
  role: "teacher" as const,
  is_active: true,
  status: "active" as const,
  version: 1,
};

const testSession: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "vi",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: activeMembership,
  memberships: [activeMembership],
  permissions: ["tenant.view", "class.view"],
};

function renderRoute(path: string, session: CurrentUser | null = testSession) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createMemoryRouter(createAppRoutes(), {
    initialEntries: [path],
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <SessionProvider mode={{ kind: "static", currentUser: session }}>
          <RouterProvider router={router} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function LanguageProbe() {
  const { language, setLanguage, t } = useI18n();
  return (
    <>
      <button onClick={() => setLanguage("en")} type="button">
        English
      </button>
      <output>{`${language}:${t("nav.home")}`}</output>
    </>
  );
}

describe("web shell", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("keeps primary navigation focused on the four Phase 2 journeys", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            status: "ok",
            service: "tutorhub-core-api",
            environment: "test",
            timestamp: "2026-07-19T00:00:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    renderRoute("/app/home");

    const navigation = screen.getByRole("navigation", {
      name: "Điều hướng chính",
    });
    expect(within(navigation).getAllByRole("link")).toHaveLength(4);
    expect(within(navigation).getByRole("link", { name: "Tổng quan" }));
    expect(within(navigation).getByRole("link", { name: "Lớp học" }));
    expect(within(navigation).getByRole("link", { name: "Workspace" }));
    expect(within(navigation).getByRole("link", { name: "Thiết lập" }));
    expect(within(navigation).queryByRole("link", { name: "Lịch" })).toBeNull();
    expect(getVisibleNavigationItems([]).map((item) => item.to)).toEqual([
      "/app/home",
      "/app/classrooms",
      "/app/settings",
    ]);
  });

  it("blocks the audit route before rendering it when audit.view is absent", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/app/workspace/audit");

    expect(
      await screen.findByRole("heading", {
        name: "Bạn chưa có quyền truy cập khu vực này",
      }),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("hiển thị trạng thái Core API từ TanStack Query", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            status: "ok",
            service: "tutorhub-core-api",
            environment: "test",
            timestamp: "2026-07-13T00:00:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    renderRoute("/app/home");

    expect(
      await screen.findByText("TutorHub API đã sẵn sàng · test"),
    ).toBeInTheDocument();
  });

  it("chuyển route được bảo vệ sang trang đăng nhập khi chưa có session", async () => {
    renderRoute("/app/home", null);

    expect(
      await screen.findByRole("heading", {
        name: "Đăng nhập vào TutorHub",
      }),
    ).toBeInTheDocument();
  });

  it("onboard người dùng mới và mở app shell sau khi tạo workspace", async () => {
    const createdSession: CurrentUser = {
      user: testSession.user,
      active_tenant: {
        ...activeMembership,
        name: "Khoa Công nghệ thông tin",
        slug: "khoa-cong-nghe-thong-tin",
        role: "org_admin",
      },
      memberships: [
        {
          ...activeMembership,
          name: "Khoa Công nghệ thông tin",
          slug: "khoa-cong-nghe-thong-tin",
          role: "org_admin",
        },
      ],
      permissions: ["tenant.manage"],
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(
          new Response(JSON.stringify({ csrf_token: "csrf-token" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (request.url.endsWith("/api/v1/tenants")) {
        return Promise.resolve(
          new Response(JSON.stringify(createdSession), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (request.url.endsWith("/health")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              status: "ok",
              service: "tutorhub-core-api",
              environment: "test",
              timestamp: "2026-07-14T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/app/home", {
      ...testSession,
      active_tenant: null,
      memberships: [],
      permissions: [],
    });

    expect(
      await screen.findByRole("heading", { name: "Tạo workspace đầu tiên" }),
    ).toBeInTheDocument();
    fireEvent.change(
      screen.getByRole("textbox", { name: "Tên tổ chức hoặc nhóm học" }),
      { target: { value: "Khoa Công nghệ thông tin" } },
    );
    const slugInput = screen.getByRole("textbox", {
      name: "Địa chỉ workspace",
    });
    await waitFor(() =>
      expect(slugInput).toHaveValue("khoa-cong-nghe-thong-tin"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Tạo workspace" }));

    expect(
      await screen.findByRole("heading", { name: "Tổng quan hôm nay" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Khoa Công nghệ thông tin")).toHaveLength(2);

    const tenantRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/api/v1/tenants"));
    expect(tenantRequest?.headers.get("X-CSRF-Token")).toBe("csrf-token");
    await expect(tenantRequest?.clone().json()).resolves.toEqual({
      name: "Khoa Công nghệ thông tin",
      slug: "khoa-cong-nghe-thong-tin",
    });
  });

  it("yêu cầu chọn membership hợp lệ khi session chưa có active tenant", async () => {
    const secondMembership = {
      id: "d53466c6-fb22-49bb-8dcb-e0399896a6c8",
      slug: "second-workspace",
      name: "Second Workspace",
      role: "student" as const,
      is_active: false,
      status: "active" as const,
      version: 1,
    };
    const switchedSession: CurrentUser = {
      ...testSession,
      active_tenant: { ...secondMembership, is_active: true },
      memberships: [
        { ...activeMembership, is_active: false },
        { ...secondMembership, is_active: true },
      ],
      permissions: ["class.view"],
    };
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((request: Request) => {
        if (request.url.endsWith("/api/v1/auth/csrf")) {
          return Promise.resolve(
            new Response(JSON.stringify({ csrf_token: "csrf-token" }), {
              status: 200,
              headers: { "Content-Type": "application/json" },
            }),
          );
        }
        if (request.url.endsWith("/api/v1/session/active-tenant")) {
          return Promise.resolve(
            new Response(JSON.stringify(switchedSession), {
              status: 200,
              headers: { "Content-Type": "application/json" },
            }),
          );
        }
        if (request.url.endsWith("/health")) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                status: "ok",
                service: "tutorhub-core-api",
                environment: "test",
                timestamp: "2026-07-14T00:00:00Z",
              }),
              {
                status: 200,
                headers: { "Content-Type": "application/json" },
              },
            ),
          );
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      }),
    );

    renderRoute("/app/home", {
      ...testSession,
      active_tenant: null,
      memberships: [activeMembership, secondMembership],
      permissions: [],
    });

    expect(
      await screen.findByRole("heading", {
        name: "Chọn workspace để tiếp tục",
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Second Workspace/ }));

    expect(
      await screen.findByRole("heading", { name: "Tổng quan hôm nay" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("combobox", { name: "Workspace đang hoạt động" }),
    ).toHaveValue(secondMembership.id);
  });

  it("thông báo pending, retry đúng workspace và về home sau khi switch", async () => {
    const secondMembership = {
      id: "d53466c6-fb22-49bb-8dcb-e0399896a6c8",
      slug: "second-workspace",
      name: "Second Workspace",
      role: "student" as const,
      is_active: false,
      status: "active" as const,
      version: 1,
    };
    const switchedSession: CurrentUser = {
      ...testSession,
      active_tenant: { ...secondMembership, is_active: true },
      memberships: [
        { ...activeMembership, is_active: false },
        { ...secondMembership, is_active: true },
      ],
      permissions: ["class.view"],
    };
    let switchAttempt = 0;
    let resolveFirstSwitch: ((response: Response) => void) | undefined;
    const firstSwitchResponse = new Promise<Response>((resolve) => {
      resolveFirstSwitch = resolve;
    });
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(
          new Response(JSON.stringify({ csrf_token: "csrf-token" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (request.url.endsWith("/api/v1/session/active-tenant")) {
        switchAttempt += 1;
        return switchAttempt === 1
          ? firstSwitchResponse
          : Promise.resolve(
              new Response(JSON.stringify(switchedSession), {
                status: 200,
                headers: { "Content-Type": "application/json" },
              }),
            );
      }
      if (request.url.endsWith("/health")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              status: "ok",
              service: "tutorhub-core-api",
              environment: "test",
              timestamp: "2026-07-18T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/app/home", {
      ...testSession,
      memberships: [activeMembership, secondMembership],
    });

    const workspaceSelect = screen.getByRole("combobox", {
      name: "Workspace đang hoạt động",
    });
    fireEvent.change(workspaceSelect, {
      target: { value: secondMembership.id },
    });

    expect(
      await screen.findByText("Đang chuyển workspace..."),
    ).toBeInTheDocument();
    expect(workspaceSelect).toBeDisabled();
    expect(workspaceSelect).toHaveAttribute("aria-busy", "true");

    await waitFor(() => expect(switchAttempt).toBe(1));
    await act(async () => {
      resolveFirstSwitch?.(
        new Response(
          JSON.stringify({
            type: "about:blank",
            title: "Service unavailable",
            status: 503,
          }),
          {
            status: 503,
            headers: { "Content-Type": "application/problem+json" },
          },
        ),
      );
    });

    expect(
      await screen.findByText(
        "Chưa thể chuyển workspace. Hãy thử lại hoặc kiểm tra membership.",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Thử lại" }));

    expect(
      await screen.findByRole("heading", { name: "Tổng quan hôm nay" }),
    ).toBeInTheDocument();
    const switchRequests = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .filter((request) =>
        request.url.endsWith("/api/v1/session/active-tenant"),
      );
    expect(switchRequests).toHaveLength(2);
    await expect(
      Promise.all(switchRequests.map((request) => request.clone().json())),
    ).resolves.toEqual([
      { tenant_id: secondMembership.id },
      { tenant_id: secondMembership.id },
    ]);
  });

  it("đưa người dùng về bộ chọn workspace sau khi lưu trữ workspace đang hoạt động", async () => {
    const adminMembership = {
      ...activeMembership,
      role: "org_admin" as const,
      version: 3,
    };
    const remainingMembership = {
      id: "d53466c6-fb22-49bb-8dcb-e0399896a6c8",
      slug: "second-workspace",
      name: "Second Workspace",
      role: "teacher" as const,
      is_active: false,
      status: "active" as const,
      version: 1,
    };
    const activeTenant: Tenant = {
      ...adminMembership,
      locale: "vi",
      timezone: "Asia/Ho_Chi_Minh",
      created_at: "2026-07-18T01:00:00Z",
      updated_at: "2026-07-18T02:00:00Z",
      archived_at: null,
    };
    const remainingTenant: Tenant = {
      ...remainingMembership,
      locale: "vi",
      timezone: "Asia/Ho_Chi_Minh",
      created_at: "2026-07-18T01:00:00Z",
      updated_at: "2026-07-18T02:00:00Z",
      archived_at: null,
    };
    const archivedSession: CurrentUser = {
      ...testSession,
      active_tenant: null,
      memberships: [remainingMembership],
      permissions: [],
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(
          new Response(JSON.stringify({ csrf_token: "archive-csrf" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (
        request.url.endsWith(`/api/v1/tenants/${activeTenant.id}/archive`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          new Response(JSON.stringify(archivedSession), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (
        request.url.endsWith(`/api/v1/tenants/${activeTenant.id}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(
          new Response(JSON.stringify(activeTenant), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (request.url.endsWith("/api/v1/tenants") && request.method === "GET") {
        return Promise.resolve(
          new Response(
            JSON.stringify({ items: [activeTenant, remainingTenant] }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/app/workspace", {
      ...testSession,
      active_tenant: adminMembership,
      memberships: [adminMembership, remainingMembership],
      permissions: ["tenant.manage", "class.view"],
    });

    await screen.findByRole("heading", { name: "Thông tin workspace" });
    fireEvent.click(screen.getByRole("button", { name: "Lưu trữ workspace" }));
    expect(
      await screen.findByRole("dialog", {
        name: "Xác nhận lưu trữ workspace",
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Xác nhận lưu trữ" }));

    expect(
      await screen.findByRole("heading", {
        name: "Chọn workspace để tiếp tục",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Second Workspace/ }),
    ).toBeInTheDocument();

    const archiveRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/archive"));
    expect(archiveRequest?.headers.get("X-CSRF-Token")).toBe("archive-csrf");
    await expect(archiveRequest?.clone().json()).resolves.toEqual({
      expected_version: 3,
    });
  });

  it("tải danh sách lớp theo workspace đang hoạt động", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            items: [
              {
                id: "a912f628-f3d2-4c18-84c6-42a9e858dc8d",
                owner_user_id: testSession.user.id,
                code: "SEC101",
                title: "Cơ sở An toàn thông tin",
                description: "Lớp học kỳ 1",
                timezone: "Asia/Ho_Chi_Minh",
                status: "draft",
                version: 1,
                archived_at: null,
                created_at: "2026-07-14T04:00:00Z",
                updated_at: "2026-07-14T04:00:00Z",
                viewer_access: {
                  class_role: null,
                  enrollment_status: null,
                  can_update_class: true,
                  can_archive_class: true,
                  can_transfer_ownership: true,
                  can_manage_enrollments: true,
                  can_join_room: false,
                  can_publish_media: false,
                  can_leave: false,
                },
              },
            ],
            next_cursor: null,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    renderRoute("/app/classrooms");

    expect(
      await screen.findByRole("heading", { name: "Lớp học" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("Cơ sở An toàn thông tin"),
    ).toBeInTheDocument();
    expect(screen.getByText("SEC101")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Tạo lớp học" }),
    ).not.toBeInTheDocument();
  });

  it("tạo lớp với CSRF rồi mở trang chi tiết", async () => {
    const createdClass = {
      id: "eaf3ae68-53a3-4f39-b34c-2fc3ad462a47",
      owner_user_id: testSession.user.id,
      code: "NET201",
      title: "Mạng máy tính nâng cao",
      description: "Thực hành theo nhóm",
      timezone: "Asia/Ho_Chi_Minh",
      status: "draft",
      version: 1,
      archived_at: null,
      created_at: "2026-07-14T05:00:00Z",
      updated_at: "2026-07-14T05:00:00Z",
      viewer_access: {
        class_role: null,
        enrollment_status: null,
        can_update_class: true,
        can_archive_class: true,
        can_transfer_ownership: true,
        can_manage_enrollments: true,
        can_join_room: false,
        can_publish_media: false,
        can_leave: false,
      },
    };
    let created = false;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(
          new Response(JSON.stringify({ csrf_token: "class-csrf" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (
        request.url.endsWith(
          `/api/v1/tenants/${testSession.active_tenant?.id}`,
        ) &&
        request.method === "GET"
      ) {
        return Promise.resolve(
          new Response(JSON.stringify({ timezone: "Asia/Ho_Chi_Minh" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (
        request.url.includes("/api/v1/classes") &&
        request.method === "POST"
      ) {
        created = true;
        return Promise.resolve(
          new Response(JSON.stringify(createdClass), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      if (request.url.includes("/api/v1/classes") && request.method === "GET") {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              items: created ? [createdClass] : [],
              next_cursor: null,
            }),
            {
              status: 200,
              headers: { "Content-Type": "application/json" },
            },
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderRoute("/app/classrooms", {
      ...testSession,
      permissions: ["class.view", "class.create"],
    });

    await screen.findByText("Workspace chưa có lớp học");
    fireEvent.click(screen.getByRole("button", { name: "Tạo lớp học" }));
    fireEvent.change(screen.getByRole("textbox", { name: /^Mã lớp/ }), {
      target: { value: "net201" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Tên lớp" }), {
      target: { value: "Mạng máy tính nâng cao" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: /^Mô tả/ }), {
      target: { value: "Thực hành theo nhóm" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Tạo lớp" }));

    expect(
      await screen.findByRole("heading", { name: "Mạng máy tính nâng cao" }),
    ).toBeInTheDocument();
    expect(screen.getByText("NET201")).toBeInTheDocument();

    const createRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find(
        (request) =>
          request.url.endsWith("/api/v1/classes") && request.method === "POST",
      );
    expect(createRequest?.headers.get("X-CSRF-Token")).toBe("class-csrf");
    await expect(createRequest?.clone().json()).resolves.toEqual({
      code: "NET201",
      title: "Mạng máy tính nâng cao",
      description: "Thực hành theo nhóm",
      timezone: "Asia/Ho_Chi_Minh",
    });
  });

  it("ẩn chi tiết lớp ngoài workspace dưới lỗi không tìm thấy", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            type: "about:blank",
            title: "Class not found",
            status: 404,
            detail: "The class does not exist in the active workspace.",
          }),
          {
            status: 404,
            headers: { "Content-Type": "application/problem+json" },
          },
        ),
      ),
    );

    renderRoute("/app/classrooms/a912f628-f3d2-4c18-84c6-42a9e858dc8d");

    expect(
      await screen.findByText("Không tìm thấy lớp học"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "← Danh sách lớp" }),
    ).toBeInTheDocument();
  });

  it("hiển thị trang 404 cho route không tồn tại", async () => {
    renderRoute("/khong-ton-tai");

    expect(
      await screen.findByRole("heading", {
        name: "Không tìm thấy trang bạn yêu cầu",
      }),
    ).toBeInTheDocument();
  });

  it("hiển thị route error có thể phục hồi", async () => {
    renderRoute("/app/system-error");

    expect(
      await screen.findByRole("heading", {
        name: "Hệ thống chưa thể xử lý yêu cầu",
      }),
    ).toBeInTheDocument();
  });

  it("hiển thị trạng thái offline trước khi vào route được bảo vệ", async () => {
    vi.stubGlobal("navigator", { onLine: false });

    renderRoute("/app/home");

    expect(
      await screen.findByRole("heading", { name: "Bạn đang ngoại tuyến" }),
    ).toBeInTheDocument();
  });

  it("chuyển ngôn ngữ vi/en qua i18n provider", () => {
    render(
      <I18nProvider>
        <LanguageProbe />
      </I18nProvider>,
    );

    expect(screen.getByText("vi:Tổng quan")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "English" }));
    expect(screen.getByText("en:Overview")).toBeInTheDocument();
  });
});
