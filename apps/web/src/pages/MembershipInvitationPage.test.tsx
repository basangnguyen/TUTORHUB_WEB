import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type {
  CurrentUser,
  MembershipInvitation,
  MembershipInvitationPreview,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import {
  MembershipInvitationAcceptedPage,
  MembershipInvitationPage,
} from "./MembershipInvitationPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";

const currentUser: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
    email: "student@example.com",
    display_name: "TutorHub Student",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: null,
  memberships: [],
  permissions: [],
};

const preview: MembershipInvitationPreview = {
  tenant_name: "TutorHub Test",
  masked_email: "s*****t@example.com",
  intended_role: "student",
  status: "pending",
  expires_at: "2026-07-25T02:00:00Z",
};

const acceptedInvitation: MembershipInvitation = {
  id: "3b3becce-96d1-456b-afd4-dc17ed2a5240",
  tenant_id: tenantID,
  email: "student@example.com",
  intended_role: "student",
  status: "accepted",
  expires_at: preview.expires_at,
  accepted_at: "2026-07-18T03:00:00Z",
  revoked_at: null,
  created_at: "2026-07-18T02:00:00Z",
  updated_at: "2026-07-18T03:00:00Z",
};

const acceptedCurrentUser: CurrentUser = {
  ...currentUser,
  memberships: [
    {
      id: tenantID,
      slug: "tutorhub-test",
      name: "TutorHub Test",
      role: "student",
      is_active: false,
      status: "active",
      version: 1,
    },
  ],
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

function renderInvitationPage(
  fetchMock: ReturnType<typeof vi.fn>,
  user: CurrentUser | null = currentUser,
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: user }}>
          <MemoryRouter initialEntries={["/invite"]}>
            <Routes>
              <Route path="/invite" element={<MembershipInvitationPage />} />
              <Route
                path="/invite/accepted"
                element={<MembershipInvitationAcceptedPage />}
              />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("MembershipInvitationPage", () => {
  afterEach(() => {
    cleanup();
    window.history.replaceState(null, "", "/");
    window.localStorage.clear();
    window.sessionStorage.clear();
    vi.unstubAllGlobals();
  });

  it("consumes the fragment token and submits it only in preview and accept bodies", async () => {
    const rawToken = "thinv1_A-secure-one-time-token";
    window.history.replaceState(null, "", `/invite#token=${rawToken}`);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith("/api/v1/membership-invitations/preview") &&
        request.method === "POST"
      ) {
        return Promise.resolve(jsonResponse(preview));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-accept" }));
      }
      if (
        request.url.endsWith("/api/v1/membership-invitations/accept") &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          jsonResponse({
            invitation: acceptedInvitation,
            current_user: acceptedCurrentUser,
          }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderInvitationPage(fetchMock);
    queryClient.setQueryData(["tenants", "private-cache"], {
      name: "stale tenant",
    });

    expect(await screen.findByText("TutorHub Test")).toBeInTheDocument();
    expect(window.location.hash).toBe("");
    expect(window.location.pathname).toBe("/invite");
    expect(window.localStorage).toHaveLength(0);
    expect(window.sessionStorage).toHaveLength(0);

    const previewRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/preview"));
    await expect(previewRequest?.clone().json()).resolves.toEqual({
      token: rawToken,
    });
    expect(previewRequest?.url).not.toContain(rawToken);
    expect(
      JSON.stringify(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => query.queryKey),
      ),
    ).not.toContain(rawToken);

    fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));

    expect(
      await screen.findByRole("heading", { name: "Workspace joined" }),
    ).toBeInTheDocument();
    const acceptRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/accept"));
    expect(acceptRequest?.headers.get("X-CSRF-Token")).toBe("csrf-accept");
    await expect(acceptRequest?.clone().json()).resolves.toEqual({
      token: rawToken,
    });
    expect(
      fetchMock.mock.calls.some((call) =>
        (call[0] as Request).url.includes(rawToken),
      ),
    ).toBe(false);
    expect(
      queryClient.getQueryData(["tenants", "private-cache"]),
    ).toBeUndefined();
    await waitFor(() =>
      expect(
        JSON.stringify(
          queryClient
            .getMutationCache()
            .getAll()
            .map((mutation) => mutation.state),
        ),
      ).not.toContain(rawToken),
    );
  });

  it("does not call the API when the invitation fragment has no token", () => {
    window.history.replaceState(null, "", "/invite");
    const fetchMock = vi.fn();

    renderInvitationPage(fetchMock, null);

    expect(
      screen.getByRole("heading", { name: "Invitation unavailable" }),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows an account mismatch as a recoverable acceptance error", async () => {
    const rawToken = "thinv1_mismatched-identity-token";
    window.history.replaceState(null, "", `/invite#token=${rawToken}`);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/preview")) {
        return Promise.resolve(jsonResponse(preview));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-accept" }));
      }
      if (request.url.endsWith("/accept")) {
        return Promise.resolve(
          jsonResponse(
            {
              type: "urn:tutorhub:problem:invitation-email-mismatch",
              title: "Invitation email mismatch",
              status: 403,
            },
            403,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderInvitationPage(fetchMock);

    await screen.findByText("TutorHub Test");
    fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The signed-in account does not match this invitation's email.",
    );
    expect(
      screen.getByRole("button", { name: "Retry acceptance" }),
    ).toBeInTheDocument();
  });
});
