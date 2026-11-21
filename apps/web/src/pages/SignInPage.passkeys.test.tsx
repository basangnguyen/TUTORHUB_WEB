import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
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
import { I18nProvider } from "../app/i18n";
import { LoginForm, SignInPage } from "./SignInPage";
import { NO_SOCIAL } from "./SignInPage.shared";

const { api, completeNativeSignInMock, sessionState } = vi.hoisted(() => ({
  api: {
    getAuthProviders: vi.fn(),
    startEmailCodeLogin: vi.fn(),
    startPasskeyAssertion: vi.fn(),
    verifyPasskeyAssertion: vi.fn(),
  },
  completeNativeSignInMock: vi.fn(),
  sessionState: { status: "unauthenticated" as string },
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
    status: sessionState.status,
  }),
}));

const PROVIDERS: AuthProvidersResponse = {
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

const ASSERTION = {
  authenticatorAttachment: "platform",
  clientExtensionResults: {},
  id: "AQID",
  rawId: "AQID",
  response: {
    authenticatorData: "BA",
    clientDataJSON: "BQ",
    signature: "Bg",
    userHandle: "Bw",
  },
  type: "public-key",
};

type GetRequest = CredentialRequestOptions & { mediation?: string };

interface WebAuthnStub {
  conditionalCalls: GetRequest[];
  get: ReturnType<typeof vi.fn>;
  resolveConditional: () => void;
  signalUnknownCredential: ReturnType<typeof vi.fn>;
}

/**
 * A WebAuthn browser. Conditional requests stay pending until aborted (like
 * an unused autofill) or until `resolveConditional()`; modal requests follow
 * `modal` outcomes in order.
 */
function installWebAuthn({
  conditional = true,
  modal = [],
}: {
  conditional?: boolean;
  modal?: Array<() => Promise<unknown>>;
} = {}): WebAuthnStub {
  const signalUnknownCredential = vi.fn(() => Promise.resolve());
  vi.stubGlobal(
    "PublicKeyCredential",
    Object.assign(function PublicKeyCredential() {}, {
      isConditionalMediationAvailable: () => Promise.resolve(conditional),
      signalUnknownCredential,
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
    return (modal.shift() ?? (() => Promise.resolve(CREDENTIAL)))();
  });
  Object.defineProperty(window.navigator, "credentials", {
    configurable: true,
    value: { create: vi.fn(), get },
  });
  return {
    conditionalCalls,
    get,
    resolveConditional: () => resolvePending?.(CREDENTIAL),
    signalUnknownCredential,
  };
}

function LandingProbe() {
  const location = useLocation();
  return <p data-testid="landing">{location.pathname}</p>;
}

async function settle() {
  await act(async () => {
    for (let tick = 0; tick < 10; tick += 1) {
      await Promise.resolve();
    }
  });
}

async function renderSignIn(from = "/app/classes") {
  const router = createMemoryRouter(
    [
      { path: "/sign-in", element: <SignInPage /> },
      { path: "*", element: <LandingProbe /> },
    ],
    {
      initialEntries: [
        { pathname: "/sign-in", state: { from: { pathname: from } } },
      ],
    },
  );
  const view = render(
    <I18nProvider initialLanguage="en">
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await settle();
  return view;
}

function passkeyButton() {
  return screen.getByRole("button", { name: "Sign in with a passkey" });
}

function apiError(
  status: number,
  problem: Record<string, unknown> = {},
  retryAfter?: number,
) {
  return new APIRequestError(
    status,
    { status, title: "Rejected", type: "about:blank", ...problem },
    retryAfter,
  );
}

describe("SignInPage passkeys", () => {
  beforeEach(() => {
    sessionState.status = "unauthenticated";
    api.getAuthProviders.mockReset().mockResolvedValue(PROVIDERS);
    api.startPasskeyAssertion.mockReset().mockResolvedValue(OPTIONS);
    api.verifyPasskeyAssertion
      .mockReset()
      .mockResolvedValue({ status: "authenticated" });
    api.startEmailCodeLogin.mockReset();
    completeNativeSignInMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
    window.localStorage.clear();
  });

  it("stays hidden when the server or the browser cannot do passkeys", async () => {
    installWebAuthn();
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      passkeys_enabled: false,
    });
    await renderSignIn();
    expect(
      screen.queryByRole("button", { name: "Sign in with a passkey" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toHaveAttribute("autocomplete", "username");
    expect(api.startPasskeyAssertion).not.toHaveBeenCalled();
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");

    // passkeys_enabled, but no WebAuthn in this browser.
    api.getAuthProviders.mockResolvedValue(PROVIDERS);
    await renderSignIn();
    expect(
      screen.queryByRole("button", { name: "Sign in with a passkey" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("textbox", { name: "Email or username" }),
    ).toHaveAttribute("autocomplete", "username webauthn");
    expect(api.startPasskeyAssertion).not.toHaveBeenCalled();
  });

  it("signs in with the button as a persistent session and returns to the protected page", async () => {
    const webAuthn = installWebAuthn({ conditional: false });
    await renderSignIn();

    // UI-AUTH-04: no "Keep me signed in" checkbox on /sign-in.
    expect(screen.queryByRole("checkbox")).toBeNull();
    fireEvent.click(passkeyButton());

    expect(await screen.findByTestId("landing")).toHaveTextContent(
      "/app/classes",
    );
    expect(webAuthn.get).toHaveBeenCalledTimes(1);
    expect(webAuthn.get.mock.calls[0]?.[0]).toMatchObject({
      mediation: "optional",
    });
    expect(api.verifyPasskeyAssertion).toHaveBeenCalledWith(
      { assertion: ASSERTION, keep_signed_in: true },
      expect.objectContaining({ baseUrl: "/api" }),
    );
    expect(completeNativeSignInMock).toHaveBeenCalledTimes(1);
    expect(webAuthn.signalUnknownCredential).not.toHaveBeenCalled();
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("places the passkey text button under the providers and above the consent line", async () => {
    installWebAuthn({ conditional: false });
    api.getAuthProviders.mockResolvedValue({
      ...PROVIDERS,
      oauth_providers: ["google", "microsoft"],
    });
    await renderSignIn();

    const button = passkeyButton();
    // A quiet text action with its key icon, not an outlined button.
    expect(button).toHaveClass("sign-in-form__text-action");
    expect(button.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
    const order = [
      screen.getByRole("button", { name: "Sign in with Google" }),
      screen.getByRole("button", { name: "Sign in with Microsoft" }),
      button,
      screen.getByText(/^By signing in, you agree to the/),
    ];
    for (let index = 1; index < order.length; index += 1) {
      expect(
        order[index - 1]?.compareDocumentPosition(order[index] as Node),
      ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    }
  });

  it("shows a gentle notice (never an alert) when the prompt is closed", async () => {
    const webAuthn = installWebAuthn({
      conditional: false,
      modal: [
        () => Promise.reject(new DOMException("closed", "NotAllowedError")),
      ],
    });
    await renderSignIn();

    passkeyButton().focus();
    fireEvent.click(passkeyButton());
    // aria-disabled while waiting: keyboard focus never leaves the button.
    expect(
      screen.getByRole("button", { name: "Waiting for your passkey…" }),
    ).toHaveAttribute("aria-disabled", "true");

    expect(
      await screen.findByText("Passkey sign-in was cancelled."),
    ).toBeInTheDocument();
    expect(passkeyButton()).toHaveFocus();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(api.verifyPasskeyAssertion).not.toHaveBeenCalled();
    expect(webAuthn.signalUnknownCredential).not.toHaveBeenCalled();
    expect(passkeyButton()).toBeEnabled();
  });

  it("signals the authenticator only for a confirmed unknown credential", async () => {
    const webAuthn = installWebAuthn({ conditional: false });
    api.verifyPasskeyAssertion
      .mockRejectedValueOnce(
        apiError(401, {
          code: "auth_invalid_passkey",
          unknown_credential: true,
        }),
      )
      .mockRejectedValueOnce(apiError(401, { code: "auth_invalid_passkey" }));
    await renderSignIn();

    fireEvent.click(passkeyButton());
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /couldn't verify this passkey/,
    );
    expect(webAuthn.signalUnknownCredential).toHaveBeenCalledWith({
      credentialId: "AQID",
      rpId: "localhost",
    });

    fireEvent.click(passkeyButton());
    await waitFor(() =>
      expect(api.verifyPasskeyAssertion).toHaveBeenCalledTimes(2),
    );
    // A revoked (known) credential is never signalled.
    expect(webAuthn.signalUnknownCredential).toHaveBeenCalledTimes(1);
    expect(completeNativeSignInMock).not.toHaveBeenCalled();
  });

  it("maps clone, rate-limit, busy, disabled and network failures", async () => {
    installWebAuthn({ conditional: false });
    await renderSignIn();
    const cases: Array<[unknown, RegExp]> = [
      [
        apiError(403, { code: "auth_passkey_clone_detected" }),
        /This passkey can't be accepted/,
      ],
      [apiError(404), /Passkey sign-in isn't available right now/],
      [new TypeError("Failed to fetch"), /Couldn't reach TutorHub/],
      // Last: Retry-After locks (and relabels) the button.
      [apiError(503, {}, 7), /busy right now\. Please try again in 7 seconds/],
    ];
    for (const [error, text] of cases) {
      api.verifyPasskeyAssertion.mockRejectedValueOnce(error);
      fireEvent.click(passkeyButton());
      await waitFor(() =>
        expect(screen.getByRole("alert")).toHaveTextContent(text),
      );
    }
  });

  it("locks the button for Retry-After when the options are rate limited", async () => {
    installWebAuthn({ conditional: false });
    api.startPasskeyAssertion.mockRejectedValueOnce(apiError(429, {}, 30));
    await renderSignIn();

    fireEvent.click(passkeyButton());

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Too many attempts. Please try again in 30 seconds.",
    );
    expect(
      screen.getByRole("button", { name: "Sign in with a passkey (30s)" }),
    ).toBeDisabled();
  });

  it("aborts the background autofill request before opening the modal prompt", async () => {
    const webAuthn = installWebAuthn();
    await renderSignIn();

    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(1));
    const conditional = webAuthn.conditionalCalls[0];
    expect(api.startPasskeyAssertion).toHaveBeenCalledWith(
      expect.objectContaining({ signal: conditional?.signal }),
    );
    expect(conditional?.signal?.aborted).toBe(false);

    fireEvent.click(passkeyButton());

    expect(await screen.findByTestId("landing")).toHaveTextContent(
      "/app/classes",
    );
    expect(conditional?.signal?.aborted).toBe(true);
    const modalCall = webAuthn.get.mock.calls.findIndex(
      ([request]) => (request as GetRequest).mediation === "optional",
    );
    expect(modalCall).toBeGreaterThan(0);
    // The aborted autofill never surfaced an error.
    expect(api.verifyPasskeyAssertion).toHaveBeenCalledTimes(1);
  });

  it("signs in when the user picks a passkey from autofill", async () => {
    const webAuthn = installWebAuthn();
    await renderSignIn();
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(1));

    await act(async () => {
      webAuthn.resolveConditional();
    });

    expect(await screen.findByTestId("landing")).toHaveTextContent(
      "/app/classes",
    );
    // UI-AUTH-04: autofill also asks for a persistent session.
    expect(api.verifyPasskeyAssertion).toHaveBeenCalledWith(
      { assertion: ASSERTION, keep_signed_in: true },
      expect.anything(),
    );
  });

  it("does not re-arm autofill after a picked passkey was rejected", async () => {
    const webAuthn = installWebAuthn();
    api.verifyPasskeyAssertion.mockRejectedValueOnce(
      apiError(401, { code: "auth_invalid_passkey" }),
    );
    await renderSignIn();
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(1));

    await act(async () => {
      webAuthn.resolveConditional();
    });

    expect(await screen.findByRole("alert")).toHaveTextContent(
      /couldn't verify this passkey/,
    );
    await settle();
    expect(webAuthn.conditionalCalls).toHaveLength(1);
    expect(api.startPasskeyAssertion).toHaveBeenCalledTimes(1);
  });

  it("re-arms autofill after a cancelled modal and keeps background failures silent", async () => {
    api.startPasskeyAssertion.mockRejectedValueOnce(apiError(503, {}, 5));
    const webAuthn = installWebAuthn({
      modal: [
        () => Promise.reject(new DOMException("closed", "NotAllowedError")),
      ],
    });
    await renderSignIn();
    await settle();
    // The background options request failed: nothing to show.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(webAuthn.conditionalCalls).toHaveLength(0);

    fireEvent.click(passkeyButton());
    expect(
      await screen.findByText("Passkey sign-in was cancelled."),
    ).toBeInTheDocument();
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(1));
  });

  it("cancels autofill when leaving the password form or the page", async () => {
    const webAuthn = installWebAuthn();
    const view = await renderSignIn();
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(1));
    const first = webAuthn.conditionalCalls[0];

    fireEvent.click(screen.getByRole("button", { name: "Log in with OTP" }));
    await waitFor(() => expect(first?.signal?.aborted).toBe(true));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "Log in with password" }),
    );
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(2));
    const second = webAuthn.conditionalCalls[1];

    fireEvent.click(screen.getByRole("button", { name: "Forgot password" }));
    await waitFor(() => expect(second?.signal?.aborted).toBe(true));
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    });
    await waitFor(() => expect(webAuthn.conditionalCalls).toHaveLength(3));
    const third = webAuthn.conditionalCalls[2];

    fireEvent.click(screen.getByRole("button", { name: "Sign Up" }));
    await waitFor(() => expect(third?.signal?.aborted).toBe(true));
    view.unmount();
  });

  it("starts no autofill for a visitor who is already signed in", async () => {
    sessionState.status = "authenticated";
    const webAuthn = installWebAuthn();
    await renderSignIn();
    await settle();
    expect(passkeyButton()).toBeInTheDocument();
    expect(webAuthn.conditionalCalls).toHaveLength(0);
    expect(api.startPasskeyAssertion).not.toHaveBeenCalled();
  });
});

describe("LoginForm passkeys on /account-link", () => {
  beforeEach(() => {
    api.startPasskeyAssertion.mockReset().mockResolvedValue(OPTIONS);
    api.verifyPasskeyAssertion
      .mockReset()
      .mockResolvedValue({ status: "authenticated" });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    Reflect.deleteProperty(window.navigator, "credentials");
  });

  it("hands the session to the caller without navigating and retries a failed hand-off", async () => {
    installWebAuthn({ conditional: false });
    const onAuthenticated = vi
      .fn()
      .mockRejectedValueOnce(new Error("csrf"))
      .mockResolvedValueOnce(undefined);
    render(
      <I18nProvider initialLanguage="en">
        <LoginForm
          autoFocus={false}
          emailCodeEnabled
          mode="password"
          onAuthenticated={onAuthenticated}
          onPasswordReset={vi.fn()}
          onSignIn={vi.fn()}
          passkeysEnabled
          passwordMinLength={8}
          recoveryEnabled
          social={NO_SOCIAL}
        />
      </I18nProvider>,
    );

    fireEvent.click(passkeyButton());
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You're signed in, but we couldn't load your account yet.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(onAuthenticated).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(screen.queryByRole("alert")).not.toBeInTheDocument(),
    );
    expect(api.verifyPasskeyAssertion).toHaveBeenCalledTimes(1);
  });
});
