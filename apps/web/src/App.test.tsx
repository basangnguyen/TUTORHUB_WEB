import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, useI18n } from "./app/i18n";
import { createAppRoutes } from "./app/routes";
import { SessionProvider } from "./app/session";
import type { CurrentUser } from "@tutorhub/api-client";

const activeMembership = {
  id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
  slug: "tutorhub-test",
  name: "TutorHub Test",
  role: "teacher" as const,
  is_active: true,
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
  permissions: ["class.view"],
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
