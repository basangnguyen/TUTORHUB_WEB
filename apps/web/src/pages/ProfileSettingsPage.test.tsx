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
  type MyPasswordStatusResponse,
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

  it("asks for a fresh sign-in on a step-up 403 and signs out on native-only deployments", async () => {
    installPasswordMock({ post: () => problemResponse(403) });
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
    const alert = await within(card).findByRole("alert");
    expect(alert).toHaveTextContent(
      "For your security, sign in again before setting or changing your password",
    );
    fireEvent.click(
      within(alert).getByRole("button", { name: "Sign in again" }),
    );
    expect(sessionActions.signOut).toHaveBeenCalledTimes(1);
    expect(sessionActions.signIn).not.toHaveBeenCalled();
    view.unmount();
    sessionActions.signOut.mockClear();

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
    const oidcAlert = await within(card).findByRole("alert");
    fireEvent.click(
      within(oidcAlert).getByRole("button", { name: "Sign in again" }),
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
