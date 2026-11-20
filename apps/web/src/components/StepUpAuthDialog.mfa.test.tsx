import { QueryClientProvider } from "@tanstack/react-query";
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
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { createTutorHubQueryClient } from "../app/queryClient";
import { useStepUpRequest } from "../app/security";
import { SessionProvider } from "../app/session";
import { StepUpAuthDialog } from "./StepUpAuthDialog";

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

const providers: AuthProvidersResponse = {
  device_management_enabled: true,
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: true,
  oauth_providers: [],
  oidc_default_enabled: false,
  passkeys_enabled: true,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: true,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: false,
};

function security(
  reauthMethods: AccountSecurityResponse["reauth_methods"],
): AccountSecurityResponse {
  return {
    email_verified: true,
    has_password: true,
    mfa_enabled: reauthMethods.includes("totp"),
    passkey_count: reauthMethods.includes("passkey") ? 1 : 0,
    reauth_methods: reauthMethods,
    recent_auth_expires_at: null,
    recovery_codes_remaining: 10,
  };
}

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

function installMock(
  overview: AccountSecurityResponse,
  {
    providerFlags = providers,
    reauth = [],
  }: {
    providerFlags?: AuthProvidersResponse;
    reauth?: Array<() => Response>;
  } = {},
) {
  const posts: Request[] = [];
  const queue = [...reauth];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/auth/providers") return json(providerFlags);
      if (path === "/api/v1/me/security") return json(overview);
      if (path === "/api/v1/auth/csrf") return json({ csrf_token: "csrf" });
      if (path === "/api/v1/auth/reauth" && request.method === "POST") {
        posts.push(request.clone());
        return (
          queue.shift() ??
          (() => json({ recent_auth_expires_at: "2026-11-20T10:10:00Z" }))
        )();
      }
      throw new Error(`Unexpected request: ${request.method} ${path}`);
    }),
  );
  return { posts };
}

function installWebAuthn() {
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {}),
  );
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get: vi.fn() },
  });
}

function Harness({
  action,
  requireAal2,
}: {
  action: () => void;
  requireAal2: boolean;
}) {
  const stepUp = useStepUpRequest();
  return (
    <>
      <button
        onClick={() => stepUp.request(action, { requireAal2 })}
        type="button"
      >
        Protected action
      </button>
      <StepUpAuthDialog {...stepUp.dialog} />
    </>
  );
}

async function openDialog({ requireAal2 = false } = {}) {
  const action = vi.fn();
  render(
    <QueryClientProvider client={createTutorHubQueryClient()}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <Harness action={action} requireAal2={requireAal2} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Protected action" }));
  const dialog = await screen.findByRole("dialog", {
    name: "Confirm it's you",
  });
  return { action, dialog };
}

describe("StepUpAuthDialog AUTH-09B", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("steps up with the authenticator app, reports a wrong code and resumes once", async () => {
    const { posts } = installMock(security(["totp"]), {
      // TOTP step-up works even where device management is off.
      providerFlags: { ...providers, device_management_enabled: false },
      reauth: [() => problem(401, "auth_invalid_verification_code")],
    });
    const { action, dialog } = await openDialog();

    const field = await within(dialog).findByLabelText("Authenticator code");
    await waitFor(() => expect(field).toHaveFocus());
    expect(field).toHaveAttribute("autocomplete", "one-time-code");
    expect(field).toHaveAttribute("inputmode", "numeric");
    // Recovery codes are refused here: the hint says so up front.
    expect(field).toHaveAccessibleDescription(/Recovery codes do not work/);
    expect(
      within(dialog).queryByRole("group", { name: "Confirmation method" }),
    ).toBeNull();

    fireEvent.change(field, { target: { value: "12ab" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(field).toHaveAccessibleDescription(
      /The authenticator code has exactly 6 digits/,
    );
    expect(posts).toHaveLength(0);

    fireEvent.change(field, { target: { value: "123456" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(field).toHaveAccessibleDescription(/That code is wrong/),
    );
    expect(field).toHaveValue("");
    expect(action).not.toHaveBeenCalled();

    fireEvent.change(field, { target: { value: "654 321" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    await expect(posts[1]?.json()).resolves.toEqual({
      code: "654321",
      method: "totp",
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("orders passkey before the authenticator app for an AAL1 session", async () => {
    installWebAuthn();
    installMock(security(["totp", "passkey"]));
    const { dialog } = await openDialog();

    const methods = await within(dialog).findByRole("group", {
      name: "Confirmation method",
    });
    expect(
      within(methods)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["Passkey", "Authenticator app"]);
  });

  it("offers only AAL2 factors when the action needs two-step verification", async () => {
    installWebAuthn();
    installMock(security(["passkey", "password", "email_code"]));
    const { dialog } = await openDialog({ requireAal2: true });

    expect(dialog).toHaveAccessibleDescription(/needs two-step verification/);
    expect(
      await within(dialog).findByRole("button", {
        name: "Confirm with passkey",
      }),
    ).toBeInTheDocument();
    // Password and email code would keep the session at AAL1.
    expect(within(dialog).queryByLabelText("Current password")).toBeNull();
    expect(
      within(dialog).queryByRole("button", { name: "Email code" }),
    ).toBeNull();
  });

  it("explains how to add a second factor when none can be used", async () => {
    installMock(security(["password", "email_code"]));
    const { action, dialog } = await openDialog({ requireAal2: true });

    expect(
      await within(dialog).findByText(
        /no passkey or authenticator app that can complete two-step verification/,
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("link", { name: "Open security settings" }),
    ).toHaveAttribute("href", "/app/settings");
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(action).not.toHaveBeenCalled();
  });

  it("says when the only second factor is a passkey this browser cannot use", async () => {
    installMock(security(["passkey", "password"]));
    const { dialog } = await openDialog({ requireAal2: true });

    expect(
      await within(dialog).findByText(
        /can only complete two-step verification with a passkey/,
      ),
    ).toBeInTheDocument();
  });
});
