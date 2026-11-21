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
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import { SignInPage } from "./SignInPage";

/**
 * AUTH-12B Home Realm Discovery on /sign-in: the explicit "Sign in with
 * school SSO" check, 403 `auth_sso_required` on every local method, the
 * `?error=auth_sso_required` callback, and the full-page hand-off.
 */

const { api } = vi.hoisted(() => ({
  api: {
    discoverAuthIdentifier: vi.fn(),
    getAuthProviders: vi.fn(),
    loginWithPassword: vi.fn(),
    startEmailCodeLogin: vi.fn(),
    startEmailSignup: vi.fn(),
    startPasskeyAssertion: vi.fn(),
    startPasswordRecovery: vi.fn(),
  },
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", () => ({
  useSession: () => ({
    completeNativeSignIn: vi.fn(),
    resetAfterPasswordRecovery: vi.fn(),
    signIn: vi.fn(),
    status: "unauthenticated",
  }),
}));

const SCHOOL_EMAIL = "learner@school.example";

const PROVIDERS: AuthProvidersResponse = {
  device_management_enabled: true,
  email_code_enabled: true,
  legal_terms_version: "2026-11-16-v2.0",
  mfa_totp_enabled: false,
  oauth_providers: ["google", "microsoft"],
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

function route(
  required: boolean,
  provider: "google" | "microsoft" = "google",
): AuthIdentifierDiscoverResponse {
  return {
    kind: "email",
    sso: { required, available: true, provider, domain: "school.example" },
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

async function renderSignIn(
  initialEntry = "/sign-in",
  language: Language = "en",
) {
  const router = createMemoryRouter(
    [
      { path: "/sign-in", element: <SignInPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: [initialEntry] },
  );
  render(
    <I18nProvider initialLanguage={language}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return router;
}

function stubLocationAssign() {
  const assign = vi.fn();
  vi.stubGlobal("location", {
    ...window.location,
    assign,
    href: window.location.href,
    origin: window.location.origin,
  });
  return assign;
}

function identifier() {
  return screen.getByRole("textbox", { name: "Email or username" });
}

function schoolButton() {
  return screen.getByRole("button", { name: "Sign in with school SSO" });
}

function startURL(assign: ReturnType<typeof vi.fn>) {
  const value = assign.mock.calls[0]?.[0] as string | undefined;
  if (!value) throw new Error("no navigation");
  return new URL(value, "http://localhost");
}

describe("SignInPage AUTH-12B school SSO", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    for (const mock of Object.values(api)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("offers nothing while the deployment flag is off", async () => {
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      sso_domains_enabled: false,
    });
    await renderSignIn();
    expect(
      screen.queryByRole("button", { name: "Sign in with school SSO" }),
    ).not.toBeInTheDocument();
  });

  it("asks for a school email before discovery", async () => {
    await renderSignIn();
    fireEvent.click(schoolButton());
    expect(
      await screen.findByText(
        "Enter your school email address to continue with school SSO.",
      ),
    ).toBeInTheDocument();
    expect(identifier()).toHaveFocus();
    expect(identifier()).toHaveAttribute("aria-invalid", "true");
    expect(api.discoverAuthIdentifier).not.toHaveBeenCalled();
  });

  it("routes a required school domain to its provider with login_hint and domain", async () => {
    const assign = stubLocationAssign();
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    await renderSignIn();
    fireEvent.change(identifier(), { target: { value: ` ${SCHOOL_EMAIL} ` } });
    fireEvent.click(schoolButton());

    expect(
      await screen.findByText(
        "school.example requires signing in with your school's Google account.",
      ),
    ).toBeInTheDocument();
    expect(api.discoverAuthIdentifier).toHaveBeenCalledWith(
      { identifier: SCHOOL_EMAIL },
      expect.anything(),
    );
    const continueButton = screen.getByRole("button", {
      name: "Continue with school Google",
    });
    await waitFor(() => expect(continueButton).toHaveFocus());
    // UI-AUTH-04: the school route sits above "Or"; the entry point is a
    // quiet text action under the providers.
    const divider = document.querySelector(".sign-in-divider");
    if (!(divider instanceof HTMLElement)) throw new Error("Missing divider");
    expect(continueButton.compareDocumentPosition(divider)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    expect(schoolButton()).toHaveClass("sign-in-form__text-action");
    expect(divider.compareDocumentPosition(schoolButton())).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
    fireEvent.click(continueButton);
    await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
    const url = startURL(assign);
    expect(url.pathname).toBe("/api/v1/auth/oauth/google/start");
    expect(url.searchParams.get("intent")).toBe("login");
    expect(url.searchParams.get("login_hint")).toBe(SCHOOL_EMAIL);
    expect(url.searchParams.get("domain")).toBe("school.example");
    expect(url.searchParams.get("return_to")).toBe("/app/home");
    // Nothing about the school account is kept in Web Storage.
    expect(
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ).not.toContain(SCHOOL_EMAIL);
  });

  it("suggests an optional Microsoft route and keeps the password form usable", async () => {
    api.discoverAuthIdentifier.mockResolvedValue(route(false, "microsoft"));
    await renderSignIn();
    fireEvent.change(identifier(), { target: { value: SCHOOL_EMAIL } });
    fireEvent.click(schoolButton());
    expect(
      await screen.findByText(/You can also continue with your password/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Continue with school Microsoft" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeEnabled();
    expect(screen.getByRole("button", { name: "Log In" })).toBeEnabled();

    // Editing the address drops the answer for the previous domain.
    fireEvent.change(identifier(), {
      target: { value: "learner@other.example" },
    });
    expect(
      screen.queryByRole("button", { name: "Continue with school Microsoft" }),
    ).not.toBeInTheDocument();
  });

  it("answers neutrally for a domain without school SSO and for refusals", async () => {
    api.discoverAuthIdentifier.mockResolvedValueOnce({
      kind: "email",
      sso: { required: false },
    });
    await renderSignIn();
    fireEvent.change(identifier(), {
      target: { value: "someone@personal.example" },
    });
    fireEvent.click(schoolButton());
    expect(
      await screen.findByText(
        "School SSO isn't set up for this email domain. Continue with another sign-in method.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Continue with school/ }),
    ).not.toBeInTheDocument();

    api.discoverAuthIdentifier.mockRejectedValueOnce(
      apiError(429, undefined, 30),
    );
    fireEvent.click(schoolButton());
    expect(
      await screen.findByText(/Please wait 30 seconds|30 seconds/),
    ).toBeInTheDocument();
  });

  it("turns a password 403 auth_sso_required into the school route and clears the password", async () => {
    const assign = stubLocationAssign();
    api.loginWithPassword.mockRejectedValue(apiError(403, "auth_sso_required"));
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    await renderSignIn();
    fireEvent.change(identifier(), { target: { value: SCHOOL_EMAIL } });
    fireEvent.change(screen.getByLabelText("Password"), {
      target: { value: "Harbor-Lantern-77" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Log In" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "This email address must sign in through your school's SSO.",
    );
    expect(screen.getByLabelText("Password")).toHaveValue("");
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Continue with school Google",
      }),
    );
    await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
    expect(startURL(assign).searchParams.get("login_hint")).toBe(SCHOOL_EMAIL);
  });

  it("handles auth_sso_required when an email code is requested", async () => {
    api.startEmailCodeLogin.mockRejectedValue(
      apiError(403, "auth_sso_required"),
    );
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Log in with OTP" }));
    const form = screen.getByRole("form", {
      name: "Log in with a code sent to your email",
    });
    fireEvent.change(
      within(form).getByRole("textbox", { name: "Email Address" }),
      {
        target: { value: SCHOOL_EMAIL },
      },
    );
    fireEvent.click(within(form).getByRole("button", { name: "Get Code" }));
    expect(
      await within(form).findByText(
        /This email address must sign in through your school's SSO/,
      ),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("button", {
        name: "Continue with school Google",
      }),
    ).toBeInTheDocument();
  });

  it("handles auth_sso_required when a sign-up code is requested", async () => {
    api.startEmailSignup.mockRejectedValue(apiError(403, "auth_sso_required"));
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Sign Up" }));
    const form = screen.getByRole("form", { name: "Create an Account" });
    fireEvent.change(within(form).getByRole("textbox", { name: "Full Name" }), {
      target: { value: "New Learner" },
    });
    fireEvent.change(
      within(form).getByRole("textbox", { name: "Email Address" }),
      { target: { value: SCHOOL_EMAIL } },
    );
    fireEvent.click(within(form).getByRole("button", { name: "Get Code" }));
    expect(
      await within(form).findByText(
        /This email address must sign in through your school's SSO/,
      ),
    ).toBeInTheDocument();
    expect(
      await within(form).findByRole("button", {
        name: "Continue with school Google",
      }),
    ).toBeInTheDocument();
  });

  it("handles auth_sso_required in the password recovery dialog", async () => {
    api.startPasswordRecovery.mockRejectedValue(
      apiError(403, "auth_sso_required"),
    );
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    await renderSignIn();
    fireEvent.click(screen.getByRole("button", { name: "Forgot password" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Email Address" }),
      {
        target: { value: SCHOOL_EMAIL },
      },
    );
    fireEvent.click(within(dialog).getByRole("button", { name: /Get Code/ }));
    expect(
      await within(dialog).findByText(
        /This email address must sign in through your school's SSO/,
      ),
    ).toBeInTheDocument();
    expect(
      await within(dialog).findByRole("button", {
        name: "Continue with school Google",
      }),
    ).toBeInTheDocument();
  });

  it("explains ?error=auth_sso_required neutrally and cleans the address", async () => {
    const router = await renderSignIn("/sign-in?error=auth_sso_required");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      "You can't sign in with that account: this email domain requires your school's SSO.",
    );
    await waitFor(() => expect(router.state.location.search).toBe(""));
  });

  it("renders Vietnamese copy", async () => {
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    await renderSignIn("/sign-in?error=auth_sso_required", "vi");
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "tên miền email này yêu cầu đăng nhập qua SSO của trường",
    );
    fireEvent.change(
      screen.getByRole("textbox", { name: "Email hoặc tên đăng nhập" }),
      { target: { value: SCHOOL_EMAIL } },
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Đăng nhập bằng SSO trường học" }),
    );
    expect(
      await screen.findByText(
        "Tên miền school.example yêu cầu đăng nhập bằng tài khoản Google của trường.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Tiếp tục với Google của trường" }),
    ).toBeInTheDocument();
  });
});

describe("SignInPage AUTH-12B passkey autofill hand-off", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("aborts the background passkey request before leaving for the school provider", async () => {
    for (const mock of Object.values(api)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      passkeys_enabled: true,
    });
    api.startPasskeyAssertion.mockResolvedValue({
      allowCredentials: [],
      challenge: "Y2hhbGxlbmdl",
      rpId: "localhost",
      timeout: 300000,
      userVerification: "preferred",
    });
    api.discoverAuthIdentifier.mockResolvedValue(route(true));
    const signals: AbortSignal[] = [];
    vi.stubGlobal(
      "PublicKeyCredential",
      Object.assign(function PublicKeyCredential() {}, {
        isConditionalMediationAvailable: () => Promise.resolve(true),
      }),
    );
    Object.defineProperty(window.navigator, "credentials", {
      configurable: true,
      value: {
        create: vi.fn(),
        get: vi.fn(
          (request: CredentialRequestOptions & { mediation?: string }) =>
            new Promise((_, reject) => {
              if (request.signal) signals.push(request.signal);
              request.signal?.addEventListener("abort", () =>
                reject(new DOMException("aborted", "AbortError")),
              );
            }),
        ),
      },
    });
    const assign = stubLocationAssign();
    await renderSignIn();
    await waitFor(() => expect(signals).toHaveLength(1));
    expect(signals[0]?.aborted).toBe(false);

    fireEvent.change(identifier(), { target: { value: SCHOOL_EMAIL } });
    fireEvent.click(schoolButton());
    fireEvent.click(
      await screen.findByRole("button", {
        name: "Continue with school Google",
      }),
    );
    await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
    expect(signals[0]?.aborted).toBe(true);
  });
});
