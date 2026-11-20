import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import {
  APIRequestError,
  type AuthProvidersResponse,
  type PasskeyRequestOptions,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { SignInPage } from "./SignInPage";

const { api, completeNativeSignInMock, resetAfterPasswordRecoveryMock } =
  vi.hoisted(() => ({
    api: {
      completePasswordRecovery: vi.fn(),
      getAuthProviders: vi.fn(),
      getPendingMFA: vi.fn(),
      loginWithPassword: vi.fn(),
      startEmailCodeLogin: vi.fn(),
      startMFAPasskey: vi.fn(),
      startPasskeyAssertion: vi.fn(),
      startPasswordRecovery: vi.fn(),
      verifyEmailCodeLogin: vi.fn(),
      verifyMFA: vi.fn(),
      verifyPasskeyAssertion: vi.fn(),
    },
    completeNativeSignInMock: vi.fn(),
    resetAfterPasswordRecoveryMock: vi.fn(),
  }));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", () => ({
  useSession: () => ({
    completeNativeSignIn: completeNativeSignInMock,
    resetAfterPasswordRecovery: resetAfterPasswordRecoveryMock,
    signIn: vi.fn(),
    status: "unauthenticated",
  }),
}));

const PROVIDERS: AuthProvidersResponse = {
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

const OPTIONS: PasskeyRequestOptions = {
  allowCredentials: [],
  challenge: "Y2hhbGxlbmdl",
  rpId: "localhost",
  timeout: 300000,
  userVerification: "preferred",
};

function buffer(...values: number[]) {
  return new Uint8Array(values).buffer;
}

const CREDENTIAL = {
  authenticatorAttachment: "platform",
  id: "AQID",
  rawId: buffer(1, 2, 3),
  response: {
    authenticatorData: buffer(4),
    clientDataJSON: buffer(5),
    signature: buffer(6),
    userHandle: buffer(7),
  },
  type: "public-key",
};

function apiError(status: number, code?: string, retryAfterSeconds?: number) {
  return new APIRequestError(
    status,
    { status, title: "Rejected", type: "about:blank", code },
    retryAfterSeconds,
  );
}

function LandingProbe() {
  const location = useLocation();
  return (
    <p data-testid="landing">{`${location.pathname}${location.search}`}</p>
  );
}

function SearchProbe() {
  const location = useLocation();
  return <p data-testid="search">{location.search}</p>;
}

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) {
      await Promise.resolve();
    }
  });
}

async function renderSignIn({
  from,
  language = "en",
  search = "",
}: {
  from?: { pathname: string };
  language?: Language;
  search?: string;
} = {}) {
  const router = createMemoryRouter(
    [
      {
        path: "/sign-in",
        element: (
          <>
            <SignInPage />
            <SearchProbe />
          </>
        ),
      },
      { path: "*", element: <LandingProbe /> },
    ],
    {
      initialEntries: [
        { pathname: "/sign-in", search, state: from ? { from } : null },
      ],
    },
  );
  render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
}

async function signInWithPassword() {
  fireEvent.change(screen.getByRole("textbox", { name: "Email or username" }), {
    target: { value: "teacher@example.com" },
  });
  fireEvent.change(screen.getByLabelText("Password"), {
    target: { value: "Correct-horse-battery" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Log In" }));
  await settle();
}

function mfaStep(name = "Two-step verification") {
  return screen.getByRole("region", { name });
}

/** A WebAuthn browser without autofill; modal prompts follow `outcomes`. */
function installWebAuthn(outcomes: Array<() => Promise<unknown>> = []) {
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {
      isConditionalMediationAvailable: () => Promise.resolve(false),
    }),
  );
  const get = vi.fn(() =>
    (outcomes.shift() ?? (() => Promise.resolve(CREDENTIAL)))(),
  );
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get },
  });
  return get;
}

describe("SignInPage AUTH-09B two-step verification", () => {
  beforeEach(() => {
    window.localStorage.clear();
    for (const mock of Object.values(api)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    completeNativeSignInMock.mockReset().mockResolvedValue(undefined);
    resetAfterPasswordRecoveryMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("finishes a password sign-in with the authenticator app after a wrong code", async () => {
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA
      .mockRejectedValueOnce(apiError(401, "auth_invalid_verification_code"))
      .mockResolvedValueOnce({ status: "authenticated" });
    await renderSignIn({ from: { pathname: "/app/classrooms" } });
    fireEvent.click(screen.getByRole("checkbox", { name: "Remember Me" }));
    await signInWithPassword();

    const step = mfaStep();
    expect(
      within(step).getByRole("heading", { name: "Two-step verification" }),
    ).toHaveFocus();
    expect(within(step).getByText(/expires in 5:00/)).toBeInTheDocument();
    const methods = within(step).getByRole("group", {
      name: "Verification method",
    });
    expect(
      within(methods)
        .getAllByRole("button")
        .map((button) => button.textContent),
    ).toEqual(["Authenticator app", "Recovery code"]);
    const code = within(step).getByLabelText(
      "6-digit code from your authenticator app",
    );
    expect(code).toHaveAttribute("autocomplete", "one-time-code");
    expect(code).toHaveAttribute("inputmode", "numeric");
    expect(code).toHaveAttribute("maxlength", "6");
    // Not signed in yet, and nothing remembered before a session exists.
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    expect(window.localStorage.length).toBe(0);

    fireEvent.change(code, { target: { value: "111111" } });
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(code).toHaveValue("");
    expect(code).toHaveFocus();
    expect(code).toHaveAccessibleDescription(/That code is wrong/);

    fireEvent.change(code, { target: { value: "222222" } });
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(api.verifyMFA).toHaveBeenLastCalledWith(
      { code: "222222", method: "totp" },
      { baseUrl: "/api" },
    );
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/classrooms");
    expect(window.localStorage.getItem("tutorhub.remembered_email")).toBe(
      "teacher@example.com",
    );
  });

  it("accepts a recovery code in any case and validates its shape first", async () => {
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockResolvedValue({ status: "authenticated" });
    await renderSignIn();
    await signInWithPassword();
    const step = mfaStep();
    fireEvent.click(
      within(step).getByRole("button", { name: "Recovery code" }),
    );
    const field = within(step).getByRole("textbox", {
      name: "Enter a recovery code",
    });
    expect(field).toHaveFocus();
    expect(field).toHaveAttribute("autocomplete", "off");
    expect(field).toHaveAttribute("spellcheck", "false");

    fireEvent.change(field, { target: { value: "abc" } });
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    expect(field).toHaveAccessibleDescription(/16 letters and digits/);
    expect(api.verifyMFA).not.toHaveBeenCalled();

    fireEvent.change(field, { target: { value: "abcd-efgh-jkmn-pqro" } });
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(api.verifyMFA).toHaveBeenCalledWith(
      { code: "ABCDEFGHJKMNPQR0", method: "recovery_code" },
      { baseUrl: "/api" },
    );
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
  });

  it("moves an email-code sign-in to the second step", async () => {
    api.startEmailCodeLogin.mockResolvedValue({
      challenge_id: "login-1",
      cooldown_seconds: 60,
      expires_in_seconds: 600,
    });
    api.verifyEmailCodeLogin.mockResolvedValue({
      methods: ["totp"],
      status: "mfa_required",
    });
    await renderSignIn();
    fireEvent.click(
      screen.getByRole("button", { name: "Log in with an email code" }),
    );
    const form = screen.getByRole("form", {
      name: "Log in with a code sent to your email",
    });
    fireEvent.change(
      within(form).getByRole("textbox", { name: "Email Address" }),
      {
        target: { value: "teacher@example.com" },
      },
    );
    fireEvent.click(within(form).getByRole("button", { name: "Get Code" }));
    await settle();
    fireEvent.change(within(form).getByRole("textbox", { name: "OTP code" }), {
      target: { value: "123456" },
    });
    fireEvent.click(within(form).getByRole("button", { name: "Log In" }));
    await settle();

    expect(
      within(mfaStep()).getByLabelText(
        "6-digit code from your authenticator app",
      ),
    ).toBeInTheDocument();
    // A single method needs no switcher.
    expect(
      screen.queryByRole("group", { name: "Verification method" }),
    ).toBeNull();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("asks a passkey without user verification for a second step, then verifies with a passkey", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      passkeys_enabled: true,
    });
    const get = installWebAuthn([
      () => Promise.resolve(CREDENTIAL),
      () => Promise.reject(new DOMException("closed", "NotAllowedError")),
      () => Promise.resolve(CREDENTIAL),
    ]);
    api.startPasskeyAssertion.mockResolvedValue(OPTIONS);
    api.verifyPasskeyAssertion.mockResolvedValue({
      methods: ["passkey", "recovery_code"],
      status: "mfa_required",
    });
    api.startMFAPasskey.mockResolvedValue({
      ...OPTIONS,
      userVerification: "required",
    });
    api.verifyMFA.mockResolvedValue({ status: "authenticated" });
    await renderSignIn();

    fireEvent.click(
      screen.getByRole("button", { name: "Sign in with a passkey" }),
    );
    await settle();
    const step = mfaStep();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    const verify = within(step).getByRole("button", {
      name: "Verify with a passkey",
    });

    // Closing the device sheet is a quiet notice, never an error banner.
    fireEvent.click(verify);
    await settle();
    expect(
      within(step).getByText(/Passkey verification was cancelled/),
    ).toBeInTheDocument();
    expect(within(step).queryByRole("alert")).toBeNull();

    fireEvent.click(verify);
    await settle();
    expect(get).toHaveBeenCalledTimes(3);
    expect(api.verifyMFA).toHaveBeenCalledWith(
      expect.objectContaining({ method: "passkey" }),
      { baseUrl: "/api" },
    );
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
  });

  it("resumes an OAuth second step from ?mfa=pending and keeps its return path", async () => {
    api.getPendingMFA.mockResolvedValue({
      expires_in_seconds: 125,
      methods: ["totp"],
    });
    api.verifyMFA.mockResolvedValue({
      return_to: "/app/calendar?view=week",
      status: "authenticated",
    });
    await renderSignIn({ search: "?mfa=pending" });

    const step = mfaStep();
    expect(within(step).getByText(/expires in 2:05/)).toBeInTheDocument();
    // The transient flag leaves the address bar.
    expect(screen.getByTestId("search")).toHaveTextContent("");
    fireEvent.change(
      within(step).getByLabelText("6-digit code from your authenticator app"),
      { target: { value: "123456" } },
    );
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/app/calendar?view=week",
    );
  });

  it("explains an expired OAuth second step (404) and shows the sign-in form", async () => {
    api.getPendingMFA.mockRejectedValue(apiError(404, "auth_mfa_unavailable"));
    await renderSignIn({ search: "?mfa=pending" });

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Your verification session expired. Please sign in again.",
    );
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("search")).toHaveTextContent("");
  });

  it("locks the verify button while Retry-After lasts (429)", async () => {
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp"],
      status: "mfa_required",
    });
    api.verifyMFA.mockRejectedValueOnce(apiError(429, undefined, 30));
    await renderSignIn();
    await signInWithPassword();
    const step = mfaStep();
    fireEvent.change(
      within(step).getByLabelText("6-digit code from your authenticator app"),
      { target: { value: "123456" } },
    );
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(within(step).getByRole("alert")).toHaveTextContent(
      "Too many attempts. Try again in 30 seconds.",
    );
    expect(
      within(step).getByRole("button", { name: "Verify (30s)" }),
    ).toBeDisabled();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("ends the step when the transaction is gone (404 auth_mfa_unavailable)", async () => {
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp"],
      status: "mfa_required",
    });
    api.verifyMFA.mockRejectedValueOnce(apiError(404, "auth_mfa_unavailable"));
    await renderSignIn();
    await signInWithPassword();
    const step = mfaStep();
    fireEvent.change(
      within(step).getByLabelText("6-digit code from your authenticator app"),
      { target: { value: "654321" } },
    );
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Your verification session expired. Please sign in again.",
    );
    expect(
      screen.queryByRole("region", { name: "Two-step verification" }),
    ).toBeNull();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("returns to the first factor once the verification clock runs out", async () => {
    vi.useFakeTimers();
    api.getPendingMFA.mockResolvedValue({
      expires_in_seconds: 3,
      methods: ["totp"],
    });
    await renderSignIn({ search: "?mfa=pending" });
    expect(within(mfaStep()).getByText(/expires in 0:03/)).toBeInTheDocument();
    for (let second = 0; second < 4; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Your verification session expired. Please sign in again.",
    );
    expect(api.verifyMFA).not.toHaveBeenCalled();
  });

  it("goes back to the first factor on request", async () => {
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp"],
      status: "mfa_required",
    });
    await renderSignIn();
    await signInWithPassword();
    fireEvent.click(
      within(mfaStep()).getByRole("button", { name: "Back to sign in" }),
    );
    await settle();
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", { name: "Email or username" }),
      ).toHaveFocus(),
    );
    expect(screen.getByLabelText("Password")).toHaveValue("");
  });

  it("requires the second factor during password recovery and never signs in after password_reset", async () => {
    api.startPasswordRecovery.mockResolvedValue({
      challenge_id: "recovery-1",
      cooldown_seconds: 60,
      expires_in_seconds: 600,
    });
    api.completePasswordRecovery.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockResolvedValue({ status: "password_reset" });
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Forget Password" }));
    const dialog = screen.getByRole("dialog", { name: "Recover Password" });
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Email Address" }),
      {
        target: { value: "teacher@example.com" },
      },
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Get Code" }));
    await settle();
    fireEvent.change(
      within(dialog).getByRole("textbox", {
        name: "Authentication Code (OTP)",
      }),
      { target: { value: "123456" } },
    );
    fireEvent.change(within(dialog).getByLabelText("New Password"), {
      target: { value: "N3w-recovered-passphrase" },
    });
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Reset Password" }),
    );
    await settle();

    const mfaDialog = screen.getByRole("dialog", { name: "Verify it's you" });
    expect(resetAfterPasswordRecoveryMock).not.toHaveBeenCalled();
    fireEvent.change(
      within(mfaDialog).getByLabelText(
        "6-digit code from your authenticator app",
      ),
      { target: { value: "123456" } },
    );
    fireEvent.click(within(mfaDialog).getByRole("button", { name: "Verify" }));
    await settle();

    expect(resetAfterPasswordRecoveryMock).toHaveBeenCalledTimes(1);
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(
      screen.getByText(/Your password has been reset and all your sessions/),
    ).toHaveAttribute("role", "status");
    expect(screen.queryByTestId("landing")).toBeNull();
    expect(JSON.stringify({ ...window.localStorage })).not.toContain(
      "N3w-recovered-passphrase",
    );
    expect(window.sessionStorage.length).toBe(0);
  });

  it("localizes the second step in Vietnamese", async () => {
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    await renderSignIn({ language: "vi" });
    fireEvent.change(
      screen.getByRole("textbox", { name: "Email hoặc tên đăng nhập" }),
      { target: { value: "teacher@example.com" } },
    );
    fireEvent.change(screen.getByLabelText("Mật khẩu"), {
      target: { value: "Correct-horse-battery" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Đăng nhập" }));
    await settle();
    const step = mfaStep("Xác minh hai bước");
    expect(
      within(step).getByRole("button", { name: "Ứng dụng xác thực" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(
      within(step).getByRole("button", { name: "Quay lại đăng nhập" }),
    ).toBeInTheDocument();
  });
});
