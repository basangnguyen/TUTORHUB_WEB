import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ClassroomClass, CurrentUser } from "@tutorhub/api-client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { ClassroomPreJoinPage } from "./LiveKitPages";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
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
    can_manage_enrollments: true,
    can_join_room: true,
    can_publish_media: true,
    can_leave: false,
  },
};

const currentUser: CurrentUser = {
  user: {
    id: classroom.owner_user_id,
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
  permissions: ["class.view", "session.join"],
};

function renderPreJoin(
  classValue: ClassroomClass,
  fetchMock: ReturnType<typeof vi.fn>,
  currentUserValue: CurrentUser = currentUser,
) {
  vi.stubGlobal("fetch", fetchMock);
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="vi">
        <SessionProvider
          mode={{ kind: "static", currentUser: currentUserValue }}
        >
          <MemoryRouter
            initialEntries={[`/app/classrooms/${classValue.id}/prejoin`]}
          >
            <Routes>
              <Route
                element={<ClassroomPreJoinPage />}
                path="/app/classrooms/:classId/prejoin"
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("ClassroomPreJoinPage class lifecycle guard", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it.each(["draft", "archived"] as const)(
    "blocks direct prejoin for a %s class before requesting a media token",
    async (status) => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            ...classroom,
            status,
            archived_at: status === "archived" ? classroom.updated_at : null,
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
      );

      renderPreJoin({ ...classroom, status }, fetchMock);

      expect(
        await screen.findByText(
          "Chỉ lớp đang hoạt động mới có thể mở phòng học trực tuyến.",
        ),
      ).toBeInTheDocument();
      expect(fetchMock).toHaveBeenCalledTimes(1);
      const request = fetchMock.mock.calls[0]?.[0] as Request;
      expect(request.method).toBe("GET");
      expect(request.url).toContain(`/api/v1/classes/${classID}`);
    },
  );

  it("keeps the active-class listen-only prejoin route available", async () => {
    const listenOnlyClass = {
      ...classroom,
      viewer_access: {
        ...classroom.viewer_access,
        can_publish_media: false,
      },
    };
    const tenantPublisher = {
      ...currentUser,
      permissions: [...currentUser.permissions, "media.publish" as const],
    };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(listenOnlyClass), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    renderPreJoin(listenOnlyClass, fetchMock, tenantPublisher);

    expect(
      await screen.findByRole("heading", {
        name: "Tham gia ở chế độ chỉ nghe",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Vào phòng học" }),
    ).toBeInTheDocument();
  });

  it("uses per-class publish access for an enrolled student without a tenant media permission", async () => {
    const enrolledStudent: CurrentUser = {
      ...currentUser,
      active_tenant: currentUser.active_tenant
        ? { ...currentUser.active_tenant, role: "student" }
        : null,
      permissions: ["tenant.view", "enrollment.leave"],
    };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(classroom), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    renderPreJoin(classroom, fetchMock, enrolledStudent);

    expect(
      await screen.findByText(
        /Trình duyệt này không cung cấp đầy đủ API camera/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Tham gia ở chế độ chỉ nghe" }),
    ).not.toBeInTheDocument();
  });
});
