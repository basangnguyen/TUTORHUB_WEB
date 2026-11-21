import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import {
  APIRequestError,
  type AuthProvidersResponse,
  type PhoneChallengeResponse,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { SignInPage } from "./SignInPage";

const { api, completeNativeSignInMock } = vi.hoisted(() => ({
  api: {
    getAuthProviders: vi.fn(),
    loginWithPassword: vi.fn(),
    resendAuthChallenge: vi.fn(),
    startEmailCodeLogin: vi.fn(),
    startMFAPasskey: vi.fn(),
    startPasskeyAssertion: vi.fn(),
    startPhoneLogin: vi.fn(),
    verifyMFA: vi.fn(),
    verifyPhoneLogin: vi.fn(),
  },
  completeNativeSignInMock: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", () => ({
  useSession: () => ({
    completeNativeSignIn: completeNativeSignInMock,
    resetAfterPasswordRecovery: vi.fn(),
    signIn: vi.fn(),
    status: "unauthenticated",
  }),
}));

const REMEMBERED_EMAIL_KEY = "tutorhub.remembered_email";

const PROVIDERS: AuthProvidersResponse = {
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
  phone_enabled: true,
  signup_enabled: true,
  sso_domains_enabled: false,
};

function challenge(id: string): PhoneChallengeResponse {
  return {
    challenge_id: id,
    cooldown_seconds: 60,
    expires_in_seconds: 300,
    masked_destination: "+84 ***5678",
    resend_after_seconds: 60,
  };
}

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

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) {
      await Promise.resolve();
    }
  });
}

function tickSeconds(seconds: number) {
  for (let second = 0; second < seconds; second += 1) {
    act(() => {
      vi.advanceTimersByTime(1000);
    });
  }
}

async function renderSignIn(language: Language = "en") {
  const router = createMemoryRouter(
    [
      { path: "/sign-in", element: <SignInPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: ["/sign-in"] },
  );
  render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return router;
}

function phoneForm() {
  return screen.getByRole("form", {
    name: "Log in with a code sent to your phone",
  });
}

function otpChannels(language: Language = "en") {
  return screen.getByRole("group", {
    name: language === "vi" ? "Nhận mã OTP qua" : "Receive the OTP by",
  });
}

/** UI-AUTH-04: "Log in with OTP", then the Phone channel (both are on). */
async function openPhone(language: Language = "en") {
  await renderSignIn(language);
  fireEvent.click(
    screen.getByRole("button", {
      name: language === "vi" ? "Đăng nhập với OTP" : "Log in with OTP",
    }),
  );
  await settle();
  fireEvent.click(
    within(otpChannels(language)).getByRole("button", {
      name: language === "vi" ? "Số điện thoại" : "Phone",
    }),
  );
  await settle();
}

async function sendCode(value = "0912 345 678") {
  const form = phoneForm();
  fireEvent.change(
    within(form).getByRole("textbox", { name: "Phone number" }),
    {
      target: { value },
    },
  );
  fireEvent.click(within(form).getByRole("button", { name: "Send code" }));
  await settle();
}

async function submitCode(code = "123456") {
  const form = phoneForm();
  fireEvent.change(
    within(form).getByRole("textbox", { name: "6-digit code" }),
    {
      target: { value: code },
    },
  );
  fireEvent.click(
    within(form).getByRole("button", { name: "Verify and log in" }),
  );
  await settle();
}

function storedText() {
  return JSON.stringify({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  });
}

describe("SignInPage AUTH-10B phone sign-in", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    for (const mock of Object.values(api)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    completeNativeSignInMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("offers the phone channel only while phone_enabled lists a region", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      phone_enabled: false,
    });
    await renderSignIn();
    // Email code alone: "Log in with OTP" opens it, with no channel chooser.
    fireEvent.click(screen.getByRole("button", { name: "Log in with OTP" }));
    await settle();
    expect(
      screen.getByRole("form", {
        name: "Log in with a code sent to your email",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("group", { name: "Receive the OTP by" }),
    ).toBeNull();
    cleanup();

    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      phone_allowed_countries: [],
    });
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Log in with OTP" }));
    await settle();
    expect(
      screen.queryByRole("group", { name: "Receive the OTP by" }),
    ).toBeNull();
    cleanup();

    // Phone alone: "Log in with OTP" opens the phone form directly.
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      email_code_enabled: false,
    });
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Log in with OTP" }));
    await settle();
    expect(phoneForm()).toBeInTheDocument();
    expect(
      screen.queryByRole("group", { name: "Receive the OTP by" }),
    ).toBeNull();
    expect(
      screen.getByRole("button", { name: "Log in with password" }),
    ).toBeInTheDocument();
  });

  it("switches between the email and phone channels while both are on", async () => {
    await openPhone();
    const channels = otpChannels();
    const email = within(channels).getByRole("button", { name: "Email" });
    const phone = within(channels).getByRole("button", { name: "Phone" });
    expect(phone).toHaveAttribute("aria-pressed", "true");
    expect(email).toHaveAttribute("aria-pressed", "false");
    expect(
      within(phoneForm()).getByRole("textbox", { name: "Phone number" }),
    ).toHaveFocus();

    fireEvent.click(email);
    await settle();
    expect(
      within(otpChannels()).getByRole("button", { name: "Email" }),
    ).toHaveAttribute("aria-pressed", "true");
    const emailForm = screen.getByRole("form", {
      name: "Log in with a code sent to your email",
    });
    expect(
      within(emailForm).getByRole("textbox", { name: "Email Address" }),
    ).toHaveFocus();
    expect(
      screen.queryByRole("form", {
        name: "Log in with a code sent to your phone",
      }),
    ).toBeNull();
  });

  it("sends a code to the typed number, resends after 60 s and signs in with a persistent session", async () => {
    vi.useFakeTimers();
    api.startPhoneLogin.mockResolvedValue(challenge("phone-1"));
    api.resendAuthChallenge.mockResolvedValue(challenge("phone-2"));
    api.verifyPhoneLogin.mockResolvedValue({ status: "authenticated" });
    await openPhone();

    const form = phoneForm();
    const number = within(form).getByRole("textbox", { name: "Phone number" });
    expect(number).toHaveFocus();
    expect(number).toHaveAttribute("type", "tel");
    expect(number).toHaveAttribute("inputmode", "tel");
    expect(number).toHaveAttribute("autocomplete", "tel");
    expect(
      within(form).getByRole("combobox", { name: "Country code" }),
    ).toHaveValue("VN");
    expect(
      within(form).getByRole("option", { name: "VN (+84)" }),
    ).toBeInTheDocument();
    // UI-AUTH-04: no checkbox; /sign-in always asks for a persistent session.
    expect(within(form).queryByRole("checkbox")).toBeNull();
    await sendCode("0912 345 678");

    expect(api.startPhoneLogin).toHaveBeenCalledWith(
      { country_code: "VN", keep_signed_in: true, phone_number: "0912345678" },
      { baseUrl: "/api" },
    );
    const codeForm = phoneForm();
    expect(
      within(codeForm).getByText(
        "If this number can receive sign-in codes, a 6-digit code is on its way to +84 ***5678.",
      ),
    ).toBeInTheDocument();
    expect(
      within(codeForm).getByText("The code expires in 5:00."),
    ).toBeInTheDocument();
    const code = within(codeForm).getByRole("textbox", {
      name: "6-digit code",
    });
    expect(code).toHaveFocus();
    expect(code).toHaveAttribute("inputmode", "numeric");
    expect(code).toHaveAttribute("autocomplete", "one-time-code");
    const resend = within(codeForm).getByRole("button", {
      name: "Resend code (60s)",
    });
    expect(resend).toBeDisabled();

    tickSeconds(60);
    const ready = within(phoneForm()).getByRole("button", {
      name: "Resend code",
    });
    expect(ready).toBeEnabled();
    expect(
      within(phoneForm()).getByText("The code expires in 4:00."),
    ).toBeInTheDocument();
    fireEvent.click(ready);
    await settle();
    expect(api.resendAuthChallenge).toHaveBeenCalledWith({ baseUrl: "/api" });
    expect(api.startPhoneLogin).toHaveBeenCalledTimes(1);
    expect(
      within(phoneForm()).getByText(
        "A new 6-digit code is on its way to +84 ***5678.",
      ),
    ).toBeInTheDocument();

    // Letters are dropped from a pasted code.
    fireEvent.change(
      within(phoneForm()).getByRole("textbox", { name: "6-digit code" }),
      { target: { value: "12a34b56" } },
    );
    expect(
      within(phoneForm()).getByRole("textbox", { name: "6-digit code" }),
    ).toHaveValue("123456");
    fireEvent.click(
      within(phoneForm()).getByRole("button", { name: "Verify and log in" }),
    );
    await settle();

    expect(api.verifyPhoneLogin).toHaveBeenCalledWith(
      { challenge_id: "phone-2", code: "123456", keep_signed_in: true },
      { baseUrl: "/api" },
    );
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
    // Neither the number nor the code reached Web Storage.
    expect(window.localStorage.getItem(REMEMBERED_EMAIL_KEY)).toBeNull();
    for (const secret of ["0912345678", "912345678", "123456", "phone-"]) {
      expect(storedText()).not.toContain(secret);
    }
  });

  it("starts a new transaction when another flow replaced it or it ended", async () => {
    vi.useFakeTimers();
    api.startPhoneLogin
      .mockResolvedValueOnce(challenge("phone-1"))
      .mockResolvedValueOnce(challenge("phone-2"));
    api.resendAuthChallenge.mockRejectedValueOnce(apiError(400));
    await openPhone();
    await sendCode("+84912345678");
    tickSeconds(60);
    fireEvent.click(
      within(phoneForm()).getByRole("button", { name: "Resend code" }),
    );
    await settle();
    expect(api.resendAuthChallenge).toHaveBeenCalledTimes(1);
    expect(api.startPhoneLogin).toHaveBeenNthCalledWith(
      2,
      {
        country_code: "VN",
        keep_signed_in: true,
        phone_number: "+84912345678",
      },
      { baseUrl: "/api" },
    );
  });

  it("refuses an empty or unsupported number on the field", async () => {
    api.startPhoneLogin.mockRejectedValueOnce(
      apiError(400, "auth_invalid_phone_number"),
    );
    await openPhone();
    fireEvent.click(
      within(phoneForm()).getByRole("button", { name: "Send code" }),
    );
    await settle();
    const number = within(phoneForm()).getByRole("textbox", {
      name: "Phone number",
    });
    expect(number).toHaveAccessibleDescription(/Enter your phone number\./);
    expect(number).toHaveAttribute("aria-invalid", "true");
    expect(api.startPhoneLogin).not.toHaveBeenCalled();

    await sendCode("024 3822 1234");
    expect(api.startPhoneLogin).toHaveBeenCalledWith(
      {
        country_code: "VN",
        keep_signed_in: true,
        phone_number: "02438221234",
      },
      { baseUrl: "/api" },
    );
    expect(
      within(phoneForm()).getByRole("textbox", { name: "Phone number" }),
    ).toHaveAccessibleDescription(/Landlines and hotline numbers/);
    expect(
      within(phoneForm()).getByRole("textbox", { name: "Phone number" }),
    ).toHaveFocus();
  });

  it("answers a wrong code neutrally, drops it from the field and lets the user change the number", async () => {
    api.startPhoneLogin.mockResolvedValue(challenge("phone-1"));
    api.verifyPhoneLogin.mockRejectedValue(
      apiError(401, "auth_invalid_verification_code"),
    );
    await openPhone();
    await sendCode();
    await submitCode("654321");

    const form = phoneForm();
    expect(within(form).getByRole("alert")).toHaveTextContent(
      "That verification code is incorrect or has expired.",
    );
    const code = within(form).getByRole("textbox", { name: "6-digit code" });
    expect(code).toHaveValue("");
    expect(code).toHaveFocus();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();

    // An incomplete code never reaches the server.
    fireEvent.change(code, { target: { value: "12" } });
    fireEvent.click(
      within(form).getByRole("button", { name: "Verify and log in" }),
    );
    await settle();
    expect(code).toHaveAccessibleDescription(/exactly 6 digits/);
    expect(api.verifyPhoneLogin).toHaveBeenCalledTimes(1);

    fireEvent.click(
      within(form).getByRole("button", { name: "Change phone number" }),
    );
    await settle();
    const number = within(phoneForm()).getByRole("textbox", {
      name: "Phone number",
    });
    expect(number).toHaveFocus();
    expect(number).toHaveValue("0912 345 678");
  });

  it.each([
    [
      "auth_phone_stale",
      "This phone number has to be verified again before it can sign you in.",
    ],
    [
      "auth_phone_second_factor_required",
      "This account needs an extra verification step that isn't available right now.",
    ],
  ])("explains %s and goes back to the number", async (code, message) => {
    api.startPhoneLogin.mockResolvedValue(challenge("phone-1"));
    api.verifyPhoneLogin.mockRejectedValue(apiError(403, code));
    await openPhone();
    await sendCode();
    await submitCode();
    const form = phoneForm();
    expect(within(form).getByRole("alert")).toHaveTextContent(message);
    expect(within(form).getByRole("alert")).toHaveTextContent(
      /email, password, a passkey or a linked provider/,
    );
    expect(
      within(form).getByRole("textbox", { name: "Phone number" }),
    ).toBeInTheDocument();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("honours Retry-After on 429 and explains a 503 delivery outage", async () => {
    vi.useFakeTimers();
    api.startPhoneLogin
      .mockRejectedValueOnce(apiError(429, undefined, 30))
      .mockRejectedValueOnce(apiError(503, "auth_phone_delivery_unavailable"));
    await openPhone();
    await sendCode();
    const locked = within(phoneForm()).getByRole("button", {
      name: "Send code (30s)",
    });
    expect(locked).toBeDisabled();
    expect(within(phoneForm()).getByRole("alert")).toHaveTextContent(
      "Too many attempts. Please try again in 30 seconds.",
    );
    tickSeconds(30);
    const send = within(phoneForm()).getByRole("button", { name: "Send code" });
    expect(send).toBeEnabled();
    fireEvent.click(send);
    await settle();
    expect(within(phoneForm()).getByRole("alert")).toHaveTextContent(
      "Phone codes are temporarily unavailable. Log in with your email, a passkey or Google, Microsoft or Facebook instead.",
    );
  });

  it("moves an account with an authenticator app to the second step", async () => {
    api.startPhoneLogin.mockResolvedValue(challenge("phone-1"));
    api.verifyPhoneLogin.mockResolvedValue({
      methods: ["totp", "recovery_code"],
      status: "mfa_required",
    });
    api.verifyMFA.mockResolvedValue({ status: "authenticated" });
    await openPhone();
    await sendCode();
    await submitCode();

    const step = screen.getByRole("region", { name: "Two-step verification" });
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
    fireEvent.change(
      within(step).getByLabelText("6-digit code from your authenticator app"),
      { target: { value: "246810" } },
    );
    fireEvent.click(within(step).getByRole("button", { name: "Verify" }));
    await settle();
    expect(api.verifyMFA).toHaveBeenCalledWith(
      { code: "246810", method: "totp" },
      expect.anything(),
    );
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
  });

  it("asks a passkey-only account for its passkey (ADR-0039 §4.3)", async () => {
    vi.stubGlobal(
      "PublicKeyCredential",
      Object.assign(function PublicKeyCredential() {}, {
        isConditionalMediationAvailable: () => Promise.resolve(false),
      }),
    );
    Object.defineProperty(window.navigator, "credentials", {
      configurable: true,
      value: { create: vi.fn(), get: vi.fn() },
    });
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      passkeys_enabled: true,
    });
    api.startPhoneLogin.mockResolvedValue(challenge("phone-1"));
    api.verifyPhoneLogin.mockResolvedValue({
      methods: ["passkey"],
      status: "mfa_required",
    });
    await openPhone();
    await sendCode();
    await submitCode();

    const step = screen.getByRole("region", { name: "Two-step verification" });
    expect(
      within(step).getByRole("button", { name: "Verify with a passkey" }),
    ).toBeInTheDocument();
    // A single method needs no chooser.
    expect(
      within(step).queryByRole("group", { name: "Verification method" }),
    ).toBeNull();
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("speaks Vietnamese and keeps the password switch", async () => {
    await openPhone("vi");
    const form = screen.getByRole("form", {
      name: "Đăng nhập bằng mã gửi tới số điện thoại",
    });
    expect(
      within(form).getByRole("textbox", { name: "Số điện thoại" }),
    ).toBeInTheDocument();
    expect(
      within(form).getByRole("button", { name: "Gửi mã" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Đăng nhập bằng mật khẩu" }),
    );
    await settle();
    expect(
      screen.getByRole("textbox", { name: "Email hoặc tên đăng nhập" }),
    ).toHaveFocus();
  });
});
