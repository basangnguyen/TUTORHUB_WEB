import {
  APIRequestError,
  type AuthProvidersResponse,
  type MySecurityResponse,
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
import { KeyRound, LogIn, LogOut, Mail, RefreshCw } from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useAuthProvidersQuery } from "../app/profile";
import {
  useMySecurityQuery,
  useReauthenticate,
  useStartReauthEmailCode,
} from "../app/security";
import { useSession } from "../app/session";
import "./StepUpAuthDialog.css";

type ReauthMethod = MySecurityResponse["reauth_methods"][number];

/** Strongest first-party factor first (ADR-0039 §7.6: password, then email code). */
const METHOD_PRIORITY: readonly ReauthMethod[] = ["password", "email_code"];
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
      return { failure: { kind: "sessionExpired" } };
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
  const lock = useCountdown();
  const cooldown = useCountdown();
  const methodGroupId = useId();
  const passwordRef = useRef<HTMLInputElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);
  const [method, setMethod] = useState<ReauthMethod>(methods[0] ?? "password");
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
  const busy = reauth.isPending || startCode.isPending;

  useEffect(() => {
    onBusyChange(busy);
  }, [busy, onBusyChange]);

  // Put the caret where the user types the proof for the current method.
  useEffect(() => {
    if (method === "password") {
      passwordRef.current?.focus();
    }
  }, [method]);

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
  }

  function applyFailure(error: unknown) {
    const outcome = classifyStepUpError(error);
    if ("field" in outcome) {
      setFieldError(outcome);
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

  async function verify(
    input:
      | { method: "password"; password: string }
      | { challenge_id: string; code: string; method: "email_code" },
  ) {
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
            {methods.map((option) => (
              <Button
                aria-pressed={method === option}
                disabled={busy}
                key={option}
                leadingIcon={option === "password" ? <KeyRound /> : <Mail />}
                onClick={() => switchMethod(option)}
                size="sm"
                variant={method === option ? "primary" : "secondary"}
              >
                {option === "password"
                  ? t("stepUp.methodPassword")
                  : t("stepUp.methodEmailCode")}
              </Button>
            ))}
          </div>
        </div>
      )}

      {method === "password" ? (
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
  kind: "signInAgain" | "portal" | "signOut";
  providers: AuthProvidersResponse | undefined;
  returnTo?: string;
}

/** Paths for sessions that cannot step up in place. */
function StepUpFallback({ kind, providers, returnTo }: FallbackProps) {
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
    kind === "portal"
      ? t("stepUp.oidcDescription")
      : kind === "signOut"
        ? t("stepUp.noMethodDescription")
        : t("stepUp.fallbackDescription");

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
  onSucceeded: () => void;
  returnTo?: string;
}

/** Mounted only while the dialog is open, so closed dialogs fetch nothing. */
function StepUpContent({
  onBusyChange,
  onSucceeded,
  returnTo,
}: StepUpContentProps) {
  const providersQuery = useAuthProvidersQuery();
  // A missing capability flag means an older server: treat it as disabled.
  const deviceManagement =
    providersQuery.data?.device_management_enabled === true;
  const securityQuery = useMySecurityQuery(deviceManagement);

  if (providersQuery.isPending) return <LoadingState />;
  if (providersQuery.isError) {
    return <LoadError onRetry={() => void providersQuery.refetch()} />;
  }
  if (!deviceManagement) {
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
  const methods = METHOD_PRIORITY.filter((method) =>
    available.includes(method),
  );
  if (methods.length === 0) {
    return (
      <StepUpFallback
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
  /** Where an OIDC re-authentication returns to; defaults to the current page. */
  returnTo?: string;
}

/**
 * Shared "Confirm it's you" dialog for actions guarded by the server's
 * 10-minute recent-authentication window (403
 * `auth_recent_authentication_required`).
 */
export function StepUpAuthDialog({
  onOpenChange,
  onVerified,
  open,
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
        <DialogDescription>{t("stepUp.description")}</DialogDescription>
        <StepUpContent
          onBusyChange={setBusy}
          onSucceeded={handleSucceeded}
          returnTo={returnTo}
        />
      </DialogContent>
    </Dialog>
  );
}
