import {
  APIRequestError,
  cancelPendingRegistration,
  completePendingRegistration,
  getPendingRegistration,
  resendPendingRegistration,
  startPendingRegistration,
  type AuthSignupChallengeResponse,
  type PendingRegistrationPreview,
  type PendingRegistrationResult,
} from "@tutorhub/api-client";
import { Button, ErrorState, Skeleton, SkeletonGroup } from "@tutorhub/ui";
import { RefreshCw, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
} from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import { SignInLegalDialog } from "./SignInLegalDialog";
import {
  ACCOUNT_LINK_PATH,
  getApiBaseUrl,
  registrationProviderFrom,
  sanitizeRegistrationReturnTo,
  useAuthProviders,
  type RegistrationProvider,
} from "./SignInPage.shared";
import type { SignInLegalTab } from "./signInLegalContent";
import "./SignInPage.css";
import "./AccountLinkPage.css";
import "./CompleteRegistrationPage.css";

/** Upper bound for client-side Retry-After locks; the server stays authoritative. */
const MAX_RETRY_AFTER_SECONDS = 3600;
const EMAIL_MAX_LENGTH = 254;
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const OTP_PATTERN = /^\d{6}$/;

const REGISTRATION_UNAVAILABLE_CODE = "auth_registration_unavailable";
const SIGNUP_DISABLED_CODE = "auth_signup_disabled";
const INVALID_CODE_CODE = "auth_invalid_verification_code";
const TERMS_CHANGED_CODE = "auth_terms_version_changed";

type Translate = (
  key: TranslationKey,
  values?: Record<string, string | number>,
) => string;

function boundedSeconds(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), MAX_RETRY_AFTER_SECONDS)
    : undefined;
}

/**
 * GET answers 404 when the registration (or the Facebook provider) is gone;
 * POSTs answer 400 `auth_registration_unavailable`.
 */
function isRegistrationUnavailable(error: unknown) {
  return (
    error instanceof APIRequestError &&
    (error.status === 404 ||
      (error.status === 400 &&
        error.problem?.code === REGISTRATION_UNAVAILABLE_CODE))
  );
}

type PreviewState =
  | { status: "loading" }
  | { status: "unavailable" }
  | { status: "error" }
  | {
      status: "ready";
      preview: PendingRegistrationPreview;
      /** Wall-clock deadline: throttled background timers cannot drift it. */
      expiresAt: number;
      remaining: number;
    };

/**
 * The masked hint stays in component state (never the shared query cache),
 * so a native sign-in that clears the cache does not blank this screen.
 */
function usePendingRegistrationPreview() {
  const [state, setState] = useState<PreviewState>({ status: "loading" });
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    getPendingRegistration({
      baseUrl: getApiBaseUrl(),
      signal: controller.signal,
    }).then(
      (preview) => {
        if (controller.signal.aborted) return;
        const remaining = Math.max(0, Math.ceil(preview.expires_in_seconds));
        setState(
          remaining > 0
            ? {
                status: "ready",
                preview,
                expiresAt: Date.now() + remaining * 1000,
                remaining,
              }
            : { status: "unavailable" },
        );
      },
      (error: unknown) => {
        if (controller.signal.aborted) return;
        setState(
          isRegistrationUnavailable(error)
            ? { status: "unavailable" }
            : { status: "error" },
        );
      },
    );
    return () => controller.abort();
  }, [generation]);

  const expiresAt = state.status === "ready" ? state.expiresAt : null;
  useEffect(() => {
    if (expiresAt === null) return;
    const timer = window.setInterval(() => {
      const remaining = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000));
      setState((current) => {
        if (current.status !== "ready" || current.expiresAt !== expiresAt) {
          return current;
        }
        return remaining > 0
          ? { ...current, remaining }
          : { status: "unavailable" };
      });
    }, 1000);
    return () => window.clearInterval(timer);
  }, [expiresAt]);

  const reload = useCallback(() => {
    setState({ status: "loading" });
    setGeneration((value) => value + 1);
  }, []);
  const markUnavailable = useCallback(() => {
    setState({ status: "unavailable" });
  }, []);

  return { markUnavailable, reload, state };
}

function useCountdown() {
  const [remaining, setRemaining] = useState(0);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(() => {
      setRemaining((value) => Math.max(0, value - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  const start = useCallback((seconds: number | undefined) => {
    setRemaining(boundedSeconds(seconds) ?? 0);
  }, []);
  const reset = useCallback(() => setRemaining(0), []);
  return { remaining, reset, start };
}

function formatRemaining(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return {
    label: `${minutes}:${String(rest).padStart(2, "0")}`,
    duration: `PT${minutes}M${rest}S`,
  };
}

type Failure =
  | { kind: "signupDisabled" }
  | { kind: "signupUnavailable" }
  | { kind: "conflict" }
  | { kind: "rateLimited"; seconds?: number }
  | { kind: "busy"; seconds?: number }
  | { kind: "network" }
  | { kind: "session" }
  | { kind: "generic" };

function failureFrom(error: unknown): Failure {
  if (!(error instanceof APIRequestError)) return { kind: "network" };
  switch (error.status) {
    case 400:
      return { kind: "signupUnavailable" };
    case 403:
      return error.problem?.code === SIGNUP_DISABLED_CODE
        ? { kind: "signupDisabled" }
        : { kind: "generic" };
    case 409:
      return { kind: "conflict" };
    case 429:
      return {
        kind: "rateLimited",
        seconds: boundedSeconds(error.retryAfterSeconds),
      };
    case 503:
      return { kind: "busy", seconds: boundedSeconds(error.retryAfterSeconds) };
    default:
      return { kind: "generic" };
  }
}

function retryAfterOf(failure: Failure) {
  return failure.kind === "rateLimited" || failure.kind === "busy"
    ? failure.seconds
    : undefined;
}

function failureText(failure: Failure, t: Translate) {
  switch (failure.kind) {
    case "signupDisabled":
      return t("completeRegistration.signupDisabled");
    case "signupUnavailable":
      return t("completeRegistration.signupUnavailable");
    case "conflict":
      return t("completeRegistration.conflict");
    case "rateLimited":
      return failure.seconds
        ? t("completeRegistration.rateLimitedWait", {
            seconds: failure.seconds,
          })
        : t("completeRegistration.rateLimited");
    case "busy":
      return t("completeRegistration.busy");
    case "network":
      return t("completeRegistration.networkError");
    case "session":
      return t("completeRegistration.sessionError");
    default:
      return t("completeRegistration.actionError");
  }
}

interface Challenge {
  email: string;
  expiresInSeconds: number;
  id: string;
  minutes: number;
}

type OtpError = "required" | "invalid" | "expired" | null;
type TermsError = "required" | "changed" | "unavailable" | null;

const PROVIDER_NAME_KEYS: Readonly<
  Record<RegistrationProvider, TranslationKey>
> = {
  facebook: "completeRegistration.providerFacebook",
  microsoft: "completeRegistration.providerMicrosoft",
};

/**
 * AUTH-06B/07B: a new Facebook subject, or a Microsoft subject without a
 * trusted email (`xms_edov`), has no account yet. The HttpOnly
 * registration cookie never reaches JavaScript; the person proves an email
 * with a one-time code, after which the server either creates the account
 * (session) or parks a link intent when the email already has an owner.
 * Every notice before a correct code is account-neutral.
 */
export function CompleteRegistrationPage() {
  const { t } = useI18n();
  const session = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const ids = {
    title: useId(),
    emailHeading: useId(),
    email: useId(),
    emailHelp: useId(),
    emailError: useId(),
    codeHeading: useId(),
    notice: useId(),
    otp: useId(),
    otpHelp: useId(),
    otpError: useId(),
    terms: useId(),
    termsText: useId(),
    termsError: useId(),
    keepSignedInHint: useId(),
  };
  const { markUnavailable, reload, state } = usePendingRegistrationPreview();
  const { reload: reloadProviders, state: providersState } = useAuthProviders();
  const providers =
    providersState.status === "ready" ? providersState.providers : null;
  // The server's preview names the provider; until it loads, the allowlisted
  // `?provider=` from the callback redirect labels the screen.
  const provider: RegistrationProvider =
    state.status === "ready"
      ? state.preview.provider
      : registrationProviderFrom(
          new URLSearchParams(location.search).get("provider"),
        );
  const providerName = t(PROVIDER_NAME_KEYS[provider]);

  const emailRef = useRef<HTMLInputElement>(null);
  const otpRef = useRef<HTMLInputElement>(null);
  const termsRef = useRef<HTMLInputElement>(null);
  const codeHeadingRef = useRef<HTMLHeadingElement>(null);
  const legalReturnFocusRef = useRef<HTMLElement | null>(null);
  /** Bumped whenever the email changes so a late response is discarded. */
  const generationRef = useRef(0);
  const focusCodeRef = useRef(false);
  /** Where to go once a session exists (kept for the hand-off retry). */
  const authenticatedReturnRef = useRef<string | null>(null);

  const [email, setEmail] = useState("");
  const [emailInvalid, setEmailInvalid] = useState(false);
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [codeExpired, setCodeExpired] = useState(false);
  const [requesting, setRequesting] = useState(false);
  const [sendFailure, setSendFailure] = useState<Failure | null>(null);
  const [otp, setOtp] = useState("");
  const [otpError, setOtpError] = useState<OtpError>(null);
  const [acceptedTerms, setAcceptedTerms] = useState(false);
  const [termsError, setTermsError] = useState<TermsError>(null);
  const [keepSignedIn, setKeepSignedIn] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitFailure, setSubmitFailure] = useState<Failure | null>(null);
  const [cancelling, setCancelling] = useState(false);
  const [cancelFailure, setCancelFailure] = useState<Failure | null>(null);
  const [legalOpen, setLegalOpen] = useState(false);
  const [legalTab, setLegalTab] = useState<SignInLegalTab>("agreement");
  const cooldown = useCountdown();
  const submitLock = useCountdown();
  const { reset: resetCooldown, start: startCooldown } = cooldown;

  // Remind the person to resend once the server-side code lifetime passed.
  useEffect(() => {
    if (!challenge) return;
    const timer = window.setTimeout(
      () => setCodeExpired(true),
      Math.min(challenge.expiresInSeconds, MAX_RETRY_AFTER_SECONDS) * 1000,
    );
    return () => window.clearTimeout(timer);
  }, [challenge]);

  // Move focus to the code step once it appears.
  useEffect(() => {
    if (challenge && focusCodeRef.current) {
      focusCodeRef.current = false;
      codeHeadingRef.current?.focus();
    }
  }, [challenge]);

  /** A code belongs to one email: editing the address drops the old one. */
  const invalidateChallenge = useCallback(() => {
    generationRef.current += 1;
    setChallenge(null);
    setCodeExpired(false);
    setRequesting(false);
    setSendFailure(null);
    setSubmitFailure(null);
    setOtp("");
    setOtpError(null);
    resetCooldown();
  }, [resetCooldown]);

  function handleFailure(
    error: unknown,
    onFailure: (failure: Failure) => void,
  ) {
    if (isRegistrationUnavailable(error)) {
      markUnavailable();
      return;
    }
    onFailure(failureFrom(error));
  }

  async function issueCode(resend: boolean) {
    const trimmed = email.trim();
    const generation = ++generationRef.current;
    const isStale = () => generation !== generationRef.current;
    const options = { baseUrl: getApiBaseUrl() };
    setRequesting(true);
    setSendFailure(null);
    let result: AuthSignupChallengeResponse;
    try {
      result = resend
        ? await resendPendingRegistration(options)
        : await startPendingRegistration({ email: trimmed }, options);
    } catch (error) {
      if (isStale()) return;
      setRequesting(false);
      if (
        !resend &&
        error instanceof APIRequestError &&
        error.status === 400 &&
        error.problem?.code !== REGISTRATION_UNAVAILABLE_CODE
      ) {
        setEmailInvalid(true);
        emailRef.current?.focus();
        return;
      }
      handleFailure(error, (failure) => {
        startCooldown(retryAfterOf(failure));
        setSendFailure(failure);
      });
      return;
    }
    if (isStale()) return;
    setRequesting(false);
    setCodeExpired(false);
    setOtpError(null);
    startCooldown(result.cooldown_seconds);
    // Uniform wording: never reveal whether the address has an account.
    focusCodeRef.current = !resend;
    setChallenge({
      email: trimmed,
      expiresInSeconds: result.expires_in_seconds,
      id: result.challenge_id,
      minutes: Math.max(1, Math.ceil(result.expires_in_seconds / 60)),
    });
  }

  function handleSendCode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmed = email.trim();
    if (!EMAIL_PATTERN.test(trimmed) || trimmed.length > EMAIL_MAX_LENGTH) {
      setEmailInvalid(true);
      emailRef.current?.focus();
      return;
    }
    if (requesting || cooldown.remaining > 0) return;
    void issueCode(false);
  }

  function handleResend() {
    if (!challenge || requesting || cooldown.remaining > 0) return;
    void issueCode(true);
  }

  function handleChangeEmail() {
    invalidateChallenge();
    // Wait for the email step to render before moving focus back.
    window.setTimeout(() => emailRef.current?.focus(), 0);
  }

  async function finishSession() {
    const returnTo = authenticatedReturnRef.current;
    if (!returnTo) return;
    setSubmitFailure(null);
    setSubmitting(true);
    try {
      await session.completeNativeSignIn();
    } catch {
      // The account and session exist; retry only the CSRF + /me hand-off.
      setSubmitFailure({ kind: "session" });
      setSubmitting(false);
      return;
    }
    void navigate(returnTo, { replace: true });
  }

  function validateCompletion(): boolean {
    let nextOtpError: OtpError = null;
    if (codeExpired) nextOtpError = "expired";
    else if (!OTP_PATTERN.test(otp.trim())) nextOtpError = "required";
    let nextTermsError: TermsError = null;
    if (!acceptedTerms) nextTermsError = "required";
    else if (!providers?.legal_terms_version) nextTermsError = "unavailable";
    setOtpError(nextOtpError);
    setTermsError(nextTermsError);
    if (nextOtpError) {
      otpRef.current?.focus();
      return false;
    }
    if (nextTermsError) {
      termsRef.current?.focus();
      return false;
    }
    return true;
  }

  async function complete(current: Challenge, termsVersion: string) {
    const generation = generationRef.current;
    const isStale = () => generation !== generationRef.current;
    setSubmitFailure(null);
    setSubmitting(true);
    let result: PendingRegistrationResult;
    try {
      result = await completePendingRegistration(
        {
          challenge_id: current.id,
          code: otp.trim(),
          keep_signed_in: keepSignedIn,
          terms_version: termsVersion,
        },
        { baseUrl: getApiBaseUrl() },
      );
    } catch (error) {
      if (isStale()) return;
      setSubmitting(false);
      if (error instanceof APIRequestError) {
        if (
          error.status === 401 &&
          (error.problem?.code === INVALID_CODE_CODE || !error.problem?.code)
        ) {
          // One message for a wrong or expired code.
          setOtp("");
          setOtpError("invalid");
          otpRef.current?.focus();
          return;
        }
        if (
          error.status === 400 &&
          error.problem?.code === TERMS_CHANGED_CODE
        ) {
          setAcceptedTerms(false);
          setTermsError("changed");
          reloadProviders();
          termsRef.current?.focus();
          return;
        }
      }
      handleFailure(error, (failure) => {
        submitLock.start(retryAfterOf(failure));
        setSubmitFailure(failure);
      });
      return;
    }
    if (isStale()) return;
    setOtp("");
    if (result.status === "link_pending") {
      // The email already has an owner: prove it on the linking screen.
      void navigate(`${ACCOUNT_LINK_PATH}?provider=${provider}`, {
        replace: true,
      });
      return;
    }
    authenticatedReturnRef.current = sanitizeRegistrationReturnTo(
      result.return_to,
    );
    await finishSession();
  }

  function handleComplete(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!challenge || submitting || submitLock.remaining > 0) return;
    if (!validateCompletion()) return;
    const termsVersion = providers?.legal_terms_version;
    if (termsVersion) void complete(challenge, termsVersion);
  }

  async function handleCancel() {
    if (cancelling) return;
    generationRef.current += 1;
    setCancelFailure(null);
    setCancelling(true);
    try {
      await cancelPendingRegistration({ baseUrl: getApiBaseUrl() });
    } catch (error) {
      if (!isRegistrationUnavailable(error)) {
        setCancelFailure(failureFrom(error));
        setCancelling(false);
        return;
      }
      // Already gone: the server cleared the stale cookie as well.
    }
    void navigate("/sign-in", { replace: true });
  }

  function openLegal(tab: SignInLegalTab, event: MouseEvent<HTMLElement>) {
    legalReturnFocusRef.current = event.currentTarget;
    setLegalTab(tab);
    setLegalOpen(true);
  }

  function acceptLegal() {
    setAcceptedTerms(true);
    setTermsError(null);
    legalReturnFocusRef.current = termsRef.current;
  }

  const ready = state.status === "ready" ? state : null;
  const remaining = ready ? formatRemaining(ready.remaining) : null;
  const otpErrorText =
    otpError === "invalid"
      ? t("completeRegistration.codeInvalid")
      : otpError === "expired"
        ? t("completeRegistration.codeExpired")
        : otpError === "required"
          ? t("completeRegistration.errorOtp")
          : null;
  const termsErrorText =
    termsError === "changed"
      ? t("completeRegistration.termsChanged")
      : termsError === "unavailable"
        ? t("completeRegistration.termsUnavailable")
        : termsError === "required"
          ? t("completeRegistration.errorTerms")
          : null;
  const resendLabel = requesting
    ? t("completeRegistration.sendingCode")
    : cooldown.remaining > 0
      ? t("completeRegistration.resendWait", { seconds: cooldown.remaining })
      : t("completeRegistration.resend");
  // Before any code exists, a lock comes from Retry-After: "try again".
  const sendLabel = requesting
    ? t("completeRegistration.sendingCode")
    : cooldown.remaining > 0
      ? t("completeRegistration.submitWait", { seconds: cooldown.remaining })
      : t("completeRegistration.sendCode");
  const submitLabel =
    submitLock.remaining > 0
      ? t("completeRegistration.submitWait", { seconds: submitLock.remaining })
      : t("completeRegistration.submit");
  const describedBy = (...parts: (string | false | null | undefined)[]) =>
    parts.filter(Boolean).join(" ") || undefined;

  return (
    <main className="account-link-page complete-registration-page">
      <section
        aria-busy={state.status === "loading" || undefined}
        aria-labelledby={ids.title}
        className="account-link-card"
      >
        <p className="account-link-card__kicker">
          {t("completeRegistration.kicker")}
        </p>
        <h1 id={ids.title}>
          {t("completeRegistration.title", { provider: providerName })}
        </h1>

        {state.status === "loading" && (
          <SkeletonGroup label={t("completeRegistration.loading")}>
            <Skeleton height={22} width="72%" />
            <Skeleton height={48} />
            <Skeleton height={42} />
          </SkeletonGroup>
        )}

        {state.status === "unavailable" && (
          <ErrorState
            actions={
              <Link className="account-link-card__continue" to="/sign-in">
                {t("completeRegistration.backToSignIn")}
              </Link>
            }
            description={t("completeRegistration.unavailableDescription", {
              provider: providerName,
            })}
            title={t("completeRegistration.unavailableTitle")}
          />
        )}

        {state.status === "error" && (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={reload}
                variant="secondary"
              >
                {t("completeRegistration.retry")}
              </Button>
            }
            description={t("completeRegistration.loadErrorDescription")}
            title={t("completeRegistration.loadErrorTitle")}
          />
        )}

        {ready && remaining && (
          <div className="account-link-card__content">
            <p className="account-link-card__lead">
              {t("completeRegistration.lead", { provider: providerName })}
            </p>
            {ready.preview.masked_email ? (
              <p className="complete-registration__hint">
                {t("completeRegistration.hint", {
                  email: ready.preview.masked_email,
                  provider: providerName,
                })}
              </p>
            ) : null}
            <p className="account-link-card__expiry">
              <ExpiryLine
                duration={remaining.duration}
                label={remaining.label}
                template={t("completeRegistration.expiresIn", {
                  time: "{time}",
                })}
              />
            </p>
            {providers && providers.signup_enabled === false ? (
              <p className="complete-registration__note">
                {t("completeRegistration.signupDisabledNotice")}
              </p>
            ) : null}

            {!challenge ? (
              <form
                aria-labelledby={ids.emailHeading}
                className="account-link-card__step complete-registration__form"
                noValidate
                onSubmit={handleSendCode}
              >
                <h2 id={ids.emailHeading}>
                  {t("completeRegistration.emailHeading")}
                </h2>
                <div className="complete-registration__field">
                  <label htmlFor={ids.email}>
                    {t("completeRegistration.emailLabel")}
                  </label>
                  <input
                    aria-describedby={describedBy(
                      ids.emailHelp,
                      emailInvalid && ids.emailError,
                    )}
                    aria-invalid={emailInvalid || undefined}
                    autoComplete="email"
                    className="sign-in-input"
                    id={ids.email}
                    inputMode="email"
                    maxLength={EMAIL_MAX_LENGTH}
                    name="email"
                    onChange={(event) => {
                      setEmail(event.target.value);
                      setEmailInvalid(false);
                      // A late answer for the previous address is discarded.
                      if (requesting) invalidateChallenge();
                    }}
                    ref={emailRef}
                    spellCheck={false}
                    type="email"
                    value={email}
                  />
                  <p className="complete-registration__help" id={ids.emailHelp}>
                    {t("completeRegistration.emailHelp")}
                  </p>
                  {emailInvalid ? (
                    <p className="sign-in-form__error" id={ids.emailError}>
                      {t("completeRegistration.errorEmail")}
                    </p>
                  ) : null}
                </div>
                {sendFailure ? (
                  <div className="account-link-card__alert" role="alert">
                    <p>{failureText(sendFailure, t)}</p>
                  </div>
                ) : null}
                <div className="account-link-card__actions">
                  <Button
                    disabled={cooldown.remaining > 0}
                    loading={requesting}
                    loadingLabel={t("completeRegistration.sendingCode")}
                    type="submit"
                  >
                    {sendLabel}
                  </Button>
                </div>
              </form>
            ) : (
              <form
                aria-labelledby={ids.codeHeading}
                className="account-link-card__step complete-registration__form"
                noValidate
                onSubmit={handleComplete}
              >
                <h2 id={ids.codeHeading} ref={codeHeadingRef} tabIndex={-1}>
                  {t("completeRegistration.codeHeading")}
                </h2>
                <div aria-live="polite">
                  <p
                    className={
                      codeExpired
                        ? "complete-registration__notice complete-registration__notice--error"
                        : "complete-registration__notice"
                    }
                    id={ids.notice}
                  >
                    {codeExpired
                      ? t("completeRegistration.codeExpired")
                      : t("completeRegistration.codeSent", {
                          email: challenge.email,
                          minutes: challenge.minutes,
                        })}
                  </p>
                </div>
                {sendFailure ? (
                  <div className="account-link-card__alert" role="alert">
                    <p>{failureText(sendFailure, t)}</p>
                  </div>
                ) : null}
                <div className="complete-registration__field">
                  <label htmlFor={ids.otp}>
                    {t("completeRegistration.otpLabel")}
                  </label>
                  <input
                    aria-describedby={describedBy(
                      ids.otpHelp,
                      otpErrorText && ids.otpError,
                    )}
                    aria-invalid={otpErrorText ? true : undefined}
                    autoComplete="one-time-code"
                    className="sign-in-input sign-in-input--otp"
                    id={ids.otp}
                    inputMode="numeric"
                    maxLength={6}
                    name="one-time-code"
                    onChange={(event) => {
                      setOtp(event.target.value.replace(/\D/g, "").slice(0, 6));
                      setOtpError(null);
                    }}
                    pattern="[0-9]{6}"
                    ref={otpRef}
                    type="text"
                    value={otp}
                  />
                  <p className="complete-registration__help" id={ids.otpHelp}>
                    {t("completeRegistration.otpHelp")}
                  </p>
                  {otpErrorText ? (
                    <p className="sign-in-form__error" id={ids.otpError}>
                      {otpErrorText}
                    </p>
                  ) : null}
                </div>
                <div className="complete-registration__inline-actions">
                  <Button
                    disabled={cooldown.remaining > 0}
                    loading={requesting}
                    loadingLabel={t("completeRegistration.sendingCode")}
                    onClick={handleResend}
                    size="sm"
                    variant="secondary"
                  >
                    {resendLabel}
                  </Button>
                  <Button
                    disabled={submitting}
                    onClick={handleChangeEmail}
                    size="sm"
                    variant="quiet"
                  >
                    {t("completeRegistration.changeEmail")}
                  </Button>
                </div>

                <div className="complete-registration__field">
                  <div className="sign-in-check sign-in-check--terms">
                    <input
                      aria-describedby={
                        termsErrorText ? ids.termsError : undefined
                      }
                      aria-invalid={termsErrorText ? true : undefined}
                      aria-labelledby={ids.termsText}
                      checked={acceptedTerms}
                      id={ids.terms}
                      name="terms"
                      onChange={(event) => {
                        setAcceptedTerms(event.target.checked);
                        setTermsError(null);
                      }}
                      ref={termsRef}
                      type="checkbox"
                    />
                    <p className="sign-in-check__text" id={ids.termsText}>
                      <label htmlFor={ids.terms}>
                        {t("completeRegistration.termsLead")}
                      </label>
                      <button
                        aria-haspopup="dialog"
                        className="sign-in-check__accent sign-in-check__link"
                        onClick={(event) => openLegal("agreement", event)}
                        type="button"
                      >
                        {t("completeRegistration.userAgreement")}
                      </button>
                      {t("completeRegistration.termsJoin")}
                      <button
                        aria-haspopup="dialog"
                        className="sign-in-check__accent sign-in-check__link"
                        onClick={(event) => openLegal("privacy", event)}
                        type="button"
                      >
                        {t("completeRegistration.privacyPolicy")}
                      </button>
                    </p>
                  </div>
                  {termsErrorText ? (
                    <p className="sign-in-form__error" id={ids.termsError}>
                      {termsErrorText}
                    </p>
                  ) : null}
                  {termsError === "unavailable" ? (
                    <Button
                      leadingIcon={<RefreshCw />}
                      onClick={reloadProviders}
                      size="sm"
                      variant="secondary"
                    >
                      {t("completeRegistration.retry")}
                    </Button>
                  ) : null}
                </div>

                <label
                  className="sign-in-check"
                  title={t("completeRegistration.keepSignedInHint")}
                >
                  <input
                    aria-describedby={ids.keepSignedInHint}
                    checked={keepSignedIn}
                    name="keep-signed-in"
                    onChange={(event) => setKeepSignedIn(event.target.checked)}
                    type="checkbox"
                  />
                  <span>{t("completeRegistration.keepSignedIn")}</span>
                </label>
                <span className="visually-hidden" id={ids.keepSignedInHint}>
                  {t("completeRegistration.keepSignedInHint")}
                </span>

                {submitFailure ? (
                  <div className="account-link-card__alert" role="alert">
                    <p>{failureText(submitFailure, t)}</p>
                    {submitFailure.kind === "session" ? (
                      <Button
                        leadingIcon={<RefreshCw />}
                        onClick={() => void finishSession()}
                        size="sm"
                        variant="secondary"
                      >
                        {t("completeRegistration.retry")}
                      </Button>
                    ) : null}
                  </div>
                ) : null}
                <div className="account-link-card__actions">
                  <Button
                    disabled={submitLock.remaining > 0}
                    loading={submitting}
                    loadingLabel={t("completeRegistration.submitting")}
                    type="submit"
                  >
                    {submitLabel}
                  </Button>
                </div>
              </form>
            )}

            {cancelFailure ? (
              <div className="account-link-card__alert" role="alert">
                <p>{failureText(cancelFailure, t)}</p>
              </div>
            ) : null}
            <div className="account-link-card__actions account-link-card__actions--secondary">
              <Button
                disabled={submitting}
                leadingIcon={<X />}
                loading={cancelling}
                loadingLabel={t("completeRegistration.cancelling")}
                onClick={() => void handleCancel()}
                variant="quiet"
              >
                {t("completeRegistration.cancel")}
              </Button>
            </div>
          </div>
        )}
      </section>

      <SignInLegalDialog
        activeTab={legalTab}
        onAccept={acceptLegal}
        onOpenChange={setLegalOpen}
        onTabChange={setLegalTab}
        open={legalOpen}
        returnFocusRef={legalReturnFocusRef}
      />
    </main>
  );
}

/** Renders a translated line whose `{time}` becomes a semantic <time>. */
function ExpiryLine({
  duration,
  label,
  template,
}: {
  duration: string;
  label: string;
  template: string;
}) {
  const [before, after = ""] = template.split("{time}");
  return (
    <>
      {before}
      <time dateTime={duration}>{label}</time>
      {after}
    </>
  );
}
