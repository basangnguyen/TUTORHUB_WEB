import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type {
  Conversation,
  CurrentUser,
  TenantCapabilities,
} from "@tutorhub/api-client";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { tenantCapabilityQueryKeys } from "../app/tenantCapabilities";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import { ConversationsPage } from "./ConversationsPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const teacherID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const studentID = "53f0dac5-6c10-46ff-bcb8-da03d07bc142";
const directID = "c82ef7ee-0a1b-4e99-b9d5-3ae20858a82e";
const classConversationID = "4cbcb21e-008f-4693-ab95-a5bc952802df";

const session: CurrentUser = {
  user: {
    id: teacherID,
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "vi",
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
  memberships: [
    {
      id: tenantID,
      slug: "tutorhub-test",
      name: "TutorHub Test",
      role: "teacher",
      is_active: true,
      status: "active",
      version: 1,
    },
  ],
  permissions: ["tenant.view"],
};

const directConversation: Conversation = {
  id: directID,
  kind: "direct",
  title: "TutorHub Student",
  participants: [
    { user_id: teacherID, display_name: "TutorHub Teacher" },
    { user_id: studentID, display_name: "TutorHub Student" },
  ],
  viewer_access: { can_post_messages: true },
  created_at: "2026-08-03T09:00:00Z",
  updated_at: "2026-08-03T09:00:00Z",
};

const archivedClassConversation: Conversation = {
  id: classConversationID,
  kind: "class",
  class_id: "a912f628-f3d2-4c18-84c6-42a9e858dc8d",
  class_status: "archived",
  title: "Cơ sở An toàn thông tin",
  participants: [],
  viewer_access: { can_post_messages: false },
  created_at: "2026-08-03T08:00:00Z",
  updated_at: "2026-08-03T10:00:00Z",
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

function renderPage(
  path: string,
  fetchMock: ReturnType<typeof vi.fn>,
  capabilities: TenantCapabilities = availableTenantCapabilities(tenantID),
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  queryClient.setQueryData(
    tenantCapabilityQueryKeys.detail(tenantID),
    capabilities,
  );
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="vi">
        <SessionProvider mode={{ kind: "static", currentUser: session }}>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route element={<ConversationsPage />} path="/app/messages" />
              <Route
                element={<ConversationsPage />}
                path="/app/messages/:conversationId"
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function listResponse(items: Conversation[] = [directConversation]) {
  return jsonResponse({ items, next_cursor: null });
}

describe("ConversationsPage", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("renders class history as read-only and focuses the selected heading", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const path = new URL(request.url).pathname;
      if (path.endsWith(`/api/v1/conversations/${classConversationID}`)) {
        return Promise.resolve(jsonResponse(archivedClassConversation));
      }
      if (path.endsWith("/api/v1/conversations")) {
        return Promise.resolve(
          listResponse([directConversation, archivedClassConversation]),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderPage(`/app/messages/${classConversationID}`, fetchMock);

    const heading = await screen.findByRole("heading", {
      name: "Cơ sở An toàn thông tin",
    });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(screen.getByText("Chỉ đọc", { exact: true })).toBeInTheDocument();
    expect(screen.getByText(/Lịch sử được giữ lại/)).toBeInTheDocument();
    expect(
      screen.getByText("Không có thành viên được hiển thị."),
    ).toBeInTheDocument();
  });

  it("keeps history visible while feature controls disable creation", async () => {
    const base = availableTenantCapabilities(tenantID);
    const capabilities: TenantCapabilities = {
      ...base,
      features: {
        ...base.features,
        conversations: {
          configured_enabled: false,
          enabled: false,
        },
      },
      operations: {
        ...base.operations,
        create_conversation: {
          available: false,
          reason: "feature_disabled",
        },
      },
    };
    const fetchMock = vi.fn().mockResolvedValue(listResponse());

    renderPage("/app/messages", fetchMock, capabilities);

    expect(await screen.findByText("TutorHub Student")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Bắt đầu trò chuyện" }),
    ).toBeDisabled();
    expect(screen.getByText(/Tính năng này đang bị tắt/)).toBeInTheDocument();
  });

  it("validates, normalizes, creates, announces, and opens a direct conversation", async () => {
    const requests: Request[] = [];
    const capabilities = availableTenantCapabilities(tenantID);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const path = new URL(request.url).pathname;
      if (path.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(
          jsonResponse({ csrf_token: "conversation-csrf" }),
        );
      }
      if (
        path.endsWith("/api/v1/conversations/direct") &&
        request.method === "POST"
      ) {
        return Promise.resolve(jsonResponse(directConversation, 201));
      }
      if (path.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(jsonResponse(capabilities));
      }
      if (path.endsWith("/api/v1/conversations")) {
        return Promise.resolve(listResponse([]));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderPage("/app/messages", fetchMock, capabilities);

    fireEvent.click(
      await screen.findByRole("button", { name: "Bắt đầu trò chuyện" }),
    );
    const submit = screen.getByRole("button", { name: "Tạo trò chuyện" });
    fireEvent.click(submit);
    expect(await screen.findByText("Hãy nhập email thành viên.")).toBeVisible();

    fireEvent.change(screen.getByLabelText("Email thành viên"), {
      target: { value: " Student@Example.COM " },
    });
    fireEvent.click(submit);

    expect(
      await screen.findByText("Đã mở cuộc trò chuyện TutorHub Student."),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "TutorHub Student" }),
      ).toHaveFocus(),
    );
    const createRequest = requests.find(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/api/v1/conversations/direct"),
    );
    await expect(createRequest?.clone().json()).resolves.toEqual({
      target_member_email: "student@example.com",
    });
  });

  it("conceals a foreign or missing conversation without rendering cached detail", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const path = new URL(request.url).pathname;
      if (path.endsWith(`/api/v1/conversations/${directID}`)) {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:http-404",
              title: "Not found",
              status: 404,
            },
            404,
          ),
        );
      }
      if (path.endsWith("/api/v1/conversations")) {
        return Promise.resolve(listResponse());
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    const queryClient = renderPage(`/app/messages/${directID}`, fetchMock);
    queryClient.setQueryData(
      ["conversations", tenantID, "detail", directID],
      directConversation,
    );
    await queryClient.invalidateQueries({
      queryKey: ["conversations", tenantID, "detail", directID],
    });

    expect(
      await screen.findByRole("heading", {
        name: "Cuộc trò chuyện không khả dụng",
      }),
    ).toBeInTheDocument();
    expect(screen.queryByText("TutorHub Teacher")).not.toBeInTheDocument();
  });
});
