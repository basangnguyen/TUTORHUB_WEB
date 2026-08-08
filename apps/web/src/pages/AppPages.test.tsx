import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { CurrentUser } from "@tutorhub/api-client";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { DashboardPage } from "./AppPages";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const classID = "fda36d51-27ef-4b0c-a1cb-7c78b3d899ec";

const currentUser: CurrentUser = {
  user: {
    id: userID,
    email: "teacher@example.test",
    display_name: "TutorHub Teacher",
    locale: "vi",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "p3-12",
    name: "P3-12 Workspace",
    role: "teacher",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: ["tenant.view", "class.view"],
};

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderDashboard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider>
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <MemoryRouter>
            <DashboardPage />
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("P3-12 Home dashboard", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("keeps healthy cards usable when notifications fail and searches authorized metadata", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url, "https://tutorhub.test");
      if (url.pathname.endsWith("/health")) {
        return Promise.resolve(
          json({
            status: "ok",
            service: "tutorhub-core-api",
            environment: "test",
            timestamp: "2026-08-08T08:00:00Z",
          }),
        );
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(
          json({
            items: [
              {
                id: "session-1",
                title: "Đại số nâng cao",
                class_title: "Toán 12",
                starts_at: "2026-08-09T01:00:00Z",
              },
            ],
            next_cursor: null,
          }),
        );
      }
      if (url.pathname.endsWith("/api/v1/notifications/unread-count")) {
        return Promise.resolve(
          json(
            {
              type: "about:blank",
              title: "Forbidden",
              status: 403,
              code: "notification_forbidden",
            },
            403,
          ),
        );
      }
      if (url.pathname.endsWith("/api/v1/conversations")) {
        return Promise.resolve(
          json({
            items: [
              {
                id: "conversation-1",
                unread_count: 3,
                unread_count_capped: false,
              },
            ],
          }),
        );
      }
      if (url.pathname.endsWith("/api/v1/home/recent-files")) {
        return Promise.resolve(
          json({
            items: [
              {
                id: "file-1",
                class_id: classID,
                class_title: "Toán 12",
                display_name: "bai-tap.pdf",
                declared_media_type: "application/pdf",
                size_bytes: 42000,
                updated_at: "2026-08-08T08:00:00Z",
              },
            ],
          }),
        );
      }
      if (url.pathname.endsWith("/api/v1/search")) {
        return Promise.resolve(
          json({
            items: [
              {
                kind: "session",
                id: "7ff59f62-5647-4d68-a221-3a2135a2fd74",
                class_id: classID,
                title: "Đại số nâng cao",
                context: "Toán 12",
                occurred_at: "2026-08-08T08:00:00Z",
              },
            ],
          }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderDashboard();

    expect(
      await screen.findByRole("link", { name: "Đại số nâng cao" }),
    ).toHaveAttribute("href", "/app/calendar");
    expect(screen.getByRole("link", { name: "bai-tap.pdf" })).toHaveAttribute(
      "href",
      `/app/classrooms/${classID}#class-files-title`,
    );
    expect(screen.getByLabelText("3 mục chưa đọc")).toBeInTheDocument();
    expect(
      await screen.findByRole("button", { name: "Thử lại Thông báo" }),
    ).toBeInTheDocument();

    const search = screen.getByRole("searchbox", { name: "Từ khóa tìm kiếm" });
    fireEvent.change(search, { target: { value: "Đại số" } });
    expect(
      await screen.findByRole("list", {
        name: "Kết quả tìm kiếm được phép",
      }),
    ).toBeInTheDocument();

    await waitFor(() => {
      const privateRequests = fetchMock.mock.calls
        .map((call) => call[0] as Request)
        .filter((request) => request.url.includes("/api/v1/"));
      expect(privateRequests.length).toBeGreaterThanOrEqual(5);
      for (const request of privateRequests) {
        expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
          tenantID,
        );
      }
    });
  });
});
