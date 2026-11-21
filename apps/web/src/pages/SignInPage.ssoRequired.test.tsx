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
  type AuthIdentifierDiscoverResponse,
  type AuthProvidersResponse,
  type PasskeyRequestOptions,
  type PhoneChallengeResponse,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { SignInPage } from "./SignInPage";

/**
 * AUTH-13B: 403 `auth_sso_required` on the /sign-in surfaces AUTH-12B left
 * out — a passkey (button and autofill), a phone code and the second step
 * (sign-in and recovery). The server consumed the transaction each time, so
 * the page returns to the identifier field where school SSO starts, with a
 * neutral notice that never names an address it was not given.
 */

const { api, completeNativeSignInMock, resetAfterPasswordRecoveryMock } =
  vi.hoisted(() => ({
    api: {
      completePasswordRecovery: vi.fn(),
      discoverAuthIdentifier: vi.fn(),
      getAuthProviders: vi.fn(),
      getPendingMFA: vi.fn(),
      loginWithPassword: vi.fn(),
      resendAuthChallenge: vi.fn(),
      startEmailCodeLogin: vi.fn(),
      startMFAPasskey: vi.fn(),
      startPasskeyAssertion: vi.fn(),
      startPasswordRecovery: vi.fn(),
      startPhoneLogin: vi.fn(),
      verifyMFA: vi.fn(),
      verifyPasskeyAssertion: vi.fn(),
      verifyPhoneLogin: vi.fn(),
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

const SCHOOL_EMAIL = "teacher@school.example";
const PASSWORD = "Harbor-Lantern-77";
const PHONE = "0912 345 678";
const SMS_CODE = "246810";
const TOTP_CODE = "135791";

const PROVIDERS: AuthProvidersResponse = {
  device_management_enabled: true,
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: true,
  oauth_providers: ["google"],
  oidc_default_enabled: false,
  passkeys_enabled: true,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: true,
  phone_allowed_countries: ["VN"],
  phone_enabled: true,
  signup_enabled: true,
  sso_domains_enabled: true,
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

const COPY = {
  en: {
    account:
      "This account belongs to a school that requires school SSO. Enter your school email, then choose “Sign in with school SSO”.",
    email: "This email address must sign in through your school's SSO.",
    identifier: "Email or username",
    password: "Password",
    passkey: "Sign in with a passkey",
    phoneMethod: "Log in with a phone number",
    passwordMethod: "Log in with a password",
    phoneForm: "Log in with a code sent to your phone",
    phoneNumber: "Phone number",
    sendCode: "Send code",
    smsCode: "6-digit code",
    verifyPhone: "Verify and log in",
    schoolSSO: "Sign in with school SSO",
    continueGoogle: "Continue with school Google",
    mfaStep: "Two-step verification",
    totp: "6-digit code from your authenticator app",
    verify: "Verify",
    mfaPasskey: "Verify with a passkey",
  },
  vi: {
    account:
      "Tài khoản này thuộc trường học bắt buộc đăng nhập qua SSO trường học. Hãy nhập email trường của bạn rồi chọn “Đăng nhập bằng SSO trường học”.",
    email: "Email này phải đăng nhập qua SSO của trường.",
    identifier: "Email hoặc tên đăng nhập",
    password: "Mật khẩu",
    passkey: "Đăng nhập bằng Passkey",
    phoneMethod: "Đăng nhập bằng số điện thoại",
    passwordMethod: "Đăng nhập bằng mật khẩu",
    phoneForm: "Đăng nhập bằng mã gửi tới số điện thoại",
    phoneNumber: "Số điện thoại",
    sendCode: "Gửi mã",
    smsCode: "Mã gồm 6 chữ số",
    verifyPhone: "Xác minh và đăng nhập",
    schoolSSO: "Đăng nhập bằng SSO trường học",
    continueGoogle: "Tiếp tục với Google của trường",
    mfaStep: "Xác minh hai bước",
    totp: "Mã 6 chữ số từ ứng dụng xác thực",
    verify: "Xác minh",
    mfaPasskey: "Xác minh bằng passkey",
  },
} as const;

function ssoRequired() {
  return new APIRequestError(403, {
    code: "auth_sso_required",
    detail: "Sign in with your school's identity provider.",
    status: 403,
    title: "School SSO required",
    type: "about:blank",
  });
}

function route(): AuthIdentifierDiscoverResponse {
  return {
    kind: "email",
    sso: {
      available: true,
      domain: "school.example",
      provider: "google",
      required: true,
    },
  };
}

function phoneChallenge(): PhoneChallengeResponse {
  return {
    challenge_id: "phone-challenge-1",
    cooldown_seconds: 60,
    expires_in_seconds: 300,
    masked_destination: "+84 ***5678",
    resend_after_seconds: 60,
  };
}

type GetRequest = CredentialRequestOptions & { mediation?: string };

/** A WebAuthn browser: autofill stays pending until `resolveConditional()`. */
function installWebAuthn({ conditional }: { conditional: boolean }) {
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {
      isConditionalMediationAvailable: () => Promise.resolve(conditional),
      signalUnknownCredential: vi.fn(() => Promise.resolve()),
    }),
  );
  const conditionalCalls: GetRequest[] = [];
  let resolvePending: ((value: unknown) => void) | null = null;
  const get = vi.fn((request: GetRequest) => {
    if (request.mediation === "conditional") {
      conditionalCalls.push(request);
      return new Promise((resolve, reject) => {
        resolvePending = resolve;
        request.signal?.addEventListener("abort", () =>
          reject(new DOMException("aborted", "AbortError")),
        );
      });
    }
    return Promise.resolve(CREDENTIAL);
  });
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get },
  });
  return {
    conditionalCalls,
    get,
    resolveConditional: () => resolvePending?.(CREDENTIAL),
  };
}

function LandingProbe() {
  const location = useLocation();
  return (
    <p data-testid="landing">{`${location.pathname}${location.search}`}</p>
  );
}

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) {
      await Promise.resolve();
    }
  });
}

async function renderSignIn(language: Language = "en", search = "") {
  const router = createMemoryRouter(
    [
      { path: "/sign-in", element: <SignInPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: [{ pathname: "/sign-in", search }] },
  );
  render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return router;
}

function storedText() {
  return JSON.stringify({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  });
}

async function openPhoneAndSubmitCode(language: Language) {
  const copy = COPY[language];
  fireEvent.click(screen.getByRole("button", { name: copy.phoneMethod }));
  await settle();
  const form = screen.getByRole("form", { name: copy.phoneForm });
  fireEvent.change(
    within(form).getByRole("textbox", { name: copy.phoneNumber }),
    { target: { value: PHONE } },
  );
  fireEvent.click(within(form).getByRole("button", { name: copy.sendCode }));
  await settle();
  fireEvent.change(within(form).getByRole("textbox", { name: copy.smsCode }), {
    target: { value: SMS_CODE },
  });
  fireEvent.click(within(form).getByRole("button", { name: copy.verifyPhone }));
  await settle();
}

describe("SignInPage AUTH-13B auth_sso_required", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    for (const mock of Object.values(api)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    api.startPasskeyAssertion.mockResolvedValue(OPTIONS);
    api.discoverAuthIdentifier.mockResolvedValue(route());
    completeNativeSignInMock.mockReset().mockResolvedValue(undefined);
    resetAfterPasswordRecoveryMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("passkey button: explains the school SSO rule and opens the identifier from another method", async () => {
    const copy = COPY.en;
    installWebAuthn({ conditional: false });
    api.verifyPasskeyAssertion.mockRejectedValue(ssoRequired());
    await renderSignIn();
    // Start from the phone form: the identifier is not on screen.
    fireEvent.click(screen.getByRole("button", { name: copy.phoneMethod }));
    await settle();
    expect(
      screen.queryByRole("textbox", { name: copy.identifier }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: copy.passkey }));
    await settle();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(copy.account);
    expect(alert).toHaveClass("sign-in-form__alert--info");
    // Back on the identifier with the school SSO entry ready.
    const identifier = screen.getByRole("textbox", { name: copy.identifier });
    await waitFor(() => expect(identifier).toHaveFocus());
    expect(
      screen.getByRole("button", { name: copy.schoolSSO }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("form", { name: copy.phoneForm }),
    ).not.toBeInTheDocument();
    // No session, no navigation, nothing stored.
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    expect(screen.queryByTestId("landing")).not.toBeInTheDocument();
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("Conditional UI: a picked passkey refused for school SSO is never re-armed (vi)", async () => {
    const copy = COPY.vi;
    const webAuthn = installWebAuthn({ conditional: true });
    api.verifyPasskeyAssertion.mockRejectedValue(ssoRequired());
    await renderSignIn("vi");
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(1));

    webAuthn.resolveConditional();
    await settle();

    expect(await screen.findByRole("alert")).toHaveTextContent(copy.account);
    expect(api.verifyPasskeyAssertion).toHaveBeenCalledTimes(1);
    expect(completeNativeSignInMock).not.toHaveBeenCalled();

    // Leaving and re-opening the password form would normally re-arm autofill.
    fireEvent.click(screen.getByRole("button", { name: copy.phoneMethod }));
    await settle();
    fireEvent.click(screen.getByRole("button", { name: copy.passwordMethod }));
    await settle();
    expect(webAuthn.conditionalCalls).toHaveLength(1);
    expect(api.startPasskeyAssertion).toHaveBeenCalledTimes(1);
    expect(storedText()).toBe(JSON.stringify({ local: {}, session: {} }));
  });

  it.each(["en", "vi"] as const)(
    "phone code (%s): leaves the code step, drops the code and number, then routes to school SSO",
    async (language) => {
      const copy = COPY[language];
      api.startPhoneLogin.mockResolvedValue(phoneChallenge());
      api.verifyPhoneLogin.mockRejectedValue(ssoRequired());
      await renderSignIn(language);
      await openPhoneAndSubmitCode(language);

      // phone/start answered 202 as usual; the refusal came with the code.
      expect(api.startPhoneLogin).toHaveBeenCalledTimes(1);
      expect(api.verifyPhoneLogin).toHaveBeenCalledWith(
        expect.objectContaining({
          challenge_id: "phone-challenge-1",
          code: SMS_CODE,
        }),
        expect.anything(),
      );
      expect(
        screen.queryByRole("form", { name: copy.phoneForm }),
      ).not.toBeInTheDocument();
      expect(screen.getByRole("alert")).toHaveTextContent(copy.account);
      const identifier = screen.getByRole("textbox", {
        name: copy.identifier,
      });
      await waitFor(() => expect(identifier).toHaveFocus());
      expect(
        screen.getByRole("button", { name: copy.schoolSSO }),
      ).toBeInTheDocument();

      // Re-opening the phone form starts from an empty number step.
      fireEvent.click(screen.getByRole("button", { name: copy.phoneMethod }));
      await settle();
      const form = screen.getByRole("form", { name: copy.phoneForm });
      expect(
        within(form).getByRole("textbox", { name: copy.phoneNumber }),
      ).toHaveValue("");
      expect(
        within(form).queryByRole("textbox", { name: copy.smsCode }),
      ).not.toBeInTheDocument();
      expect(completeNativeSignInMock).not.toHaveBeenCalled();
      const stored = storedText();
      expect(stored).not.toContain(SMS_CODE);
      expect(stored).not.toContain("0912");
    },
  );

  it("second step after a typed school email: closes the step and runs Home Realm Discovery", async () => {
    const copy = COPY.en;
    api.loginWithPassword.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockRejectedValue(ssoRequired());
    await renderSignIn();
    fireEvent.change(screen.getByRole("textbox", { name: copy.identifier }), {
      target: { value: SCHOOL_EMAIL },
    });
    fireEvent.change(screen.getByLabelText(copy.password), {
      target: { value: PASSWORD },
    });
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));
    await settle();

    const step = screen.getByRole("region", { name: copy.mfaStep });
    fireEvent.change(within(step).getByLabelText(copy.totp), {
      target: { value: TOTP_CODE },
    });
    fireEvent.click(within(step).getByRole("button", { name: copy.verify }));
    await settle();

    expect(
      screen.queryByRole("region", { name: copy.mfaStep }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(copy.email);
    const identifier = screen.getByRole("textbox", { name: copy.identifier });
    expect(identifier).toHaveValue(SCHOOL_EMAIL);
    await waitFor(() => expect(identifier).toHaveFocus());
    expect(screen.getByLabelText(copy.password)).toHaveValue("");
    expect(api.discoverAuthIdentifier).toHaveBeenCalledWith(
      { identifier: SCHOOL_EMAIL },
      expect.anything(),
    );
    expect(
      await screen.findByRole("button", { name: copy.continueGoogle }),
    ).toBeInTheDocument();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    const stored = storedText();
    expect(stored).not.toContain(TOTP_CODE);
    expect(stored).not.toContain(PASSWORD);
  });

  it("second step resumed from ?mfa=pending (vi): a passkey refused for school SSO returns to the identifier", async () => {
    const copy = COPY.vi;
    installWebAuthn({ conditional: false });
    api.getPendingMFA.mockResolvedValue({
      expires_in_seconds: 240,
      methods: ["passkey"],
    });
    api.startMFAPasskey.mockRejectedValue(ssoRequired());
    await renderSignIn("vi", "?mfa=pending");

    const step = screen.getByRole("region", { name: copy.mfaStep });
    fireEvent.click(
      within(step).getByRole("button", { name: copy.mfaPasskey }),
    );
    await settle();

    expect(
      screen.queryByRole("region", { name: copy.mfaStep }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(copy.account);
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", { name: copy.identifier }),
      ).toHaveFocus(),
    );
    // No address was typed: nothing to discover, nothing named.
    expect(api.discoverAuthIdentifier).not.toHaveBeenCalled();
    expect(api.verifyMFA).not.toHaveBeenCalled();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("the closed step never reports an expiry afterwards", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const copy = COPY.en;
    api.getPendingMFA.mockResolvedValue({
      expires_in_seconds: 30,
      methods: ["totp", "recovery_code"],
    });
    api.verifyMFA.mockRejectedValue(ssoRequired());
    await renderSignIn("en", "?mfa=pending");
    const step = screen.getByRole("region", { name: copy.mfaStep });
    fireEvent.change(within(step).getByLabelText(copy.totp), {
      target: { value: TOTP_CODE },
    });
    fireEvent.click(within(step).getByRole("button", { name: copy.verify }));
    await settle();
    expect(screen.getByRole("alert")).toHaveTextContent(copy.account);

    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(
      screen.queryByText(
        "Your verification session expired. Please sign in again.",
      ),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(copy.account);
  });

  it("password recovery: a second step refused for school SSO resets nothing and shows the school route", async () => {
    api.startPasswordRecovery.mockResolvedValue({
      challenge_id: "recovery-1",
      cooldown_seconds: 60,
      expires_in_seconds: 600,
    });
    api.completePasswordRecovery.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockRejectedValue(ssoRequired());
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Forget Password" }));
    const dialog = screen.getByRole("dialog", { name: "Recover Password" });
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Email Address" }),
      { target: { value: SCHOOL_EMAIL } },
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
    fireEvent.change(
      within(mfaDialog).getByLabelText(
        "6-digit code from your authenticator app",
      ),
      { target: { value: TOTP_CODE } },
    );
    fireEvent.click(within(mfaDialog).getByRole("button", { name: "Verify" }));
    await settle();

    const recovery = screen.getByRole("dialog", { name: "Recover Password" });
    expect(within(recovery).getByRole("alert")).toHaveTextContent(
      COPY.en.email,
    );
    expect(
      await within(recovery).findByRole("button", {
        name: COPY.en.continueGoogle,
      }),
    ).toBeInTheDocument();
    expect(resetAfterPasswordRecoveryMock).not.toHaveBeenCalled();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    expect(storedText()).not.toContain("N3w-recovered-passphrase");
  });
});
