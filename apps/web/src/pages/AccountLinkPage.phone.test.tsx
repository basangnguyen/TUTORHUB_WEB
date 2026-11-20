import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import type {
  AuthProvidersResponse,
  CurrentUser,
  PhoneChallengeResponse,
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
      getOAuthLinkIntent: vi.fn(),
      rotateCSRFToken: vi.fn(),
      startPhoneLogin: vi.fn(),
      verifyMFA: vi.fn(),
      verifyPhoneLogin: vi.fn(),
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
  phone_enabled: true,
  signup_enabled: true,
  sso_domains_enabled: false,
};

const CHALLENGE: PhoneChallengeResponse = {
  challenge_id: "phone-1",
  cooldown_seconds: 60,
  expires_in_seconds: 300,
  masked_destination: "+84 ***5678",
  resend_after_seconds: 60,
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

async function signInWithPhone() {
  fireEvent.click(
    screen.getByRole("button", { name: "Log in with a phone number" }),
  );
  await settle();
  const form = screen.getByRole("form", {
    name: "Log in with a code sent to your phone",
  });
  fireEvent.change(
    within(form).getByRole("textbox", { name: "Phone number" }),
    {
      target: { value: "0912345678" },
    },
  );
  fireEvent.click(within(form).getByRole("button", { name: "Send code" }));
  await settle();
  fireEvent.change(
    within(
      screen.getByRole("form", {
        name: "Log in with a code sent to your phone",
      }),
    ).getByRole("textbox", { name: "6-digit code" }),
    { target: { value: "123456" } },
  );
  fireEvent.click(screen.getByRole("button", { name: "Verify and log in" }));
  await settle();
}

describe("AccountLinkPage AUTH-10B phone sign-in", () => {
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
    api.startPhoneLogin.mockResolvedValue(CHALLENGE);
    setSession("unauthenticated");
    actions.completeNativeSignIn.mockImplementation(async () => {
      setSession("authenticated", currentUser);
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("signs in with a phone code in place and stays to confirm the link", async () => {
    api.verifyPhoneLogin.mockResolvedValue({ status: "authenticated" });
    await renderPage();
    // No social buttons on the linking screen; the phone switch is there.
    expect(
      screen.queryByRole("button", { name: "Sign in with Google" }),
    ).toBeNull();
    await signInWithPhone();

    expect(api.startPhoneLogin).toHaveBeenCalledWith(
      { country_code: "VN", keep_signed_in: false, phone_number: "0912345678" },
      expect.anything(),
    );
    expect(api.verifyPhoneLogin).toHaveBeenCalledWith(
      { challenge_id: "phone-1", code: "123456", keep_signed_in: false },
      expect.anything(),
    );
    expect(actions.completeNativeSignIn).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("landing")).toBeNull();
    expect(
      screen.getByRole("heading", { name: "Confirm linking" }),
    ).toHaveFocus();
    expect(
      screen.getByRole("button", { name: "Link Google" }),
    ).toBeInTheDocument();
  });

  it("continues with the second step and still stays on the page", async () => {
    api.verifyPhoneLogin.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockResolvedValue({ status: "authenticated" });
    await renderPage();
    await signInWithPhone();

    expect(actions.completeNativeSignIn).not.toHaveBeenCalled();
    const step = screen.getByRole("region", { name: "Two-step verification" });
    fireEvent.change(
      within(step).getByLabelText("6-digit code from your authenticator app"),
      { target: { value: "135790" } },
    );
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();

    expect(actions.completeNativeSignIn).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("landing")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Link Google" }),
    ).toBeInTheDocument();
  });
});
