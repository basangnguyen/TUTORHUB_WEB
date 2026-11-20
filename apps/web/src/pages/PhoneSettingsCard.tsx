import {
  APIRequestError,
  type AuthSignupChallengeResponse,
} from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  EmptyState,
  ErrorState,
  ForbiddenState,
  SelectField,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import {
  Plus,
  RefreshCw,
  Send,
  ShieldCheck,
  Smartphone,
  Trash2,
} from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  DEFAULT_PHONE_CODE_TTL_SECONDS,
  DEFAULT_PHONE_RESEND_SECONDS,
  PHONE_CODE_LENGTH,
  PHONE_INPUT_MAX_LENGTH,
  boundedPhoneSeconds,
  compactPhoneNumber,
  formatPhoneClock,
  isCompletePhoneCode,
  isPlausiblePhoneNumber,
  phoneCountryLabel,
  phoneCountryOptions,
  sanitizePhoneCode,
  usePhoneCountdown,
} from "../app/phone";
import {
  stepUpRequirementFor,
  useMyPhoneQuery,
  useRemoveMyPhone,
  useResendMyPhoneChallenge,
  useStartMyPhone,
  useVerifyMyPhone,
  type StepUpRequestOptions,
  type StepUpRetry,
} from "../app/security";

type RequestStepUp = (
  retry?: StepUpRetry,
  options?: StepUpRequestOptions,
) => void;

type FlowMode = "add" | "change" | "reverify";
type Stage = "start" | "resend" | "verify" | "remove";

/** AUTH-10A problem contract of the Settings phone endpoints. */
type PhoneFailure =
  | { kind: "invalidNumber" }
  | { kind: "codeInvalid" }
  | { kind: "inUse" }
  | { kind: "lastMethod" }
  | { kind: "reauthRequired"; requireAal2: boolean }
  | { kind: "delivery" }
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "network" }
  | { kind: "generic" };

function classifyPhoneError(error: unknown, stage: Stage): PhoneFailure {
  if (!(error instanceof APIRequestError)) return { kind: "network" };
  const requirement = stepUpRequirementFor(error);
  if (requirement) {
    return { kind: "reauthRequired", requireAal2: requirement.requireAal2 };
  }
  const code = error.problem?.code;
  switch (error.status) {
    case 400:
      if (code === "auth_invalid_phone_number" || stage === "start") {
        return { kind: "invalidNumber" };
      }
      return stage === "remove" ? { kind: "generic" } : { kind: "codeInvalid" };
    case 401:
      // An uncoded 401 is a lost session (handled globally).
      return code === "auth_invalid_verification_code"
        ? { kind: "codeInvalid" }
        : { kind: "generic" };
    case 404:
      return { kind: "delivery" };
    case 409:
      if (code === "auth_phone_in_use") return { kind: "inUse" };
      if (code === "auth_last_identity") return { kind: "lastMethod" };
      return { kind: "generic" };
    case 429:
      return {
        kind: "rateLimited",
        seconds: boundedPhoneSeconds(error.retryAfterSeconds),
      };
    case 503:
      return code === "auth_phone_delivery_unavailable"
        ? { kind: "delivery" }
        : {
            kind: "busy",
            seconds: boundedPhoneSeconds(error.retryAfterSeconds),
          };
    default:
      return { kind: "generic" };
  }
}

function usePhoneFailureText() {
  const { t } = useI18n();
  return (failure: PhoneFailure) => {
    switch (failure.kind) {
      case "invalidNumber":
        return t("phone.invalidNumber");
      case "codeInvalid":
        return t("phone.codeInvalid");
      case "inUse":
        return t("phone.inUse");
      case "lastMethod":
        return t("phone.lastMethod");
      case "reauthRequired":
        return failure.requireAal2
          ? t("phone.assuranceRequired")
          : t("phone.reauthRequired");
      case "delivery":
        return t("phone.delivery");
      case "rateLimited":
        return failure.seconds > 0
          ? t("phone.rateLimited", { seconds: failure.seconds })
          : t("phone.rateLimitedNoWait");
      case "busy":
        return failure.seconds > 0
          ? t("phone.busy", { seconds: failure.seconds })
          : t("phone.busyNoWait");
      case "network":
        return t("phone.network");
      default:
        return t("phone.actionError");
    }
  };
}

/** A translated line whose `{date}` becomes a semantic <time>. */
function DateLine({
  formatter,
  template,
  value,
}: {
  formatter: Intl.DateTimeFormat;
  template: string;
  value: string;
}) {
  const [before, after = ""] = template.split("{date}");
  return (
    <>
      {before}
      <time dateTime={value}>{formatter.format(new Date(value))}</time>
      {after}
    </>
  );
}

/** Error, forbidden and Retry-After view for GET /me/phone. */
function PhoneQueryError({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  const status = error instanceof APIRequestError ? error.status : 0;
  const retry = (
    <Button
      leadingIcon={<RefreshCw />}
      onClick={onRetry}
      size="sm"
      variant="secondary"
    >
      {t("state.retry")}
    </Button>
  );
  if (status === 401 || status === 403) {
    return (
      <ForbiddenState
        actions={retry}
        description={t("security.forbidden")}
        title={t("security.forbiddenTitle")}
      />
    );
  }
  const seconds =
    error instanceof APIRequestError
      ? boundedPhoneSeconds(error.retryAfterSeconds)
      : 0;
  let description = t("phone.error");
  if (!(error instanceof APIRequestError)) {
    description = t("phone.network");
  } else if (status === 429) {
    description =
      seconds > 0
        ? t("phone.rateLimited", { seconds })
        : t("phone.rateLimitedNoWait");
  } else if (status === 503) {
    description =
      seconds > 0 ? t("phone.busy", { seconds }) : t("phone.busyNoWait");
  }
  return (
    <ErrorState
      actions={retry}
      description={description}
      title={t("phone.errorTitle")}
    />
  );
}

interface PendingChallenge {
  country: string;
  destination: string;
  id: string;
  /** Compacted number, kept in memory only to send a new code. */
  number: string;
}

export interface PhoneSettingsCardProps {
  /** `phone_allowed_countries`; the first region is the default. */
  allowedCountries: readonly string[];
  dateFormatter: Intl.DateTimeFormat;
  onRequestStepUp: RequestStepUp;
}

const FLOW_TITLES: Readonly<Record<FlowMode, TranslationKey>> = {
  add: "phone.formAddTitle",
  change: "phone.formChangeTitle",
  reverify: "phone.formReverifyTitle",
};

const FLOW_DONE: Readonly<Record<FlowMode, TranslationKey>> = {
  add: "phone.added",
  change: "phone.changed",
  reverify: "phone.reverified",
};

/**
 * AUTH-10B: adds, changes, re-verifies and removes the account's phone
 * number. Every change needs a recent authentication (403
 * `auth_recent_authentication_required`) and, once a passkey or an
 * authenticator app exists, an AAL2 session (403 `auth_assurance_required`):
 * the shared step-up dialog opens and the action runs again exactly once.
 * A step-up replaces the session (and its pending code), so a refused code
 * check sends a fresh code to the same number instead of reusing the old one.
 */
export function PhoneSettingsCard({
  allowedCountries,
  dateFormatter,
  onRequestStepUp,
}: PhoneSettingsCardProps) {
  const { t } = useI18n();
  const phoneQuery = useMyPhoneQuery();
  const start = useStartMyPhone();
  const resend = useResendMyPhoneChallenge();
  const verify = useVerifyMyPhone();
  const remove = useRemoveMyPhone();
  const failureText = usePhoneFailureText();
  const lock = usePhoneCountdown();
  const expiry = usePhoneCountdown();
  const cooldown = usePhoneCountdown();
  const countries = phoneCountryOptions(allowedCountries);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const numberFieldRef = useRef<HTMLInputElement>(null);
  const codeFieldRef = useRef<HTMLInputElement>(null);
  const retryRef = useRef<StepUpRetry | null>(null);
  const moveFocusRef = useRef<"number" | "code" | "heading" | null>(null);
  const removedRef = useRef(false);
  const [flow, setFlow] = useState<FlowMode | null>(null);
  const [country, setCountry] = useState(() => countries[0] ?? "VN");
  const [number, setNumber] = useState("");
  const [code, setCode] = useState("");
  const [challenge, setChallenge] = useState<PendingChallenge | null>(null);
  const [sentAfterStepUp, setSentAfterStepUp] = useState(false);
  const [numberError, setNumberError] = useState<TranslationKey | null>(null);
  const [codeError, setCodeError] = useState<TranslationKey | null>(null);
  const [failure, setFailure] = useState<PhoneFailure | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [removeOpen, setRemoveOpen] = useState(false);
  const phone = phoneQuery.data?.phone ?? null;
  const busy =
    start.isPending || resend.isPending || verify.isPending || remove.isPending;
  const expired = challenge !== null && expiry.remaining <= 0;
  const step = flow === null ? null : challenge ? "code" : "number";
  const challengeId = challenge?.id ?? null;

  // Move the caret to whatever the user does next (never on first render).
  useEffect(() => {
    const target = moveFocusRef.current;
    if (!target) return;
    moveFocusRef.current = null;
    if (target === "code") codeFieldRef.current?.focus();
    else if (target === "number") numberFieldRef.current?.focus();
    else headingRef.current?.focus();
  }, [step, challengeId, flow]);

  function clearMessages() {
    setFeedback(null);
    setFailure(null);
  }

  function resetChallenge() {
    setChallenge(null);
    setCode("");
    setCodeError(null);
    expiry.reset();
    cooldown.reset();
  }

  function openFlow(mode: FlowMode) {
    clearMessages();
    resetChallenge();
    setNumber("");
    setNumberError(null);
    setSentAfterStepUp(false);
    setCountry(
      mode === "reverify" && phone && countries.includes(phone.country_code)
        ? phone.country_code
        : (countries[0] ?? "VN"),
    );
    moveFocusRef.current = "number";
    setFlow(mode);
  }

  function closeFlow() {
    resetChallenge();
    setNumber("");
    setNumberError(null);
    moveFocusRef.current = "heading";
    setFlow(null);
  }

  /** A refused action: show it, and request the step-up once. */
  function applyFailure(
    error: unknown,
    stage: Stage,
    retry: StepUpRetry,
    afterStepUp: boolean,
  ) {
    const outcome = classifyPhoneError(error, stage);
    if (outcome.kind === "invalidNumber") {
      setNumberError("phone.invalidNumber");
      numberFieldRef.current?.focus();
      return;
    }
    if (outcome.kind === "codeInvalid") {
      setCodeError("phone.codeInvalid");
      codeFieldRef.current?.focus();
      return;
    }
    if (outcome.kind === "reauthRequired") {
      retryRef.current = retry;
      if (!afterStepUp) {
        onRequestStepUp(retry, { requireAal2: outcome.requireAal2 });
      }
    }
    if (outcome.kind === "inUse") {
      // The server ended the attempt: another number is the only way on.
      resetChallenge();
      moveFocusRef.current = "number";
    }
    if (outcome.kind === "rateLimited" || outcome.kind === "busy") {
      if (outcome.seconds > 0) lock.start(outcome.seconds);
    }
    setFailure(outcome);
  }

  function applyChallenge(
    result: AuthSignupChallengeResponse,
    target: { country: string; number: string },
    afterStepUp: boolean,
  ) {
    moveFocusRef.current = "code";
    setChallenge((current) => ({
      country: target.country,
      destination: result.masked_destination ?? current?.destination ?? "",
      id: result.challenge_id,
      number: target.number,
    }));
    setSentAfterStepUp(afterStepUp);
    setCode("");
    setCodeError(null);
    expiry.start(result.expires_in_seconds || DEFAULT_PHONE_CODE_TTL_SECONDS);
    cooldown.start(
      result.resend_after_seconds ??
        result.cooldown_seconds ??
        DEFAULT_PHONE_RESEND_SECONDS,
    );
  }

  async function runStart(
    target: { country: string; number: string },
    afterStepUp = false,
  ) {
    if (start.isPending || lock.remaining > 0) return;
    clearMessages();
    try {
      const result = await start.mutateAsync({
        country_code: target.country,
        phone_number: target.number,
      });
      applyChallenge(result, target, afterStepUp);
    } catch (error) {
      applyFailure(error, "start", () => runStart(target, true), afterStepUp);
    } finally {
      start.reset();
    }
  }

  async function runResend() {
    if (
      !challenge ||
      resend.isPending ||
      cooldown.remaining > 0 ||
      lock.remaining > 0
    ) {
      return;
    }
    const target = { country: challenge.country, number: challenge.number };
    clearMessages();
    try {
      const result = await resend.mutateAsync();
      applyChallenge(result, target, false);
    } catch (error) {
      resend.reset();
      if (
        error instanceof APIRequestError &&
        (error.status === 400 ||
          (error.status === 401 &&
            error.problem?.code === "auth_invalid_verification_code"))
      ) {
        // The attempt ended (TTL, attempts or a new session): start again.
        await runStart(target);
        return;
      }
      applyFailure(error, "resend", () => runStart(target, true), false);
    } finally {
      resend.reset();
    }
  }

  async function runVerify(value: string, mode: FlowMode) {
    if (!challenge || verify.isPending || lock.remaining > 0) return;
    const target = { country: challenge.country, number: challenge.number };
    const fallback = challenge.destination;
    clearMessages();
    try {
      const result = await verify.mutateAsync({
        challenge_id: challenge.id,
        code: value,
      });
      setFeedback(
        t(FLOW_DONE[mode], {
          number: result.phone?.masked_number ?? fallback,
        }),
      );
      closeFlow();
    } catch (error) {
      // A step-up replaces the session, so the pending code cannot be
      // reused: once confirmed, a new code goes to the same number.
      applyFailure(error, "verify", () => runStart(target, true), false);
    } finally {
      verify.reset();
    }
  }

  async function runRemove(afterStepUp = false) {
    if (remove.isPending || lock.remaining > 0) return;
    clearMessages();
    try {
      await remove.mutateAsync();
      removedRef.current = true;
      setRemoveOpen(false);
      setFeedback(t("phone.removed"));
    } catch (error) {
      // The confirmation already happened; close it so the step-up dialog
      // or the explanation is what the user sees next.
      setRemoveOpen(false);
      applyFailure(error, "remove", () => runRemove(true), afterStepUp);
    } finally {
      remove.reset();
    }
  }

  function handleNumberSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const compact = compactPhoneNumber(number);
    if (!compact) {
      setNumberError("phone.numberRequired");
      numberFieldRef.current?.focus();
      return;
    }
    if (!isPlausiblePhoneNumber(compact)) {
      setNumberError("phone.invalidNumber");
      numberFieldRef.current?.focus();
      return;
    }
    setNumberError(null);
    void runStart({ country, number: compact });
  }

  function handleCodeSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!flow) return;
    const value = sanitizePhoneCode(code);
    let error: TranslationKey | null = null;
    if (expired) error = "phone.codeExpired";
    else if (!value) error = "phone.codeRequired";
    else if (!isCompletePhoneCode(value)) error = "phone.codeFormat";
    if (error) {
      setCodeError(error);
      codeFieldRef.current?.focus();
      return;
    }
    // The typed code never outlives its submission.
    setCode("");
    setCodeError(null);
    void runVerify(value, flow);
  }

  const lockSuffix = (label: string) =>
    lock.remaining > 0
      ? t("security.retryWait", { label, seconds: lock.remaining })
      : label;
  const countryOptions = countries.map((option) => ({
    label: phoneCountryLabel(option),
    value: option,
  }));

  return (
    <section aria-labelledby="security-phone-title" className="profile-panel">
      <div className="profile-panel__heading profile-panel__heading--actions">
        <span className="profile-panel__icon" aria-hidden="true">
          <Smartphone />
        </span>
        <div>
          <h2 id="security-phone-title" ref={headingRef} tabIndex={-1}>
            {t("phone.title")}
          </h2>
          <p>{t("phone.description")}</p>
        </div>
        {phoneQuery.isSuccess && phone === null && flow === null && (
          <Button
            disabled={busy || lock.remaining > 0}
            leadingIcon={<Plus />}
            onClick={() => openFlow("add")}
            size="sm"
            variant="secondary"
          >
            {lockSuffix(t("phone.add"))}
          </Button>
        )}
      </div>

      {phoneQuery.isPending && (
        <SkeletonGroup label={t("phone.loading")}>
          <Skeleton height={76} />
        </SkeletonGroup>
      )}

      {phoneQuery.isError && (
        <PhoneQueryError
          error={phoneQuery.error}
          key={phoneQuery.errorUpdatedAt}
          onRetry={() => void phoneQuery.refetch()}
        />
      )}

      {phoneQuery.isSuccess && phone === null && flow === null && (
        <EmptyState
          description={t("phone.emptyDescription")}
          title={t("phone.empty")}
        />
      )}

      {phone && (
        <ul className="session-list phone-list">
          <li className="session-row phone-row">
            <span className="session-row__marker" aria-hidden="true">
              <Smartphone />
            </span>
            <div className="session-row__body">
              <div className="session-row__title">
                <strong className="phone-row__number">
                  {phone.masked_number}
                </strong>
                <StatusBadge tone={phone.stale ? "warning" : "success"}>
                  {phone.stale ? t("phone.stale") : t("phone.verified")}
                </StatusBadge>
              </div>
              {phone.stale && (
                <p className="phone-row__warning">
                  {t("phone.staleExplanation")}
                </p>
              )}
              <ul className="session-row__meta">
                <li>
                  {t("phone.country", {
                    country: phoneCountryLabel(
                      phone.country_code,
                      phone.calling_code,
                    ),
                  })}
                </li>
                <li>
                  <DateLine
                    formatter={dateFormatter}
                    template={t("phone.verifiedAt")}
                    value={phone.verified_at}
                  />
                </li>
                <li>
                  <DateLine
                    formatter={dateFormatter}
                    template={t("phone.lastUsedAt")}
                    value={phone.last_used_at}
                  />
                </li>
              </ul>
            </div>
            {flow === null && (
              <div className="session-row__action passkey-row__actions">
                {phone.stale && (
                  <Button
                    disabled={busy || lock.remaining > 0}
                    leadingIcon={<ShieldCheck />}
                    onClick={() => openFlow("reverify")}
                    size="sm"
                  >
                    {t("phone.reverify")}
                  </Button>
                )}
                <Button
                  disabled={busy || lock.remaining > 0}
                  leadingIcon={<RefreshCw />}
                  onClick={() => openFlow("change")}
                  size="sm"
                  variant="secondary"
                >
                  {t("phone.change")}
                </Button>
                <Button
                  disabled={busy || lock.remaining > 0}
                  leadingIcon={<Trash2 />}
                  onClick={() => {
                    clearMessages();
                    setRemoveOpen(true);
                  }}
                  size="sm"
                  variant="quiet"
                >
                  {t("phone.remove")}
                </Button>
              </div>
            )}
          </li>
        </ul>
      )}

      {flow && step === "number" && (
        <form
          aria-label={t(FLOW_TITLES[flow])}
          className="passkey-form phone-form"
          noValidate
          onSubmit={handleNumberSubmit}
        >
          {flow === "reverify" && phone && (
            <p className="phone-form__note">
              {t("phone.reverifyHint", { number: phone.masked_number })}
            </p>
          )}
          <div className="phone-form__row">
            <SelectField
              ariaLabel={t("phone.countryLabel")}
              label={t("phone.countryLabel")}
              name="phone-country"
              onValueChange={setCountry}
              options={countryOptions}
              value={country}
            />
            <TextField
              autoComplete="tel"
              error={numberError ? t(numberError) : undefined}
              hint={t("phone.numberHint")}
              inputMode="tel"
              label={t("phone.numberLabel")}
              maxLength={PHONE_INPUT_MAX_LENGTH}
              name="phone-number"
              onChange={(event) => {
                setNumber(event.target.value);
                if (numberError) setNumberError(null);
              }}
              ref={numberFieldRef}
              spellCheck={false}
              type="tel"
              value={number}
            />
          </div>
          <div className="passkey-form__actions">
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<Send />}
              loading={start.isPending}
              loadingLabel={t("phone.sending")}
              type="submit"
            >
              {lockSuffix(t("phone.sendCode"))}
            </Button>
            <Button
              disabled={start.isPending}
              onClick={closeFlow}
              variant="secondary"
            >
              {t("phone.cancel")}
            </Button>
          </div>
        </form>
      )}

      {flow && step === "code" && challenge && (
        <form
          aria-label={t(FLOW_TITLES[flow])}
          className="passkey-form phone-form"
          noValidate
          onSubmit={handleCodeSubmit}
        >
          <p aria-live="polite" className="phone-form__note">
            {expired
              ? t("phone.codeExpired")
              : t(
                  sentAfterStepUp
                    ? "phone.codeSentAfterStepUp"
                    : "phone.codeSent",
                  { destination: challenge.destination },
                )}
          </p>
          {!expired && (
            // Static text: never a live region, so the clock is not re-read.
            <p className="phone-form__clock">
              {t("phone.codeExpiresIn", {
                time: formatPhoneClock(expiry.remaining),
              })}
            </p>
          )}
          <TextField
            autoComplete="one-time-code"
            error={codeError ? t(codeError) : undefined}
            inputMode="numeric"
            label={t("phone.codeLabel")}
            maxLength={PHONE_CODE_LENGTH}
            name="phone-code"
            onChange={(event) => {
              setCode(sanitizePhoneCode(event.target.value));
              if (codeError) setCodeError(null);
            }}
            ref={codeFieldRef}
            value={code}
          />
          <div className="passkey-form__actions">
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<ShieldCheck />}
              loading={verify.isPending || start.isPending}
              loadingLabel={t("phone.verifying")}
              type="submit"
            >
              {lockSuffix(t("phone.verify"))}
            </Button>
            <Button
              disabled={cooldown.remaining > 0 || lock.remaining > 0 || busy}
              leadingIcon={<RefreshCw />}
              loading={resend.isPending}
              loadingLabel={t("phone.sending")}
              onClick={() => void runResend()}
              variant="secondary"
            >
              {cooldown.remaining > 0
                ? t("phone.resendWait", { seconds: cooldown.remaining })
                : t("phone.resend")}
            </Button>
            <Button
              disabled={busy}
              onClick={() => {
                clearMessages();
                resetChallenge();
                moveFocusRef.current = "number";
              }}
              variant="quiet"
            >
              {t("phone.otherNumber")}
            </Button>
            <Button disabled={busy} onClick={closeFlow} variant="quiet">
              {t("phone.cancel")}
            </Button>
          </div>
        </form>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (remove.isPending) return;
          setRemoveOpen(open);
        }}
        open={removeOpen}
      >
        <DialogContent
          closeLabel={t("security.dialogCloseLabel")}
          onCloseAutoFocus={(event) => {
            // The number (and its buttons) is gone: stay in the card.
            if (!removedRef.current) return;
            removedRef.current = false;
            event.preventDefault();
            headingRef.current?.focus();
          }}
        >
          <DialogTitle>{t("phone.removeTitle")}</DialogTitle>
          <DialogDescription>
            {t("phone.removeDescription", {
              number: phone?.masked_number ?? "",
            })}
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={remove.isPending} variant="secondary">
                {t("phone.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<Trash2 />}
              loading={remove.isPending}
              loadingLabel={t("phone.removing")}
              onClick={() => void runRemove()}
              variant="danger"
            >
              {t("phone.removeConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <p
        aria-live="polite"
        className="profile-feedback profile-feedback--success security-sessions__feedback"
        role="status"
      >
        {feedback}
      </p>

      {failure && (
        <div className="profile-feedback profile-feedback--error" role="alert">
          <span>{failureText(failure)}</span>
          {failure.kind === "reauthRequired" && (
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
    </section>
  );
}
