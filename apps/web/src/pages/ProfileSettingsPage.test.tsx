import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import {
  getCurrentCSRFToken,
  type AuthProvidersResponse,
  type CurrentUser,
  type MyLoginMethodsResponse,
  type MyPasswordStatusResponse,
  type MySecurityResponse,
  type MySession,
} from "@tutorhub/api-client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import type * as SessionModule from "../app/session";
import { SessionProvider } from "../app/session";
import { ProfileSettingsPage } from "./ProfileSettingsPage";

const { sessionActions } = vi.hoisted(() => ({
  sessionActions: { signIn: vi.fn(), signOut: vi.fn() },
}));

// Keep the real static session; only observe the re-authentication actions.
vi.mock("../app/session", async (importOriginal) => {
  const actual = await importOriginal<typeof SessionModule>();
  return {
    ...actual,
    useSession: () => ({ ...actual.useSession(), ...sessionActions }),
  };
});

const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const identityID = "41d58a8a-8f20-4828-a3f7-a6f5fa463954";

const profile = {
  user: {
    id: userID,
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "vi",
    timezone: "Asia/Ho_Chi_Minh",
    avatar_object_key: `avatars/${userID}/profile.webp`,
  },
};

const identities = {
  identities: [
    {
      id: identityID,
      provider: "https://identity.example.test",
      email: "teacher@example.com",
      email_verified: true,
      created_at: "2026-07-16T08:00:00Z",
      last_authenticated_at: "2026-07-17T08:00:00Z",
    },
  ],
};

const session: CurrentUser = {
  user: profile.user,
  active_tenant: {
    id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role: "teacher",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: [],
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

function requestFrom(input: RequestInfo | URL, init?: RequestInit) {
  return input instanceof Request ? input : new Request(input, init);
}

function renderProfile() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: session }}>
          <ProfileSettingsPage />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const passwordStatus: MyPasswordStatusResponse = {
  has_password: true,
  password_enabled: true,
  updated_at: "2026-11-10T08:00:00Z",
};

const providers: AuthProvidersResponse = {
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
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

function installSuccessfulReadMock() {
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const request = requestFrom(input, init);
    const path = new URL(request.url).pathname;
    if (path === "/api/v1/me/profile" && request.method === "GET") {
      return Promise.resolve(jsonResponse(profile));
    }
    if (path === "/api/v1/me/identities" && request.method === "GET") {
      return Promise.resolve(jsonResponse(identities));
    }
    if (path === "/api/v1/me/password" && request.method === "GET") {
      return Promise.resolve(jsonResponse(passwordStatus));
    }
    if (path === "/api/v1/auth/providers" && request.method === "GET") {
      return Promise.resolve(jsonResponse(providers));
    }
    return Promise.reject(
      new Error(`Unexpected request: ${request.method} ${request.url}`),
    );
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("ProfileSettingsPage", () => {
  beforeEach(() => {
    sessionActions.signIn.mockReset();
    sessionActions.signOut.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("announces the loading state while the profile is being fetched", () => {
    let resolveProfile: ((response: Response) => void) | undefined;
    const pendingProfile = new Promise<Response>((resolve) => {
      resolveProfile = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const request = requestFrom(input, init);
        const path = new URL(request.url).pathname;
        if (path === "/api/v1/me/profile") {
          return pendingProfile;
        }
        if (path === "/api/v1/me/identities") {
          return Promise.resolve(jsonResponse(identities));
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      }),
    );

    renderProfile();

    expect(
      screen.getByRole("status", { name: "Loading your profile" }),
    ).toBeInTheDocument();
    resolveProfile?.(jsonResponse(profile));
  });

  it("renders persisted profile data and protects the last identity", async () => {
    installSuccessfulReadMock();

    renderProfile();

    expect(
      await screen.findByRole("heading", { name: "Profile and identities" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Display name")).toHaveValue(
      "TutorHub Teacher",
    );
    expect(screen.getByLabelText("Timezone")).toHaveValue("Asia/Ho_Chi_Minh");
    expect(
      screen.getByText("https://identity.example.test"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Unlink" })).toBeDisabled();
    expect(
      screen.getByText("The last sign-in method cannot be removed."),
    ).toBeInTheDocument();
  });

  it("focuses the invalid display name and does not call the mutation", async () => {
    const fetchMock = installSuccessfulReadMock();
    renderProfile();
    const displayName = await screen.findByLabelText("Display name");

    fireEvent.change(displayName, { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(
      await screen.findByText("Enter a display name."),
    ).toBeInTheDocument();
    expect(displayName).toHaveFocus();
    expect(
      fetchMock.mock.calls.some(([input, init]) => {
        const request = requestFrom(input, init);
        return request.method === "PATCH";
      }),
    ).toBe(false);
  });

  it("recovers from a profile loading error through retry", async () => {
    let profileCalls = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const request = requestFrom(input, init);
        const path = new URL(request.url).pathname;
        if (path === "/api/v1/me/profile") {
          profileCalls += 1;
          if (profileCalls === 1) {
            return Promise.resolve(
              jsonResponse(
                {
                  type: "about:blank",
                  title: "Profile unavailable",
                  status: 503,
                  detail: "Profile storage is temporarily unavailable.",
                },
                503,
              ),
            );
          }
          return Promise.resolve(jsonResponse(profile));
        }
        if (path === "/api/v1/me/identities") {
          return Promise.resolve(jsonResponse(identities));
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      }),
    );

    renderProfile();

    expect(
      await screen.findByRole("heading", { name: "Profile unavailable" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Profile storage is temporarily unavailable."),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(
      await screen.findByRole("heading", { name: "Profile and identities" }),
    ).toBeInTheDocument();
    expect(profileCalls).toBe(2);
  });

  it("rotates CSRF and persists normalized profile changes", async () => {
    let patchRequest: Request | undefined;
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const request = requestFrom(input, init);
        const path = new URL(request.url).pathname;
        if (path === "/api/v1/me/profile" && request.method === "GET") {
          return jsonResponse(profile);
        }
        if (path === "/api/v1/me/identities" && request.method === "GET") {
          return jsonResponse(identities);
        }
        if (path === "/api/v1/auth/csrf" && request.method === "GET") {
          return jsonResponse({ csrf_token: "profile-csrf" });
        }
        if (path === "/api/v1/me/profile" && request.method === "PATCH") {
          patchRequest = request.clone();
          return jsonResponse({
            user: {
              ...profile.user,
              display_name: "Updated Teacher",
              locale: "en",
              timezone: "Europe/London",
            },
          });
        }
        throw new Error(`Unexpected request: ${request.method} ${request.url}`);
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    renderProfile();

    const displayName = await screen.findByLabelText("Display name");
    fireEvent.change(displayName, { target: { value: "  Updated Teacher  " } });
    fireEvent.change(screen.getByLabelText("Timezone"), {
      target: { value: "Europe/London" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(await screen.findByText("Profile updated.")).toBeInTheDocument();
    await waitFor(() => expect(patchRequest).toBeDefined());
    expect(patchRequest?.headers.get("X-CSRF-Token")).toBe("profile-csrf");
    await expect(patchRequest?.json()).resolves.toEqual({
      avatar_object_key: `avatars/${userID}/profile.webp`,
      display_name: "Updated Teacher",
      locale: "vi",
      timezone: "Europe/London",
    });
  });
});

/* ---------- AUTH-03B: password card ---------- */

function problemResponse(status: number, code?: string, retryAfter?: number) {
  const headers: Record<string, string> = {
    "Content-Type": "application/problem+json",
  };
  if (retryAfter !== undefined) headers["Retry-After"] = String(retryAfter);
  return new Response(
    JSON.stringify({ type: "about:blank", title: "Rejected", status, code }),
    { status, headers },
  );
}

interface PasswordMockOptions {
  passwordGet?: () => Response;
  post?: (request: Request) => Response;
  providers?: AuthProvidersResponse;
  status?: MyPasswordStatusResponse;
}

function installPasswordMock({
  passwordGet,
  post = () =>
    jsonResponse({ has_password: true, updated_at: "2026-11-17T09:00:00Z" }),
  providers: providersOverride = providers,
  status = passwordStatus,
}: PasswordMockOptions = {}) {
  const calls: string[] = [];
  const posts: Request[] = [];
  let currentStatus = status;
  let csrfCount = 0;
  const fetchMock = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = requestFrom(input, init);
      const path = new URL(request.url).pathname;
      calls.push(`${request.method} ${path}`);
      if (path === "/api/v1/me/profile" && request.method === "GET") {
        return jsonResponse(profile);
      }
      if (path === "/api/v1/me/identities" && request.method === "GET") {
        return jsonResponse(identities);
      }
      if (path === "/api/v1/auth/providers" && request.method === "GET") {
        return jsonResponse(providersOverride);
      }
      if (path === "/api/v1/me/password" && request.method === "GET") {
        return passwordGet ? passwordGet() : jsonResponse(currentStatus);
      }
      if (path === "/api/v1/auth/csrf" && request.method === "GET") {
        csrfCount += 1;
        return jsonResponse({ csrf_token: `password-csrf-${csrfCount}` });
      }
      if (path === "/api/v1/me/password" && request.method === "POST") {
        posts.push(request.clone());
        const response = post(request);
        if (response.ok) {
          currentStatus = {
            ...currentStatus,
            has_password: true,
            updated_at: "2026-11-17T09:00:00Z",
          };
        }
        return response;
      }
      throw new Error(`Unexpected request: ${request.method} ${request.url}`);
    },
  );
  vi.stubGlobal("fetch", fetchMock);
  return { calls, posts };
}

async function findPasswordCard() {
  const card = await screen.findByRole("region", { name: "Password" });
  await within(card).findByRole("group");
  return card;
}

function fillPasswordForm(
  card: HTMLElement,
  values: { confirm: string; current?: string; next: string },
) {
  if (values.current !== undefined) {
    fireEvent.change(within(card).getByLabelText("Current password"), {
      target: { value: values.current },
    });
  }
  fireEvent.change(within(card).getByLabelText("New password"), {
    target: { value: values.next },
  });
  fireEvent.change(within(card).getByLabelText("Confirm new password"), {
    target: { value: values.confirm },
  });
}

describe("ProfileSettingsPage password card", () => {
  beforeEach(() => {
    sessionActions.signIn.mockReset();
    sessionActions.signOut.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("changes the password with a rotated CSRF token, clears the fields and stays signed in", async () => {
    const { calls, posts } = installPasswordMock();
    renderProfile();
    const card = await findPasswordCard();

    expect(within(card).getByText("Password set")).toBeInTheDocument();
    expect(within(card).getByText(/^Last changed /)).toBeInTheDocument();
    expect(
      within(card).getByRole("group", { name: "Change password" }),
    ).toBeEnabled();
    fillPasswordForm(card, {
      confirm: "N3w-settings-passphrase",
      current: "Old-settings-passphrase",
      next: "N3w-settings-passphrase",
    });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );

    expect(
      await within(card).findByText(
        "Password changed. Your other sessions have been signed out.",
      ),
    ).toHaveAttribute("role", "status");
    expect(posts).toHaveLength(1);
    expect(posts[0]?.headers.get("X-CSRF-Token")).toBe("password-csrf-1");
    await expect(posts[0]?.json()).resolves.toEqual({
      current_password: "Old-settings-passphrase",
      new_password: "N3w-settings-passphrase",
    });
    // The server rotated the CSRF cookie: the in-memory token is resynced.
    const postIndex = calls.indexOf("POST /api/v1/me/password");
    expect(calls.slice(postIndex + 1)).toContain("GET /api/v1/auth/csrf");
    await waitFor(() => expect(getCurrentCSRFToken()).toBe("password-csrf-2"));
    for (const label of [
      "Current password",
      "New password",
      "Confirm new password",
    ]) {
      expect(within(card).getByLabelText(label)).toHaveValue("");
    }
    expect(sessionActions.signOut).not.toHaveBeenCalled();
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("sets a first password without asking for a current one", async () => {
    const { posts } = installPasswordMock({
      status: { has_password: false, password_enabled: true },
    });
    renderProfile();
    const card = await findPasswordCard();

    expect(within(card).getByText("No password yet")).toBeInTheDocument();
    expect(
      within(card).getByRole("group", { name: "Set a password" }),
    ).toBeInTheDocument();
    expect(within(card).queryByLabelText("Current password")).toBeNull();
    fillPasswordForm(card, {
      confirm: "First-settings-passphrase",
      next: "First-settings-passphrase",
    });
    fireEvent.click(within(card).getByRole("button", { name: "Set password" }));

    expect(
      await within(card).findByText(
        "Password set. Your other sessions have been signed out.",
      ),
    ).toBeInTheDocument();
    await expect(posts[0]?.json()).resolves.toEqual({
      new_password: "First-settings-passphrase",
    });
    // The refreshed status switches the card to the change form.
    expect(
      await within(card).findByRole("group", { name: "Change password" }),
    ).toBeInTheDocument();
  });

  it("validates the confirmation and minimum length before calling the server", async () => {
    const { posts } = installPasswordMock();
    renderProfile();
    const card = await findPasswordCard();

    fillPasswordForm(card, {
      confirm: "Different-passphrase",
      current: "Old-settings-passphrase",
      next: "N3w-settings-passphrase",
    });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );
    const confirm = within(card).getByLabelText("Confirm new password");
    expect(confirm).toHaveAccessibleDescription("The passwords don't match.");
    expect(confirm).toHaveFocus();

    fillPasswordForm(card, { confirm: "short", next: "short" });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );
    const next = within(card).getByLabelText("New password");
    expect(next).toHaveAttribute("aria-invalid", "true");
    expect(next).toHaveAccessibleDescription(
      /The new password must be at least 8 characters\./,
    );
    expect(next).toHaveFocus();
    expect(posts).toHaveLength(0);
  });

  it("flags a wrong current password on its field and keeps the new one", async () => {
    installPasswordMock({
      post: () => problemResponse(401, "auth_invalid_current_password"),
    });
    renderProfile();
    const card = await findPasswordCard();
    fillPasswordForm(card, {
      confirm: "N3w-settings-passphrase",
      current: "Wrong-passphrase",
      next: "N3w-settings-passphrase",
    });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );

    const current = within(card).getByLabelText("Current password");
    await waitFor(() =>
      expect(current).toHaveAccessibleDescription(
        "The current password is incorrect.",
      ),
    );
    expect(current).toHaveValue("");
    expect(current).toHaveFocus();
    expect(within(card).getByLabelText("New password")).toHaveValue(
      "N3w-settings-passphrase",
    );
    // Only the field error is announced; no form-level failure is shown.
    expect(within(card).getAllByRole("alert")).toHaveLength(1);
  });

  it("maps verified-email, unchanged, policy and Retry-After responses", async () => {
    const responses = [
      problemResponse(403, "auth_verified_email_required"),
      problemResponse(400, "auth_password_unchanged"),
      problemResponse(400, "auth_password_policy"),
      problemResponse(429, undefined, 5),
    ];
    installPasswordMock({
      post: () => responses.shift() ?? problemResponse(500),
    });
    renderProfile();
    const card = await findPasswordCard();
    const submit = () =>
      fireEvent.click(
        within(card).getByRole("button", { name: /Change password/ }),
      );
    const values = {
      confirm: "N3w-settings-passphrase",
      current: "Old-settings-passphrase",
      next: "N3w-settings-passphrase",
    };

    fillPasswordForm(card, values);
    submit();
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "Your account email must be verified before you can set a password.",
    );

    fillPasswordForm(card, values);
    submit();
    const next = within(card).getByLabelText("New password");
    await waitFor(() =>
      expect(next).toHaveAccessibleDescription(
        /The new password must be different from the current one\./,
      ),
    );

    fillPasswordForm(card, values);
    submit();
    await waitFor(() =>
      expect(next).toHaveAccessibleDescription(
        /This password is too weak or appears on a list of common or leaked passwords\./,
      ),
    );
    expect(next).toHaveValue("");
    expect(within(card).getByLabelText("Confirm new password")).toHaveValue("");
    expect(within(card).getByLabelText("Current password")).toHaveValue(
      "Old-settings-passphrase",
    );

    fillPasswordForm(card, values);
    submit();
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "Too many attempts. Please try again in 5 seconds.",
    );
    const locked = within(card).getByRole("button", {
      name: "Change password (5s)",
    });
    expect(locked).toBeDisabled();
  });

  it("keeps the sign-in-again fallback for a step-up 403 when device management is off", async () => {
    installPasswordMock({
      post: () => problemResponse(403, "auth_recent_authentication_required"),
    });
    const view = renderProfile();
    let card = await findPasswordCard();
    fillPasswordForm(card, {
      confirm: "N3w-settings-passphrase",
      current: "Old-settings-passphrase",
      next: "N3w-settings-passphrase",
    });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );
    let dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Sign in again" }),
    );
    await waitFor(() =>
      expect(sessionActions.signOut).toHaveBeenCalledTimes(1),
    );
    expect(sessionActions.signIn).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    // The inline alert can reopen the same dialog after a cancel.
    const alert = within(card).getByRole("alert");
    expect(alert).toHaveTextContent(
      "For your security, confirm it's you before setting or changing your password",
    );
    fireEvent.click(
      within(alert).getByRole("button", { name: "Confirm it's you" }),
    );
    expect(
      await screen.findByRole("dialog", { name: "Confirm it's you" }),
    ).toBeInTheDocument();
    view.unmount();
    sessionActions.signOut.mockClear();

    // A legacy 403 without a code takes the same path.
    installPasswordMock({
      post: () => problemResponse(403),
      providers: { ...providers, oidc_default_enabled: true },
    });
    renderProfile();
    card = await findPasswordCard();
    fillPasswordForm(card, {
      confirm: "N3w-settings-passphrase",
      current: "Old-settings-passphrase",
      next: "N3w-settings-passphrase",
    });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );
    dialog = await screen.findByRole("dialog", { name: "Confirm it's you" });
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Sign in again" }),
    );
    expect(sessionActions.signIn).toHaveBeenCalledWith("/app/settings");
    expect(sessionActions.signOut).not.toHaveBeenCalled();
  });

  it("keeps the status visible but locks the form when password sign-in is disabled", async () => {
    const { posts } = installPasswordMock({
      status: { has_password: true, password_enabled: false },
    });
    renderProfile();
    const card = await findPasswordCard();

    const group = within(card).getByRole("group", { name: "Change password" });
    expect(group).toBeDisabled();
    expect(group).toHaveAccessibleDescription(
      /Password sign-in is turned off for this TutorHub deployment/,
    );
    expect(within(card).getByText("Password set")).toBeInTheDocument();
    expect(within(card).getByLabelText("New password")).toBeDisabled();
    expect(
      within(card).getByRole("button", { name: "Change password" }),
    ).toBeDisabled();
    expect(posts).toHaveLength(0);
  });

  it("shows loading, error with retry and forbidden states for the status", async () => {
    let attempts = 0;
    installPasswordMock({
      passwordGet: () => {
        attempts += 1;
        return attempts === 1
          ? problemResponse(503)
          : jsonResponse(passwordStatus);
      },
    });
    const view = renderProfile();
    const card = await screen.findByRole("region", { name: "Password" });
    expect(
      await within(card).findByRole("heading", {
        name: "Password status unavailable",
      }),
    ).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Try again" }));
    expect(await within(card).findByText("Password set")).toBeInTheDocument();
    expect(attempts).toBe(2);
    view.unmount();

    installPasswordMock({ passwordGet: () => problemResponse(403) });
    renderProfile();
    const forbiddenCard = await screen.findByRole("region", {
      name: "Password",
    });
    expect(
      await within(forbiddenCard).findByRole("heading", {
        name: "Password management unavailable",
      }),
    ).toBeInTheDocument();
  });
});

/* ---------- AUTH-04B: step-up, security overview and devices ---------- */

const deviceProviders: AuthProvidersResponse = {
  ...providers,
  device_management_enabled: true,
};

const securityOverview: MySecurityResponse = {
  email_verified: true,
  has_password: true,
  mfa_enabled: false,
  reauth_methods: ["password", "email_code"],
  recent_auth_expires_at: null,
};

const loginMethods: MyLoginMethodsResponse = {
  password: { has_password: true, updated_at: "2026-11-10T08:00:00Z" },
  identities: [
    {
      id: "0d6b0a52-7a39-4bde-9d0e-3c51d6c0b8a1",
      kind: "email",
      provider_slug: null,
      email_at_provider: "teacher@example.com",
      email_verified: true,
      last_authenticated_at: "2026-11-17T08:00:00Z",
      created_at: "2026-11-01T08:00:00Z",
    },
    {
      id: "a5f0c7d8-1d51-4c43-9c1a-8a0e4cbbf0f2",
      kind: "oauth2",
      provider_slug: "google",
      email_at_provider: "teacher.google@example.com",
      email_verified: false,
      last_authenticated_at: null,
      created_at: "2026-11-02T08:00:00Z",
    },
  ],
};

const currentSession: MySession = {
  id: "5b1e8f1c-8a0f-4f55-a4b4-7a5d70b6c6d1",
  device_label: "Chrome · Windows",
  auth_methods: ["pwd", "email_code", "pwd"],
  created_at: "2026-11-18T07:00:00Z",
  last_seen_at: "2026-11-18T07:30:00Z",
  expires_at: "2026-12-18T07:00:00Z",
  persistent: true,
  is_current: true,
};

const otherSession: MySession = {
  id: "c3a7f0d2-2b6e-4e0e-9a51-0f6f3d9e7b42",
  device_label: "Firefox · Linux",
  auth_methods: ["email_code", "magic"],
  created_at: "2026-11-18T06:00:00Z",
  last_seen_at: "2026-11-18T08:00:00Z",
  expires_at: "2026-11-19T06:00:00Z",
  persistent: false,
  is_current: false,
};

const thirdSession: MySession = {
  ...otherSession,
  id: "f0b5c1a9-6a0e-4b8c-8a2e-2f1d9e4c7a63",
  device_label: null,
  auth_methods: ["fed"],
};

const sessionDate = new Intl.DateTimeFormat("en-US", {
  dateStyle: "medium",
  timeStyle: "short",
});

type MaybeAsyncResponse = Response | Promise<Response>;
type Handler = (request: Request) => MaybeAsyncResponse | undefined;

interface SecurityMockOptions {
  extra?: Handler;
  identities?: typeof identities;
  passwordPosts?: Array<() => MaybeAsyncResponse>;
  providers?: AuthProvidersResponse;
  sessions?: Array<() => MaybeAsyncResponse>;
}

function installSecurityMock(options: SecurityMockOptions = {}) {
  const calls: string[] = [];
  const posts: Request[] = [];
  const passwordQueue = [...(options.passwordPosts ?? [])];
  const sessionsQueue = [...(options.sessions ?? [])];
  let overview = securityOverview;
  let liveSessions: MySession[] = [otherSession, currentSession];
  let csrfCount = 0;
  const state = {
    setSessions(value: MySession[]) {
      liveSessions = value;
    },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = requestFrom(input, init);
      const path = new URL(request.url).pathname;
      calls.push(`${request.method} ${path}`);
      if (request.method !== "GET") posts.push(request.clone());
      const extra = options.extra?.(request);
      if (extra) return extra;
      if (path === "/api/v1/me/profile") return jsonResponse(profile);
      if (path === "/api/v1/me/identities" && request.method === "GET") {
        return jsonResponse(options.identities ?? identities);
      }
      if (path === "/api/v1/auth/providers") {
        return jsonResponse(options.providers ?? deviceProviders);
      }
      if (path === "/api/v1/me/password" && request.method === "GET") {
        return jsonResponse(passwordStatus);
      }
      if (path === "/api/v1/me/password" && request.method === "POST") {
        const next = passwordQueue.shift();
        return next
          ? next()
          : jsonResponse({
              has_password: true,
              updated_at: "2026-11-18T09:00:00Z",
            });
      }
      if (path === "/api/v1/auth/csrf") {
        csrfCount += 1;
        return jsonResponse({ csrf_token: `security-csrf-${csrfCount}` });
      }
      if (path === "/api/v1/me/security") return jsonResponse(overview);
      if (path === "/api/v1/me/login-methods") {
        return jsonResponse(loginMethods);
      }
      if (path === "/api/v1/me/sessions" && request.method === "GET") {
        const next = sessionsQueue.shift();
        return next ? next() : jsonResponse({ sessions: liveSessions });
      }
      if (path === "/api/v1/auth/reauth" && request.method === "POST") {
        overview = {
          ...overview,
          recent_auth_expires_at: new Date(
            Date.now() + 10 * 60_000,
          ).toISOString(),
        };
        return jsonResponse({
          recent_auth_expires_at: overview.recent_auth_expires_at,
        });
      }
      const revoke = /^\/api\/v1\/me\/sessions\/([^/]+)\/revoke$/.exec(path);
      if (revoke && request.method === "POST") {
        liveSessions = liveSessions.filter((item) => item.id !== revoke[1]);
        return new Response(null, { status: 204 });
      }
      if (
        path === "/api/v1/me/sessions/revoke-others" &&
        request.method === "POST"
      ) {
        const revoked = liveSessions.filter((item) => !item.is_current).length;
        liveSessions = liveSessions.filter((item) => item.is_current);
        return jsonResponse({ revoked_count: revoked });
      }
      throw new Error(`Unexpected request: ${request.method} ${request.url}`);
    }),
  );
  return { calls, posts, state };
}

async function confirmWithPassword(password: string) {
  const dialog = await screen.findByRole("dialog", {
    name: "Confirm it's you",
  });
  fireEvent.change(await within(dialog).findByLabelText("Current password"), {
    target: { value: password },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
}

function sessionRows(card: HTMLElement) {
  return Array.from(card.querySelectorAll<HTMLElement>("li.session-row"));
}

describe("ProfileSettingsPage AUTH-04B security", () => {
  beforeEach(() => {
    sessionActions.signIn.mockReset();
    sessionActions.signOut.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("hides the security cards and never calls their endpoints without the capability flag", async () => {
    const fetchMock = installSuccessfulReadMock();
    renderProfile();
    await findPasswordCard();

    expect(
      screen.queryByRole("region", { name: "Account security" }),
    ).toBeNull();
    expect(
      screen.queryByRole("region", { name: "Signed-in devices" }),
    ).toBeNull();
    const paths = fetchMock.mock.calls.map(
      ([input, init]) => new URL(requestFrom(input, init).url).pathname,
    );
    for (const path of [
      "/api/v1/me/security",
      "/api/v1/me/login-methods",
      "/api/v1/me/sessions",
    ]) {
      expect(paths).not.toContain(path);
    }
  });

  it("shows the security overview and sign-in methods, and confirms identity from the banner", async () => {
    const { posts } = installSecurityMock();
    renderProfile();
    const card = await screen.findByRole("region", {
      name: "Account security",
    });

    expect(
      await within(card).findByText("Not confirmed in the last 10 minutes"),
    ).toBeInTheDocument();
    expect(within(card).getByText("Set")).toBeInTheDocument();
    expect(within(card).getByText("Off")).toBeInTheDocument();

    const methods = await within(card).findByRole("list");
    const rows = within(methods).getAllByRole("listitem");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent(/^PasswordLast changed /);
    expect(rows[1]).toHaveTextContent("Email sign-in code");
    expect(rows[1]).toHaveTextContent("Verified");
    expect(rows[1]).toHaveTextContent("teacher@example.com");
    expect(rows[1]?.querySelector("time")).toHaveAttribute(
      "dateTime",
      "2026-11-17T08:00:00Z",
    );
    expect(rows[2]).toHaveTextContent("Google account");
    expect(rows[2]).toHaveTextContent("Not verified");
    expect(rows[2]).toHaveTextContent("Not used to sign in yet");

    const confirmNow = within(card).getByRole("button", {
      name: "Confirm now",
    });
    expect(confirmNow).toHaveAccessibleDescription(
      /Sensitive actions such as changing your password/,
    );
    fireEvent.click(confirmNow);
    await confirmWithPassword("Banner-passphrase");

    expect(await within(card).findByText(/^Valid until /)).toBeInTheDocument();
    expect(
      within(card).queryByRole("button", { name: "Confirm now" }),
    ).toBeNull();
    const reauth = posts.find((request) =>
      request.url.endsWith("/api/v1/auth/reauth"),
    );
    await expect(reauth?.json()).resolves.toEqual({
      method: "password",
      password: "Banner-passphrase",
    });
  });

  it("lists signed-in devices with this device first and signs out another device", async () => {
    const { posts } = installSecurityMock();
    renderProfile();
    const card = await screen.findByRole("region", {
      name: "Signed-in devices",
    });

    await within(card).findByText("Firefox · Linux");
    const rows = sessionRows(card);
    expect(rows).toHaveLength(2);
    const [current, other] = rows as [HTMLElement, HTMLElement];
    expect(current).toHaveTextContent("Chrome · Windows");
    expect(current).toHaveTextContent("This device");
    expect(current).toHaveTextContent("Signed in with: Password, Email code");
    expect(current).toHaveTextContent("Stays signed in until");
    expect(within(current).queryByRole("button")).toBeNull();
    expect(other).toHaveTextContent("Firefox · Linux");
    expect(other).toHaveTextContent("Signed in with: Email code, Other method");
    expect(other).toHaveTextContent("Expires");
    expect(other).not.toHaveTextContent("This device");

    fireEvent.click(
      within(other).getByRole("button", {
        name: `Sign out Firefox · Linux (started ${sessionDate.format(
          new Date(otherSession.created_at),
        )})`,
      }),
    );

    expect(
      await within(card).findByText("Firefox · Linux has been signed out."),
    ).toHaveAttribute("role", "status");
    await waitFor(() => expect(sessionRows(card)).toHaveLength(1));
    expect(
      within(card).getByRole("heading", { name: "Signed-in devices" }),
    ).toHaveFocus();
    const request = posts.find((item) =>
      item.url.endsWith(`/api/v1/me/sessions/${otherSession.id}/revoke`),
    );
    expect(request?.headers.get("X-CSRF-Token")).toMatch(/^security-csrf-/);
    await expect(request?.json()).resolves.toEqual({});
  });

  it("signs out all other devices only after an explicit confirmation", async () => {
    const { posts, state } = installSecurityMock();
    state.setSessions([otherSession, currentSession, thirdSession]);
    renderProfile();
    const card = await screen.findByRole("region", {
      name: "Signed-in devices",
    });
    expect(await within(card).findByText("Unknown device")).toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", {
        name: "Sign out of all other devices",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Sign out of all other devices?",
    });
    expect(dialog).toHaveAccessibleDescription(
      "2 other session(s) will end right away. This device stays signed in.",
    );
    expect(posts.some((item) => item.url.endsWith("/revoke-others"))).toBe(
      false,
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Sign out other devices" }),
    );

    expect(
      await within(card).findByText("Signed out 2 other session(s)."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await waitFor(() => expect(sessionRows(card)).toHaveLength(1));
    const bulk = within(card).getByRole("button", {
      name: "Sign out of all other devices",
    });
    expect(bulk).toBeDisabled();
    expect(bulk).toHaveAccessibleDescription("Only this device is signed in.");
  });

  it("handles Retry-After, already-ended and current-session revoke responses", async () => {
    const revokeResponses = [
      () => problemResponse(404),
      () => problemResponse(400, "auth_cannot_revoke_current_session"),
      () => problemResponse(429, undefined, 2),
    ];
    installSecurityMock({
      sessions: [() => problemResponse(503, undefined, 1)],
      extra: (request) => {
        if (
          request.method === "POST" &&
          /\/api\/v1\/me\/sessions\/[^/]+\/revoke$/.test(request.url)
        ) {
          return (revokeResponses.shift() ?? (() => problemResponse(500)))();
        }
        return undefined;
      },
    });
    renderProfile();
    const card = await screen.findByRole("region", {
      name: "Signed-in devices",
    });

    expect(
      await within(card).findByText(
        "This feature is temporarily unavailable. Try again in 1 seconds.",
      ),
    ).toBeInTheDocument();
    expect(
      within(card).getByRole("button", { name: "Try again (1s)" }),
    ).toBeDisabled();
    fireEvent.click(
      await within(card).findByRole(
        "button",
        { name: "Try again" },
        { timeout: 3000 },
      ),
    );
    const revokeName = /^Sign out Firefox · Linux/;
    fireEvent.click(
      await within(card).findByRole("button", { name: revokeName }),
    );
    expect(
      await within(card).findByText("That session had already ended."),
    ).toBeInTheDocument();

    fireEvent.click(
      await within(card).findByRole("button", { name: revokeName }),
    );
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "You can't sign out this device here.",
    );

    fireEvent.click(within(card).getByRole("button", { name: revokeName }));
    await waitFor(() =>
      expect(within(card).getByRole("alert")).toHaveTextContent(
        "Too many requests. Try again in 2 seconds.",
      ),
    );
    expect(
      within(card).getByRole("button", { name: revokeName }),
    ).toBeDisabled();
  });

  it("shows the loading skeleton, then the empty state for devices", async () => {
    let releaseSessions: ((response: Response) => void) | undefined;
    installSecurityMock({
      sessions: [
        () =>
          new Promise<Response>((resolve) => {
            releaseSessions = resolve;
          }),
      ],
    });
    renderProfile();
    const card = await screen.findByRole("region", {
      name: "Signed-in devices",
    });
    expect(
      await within(card).findByRole("status", {
        name: "Loading signed-in devices",
      }),
    ).toBeInTheDocument();
    await waitFor(() => expect(releaseSessions).toBeDefined());
    releaseSessions?.(jsonResponse({ sessions: [] }));
    expect(
      await within(card).findByRole("heading", { name: "No active sessions" }),
    ).toBeInTheDocument();
  });

  it("resubmits the same password change after an in-place step-up", async () => {
    const { posts } = installSecurityMock({
      passwordPosts: [
        () => problemResponse(403, "auth_recent_authentication_required"),
      ],
    });
    renderProfile();
    const card = await findPasswordCard();
    fillPasswordForm(card, {
      confirm: "N3w-settings-passphrase",
      current: "Old-settings-passphrase",
      next: "N3w-settings-passphrase",
    });
    fireEvent.click(
      within(card).getByRole("button", { name: "Change password" }),
    );

    await confirmWithPassword("Old-settings-passphrase");

    expect(
      await within(card).findByText(
        "Password changed. Your other sessions have been signed out.",
      ),
    ).toBeInTheDocument();
    const passwordPosts = posts.filter((request) =>
      request.url.endsWith("/api/v1/me/password"),
    );
    expect(passwordPosts).toHaveLength(2);
    const bodies = await Promise.all(
      passwordPosts.map((request) => request.json()),
    );
    expect(bodies[1]).toEqual(bodies[0]);
    expect(bodies[1]).toEqual({
      current_password: "Old-settings-passphrase",
      new_password: "N3w-settings-passphrase",
    });
    expect(within(card).getByLabelText("New password")).toHaveValue("");
    expect(within(card).queryByRole("alert")).toBeNull();
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("re-runs an identity unlink after confirming it is you", async () => {
    const secondIdentityID = "8f9b3a71-5c2d-4f0e-b6a4-1e7d9c3b2a50";
    const unlinkResponses = [
      () => problemResponse(403, "auth_recent_authentication_required"),
      () => new Response(null, { status: 204 }),
    ];
    const { posts } = installSecurityMock({
      identities: {
        identities: [
          ...identities.identities,
          {
            id: secondIdentityID,
            provider: "https://second-identity.example.test",
            email: "teacher@example.com",
            email_verified: true,
            created_at: "2026-07-18T08:00:00Z",
            last_authenticated_at: "2026-07-18T08:00:00Z",
          },
        ],
      },
      extra: (request) =>
        request.method === "DELETE"
          ? (unlinkResponses.shift() ?? (() => problemResponse(500)))()
          : undefined,
    });
    renderProfile();
    const identityCard = await screen.findByRole("region", {
      name: "Sign-in identities",
    });
    const unlinkButtons = await within(identityCard).findAllByRole("button", {
      name: "Unlink",
    });
    fireEvent.click(unlinkButtons[1] as HTMLElement);

    await confirmWithPassword("Identity-passphrase");

    expect(
      await within(identityCard).findByText("Identity unlinked."),
    ).toBeInTheDocument();
    const deletes = posts.filter((request) => request.method === "DELETE");
    expect(deletes).toHaveLength(2);
    expect(deletes[1]?.url).toContain(secondIdentityID);
  });
});
