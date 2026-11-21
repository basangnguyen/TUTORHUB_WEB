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
  type AuthSignupChallengeResponse,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { CompleteRegistrationPage } from "./CompleteRegistrationPage";

/**
 * AUTH-13B: a Facebook (or Microsoft) registration without a trusted email
 * asks for one. An address on a school domain that requires SSO answers 403
 * `auth_sso_required` on start, resend or complete: the page keeps (or
 * returns to) the email step, asks for another personal address and offers
 * to cancel the registration and sign in with school SSO.
 */

const { api, session } = vi.hoisted(() => ({
  api: {
    cancelPendingRegistration: vi.fn(),
    completePendingRegistration: vi.fn(),
    getAuthProviders: vi.fn(),
    getPendingRegistration: vi.fn(),
    resendPendingRegistration: vi.fn(),
    startPendingRegistration: vi.fn(),
  },
  session: { completeNativeSignIn: vi.fn() },
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", () => ({
  useSession: () => session,
}));

const SCHOOL_EMAIL = "student@school.example";
const PERSONAL_EMAIL = "fresh.person@example.test";
const CODE = "123456";

const PROVIDERS: AuthProvidersResponse = {
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
  oauth_providers: ["facebook"],
  oidc_default_enabled: false,
  passkeys_enabled: false,
  password_enabled: true,
  password_min_length: 8,
  password_recovery_enabled: true,
  phone_allowed_countries: ["VN"],
  phone_enabled: false,
  signup_enabled: true,
  sso_domains_enabled: true,
};

const COPY = {
  en: {
    notice:
      "This email address belongs to a school that requires school SSO, so it can't be used here. Enter a different personal email address, or cancel this sign-up and choose “Sign in with school SSO” on the sign-in page.",
    cancel: "Cancel and go to sign-in",
    email: "Email address",
    send: "Send code",
    otp: "Verification code",
    resend: "Resend code",
    submit: "Finish sign-up",
  },
  vi: {
    notice:
      "Địa chỉ email này thuộc trường học bắt buộc đăng nhập qua SSO trường học nên không dùng được ở đây. Hãy nhập một địa chỉ email cá nhân khác, hoặc hủy đăng ký này rồi chọn “Đăng nhập bằng SSO trường học” trên trang đăng nhập.",
    cancel: "Hủy và quay lại đăng nhập",
    email: "Địa chỉ email",
    send: "Gửi mã",
    otp: "Mã xác minh",
    resend: "Gửi lại mã",
    submit: "Hoàn tất đăng ký",
  },
} as const;

function challenge(
  overrides: Partial<AuthSignupChallengeResponse> = {},
): AuthSignupChallengeResponse {
  return {
    challenge_id: "0f8f3b7c-5f7a-4d4e-9a62-3b1f7c2d8e90",
    cooldown_seconds: 60,
    expires_in_seconds: 600,
    ...overrides,
  };
}

function ssoRequired() {
  return new APIRequestError(403, {
    code: "auth_sso_required",
    status: 403,
    title: "School SSO required",
    type: "about:blank",
  });
}

function LandingProbe() {
  const location = useLocation();
  return (
    <p data-testid="landing">{`${location.pathname}${location.search}`}</p>
  );
}

async function settle() {
  const flush = async () => {
    await act(async () => {
      for (let tick = 0; tick < 12; tick += 1) {
        await Promise.resolve();
      }
    });
  };
  await flush();
  if (vi.isFakeTimers()) {
    act(() => {
      vi.advanceTimersByTime(0);
    });
  } else {
    await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  }
  await flush();
}

async function renderPage(language: Language = "en") {
  const router = createMemoryRouter(
    [
      { path: "/complete-registration", element: <CompleteRegistrationPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: ["/complete-registration?provider=facebook"] },
  );
  render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return router;
}

function emailInput(language: Language = "en") {
  return screen.getByRole("textbox", { name: COPY[language].email });
}

async function sendCode(email: string, language: Language = "en") {
  fireEvent.change(emailInput(language), { target: { value: email } });
  fireEvent.click(screen.getByRole("button", { name: COPY[language].send }));
  await settle();
}

function storageSnapshot() {
  return JSON.stringify({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  });
}

describe("CompleteRegistrationPage AUTH-13B auth_sso_required", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    for (const mock of Object.values(api)) mock.mockReset();
    session.completeNativeSignIn.mockReset().mockResolvedValue(undefined);
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    api.getPendingRegistration.mockResolvedValue({
      expires_in_seconds: 900,
      provider: "facebook",
    });
    api.cancelPendingRegistration.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("start: keeps the email step, asks for another address and cancels on request", async () => {
    const copy = COPY.en;
    api.startPendingRegistration.mockRejectedValue(ssoRequired());
    const router = await renderPage();
    await sendCode(SCHOOL_EMAIL);

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(copy.notice);
    // Domain-level guidance: the address itself is not "invalid".
    expect(emailInput()).not.toHaveAttribute("aria-invalid");
    await waitFor(() => expect(emailInput()).toHaveFocus());
    expect(
      screen.queryByRole("textbox", { name: copy.otp }),
    ).not.toBeInTheDocument();
    // No Retry-After lock: another address can be sent right away.
    expect(screen.getByRole("button", { name: copy.send })).toBeEnabled();
    expect(storageSnapshot()).not.toContain(SCHOOL_EMAIL);

    fireEvent.click(within(alert).getByRole("button", { name: copy.cancel }));
    await settle();
    expect(api.cancelPendingRegistration).toHaveBeenCalledTimes(1);
    expect(router.state.location.pathname).toBe("/sign-in");
  });

  it("start: a different personal address clears the notice and continues", async () => {
    const copy = COPY.en;
    api.startPendingRegistration
      .mockRejectedValueOnce(ssoRequired())
      .mockResolvedValueOnce(challenge());
    await renderPage();
    await sendCode(SCHOOL_EMAIL);
    expect(screen.getByRole("alert")).toHaveTextContent(copy.notice);

    fireEvent.change(emailInput(), { target: { value: PERSONAL_EMAIL } });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: copy.send }));
    await settle();
    expect(api.startPendingRegistration).toHaveBeenLastCalledWith(
      { email: PERSONAL_EMAIL },
      expect.anything(),
    );
    expect(screen.getByRole("textbox", { name: copy.otp })).toBeInTheDocument();
  });

  it("complete: the dead code step returns to the email step with the notice", async () => {
    const copy = COPY.en;
    api.startPendingRegistration.mockResolvedValue(challenge());
    api.completePendingRegistration.mockRejectedValue(ssoRequired());
    await renderPage();
    await sendCode(SCHOOL_EMAIL);
    fireEvent.change(screen.getByRole("textbox", { name: copy.otp }), {
      target: { value: CODE },
    });
    fireEvent.click(screen.getByRole("checkbox", { name: /User Agreement/ }));
    fireEvent.click(screen.getByRole("button", { name: copy.submit }));
    await settle();

    expect(
      screen.queryByRole("textbox", { name: copy.otp }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent(copy.notice);
    await waitFor(() => expect(emailInput()).toHaveFocus());
    expect(emailInput()).toHaveValue(SCHOOL_EMAIL);
    expect(session.completeNativeSignIn).not.toHaveBeenCalled();
    expect(storageSnapshot()).not.toContain(CODE);
  });

  it("resend (vi): returns to the email step with the Vietnamese notice", async () => {
    vi.useFakeTimers();
    const copy = COPY.vi;
    api.startPendingRegistration.mockResolvedValue(challenge());
    api.resendPendingRegistration.mockRejectedValue(ssoRequired());
    await renderPage("vi");
    await sendCode(SCHOOL_EMAIL, "vi");
    // The 60-second resend cooldown ticks once per rendered second.
    for (let second = 0; second < 60; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    await settle();
    fireEvent.click(screen.getByRole("button", { name: copy.resend }));
    await settle();
    expect(api.resendPendingRegistration).toHaveBeenCalledTimes(1);

    expect(
      screen.queryByRole("textbox", { name: copy.otp }),
    ).not.toBeInTheDocument();
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(copy.notice);
    expect(
      within(alert).getByRole("button", { name: copy.cancel }),
    ).toBeInTheDocument();
    expect(emailInput("vi")).toHaveFocus();
  });
});
