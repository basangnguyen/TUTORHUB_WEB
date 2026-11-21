import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
  type CurrentUser,
  type OAuthLinkIntentResponse,
  type PasskeyRequestOptions,
} from "@tutorhub/api-client";
import { createMemoryRouter, useLocation } from "react-router";
import { RouterProvider } from "react-router/dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider, type Language } from "../app/i18n";
import type { SessionStatus } from "../app/session";
import { AccountLinkPage } from "./AccountLinkPage";

/**
 * AUTH-13B: /account-link embeds LoginForm without Home Realm Discovery.
 * A 403 `auth_sso_required` from a password, an email code, a passkey or a
 * phone code explains the rule and links back to /sign-in, where school SSO
 * starts; the link intent itself is left alone.
 */

const { api, sessionStore } = vi.hoisted(() => {
  type Listener = () => void;
  interface SessionSnapshot {
    completeNativeSignIn: () => Promise<void>;
    currentUser: CurrentUser | null;
    error: Error | null;
    refresh: () => Promise<void>;
    resetAfterPasswordRecovery: () => Promise<void>;
    signIn: (returnTo?: string) => void;
    signOut: (options?: { returnTo?: string }) => Promise<void>;
    status: SessionStatus;
  }
  const listeners = new Set<Listener>();
  let snapshot = {} as SessionSnapshot;
  return {
    api: {
      cancelLinkIntent: vi.fn(),
      confirmLinkIntent: vi.fn(),
      getAuthProviders: vi.fn(),
      getOAuthLinkIntent: vi.fn(),
      loginWithPassword: vi.fn(),
      resendAuthChallenge: vi.fn(),
      startEmailCodeLogin: vi.fn(),
      startPasskeyAssertion: vi.fn(),
      startPhoneLogin: vi.fn(),
      verifyPasskeyAssertion: vi.fn(),
      verifyPhoneLogin: vi.fn(),
    },
    sessionStore: {
      get: () => snapshot,
      set(next: Partial<SessionSnapshot>) {
        snapshot = { ...snapshot, ...next };
        for (const listener of listeners) listener();
      },
      subscribe(listener: Listener) {
        listeners.add(listener);
        return () => listeners.delete(listener);
      },
    },
  };
});

vi.mock("@tutorhub/api-client", async (importOriginal) => ({
  ...(await importOriginal<typeof ApiClient>()),
  ...api,
}));

vi.mock("../app/session", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    useSession: () =>
      useSyncExternalStore(sessionStore.subscribe, sessionStore.get),
  };
});

const SCHOOL_EMAIL = "teacher@school.example";
const PASSWORD = "Harbor-Lantern-77";

const PROVIDERS: AuthProvidersResponse = {
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
  // Even when the deployment has school SSO, this page offers no HRD.
  sso_domains_enabled: true,
};

const OPTIONS: PasskeyRequestOptions = {
  allowCredentials: [],
  challenge: "Y2hhbGxlbmdl",
  rpId: "localhost",
  timeout: 300000,
  userVerification: "preferred",
};

const COPY = {
  en: {
    notice:
      "This account must sign in through your school's SSO. Go back to the sign-in page, enter your school email and choose “Sign in with school SSO”.",
    link: "Back to sign-in for school SSO",
    schoolSSO: "Sign in with school SSO",
    identifier: "Email or username",
    password: "Password",
    logIn: "Log In",
    emailMethod: "Log in with an email code",
    emailForm: "Log in with a code sent to your email",
    email: "Email Address",
    getCode: "Get Code",
    passkey: "Sign in with a passkey",
    phoneMethod: "Log in with a phone number",
    phoneForm: "Log in with a code sent to your phone",
    phoneNumber: "Phone number",
    sendCode: "Send code",
    smsCode: "6-digit code",
    verifyPhone: "Verify and log in",
  },
  vi: {
    notice:
      "Tài khoản này phải đăng nhập qua SSO của trường. Hãy quay lại trang đăng nhập, nhập email trường của bạn rồi chọn “Đăng nhập bằng SSO trường học”.",
    link: "Quay lại đăng nhập bằng SSO trường học",
    schoolSSO: "Đăng nhập bằng SSO trường học",
    identifier: "Email hoặc tên đăng nhập",
    password: "Mật khẩu",
    logIn: "Đăng nhập",
    emailMethod: "Đăng nhập bằng mã email",
    emailForm: "Đăng nhập bằng mã gửi tới email",
    email: "Địa chỉ email",
    getCode: "Lấy mã",
    passkey: "Đăng nhập bằng Passkey",
    phoneMethod: "Đăng nhập bằng số điện thoại",
    phoneForm: "Đăng nhập bằng mã gửi tới số điện thoại",
    phoneNumber: "Số điện thoại",
    sendCode: "Gửi mã",
    smsCode: "Mã gồm 6 chữ số",
    verifyPhone: "Xác minh và đăng nhập",
  },
} as const;

function intent(): OAuthLinkIntentResponse {
  return {
    expires_in_seconds: 900,
    masked_email: "t***@school.example",
    provider: "google",
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

const actions = {
  completeNativeSignIn: vi.fn(),
  refresh: vi.fn(),
  resetAfterPasswordRecovery: vi.fn(),
  signIn: vi.fn(),
  signOut: vi.fn(),
};

function setSession(status: SessionStatus) {
  sessionStore.set({ ...actions, currentUser: null, error: null, status });
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
  await act(() => new Promise<void>((resolve) => setTimeout(resolve, 0)));
  await flushMicrotasks();
}

async function renderPage(language: Language = "en") {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  const router = createMemoryRouter(
    [
      { path: "/account-link", element: <AccountLinkPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    { initialEntries: ["/account-link?provider=google"] },
  );
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage={language}>
        <RouterProvider router={router} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await settle();
  return router;
}

function installWebAuthn() {
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {
      isConditionalMediationAvailable: () => Promise.resolve(false),
    }),
  );
  const buffer = (...values: number[]) => new Uint8Array(values).buffer;
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: {
      create: vi.fn(),
      get: vi.fn(() =>
        Promise.resolve({
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
        }),
      ),
    },
  });
}

/** The alert explains the rule and its link goes back to /sign-in. */
async function expectSchoolSSOLink(
  language: Language,
  router: Awaited<ReturnType<typeof renderPage>>,
) {
  const copy = COPY[language];
  const alert = screen.getByRole("alert");
  expect(alert).toHaveTextContent(copy.notice);
  const link = within(alert).getByRole("link", { name: copy.link });
  expect(link).toHaveAttribute("href", "/sign-in");
  // This page has no Home Realm Discovery button to point at.
  expect(
    screen.queryByRole("button", { name: copy.schoolSSO }),
  ).not.toBeInTheDocument();
  expect(actions.completeNativeSignIn).not.toHaveBeenCalled();

  fireEvent.click(link);
  await settle();
  expect(router.state.location.pathname).toBe("/sign-in");
  // The link intent is kept for after school SSO.
  expect(api.cancelLinkIntent).not.toHaveBeenCalled();
}

describe("AccountLinkPage AUTH-13B auth_sso_required", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    for (const mock of Object.values(api)) mock.mockReset();
    for (const mock of Object.values(actions)) mock.mockReset();
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    api.getOAuthLinkIntent.mockResolvedValue(intent());
    api.startPasskeyAssertion.mockResolvedValue(OPTIONS);
    setSession("unauthenticated");
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("password: links back to /sign-in and drops the password", async () => {
    const copy = COPY.en;
    api.loginWithPassword.mockRejectedValue(ssoRequired());
    const router = await renderPage();
    fireEvent.change(screen.getByRole("textbox", { name: copy.identifier }), {
      target: { value: SCHOOL_EMAIL },
    });
    fireEvent.change(screen.getByLabelText(copy.password), {
      target: { value: PASSWORD },
    });
    fireEvent.click(screen.getByRole("button", { name: copy.logIn }));
    await settle();

    expect(screen.getByLabelText(copy.password)).toHaveValue("");
    expect(
      JSON.stringify({ ...localStorage, ...sessionStorage }),
    ).not.toContain(PASSWORD);
    await expectSchoolSSOLink("en", router);
  });

  it("email code (vi): the refusal is an alert with the way back, not a hint", async () => {
    const copy = COPY.vi;
    api.startEmailCodeLogin.mockRejectedValue(ssoRequired());
    const router = await renderPage("vi");
    fireEvent.click(screen.getByRole("button", { name: copy.emailMethod }));
    await settle();
    const form = screen.getByRole("form", { name: copy.emailForm });
    fireEvent.change(within(form).getByRole("textbox", { name: copy.email }), {
      target: { value: SCHOOL_EMAIL },
    });
    fireEvent.click(within(form).getByRole("button", { name: copy.getCode }));
    await settle();

    expect(within(form).getAllByRole("alert")).toHaveLength(1);
    // The /sign-in wording (which names a button) is not used here.
    expect(form).not.toHaveTextContent(
      "Email này phải đăng nhập qua SSO của trường.",
    );
    await expectSchoolSSOLink("vi", router);
  });

  it("passkey: links back to /sign-in", async () => {
    installWebAuthn();
    api.verifyPasskeyAssertion.mockRejectedValue(ssoRequired());
    const router = await renderPage();
    fireEvent.click(screen.getByRole("button", { name: COPY.en.passkey }));
    await settle();
    await expectSchoolSSOLink("en", router);
  });

  it("phone code: leaves the code step and links back to /sign-in", async () => {
    const copy = COPY.en;
    api.startPhoneLogin.mockResolvedValue({
      challenge_id: "phone-challenge-1",
      cooldown_seconds: 60,
      expires_in_seconds: 300,
      masked_destination: "+84 ***5678",
      resend_after_seconds: 60,
    });
    api.verifyPhoneLogin.mockRejectedValue(ssoRequired());
    const router = await renderPage();
    fireEvent.click(screen.getByRole("button", { name: copy.phoneMethod }));
    await settle();
    const form = screen.getByRole("form", { name: copy.phoneForm });
    fireEvent.change(
      within(form).getByRole("textbox", { name: copy.phoneNumber }),
      { target: { value: "0912 345 678" } },
    );
    fireEvent.click(within(form).getByRole("button", { name: copy.sendCode }));
    await settle();
    fireEvent.change(
      within(form).getByRole("textbox", { name: copy.smsCode }),
      { target: { value: "246810" } },
    );
    fireEvent.click(
      within(form).getByRole("button", { name: copy.verifyPhone }),
    );
    await settle();

    expect(
      screen.queryByRole("form", { name: copy.phoneForm }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: copy.identifier }),
    ).toBeInTheDocument();
    await expectSchoolSSOLink("en", router);
  });
});
