import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { CurrentUser, Tenant } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { tenantQueryKeys } from "../app/workspaces";
import { WorkspaceManagementPage } from "./WorkspaceManagementPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";

const tenant: Tenant = {
  id: tenantID,
  slug: "tutorhub-test",
  name: "TutorHub Test",
  locale: "vi",
  timezone: "Asia/Ho_Chi_Minh",
  status: "active",
  version: 3,
  role: "org_admin",
  is_active: true,
  created_at: "2026-07-18T01:00:00Z",
  updated_at: "2026-07-18T02:00:00Z",
  archived_at: null,
};

function sessionFor(
  role: "org_admin" | "student",
  canManage: boolean,
): CurrentUser {
  const membership = {
    id: tenant.id,
    slug: tenant.slug,
    name: tenant.name,
    role,
    is_active: true,
    status: tenant.status,
    version: tenant.version,
  };
  return {
    user: {
      id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      email: "member@example.com",
      display_name: "TutorHub Member",
      locale: "vi",
      timezone: "Asia/Ho_Chi_Minh",
    },
    active_tenant: membership,
    memberships: [membership],
    permissions: canManage ? ["tenant.manage", "class.view"] : ["class.view"],
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

function renderPage(
  fetchMock: ReturnType<typeof vi.fn>,
  currentUser: CurrentUser,
  language: "vi" | "en" = "vi",
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage={language}>
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <MemoryRouter>
            <WorkspaceManagementPage />
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function successfulReads(tenantValue: Tenant = tenant) {
  return vi.fn().mockImplementation((request: Request) => {
    if (request.url.endsWith(`/api/v1/tenants/${tenantID}`)) {
      return Promise.resolve(jsonResponse(tenantValue));
    }
    if (request.url.endsWith("/api/v1/tenants")) {
      return Promise.resolve(jsonResponse({ items: [tenantValue] }));
    }
    return Promise.reject(new Error(`Unexpected request: ${request.url}`));
  });
}

describe("WorkspaceManagementPage", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("lets every active member view workspace facts without management actions", async () => {
    renderPage(
      successfulReads({ ...tenant, role: "student" }),
      sessionFor("student", false),
    );

    expect(
      await screen.findByRole("heading", { name: "Thông tin workspace" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("TutorHub Test").length).toBeGreaterThan(0);
    expect(screen.getByText("Học viên")).toBeInTheDocument();
    expect(
      screen.getByText("Chỉ quản trị viên được chỉnh sửa"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Lưu cấu hình" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Lưu trữ workspace" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Lời mời thành viên")).not.toBeInTheDocument();
  });

  it("loads membership invitations only when the session grants tenant.manage_members", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith(`/api/v1/tenants/${tenantID}/invitations`)) {
        return Promise.resolve(jsonResponse({ items: [] }));
      }
      if (request.url.endsWith(`/api/v1/tenants/${tenantID}`)) {
        return Promise.resolve(jsonResponse(tenant));
      }
      if (request.url.endsWith("/api/v1/tenants")) {
        return Promise.resolve(jsonResponse({ items: [tenant] }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const admin = sessionFor("org_admin", true);

    renderPage(
      fetchMock,
      {
        ...admin,
        permissions: [...admin.permissions, "tenant.manage_members"],
      },
      "en",
    );

    expect(
      await screen.findByRole("heading", { name: "Member invitations" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "No invitations yet" }),
    ).toBeInTheDocument();
    expect(
      fetchMock.mock.calls.some((call) =>
        (call[0] as Request).url.endsWith(
          `/api/v1/tenants/${tenantID}/invitations`,
        ),
      ),
    ).toBe(true);
  });

  it("shows an audit-history link only when the session grants audit.view", async () => {
    const admin = sessionFor("org_admin", true);
    renderPage(
      successfulReads(),
      { ...admin, permissions: [...admin.permissions, "audit.view"] },
      "en",
    );

    const auditLink = await screen.findByRole("link", {
      name: /View audit log/,
    });
    expect(auditLink).toHaveAttribute("href", "/app/workspace/audit");
  });

  it("updates metadata with expected_version and synchronizes tenant caches", async () => {
    const updatedTenant: Tenant = {
      ...tenant,
      name: "TutorHub Engineering",
      version: 4,
      updated_at: "2026-07-18T03:00:00Z",
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(tenant));
      }
      if (request.url.endsWith("/api/v1/tenants") && request.method === "GET") {
        return Promise.resolve(jsonResponse({ items: [tenant] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}`) &&
        request.method === "PATCH"
      ) {
        return Promise.resolve(jsonResponse(updatedTenant));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPage(fetchMock, sessionFor("org_admin", true));

    const nameInput = await screen.findByRole("textbox", {
      name: "Tên tổ chức hoặc nhóm học",
    });
    fireEvent.change(nameInput, {
      target: { value: "TutorHub Engineering" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));

    expect(
      await screen.findByText("Đã cập nhật workspace."),
    ).toBeInTheDocument();
    expect(queryClient.getQueryData(tenantQueryKeys.list)).toEqual({
      items: [updatedTenant],
    });
    expect(queryClient.getQueryData(tenantQueryKeys.detail(tenantID))).toEqual(
      updatedTenant,
    );
    const updateRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.method === "PATCH");
    await expect(updateRequest?.clone().json()).resolves.toEqual({
      expected_version: 3,
      name: "TutorHub Engineering",
      slug: tenant.slug,
      locale: tenant.locale,
      timezone: tenant.timezone,
    });
  });

  it("recovers from a 409 update by loading the latest server version", async () => {
    const latestTenant: Tenant = {
      ...tenant,
      name: "TutorHub Latest",
      version: 4,
      updated_at: "2026-07-18T03:00:00Z",
    };
    let detailReads = 0;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}`) &&
        request.method === "GET"
      ) {
        detailReads += 1;
        return Promise.resolve(
          jsonResponse(detailReads === 1 ? tenant : latestTenant),
        );
      }
      if (request.url.endsWith("/api/v1/tenants") && request.method === "GET") {
        return Promise.resolve(jsonResponse({ items: [tenant] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (request.method === "PATCH") {
        return Promise.resolve(
          jsonResponse(
            {
              type: "about:blank",
              title: "Workspace changed",
              status: 409,
            },
            409,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPage(fetchMock, sessionFor("org_admin", true));

    const nameInput = await screen.findByRole("textbox", {
      name: "Tên tổ chức hoặc nhóm học",
    });
    fireEvent.change(nameInput, { target: { value: "Stale change" } });
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));

    expect(
      await screen.findByText(/Workspace đã được thay đổi ở nơi khác/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Tải bản mới nhất" }));

    await waitFor(() => expect(nameInput).toHaveValue("TutorHub Latest"));
    expect(detailReads).toBe(2);
  });

  it("keeps the draft base version when a background refetch arrives", async () => {
    const latestTenant: Tenant = {
      ...tenant,
      name: "TutorHub Latest",
      version: 4,
      updated_at: "2026-07-18T03:00:00Z",
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(tenant));
      }
      if (request.url.endsWith("/api/v1/tenants") && request.method === "GET") {
        return Promise.resolve(jsonResponse({ items: [tenant] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (request.method === "PATCH") {
        return Promise.resolve(
          jsonResponse({ status: 409, title: "Workspace changed" }, 409),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPage(fetchMock, sessionFor("org_admin", true));
    const nameInput = await screen.findByRole("textbox", {
      name: "Tên tổ chức hoặc nhóm học",
    });
    fireEvent.change(nameInput, { target: { value: "My unsaved draft" } });

    act(() => {
      queryClient.setQueryData(tenantQueryKeys.detail(tenantID), latestTenant);
    });
    expect(nameInput).toHaveValue("My unsaved draft");
    fireEvent.click(screen.getByRole("button", { name: "Lưu cấu hình" }));

    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          (call) => (call[0] as Request).method === "PATCH",
        ),
      ).toBe(true),
    );
    const updateRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.method === "PATCH");
    await expect(updateRequest?.clone().json()).resolves.toMatchObject({
      expected_version: 3,
      name: "My unsaved draft",
    });
  });

  it("shows a forbidden state with retry when active-tenant detail is denied", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith(`/api/v1/tenants/${tenantID}`)) {
        return Promise.resolve(
          jsonResponse(
            {
              type: "about:blank",
              title: "Workspace access denied",
              status: 403,
            },
            403,
          ),
        );
      }
      if (request.url.endsWith("/api/v1/tenants")) {
        return Promise.resolve(jsonResponse({ items: [tenant] }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPage(fetchMock, sessionFor("student", false));

    expect(
      await screen.findByText("Bạn chưa thể xem workspace này"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Thử lại" })).toBeVisible();
  });
});
