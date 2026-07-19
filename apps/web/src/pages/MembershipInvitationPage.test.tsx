import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
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
import { membershipInvitationQueryKeys } from "../app/invitations";
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
              <Route path="/app/home" element={<h1>Home</h1>} />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function renderAcceptedPage(user: CurrentUser | null) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  const fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: user }}>
          <MemoryRouter
            initialEntries={[
              {
                pathname: "/invite/accepted",
                state: {
                  tenantID,
                  tenantName: "TutorHub Test",
                },
              },
            ]}
          >
            <Routes>
              <Route
                path="/invite/accepted"
                element={<MembershipInvitationAcceptedPage />}
              />
              <Route path="/app/home" element={<h1>Home</h1>} />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { fetchMock, queryClient };
}

describe("MembershipInvitationPage", () => {
  afterEach(() => {
    cleanup();
    window.history.replaceState(null, "", "/");
    window.localStorage.clear();
    window.sessionStorage.clear();
    vi.restoreAllMocks();
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
    const targetAuditKey = ["audit", tenantID, "list"] as const;
    const otherAuditKey = [
      "audit",
      "8d08d79d-5b50-4ddf-bbe7-87b13654c908",
      "list",
    ] as const;
    queryClient.setQueryData(targetAuditKey, ["target-event"]);
    queryClient.setQueryData(otherAuditKey, ["other-event"]);

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
    expect(queryClient.getQueryState(targetAuditKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(otherAuditKey)?.isInvalidated).toBe(false);
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
    expect(
      screen.getByRole("link", { name: "Continue to TutorHub" }),
    ).toHaveAttribute("href", "/app/home");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps the consumed token when retrying the preview after an offline state", async () => {
    const rawToken = "thinv1_offline-retry-token";
    window.history.replaceState(null, "", `/invite#token=${rawToken}`);
    const online = vi
      .spyOn(window.navigator, "onLine", "get")
      .mockReturnValue(false);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/preview")) {
        return Promise.resolve(jsonResponse(preview));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderInvitationPage(fetchMock);

    expect(screen.getByText("You are offline")).toBeInTheDocument();
    expect(window.location.hash).toBe("");
    expect(fetchMock).not.toHaveBeenCalled();

    online.mockReturnValue(true);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByText("TutorHub Test")).toBeInTheDocument();
    const previewRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith("/preview"));
    await expect(previewRequest?.clone().json()).resolves.toEqual({
      token: rawToken,
    });
  });

  it.each([400, 404, 410])(
    "conceals cached invitation preview details after terminal refresh %s",
    async (status) => {
      const rawToken = `thinv1_terminal-refresh-${status}`;
      window.history.replaceState(null, "", `/invite#token=${rawToken}`);
      let reads = 0;
      const fetchMock = vi.fn().mockImplementation((request: Request) => {
        if (request.url.endsWith("/preview")) {
          reads += 1;
          return Promise.resolve(
            reads === 1
              ? jsonResponse(preview)
              : jsonResponse(
                  {
                    title: "Invitation unavailable",
                    status,
                  },
                  status,
                ),
          );
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      });
      const queryClient = renderInvitationPage(fetchMock);

      expect(await screen.findByText(preview.tenant_name)).toBeInTheDocument();
      expect(screen.getByText(preview.masked_email)).toBeInTheDocument();
      await queryClient.refetchQueries({
        exact: true,
        queryKey: membershipInvitationQueryKeys.preview,
      });

      expect(
        await screen.findByRole("heading", { name: "Invitation unavailable" }),
      ).toBeInTheDocument();
      expect(screen.queryByText(preview.tenant_name)).not.toBeInTheDocument();
      expect(screen.queryByText(preview.masked_email)).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Accept invitation" }),
      ).not.toBeInTheDocument();
    },
  );

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
              type: "urn:tutorhub:problem:http-403",
              title: "Invitation identity mismatch",
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
      screen.getByRole("button", { name: "Use another account" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Retry acceptance" }),
    ).not.toBeInTheDocument();
  });

  it("keeps a non-mismatch 403 retryable instead of signing out the user", async () => {
    const rawToken = "thinv1_csrf-verification-token";
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
              type: "urn:tutorhub:problem:http-403",
              title: "Request verification failed",
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
      "The invitation could not be accepted. Try again.",
    );
    expect(
      screen.getByRole("button", { name: "Retry acceptance" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Use another account" }),
    ).not.toBeInTheDocument();
  });

  it("offers a safe home action when acceptance becomes unavailable", async () => {
    const rawToken = "thinv1_expired-during-accept-token";
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
              type: "urn:tutorhub:problem:invitation-expired",
              title: "Invitation expired",
              status: 410,
            },
            410,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderInvitationPage(fetchMock);

    await screen.findByText("TutorHub Test");
    fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The link is invalid, expired, already used, or revoked.",
    );
    expect(
      screen.getByRole("link", { name: "Continue to TutorHub" }),
    ).toHaveAttribute("href", "/app/home");
    expect(
      screen.queryByRole("button", { name: "Retry acceptance" }),
    ).not.toBeInTheDocument();
  });

  it("passes the accepted tenant to a recoverable workspace switch", async () => {
    const rawToken = "thinv1_switch-workspace-token";
    window.history.replaceState(null, "", `/invite#token=${rawToken}`);
    let resolveFirstSwitch: ((response: Response) => void) | undefined;
    let switchAttempts = 0;
    const switchedCurrentUser: CurrentUser = {
      ...acceptedCurrentUser,
      active_tenant: {
        ...acceptedCurrentUser.memberships[0]!,
        is_active: true,
      },
      memberships: acceptedCurrentUser.memberships.map((membership) => ({
        ...membership,
        is_active: membership.id === tenantID,
      })),
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/preview")) {
        return Promise.resolve(jsonResponse(preview));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (request.url.endsWith("/accept")) {
        return Promise.resolve(
          jsonResponse({
            invitation: acceptedInvitation,
            current_user: acceptedCurrentUser,
          }),
        );
      }
      if (request.url.endsWith("/api/v1/session/active-tenant")) {
        switchAttempts += 1;
        if (switchAttempts === 1) {
          return new Promise<Response>((resolve) => {
            resolveFirstSwitch = resolve;
          });
        }
        return Promise.resolve(jsonResponse(switchedCurrentUser));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderInvitationPage(fetchMock);

    await screen.findByText("TutorHub Test");
    fireEvent.click(screen.getByRole("button", { name: "Accept invitation" }));
    expect(
      await screen.findByRole("heading", { name: "Workspace joined" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Switch to this workspace" }),
    );
    expect(
      await screen.findByRole("button", { name: "Switching workspace..." }),
    ).toBeDisabled();

    await act(async () => {
      resolveFirstSwitch?.(
        jsonResponse(
          {
            status: 503,
            title: "Workspace switch unavailable",
          },
          503,
        ),
      );
    });

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "We could not switch workspaces.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(
      await screen.findByRole("heading", { name: "Home" }),
    ).toBeInTheDocument();

    const switchRequests = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .filter((request) =>
        request.url.endsWith("/api/v1/session/active-tenant"),
      );
    expect(switchRequests).toHaveLength(2);
    await expect(switchRequests[0]?.clone().json()).resolves.toEqual({
      tenant_id: tenantID,
    });
  });

  it("does not offer workspace switching when the accepted session expired", () => {
    const { fetchMock } = renderAcceptedPage(null);

    expect(
      screen.getByRole("heading", { name: "Workspace joined" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Your session expired. Sign in again to choose the workspace you joined.",
    );
    expect(
      screen.getByRole("button", { name: "Sign in to TutorHub" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Switch to this workspace" }),
    ).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
