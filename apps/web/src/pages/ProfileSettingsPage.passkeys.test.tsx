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
  AuthProvidersResponse,
  CurrentUser,
  MyLoginMethodsResponse,
  MyPasskey,
  MySecurityResponse,
} from "@tutorhub/api-client";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { createTutorHubQueryClient } from "../app/queryClient";
import type * as SessionModule from "../app/session";
import { SessionProvider } from "../app/session";
import { ProfileSettingsPage } from "./ProfileSettingsPage";

const { sessionActions } = vi.hoisted(() => ({
  sessionActions: { signIn: vi.fn(), signOut: vi.fn() },
}));

vi.mock("../app/session", async (importOriginal) => {
  const actual = await importOriginal<typeof SessionModule>();
  return {
    ...actual,
    useSession: () => ({ ...actual.useSession(), ...sessionActions }),
  };
});

const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const firstID = "6f9b0f86-5d1f-4bdb-9d8c-0a1b2c3d4e5f";
const secondID = "8e7d6c5b-4a39-4281-9706-f5e4d3c2b1a0";
const googleIdentityID = "41d58a8a-8f20-4828-a3f7-a6f5fa463954";
const WINDOWS_CHROME =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36";

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
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
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

const syncedPasskey: MyPasskey = {
  backup_eligible: true,
  backup_state: true,
  created_at: "2026-11-18T07:00:00Z",
  id: firstID,
  label: "Windows · Chrome",
  last_used_at: "2026-11-19T08:00:00Z",
  transports: ["internal", "hybrid"],
};

const devicePasskey: MyPasskey = {
  backup_eligible: false,
  backup_state: false,
  created_at: "2026-11-19T07:00:00Z",
  id: secondID,
  label: "Security key",
  last_used_at: null,
  transports: ["usb"],
};

const security: MySecurityResponse = {
  email_verified: true,
  has_password: true,
  mfa_enabled: false,
  passkey_count: 2,
  reauth_methods: ["password", "email_code"],
  recent_auth_expires_at: null,
};

const loginMethods: MyLoginMethodsResponse = {
  identities: [],
  passkey_count: 2,
  password: { has_password: true, updated_at: "2026-11-10T08:00:00Z" },
};

const creationOptions = {
  attestation: "none",
  authenticatorSelection: {
    residentKey: "required",
    userVerification: "preferred",
  },
  challenge: "AAEC",
  excludeCredentials: [{ id: "AQID", type: "public-key" }],
  pubKeyCredParams: [{ alg: -7, type: "public-key" }],
  rp: { id: "localhost", name: "TutorHub" },
  user: { displayName: "Teacher", id: "CQoL", name: "teacher@example.com" },
};

function buffer(...values: number[]) {
  return new Uint8Array(values).buffer;
}

const attestationCredential = {
  authenticatorAttachment: "platform",
  id: "BAUG",
  rawId: buffer(4, 5, 6),
  response: {
    attestationObject: buffer(10),
    clientDataJSON: buffer(11),
    getTransports: () => ["internal"],
  },
  type: "public-key",
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

function problem(status: number, code?: string) {
  return jsonResponse(
    { type: "about:blank", title: "Rejected", status, code },
    status,
  );
}

function requestFrom(input: RequestInfo | URL, init?: RequestInit) {
  return input instanceof Request ? input : new Request(input, init);
}

type Handler = () => Response;

interface MockOptions {
  identities?: unknown[];
  passkeys?: Handler[];
  providers?: AuthProvidersResponse;
  register?: Handler[];
  rename?: Handler[];
  revoke?: Handler[];
  verify?: Handler[];
}

function installMock(options: MockOptions = {}) {
  const calls: string[] = [];
  const bodies: Array<{ body: string; path: string }> = [];
  const passkeysQueue = [...(options.passkeys ?? [])];
  const registerQueue = [...(options.register ?? [])];
  const verifyQueue = [...(options.verify ?? [])];
  const renameQueue = [...(options.rename ?? [])];
  const revokeQueue = [...(options.revoke ?? [])];
  let csrf = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = requestFrom(input, init);
      const path = new URL(request.url).pathname;
      calls.push(`${request.method} ${path}`);
      if (request.method !== "GET") {
        bodies.push({ body: await request.clone().text(), path });
      }
      if (request.method === "GET") {
        switch (path) {
          case "/api/v1/me/profile":
            return jsonResponse(profile);
          case "/api/v1/me/identities":
            return jsonResponse({ identities: options.identities ?? [] });
          case "/api/v1/me/password":
            return jsonResponse({ has_password: true, password_enabled: true });
          case "/api/v1/auth/providers":
            return jsonResponse(options.providers ?? providers);
          case "/api/v1/me/security":
            return jsonResponse(security);
          case "/api/v1/me/login-methods":
            return jsonResponse(loginMethods);
          case "/api/v1/me/sessions":
            return jsonResponse({ sessions: [] });
          case "/api/v1/auth/csrf":
            csrf += 1;
            return jsonResponse({ csrf_token: `csrf-${csrf}` });
          case "/api/v1/me/passkeys":
            return (
              passkeysQueue.shift() ??
              (() => jsonResponse({ passkeys: [syncedPasskey, devicePasskey] }))
            )();
        }
      }
      if (path === "/api/v1/me/passkeys/registration/options") {
        return (
          registerQueue.shift() ?? (() => jsonResponse(creationOptions))
        )();
      }
      if (path === "/api/v1/me/passkeys/registration/verify") {
        return (
          verifyQueue.shift() ??
          (() =>
            jsonResponse(
              { ...devicePasskey, id: secondID, label: "Work laptop" },
              201,
            ))
        )();
      }
      if (
        path.startsWith("/api/v1/me/passkeys/") &&
        request.method === "PATCH"
      ) {
        return (
          renameQueue.shift() ??
          (() => jsonResponse({ ...syncedPasskey, label: "Renamed key" }))
        )();
      }
      if (
        path.startsWith("/api/v1/me/passkeys/") &&
        request.method === "DELETE"
      ) {
        return (
          revokeQueue.shift() ?? (() => new Response(null, { status: 204 }))
        )();
      }
      if (path === "/api/v1/auth/reauth" && request.method === "POST") {
        return jsonResponse({ recent_auth_expires_at: "2026-11-19T10:10:00Z" });
      }
      throw new Error(`Unexpected request: ${request.method} ${request.url}`);
    }),
  );
  return { bodies, calls };
}

function installWebAuthn(create: Array<() => Promise<unknown>> = []) {
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {}),
  );
  const createMock = vi.fn(() =>
    (create.shift() ?? (() => Promise.resolve(attestationCredential)))(),
  );
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: { create: createMock, get: vi.fn() },
  });
  Object.defineProperty(window.navigator, "userAgent", {
    configurable: true,
    value: WINDOWS_CHROME,
  });
  return createMock;
}

function renderSettings() {
  const queryClient = createTutorHubQueryClient();
  render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/app/settings"]}>
        <I18nProvider initialLanguage="en">
          <SessionProvider mode={{ kind: "static", currentUser: session }}>
            <ProfileSettingsPage />
          </SessionProvider>
        </I18nProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return queryClient;
}

async function passkeysCard() {
  return screen.findByRole("region", { name: "Passkeys" });
}

describe("ProfileSettingsPage passkeys", () => {
  beforeEach(() => {
    sessionActions.signIn.mockReset();
    sessionActions.signOut.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
    Reflect.deleteProperty(window.navigator, "userAgent");
  });

  it("stays away from the passkey endpoints while passkeys are disabled", async () => {
    const { calls } = installMock({
      providers: { ...providers, passkeys_enabled: false },
    });
    renderSettings();
    expect(
      await screen.findByRole("heading", { name: "Account security" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("region", { name: "Passkeys" }),
    ).not.toBeInTheDocument();
    expect(calls).not.toContain("GET /api/v1/me/passkeys");
  });

  it("lists passkeys with sync badges and dates, and shows the counts in the security card", async () => {
    installWebAuthn();
    installMock();
    renderSettings();
    const card = await passkeysCard();

    expect(
      await within(card).findByText("Windows · Chrome"),
    ).toBeInTheDocument();
    const rows = within(card)
      .getAllByRole("listitem")
      .filter((item) => item.classList.contains("passkey-row"));
    expect(rows).toHaveLength(2);
    expect(
      within(rows[0] as HTMLElement).getByText("Synced"),
    ).toBeInTheDocument();
    expect(
      within(rows[1] as HTMLElement).getByText("Device-bound"),
    ).toBeInTheDocument();
    expect(
      within(rows[1] as HTMLElement).getByText("Not used to sign in yet"),
    ).toBeInTheDocument();
    expect(
      within(rows[0] as HTMLElement).getByText(/^Created/),
    ).toBeInTheDocument();
    expect(
      within(rows[0] as HTMLElement).getByText(/^Last used/),
    ).toBeInTheDocument();

    const securityCard = screen.getByRole("region", {
      name: "Account security",
    });
    expect(
      await within(securityCard).findByText("2 passkey(s)"),
    ).toBeInTheDocument();
    expect(
      await within(securityCard).findByText("2 active passkey(s)"),
    ).toBeInTheDocument();
  });

  it("shows the empty state and recovers from a load error with Retry", async () => {
    installWebAuthn();
    installMock({
      passkeys: [() => problem(503), () => jsonResponse({ passkeys: [] })],
    });
    renderSettings();
    const card = await passkeysCard();

    expect(
      await within(card).findByRole("heading", {
        name: "Passkeys unavailable",
      }),
    ).toBeInTheDocument();
    fireEvent.click(within(card).getByRole("button", { name: "Try again" }));
    expect(
      await within(card).findByRole("heading", { name: "No passkeys yet" }),
    ).toBeInTheDocument();
  });

  it("adds a passkey with a suggested, editable name after a step-up", async () => {
    const create = installWebAuthn();
    const { bodies, calls } = installMock({
      register: [() => problem(403, "auth_recent_authentication_required")],
    });
    renderSettings();
    const card = await passkeysCard();
    await within(card).findByText("Windows · Chrome");

    fireEvent.click(
      within(card).getByRole("button", { name: "Add a passkey" }),
    );
    const name = within(card).getByLabelText("Passkey name");
    expect(name).toHaveValue("Windows · Chrome");
    expect(name).toHaveFocus();

    fireEvent.change(name, { target: { value: "   " } });
    fireEvent.click(
      within(card).getByRole("button", { name: "Create passkey" }),
    );
    expect(name).toHaveAccessibleDescription(
      expect.stringContaining("Enter a name for the passkey."),
    );
    fireEvent.change(name, { target: { value: "x".repeat(81) } });
    fireEvent.click(
      within(card).getByRole("button", { name: "Create passkey" }),
    );
    expect(name).toHaveAccessibleDescription(
      expect.stringContaining("at most 80 characters"),
    );
    expect(calls).not.toContain(
      "POST /api/v1/me/passkeys/registration/options",
    );

    fireEvent.change(name, { target: { value: "  Work laptop " } });
    fireEvent.click(
      within(card).getByRole("button", { name: "Create passkey" }),
    );

    // 403 recent-auth → the shared step-up dialog, then exactly one retry.
    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    fireEvent.change(await within(dialog).findByLabelText("Current password"), {
      target: { value: "Right-passphrase" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    expect(
      await within(card).findByText('Passkey "Work laptop" added.'),
    ).toBeInTheDocument();
    expect(create).toHaveBeenCalledTimes(1);
    expect(
      calls.filter(
        (call) => call === "POST /api/v1/me/passkeys/registration/options",
      ),
    ).toHaveLength(2);
    const verify = bodies.find(
      (entry) => entry.path === "/api/v1/me/passkeys/registration/verify",
    );
    expect(JSON.parse(verify?.body ?? "{}")).toMatchObject({
      attestation: {
        id: "BAUG",
        rawId: "BAUG",
        response: {
          attestationObject: "Cg",
          clientDataJSON: "Cw",
          transports: ["internal"],
        },
        type: "public-key",
      },
      label: "Work laptop",
    });
    await waitFor(() =>
      expect(
        within(card).getByRole("button", { name: "Add a passkey" }),
      ).toHaveFocus(),
    );
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("explains a duplicate passkey and a closed prompt", async () => {
    installWebAuthn([
      () => Promise.reject(new DOMException("closed", "NotAllowedError")),
      () => Promise.resolve(attestationCredential),
      () => Promise.reject(new DOMException("known", "InvalidStateError")),
    ]);
    installMock({ verify: [() => problem(409, "auth_passkey_exists")] });
    renderSettings();
    const card = await passkeysCard();
    await within(card).findByText("Windows · Chrome");
    fireEvent.click(
      within(card).getByRole("button", { name: "Add a passkey" }),
    );
    const create = within(card).getByRole("button", { name: "Create passkey" });

    fireEvent.click(create);
    expect(
      await within(card).findByText("Passkey creation was cancelled."),
    ).toBeInTheDocument();
    expect(within(card).queryByRole("alert")).not.toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", { name: "Create passkey" }),
    );
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "This passkey is already registered to your account.",
    );

    fireEvent.click(
      within(card).getByRole("button", { name: "Create passkey" }),
    );
    await waitFor(() =>
      expect(within(card).getByRole("alert")).toHaveTextContent(
        "This passkey is already registered to your account.",
      ),
    );
  });

  it("renames in place and resumes after a step-up", async () => {
    installWebAuthn();
    const { bodies } = installMock({
      rename: [() => problem(403, "auth_recent_authentication_required")],
    });
    renderSettings();
    const card = await passkeysCard();
    await within(card).findByText("Windows · Chrome");

    fireEvent.click(
      within(card).getByRole("button", {
        name: "Rename passkey Windows · Chrome",
      }),
    );
    const form = within(card).getByRole("form", {
      name: "Rename passkey Windows · Chrome",
    });
    const field = within(form).getByLabelText("Passkey name");
    expect(field).toHaveFocus();
    fireEvent.change(field, { target: { value: "Renamed key" } });
    fireEvent.click(within(form).getByRole("button", { name: "Save name" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Confirm it's you",
    });
    fireEvent.change(await within(dialog).findByLabelText("Current password"), {
      target: { value: "Right-passphrase" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    expect(
      await within(card).findByText("Passkey renamed."),
    ).toBeInTheDocument();
    const patches = bodies.filter((entry) =>
      entry.path.endsWith(`/api/v1/me/passkeys/${firstID}`),
    );
    expect(patches).toHaveLength(2);
    expect(JSON.parse(patches[1]?.body ?? "{}")).toEqual({
      label: "Renamed key",
    });
  });

  it("confirms before removing, explains the last sign-in method and then removes", async () => {
    installWebAuthn();
    const { calls } = installMock({
      revoke: [() => problem(409, "auth_last_identity")],
    });
    renderSettings();
    const card = await passkeysCard();
    await within(card).findByText("Security key");

    fireEvent.click(
      within(card).getByRole("button", { name: "Remove passkey Security key" }),
    );
    let dialog = await screen.findByRole("dialog", {
      name: "Remove this passkey?",
    });
    expect(dialog).toHaveTextContent(/"Security key"/);
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(calls.some((call) => call.startsWith("DELETE"))).toBe(false);

    fireEvent.click(
      within(card).getByRole("button", { name: "Remove passkey Security key" }),
    );
    dialog = await screen.findByRole("dialog", {
      name: "Remove this passkey?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Remove passkey" }),
    );
    expect(await within(card).findByRole("alert")).toHaveTextContent(
      "You can't remove this passkey because it's your last sign-in method.",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    fireEvent.click(
      within(card).getByRole("button", { name: "Remove passkey Security key" }),
    );
    dialog = await screen.findByRole("dialog", {
      name: "Remove this passkey?",
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Remove passkey" }),
    );
    expect(
      await within(card).findByText("Passkey removed."),
    ).toBeInTheDocument();
    expect(
      calls.filter((call) => call === `DELETE /api/v1/me/passkeys/${secondID}`),
    ).toHaveLength(2);
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Passkeys" })).toHaveFocus(),
    );
  });

  it("lets a federated identity be unlinked once a passkey exists", async () => {
    installWebAuthn();
    installMock({
      identities: [
        {
          created_at: "2026-11-01T08:00:00Z",
          email: "teacher@example.com",
          email_verified: true,
          id: googleIdentityID,
          last_authenticated_at: "2026-11-18T08:00:00Z",
          provider: "https://accounts.google.com",
          provider_slug: "google",
        },
      ],
    });
    renderSettings();
    const unlink = await screen.findByRole("button", { name: "Unlink" });
    await waitFor(() => expect(unlink).toBeEnabled());
    expect(
      screen.queryByText("The last sign-in method cannot be removed."),
    ).not.toBeInTheDocument();
  });

  it("explains that this browser cannot create passkeys", async () => {
    installMock();
    renderSettings();
    const card = await passkeysCard();
    expect(
      within(card).getByRole("button", { name: "Add a passkey" }),
    ).toBeDisabled();
    expect(
      within(card).getByText(
        "This browser can't create passkeys. You can still rename or remove existing ones.",
      ),
    ).toBeInTheDocument();
  });
});
