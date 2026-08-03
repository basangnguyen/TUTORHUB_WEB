import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type {
  Conversation,
  CurrentUser,
  TenantCapabilities,
} from "@tutorhub/api-client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { tenantCapabilityQueryKeys } from "../app/tenantCapabilities";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import { ClassConversationAction } from "./ClassConversationAction";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const conversationID = "4cbcb21e-008f-4693-ab95-a5bc952802df";

const session: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
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
  memberships: [],
  permissions: ["tenant.view"],
};

const conversation: Conversation = {
  id: conversationID,
  kind: "class",
  class_id: classID,
  class_status: "active",
  title: "Cơ sở An toàn thông tin",
  participants: [],
  viewer_access: { can_post_messages: true },
  created_at: "2026-08-03T09:00:00Z",
  updated_at: "2026-08-03T09:00:00Z",
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

function renderAction(
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
          <MemoryRouter initialEntries={[`/app/classrooms/${classID}`]}>
            <Routes>
              <Route
                element={<ClassConversationAction classID={classID} />}
                path="/app/classrooms/:classId"
              />
              <Route
                element={<h1>Conversation destination</h1>}
                path="/app/messages/:conversationId"
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("ClassConversationAction", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("rotates CSRF, ensures the authoritative class conversation, and navigates", async () => {
    const requests: Request[] = [];
    const capabilities = availableTenantCapabilities(tenantID);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const path = new URL(request.url).pathname;
      if (path.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "class-csrf" }));
      }
      if (path.endsWith(`/api/v1/classes/${classID}/conversation`)) {
        return Promise.resolve(jsonResponse(conversation, 201));
      }
      if (path.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(jsonResponse(capabilities));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderAction(fetchMock, capabilities);

    fireEvent.click(screen.getByRole("button", { name: "Mở trao đổi lớp" }));

    expect(
      await screen.findByRole("heading", { name: "Conversation destination" }),
    ).toBeInTheDocument();
    const ensureRequest = requests.find((request) =>
      new URL(request.url).pathname.endsWith(
        `/api/v1/classes/${classID}/conversation`,
      ),
    );
    expect(ensureRequest?.headers.get("X-CSRF-Token")).toBe("class-csrf");
    expect(ensureRequest?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
  });

  it("disables creation without hiding the class page when the feature is off", () => {
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
    const fetchMock = vi.fn();
    renderAction(fetchMock, capabilities);

    expect(
      screen.getByRole("heading", { name: "Trao đổi lớp học" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Mở trao đổi lớp" }),
    ).toBeDisabled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("conceals an inaccessible class conversation", async () => {
    const capabilities = availableTenantCapabilities(tenantID);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const path = new URL(request.url).pathname;
      if (path.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "class-csrf" }));
      }
      if (path.endsWith(`/api/v1/classes/${classID}/conversation`)) {
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
      if (path.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(jsonResponse(capabilities));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderAction(fetchMock, capabilities);

    fireEvent.click(screen.getByRole("button", { name: "Mở trao đổi lớp" }));

    expect(
      await screen.findByText(
        "Trao đổi lớp không khả dụng hoặc bạn không còn quyền truy cập.",
      ),
    ).toHaveAttribute("role", "alert");
  });
});
