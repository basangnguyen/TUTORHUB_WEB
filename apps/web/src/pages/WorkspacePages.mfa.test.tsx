import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type {
  AccountSecurityResponse,
  AuthProvidersResponse,
  CurrentUser,
} from "@tutorhub/api-client";
import { createMemoryRouter } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { createAppRoutes, isTenantlessPath } from "../app/routes";
import { SessionProvider } from "../app/session";

const adminMembership = {
  id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
  slug: "school-admin",
  name: "School Admin",
  role: "org_admin" as const,
  is_active: true,
  status: "active" as const,
  version: 1,
};

const user = {
  id: "be85eb92-0f18-4163-85ba-50e4d343d632",
  email: "admin@example.com",
  display_name: "School Administrator",
  locale: "en",
  timezone: "Asia/Ho_Chi_Minh",
};

/** AUTH_REQUIRE_ADMIN_MFA hides the org_admin workspace from an AAL1 session. */
const aal1Admin: CurrentUser = {
  user,
  active_tenant: null,
  memberships: [adminMembership],
  permissions: [],
};

const providers: AuthProvidersResponse = {
  device_management_enabled: true,
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: true,
  oauth_providers: [],
  oidc_default_enabled: false,
  passkeys_enabled: false,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: true,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: false,
};

const security: AccountSecurityResponse = {
  email_verified: true,
  has_password: true,
  mfa_enabled: true,
  passkey_count: 0,
  reauth_methods: ["totp"],
  recent_auth_expires_at: null,
  recovery_codes_remaining: 10,
};

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function problem(status: number, code?: string) {
  return new Response(
    JSON.stringify({ type: "about:blank", title: "Rejected", status, code }),
    { status, headers: { "Content-Type": "application/problem+json" } },
  );
}

function installMock({
  createTenant = [],
  switchTenant = [],
}: {
  createTenant?: Array<() => Response>;
  switchTenant?: Array<() => Response>;
} = {}) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      calls.push(`${request.method} ${path}`);
      if (request.method === "GET") {
        switch (path) {
          case "/api/v1/me/profile":
            return json({ user });
          case "/api/v1/me/identities":
            return json({ identities: [] });
          case "/api/v1/me/password":
            return json({ has_password: true, password_enabled: true });
          case "/api/v1/auth/providers":
            return json(providers);
          case "/api/v1/me/security":
            return json(security);
          case "/api/v1/me/login-methods":
            return json({
              identities: [],
              passkey_count: 0,
              password: { has_password: true },
            });
          case "/api/v1/me/sessions":
            return json({ sessions: [] });
          case "/api/v1/auth/csrf":
            return json({ csrf_token: "csrf" });
        }
      }
      if (path === "/api/v1/session/active-tenant") {
        return (
          switchTenant.shift() ??
          (() =>
            json({
              ...aal1Admin,
              active_tenant: adminMembership,
              permissions: ["tenant.view"],
            }))
        )();
      }
      if (path === "/api/v1/tenants" && request.method === "POST") {
        return (createTenant.shift() ?? (() => problem(503)))();
      }
      if (path === "/api/v1/auth/reauth" && request.method === "POST") {
        return json({ recent_auth_expires_at: "2026-11-20T10:10:00Z" });
      }
      // The dashboard behind a successful switch is outside this test.
      return problem(503);
    }),
  );
  return { calls };
}

function renderRoute(path: string, session: CurrentUser = aal1Admin) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  });
  const router = createMemoryRouter(createAppRoutes(), {
    initialEntries: [path],
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: session }}>
          <RouterProvider router={router} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return router;
}

describe("AUTH-09B workspace gates and administrator MFA", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("treats only account settings as tenantless", () => {
    expect(isTenantlessPath("/app/settings")).toBe(true);
    expect(isTenantlessPath("/app/settings/security")).toBe(true);
    expect(isTenantlessPath("/app/settingsx")).toBe(false);
    expect(isTenantlessPath("/app/home")).toBe(false);
  });

  it("keeps account settings reachable without an active workspace", async () => {
    installMock();
    renderRoute("/app/settings");

    expect(
      await screen.findByRole("heading", { name: "Profile and identities" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("region", { name: "Authenticator app" }),
    ).toBeInTheDocument();
    // Only workspace-free navigation is offered, plus a way back.
    const navigation = screen.getByRole("navigation", { name: /navigation/i });
    expect(
      within(navigation)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(["Settings"]);
    const notice = screen.getByRole("region", {
      name: "No workspace selected",
    });
    expect(
      within(notice).getByRole("link", {
        name: "Choose or create a workspace",
      }),
    ).toHaveAttribute("href", "/app/home");
    expect(screen.queryByRole("combobox", { name: /workspace/i })).toBeNull();
  });

  it("links the workspace chooser to account settings", async () => {
    installMock();
    const router = renderRoute("/app/home");

    expect(
      await screen.findByRole("heading", {
        name: "Choose a workspace to continue",
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link", { name: "Account settings" }));
    expect(
      await screen.findByRole("heading", { name: "Profile and identities" }),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/app/settings");
  });

  it("explains auth_admin_mfa_required, verifies in place and retries the switch once", async () => {
    const { calls } = installMock({
      switchTenant: [() => problem(403, "auth_admin_mfa_required")],
    });
    renderRoute("/app/home");

    fireEvent.click(
      await screen.findByRole("button", { name: /School Admin/ }),
    );
    const notice = await screen.findByRole("alert");
    expect(notice).toHaveTextContent(
      "This admin workspace needs two-step verification",
    );
    expect(
      within(notice).getByRole("link", {
        name: "Set up two-step verification",
      }),
    ).toHaveAttribute("href", "/app/settings");

    fireEvent.click(within(notice).getByRole("button", { name: "Verify now" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    expect(dialog).toHaveAccessibleDescription(/needs two-step verification/);
    fireEvent.change(
      await within(dialog).findByLabelText("Authenticator code"),
      {
        target: { value: "123456" },
      },
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    await waitFor(() =>
      expect(
        calls.filter((call) => call === "PUT /api/v1/session/active-tenant"),
      ).toHaveLength(2),
    );
    expect(calls).toContain("POST /api/v1/auth/reauth");
    await waitFor(() =>
      expect(
        screen.queryByRole("heading", {
          name: "Choose a workspace to continue",
        }),
      ).toBeNull(),
    );
  });

  it("explains auth_admin_mfa_required when creating a workspace", async () => {
    installMock({
      createTenant: [() => problem(403, "auth_admin_mfa_required")],
    });
    renderRoute("/app/home", { ...aal1Admin, memberships: [] });

    fireEvent.change(
      await screen.findByRole("textbox", {
        name: "Organization or learning group name",
      }),
      { target: { value: "North Campus" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Create workspace" }));
    const notice = await screen.findByRole("alert");
    expect(notice).toHaveTextContent(
      "This admin workspace needs two-step verification",
    );
    expect(
      within(notice).getByRole("button", { name: "Verify now" }),
    ).toBeInTheDocument();
  });
});
