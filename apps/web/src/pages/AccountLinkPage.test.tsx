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
import {
  APIRequestError,
  type AuthProvidersResponse,
  type CurrentUser,
  type OAuthLinkIntentResponse,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import type { SessionStatus } from "../app/session";
import { AccountLinkPage } from "./AccountLinkPage";

const { api, sessionStore } = vi.hoisted(() => {
  type Listener = () => void;
  interface SessionSnapshot {
    completeNativeSignIn: () => Promise<void>;
    currentUser: CurrentUser | null;
    error: Error | null;
    refresh: () => Promise<void>;
    resetAfterPasswordRecovery: () => Promise<void>;
    signIn: (returnTo?: string) => void;
    signOut: (options?: { returnTo?: string }) => Promise<void>;
    status: SessionStatus;
  }
  const listeners = new Set<Listener>();
  let snapshot = {} as SessionSnapshot;
  return {
    api: {
      cancelLinkIntent: vi.fn(),
      confirmLinkIntent: vi.fn(),
      getAuthProviders: vi.fn(),
      getOAuthLinkIntent: vi.fn(),
      loginWithPassword: vi.fn(),
      rotateCSRFToken: vi.fn(),
      startPasskeyAssertion: vi.fn(),
      verifyPasskeyAssertion: vi.fn(),
    },
    sessionStore: {
      get: () => snapshot,
      set(next: Partial<SessionSnapshot>) {
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

const MASKED = "t***@example.com";
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

const NATIVE_PROVIDERS: AuthProvidersResponse = {
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
  oauth_providers: ["google"],
  oidc_default_enabled: true,
  passkeys_enabled: false,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: true,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: false,
};

function intent(
  overrides: Partial<OAuthLinkIntentResponse> = {},
): OAuthLinkIntentResponse {
  return {
    expires_in_seconds: 900,
    masked_email: MASKED,
    provider: "google",
    ...overrides,
  };
}

function apiError(status: number, code?: string, retryAfterSeconds?: number) {
  return new APIRequestError(
    status,
    { status, title: "Rejected", type: "about:blank", code },
    retryAfterSeconds,
  );
}

const actions = {
  completeNativeSignIn: vi.fn(),
  refresh: vi.fn(),
  resetAfterPasswordRecovery: vi.fn(),
  signIn: vi.fn(),
  signOut: vi.fn(),
};

function setSession(status: SessionStatus, user: CurrentUser | null = null) {
  sessionStore.set({
    ...actions,
    currentUser: user,
    error: status === "error" ? new Error("unavailable") : null,
    status,
  });
}

function LandingProbe() {
  const location = useLocation();
  return (
    <p data-testid="landing">{`${location.pathname}${location.search}`}</p>
  );
}

async function flushMicrotasks() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) {
      await Promise.resolve();
    }
  });
}

/** Lets API promises settle, then React Query's setTimeout(0) notifications. */
async function settle() {
  await flushMicrotasks();
  if (vi.isFakeTimers()) {
    act(() => {
      vi.advanceTimersByTime(0);
    });
  } else {
    await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  }
  await flushMicrotasks();
}

async function renderPage({
  language = "en",
  search = "?provider=google",
}: { language?: Language; search?: string } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  const router = createMemoryRouter(
    [
      { path: "/account-link", element: <AccountLinkPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: [`/account-link${search}`] },
  );
  const view = render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage={language}>
        <RouterProvider router={router} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await settle();
  return { ...view, invalidate, router };
}

function storageSnapshot() {
  return JSON.stringify({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  });
}

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  for (const mock of Object.values(api)) mock.mockReset();
  for (const mock of Object.values(actions)) mock.mockReset();
  actions.completeNativeSignIn.mockImplementation(async () => {
    setSession("authenticated", currentUser);
  });
  actions.refresh.mockResolvedValue(undefined);
  actions.resetAfterPasswordRecovery.mockResolvedValue(undefined);
  actions.signOut.mockResolvedValue(undefined);
  api.getAuthProviders.mockResolvedValue(NATIVE_PROVIDERS);
  api.getOAuthLinkIntent.mockResolvedValue(intent());
  api.rotateCSRFToken.mockResolvedValue({ csrf_token: "csrf-1" });
  setSession("unauthenticated");
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("AccountLinkPage preview", () => {
  it("announces loading, then shows the masked email and a live countdown", async () => {
    vi.useFakeTimers();
    let resolveIntent: (value: OAuthLinkIntentResponse) => void = () =>
      undefined;
    api.getOAuthLinkIntent.mockReturnValue(
      new Promise<OAuthLinkIntentResponse>((resolve) => {
        resolveIntent = resolve;
      }),
    );
    await renderPage();

    expect(
      screen.getByRole("status", { name: "Checking the linking request" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Link Google to your TutorHub account",
      }),
    ).toBeInTheDocument();

    await act(async () => resolveIntent(intent()));
    await settle();
    expect(
      screen.getByText(
        `The email ${MASKED} already has a TutorHub account. Sign in to that account to link Google.`,
      ),
    ).toBeInTheDocument();
    const remaining = screen.getByText("15:00");
    expect(remaining.tagName).toBe("TIME");
    expect(remaining).toHaveAttribute("datetime", "PT15M0S");

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByText("14:59")).toBeInTheDocument();
    expect(api.getOAuthLinkIntent).toHaveBeenCalledWith(
      expect.objectContaining({ baseUrl: "/api" }),
    );
  });

  it("turns into the unavailable state when the countdown reaches zero", async () => {
    vi.useFakeTimers();
    api.getOAuthLinkIntent.mockResolvedValue(intent({ expires_in_seconds: 2 }));
    await renderPage();
    expect(screen.getByText("0:02")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(
      screen.getByRole("heading", {
        name: "This linking request is no longer valid",
      }),
    ).toBeInTheDocument();
    expect(screen.queryByText(MASKED, { exact: false })).toBeNull();
  });

  it("explains a missing request (404) and links back to sign-in", async () => {
    api.getOAuthLinkIntent.mockRejectedValue(
      apiError(404, "auth_link_intent_unavailable"),
    );
    await renderPage();

    expect(
      screen.getByRole("heading", {
        name: "This linking request is no longer valid",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Back to sign-in" }),
    ).toHaveAttribute("href", "/sign-in");
    expect(screen.queryByRole("button", { name: /Link Google/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
  });

  it("offers Retry when the request cannot be checked", async () => {
    api.getOAuthLinkIntent
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(intent());
    await renderPage();

    expect(
      screen.getByRole("heading", {
        name: "We couldn't check the linking request",
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await settle();
    expect(api.getOAuthLinkIntent).toHaveBeenCalledTimes(2);
    expect(screen.getByText(MASKED, { exact: false })).toBeInTheDocument();
  });

  it("names single sign-on for an OIDC request and renders Vietnamese", async () => {
    api.getOAuthLinkIntent.mockResolvedValue(intent({ provider: "oidc" }));
    await renderPage({ language: "vi", search: "?provider=oidc" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Liên kết đăng nhập một lần (SSO) với tài khoản TutorHub",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        `Email ${MASKED} đã có tài khoản TutorHub. Đăng nhập đúng tài khoản đó để liên kết đăng nhập một lần (SSO).`,
      ),
    ).toBeInTheDocument();
  });
});

describe("AccountLinkPage Facebook", () => {
  it("names Facebook for a link intent from a pending registration", async () => {
    api.getOAuthLinkIntent.mockResolvedValue(intent({ provider: "facebook" }));
    await renderPage({ search: "?provider=facebook" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Link Facebook to your TutorHub account",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(MASKED, { exact: false })).toHaveTextContent(
      "Facebook",
    );
  });

  it("renders the Facebook label in Vietnamese", async () => {
    api.getOAuthLinkIntent.mockResolvedValue(intent({ provider: "facebook" }));
    await renderPage({ language: "vi", search: "?provider=facebook" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Liên kết Facebook với tài khoản TutorHub",
      }),
    ).toBeInTheDocument();
  });

  it("keeps the Facebook provider while the request loads", async () => {
    api.getOAuthLinkIntent.mockReturnValue(new Promise(() => undefined));
    await renderPage({ search: "?provider=facebook" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Link Facebook to your TutorHub account",
      }),
    ).toBeInTheDocument();
  });
});

describe("AccountLinkPage AUTH-07B Microsoft", () => {
  it("names Microsoft for a link intent and keeps it while loading", async () => {
    api.getOAuthLinkIntent.mockReturnValue(new Promise(() => undefined));
    await renderPage({ search: "?provider=microsoft" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Link Microsoft to your TutorHub account",
      }),
    ).toBeInTheDocument();
  });

  it("shows the masked email of a Microsoft request and signs in in place", async () => {
    api.getOAuthLinkIntent.mockResolvedValue(intent({ provider: "microsoft" }));
    await renderPage({ search: "?provider=microsoft" });

    expect(screen.getByText(MASKED, { exact: false })).toHaveTextContent(
      "Microsoft",
    );
    // The in-place sign-in never offers social buttons, Microsoft included.
    expect(
      screen.getByRole("form", { name: "Log in to TutorHub" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Sign in with Microsoft" }),
    ).not.toBeInTheDocument();
  });

  it("confirms a Microsoft link once signed in", async () => {
    setSession("authenticated", currentUser);
    api.getOAuthLinkIntent.mockResolvedValue(intent({ provider: "microsoft" }));
    api.confirmLinkIntent.mockResolvedValue({
      return_to: "/app/home",
      status: "linked",
    });
    await renderPage({ search: "?provider=microsoft" });

    fireEvent.click(screen.getByRole("button", { name: "Link Microsoft" }));
    await settle();
    expect(api.confirmLinkIntent).toHaveBeenCalledWith(
      "csrf-1",
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
    expect(storageSnapshot()).not.toContain(MASKED);
  });

  it("renders the Microsoft label in Vietnamese", async () => {
    api.getOAuthLinkIntent.mockResolvedValue(intent({ provider: "microsoft" }));
    await renderPage({ language: "vi", search: "?provider=microsoft" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Liên kết Microsoft với tài khoản TutorHub",
      }),
    ).toBeInTheDocument();
  });
});

describe("AccountLinkPage signed out", () => {
  it("offers the enabled methods in place, without social buttons", async () => {
    const { container } = await renderPage();

    expect(
      screen.getByRole("heading", {
        level: 2,
        name: "Sign in to your existing TutorHub account",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Log in with an email code" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Sign in with Google" }),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Continue with Facebook" }),
    ).toBeNull();
    expect(container.querySelector(".sign-in--embedded")).not.toBeNull();
    expect(storageSnapshot()).not.toContain(MASKED);
  });

  it("signs in with a password and stays on the page to confirm", async () => {
    api.loginWithPassword.mockResolvedValue({ status: "authenticated" });
    await renderPage();

    fireEvent.change(
      screen.getByRole("textbox", { name: "Email or username" }),
      { target: { value: "teacher@example.com" } },
    );
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "Correct-Horse-9" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    await settle();

    expect(api.loginWithPassword).toHaveBeenCalledWith(
      {
        identifier: "teacher@example.com",
        keep_signed_in: false,
        password: "Correct-Horse-9",
      },
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(actions.completeNativeSignIn).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("landing")).toBeNull();
    const heading = screen.getByRole("heading", { name: "Confirm linking" });
    expect(heading).toHaveFocus();
    expect(screen.getByText("TutorHub Teacher")).toBeInTheDocument();
    expect(screen.getByText("teacher@example.com")).toBeInTheDocument();
    expect(api.confirmLinkIntent).not.toHaveBeenCalled();
    expect(storageSnapshot()).not.toContain("Correct-Horse-9");
  });

  it("signs in with a passkey and stays on the page to confirm", async () => {
    vi.stubGlobal(
      "PublicKeyCredential",
      Object.assign(function PublicKeyCredential() {}, {
        isConditionalMediationAvailable: () => Promise.resolve(false),
      }),
    );
    const credential = {
      id: "AQID",
      rawId: new Uint8Array([1, 2, 3]).buffer,
      response: {
        authenticatorData: new Uint8Array([4]).buffer,
        clientDataJSON: new Uint8Array([5]).buffer,
        signature: new Uint8Array([6]).buffer,
        userHandle: new Uint8Array([7]).buffer,
      },
      type: "public-key",
    };
    Object.defineProperty(window.navigator, "credentials", {
      configurable: true,
      value: { create: vi.fn(), get: vi.fn(() => Promise.resolve(credential)) },
    });
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      passkeys_enabled: true,
    });
    api.startPasskeyAssertion.mockResolvedValue({
      allowCredentials: [],
      challenge: "Y2hhbGxlbmdl",
      rpId: "localhost",
      userVerification: "preferred",
    });
    api.verifyPasskeyAssertion.mockResolvedValue({ status: "authenticated" });
    try {
      await renderPage();
      expect(
        screen.getByRole("textbox", { name: "Email or username" }),
      ).toHaveAttribute("autocomplete", "username webauthn");

      fireEvent.click(
        screen.getByRole("button", { name: "Sign in with a passkey" }),
      );
      await settle();

      expect(api.verifyPasskeyAssertion).toHaveBeenCalledTimes(1);
      expect(actions.completeNativeSignIn).toHaveBeenCalledTimes(1);
      expect(screen.queryByTestId("landing")).toBeNull();
      expect(
        await screen.findByRole("heading", { name: "Confirm linking" }),
      ).toHaveFocus();
      expect(api.confirmLinkIntent).not.toHaveBeenCalled();
      expect(storageSnapshot()).not.toContain("AQID");
    } finally {
      vi.unstubAllGlobals();
      Reflect.deleteProperty(window.navigator, "credentials");
    }
  });

  it("returns to this screen through the OIDC portal when it is the only method", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...NATIVE_PROVIDERS,
      email_code_enabled: false,
      password_enabled: false,
      password_recovery_enabled: false,
    });
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    expect(actions.signIn).toHaveBeenCalledWith(
      "/account-link?provider=google",
    );
  });

  it("shows Retry when the sign-in options cannot be loaded", async () => {
    api.getAuthProviders
      .mockRejectedValueOnce(new TypeError("Failed to fetch"))
      .mockResolvedValueOnce(NATIVE_PROVIDERS);
    await renderPage();

    expect(
      screen.getByRole("heading", {
        name: "We couldn't load the sign-in options",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "Email or username" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await settle();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toBeInTheDocument();
  });

  it("cancels before signing in and returns to sign-in", async () => {
    api.cancelLinkIntent.mockResolvedValue(undefined);
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await settle();
    expect(api.cancelLinkIntent).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent("/sign-in");
  });
});

describe("AccountLinkPage session states", () => {
  it("waits while the session is checked", async () => {
    setSession("loading");
    await renderPage();

    expect(
      screen.getByRole("button", { name: "Checking your session..." }),
    ).toBeDisabled();
  });

  it("retries an unavailable session check", async () => {
    setSession("error");
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(actions.refresh).toHaveBeenCalledTimes(1);
  });
});

describe("AccountLinkPage confirmation", () => {
  beforeEach(() => {
    setSession("authenticated", currentUser);
  });

  it("links with a fresh CSRF token, resyncs caches and returns to the path", async () => {
    api.confirmLinkIntent.mockResolvedValue({
      return_to: "/app/classrooms?tab=1",
      status: "linked",
    });
    const { invalidate } = await renderPage();

    expect(screen.getByText("Signed-in account")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();

    expect(api.rotateCSRFToken).toHaveBeenCalled();
    expect(api.confirmLinkIntent).toHaveBeenCalledWith(
      "csrf-1",
      expect.objectContaining({ baseUrl: "/api" }),
    );
    for (const queryKey of [
      ["auth", "me"],
      ["security", "overview"],
      ["security", "login-methods"],
      ["security", "sessions"],
      ["profile", "identities"],
    ]) {
      expect(invalidate).toHaveBeenCalledWith({ queryKey });
    }
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/app/classrooms?tab=1",
    );
  });

  it.each([
    ["//evil.example/app", "/app/home"],
    ["https://evil.example", "/app/home"],
    ["/account-link?provider=google", "/app/home"],
    ["/sign-in", "/app/home"],
  ])("re-sanitizes return_to %s", async (returnTo, expected) => {
    api.confirmLinkIntent.mockResolvedValue({
      return_to: returnTo,
      status: "linked",
    });
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();
    expect(screen.getByTestId("landing")).toHaveTextContent(expected);
  });

  it("reports a different signed-in account, keeps the request and offers sign-out", async () => {
    api.confirmLinkIntent.mockRejectedValue(
      apiError(403, "auth_link_intent_mismatch"),
    );
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "You're signed in to a different account than the one this linking request belongs to.",
    );
    expect(screen.getByText(MASKED, { exact: false })).toBeInTheDocument();
    expect(api.getOAuthLinkIntent).toHaveBeenCalledTimes(1);
    fireEvent.click(within(alert).getByRole("button", { name: "Sign out" }));
    expect(actions.signOut).toHaveBeenCalledTimes(1);
    // A native logout keeps the link cookie: come back to this screen.
    expect(actions.signOut).toHaveBeenCalledWith({
      returnTo: "/account-link?provider=google",
    });
  });

  // AUTH-09B: 403 auth_assurance_required opens the AAL2 step-up instead
  // (covered in AccountLinkPage.mfa.test.tsx).
  it.each([
    [
      apiError(401),
      "Your session has expired. Sign in again to continue linking.",
    ],
    [apiError(503), "TutorHub is busy right now. Please try again shortly."],
    [
      new TypeError("Failed to fetch"),
      "Couldn't reach TutorHub. Check your network and try again.",
    ],
    [apiError(403), "That didn't work. Please try again."],
  ])(
    "maps a confirmation failure to a fixed message (%#)",
    async (error, text) => {
      api.confirmLinkIntent.mockRejectedValue(error);
      await renderPage();

      fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
      await settle();
      expect(screen.getByRole("alert")).toHaveTextContent(text);
      expect(screen.queryByTestId("landing")).toBeNull();
    },
  );

  it("shows the unavailable state when the request expired or was used", async () => {
    api.confirmLinkIntent.mockRejectedValue(
      apiError(400, "auth_link_intent_unavailable"),
    );
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();
    expect(
      screen.getByRole("heading", {
        name: "This linking request is no longer valid",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Continue to TutorHub" }),
    ).toHaveAttribute("href", "/app/home");
  });

  it("refreshes the session after a 409 conflict", async () => {
    api.confirmLinkIntent.mockRejectedValue(apiError(409));
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "Your session just changed somewhere else. Refresh it and try again.",
    );
    fireEvent.click(
      within(alert).getByRole("button", { name: "Refresh session" }),
    );
    await settle();
    expect(actions.refresh).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("locks the button for the Retry-After window of a 429", async () => {
    vi.useFakeTimers();
    api.confirmLinkIntent.mockRejectedValueOnce(apiError(429, undefined, 30));
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Link Google" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Too many attempts. Please try again in 30 seconds.",
    );
    expect(
      screen.getByRole("button", { name: "Try again (30s)" }),
    ).toBeDisabled();

    for (let second = 0; second < 30; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    expect(screen.getByRole("button", { name: "Link Google" })).toBeEnabled();
  });

  it("cancels the request and returns to sign-in", async () => {
    api.cancelLinkIntent.mockResolvedValue(undefined);
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await settle();
    expect(api.cancelLinkIntent).toHaveBeenCalledWith(
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(api.confirmLinkIntent).not.toHaveBeenCalled();
    expect(screen.getByTestId("landing")).toHaveTextContent("/sign-in");
  });

  it("still returns to sign-in when the request was already gone", async () => {
    api.cancelLinkIntent.mockRejectedValue(
      apiError(400, "auth_link_intent_unavailable"),
    );
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.getByTestId("landing")).toHaveTextContent("/sign-in");
  });

  it("stays on the page when cancelling fails", async () => {
    api.cancelLinkIntent.mockRejectedValue(apiError(503));
    await renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "TutorHub is busy right now. Please try again shortly.",
    );
    expect(screen.queryByTestId("landing")).toBeNull();
  });
});
