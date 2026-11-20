import {
  APIRequestError,
  type AccountSecurityResponse,
  type AuthProvidersResponse,
  type ReauthenticateRequest,
} from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  ErrorState,
  Skeleton,
  SkeletonGroup,
  TextField,
} from "@tutorhub/ui";
import {
  Fingerprint,
  KeyRound,
  LogIn,
  LogOut,
  Mail,
  RefreshCw,
  Settings,
  Smartphone,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { Link, useInRouterContext } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  INVALID_PASSKEY_CODE,
  classifyCeremonyError,
  getPasskeyAssertion,
  isWebAuthnSupported,
} from "../app/passkeys";
import { useAuthProvidersQuery } from "../app/profile";
import {
  useMySecurityQuery,
  useReauthenticate,
  useStartPasskeyReauthentication,
  useStartReauthEmailCode,
} from "../app/security";
import { useSession } from "../app/session";
import "./StepUpAuthDialog.css";

type ReauthMethod = AccountSecurityResponse["reauth_methods"][number];

/**
 * Strongest first-party factor first (ADR-0039 §7.6: passkey, then TOTP,
 * then password, then email code), matching the server's evaluation order.
 */
const METHOD_PRIORITY: readonly ReauthMethod[] = [
  "passkey",
  "totp",
  "password",
  "email_code",
];
/** Factors that raise the session to AAL2 (AUTH-09A). */
const AAL2_METHODS: ReadonlySet<ReauthMethod> = new Set(["passkey", "totp"]);
const METHOD_LABEL_KEYS: Readonly<Record<ReauthMethod, TranslationKey>> = {
  email_code: "stepUp.methodEmailCode",
  passkey: "stepUp.methodPasskey",
  password: "stepUp.methodPassword",
  totp: "stepUp.methodTotp",
};
const METHOD_ICONS: Readonly<Record<ReauthMethod, typeof KeyRound>> = {
  email_code: Mail,
  passkey: Fingerprint,
  password: KeyRound,
  totp: Smartphone,
};
const OTP_PATTERN = /^\d{6}$/;
/** Upper bound for client-side locks; the server stays authoritative. */
const MAX_LOCK_SECONDS = 3600;
const DEFAULT_CODE_COOLDOWN_SECONDS = 60;

function boundedSeconds(seconds: number | undefined) {
  return typeof seconds === "number" && Number.isFinite(seconds) && seconds > 0
    ? Math.min(Math.ceil(seconds), MAX_LOCK_SECONDS)
    : 0;
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
    setRemaining(boundedSeconds(seconds));
  }, []);

  return { remaining, start };
}

type StepUpFailure =
  | { kind: "conflict" }
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "sessionExpired" }
  | { kind: "network" }
  | { kind: "passkeyInvalid" }
  | { kind: "passkeyClone" }
  | { kind: "passkeyUnavailable" }
  | { kind: "passkeyUnsupported" }
  | { kind: "passkeyFailed" }
  | { kind: "generic" };

type StepUpOutcome =
  | { field: "password" | "code"; key: TranslationKey }
  | { failure: StepUpFailure };

/** Maps the AUTH-04A problem contract; typed-credential 401s keep the session. */
function classifyStepUpError(error: unknown): StepUpOutcome {
  if (!(error instanceof APIRequestError)) {
    return { failure: { kind: "network" } };
  }
  const code = error.problem?.code;
  switch (error.status) {
    case 401:
      if (code === "auth_invalid_current_password") {
        return { field: "password", key: "stepUp.passwordInvalid" };
      }
      if (code === "auth_invalid_verification_code") {
        return { field: "code", key: "stepUp.codeInvalid" };
      }
      // A rejected assertion keeps the session (see queryClient.ts).
      if (code === INVALID_PASSKEY_CODE) {
        return { failure: { kind: "passkeyInvalid" } };
      }
      return { failure: { kind: "sessionExpired" } };
    case 403:
      return code === "auth_passkey_clone_detected"
        ? { failure: { kind: "passkeyClone" } }
        : { failure: { kind: "generic" } };
    case 404:
      // auth_passkey_not_found, or passkeys switched off meanwhile.
      return { failure: { kind: "passkeyUnavailable" } };
    case 409:
      return { failure: { kind: "conflict" } };
    case 429:
      return {
        failure: {
          kind: "rateLimited",
          seconds: boundedSeconds(error.retryAfterSeconds),
        },
      };
    case 503:
      return {
        failure: {
          kind: "busy",
          seconds: boundedSeconds(error.retryAfterSeconds),
        },
      };
    default:
      return { failure: { kind: "generic" } };
  }
}

interface FailureAlertProps {
  failure: StepUpFailure;
  onRefreshSession: () => void;
  refreshing: boolean;
}

function FailureAlert({
  failure,
  onRefreshSession,
  refreshing,
}: FailureAlertProps) {
  const { t } = useI18n();
  let text: string;
  switch (failure.kind) {
    case "conflict":
      text = t("stepUp.conflict");
      break;
    case "rateLimited":
      // A static value: a ticking number inside role="alert" re-announces.
      text =
        failure.seconds > 0
          ? t("stepUp.rateLimited", { seconds: failure.seconds })
          : t("stepUp.rateLimitedNoWait");
      break;
    case "busy":
      text =
        failure.seconds > 0
          ? t("stepUp.busy", { seconds: failure.seconds })
          : t("stepUp.busyNoWait");
      break;
    case "sessionExpired":
      text = t("stepUp.sessionExpired");
      break;
    case "network":
      text = t("stepUp.network");
      break;
    case "passkeyInvalid":
      text = t("stepUp.passkeyInvalid");
      break;
    case "passkeyClone":
      text = t("stepUp.passkeyClone");
      break;
    case "passkeyUnavailable":
      text = t("stepUp.passkeyUnavailable");
      break;
    case "passkeyUnsupported":
      text = t("stepUp.passkeyUnsupported");
      break;
    case "passkeyFailed":
      text = t("stepUp.passkeyFailed");
      break;
    default:
      text = t("stepUp.generic");
  }
  return (
    <div className="step-up__alert" role="alert">
      <span>{text}</span>
      {failure.kind === "conflict" && (
        <Button
          leadingIcon={<RefreshCw />}
          loading={refreshing}
          loadingLabel={t("stepUp.refreshingSession")}
          onClick={onRefreshSession}
          size="sm"
          variant="secondary"
        >
          {t("stepUp.refreshSession")}
        </Button>
      )}
    </div>
  );
}

interface StepUpFormsProps {
  methods: readonly ReauthMethod[];
  onBusyChange: (busy: boolean) => void;
  onSucceeded: () => void;
}

function StepUpForms({ methods, onBusyChange, onSucceeded }: StepUpFormsProps) {
  const { t } = useI18n();
  const session = useSession();
  const securityQuery = useMySecurityQuery();
  const reauth = useReauthenticate();
  const startCode = useStartReauthEmailCode();
  const startPasskey = useStartPasskeyReauthentication();
  const lock = useCountdown();
  const cooldown = useCountdown();
  const methodGroupId = useId();
  const passwordRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);
  const passkeyButtonRef = useRef<HTMLButtonElement>(null);
  const ceremonyRef = useRef<AbortController | null>(null);
  const [method, setMethod] = useState<ReauthMethod>(methods[0] ?? "password");
  const [ceremonyPending, setCeremonyPending] = useState(false);
  const [passkeyCancelled, setPasskeyCancelled] = useState(false);
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [challenge, setChallenge] = useState<{
    expiresInSeconds: number;
    id: string;
  } | null>(null);
  const [expired, setExpired] = useState(false);
  const [sentNotice, setSentNotice] = useState<{
    minutes: number;
    seconds: number;
  } | null>(null);
  const [fieldError, setFieldError] = useState<{
    field: "password" | "code";
    key: TranslationKey;
  } | null>(null);
  const [failure, setFailure] = useState<StepUpFailure | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const busy =
    reauth.isPending ||
    startCode.isPending ||
    startPasskey.isPending ||
    ceremonyPending;

  useEffect(() => {
    onBusyChange(busy);
  }, [busy, onBusyChange]);

  // Put the caret where the user types the proof for the current method;
  // for a passkey, on the button that opens the device prompt.
  useEffect(() => {
    if (method === "password") {
      passwordRef.current?.focus();
    } else if (method === "totp") {
      codeRef.current?.focus();
    } else if (method === "passkey") {
      passkeyButtonRef.current?.focus();
    }
  }, [method]);

  // The confirm button is disabled while the device prompt is open; once the
  // prompt was cancelled or refused, hand keyboard focus back to it.
  useEffect(() => {
    if (method === "passkey" && !busy && (passkeyCancelled || failure)) {
      passkeyButtonRef.current?.focus();
    }
  }, [busy, failure, method, passkeyCancelled]);

  // Closing the dialog cancels a pending device prompt.
  useEffect(
    () => () => {
      ceremonyRef.current?.abort();
      ceremonyRef.current = null;
    },
    [],
  );

  // Remind the user to resend once the server-side code lifetime has passed.
  useEffect(() => {
    if (!challenge) return;
    const timer = window.setTimeout(
      () => setExpired(true),
      boundedSeconds(challenge.expiresInSeconds) * 1000,
    );
    return () => window.clearTimeout(timer);
  }, [challenge]);

  function switchMethod(next: ReauthMethod) {
    if (busy || next === method) return;
    setMethod(next);
    setPassword("");
    setCode("");
    setFieldError(null);
    setFailure(null);
    setPasskeyCancelled(false);
  }

  function applyFailure(error: unknown) {
    const outcome = classifyStepUpError(error);
    if ("field" in outcome) {
      // A wrong authenticator code is not an emailed code: say which.
      setFieldError(
        method === "totp" && outcome.field === "code"
          ? { field: "code", key: "stepUp.totpInvalid" }
          : outcome,
      );
      (outcome.field === "password" ? passwordRef : codeRef).current?.focus();
      return;
    }
    if (
      outcome.failure.kind === "rateLimited" ||
      outcome.failure.kind === "busy"
    ) {
      lock.start(outcome.failure.seconds);
    }
    setFailure(outcome.failure);
  }

  async function verify(input: ReauthenticateRequest) {
    setFailure(null);
    setFieldError(null);
    try {
      await reauth.mutateAsync(input);
    } catch (error) {
      applyFailure(error);
      return;
    } finally {
      // Never keep the typed proof longer than the request needs it.
      setPassword("");
      setCode("");
      reauth.reset();
    }
    onSucceeded();
  }

  async function handlePasswordSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || lock.remaining > 0) return;
    if (!password) {
      setFieldError({ field: "password", key: "stepUp.passwordRequired" });
      passwordRef.current?.focus();
      return;
    }
    await verify({ method: "password", password });
  }

  /** POST /auth/reauth with the authenticator app's current code. */
  async function handleTotpSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || lock.remaining > 0) return;
    const value = code.replace(/\s+/g, "");
    if (!OTP_PATTERN.test(value)) {
      setFieldError({
        field: "code",
        key: value ? "stepUp.totpFormat" : "stepUp.totpRequired",
      });
      codeRef.current?.focus();
      return;
    }
    await verify({ code: value, method: "totp" });
  }

  async function handleCodeSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || lock.remaining > 0) return;
    const invalidKey: TranslationKey | null = !challenge
      ? "stepUp.codeRequired"
      : expired
        ? "stepUp.codeExpired"
        : OTP_PATTERN.test(code.trim())
          ? null
          : "stepUp.codeFormat";
    if (!challenge || invalidKey) {
      setFieldError({
        field: "code",
        key: invalidKey ?? "stepUp.codeRequired",
      });
      codeRef.current?.focus();
      return;
    }
    await verify({
      challenge_id: challenge.id,
      code: code.trim(),
      method: "email_code",
    });
  }

  /**
   * Options (bound to this session by the HttpOnly auth_tx cookie) → device
   * prompt → POST /auth/reauth. Closing the system sheet is a quiet cancel.
   */
  async function handlePasskeySubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || lock.remaining > 0) return;
    setFailure(null);
    setFieldError(null);
    setPasskeyCancelled(false);
    setCeremonyPending(true);
    const controller = new AbortController();
    ceremonyRef.current = controller;
    try {
      let options;
      try {
        options = await startPasskey.mutateAsync();
      } catch (error) {
        applyFailure(error);
        return;
      } finally {
        startPasskey.reset();
      }
      let assertion;
      try {
        assertion = await getPasskeyAssertion(options, {
          signal: controller.signal,
        });
      } catch (error) {
        if (controller.signal.aborted) return;
        const kind = classifyCeremonyError(error);
        if (kind === "cancelled") {
          setPasskeyCancelled(true);
          return;
        }
        setFailure({
          kind: kind === "unsupported" ? "passkeyUnsupported" : "passkeyFailed",
        });
        return;
      }
      await verify({ assertion, method: "passkey" });
    } finally {
      if (ceremonyRef.current === controller) ceremonyRef.current = null;
      setCeremonyPending(false);
    }
  }

  async function sendCode() {
    if (busy || cooldown.remaining > 0) return;
    setFailure(null);
    setFieldError(null);
    try {
      // The server picks the verified destination; the client never sends one.
      const result = await startCode.mutateAsync();
      const cooldownSeconds =
        boundedSeconds(result.cooldown_seconds) ||
        DEFAULT_CODE_COOLDOWN_SECONDS;
      setChallenge({
        expiresInSeconds: result.expires_in_seconds,
        id: result.challenge_id,
      });
      setExpired(false);
      setCode("");
      cooldown.start(cooldownSeconds);
      setSentNotice({
        minutes: Math.max(1, Math.ceil(result.expires_in_seconds / 60)),
        seconds: cooldownSeconds,
      });
      codeRef.current?.focus();
    } catch (error) {
      const outcome = classifyStepUpError(error);
      if (
        "failure" in outcome &&
        (outcome.failure.kind === "rateLimited" ||
          outcome.failure.kind === "busy")
      ) {
        cooldown.start(
          outcome.failure.seconds || DEFAULT_CODE_COOLDOWN_SECONDS,
        );
      }
      setFailure("failure" in outcome ? outcome.failure : { kind: "generic" });
    } finally {
      startCode.reset();
    }
  }

  async function refreshSession() {
    setRefreshing(true);
    try {
      await session.refresh();
      await securityQuery.refetch();
      setFailure(null);
    } catch {
      setFailure({ kind: "generic" });
    } finally {
      setRefreshing(false);
    }
  }

  const submitLabel = t("stepUp.submit");
  const submitText =
    lock.remaining > 0
      ? t("stepUp.submitWait", { label: submitLabel, seconds: lock.remaining })
      : submitLabel;
  const codeButtonLabel = challenge
    ? t("stepUp.resendCode")
    : t("stepUp.sendCode");
  const errorFor = (field: "password" | "code") =>
    fieldError?.field === field ? t(fieldError.key) : undefined;

  return (
    <div className="step-up__body">
      {methods.length > 1 && (
        <div
          aria-labelledby={methodGroupId}
          className="step-up__methods"
          role="group"
        >
          <span className="step-up__methods-label" id={methodGroupId}>
            {t("stepUp.methodGroup")}
          </span>
          <div className="step-up__methods-options">
            {methods.map((option) => {
              const Icon = METHOD_ICONS[option];
              return (
                <Button
                  aria-pressed={method === option}
                  disabled={busy}
                  key={option}
                  leadingIcon={<Icon />}
                  onClick={() => switchMethod(option)}
                  size="sm"
                  variant={method === option ? "primary" : "secondary"}
                >
                  {t(METHOD_LABEL_KEYS[option])}
                </Button>
              );
            })}
          </div>
        </div>
      )}

      {method === "passkey" ? (
        <form
          aria-label={t("stepUp.methodPasskey")}
          className="step-up__form"
          noValidate
          onSubmit={(event) => void handlePasskeySubmit(event)}
        >
          <p className="step-up__intro">
            {methods.length === 1
              ? t("stepUp.passkeyOnlyIntro")
              : t("stepUp.passkeyIntro")}
          </p>
          {/* Always mounted so assistive tech observes the update. */}
          <p aria-live="polite" className="step-up__notice" role="status">
            {passkeyCancelled ? t("stepUp.passkeyCancelled") : null}
          </p>
          {failure && (
            <FailureAlert
              failure={failure}
              onRefreshSession={() => void refreshSession()}
              refreshing={refreshing}
            />
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("stepUp.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<Fingerprint />}
              loading={busy}
              loadingLabel={t("stepUp.passkeyWaiting")}
              ref={passkeyButtonRef}
              type="submit"
            >
              {lock.remaining > 0 ? submitText : t("stepUp.passkeySubmit")}
            </Button>
          </DialogFooter>
        </form>
      ) : method === "totp" ? (
        <form
          aria-label={t("stepUp.methodTotp")}
          className="step-up__form"
          noValidate
          onSubmit={(event) => void handleTotpSubmit(event)}
        >
          <p className="step-up__intro">{t("stepUp.totpIntro")}</p>
          <TextField
            autoComplete="one-time-code"
            error={errorFor("code")}
            hint={t("stepUp.totpHint")}
            inputMode="numeric"
            label={t("stepUp.totpLabel")}
            maxLength={6}
            name="step-up-totp"
            onChange={(event) => {
              setCode(event.target.value);
              if (fieldError?.field === "code") setFieldError(null);
            }}
            ref={codeRef}
            spellCheck={false}
            type="text"
            value={code}
          />
          {failure && (
            <FailureAlert
              failure={failure}
              onRefreshSession={() => void refreshSession()}
              refreshing={refreshing}
            />
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("stepUp.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<Smartphone />}
              loading={reauth.isPending}
              loadingLabel={t("stepUp.submitting")}
              type="submit"
            >
              {submitText}
            </Button>
          </DialogFooter>
        </form>
      ) : method === "password" ? (
        <form
          aria-label={t("stepUp.methodPassword")}
          className="step-up__form"
          noValidate
          onSubmit={(event) => void handlePasswordSubmit(event)}
        >
          <TextField
            autoComplete="current-password"
            error={errorFor("password")}
            label={t("stepUp.passwordLabel")}
            name="step-up-password"
            onChange={(event) => {
              setPassword(event.target.value);
              if (fieldError?.field === "password") setFieldError(null);
            }}
            ref={passwordRef}
            type="password"
            value={password}
          />
          {failure && (
            <FailureAlert
              failure={failure}
              onRefreshSession={() => void refreshSession()}
              refreshing={refreshing}
            />
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("stepUp.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<KeyRound />}
              loading={reauth.isPending}
              loadingLabel={t("stepUp.submitting")}
              type="submit"
            >
              {submitText}
            </Button>
          </DialogFooter>
        </form>
      ) : (
        <form
          aria-label={t("stepUp.methodEmailCode")}
          className="step-up__form"
          noValidate
          onSubmit={(event) => void handleCodeSubmit(event)}
        >
          <p className="step-up__intro">{t("stepUp.emailCodeIntro")}</p>
          <div className="step-up__send">
            <Button
              disabled={cooldown.remaining > 0 || reauth.isPending}
              leadingIcon={<Mail />}
              loading={startCode.isPending}
              loadingLabel={t("stepUp.sendingCode")}
              onClick={() => void sendCode()}
              size="sm"
              variant="secondary"
            >
              {cooldown.remaining > 0
                ? t("stepUp.codeWait", {
                    label: codeButtonLabel,
                    seconds: cooldown.remaining,
                  })
                : codeButtonLabel}
            </Button>
            {/* Always mounted so assistive tech observes the update. */}
            <p aria-live="polite" className="step-up__notice" role="status">
              {challenge && expired
                ? t("stepUp.codeExpired")
                : sentNotice
                  ? t("stepUp.codeSent", sentNotice)
                  : null}
            </p>
          </div>
          <TextField
            autoComplete="one-time-code"
            error={errorFor("code")}
            hint={t("stepUp.codeHint")}
            inputMode="numeric"
            label={t("stepUp.codeLabel")}
            maxLength={6}
            name="step-up-code"
            onChange={(event) => {
              setCode(event.target.value);
              if (fieldError?.field === "code") setFieldError(null);
            }}
            ref={codeRef}
            spellCheck={false}
            type="text"
            value={code}
          />
          {failure && (
            <FailureAlert
              failure={failure}
              onRefreshSession={() => void refreshSession()}
              refreshing={refreshing}
            />
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("stepUp.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={lock.remaining > 0 || startCode.isPending}
              leadingIcon={<KeyRound />}
              loading={reauth.isPending}
              loadingLabel={t("stepUp.submitting")}
              type="submit"
            >
              {submitText}
            </Button>
          </DialogFooter>
        </form>
      )}
    </div>
  );
}

function currentPath() {
  return `${window.location.pathname}${window.location.search}`;
}

interface FallbackProps {
  /** Overrides the kind's explanation (e.g. a passkey-only browser gap). */
  description?: string;
  kind: "signInAgain" | "portal" | "signOut";
  providers: AuthProvidersResponse | undefined;
  returnTo?: string;
}

/** Paths for sessions that cannot step up in place. */
function StepUpFallback({
  description: descriptionOverride,
  kind,
  providers,
  returnTo,
}: FallbackProps) {
  const { t } = useI18n();
  const session = useSession();
  const [signingOut, setSigningOut] = useState(false);

  async function signOut() {
    setSigningOut(true);
    try {
      await session.signOut();
    } finally {
      setSigningOut(false);
    }
  }

  // Without device management the only fresh proof is a new sign-in: through
  // the OIDC portal when it exists, otherwise sign out and log in natively.
  const usePortal =
    kind === "portal" ||
    (kind === "signInAgain" && providers?.oidc_default_enabled === true);
  const description =
    descriptionOverride ??
    (kind === "portal"
      ? t("stepUp.oidcDescription")
      : kind === "signOut"
        ? t("stepUp.noMethodDescription")
        : t("stepUp.fallbackDescription"));

  return (
    <div className="step-up__body">
      <p className="step-up__intro">{description}</p>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="secondary">{t("stepUp.cancel")}</Button>
        </DialogClose>
        {usePortal ? (
          <Button
            leadingIcon={<LogIn />}
            onClick={() => session.signIn(returnTo ?? currentPath())}
          >
            {kind === "portal"
              ? t("stepUp.continueWithPortal")
              : t("stepUp.signInAgain")}
          </Button>
        ) : (
          <Button
            leadingIcon={kind === "signOut" ? <LogOut /> : <LogIn />}
            loading={signingOut}
            loadingLabel={t("stepUp.signingOut")}
            onClick={() => void signOut()}
          >
            {kind === "signOut" ? t("stepUp.signOut") : t("stepUp.signInAgain")}
          </Button>
        )}
      </DialogFooter>
    </div>
  );
}

function isOnSettingsPage() {
  const path = window.location.pathname;
  return path === "/app/settings" || path.startsWith("/app/settings/");
}

/** Opens Settings in the SPA when a router exists, else as a page load. */
function SettingsLink({ onNavigate }: { onNavigate: () => void }) {
  const { t } = useI18n();
  const inRouter = useInRouterContext();
  const className = "th-button th-button--primary th-button--md step-up__link";
  const content = (
    <>
      <span aria-hidden="true" className="th-button__icon">
        <Settings />
      </span>
      <span>{t("stepUp.aal2OpenSettings")}</span>
    </>
  );
  return inRouter ? (
    <Link className={className} onClick={onNavigate} to="/app/settings">
      {content}
    </Link>
  ) : (
    <a className={className} href="/app/settings">
      {content}
    </a>
  );
}

/**
 * AAL2 is required but no passkey or authenticator app can be used here:
 * explain how to add one instead of offering factors that keep AAL1.
 */
function Aal2Guidance({
  onNavigate,
  passkeyUnsupported,
}: {
  onNavigate: () => void;
  passkeyUnsupported: boolean;
}) {
  const { t } = useI18n();
  const onSettings = isOnSettingsPage();
  return (
    <div className="step-up__body">
      <p className="step-up__intro">
        {passkeyUnsupported
          ? t("stepUp.aal2PasskeyUnsupported")
          : t("stepUp.aal2NoMethod")}
      </p>
      <p className="step-up__intro">
        {onSettings ? t("stepUp.aal2SetupHere") : t("stepUp.aal2SettingsHint")}
      </p>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="secondary">{t("stepUp.close")}</Button>
        </DialogClose>
        {!onSettings && <SettingsLink onNavigate={onNavigate} />}
      </DialogFooter>
    </div>
  );
}

function LoadingState() {
  const { t } = useI18n();
  return (
    <SkeletonGroup label={t("stepUp.loading")}>
      <Skeleton height={20} width="45%" />
      <Skeleton height={44} />
      <Skeleton height={40} width="60%" />
    </SkeletonGroup>
  );
}

function LoadError({ onRetry }: { onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <ErrorState
      actions={
        <Button
          leadingIcon={<RefreshCw />}
          onClick={onRetry}
          size="sm"
          variant="secondary"
        >
          {t("state.retry")}
        </Button>
      }
      description={t("stepUp.loadError")}
      title={t("stepUp.loadErrorTitle")}
    />
  );
}

interface StepUpContentProps {
  onBusyChange: (busy: boolean) => void;
  onClose: () => void;
  onSucceeded: () => void;
  requireAal2: boolean;
  returnTo?: string;
}

/** Mounted only while the dialog is open, so closed dialogs fetch nothing. */
function StepUpContent({
  onBusyChange,
  onClose,
  onSucceeded,
  requireAal2,
  returnTo,
}: StepUpContentProps) {
  const providersQuery = useAuthProvidersQuery();
  // A missing capability flag means an older server: treat it as disabled.
  const deviceManagement =
    providersQuery.data?.device_management_enabled === true;
  const totpEnabled = providersQuery.data?.mfa_totp_enabled === true;
  // AUTH-09A serves /me/security and TOTP step-up without device management;
  // password and email-code step-up still need it.
  const securityEnabled = deviceManagement || totpEnabled;
  const securityQuery = useMySecurityQuery(securityEnabled);
  const { t } = useI18n();
  const [webAuthn] = useState(isWebAuthnSupported);

  if (providersQuery.isPending) return <LoadingState />;
  if (providersQuery.isError) {
    return <LoadError onRetry={() => void providersQuery.refetch()} />;
  }
  if (!securityEnabled) {
    return (
      <StepUpFallback
        kind="signInAgain"
        providers={providersQuery.data}
        returnTo={returnTo}
      />
    );
  }
  if (securityQuery.isPending) return <LoadingState />;
  if (securityQuery.isError) {
    return <LoadError onRetry={() => void securityQuery.refetch()} />;
  }

  const available = securityQuery.data.reauth_methods;
  const methods = METHOD_PRIORITY.filter(
    (method) =>
      available.includes(method) &&
      // A passkey is only offered where this browser can run the ceremony.
      (method !== "passkey" || webAuthn) &&
      (method !== "totp" || totpEnabled) &&
      (deviceManagement || AAL2_METHODS.has(method)) &&
      // AAL1 factors would only loop back into the same refusal.
      (!requireAal2 || AAL2_METHODS.has(method)),
  );
  if (methods.length === 0 && requireAal2) {
    return (
      <Aal2Guidance
        onNavigate={onClose}
        passkeyUnsupported={available.includes("passkey") && !webAuthn}
      />
    );
  }
  if (methods.length === 0 && !deviceManagement) {
    return (
      <StepUpFallback
        kind="signInAgain"
        providers={providersQuery.data}
        returnTo={returnTo}
      />
    );
  }
  if (methods.length === 0) {
    return (
      <StepUpFallback
        description={
          available.includes("passkey")
            ? t("stepUp.passkeyBrowserUnsupported")
            : undefined
        }
        kind={providersQuery.data.oidc_default_enabled ? "portal" : "signOut"}
        providers={providersQuery.data}
        returnTo={returnTo}
      />
    );
  }
  return (
    <StepUpForms
      methods={methods}
      onBusyChange={onBusyChange}
      onSucceeded={onSucceeded}
    />
  );
}

export interface StepUpAuthDialogProps {
  onOpenChange: (open: boolean) => void;
  /** Runs once after a successful step-up, after focus has been restored. */
  onVerified: () => void;
  open: boolean;
  /**
   * AUTH-09B: offer only factors that raise the session to AAL2 (passkey,
   * authenticator app), for 403 `auth_assurance_required` and
   * `auth_admin_mfa_required`.
   */
  requireAal2?: boolean;
  /** Where an OIDC re-authentication returns to; defaults to the current page. */
  returnTo?: string;
}

/**
 * Shared "Confirm it's you" dialog for actions guarded by the server's
 * 10-minute recent-authentication window (403
 * `auth_recent_authentication_required`) or by the AAL2 policy (403
 * `auth_assurance_required` / `auth_admin_mfa_required`, with
 * `requireAal2`).
 */
export function StepUpAuthDialog({
  onOpenChange,
  onVerified,
  open,
  requireAal2 = false,
  returnTo,
}: StepUpAuthDialogProps) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [verified, setVerified] = useState(false);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const pendingVerifiedRef = useRef(false);

  const finishVerified = useCallback(() => {
    if (!pendingVerifiedRef.current) return;
    pendingVerifiedRef.current = false;
    setVerified(false);
    onVerified();
  }, [onVerified]);

  // Fallback in case the close focus event does not fire: the timer is queued
  // after Radix's own focus-restoring timer, so focus still settles first.
  useEffect(() => {
    if (!verified) return;
    const timer = window.setTimeout(finishVerified, 0);
    return () => window.clearTimeout(timer);
  }, [finishVerified, verified]);

  const handleSucceeded = useCallback(() => {
    pendingVerifiedRef.current = true;
    setBusy(false);
    setVerified(true);
  }, []);

  // Remember what had focus before the dialog (or its cached forms, which
  // focus their first field) takes it, so closing can hand it back.
  useLayoutEffect(() => {
    if (!open) return;
    const active = document.activeElement;
    restoreFocusRef.current =
      active instanceof HTMLElement && active !== document.body ? active : null;
  }, [open]);

  return (
    <Dialog
      onOpenChange={(nextOpen) => {
        // Closing mid-request would orphan a rotated session cookie.
        if (!nextOpen && busy) return;
        onOpenChange(nextOpen);
      }}
      open={open && !verified}
    >
      <DialogContent
        className="step-up"
        closeLabel={t("stepUp.closeLabel")}
        onCloseAutoFocus={(event) => {
          // Controlled dialogs have no trigger: return focus to whatever
          // opened the step-up (the blocked action), then resume it.
          event.preventDefault();
          const target = restoreFocusRef.current;
          restoreFocusRef.current = null;
          if (target?.isConnected) target.focus({ preventScroll: true });
          finishVerified();
        }}
      >
        <DialogTitle>{t("stepUp.title")}</DialogTitle>
        <DialogDescription>
          {requireAal2 ? t("stepUp.aal2Description") : t("stepUp.description")}
        </DialogDescription>
        <StepUpContent
          onBusyChange={setBusy}
          onClose={() => onOpenChange(false)}
          onSucceeded={handleSucceeded}
          requireAal2={requireAal2}
          returnTo={returnTo}
        />
      </DialogContent>
    </Dialog>
  );
}
