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
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { createTutorHubQueryClient } from "../app/queryClient";
import { SessionProvider } from "../app/session";
import { ProfileSettingsPage } from "./ProfileSettingsPage";

const profile = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
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

const SECRET = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
const enrollment = {
  account_name: "teacher@example.com",
  issuer: "TutorHub",
  otpauth_uri: `otpauth://totp/TutorHub:teacher%40example.com?algorithm=SHA1&digits=6&issuer=TutorHub&period=30&secret=${SECRET}`,
  secret: SECRET,
};
const CODES = Array.from(
  { length: 10 },
  (_, index) => `AB${index}D-EFGH-JKMN-PQR${index}`,
);

const securityOff: AccountSecurityResponse = {
  email_verified: true,
  has_password: true,
  mfa_enabled: false,
  passkey_count: 0,
  reauth_methods: ["password", "email_code"],
  recent_auth_expires_at: null,
  recovery_codes_remaining: 0,
};
const securityOn = (remaining = 10): AccountSecurityResponse => ({
  ...securityOff,
  mfa_enabled: true,
  reauth_methods: ["totp"],
  recovery_codes_remaining: remaining,
});

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

type Handler = () => Response;

interface MockOptions {
  confirm?: Handler[];
  disable?: Handler[];
  enroll?: Handler[];
  regenerate?: Handler[];
  security?: AccountSecurityResponse[];
}

function installMock(options: MockOptions = {}) {
  const calls: string[] = [];
  const bodies: Array<{ body: string; path: string }> = [];
  const securityQueue = [...(options.security ?? [securityOff])];
  const queues = {
    confirm: [...(options.confirm ?? [])],
    disable: [...(options.disable ?? [])],
    enroll: [...(options.enroll ?? [])],
    regenerate: [...(options.regenerate ?? [])],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const url = new URL(request.url);
      const path = url.pathname;
      calls.push(`${request.method} ${url.origin}${path}`);
      if (request.method !== "GET") {
        bodies.push({ body: await request.clone().text(), path });
      }
      if (request.method === "GET") {
        switch (path) {
          case "/api/v1/me/profile":
            return json(profile);
          case "/api/v1/me/identities":
            return json({ identities: [] });
          case "/api/v1/me/password":
            return json({ has_password: true, password_enabled: true });
          case "/api/v1/auth/providers":
            return json(providers);
          case "/api/v1/me/security": {
            const next =
              securityQueue.length > 1
                ? securityQueue.shift()
                : securityQueue[0];
            return json(next);
          }
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
      if (path === "/api/v1/me/mfa/totp/enroll") {
        return (queues.enroll.shift() ?? (() => json(enrollment)))();
      }
      if (path === "/api/v1/me/mfa/totp/confirm") {
        return (
          queues.confirm.shift() ?? (() => json({ recovery_codes: CODES }))
        )();
      }
      if (path === "/api/v1/me/mfa/recovery-codes") {
        return (
          queues.regenerate.shift() ?? (() => json({ recovery_codes: CODES }))
        )();
      }
      if (path === "/api/v1/me/mfa/totp" && request.method === "DELETE") {
        return (
          queues.disable.shift() ?? (() => new Response(null, { status: 204 }))
        )();
      }
      if (path === "/api/v1/auth/reauth" && request.method === "POST") {
        return json({ recent_auth_expires_at: "2026-11-20T10:10:00Z" });
      }
      throw new Error(`Unexpected request: ${request.method} ${path}`);
    }),
  );
  return { bodies, calls };
}

function renderSettings() {
  render(
    <QueryClientProvider client={createTutorHubQueryClient()}>
      <MemoryRouter initialEntries={["/app/settings"]}>
        <I18nProvider initialLanguage="en">
          <SessionProvider mode={{ kind: "static", currentUser: session }}>
            <ProfileSettingsPage />
          </SessionProvider>
        </I18nProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

async function totpCard() {
  return screen.findByRole("region", { name: "Authenticator app" });
}

function expectNoSecretsInWebStorage() {
  const stored = JSON.stringify({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  });
  for (const value of [SECRET, enrollment.otpauth_uri, ...CODES]) {
    expect(stored).not.toContain(value);
  }
}

describe("ProfileSettingsPage authenticator app (AUTH-09B)", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    window.localStorage.clear();
    window.sessionStorage.clear();
  });

  it("enrolls with a local QR code, confirms, resyncs the session and shows the recovery codes once", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    vi.stubGlobal("navigator", {
      ...window.navigator,
      clipboard: { writeText },
    });
    const { bodies, calls } = installMock({
      confirm: [() => problem(401, "auth_invalid_verification_code")],
      security: [securityOff, securityOn()],
    });
    renderSettings();
    const card = await totpCard();
    expect(await within(card).findByText("Off")).toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", { name: "Set up authenticator app" }),
    );
    const heading = await within(card).findByRole("heading", {
      name: "Add TutorHub to your authenticator app",
    });
    await waitFor(() => expect(heading).toHaveFocus());

    const qr = within(card).getByRole("img", {
      name: "QR code that adds TutorHub to your authenticator app",
    });
    expect(qr.tagName.toLowerCase()).toBe("svg");
    expect(qr.querySelector("path")?.getAttribute("d")).toMatch(/^M4 4h7v1/);
    expect(qr.getAttribute("aria-label")).not.toContain(SECRET);
    // The QR code is drawn locally: every request stayed on the API origin.
    expect(calls.every((call) => call.includes("/api/v1/"))).toBe(true);
    expect(
      within(card).getByText("JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP"),
    ).toBeInTheDocument();
    expect(within(card).getByText("teacher@example.com")).toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", { name: "Copy setup key" }),
    );
    expect(
      await within(card).findByText("Setup key copied."),
    ).toBeInTheDocument();
    expect(writeText).toHaveBeenCalledWith(SECRET);

    const code = within(card).getByLabelText("6-digit code from the app");
    fireEvent.change(code, { target: { value: "12" } });
    fireEvent.click(
      within(card).getByRole("button", { name: "Verify and turn on" }),
    );
    expect(code).toHaveAccessibleDescription(/exactly 6 digits/);

    fireEvent.change(code, { target: { value: "111111" } });
    fireEvent.click(
      within(card).getByRole("button", { name: "Verify and turn on" }),
    );
    await waitFor(() =>
      expect(code).toHaveAccessibleDescription(/That code is wrong/),
    );
    expect(code).toHaveValue("");

    fireEvent.change(code, { target: { value: "222222" } });
    fireEvent.click(
      within(card).getByRole("button", { name: "Verify and turn on" }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: "Save your recovery codes",
    });
    const list = within(dialog).getByRole("list", { name: "Recovery codes" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(10);
    expect(bodies.at(-1)).toEqual({
      body: JSON.stringify({ code: "222222" }),
      path: "/api/v1/me/mfa/totp/confirm",
    });
    // The rotated session and CSRF cookies are resynced right away.
    const confirmed = calls.lastIndexOf(
      "POST http://localhost:3000/api/v1/me/mfa/totp/confirm",
    );
    expect(calls.slice(confirmed + 1)).toContain(
      "GET http://localhost:3000/api/v1/auth/csrf",
    );

    // The codes cannot be dismissed before the user confirms saving them.
    const done = within(dialog).getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    fireEvent.keyDown(dialog, { key: "Escape" });
    expect(
      screen.getByRole("dialog", { name: "Save your recovery codes" }),
    ).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Copy all" }));
    expect(
      await within(dialog).findByText("All 10 recovery codes copied."),
    ).toBeInTheDocument();
    expect(writeText).toHaveBeenLastCalledWith(CODES.join("\n"));

    fireEvent.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved these recovery codes somewhere safe",
      }),
    );
    fireEvent.click(done);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.queryByText(CODES[0] ?? "")).toBeNull();
    expect(
      await within(card).findByText("10 of 10 recovery codes left."),
    ).toBeInTheDocument();
    expect(within(card).getByText("On")).toBeInTheDocument();
    expect(within(card).queryByRole("img")).toBeNull();
    expectNoSecretsInWebStorage();
  });

  it("drops the enrollment on cancel and returns focus to the setup button", async () => {
    installMock();
    renderSettings();
    const card = await totpCard();
    fireEvent.click(
      await within(card).findByRole("button", {
        name: "Set up authenticator app",
      }),
    );
    await within(card).findByRole("img");
    fireEvent.click(within(card).getByRole("button", { name: "Cancel" }));
    expect(within(card).queryByRole("img")).toBeNull();
    expect(within(card).queryByText(/JBSW/)).toBeNull();
    await waitFor(() =>
      expect(
        within(card).getByRole("button", { name: "Set up authenticator app" }),
      ).toHaveFocus(),
    );
  });

  it("steps up once for a stale session, then continues the enrollment", async () => {
    const { calls } = installMock({
      enroll: [() => problem(403, "auth_recent_authentication_required")],
    });
    renderSettings();
    const card = await totpCard();
    fireEvent.click(
      await within(card).findByRole("button", {
        name: "Set up authenticator app",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    fireEvent.change(await within(dialog).findByLabelText("Current password"), {
      target: { value: "Right-passphrase" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(await within(card).findByRole("img")).toBeInTheDocument();
    expect(
      calls.filter((call) => call.endsWith("/api/v1/me/mfa/totp/enroll")),
    ).toHaveLength(2);
  });

  it("asks for an AAL2 factor when the account already has one (auth_assurance_required)", async () => {
    vi.stubGlobal(
      "PublicKeyCredential",
      Object.assign(function PublicKeyCredential() {}, {}),
    );
    Object.defineProperty(window.navigator, "credentials", {
      configurable: true,
      value: { create: vi.fn(), get: vi.fn() },
    });
    installMock({
      enroll: [() => problem(403, "auth_assurance_required")],
      security: [
        {
          ...securityOff,
          passkey_count: 1,
          reauth_methods: ["passkey", "password", "email_code"],
        },
      ],
    });
    renderSettings();
    const card = await totpCard();
    fireEvent.click(
      await within(card).findByRole("button", {
        name: "Set up authenticator app",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    expect(dialog).toHaveAccessibleDescription(/needs two-step verification/);
    expect(
      await within(dialog).findByRole("button", {
        name: "Confirm with passkey",
      }),
    ).toBeInTheDocument();
    expect(within(dialog).queryByLabelText("Current password")).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(within(card).getByRole("alert")).toHaveTextContent(
      /this change needs two-step verification/,
    );
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("warns about few recovery codes, regenerates them after confirmation and turns the app off", async () => {
    const { calls } = installMock({
      security: [securityOn(2), securityOn(10), securityOff],
    });
    renderSettings();
    const card = await totpCard();
    expect(
      await within(card).findByText("2 of 10 recovery codes left."),
    ).toBeInTheDocument();
    expect(
      within(card).getByText(/running low on recovery codes/),
    ).toBeInTheDocument();
    // The overview card mirrors the state.
    const overview = screen.getByRole("region", { name: "Account security" });
    expect(
      await within(overview).findByText("2/10 recovery codes"),
    ).toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", { name: "Generate new recovery codes" }),
    );
    const confirmDialog = await screen.findByRole("dialog", {
      name: "Generate new recovery codes?",
    });
    expect(confirmDialog).toHaveAccessibleDescription(
      /stops working immediately/,
    );
    fireEvent.click(
      within(confirmDialog).getByRole("button", { name: "Generate new codes" }),
    );
    const codesDialog = await screen.findByRole("dialog", {
      name: "Save your recovery codes",
    });
    fireEvent.click(within(codesDialog).getByRole("checkbox"));
    fireEvent.click(within(codesDialog).getByRole("button", { name: "Done" }));
    expect(
      await within(card).findByText("10 of 10 recovery codes left."),
    ).toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", { name: "Turn off authenticator app" }),
    );
    const disableDialog = await screen.findByRole("dialog", {
      name: "Turn off the authenticator app?",
    });
    expect(disableDialog).toHaveAccessibleDescription(
      /every current recovery code is also cancelled/,
    );
    fireEvent.click(
      within(disableDialog).getByRole("button", {
        name: "Turn off two-step verification",
      }),
    );
    expect(
      await within(card).findByText(
        "The authenticator app was turned off and its recovery codes cancelled.",
      ),
    ).toBeInTheDocument();
    expect(
      await within(card).findByRole("button", {
        name: "Set up authenticator app",
      }),
    ).toBeInTheDocument();
    expect(calls).toContain("DELETE http://localhost:3000/api/v1/me/mfa/totp");
  });

  it("reloads the status after a factor conflict (409 auth_mfa_conflict)", async () => {
    const { calls } = installMock({
      regenerate: [() => problem(409, "auth_mfa_conflict")],
      security: [securityOn(5), securityOff],
    });
    renderSettings();
    const card = await totpCard();
    fireEvent.click(
      await within(card).findByRole("button", {
        name: "Generate new recovery codes",
      }),
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Generate new recovery codes?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Generate new codes" }),
    );
    expect(
      await within(card).findByText(/Two-step verification changed meanwhile/),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        calls.filter((call) => call.endsWith("/api/v1/me/security")).length,
      ).toBeGreaterThanOrEqual(2),
    );
  });
});
