import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type * as ApiClient from "@tutorhub/api-client";
import {
  APIRequestError,
  type AuthProvidersResponse,
  type AuthSignupChallengeResponse,
  type PendingRegistrationPreview,
  type PendingRegistrationResult,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { CompleteRegistrationPage } from "./CompleteRegistrationPage";

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

const MASKED = "f***@example.com";
const EMAIL = "fresh.person@example.test";
const CODE = "123456";
const CHALLENGE_ID = "0f8f3b7c-5f7a-4d4e-9a62-3b1f7c2d8e90";
const TERMS = "2026-11-16-v2.0";

const PROVIDERS: AuthProvidersResponse = {
  email_code_enabled: true,
  legal_terms_version: TERMS,
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
  sso_domains_enabled: false,
};

function preview(
  overrides: Partial<PendingRegistrationPreview> = {},
): PendingRegistrationPreview {
  return {
    expires_in_seconds: 900,
    masked_email: MASKED,
    provider: "facebook",
    ...overrides,
  };
}

function challenge(
  overrides: Partial<AuthSignupChallengeResponse> = {},
): AuthSignupChallengeResponse {
  return {
    challenge_id: CHALLENGE_ID,
    cooldown_seconds: 60,
    expires_in_seconds: 600,
    ...overrides,
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

async function flushMicrotasks() {
  await act(async () => {
    for (let tick = 0; tick < 12; tick += 1) {
      await Promise.resolve();
    }
  });
}

async function settle() {
  await flushMicrotasks();
  if (vi.isFakeTimers()) {
    act(() => {
      vi.advanceTimersByTime(0);
    });
  } else {
    await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  }
  await flushMicrotasks();
}

async function renderPage({
  entry = "/complete-registration?provider=facebook",
  language = "en",
}: { entry?: string; language?: Language } = {}) {
  const router = createMemoryRouter(
    [
      { path: "/complete-registration", element: <CompleteRegistrationPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: [entry] },
  );
  const view = render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return { ...view, router };
}

function emailInput() {
  return screen.getByRole("textbox", { name: "Email address" });
}

function otpInput() {
  return screen.getByRole("textbox", { name: "Verification code" });
}

async function sendCode(email = EMAIL) {
  fireEvent.change(emailInput(), { target: { value: email } });
  fireEvent.click(screen.getByRole("button", { name: "Send code" }));
  await settle();
}

async function fillAndSubmit({
  code = CODE,
  terms = true,
}: { code?: string; terms?: boolean } = {}) {
  fireEvent.change(otpInput(), { target: { value: code } });
  if (terms) {
    fireEvent.click(screen.getByRole("checkbox", { name: /User Agreement/ }));
  }
  fireEvent.click(screen.getByRole("button", { name: "Finish sign-up" }));
  await settle();
}

function storageSnapshot() {
  return JSON.stringify({
    local: { ...window.localStorage },
    session: { ...window.sessionStorage },
  });
}

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  for (const mock of Object.values(api)) mock.mockReset();
  session.completeNativeSignIn.mockReset();
  session.completeNativeSignIn.mockResolvedValue(undefined);
  api.getAuthProviders.mockResolvedValue(PROVIDERS);
  api.getPendingRegistration.mockResolvedValue(preview());
  api.startPendingRegistration.mockResolvedValue(challenge());
  api.resendPendingRegistration.mockResolvedValue(challenge());
  api.cancelPendingRegistration.mockResolvedValue(undefined);
  api.completePendingRegistration.mockResolvedValue({
    return_to: "/app/classes?tab=1",
    status: "authenticated",
  } satisfies PendingRegistrationResult);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("CompleteRegistrationPage preview", () => {
  it("announces loading, then shows the masked hint and a live countdown", async () => {
    vi.useFakeTimers();
    let resolvePreview: (value: PendingRegistrationPreview) => void = () =>
      undefined;
    api.getPendingRegistration.mockReturnValue(
      new Promise<PendingRegistrationPreview>((resolve) => {
        resolvePreview = resolve;
      }),
    );
    await renderPage();

    expect(
      screen.getByRole("status", { name: "Checking your sign-up request" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Finish signing up with Facebook",
      }),
    ).toBeInTheDocument();

    await act(async () => resolvePreview(preview()));
    await settle();
    expect(
      screen.getByText(`Suggested by Facebook: ${MASKED}`),
    ).toBeInTheDocument();
    const remaining = screen.getByText("15:00");
    expect(remaining.tagName).toBe("TIME");
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByText("14:59")).toBeInTheDocument();
    // The full email is never prefilled, not even from the hint.
    expect(emailInput()).toHaveValue("");
  });

  it("omits the hint when Facebook shared no email", async () => {
    api.getPendingRegistration.mockResolvedValue(
      preview({ masked_email: undefined }),
    );
    await renderPage();

    expect(screen.queryByText(/Suggested by Facebook/)).not.toBeInTheDocument();
    expect(emailInput()).toBeInTheDocument();
  });

  it("turns into the unavailable state when the countdown reaches zero", async () => {
    vi.useFakeTimers();
    api.getPendingRegistration.mockResolvedValue(
      preview({ expires_in_seconds: 2 }),
    );
    await renderPage();
    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(
      screen.getByRole("heading", {
        name: "This sign-up request is no longer valid",
      }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("explains a missing request (404) and links back to sign-in", async () => {
    api.getPendingRegistration.mockRejectedValue(
      apiError(404, "auth_registration_unavailable"),
    );
    await renderPage();

    expect(
      screen.getByRole("heading", {
        name: "This sign-up request is no longer valid",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Back to sign-in" }),
    ).toHaveAttribute("href", "/sign-in");
  });

  it("offers Retry when the request cannot be checked", async () => {
    api.getPendingRegistration
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValueOnce(preview());
    await renderPage();

    expect(
      screen.getByRole("heading", {
        name: "We couldn't check the sign-up request",
      }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await settle();
    expect(api.getPendingRegistration).toHaveBeenCalledTimes(2);
    expect(emailInput()).toBeInTheDocument();
  });

  it("notes when new accounts are disabled without hiding the form", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      signup_enabled: false,
    });
    await renderPage();

    expect(
      screen.getByText(/New accounts can't be created right now/),
    ).toBeInTheDocument();
    expect(emailInput()).toBeInTheDocument();
  });

  it("renders Vietnamese copy", async () => {
    await renderPage({ language: "vi" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Hoàn tất đăng ký bằng Facebook",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Địa chỉ email" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Gửi mã" })).toBeInTheDocument();
  });
});

describe("CompleteRegistrationPage email step", () => {
  it("validates the email locally without calling the API", async () => {
    await renderPage();
    await sendCode("not-an-email");

    expect(api.startPendingRegistration).not.toHaveBeenCalled();
    expect(emailInput()).toHaveAttribute("aria-invalid", "true");
    expect(emailInput()).toHaveAccessibleDescription(
      expect.stringContaining("Please enter a valid email address."),
    );
  });

  it("sends a code, shows a neutral notice and moves to the code step", async () => {
    await renderPage();
    await sendCode(`  ${EMAIL}  `);

    expect(api.startPendingRegistration).toHaveBeenCalledWith(
      { email: EMAIL },
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(
      screen.getByText(
        `If the address is valid, a 6-digit code has been sent to ${EMAIL}. It's valid for 10 minutes.`,
      ),
    ).toBeInTheDocument();
    const heading = screen.getByRole("heading", {
      name: "Step 2: Enter the verification code",
    });
    expect(heading).toHaveFocus();
    const otp = otpInput();
    expect(otp).toHaveAttribute("autocomplete", "one-time-code");
    expect(otp).toHaveAttribute("inputmode", "numeric");
    expect(otp).toHaveAttribute("maxlength", "6");
    expect(
      screen.getByRole("button", { name: "Resend in 60s" }),
    ).toBeDisabled();
    // Neither the email nor anything else lands in Web Storage.
    expect(storageSnapshot()).not.toContain(EMAIL);
  });

  it("maps a server-side email rejection to the field error", async () => {
    api.startPendingRegistration.mockRejectedValue(apiError(400));
    await renderPage();
    await sendCode();

    expect(emailInput()).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("textbox", { name: "Verification code" })).toBe(
      null,
    );
  });

  it("turns into the unavailable state when the registration ended", async () => {
    api.startPendingRegistration.mockRejectedValue(
      apiError(400, "auth_registration_unavailable"),
    );
    await renderPage();
    await sendCode();

    expect(
      screen.getByRole("heading", {
        name: "This sign-up request is no longer valid",
      }),
    ).toBeInTheDocument();
  });

  it("locks sending for the Retry-After window of a 429", async () => {
    vi.useFakeTimers();
    api.startPendingRegistration.mockRejectedValue(
      apiError(429, undefined, 30),
    );
    await renderPage();
    await sendCode();

    expect(screen.getByRole("alert")).toHaveTextContent(
      "Too many attempts. Please try again in 30 seconds.",
    );
    expect(
      screen.getByRole("button", { name: "Try again in 30s" }),
    ).toBeDisabled();
  });

  it.each([
    [apiError(503, undefined, 2), "TutorHub is busy right now."],
    [new TypeError("offline"), "Couldn't reach TutorHub."],
  ])("reports %s while sending", async (error, message) => {
    api.startPendingRegistration.mockRejectedValue(error);
    await renderPage();
    await sendCode();

    expect(screen.getByRole("alert")).toHaveTextContent(message);
  });

  it("ignores a late answer after the email was edited", async () => {
    let resolveStart: (value: AuthSignupChallengeResponse) => void = () =>
      undefined;
    api.startPendingRegistration.mockReturnValue(
      new Promise<AuthSignupChallengeResponse>((resolve) => {
        resolveStart = resolve;
      }),
    );
    await renderPage();
    await sendCode();
    fireEvent.change(emailInput(), {
      target: { value: "another@example.test" },
    });
    await act(async () => resolveStart(challenge()));
    await settle();

    expect(emailInput()).toHaveValue("another@example.test");
    expect(
      screen.queryByRole("textbox", { name: "Verification code" }),
    ).not.toBeInTheDocument();
  });
});

describe("CompleteRegistrationPage code step", () => {
  it("resends after the cooldown and reminds when the code expired", async () => {
    vi.useFakeTimers();
    api.startPendingRegistration.mockResolvedValue(
      challenge({ expires_in_seconds: 120 }),
    );
    await renderPage();
    await sendCode();

    // The cooldown ticks once per rendered second.
    for (let second = 0; second < 60; second += 1) {
      act(() => {
        vi.advanceTimersByTime(1000);
      });
    }
    await settle();
    fireEvent.click(screen.getByRole("button", { name: "Resend code" }));
    await settle();
    expect(api.resendPendingRegistration).toHaveBeenCalledTimes(1);
    expect(api.startPendingRegistration).toHaveBeenCalledTimes(1);

    act(() => {
      vi.advanceTimersByTime(600_000);
    });
    await settle();
    expect(
      screen.getByText(
        "The code has expired. Select Resend code to get a new one.",
      ),
    ).toBeInTheDocument();
  });

  it("drops the old code when the email is changed", async () => {
    await renderPage();
    await sendCode();
    fireEvent.change(otpInput(), { target: { value: "654321" } });
    fireEvent.click(screen.getByRole("button", { name: "Change email" }));
    await settle();

    expect(
      screen.getByRole("heading", { name: "Step 1: Verify your email" }),
    ).toBeInTheDocument();
    expect(emailInput()).toHaveValue(EMAIL);
    // Sending again starts a new challenge instead of resending the old one.
    fireEvent.click(screen.getByRole("button", { name: "Send code" }));
    await settle();
    expect(api.startPendingRegistration).toHaveBeenCalledTimes(2);
    expect(otpInput()).toHaveValue("");
  });

  it("requires a 6-digit code and the terms before completing", async () => {
    await renderPage();
    await sendCode();
    await fillAndSubmit({ code: "12", terms: false });

    expect(api.completePendingRegistration).not.toHaveBeenCalled();
    expect(otpInput()).toHaveAttribute("aria-invalid", "true");
    fireEvent.change(otpInput(), { target: { value: CODE } });
    fireEvent.click(screen.getByRole("button", { name: "Finish sign-up" }));
    await settle();
    expect(api.completePendingRegistration).not.toHaveBeenCalled();
    expect(
      screen.getByText(
        "You must agree to the User Agreement and Privacy Policy.",
      ),
    ).toBeInTheDocument();
  });

  it("accepts the terms from the legal dialog", async () => {
    await renderPage();
    await sendCode();
    fireEvent.click(screen.getByRole("button", { name: "User Agreement" }));
    await settle();
    const dialog = screen.getByRole("dialog");
    expect(dialog).toBeInTheDocument();
    const accept = screen
      .getAllByRole("button")
      .find((button) => /agree|accept|đồng ý/i.test(button.textContent ?? ""));
    expect(accept).toBeDefined();
    if (accept) fireEvent.click(accept);
    await settle();
    expect(
      screen.getByRole("checkbox", { name: /User Agreement/ }),
    ).toBeChecked();
  });

  it("creates the account, finishes the session and returns to a safe path", async () => {
    await renderPage();
    await sendCode();
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Keep me signed in" }),
    );
    await fillAndSubmit();

    expect(api.completePendingRegistration).toHaveBeenCalledWith(
      {
        challenge_id: CHALLENGE_ID,
        code: CODE,
        keep_signed_in: true,
        terms_version: TERMS,
      },
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(session.completeNativeSignIn).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/app/classes?tab=1",
    );
    expect(storageSnapshot()).not.toContain(CODE);
  });

  it.each([
    "//evil.example/app",
    "https://evil.example",
    "/complete-registration?provider=facebook",
    "/account-link?provider=facebook",
  ])("re-sanitizes return_to %s", async (returnTo) => {
    api.completePendingRegistration.mockResolvedValue({
      return_to: returnTo,
      status: "authenticated",
    });
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    expect(screen.getByTestId("landing")).toHaveTextContent("/app/home");
  });

  it("moves an email that already has an owner to the linking screen", async () => {
    api.completePendingRegistration.mockResolvedValue({
      return_to: "/app/home",
      status: "link_pending",
    });
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    expect(session.completeNativeSignIn).not.toHaveBeenCalled();
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/account-link?provider=facebook",
    );
  });

  it("uses one generic message for a wrong or expired code", async () => {
    api.completePendingRegistration.mockRejectedValue(
      apiError(401, "auth_invalid_verification_code"),
    );
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    expect(otpInput()).toHaveValue("");
    expect(otpInput()).toHaveFocus();
    expect(
      screen.getByText(
        "The code is incorrect or has expired. Check it or request a new one.",
      ),
    ).toBeInTheDocument();
  });

  it("asks to accept again when the terms changed", async () => {
    api.completePendingRegistration.mockRejectedValue(
      apiError(400, "auth_terms_version_changed"),
    );
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    expect(
      screen.getByRole("checkbox", { name: /User Agreement/ }),
    ).not.toBeChecked();
    expect(
      screen.getByText(
        "The terms have just been updated. Please review them and agree again.",
      ),
    ).toBeInTheDocument();
    expect(api.getAuthProviders).toHaveBeenCalledTimes(2);
  });

  it.each([
    [
      apiError(403, "auth_signup_disabled"),
      "New accounts can't be created with Facebook right now.",
    ],
    [apiError(400), "We couldn't finish signing up with this email."],
    [apiError(409), "The request changed while we were processing it."],
    [apiError(503, undefined, 2), "TutorHub is busy right now."],
    [new TypeError("offline"), "Couldn't reach TutorHub."],
  ])(
    "maps a completion failure to a fixed message (%s)",
    async (error, text) => {
      api.completePendingRegistration.mockRejectedValue(error);
      await renderPage();
      await sendCode();
      await fillAndSubmit();

      expect(screen.getByRole("alert")).toHaveTextContent(text);
      expect(session.completeNativeSignIn).not.toHaveBeenCalled();
    },
  );

  it("turns into the unavailable state when the registration was used", async () => {
    api.completePendingRegistration.mockRejectedValue(
      apiError(400, "auth_registration_unavailable"),
    );
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    expect(
      screen.getByRole("heading", {
        name: "This sign-up request is no longer valid",
      }),
    ).toBeInTheDocument();
  });

  it("locks the button for the Retry-After window of a 429", async () => {
    vi.useFakeTimers();
    api.completePendingRegistration.mockRejectedValue(
      apiError(429, undefined, 45),
    );
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    expect(
      screen.getByRole("button", { name: "Try again in 45s" }),
    ).toBeDisabled();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Please try again in 45 seconds.",
    );
  });

  it("retries only the session hand-off when it fails", async () => {
    session.completeNativeSignIn
      .mockRejectedValueOnce(new Error("csrf"))
      .mockResolvedValueOnce(undefined);
    await renderPage();
    await sendCode();
    await fillAndSubmit();

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(
      "Your account is ready, but we couldn't load your session yet.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await settle();
    expect(api.completePendingRegistration).toHaveBeenCalledTimes(1);
    expect(session.completeNativeSignIn).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/app/classes?tab=1",
    );
  });
});

describe("CompleteRegistrationPage cancel", () => {
  it("cancels the registration and returns to sign-in", async () => {
    await renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign-up" }));
    await settle();

    expect(api.cancelPendingRegistration).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent("/sign-in");
  });

  it("still returns to sign-in when the registration was already gone", async () => {
    api.cancelPendingRegistration.mockRejectedValue(
      apiError(400, "auth_registration_unavailable"),
    );
    await renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign-up" }));
    await settle();

    expect(screen.getByTestId("landing")).toHaveTextContent("/sign-in");
  });

  it("stays on the page when cancelling fails", async () => {
    api.cancelPendingRegistration.mockRejectedValue(apiError(503));
    await renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Cancel sign-up" }));
    await settle();

    expect(screen.getByRole("alert")).toHaveTextContent(
      "TutorHub is busy right now.",
    );
    expect(screen.queryByTestId("landing")).not.toBeInTheDocument();
  });
});

describe("CompleteRegistrationPage AUTH-07B Microsoft", () => {
  const MICROSOFT_ENTRY = "/complete-registration?provider=microsoft";

  beforeEach(() => {
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      oauth_providers: ["microsoft"],
    });
    api.getPendingRegistration.mockResolvedValue(
      preview({ provider: "microsoft" }),
    );
  });

  it("names Microsoft while loading and after the preview, with the same OTP form", async () => {
    let resolvePreview: (value: PendingRegistrationPreview) => void = () =>
      undefined;
    api.getPendingRegistration.mockReturnValue(
      new Promise<PendingRegistrationPreview>((resolve) => {
        resolvePreview = resolve;
      }),
    );
    await renderPage({ entry: MICROSOFT_ENTRY });
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Finish signing up with Microsoft",
      }),
    ).toBeInTheDocument();

    await act(async () => resolvePreview(preview({ provider: "microsoft" })));
    await settle();
    expect(
      screen.getByText(`Suggested by Microsoft: ${MASKED}`),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Facebook/)).not.toBeInTheDocument();
    // The hint is never prefilled: the person proves an email with a code.
    expect(emailInput()).toHaveValue("");
    await sendCode();
    expect(api.startPendingRegistration).toHaveBeenCalledWith(
      { email: EMAIL },
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(otpInput()).toHaveAttribute("autocomplete", "one-time-code");
    expect(otpInput()).toHaveAttribute("inputmode", "numeric");
  });

  it("trusts the server's provider over the address bar", async () => {
    await renderPage({ entry: "/complete-registration?provider=facebook" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Finish signing up with Microsoft",
      }),
    ).toBeInTheDocument();
  });

  it("creates the account and finishes the session", async () => {
    await renderPage({ entry: MICROSOFT_ENTRY });
    await sendCode();
    await fillAndSubmit();

    expect(session.completeNativeSignIn).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/app/classes?tab=1",
    );
    expect(storageSnapshot()).not.toContain(CODE);
    expect(storageSnapshot()).not.toContain(EMAIL);
  });

  it("moves an email that already has an owner to the Microsoft linking screen", async () => {
    api.completePendingRegistration.mockResolvedValue({
      return_to: "/app/home",
      status: "link_pending",
    });
    await renderPage({ entry: MICROSOFT_ENTRY });
    await sendCode();
    await fillAndSubmit();

    expect(session.completeNativeSignIn).not.toHaveBeenCalled();
    expect(screen.getByTestId("landing")).toHaveTextContent(
      "/account-link?provider=microsoft",
    );
  });

  it("asks to sign in with Microsoft again when the request is gone", async () => {
    api.getPendingRegistration.mockRejectedValue(
      apiError(404, "auth_registration_unavailable"),
    );
    await renderPage({ entry: MICROSOFT_ENTRY });

    expect(
      screen.getByText(
        "It may have expired, been used, or been cancelled. Sign in with Microsoft again to start over.",
      ),
    ).toBeInTheDocument();
  });

  it("renders Vietnamese Microsoft copy", async () => {
    await renderPage({ entry: MICROSOFT_ENTRY, language: "vi" });

    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Hoàn tất đăng ký bằng Microsoft",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(`Gợi ý từ Microsoft: ${MASKED}`),
    ).toBeInTheDocument();
  });
});
