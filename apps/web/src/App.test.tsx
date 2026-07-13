import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, useI18n } from "./app/i18n";
import { createAppRoutes } from "./app/routes";
import { SessionProvider } from "./app/session";
import type { CurrentUser } from "@tutorhub/api-client";

const testSession: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "vi",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role: "teacher",
    is_active: true,
  },
  memberships: [],
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
