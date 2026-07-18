import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type {
  MembershipInvitation,
  MembershipInvitationListResponse,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { membershipInvitationQueryKeys } from "../app/invitations";
import { MembershipInvitationPanel } from "./MembershipInvitationPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const invitationID = "3b3becce-96d1-456b-afd4-dc17ed2a5240";

const pendingInvitation: MembershipInvitation = {
  id: invitationID,
  tenant_id: tenantID,
  email: "student@example.com",
  intended_role: "student",
  status: "pending",
  expires_at: "2026-07-25T02:00:00Z",
  accepted_at: null,
  revoked_at: null,
  created_at: "2026-07-18T02:00:00Z",
  updated_at: "2026-07-18T02:00:00Z",
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

function renderPanel(fetchMock: ReturnType<typeof vi.fn>) {
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
        <MembershipInvitationPanel tenantID={tenantID} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("MembershipInvitationPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("creates an invitation, updates the list, and keeps the one-time URL out of query caches", async () => {
    const rawToken = "thinv1_A-secure-one-time-token";
    const acceptURL = `https://app.tutorhub.test/invite#token=${rawToken}`;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}/invitations`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse({ items: [] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-create" }));
      }
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}/invitations`) &&
        request.method === "POST"
      ) {
        return Promise.resolve(
          jsonResponse(
            {
              invitation: pendingInvitation,
              accept_url: acceptURL,
            },
            201,
          ),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPanel(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "No invitations yet" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Invite member" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Invitee email" }), {
      target: { value: "  Student@Example.com  " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create invitation" }));

    expect(await screen.findByDisplayValue(acceptURL)).toBeInTheDocument();
    const createRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find(
        (request) =>
          request.method === "POST" &&
          request.url.endsWith(`/api/v1/tenants/${tenantID}/invitations`),
      );
    expect(createRequest?.headers.get("X-CSRF-Token")).toBe("csrf-create");
    await expect(createRequest?.clone().json()).resolves.toEqual({
      email: "student@example.com",
      intended_role: "student",
    });
    expect(
      queryClient.getQueryData<MembershipInvitationListResponse>(
        membershipInvitationQueryKeys.tenantList(tenantID),
      ),
    ).toEqual({ items: [pendingInvitation] });

    expect(
      fetchMock.mock.calls.some((call) =>
        (call[0] as Request).url.includes(rawToken),
      ),
    ).toBe(false);
    expect(
      JSON.stringify(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => ({ data: query.state.data, key: query.queryKey })),
      ),
    ).not.toContain(rawToken);
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

  it("revokes a pending invitation and synchronizes its cached status", async () => {
    const revokedInvitation: MembershipInvitation = {
      ...pendingInvitation,
      status: "revoked",
      revoked_at: "2026-07-18T03:00:00Z",
      updated_at: "2026-07-18T03:00:00Z",
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantID}/invitations`) &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse({ items: [pendingInvitation] }));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-revoke" }));
      }
      if (
        request.url.endsWith(
          `/api/v1/tenants/${tenantID}/invitations/${invitationID}/revoke`,
        ) &&
        request.method === "POST"
      ) {
        return Promise.resolve(jsonResponse(revokedInvitation));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderPanel(fetchMock);

    expect(await screen.findByText("student@example.com")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Revoke invitation for student@example.com",
      }),
    );
    expect(
      screen.getByRole("dialog", { name: "Revoke invitation?" }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirm revoke" }));

    expect(
      await screen.findByText("Invitation for student@example.com revoked."),
    ).toBeInTheDocument();
    expect(screen.getByText("Revoked")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: "Revoke invitation for student@example.com",
      }),
    ).not.toBeInTheDocument();
    expect(
      queryClient.getQueryData<MembershipInvitationListResponse>(
        membershipInvitationQueryKeys.tenantList(tenantID),
      ),
    ).toEqual({ items: [revokedInvitation] });

    const revokeRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.url.endsWith(`/${invitationID}/revoke`));
    expect(revokeRequest?.headers.get("X-CSRF-Token")).toBe("csrf-revoke");
  });
});
