import { QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type {
  AuthProvidersResponse,
  CurrentUser,
  MyLoginMethodsResponse,
  MySecurityResponse,
  MySession,
  PhoneSummary,
} from "@tutorhub/api-client";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { createTutorHubQueryClient } from "../app/queryClient";
import type * as SessionModule from "../app/session";
import { SessionProvider } from "../app/session";
import { ProfileSettingsPage } from "./ProfileSettingsPage";

vi.mock("../app/session", async (importOriginal) => {
  const actual = await importOriginal<typeof SessionModule>();
  return {
    ...actual,
    useSession: () => ({
      ...actual.useSession(),
      signIn: vi.fn(),
      signOut: vi.fn(),
    }),
  };
});

const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const googleIdentityID = "41d58a8a-8f20-4828-a3f7-a6f5fa463954";
const phoneIdentityID = "5a3c2f1e-0b9d-4e8a-a7c6-1d2e3f4a5b6c";

const profile = {
  user: {
    id: userID,
    email: "teacher@example.com",
    display_name: "TutorHub Teacher",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
};

const session: CurrentUser = {
  user: profile.user,
  active_tenant: null,
  memberships: [],
  permissions: [],
};

const providers: AuthProvidersResponse = {
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

const phone: PhoneSummary = {
  calling_code: 84,
  country_code: "VN",
  id: "0d6f3c1e-7b0e-4f43-9b3f-1f0c3a0d9e21",
  last_used_at: "2026-11-19T08:00:00Z",
  last4: "5678",
  masked_number: "+84 ***5678",
  stale: false,
  verified_at: "2026-09-01T07:00:00Z",
};

const googleIdentity = {
  created_at: "2026-11-01T07:00:00Z",
  email: "teacher@gmail.example",
  email_verified: true,
  id: googleIdentityID,
  last_authenticated_at: "2026-11-18T07:00:00Z",
  provider: "https://accounts.google.com",
  provider_slug: "google",
};

const phoneSession: MySession = {
  auth_methods: ["phone_otp"],
  created_at: "2026-11-20T07:00:00Z",
  device_label: "Windows · Chrome",
  expires_at: "2026-12-20T07:00:00Z",
  id: "8e7d6c5b-4a39-4281-9706-f5e4d3c2b1a0",
  is_current: true,
  last_seen_at: "2026-11-20T07:05:00Z",
  persistent: true,
};

interface MockOptions {
  loginMethods?: MyLoginMethodsResponse;
  phone?: PhoneSummary | null;
  providers?: AuthProvidersResponse;
  security?: Partial<MySecurityResponse>;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function requestFrom(input: RequestInfo | URL, init?: RequestInit) {
  return input instanceof Request ? input : new Request(input, init);
}

function installMock(options: MockOptions = {}) {
  const calls: string[] = [];
  const current = options.phone === undefined ? phone : options.phone;
  const security: MySecurityResponse = {
    email_verified: true,
    has_password: false,
    mfa_enabled: false,
    passkey_count: 0,
    phone: current,
    reauth_methods: ["email_code"],
    recent_auth_expires_at: null,
    ...options.security,
  };
  const loginMethods: MyLoginMethodsResponse = options.loginMethods ?? {
    identities: [
      {
        created_at: "2026-11-01T07:00:00Z",
        email_at_provider: "teacher@gmail.example",
        email_verified: true,
        id: googleIdentityID,
        kind: "oidc",
        last_authenticated_at: "2026-11-18T07:00:00Z",
        provider_slug: "google",
      },
      ...(current
        ? [
            {
              created_at: "2026-09-01T07:00:00Z",
              email_at_provider: null,
              email_verified: false,
              id: phoneIdentityID,
              kind: "phone" as const,
              last_authenticated_at: "2026-11-19T08:00:00Z",
              provider_slug: "phone",
            },
          ]
        : []),
    ],
    passkey_count: 0,
    password: { has_password: false, updated_at: null },
    phone: current,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = requestFrom(input, init);
      const path = new URL(request.url).pathname;
      calls.push(`${request.method} ${path}`);
      if (request.method === "GET") {
        switch (path) {
          case "/api/v1/me/profile":
            return jsonResponse(profile);
          case "/api/v1/me/identities":
            return jsonResponse({ identities: [googleIdentity] });
          case "/api/v1/me/password":
            return jsonResponse({
              has_password: false,
              password_enabled: true,
            });
          case "/api/v1/auth/providers":
            return jsonResponse(options.providers ?? providers);
          case "/api/v1/me/security":
            return jsonResponse(security);
          case "/api/v1/me/login-methods":
            return jsonResponse(loginMethods);
          case "/api/v1/me/sessions":
            return jsonResponse({ sessions: [phoneSession] });
          case "/api/v1/me/phone":
            return jsonResponse({ phone: current });
          case "/api/v1/me/mfa":
            return jsonResponse({ enabled: false });
        }
      }
      throw new Error(`Unexpected request: ${request.method} ${request.url}`);
    }),
  );
  return { calls };
}

function renderSettings(language: Language = "en") {
  render(
    <QueryClientProvider client={createTutorHubQueryClient()}>
      <MemoryRouter initialEntries={["/app/settings"]}>
        <I18nProvider initialLanguage={language}>
          <SessionProvider mode={{ kind: "static", currentUser: session }}>
            <ProfileSettingsPage />
          </SessionProvider>
        </I18nProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function unlinkButton() {
  const card = await screen.findByRole("region", {
    name: "Sign-in identities",
  });
  return within(card).findByRole("button", { name: "Unlink" });
}

describe("ProfileSettingsPage phone (AUTH-10B)", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("stays away from the phone endpoint while phone sign-in is off", async () => {
    const { calls } = installMock({
      phone: null,
      providers: { ...providers, phone_enabled: false },
    });
    renderSettings();
    expect(
      await screen.findByRole("heading", { name: "Account security" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Phone number" }),
    ).not.toBeInTheDocument();
    expect(calls).not.toContain("GET /api/v1/me/phone");
  });

  it("labels the phone identity, the security status and a phone-code session", async () => {
    installMock();
    renderSettings();
    const security = await screen.findByRole("region", {
      name: "Account security",
    });
    const methods = await within(security).findByText("Phone number", {
      selector: "strong",
    });
    const row = methods.closest("li");
    expect(row).not.toBeNull();
    expect(row).toHaveTextContent("+84 ***5678");
    expect(row).toHaveTextContent("Verified");
    expect(row).not.toHaveTextContent("Unverified");
    expect(row).toHaveTextContent("Last used or verified");
    // The overview lists the number next to email and password.
    await waitFor(() =>
      expect(
        within(security).getByText("Phone number", { selector: "dt" }),
      ).toBeInTheDocument(),
    );

    const devices = await screen.findByRole("region", {
      name: "Signed-in devices",
    });
    expect(
      await within(devices).findByText("Signed in with: Phone code"),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("region", { name: "Phone number" }),
    ).toBeInTheDocument();
  });

  it("says Mã SĐT in Vietnamese", async () => {
    installMock();
    renderSettings("vi");
    const devices = await screen.findByRole("region", {
      name: "Thiết bị đang đăng nhập",
    });
    expect(
      await within(devices).findByText("Đăng nhập bằng: Mã SĐT"),
    ).toBeInTheDocument();
  });

  it("flags a stale number in the sign-in methods", async () => {
    installMock({ phone: { ...phone, stale: true } });
    renderSettings();
    const security = await screen.findByRole("region", {
      name: "Account security",
    });
    const methods = await within(security).findByText("Phone number", {
      selector: "strong",
    });
    expect(methods.closest("li")).toHaveTextContent("Re-verification needed");
  });

  it("lets the only linked identity go while a usable phone number remains", async () => {
    installMock();
    renderSettings();
    const unlink = await unlinkButton();
    await waitFor(() => expect(unlink).toBeEnabled());
  });

  it("keeps the only linked identity when the phone number is stale", async () => {
    installMock({ phone: { ...phone, stale: true } });
    renderSettings();
    const unlink = await unlinkButton();
    expect(
      (await screen.findAllByText("Re-verification needed")).length,
    ).toBeGreaterThan(0);
    expect(unlink).toBeDisabled();
    expect(
      screen.getByText("The last sign-in method cannot be removed."),
    ).toBeInTheDocument();
  });

  it("does not count a phone whose second step cannot run", async () => {
    // A passkey exists but passkey sign-in is off and no TOTP is active.
    installMock({ security: { passkey_count: 1 } });
    renderSettings();
    const unlink = await unlinkButton();
    await screen.findByText("Signed in with: Phone code");
    expect(unlink).toBeDisabled();
  });

  it("counts the account's verified email with a password", async () => {
    installMock({
      loginMethods: {
        identities: [
          {
            created_at: "2026-11-01T07:00:00Z",
            email_at_provider: "teacher@gmail.example",
            email_verified: true,
            id: googleIdentityID,
            kind: "oidc",
            last_authenticated_at: null,
            provider_slug: "google",
          },
          {
            created_at: "2026-10-01T07:00:00Z",
            email_at_provider: "teacher@example.com",
            email_verified: true,
            id: "7c6b5a49-3827-4615-8f4e-3d2c1b0a9f8e",
            kind: "email",
            last_authenticated_at: null,
            provider_slug: null,
          },
        ],
        passkey_count: 0,
        password: { has_password: true, updated_at: "2026-11-10T08:00:00Z" },
        phone: null,
      },
      phone: null,
    });
    renderSettings();
    const unlink = await unlinkButton();
    await waitFor(() => expect(unlink).toBeEnabled());
  });
});
