import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type {
  ClassroomClass,
  ContentFile,
  CurrentUser,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { tenantCapabilityQueryKeys } from "../app/tenantCapabilities";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import { ClassFilesPanel } from "./ClassFilesPanel";

const tenantID = "8af813a8-f986-45fc-ac1f-a990803101a9";
const userID = "d5b4c632-e1f7-4bbd-be72-ae39cc630eb4";
const classID = "6f430f0e-061c-43b7-9f46-5aba222c4a7a";

const classroom: ClassroomClass = {
  id: classID,
  owner_user_id: userID,
  code: "FILES101",
  title: "Safe files",
  description: "",
  timezone: "UTC",
  status: "active",
  version: 1,
  archived_at: null,
  created_at: "2026-08-08T01:00:00Z",
  updated_at: "2026-08-08T01:00:00Z",
  viewer_access: {
    class_role: "owner",
    enrollment_status: "active",
    can_update_class: true,
    can_archive_class: false,
    can_transfer_ownership: false,
    can_manage_enrollments: true,
    can_schedule_sessions: true,
    can_join_room: true,
    can_publish_media: true,
    can_leave: false,
  },
};

const pendingFile: ContentFile = {
  id: "663eeef2-9e84-4707-b393-92cae18a465f",
  class_id: classID,
  creator_user_id: userID,
  display_name: "lesson.pdf",
  declared_media_type: "application/pdf",
  expected_size_bytes: 4096,
  expected_checksum_sha256: "00".repeat(32),
  status: "pending",
  version: 1,
  upload_expires_at: "2026-08-08T01:15:00Z",
  created_at: "2026-08-08T01:00:00Z",
  updated_at: "2026-08-08T01:00:00Z",
  viewer_access: { can_download: false, can_retry_upload: true },
};

function currentUser(): CurrentUser {
  const tenant = {
    id: tenantID,
    slug: "files-test",
    name: "Files test",
    role: "teacher" as const,
    is_active: true,
    status: "active" as const,
    version: 1,
  };
  return {
    user: {
      id: userID,
      email: "teacher@example.test",
      display_name: "Teacher",
      locale: "vi",
      timezone: "UTC",
    },
    active_tenant: tenant,
    memberships: [tenant],
    permissions: ["class.view"],
  };
}

function renderPanel(response: Response, uploadsEnabled = false) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const baseCapabilities = availableTenantCapabilities(tenantID);
  const capabilities = {
    ...baseCapabilities,
    features: {
      ...baseCapabilities.features,
      file_uploads: {
        configured_enabled: uploadsEnabled,
        enabled: uploadsEnabled,
      },
    },
  };
  queryClient.setQueryData(
    tenantCapabilityQueryKeys.detail(tenantID),
    capabilities,
  );
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: currentUser() }}>
          <ClassFilesPanel classroom={classroom} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("ClassFilesPanel P3-11A", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("keeps upload controls off while showing authorized transfer metadata", async () => {
    renderPanel(
      new Response(
        JSON.stringify({
          items: [pendingFile],
          next_cursor: null,
          viewer_access: { can_upload: true },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    expect(await screen.findByText("lesson.pdf")).toBeInTheDocument();
    expect(
      screen.getByText("File uploads are temporarily off"),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Choose a file")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Download" })).toBeNull();
  });

  it("renders a concealed forbidden state without upload or file details", async () => {
    renderPanel(
      new Response(
        JSON.stringify({
          type: "about:blank",
          title: "File unavailable",
          status: 404,
          code: "file_not_found",
        }),
        {
          status: 404,
          headers: { "Content-Type": "application/problem+json" },
        },
      ),
    );

    expect(
      await screen.findByText("Class files are unavailable"),
    ).toBeInTheDocument();
    expect(screen.queryByText("lesson.pdf")).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  });

  it("offers ready files to an authorized viewer through a fresh capability", async () => {
    const readyFile: ContentFile = {
      ...pendingFile,
      status: "ready",
      version: 3,
      viewer_access: { can_download: true, can_retry_upload: false },
    };
    renderPanel(
      new Response(
        JSON.stringify({
          items: [readyFile],
          next_cursor: null,
          viewer_access: { can_upload: false },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    expect(await screen.findByText("lesson.pdf")).toBeInTheDocument();
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockImplementation((input) => {
      const request = input as Request;
      const path = new URL(request.url).pathname;
      if (path.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(
          new Response(JSON.stringify({ csrf_token: "download-csrf" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }
      return Promise.resolve(
        new Response(
          JSON.stringify({
            method: "GET",
            url: "https://storage.example.test/safe-download",
            expires_at: "2026-08-08T01:02:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    });
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);

    fireEvent.click(screen.getByRole("button", { name: "Download" }));

    await waitFor(() => expect(click).toHaveBeenCalledOnce());
    const capabilityRequest = fetchMock.mock.calls.at(-1)?.[0] as Request;
    expect(capabilityRequest.method).toBe("POST");
    expect(capabilityRequest.headers.get("X-CSRF-Token")).toBe("download-csrf");
    expect(capabilityRequest.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
  });
});
