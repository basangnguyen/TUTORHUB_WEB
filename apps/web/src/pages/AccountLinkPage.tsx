import {
  APIRequestError,
  getOAuthLinkIntent,
  type OAuthLinkIntentResponse,
} from "@tutorhub/api-client";
import { Button, ErrorState, Skeleton, SkeletonGroup } from "@tutorhub/ui";
import { Link2, LogOut, RefreshCw, ShieldCheck, X } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  useCancelLinkIntent,
  useConfirmLinkIntent,
  useStepUpRequest,
} from "../app/security";
import { useSession } from "../app/session";
import { StepUpAuthDialog } from "../components/StepUpAuthDialog";
import { LoginForm } from "./SignInPage";
import {
  ACCOUNT_LINK_PATH,
  NO_SOCIAL,
  getApiBaseUrl,
  linkProviderFrom,
  loginModeFor,
  passwordMinLengthFor,
  sanitizeLinkReturnTo,
  useAuthProviders,
  type LinkProvider,
} from "./SignInPage.shared";
import "./AccountLinkPage.css";

/** Upper bound for client-side Retry-After locks; the server stays authoritative. */
const MAX_RETRY_AFTER_SECONDS = 3600;

const LINK_INTENT_UNAVAILABLE_CODE = "auth_link_intent_unavailable";
const LINK_INTENT_MISMATCH_CODE = "auth_link_intent_mismatch";
const ASSURANCE_REQUIRED_CODE = "auth_assurance_required";

type IntentState =
  | { status: "loading" }
  | { status: "unavailable" }
  | { status: "error" }
  | {
      status: "ready";
      intent: OAuthLinkIntentResponse;
      /** Wall-clock deadline: throttled background timers cannot drift it. */
      expiresAt: number;
      remaining: number;
    };

/** 404 on preview, or 400 `auth_link_intent_unavailable` on confirm/cancel. */
function isIntentUnavailable(error: unknown) {
  return (
    error instanceof APIRequestError &&
    (error.status === 404 ||
      (error.status === 400 &&
        error.problem?.code === LINK_INTENT_UNAVAILABLE_CODE))
  );
}

function boundedSeconds(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), MAX_RETRY_AFTER_SECONDS)
    : undefined;
}

/**
 * The masked preview stays in component state (not the shared query cache),
 * so a native sign-in that clears the cache does not blank this screen.
 */
function useLinkIntentPreview() {
  const [state, setState] = useState<IntentState>({ status: "loading" });
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    // Every provider the API supports, Microsoft included (AUTH-07B). Facebook
    // and Microsoft intents may come from a completed pending registration.
    getOAuthLinkIntent({
      baseUrl: getApiBaseUrl(),
      signal: controller.signal,
    }).then(
      (intent) => {
        if (controller.signal.aborted) return;
        const remaining = Math.max(0, Math.ceil(intent.expires_in_seconds));
        setState(
          remaining > 0
            ? {
                status: "ready",
                intent,
                expiresAt: Date.now() + remaining * 1000,
                remaining,
              }
            : { status: "unavailable" },
        );
      },
      (error: unknown) => {
        if (controller.signal.aborted) return;
        setState(
          isIntentUnavailable(error)
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

function useRetryLock() {
  const [remaining, setRemaining] = useState(0);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(() => {
      setRemaining((value) => Math.max(0, value - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  const start = useCallback((seconds: number) => setRemaining(seconds), []);
  return { remaining, start };
}

function formatRemaining(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return {
    label: `${minutes}:${String(rest).padStart(2, "0")}`,
    duration: `PT${minutes}M${rest}S`,
  };
}

type ActionFailure =
  | { kind: "mismatch" }
  | { kind: "assurance" }
  | { kind: "session" }
  | { kind: "conflict" }
  | { kind: "rateLimited"; seconds?: number }
  | { kind: "busy" }
  | { kind: "network" }
  | { kind: "generic" };

function actionFailureFrom(error: unknown): ActionFailure {
  if (!(error instanceof APIRequestError)) return { kind: "network" };
  switch (error.status) {
    case 401:
      return { kind: "session" };
    case 403:
      if (error.problem?.code === LINK_INTENT_MISMATCH_CODE) {
        return { kind: "mismatch" };
      }
      if (error.problem?.code === ASSURANCE_REQUIRED_CODE) {
        return { kind: "assurance" };
      }
      return { kind: "generic" };
    case 409:
      return { kind: "conflict" };
    case 429:
      return {
        kind: "rateLimited",
        seconds: boundedSeconds(error.retryAfterSeconds),
      };
    case 503:
      return { kind: "busy" };
    default:
      return { kind: "generic" };
  }
}

function actionFailureText(
  failure: ActionFailure,
  t: (key: TranslationKey, values?: Record<string, string | number>) => string,
) {
  switch (failure.kind) {
    case "mismatch":
      return t("accountLink.mismatch");
    case "assurance":
      return t("accountLink.assuranceRequired");
    case "session":
      return t("accountLink.sessionExpired");
    case "conflict":
      return t("accountLink.conflict");
    case "rateLimited":
      return failure.seconds
        ? t("accountLink.rateLimitedWait", { seconds: failure.seconds })
        : t("accountLink.rateLimited");
    case "busy":
      return t("accountLink.busy");
    case "network":
      return t("accountLink.networkError");
    default:
      return t("accountLink.actionError");
  }
}

const PROVIDER_NAME_KEYS: Readonly<Record<LinkProvider, TranslationKey>> = {
  facebook: "accountLink.providerFacebook",
  google: "accountLink.providerGoogle",
  microsoft: "accountLink.providerMicrosoft",
  oidc: "accountLink.providerOidc",
};

/**
 * AUTH-05B: public confirmation screen for a pending provider link. The
 * HttpOnly `link_tx` cookie never reaches JavaScript; the page only sees the
 * masked email and remaining lifetime the server returns.
 */
export function AccountLinkPage() {
  const { t } = useI18n();
  const session = useSession();
  const navigate = useNavigate();
  const location = useLocation();
  const titleId = useId();
  const confirmHeadingRef = useRef<HTMLHeadingElement>(null);
  const focusConfirmRef = useRef(false);
  const { markUnavailable, reload, state } = useLinkIntentPreview();
  const { reload: reloadProviders, state: providersState } = useAuthProviders();
  const confirm = useConfirmLinkIntent();
  const cancel = useCancelLinkIntent();
  const lock = useRetryLock();
  const stepUp = useStepUpRequest();
  const [sessionRefreshing, setSessionRefreshing] = useState(false);

  const provider: LinkProvider =
    state.status === "ready"
      ? state.intent.provider
      : linkProviderFrom(new URLSearchParams(location.search).get("provider"));
  const providerName = t(PROVIDER_NAME_KEYS[provider]);
  const selfPath = `${ACCOUNT_LINK_PATH}?provider=${provider}`;
  const providers =
    providersState.status === "ready" ? providersState.providers : null;
  const authenticated = session.status === "authenticated";

  // After an in-place sign-in, move focus to the step that is now active.
  useEffect(() => {
    if (authenticated && state.status === "ready" && focusConfirmRef.current) {
      focusConfirmRef.current = false;
      confirmHeadingRef.current?.focus();
    }
  }, [authenticated, state.status]);

  const { reset: resetConfirm } = confirm;
  const handleAuthenticated = useCallback(async () => {
    resetConfirm();
    focusConfirmRef.current = true;
    // Stay on this screen: the user still has to confirm the link.
    await session.completeNativeSignIn();
  }, [resetConfirm, session]);

  function failWith(error: unknown) {
    if (isIntentUnavailable(error)) {
      markUnavailable();
      return;
    }
    const failure = actionFailureFrom(error);
    if (failure.kind === "rateLimited" && failure.seconds) {
      lock.start(failure.seconds);
    }
  }

  /**
   * AUTH-09B: an account with a second factor links only from an AAL2
   * session; 403 `auth_assurance_required` opens the AAL2 step-up here and
   * the confirmation runs again exactly once after it succeeds.
   */
  async function handleConfirm(afterStepUp = false) {
    if (lock.remaining > 0 || confirm.isPending) return;
    cancel.reset();
    try {
      const result = await confirm.mutateAsync();
      void navigate(sanitizeLinkReturnTo(result.return_to), { replace: true });
    } catch (error) {
      failWith(error);
      if (actionFailureFrom(error).kind === "assurance" && !afterStepUp) {
        requestAal2StepUp();
      }
    }
  }

  function requestAal2StepUp() {
    stepUp.request(() => handleConfirm(true), { requireAal2: true });
  }

  async function handleCancel() {
    if (cancel.isPending) return;
    confirm.reset();
    try {
      await cancel.mutateAsync();
    } catch (error) {
      if (!isIntentUnavailable(error)) {
        failWith(error);
        return;
      }
      // Already gone: the server cleared the stale cookie as well.
    }
    void navigate("/sign-in", { replace: true });
  }

  async function handleRefreshSession() {
    setSessionRefreshing(true);
    try {
      await session.refresh();
      confirm.reset();
    } catch {
      // The session error state below offers its own retry.
    } finally {
      setSessionRefreshing(false);
    }
  }

  const actionError = confirm.error ?? cancel.error;
  const failure =
    actionError && !isIntentUnavailable(actionError)
      ? actionFailureFrom(actionError)
      : null;
  const lockLabel =
    lock.remaining > 0
      ? t("accountLink.retryWait", { seconds: lock.remaining })
      : null;
  const ready = state.status === "ready" ? state : null;
  const remaining = ready ? formatRemaining(ready.remaining) : null;

  return (
    <main className="account-link-page">
      <section
        aria-busy={state.status === "loading" || undefined}
        aria-labelledby={titleId}
        className="account-link-card"
      >
        <p className="account-link-card__kicker">{t("accountLink.kicker")}</p>
        <h1 id={titleId}>
          {t("accountLink.title", { provider: providerName })}
        </h1>

        {state.status === "loading" && (
          <SkeletonGroup label={t("accountLink.loading")}>
            <Skeleton height={22} width="72%" />
            <Skeleton height={86} />
            <Skeleton height={42} />
          </SkeletonGroup>
        )}

        {state.status === "unavailable" && (
          <ErrorState
            actions={
              authenticated ? (
                <Link className="account-link-card__continue" to="/app/home">
                  {t("invitation.publicContinueAction")}
                </Link>
              ) : (
                <Link className="account-link-card__continue" to="/sign-in">
                  {t("accountLink.backToSignIn")}
                </Link>
              )
            }
            description={t("accountLink.unavailableDescription")}
            title={t("accountLink.unavailableTitle")}
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
                {t("accountLink.retry")}
              </Button>
            }
            description={t("accountLink.loadErrorDescription")}
            title={t("accountLink.loadErrorTitle")}
          />
        )}

        {ready && remaining && (
          <div className="account-link-card__content">
            <p className="account-link-card__lead">
              {t("accountLink.existing", {
                email: ready.intent.masked_email,
                provider: providerName,
              })}
            </p>
            <p className="account-link-card__expiry">
              <ExpiryLine
                duration={remaining.duration}
                label={remaining.label}
                template={t("accountLink.expiresIn", { time: "{time}" })}
              />
            </p>

            {failure && (
              <div className="account-link-card__alert" role="alert">
                <p>{actionFailureText(failure, t)}</p>
                {failure.kind === "mismatch" && authenticated && (
                  <Button
                    leadingIcon={<LogOut />}
                    // A native logout keeps the HttpOnly link cookie, so the
                    // user comes back here to sign in to the right account.
                    onClick={() => void session.signOut({ returnTo: selfPath })}
                    size="sm"
                    variant="secondary"
                  >
                    {t("accountLink.signOut")}
                  </Button>
                )}
                {failure.kind === "assurance" && authenticated && (
                  <Button
                    leadingIcon={<ShieldCheck />}
                    onClick={requestAal2StepUp}
                    size="sm"
                    variant="secondary"
                  >
                    {t("accountLink.verifyNow")}
                  </Button>
                )}
                {failure.kind === "conflict" && authenticated && (
                  <Button
                    leadingIcon={<RefreshCw />}
                    loading={sessionRefreshing}
                    loadingLabel={t("accountLink.checkingSession")}
                    onClick={() => void handleRefreshSession()}
                    size="sm"
                    variant="secondary"
                  >
                    {t("accountLink.refreshSession")}
                  </Button>
                )}
              </div>
            )}

            {session.status === "loading" && (
              <div className="account-link-card__actions">
                <Button disabled>{t("accountLink.checkingSession")}</Button>
              </div>
            )}

            {session.status === "error" && (
              <ErrorState
                actions={
                  <Button
                    leadingIcon={<RefreshCw />}
                    onClick={() =>
                      void session.refresh().catch(() => undefined)
                    }
                    variant="secondary"
                  >
                    {t("accountLink.retry")}
                  </Button>
                }
                description={t("auth.unavailableDescription")}
                title={t("auth.unavailableTitle")}
              />
            )}

            {session.status === "unauthenticated" && (
              <div className="account-link-card__step">
                <h2>{t("accountLink.signInHeading")}</h2>
                <p>
                  {t("accountLink.signInDescription", {
                    provider: providerName,
                  })}
                </p>
                {providersState.status === "loading" && (
                  <SkeletonGroup label={t("accountLink.providersLoading")}>
                    <Skeleton height={48} />
                    <Skeleton height={48} />
                  </SkeletonGroup>
                )}
                {providersState.status === "error" && (
                  <ErrorState
                    actions={
                      <Button
                        leadingIcon={<RefreshCw />}
                        onClick={reloadProviders}
                        variant="secondary"
                      >
                        {t("accountLink.retry")}
                      </Button>
                    }
                    description={t("accountLink.loadErrorDescription")}
                    title={t("accountLink.providersErrorTitle")}
                  />
                )}
                {providers && (
                  <div className="sign-in sign-in--embedded">
                    <LoginForm
                      autoFocus={false}
                      emailCodeEnabled={providers.email_code_enabled === true}
                      mode={loginModeFor(providers)}
                      onAuthenticated={handleAuthenticated}
                      onPasswordReset={session.resetAfterPasswordRecovery}
                      onSignIn={() => session.signIn(selfPath)}
                      // Passkey sign-in also stays here to confirm the link.
                      passkeysEnabled={providers.passkeys_enabled === true}
                      passwordMinLength={passwordMinLengthFor(providers)}
                      // AUTH-10B: a phone code (and its second step) also
                      // signs in here and stays to confirm the link.
                      phoneAllowedCountries={providers.phone_allowed_countries}
                      phoneEnabled={providers.phone_enabled === true}
                      recoveryEnabled={
                        providers.password_recovery_enabled === true
                      }
                      social={NO_SOCIAL}
                    />
                  </div>
                )}
                <div className="account-link-card__actions account-link-card__actions--secondary">
                  <Button
                    leadingIcon={<X />}
                    loading={cancel.isPending}
                    loadingLabel={t("accountLink.cancelling")}
                    onClick={() => void handleCancel()}
                    variant="quiet"
                  >
                    {t("accountLink.cancel")}
                  </Button>
                </div>
              </div>
            )}

            {authenticated && session.currentUser && (
              <div className="account-link-card__step">
                <h2 ref={confirmHeadingRef} tabIndex={-1}>
                  {t("accountLink.confirmHeading")}
                </h2>
                <dl className="account-link-card__facts">
                  <div>
                    <dt>{t("accountLink.currentAccount")}</dt>
                    <dd>
                      <strong>{session.currentUser.user.display_name}</strong>
                      <span>{session.currentUser.user.email}</span>
                    </dd>
                  </div>
                </dl>
                <p>
                  {t("accountLink.confirmDescription", {
                    provider: providerName,
                  })}
                </p>
                <div className="account-link-card__actions">
                  <Button
                    disabled={lock.remaining > 0}
                    leadingIcon={<Link2 />}
                    loading={confirm.isPending}
                    loadingLabel={t("accountLink.confirming")}
                    onClick={() => void handleConfirm()}
                  >
                    {lockLabel ??
                      t("accountLink.confirm", { provider: providerName })}
                  </Button>
                  <Button
                    disabled={confirm.isPending}
                    leadingIcon={<X />}
                    loading={cancel.isPending}
                    loadingLabel={t("accountLink.cancelling")}
                    onClick={() => void handleCancel()}
                    variant="secondary"
                  >
                    {t("accountLink.cancel")}
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}
      </section>
      <StepUpAuthDialog {...stepUp.dialog} returnTo={selfPath} />
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
