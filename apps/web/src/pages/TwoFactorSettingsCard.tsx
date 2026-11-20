import {
  APIRequestError,
  type TOTPEnrollmentResponse,
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
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import {
  Copy,
  Download,
  KeySquare,
  RefreshCw,
  ShieldCheck,
  ShieldOff,
  Smartphone,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useI18n, type TranslationKey } from "../app/i18n";
import { recoveryCodesFileText } from "../app/mfa";
import {
  stepUpRequirementFor,
  useConfirmTOTP,
  useDisableTOTP,
  useEnrollTOTP,
  useMySecurityQuery,
  useRegenerateRecoveryCodes,
  type StepUpRequestOptions,
  type StepUpRetry,
} from "../app/security";
import { encodeQrCode, formatTotpSecret, qrSvgPath } from "../app/totpQr";

type RequestStepUp = (
  retry?: StepUpRetry,
  options?: StepUpRequestOptions,
) => void;

const TOTP_PATTERN = /^\d{6}$/;
const LOW_RECOVERY_CODES = 3;
const MAX_LOCK_SECONDS = 3600;
const COPY_NOTICE_MS = 4000;
const RECOVERY_FILE_NAME = "tutorhub-recovery-codes.txt";

type TwoFactorFailure =
  | { kind: "stepUp"; requireAal2: boolean }
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "network" }
  | { kind: "generic" };

type TwoFactorOutcome =
  { failure: TwoFactorFailure } | { conflict: true } | { invalidCode: true };

function boundedSeconds(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), MAX_LOCK_SECONDS)
    : 0;
}

/** AUTH-09A problem contract of the TOTP and recovery-code endpoints. */
function classifyTwoFactorError(error: unknown): TwoFactorOutcome {
  const requirement = stepUpRequirementFor(error);
  if (requirement) {
    return { failure: { kind: "stepUp", ...requirement } };
  }
  if (!(error instanceof APIRequestError)) {
    return { failure: { kind: "network" } };
  }
  switch (error.status) {
    case 400:
      return { invalidCode: true };
    case 401:
      return error.problem?.code === "auth_invalid_verification_code"
        ? { invalidCode: true }
        : { failure: { kind: "generic" } };
    case 409:
      return { conflict: true };
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

function useLock() {
  const [remaining, setRemaining] = useState(0);
  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(
      () => setRemaining((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearTimeout(timer);
  }, [remaining]);
  const start = useCallback((seconds: number) => {
    setRemaining(boundedSeconds(seconds));
  }, []);
  return { remaining, start };
}

/** A short-lived status line (e.g. "Copied"); cleared after a few seconds. */
function useTransientNotice() {
  const [notice, setNotice] = useState<TranslationKey | null>(null);
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), COPY_NOTICE_MS);
    return () => window.clearTimeout(timer);
  }, [notice]);
  return [notice, setNotice] as const;
}

async function copyText(text: string) {
  // The Clipboard API may be missing (insecure context) or refused.
  if (!navigator.clipboard?.writeText) return false;
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

/**
 * Renders the `otpauth://` URI as a crisp SVG QR code, generated in the
 * browser: the seed never reaches an image service. The accessible name is
 * generic and never contains the secret.
 */
function TotpQrCode({ uri }: { uri: string }) {
  const { t } = useI18n();
  const qr = useMemo(() => {
    const matrix = encodeQrCode(uri);
    return matrix ? qrSvgPath(matrix) : null;
  }, [uri]);
  if (!qr) {
    return <p className="mfa-enroll__qr-fallback">{t("mfa.qrUnavailable")}</p>;
  }
  return (
    <svg
      aria-label={t("mfa.qrLabel")}
      className="mfa-enroll__qr"
      focusable="false"
      height={qr.viewBoxSize * 4}
      role="img"
      shapeRendering="crispEdges"
      viewBox={`0 0 ${qr.viewBoxSize} ${qr.viewBoxSize}`}
      width={qr.viewBoxSize * 4}
    >
      <rect fill="#fff" height={qr.viewBoxSize} width={qr.viewBoxSize} />
      <path d={qr.path} fill="#000" />
    </svg>
  );
}

interface RecoveryCodesDialogProps {
  codes: readonly string[] | null;
  onClose: () => void;
  /** Receives focus once the dialog closes (its opener may be gone). */
  returnFocusRef: { readonly current: HTMLElement | null };
}

/**
 * Shows a fresh set of recovery codes exactly once. It closes only after the
 * user confirms the codes were saved; closing drops them from memory.
 */
function RecoveryCodesDialog({
  codes,
  onClose,
  returnFocusRef,
}: RecoveryCodesDialogProps) {
  const { t } = useI18n();
  const [acknowledged, setAcknowledged] = useState(false);
  const [notice, setNotice] = useTransientNotice();
  const acknowledgeId = useId();
  const open = codes !== null;

  function close() {
    if (!acknowledged) return;
    setAcknowledged(false);
    setNotice(null);
    onClose();
  }

  async function copyAll() {
    if (!codes) return;
    setNotice(
      (await copyText(codes.join("\n"))) ? "mfa.codesCopied" : "mfa.copyFailed",
    );
  }

  function download() {
    if (!codes) return;
    const blob = new Blob(
      [
        recoveryCodesFileText(codes, {
          heading: t("mfa.codesFileHeading"),
          warning: t("mfa.codesFileWarning"),
        }),
      ],
      { type: "text/plain;charset=utf-8" },
    );
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = RECOVERY_FILE_NAME;
    link.rel = "noopener";
    document.body.append(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }

  return (
    <Dialog
      onOpenChange={(next) => {
        if (!next) close();
      }}
      open={open}
    >
      <DialogContent
        className="mfa-codes"
        hideClose
        // The codes must not disappear by accident before they are saved.
        onEscapeKeyDown={(event) => {
          if (!acknowledged) event.preventDefault();
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault();
          returnFocusRef.current?.focus();
        }}
        onInteractOutside={(event) => event.preventDefault()}
      >
        <DialogTitle>{t("mfa.codesTitle")}</DialogTitle>
        <DialogDescription>{t("mfa.codesDescription")}</DialogDescription>
        <p className="mfa-codes__warning">{t("mfa.codesWarning")}</p>
        <ol aria-label={t("mfa.codesListLabel")} className="mfa-codes__list">
          {(codes ?? []).map((code, index) => (
            <li key={`${index}-${code}`}>
              <code>{code}</code>
            </li>
          ))}
        </ol>
        <div className="mfa-codes__actions">
          <Button
            leadingIcon={<Copy />}
            onClick={() => void copyAll()}
            size="sm"
            variant="secondary"
          >
            {t("mfa.codesCopy")}
          </Button>
          <Button
            leadingIcon={<Download />}
            onClick={download}
            size="sm"
            variant="secondary"
          >
            {t("mfa.codesDownload")}
          </Button>
        </div>
        <p aria-live="polite" className="mfa-codes__notice" role="status">
          {notice ? t(notice) : null}
        </p>
        <div className="mfa-codes__ack">
          <input
            checked={acknowledged}
            id={acknowledgeId}
            onChange={(event) => setAcknowledged(event.target.checked)}
            type="checkbox"
          />
          <label htmlFor={acknowledgeId}>{t("mfa.codesAcknowledge")}</label>
        </div>
        <DialogFooter>
          <Button disabled={!acknowledged} onClick={close}>
            {t("mfa.codesDone")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type ConfirmAction = "regenerate" | "disable";

/**
 * AUTH-09B: set up, confirm, manage and turn off the authenticator app
 * (TOTP) and its recovery codes. The enrollment seed and the one-time codes
 * live only in this component's state; nothing reaches Web Storage, the URL
 * or the query cache.
 */
export function TwoFactorSettingsCard({
  onRequestStepUp,
}: {
  onRequestStepUp: RequestStepUp;
}) {
  const { t } = useI18n();
  const securityQuery = useMySecurityQuery();
  const enroll = useEnrollTOTP();
  const confirm = useConfirmTOTP();
  const regenerate = useRegenerateRecoveryCodes();
  const disable = useDisableTOTP();
  const lock = useLock();
  const [enrollment, setEnrollment] = useState<TOTPEnrollmentResponse | null>(
    null,
  );
  const [code, setCode] = useState("");
  const [codeError, setCodeError] = useState<TranslationKey | null>(null);
  const [codes, setCodes] = useState<readonly string[] | null>(null);
  const [failure, setFailure] = useState<TwoFactorFailure | null>(null);
  const [feedback, setFeedback] = useState<TranslationKey | null>(null);
  const [copyNotice, setCopyNotice] = useTransientNotice();
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(
    null,
  );
  const retryRef = useRef<StepUpRetry | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const setupButtonRef = useRef<HTMLButtonElement>(null);
  const enrollHeadingRef = useRef<HTMLHeadingElement>(null);
  const codeRef = useRef<HTMLInputElement>(null);
  const focusHeadingRef = useRef(false);
  const ids = { secret: useId(), enrollTitle: useId(), remaining: useId() };
  const busy =
    enroll.isPending ||
    confirm.isPending ||
    regenerate.isPending ||
    disable.isPending;

  // The new step takes focus: the QR panel after "Set up".
  useEffect(() => {
    if (enrollment) enrollHeadingRef.current?.focus();
  }, [enrollment]);

  function clearMessages() {
    setFailure(null);
    setFeedback(null);
    setCodeError(null);
  }

  /**
   * Shows a failed action. A step-up refusal opens the shared dialog once;
   * after the step-up the action runs again exactly once.
   */
  function applyOutcome(
    error: unknown,
    retry: StepUpRetry,
    afterStepUp: boolean,
  ) {
    const outcome = classifyTwoFactorError(error);
    if ("conflict" in outcome) {
      // auth_mfa_conflict: another tab changed the factor; reload the truth.
      setEnrollment(null);
      setCode("");
      setFeedback("mfa.conflict");
      void securityQuery.refetch();
      return;
    }
    if ("invalidCode" in outcome) {
      setCode("");
      setCodeError("mfa.codeInvalid");
      codeRef.current?.focus();
      return;
    }
    const next = outcome.failure;
    if (next.kind === "stepUp") {
      retryRef.current = retry;
      if (!afterStepUp) {
        onRequestStepUp(retry, { requireAal2: next.requireAal2 });
      }
    }
    if (next.kind === "rateLimited" || next.kind === "busy") {
      lock.start(next.seconds);
    }
    setFailure(next);
  }

  async function startEnrollment(afterStepUp = false) {
    if (busy || lock.remaining > 0) return;
    clearMessages();
    try {
      const result = await enroll.mutateAsync();
      setCode("");
      setEnrollment(result);
    } catch (error) {
      applyOutcome(error, () => startEnrollment(true), afterStepUp);
    } finally {
      // Drop the seed from the mutation observer right away.
      enroll.reset();
    }
  }

  function cancelEnrollment() {
    setEnrollment(null);
    setCode("");
    setCodeError(null);
    setFailure(null);
    // The setup button returns after this render.
    window.setTimeout(() => setupButtonRef.current?.focus(), 0);
  }

  async function confirmEnrollment(value: string, afterStepUp = false) {
    clearMessages();
    try {
      const result = await confirm.mutateAsync(value);
      setEnrollment(null);
      setCode("");
      setCodes(result.recovery_codes);
      setFeedback("mfa.enabledDone");
    } catch (error) {
      applyOutcome(error, () => confirmEnrollment(value, true), afterStepUp);
    } finally {
      confirm.reset();
    }
  }

  function handleConfirmSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || lock.remaining > 0) return;
    const value = code.replace(/\s+/g, "");
    if (!TOTP_PATTERN.test(value)) {
      setCodeError(value ? "mfa.codeFormat" : "mfa.codeRequired");
      codeRef.current?.focus();
      return;
    }
    void confirmEnrollment(value);
  }

  async function runRegenerate(afterStepUp = false) {
    clearMessages();
    try {
      const result = await regenerate.mutateAsync();
      setConfirmAction(null);
      setCodes(result.recovery_codes);
      setFeedback("mfa.regenerateDone");
    } catch (error) {
      setConfirmAction(null);
      applyOutcome(error, () => runRegenerate(true), afterStepUp);
    } finally {
      regenerate.reset();
    }
  }

  async function runDisable(afterStepUp = false) {
    clearMessages();
    try {
      await disable.mutateAsync();
      setConfirmAction(null);
      focusHeadingRef.current = true;
      setFeedback("mfa.disabledDone");
    } catch (error) {
      setConfirmAction(null);
      applyOutcome(error, () => runDisable(true), afterStepUp);
    } finally {
      disable.reset();
    }
  }

  async function copySecret(secret: string) {
    setCopyNotice(
      (await copyText(secret)) ? "mfa.secretCopied" : "mfa.copyFailed",
    );
  }

  function closeCodes() {
    // The one-time codes leave memory with the state.
    setCodes(null);
  }

  function failureText(value: TwoFactorFailure) {
    switch (value.kind) {
      case "stepUp":
        return value.requireAal2
          ? t("mfa.assuranceRequired")
          : t("mfa.reauthRequired");
      case "rateLimited":
        return value.seconds > 0
          ? t("mfa.rateLimited", { seconds: value.seconds })
          : t("mfa.rateLimitedNoWait");
      case "busy":
        return value.seconds > 0
          ? t("mfa.busy", { seconds: value.seconds })
          : t("mfa.busyNoWait");
      case "network":
        return t("mfa.network");
      default:
        return t("mfa.actionError");
    }
  }

  const data = securityQuery.data;
  const enabled = data?.mfa_enabled === true;
  const remaining = data?.recovery_codes_remaining ?? 0;
  const lockSuffix = (label: string) =>
    lock.remaining > 0
      ? t("security.retryWait", { label, seconds: lock.remaining })
      : label;

  return (
    <section aria-labelledby="security-mfa-title" className="profile-panel">
      <div className="profile-panel__heading">
        <span className="profile-panel__icon" aria-hidden="true">
          <Smartphone />
        </span>
        <div>
          <h2 id="security-mfa-title" ref={headingRef} tabIndex={-1}>
            {t("mfa.cardTitle")}
          </h2>
          <p>{t("mfa.cardDescription")}</p>
        </div>
      </div>

      {securityQuery.isPending && (
        <SkeletonGroup label={t("mfa.loading")}>
          <Skeleton height={24} width="40%" />
          <Skeleton height={40} width="55%" />
        </SkeletonGroup>
      )}

      {securityQuery.isError && (
        <ErrorState
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void securityQuery.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={t("mfa.loadError")}
          title={t("mfa.loadErrorTitle")}
        />
      )}

      {securityQuery.isSuccess && !enabled && !enrollment && (
        <div className="mfa-card__state">
          <StatusBadge tone="neutral">{t("mfa.statusOff")}</StatusBadge>
          <p className="mfa-card__text">{t("mfa.offDescription")}</p>
          <Button
            disabled={lock.remaining > 0}
            leadingIcon={<ShieldCheck />}
            loading={enroll.isPending}
            loadingLabel={t("mfa.settingUp")}
            onClick={() => void startEnrollment()}
            ref={setupButtonRef}
          >
            {lockSuffix(t("mfa.setup"))}
          </Button>
        </div>
      )}

      {securityQuery.isSuccess && !enabled && enrollment && (
        <div
          aria-labelledby={ids.enrollTitle}
          className="mfa-enroll"
          role="group"
        >
          <h3
            className="mfa-enroll__title"
            id={ids.enrollTitle}
            ref={enrollHeadingRef}
            tabIndex={-1}
          >
            {t("mfa.enrollTitle")}
          </h3>
          <ol className="mfa-enroll__steps">
            <li>
              <p>{t("mfa.enrollStepScan")}</p>
              <TotpQrCode uri={enrollment.otpauth_uri} />
            </li>
            <li>
              <p>{t("mfa.enrollStepManual")}</p>
              <dl className="mfa-enroll__facts">
                <div>
                  <dt>{t("mfa.issuerLabel")}</dt>
                  <dd>{enrollment.issuer}</dd>
                </div>
                <div>
                  <dt>{t("mfa.accountLabel")}</dt>
                  <dd>{enrollment.account_name}</dd>
                </div>
                <div>
                  <dt id={ids.secret}>{t("mfa.secretLabel")}</dt>
                  <dd>
                    <code
                      aria-labelledby={ids.secret}
                      className="mfa-enroll__secret"
                    >
                      {formatTotpSecret(enrollment.secret)}
                    </code>
                  </dd>
                </div>
              </dl>
              <Button
                leadingIcon={<Copy />}
                onClick={() => void copySecret(enrollment.secret)}
                size="sm"
                variant="secondary"
              >
                {t("mfa.copySecret")}
              </Button>
              <p aria-live="polite" className="mfa-card__notice" role="status">
                {copyNotice ? t(copyNotice) : null}
              </p>
            </li>
            <li>
              <form
                aria-label={t("mfa.confirmForm")}
                className="mfa-enroll__form"
                noValidate
                onSubmit={handleConfirmSubmit}
              >
                <p>{t("mfa.enrollStepConfirm")}</p>
                <TextField
                  autoComplete="one-time-code"
                  error={codeError ? t(codeError) : undefined}
                  hint={t("mfa.codeHint")}
                  inputMode="numeric"
                  label={t("mfa.codeLabel")}
                  maxLength={6}
                  name="totp-confirm-code"
                  onChange={(event) => {
                    setCode(event.target.value);
                    if (codeError) setCodeError(null);
                  }}
                  ref={codeRef}
                  spellCheck={false}
                  type="text"
                  value={code}
                />
                <div className="mfa-enroll__actions">
                  <Button
                    disabled={lock.remaining > 0}
                    leadingIcon={<ShieldCheck />}
                    loading={confirm.isPending}
                    loadingLabel={t("mfa.confirming")}
                    type="submit"
                  >
                    {lockSuffix(t("mfa.confirm"))}
                  </Button>
                  <Button
                    disabled={confirm.isPending}
                    onClick={cancelEnrollment}
                    variant="secondary"
                  >
                    {t("mfa.cancel")}
                  </Button>
                </div>
              </form>
            </li>
          </ol>
        </div>
      )}

      {securityQuery.isSuccess && enabled && (
        <div className="mfa-card__state">
          <StatusBadge tone="success">{t("mfa.statusOn")}</StatusBadge>
          <p className="mfa-card__text" id={ids.remaining}>
            {t("mfa.remaining", { count: remaining })}
          </p>
          {remaining <= LOW_RECOVERY_CODES && (
            <p
              className={
                remaining === 0
                  ? "mfa-card__warning mfa-card__warning--danger"
                  : "mfa-card__warning"
              }
            >
              {remaining === 0 ? t("mfa.remainingNone") : t("mfa.remainingLow")}
            </p>
          )}
          <div className="mfa-card__actions">
            <Button
              aria-describedby={ids.remaining}
              disabled={busy || lock.remaining > 0}
              leadingIcon={<KeySquare />}
              onClick={() => {
                clearMessages();
                setConfirmAction("regenerate");
              }}
              size="sm"
              variant="secondary"
            >
              {lockSuffix(t("mfa.regenerate"))}
            </Button>
            <Button
              disabled={busy || lock.remaining > 0}
              leadingIcon={<ShieldOff />}
              onClick={() => {
                clearMessages();
                setConfirmAction("disable");
              }}
              size="sm"
              variant="quiet"
            >
              {t("mfa.disable")}
            </Button>
          </div>
        </div>
      )}

      <p
        aria-live="polite"
        className="profile-feedback profile-feedback--success security-sessions__feedback"
        role="status"
      >
        {feedback ? t(feedback) : null}
      </p>

      {failure && (
        <div className="profile-feedback profile-feedback--error" role="alert">
          <span>{failureText(failure)}</span>
          {failure.kind === "stepUp" && (
            <Button
              leadingIcon={<ShieldCheck />}
              onClick={() =>
                onRequestStepUp(retryRef.current ?? undefined, {
                  requireAal2: failure.requireAal2,
                })
              }
              size="sm"
              variant="secondary"
            >
              {t("stepUp.open")}
            </Button>
          )}
        </div>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (busy) return;
          if (!open) setConfirmAction(null);
        }}
        open={confirmAction !== null}
      >
        <DialogContent
          closeLabel={t("security.dialogCloseLabel")}
          onCloseAutoFocus={(event) => {
            // After turning the app off, its buttons are gone: stay in the card.
            if (!focusHeadingRef.current) return;
            focusHeadingRef.current = false;
            event.preventDefault();
            headingRef.current?.focus();
          }}
        >
          <DialogTitle>
            {confirmAction === "disable"
              ? t("mfa.disableTitle")
              : t("mfa.regenerateTitle")}
          </DialogTitle>
          <DialogDescription>
            {confirmAction === "disable"
              ? t("mfa.disableDescription")
              : t("mfa.regenerateDescription")}
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("mfa.cancel")}
              </Button>
            </DialogClose>
            {confirmAction === "disable" ? (
              <Button
                leadingIcon={<ShieldOff />}
                loading={disable.isPending}
                loadingLabel={t("mfa.disabling")}
                onClick={() => void runDisable()}
                variant="danger"
              >
                {t("mfa.disableConfirm")}
              </Button>
            ) : (
              <Button
                leadingIcon={<KeySquare />}
                loading={regenerate.isPending}
                loadingLabel={t("mfa.regenerating")}
                onClick={() => void runRegenerate()}
              >
                {t("mfa.regenerateConfirm")}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <RecoveryCodesDialog
        codes={codes}
        onClose={closeCodes}
        returnFocusRef={headingRef}
      />
    </section>
  );
}
