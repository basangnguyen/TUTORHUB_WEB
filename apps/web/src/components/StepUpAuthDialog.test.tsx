import { QueryClientProvider } from "@tanstack/react-query";
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
  type MySecurityResponse,
} from "@tutorhub/api-client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { createTutorHubQueryClient } from "../app/queryClient";
import { useStepUpRequest } from "../app/security";
import type * as SessionModule from "../app/session";
import { SessionProvider } from "../app/session";
import { StepUpAuthDialog } from "./StepUpAuthDialog";

const { sessionActions } = vi.hoisted(() => ({
  sessionActions: { refresh: vi.fn(), signIn: vi.fn(), signOut: vi.fn() },
}));

// Keep the real static session; only observe the account actions.
vi.mock("../app/session", async (importOriginal) => {
  const actual = await importOriginal<typeof SessionModule>();
  return {
    ...actual,
    useSession: () => ({ ...actual.useSession(), ...sessionActions }),
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

const providers: AuthProvidersResponse = {
  device_management_enabled: true,
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

const security: MySecurityResponse = {
  email_verified: true,
  has_password: true,
  mfa_enabled: false,
  reauth_methods: ["email_code", "password"],
  recent_auth_expires_at: null,
};

const challengeID = "6a0d1f43-4f39-4f1b-9b7e-1f7c5d8f0a11";
const privateKey = ["tenants", "private"] as const;

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

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

function requestFrom(input: RequestInfo | URL, init?: RequestInit) {
  return input instanceof Request ? input : new Request(input, init);
}

interface MockOptions {
  passkeyOptions?: Array<() => Response>;
  providers?: AuthProvidersResponse;
  reauth?: Array<() => Response>;
  security?: Array<() => Response>;
  start?: Array<() => Response>;
}

const passkeyRequestOptions = {
  allowCredentials: [{ id: "AQID", type: "public-key" }],
  challenge: "Y2hhbGxlbmdl",
  rpId: "localhost",
  timeout: 300000,
  userVerification: "required",
};

function installMock(options: MockOptions = {}) {
  const calls: string[] = [];
  const posts: Request[] = [];
  const reauthQueue = [...(options.reauth ?? [])];
  const securityQueue = [...(options.security ?? [])];
  const startQueue = [...(options.start ?? [])];
  const passkeyQueue = [...(options.passkeyOptions ?? [])];
  let csrfCount = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = requestFrom(input, init);
      const path = new URL(request.url).pathname;
      calls.push(`${request.method} ${path}`);
      if (path === "/api/v1/auth/providers" && request.method === "GET") {
        return jsonResponse(options.providers ?? providers);
      }
      if (path === "/api/v1/me/security" && request.method === "GET") {
        return (securityQueue.shift() ?? (() => jsonResponse(security)))();
      }
      if (path === "/api/v1/auth/csrf" && request.method === "GET") {
        csrfCount += 1;
        return jsonResponse({ csrf_token: `step-up-csrf-${csrfCount}` });
      }
      if (
        path === "/api/v1/auth/reauth/email-code/start" &&
        request.method === "POST"
      ) {
        posts.push(request.clone());
        const next = startQueue.shift();
        return next
          ? next()
          : new Response(
              JSON.stringify({
                challenge_id: challengeID,
                cooldown_seconds: 60,
                expires_in_seconds: 600,
              }),
              {
                status: 202,
                headers: { "Content-Type": "application/json" },
              },
            );
      }
      if (
        path === "/api/v1/auth/reauth/passkey/options" &&
        request.method === "POST"
      ) {
        posts.push(request.clone());
        return (
          passkeyQueue.shift() ?? (() => jsonResponse(passkeyRequestOptions))
        )();
      }
      if (path === "/api/v1/auth/reauth" && request.method === "POST") {
        posts.push(request.clone());
        const next = reauthQueue.shift();
        return next
          ? next()
          : jsonResponse({ recent_auth_expires_at: "2026-11-18T10:10:00Z" });
      }
      throw new Error(`Unexpected request: ${request.method} ${request.url}`);
    }),
  );
  return { calls, posts };
}

function buffer(...values: number[]) {
  return new Uint8Array(values).buffer;
}

const fakeAssertion = {
  authenticatorAttachment: "platform",
  id: "AQID",
  rawId: buffer(1, 2, 3),
  response: {
    authenticatorData: buffer(4),
    clientDataJSON: buffer(5),
    signature: buffer(6),
    userHandle: null,
  },
  type: "public-key",
};

const serializedAssertion = {
  authenticatorAttachment: "platform",
  clientExtensionResults: {},
  id: "AQID",
  rawId: "AQID",
  response: {
    authenticatorData: "BA",
    clientDataJSON: "BQ",
    signature: "Bg",
    userHandle: null,
  },
  type: "public-key",
};

/** A WebAuthn-capable browser whose device prompt follows `outcomes`. */
function installWebAuthn(outcomes: Array<() => Promise<unknown>>) {
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {}),
  );
  const get = vi.fn<(options?: CredentialRequestOptions) => Promise<unknown>>(
    () => (outcomes.shift() ?? (() => Promise.resolve(fakeAssertion)))(),
  );
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get },
  });
  return get;
}

function Harness({ action }: { action: () => void }) {
  const stepUp = useStepUpRequest();
  return (
    <>
      <button onClick={() => stepUp.request(action)} type="button">
        Change protected setting
      </button>
      <StepUpAuthDialog {...stepUp.dialog} returnTo="/app/settings" />
    </>
  );
}

function renderHarness() {
  const action = vi.fn();
  const queryClient = createTutorHubQueryClient();
  queryClient.setQueryData(privateKey, { name: "Private workspace" });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <Harness action={action} />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { action, queryClient };
}

async function openDialog() {
  const trigger = screen.getByRole("button", {
    name: "Change protected setting",
  });
  trigger.focus();
  fireEvent.click(trigger);
  const dialog = await screen.findByRole("dialog", {
    name: "Confirm it's you",
  });
  return { dialog, trigger };
}

describe("StepUpAuthDialog", () => {
  beforeEach(() => {
    sessionActions.refresh.mockReset().mockResolvedValue(undefined);
    sessionActions.signIn.mockReset();
    sessionActions.signOut.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("re-authenticates with the password, resyncs CSRF and resumes the blocked action once", async () => {
    const { calls, posts } = installMock({
      reauth: [() => problemResponse(401, "auth_invalid_current_password")],
    });
    const { action, queryClient } = renderHarness();
    const { dialog, trigger } = await openDialog();

    const password = await within(dialog).findByLabelText("Current password");
    await waitFor(() => expect(password).toHaveFocus());
    const methods = within(dialog).getByRole("group", {
      name: "Confirmation method",
    });
    // Password is offered first even though the server listed it second.
    const options = within(methods).getAllByRole("button");
    expect(options.map((option) => option.textContent)).toEqual([
      "Password",
      "Email code",
    ]);
    expect(options[0]).toHaveAttribute("aria-pressed", "true");

    fireEvent.change(password, { target: { value: "Wrong-passphrase" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(password).toHaveAccessibleDescription(
        "The password is incorrect or temporarily locked. Please try again.",
      ),
    );
    expect(password).toHaveValue("");
    expect(password).toHaveFocus();
    // A rejected typed credential never ends the session.
    expect(queryClient.getQueryData(privateKey)).toEqual({
      name: "Private workspace",
    });
    expect(action).not.toHaveBeenCalled();

    fireEvent.change(password, { target: { value: "Right-passphrase" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(posts).toHaveLength(2);
    await expect(posts[1]?.json()).resolves.toEqual({
      method: "password",
      password: "Right-passphrase",
    });
    // Each POST rotates first; the success path resyncs the rotated cookie.
    const lastReauth = calls.lastIndexOf("POST /api/v1/auth/reauth");
    expect(calls[lastReauth - 1]).toBe("GET /api/v1/auth/csrf");
    expect(posts[1]?.headers.get("X-CSRF-Token")).toBe("step-up-csrf-2");
    expect(calls.slice(lastReauth + 1)).toContain("GET /api/v1/auth/csrf");
    await waitFor(() => expect(getCurrentCSRFToken()).toBe("step-up-csrf-3"));
    // The typed password does not linger in the mutation cache.
    await waitFor(() =>
      expect(
        JSON.stringify(
          queryClient
            .getMutationCache()
            .getAll()
            .map((mutation) => mutation.state.variables),
        ),
      ).not.toContain("passphrase"),
    );
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("re-authenticates with an emailed code: Retry-After, format and wrong-code handling", async () => {
    const { posts } = installMock({
      reauth: [() => problemResponse(401, "auth_invalid_verification_code")],
      security: [
        () => jsonResponse({ ...security, reauth_methods: ["email_code"] }),
      ],
      start: [() => problemResponse(429, undefined, 2)],
    });
    const { action, queryClient } = renderHarness();
    const { dialog } = await openDialog();

    const code = await within(dialog).findByLabelText("Verification code");
    // Only one method: no switcher.
    expect(
      within(dialog).queryByRole("group", { name: "Confirmation method" }),
    ).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(code).toHaveAccessibleDescription(/Send a code before confirming\./);
    expect(posts).toHaveLength(0);

    fireEvent.click(within(dialog).getByRole("button", { name: "Send code" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 2 seconds.",
    );
    expect(
      within(dialog).getByRole("button", { name: "Send code (2s)" }),
    ).toBeDisabled();
    const send = await within(dialog).findByRole(
      "button",
      { name: "Send code" },
      { timeout: 4000 },
    );
    expect(send).toBeEnabled();

    fireEvent.click(send);
    expect(
      await within(dialog).findByText(
        "We sent a code to your email. It's valid for 10 minutes; you can resend it after 60 seconds.",
      ),
    ).toHaveAttribute("aria-live", "polite");
    expect(
      within(dialog).getByRole("button", { name: "Resend code (60s)" }),
    ).toBeDisabled();
    await waitFor(() => expect(code).toHaveFocus());
    const start = posts[1];
    expect(start?.headers.get("X-CSRF-Token")).toMatch(/^step-up-csrf-/);
    // The client never names a destination.
    await expect(start?.json()).resolves.toEqual({});

    fireEvent.change(code, { target: { value: "12ab" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(code).toHaveAccessibleDescription(/Enter the 6-digit code\./);
    expect(posts).toHaveLength(2);

    fireEvent.change(code, { target: { value: "000000" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() =>
      expect(code).toHaveAccessibleDescription(
        /The code is incorrect or expired\. Check it or send a new one\./,
      ),
    );
    expect(code).toHaveValue("");
    expect(queryClient.getQueryData(privateKey)).toEqual({
      name: "Private workspace",
    });

    fireEvent.change(code, { target: { value: " 123456 " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    await expect(posts[3]?.json()).resolves.toEqual({
      challenge_id: challengeID,
      code: "123456",
      method: "email_code",
    });
  });

  it("maps a context conflict and Retry-After without closing the dialog", async () => {
    installMock({
      reauth: [
        () => problemResponse(409),
        () => problemResponse(429, undefined, 30),
      ],
    });
    const { action } = renderHarness();
    const { dialog } = await openDialog();
    const password = await within(dialog).findByLabelText("Current password");

    fireEvent.change(password, { target: { value: "Some-passphrase" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    const conflict = await within(dialog).findByRole("alert");
    expect(conflict).toHaveTextContent(
      "Your session just changed (for example in another tab). Refresh the session and try again.",
    );
    fireEvent.click(
      within(conflict).getByRole("button", { name: "Refresh session" }),
    );
    await waitFor(() =>
      expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument(),
    );
    expect(sessionActions.refresh).toHaveBeenCalledTimes(1);

    fireEvent.change(password, { target: { value: "Some-passphrase" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 30 seconds.",
    );
    expect(
      within(dialog).getByRole("button", { name: /^Confirm \(\d+s\)$/ }),
    ).toBeDisabled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(action).not.toHaveBeenCalled();
  });

  it("treats a 401 without a code as an expired session", async () => {
    installMock({ reauth: [() => problemResponse(401)] });
    const { action, queryClient } = renderHarness();
    const { dialog } = await openDialog();
    const password = await within(dialog).findByLabelText("Current password");

    fireEvent.change(password, { target: { value: "Some-passphrase" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Your session has expired. Please sign in again.",
    );
    await waitFor(() =>
      expect(queryClient.getQueryData(privateKey)).toBeUndefined(),
    );
    expect(action).not.toHaveBeenCalled();
  });

  it("loads the options with a retry after an error", async () => {
    installMock({ security: [() => problemResponse(503)] });
    renderHarness();
    const { dialog } = await openDialog();

    expect(
      await within(dialog).findByRole("heading", {
        name: "Confirmation options unavailable",
      }),
    ).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Try again" }));
    expect(
      await within(dialog).findByLabelText("Current password"),
    ).toBeInTheDocument();
  });

  it("cancels without running the blocked action and returns focus", async () => {
    installMock();
    const { action } = renderHarness();
    const { dialog, trigger } = await openDialog();
    await within(dialog).findByLabelText("Current password");

    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(action).not.toHaveBeenCalled();
  });

  it("keeps the sign-in-again fallback when device management is missing", async () => {
    const legacyProviders: AuthProvidersResponse = { ...providers };
    delete (legacyProviders as { device_management_enabled?: boolean })
      .device_management_enabled;
    const { calls } = installMock({ providers: legacyProviders });
    const view = renderHarness();
    let { dialog } = await openDialog();

    expect(
      await within(dialog).findByText(
        "To protect your account, sign in again and then repeat this action within 10 minutes.",
      ),
    ).toBeInTheDocument();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Sign in again" }),
    );
    await waitFor(() =>
      expect(sessionActions.signOut).toHaveBeenCalledTimes(1),
    );
    expect(sessionActions.signIn).not.toHaveBeenCalled();
    // A missing flag never reaches the device-management endpoints.
    expect(calls).not.toContain("GET /api/v1/me/security");
    expect(view.action).not.toHaveBeenCalled();
    cleanup();

    installMock({
      providers: { ...legacyProviders, oidc_default_enabled: true },
    });
    renderHarness();
    ({ dialog } = await openDialog());
    fireEvent.click(
      await within(dialog).findByRole("button", { name: "Sign in again" }),
    );
    expect(sessionActions.signIn).toHaveBeenCalledWith("/app/settings");
  });

  it("offers the sign-in portal or sign-out when no in-place method exists", async () => {
    const noMethods = () => jsonResponse({ ...security, reauth_methods: [] });
    installMock({
      providers: { ...providers, oidc_default_enabled: true },
      security: [noMethods],
    });
    renderHarness();
    let { dialog } = await openDialog();
    fireEvent.click(
      await within(dialog).findByRole("button", {
        name: "Continue with the sign-in portal",
      }),
    );
    expect(sessionActions.signIn).toHaveBeenCalledWith("/app/settings");
    cleanup();

    installMock({ security: [noMethods] });
    renderHarness();
    ({ dialog } = await openDialog());
    expect(
      await within(dialog).findByText(
        /No confirmation method is available for this account\./,
      ),
    ).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign out" }));
    await waitFor(() =>
      expect(sessionActions.signOut).toHaveBeenCalledTimes(1),
    );
  });

  it("offers the passkey first and resumes the blocked action once after a passkey step-up", async () => {
    const get = installWebAuthn([]);
    const { calls, posts } = installMock({
      security: [
        () =>
          jsonResponse({
            ...security,
            reauth_methods: ["password", "email_code", "passkey"],
          }),
      ],
    });
    const { action } = renderHarness();
    const { dialog, trigger } = await openDialog();

    const methods = await within(dialog).findByRole("group", {
      name: "Confirmation method",
    });
    const options = within(methods).getAllByRole("button");
    expect(options.map((option) => option.textContent)).toEqual([
      "Passkey",
      "Password",
      "Email code",
    ]);
    expect(options[0]).toHaveAttribute("aria-pressed", "true");
    expect(
      within(dialog).getByText(
        "Use your device's screen lock, fingerprint, face or security key to confirm.",
      ),
    ).toBeInTheDocument();
    const confirm = within(dialog).getByRole("button", {
      name: "Confirm with passkey",
    });
    await waitFor(() => expect(confirm).toHaveFocus());

    fireEvent.click(confirm);

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(get).toHaveBeenCalledTimes(1);
    expect(get.mock.calls[0]?.[0]).toMatchObject({ mediation: "optional" });
    const optionsCall = calls.indexOf(
      "POST /api/v1/auth/reauth/passkey/options",
    );
    expect(calls[optionsCall - 1]).toBe("GET /api/v1/auth/csrf");
    expect(posts[0]?.headers.get("X-CSRF-Token")).toMatch(/^step-up-csrf-/);
    await expect(posts[1]?.json()).resolves.toEqual({
      assertion: serializedAssertion,
      method: "passkey",
    });
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("keeps the session on a rejected passkey, treats a closed prompt as a quiet cancel and switches to an email code", async () => {
    installWebAuthn([
      () => Promise.reject(new DOMException("closed", "NotAllowedError")),
    ]);
    const { posts } = installMock({
      reauth: [() => problemResponse(401, "auth_invalid_passkey")],
      security: [
        () =>
          jsonResponse({
            ...security,
            reauth_methods: ["passkey", "password", "email_code"],
          }),
      ],
    });
    const { action, queryClient } = renderHarness();
    const { dialog } = await openDialog();
    const confirm = await within(dialog).findByRole("button", {
      name: "Confirm with passkey",
    });

    fireEvent.click(confirm);
    expect(
      await within(dialog).findByText(
        "Passkey verification was cancelled. Try again or choose another method.",
      ),
    ).toBeInTheDocument();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm with passkey" }),
    );
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "We couldn't verify this passkey. Try again or choose another method.",
    );
    // A rejected assertion never ends the session.
    expect(queryClient.getQueryData(privateKey)).toEqual({
      name: "Private workspace",
    });
    expect(action).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Email code" }));
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Send code" }));
    const codeField = await within(dialog).findByLabelText("Verification code");
    fireEvent.change(codeField, { target: { value: "123456" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
    const bodies = await Promise.all(
      posts.map(async (request) => ({
        path: new URL(request.url).pathname,
        body: await request.text(),
      })),
    );
    expect(bodies.map((entry) => entry.path)).toEqual([
      "/api/v1/auth/reauth/passkey/options",
      "/api/v1/auth/reauth/passkey/options",
      "/api/v1/auth/reauth",
      "/api/v1/auth/reauth/email-code/start",
      "/api/v1/auth/reauth",
    ]);
    expect(JSON.parse(bodies[4]?.body ?? "{}")).toEqual({
      challenge_id: challengeID,
      code: "123456",
      method: "email_code",
    });
  });

  it("explains a passkey-only (AAL2) session and maps a clone warning", async () => {
    installWebAuthn([]);
    installMock({
      reauth: [() => problemResponse(403, "auth_passkey_clone_detected")],
      security: [
        () => jsonResponse({ ...security, reauth_methods: ["passkey"] }),
      ],
    });
    const { action } = renderHarness();
    const { dialog } = await openDialog();

    expect(
      await within(dialog).findByText(
        /This session can only be confirmed with a passkey\./,
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByRole("group", { name: "Confirmation method" }),
    ).not.toBeInTheDocument();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm with passkey" }),
    );
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      /may have been copied/,
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Confirm with passkey" }),
    );
    await waitFor(() => expect(action).toHaveBeenCalledTimes(1));
  });

  it("falls back when only a passkey works but this browser lacks WebAuthn", async () => {
    installMock({
      security: [
        () => jsonResponse({ ...security, reauth_methods: ["passkey"] }),
      ],
    });
    renderHarness();
    const { dialog } = await openDialog();

    expect(
      await within(dialog).findByText(
        /this browser doesn't support passkeys\. Sign out and sign in again/,
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Sign out" }),
    ).toBeInTheDocument();
  });
});
