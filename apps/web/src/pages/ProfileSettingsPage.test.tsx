import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { CurrentUser } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { ProfileSettingsPage } from "./ProfileSettingsPage";

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
    return Promise.reject(
      new Error(`Unexpected request: ${request.method} ${request.url}`),
    );
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("ProfileSettingsPage", () => {
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
