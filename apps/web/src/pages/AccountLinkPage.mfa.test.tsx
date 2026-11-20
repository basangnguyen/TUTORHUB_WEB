import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import {
  APIRequestError,
  type AuthProvidersResponse,
  type CurrentUser,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import type { SessionStatus } from "../app/session";
import { AccountLinkPage } from "./AccountLinkPage";

const { api, sessionStore } = vi.hoisted(() => {
  type Listener = () => void;
  const listeners = new Set<Listener>();
  let snapshot: Record<string, unknown> = {};
  return {
    api: {
      cancelLinkIntent: vi.fn(),
      confirmLinkIntent: vi.fn(),
      getAuthProviders: vi.fn(),
      getMySecurity: vi.fn(),
      getOAuthLinkIntent: vi.fn(),
      loginWithPassword: vi.fn(),
      reauthenticate: vi.fn(),
      rotateCSRFToken: vi.fn(),
      verifyMFA: vi.fn(),
    },
    sessionStore: {
      get: () => snapshot,
      set(next: Record<string, unknown>) {
        snapshot = { ...snapshot, ...next };
        for (const listener of listeners) listener();
      },
      subscribe(listener: Listener) {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
    },
  };
});

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    useSession: () =>
      useSyncExternalStore(sessionStore.subscribe, sessionStore.get),
  };
});

const currentUser: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: null,
  memberships: [],
  permissions: [],
};

const PROVIDERS: AuthProvidersResponse = {
  device_management_enabled: true,
  email_code_enabled: false,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: true,
  oauth_providers: ["google"],
  oidc_default_enabled: false,
  passkeys_enabled: false,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: false,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: false,
};

const actions = {
  completeNativeSignIn: vi.fn(),
  refresh: vi.fn(),
  resetAfterPasswordRecovery: vi.fn(),
  signIn: vi.fn(),
  signOut: vi.fn(),
};

function setSession(status: SessionStatus, user: CurrentUser | null = null) {
  sessionStore.set({ ...actions, currentUser: user, error: null, status });
}

function apiError(status: number, code?: string) {
  return new APIRequestError(status, {
    status,
    title: "Rejected",
    type: "about:blank",
    code,
  });
}

function LandingProbe() {
  const location = useLocation();
  return <p data-testid="landing">{location.pathname}</p>;
}

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) await Promise.resolve();
  });
  await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) await Promise.resolve();
  });
}

async function renderPage() {
  const router = createMemoryRouter(
    [
      { path: "/account-link", element: <AccountLinkPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: ["/account-link?provider=google"] },
  );
  render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            mutations: { retry: false },
            queries: { retry: false },
          },
        })
      }
    >
      <I18nProvider initialLanguage="en">
        <RouterProvider router={router} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await settle();
  return router;
}

describe("AccountLinkPage AUTH-09B", () => {
  beforeEach(() => {
    for (const mock of Object.values(api)) mock.mockReset();
    for (const mock of Object.values(actions)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    api.getOAuthLinkIntent.mockResolvedValue({
      expires_in_seconds: 900,
      masked_email: "t***@example.com",
      provider: "google",
    });
    api.rotateCSRFToken.mockResolvedValue({ csrf_token: "csrf" });
  });

  afterEach(() => {
    cleanup();
  });

  it("finishes an in-place sign-in with the second step and stays to confirm the link", async () => {
    setSession("unauthenticated");
    actions.completeNativeSignIn.mockImplementation(async () => {
      setSession("authenticated", currentUser);
    });
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockResolvedValue({ status: "authenticated" });
    await renderPage();

    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      {
        target: { value: "teacher@example.com" },
      },
    );
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "Correct-horse-battery" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    await settle();

    const step = screen.getByRole("region", { name: "Two-step verification" });
    fireEvent.change(
      within(step).getByLabelText("6-digit code from your authenticator app"),
      { target: { value: "123456" } },
    );
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();

    expect(actions.completeNativeSignIn).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("landing")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Link Google" }),
    ).toBeInTheDocument();
  });

  it("raises an AAL1 session to AAL2 in place and confirms the link once more", async () => {
    setSession("authenticated", currentUser);
    api.confirmLinkIntent
      .mockRejectedValueOnce(apiError(403, "auth_assurance_required"))
      .mockResolvedValueOnce({ return_to: "/app/settings?identity=linked" });
    api.getMySecurity.mockResolvedValue({
      email_verified: true,
      has_password: true,
      mfa_enabled: true,
      reauth_methods: ["totp"],
      recent_auth_expires_at: null,
      recovery_codes_remaining: 10,
    });
    api.reauthenticate.mockResolvedValue({
      recent_auth_expires_at: "2026-11-20T10:10:00Z",
    });
    const router = await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();
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

    await waitFor(() => expect(api.confirmLinkIntent).toHaveBeenCalledTimes(2));
    expect(api.reauthenticate).toHaveBeenCalledWith(
      { code: "123456", method: "totp" },
      "csrf",
      expect.anything(),
    );
    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/app/settings"),
    );
  });

  it("keeps a verify button after a declined AAL2 step-up", async () => {
    setSession("authenticated", currentUser);
    api.confirmLinkIntent.mockRejectedValue(
      apiError(403, "auth_assurance_required"),
    );
    api.getMySecurity.mockResolvedValue({
      email_verified: true,
      has_password: true,
      mfa_enabled: true,
      reauth_methods: ["totp"],
      recent_auth_expires_at: null,
    });
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Cancel" }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "This account requires stronger authentication before linking.",
    );
    fireEvent.click(
      within(alert).getByRole("button", {
        name: "Verify with a second factor",
      }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Confirm it's you" }),
    ).toBeInTheDocument();
    expect(api.confirmLinkIntent).toHaveBeenCalledTimes(1);
  });
});
