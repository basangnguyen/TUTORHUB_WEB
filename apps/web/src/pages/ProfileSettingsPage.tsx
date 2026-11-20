import {
  APIRequestError,
  type ExternalIdentity,
  type MyLoginMethodsResponse,
  type MyPasskey,
  type MyPasswordStatusResponse,
  type MyPasswordUpdateRequest,
  type MySession,
  type UserProfile,
} from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
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
  Fingerprint,
  KeyRound,
  Link2,
  LogOut,
  MonitorSmartphone,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  ShieldCheck,
  Smartphone,
  Trash2,
  UserRound,
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
import { useLocation, useNavigate } from "react-router";
import {
  useAuthProvidersQuery,
  useBeginIdentityLink,
  useIdentitiesQuery,
  useLinkProviderIdentity,
  usePasswordStatusQuery,
  useProfileQuery,
  useUnlinkIdentity,
  useUpdateMyPassword,
  useUpdateProfile,
  type LinkableProvider,
} from "../app/profile";
import { useI18n, type TranslationKey } from "../app/i18n";
import { hasAnotherSignInMethod } from "../app/phone";
import {
  PasskeyCeremonyError,
  classifyCeremonyError,
  defaultPasskeyLabel,
  isWebAuthnSupported,
  validatePasskeyLabel,
  type PasskeyLabelProblem,
} from "../app/passkeys";
import {
  stepUpRequirementFor,
  useMyLoginMethodsQuery,
  useMyPasskeysQuery,
  useMyPhoneQuery,
  useMySecurityQuery,
  useMySessionsQuery,
  useRegisterPasskey,
  useRenameMyPasskey,
  useRevokeMyPasskey,
  useRevokeMySession,
  useRevokeOtherSessions,
  useStepUpRequest,
  type StepUpRequestOptions,
  type StepUpRetry,
} from "../app/security";
import { useSession } from "../app/session";
import { StepUpAuthDialog } from "../components/StepUpAuthDialog";
import { PhoneSettingsCard } from "./PhoneSettingsCard";
import { TwoFactorSettingsCard } from "./TwoFactorSettingsCard";
import "./ProfileSettingsPage.css";

type RequestStepUp = (
  retry?: StepUpRetry,
  options?: StepUpRequestOptions,
) => void;

/** Client-side floor only; the server enforces the deployment policy. */
const DEFAULT_PASSWORD_MIN_LENGTH = 8;
/** Upper bound for client-side Retry-After locks; the server stays authoritative. */
const MAX_RETRY_AFTER_SECONDS = 3600;

interface ProfileFormErrors {
  displayName?: string;
  timezone?: string;
}

function isValidTimeZone(timezone: string) {
  try {
    new Intl.DateTimeFormat("en", { timeZone: timezone }).format();
    return true;
  } catch {
    return false;
  }
}

function errorMessage(error: unknown, fallback: string) {
  if (error instanceof APIRequestError) {
    return error.problem?.detail ?? error.message;
  }
  return error instanceof Error ? error.message : fallback;
}

function ProfileDetailsForm({ user }: { user: UserProfile }) {
  const { t } = useI18n();
  const session = useSession();
  const updateProfile = useUpdateProfile();
  const displayNameRef = useRef<HTMLInputElement>(null);
  const timezoneRef = useRef<HTMLInputElement>(null);
  const [displayName, setDisplayName] = useState(user.display_name);
  const [locale, setLocale] = useState<"vi" | "en">(
    user.locale === "en" ? "en" : "vi",
  );
  const [timezone, setTimezone] = useState(user.timezone);
  const [avatarObjectKey, setAvatarObjectKey] = useState<string | null>(
    user.avatar_object_key ?? null,
  );
  const [formErrors, setFormErrors] = useState<ProfileFormErrors>({});
  const [feedback, setFeedback] = useState<string | null>(null);

  const localeOptions = useMemo(
    () => [
      { label: t("profile.localeVietnamese"), value: "vi" },
      { label: t("profile.localeEnglish"), value: "en" },
    ],
    [t],
  );

  function validateForm() {
    const nextErrors: ProfileFormErrors = {};
    const normalizedName = displayName.trim().normalize("NFC");
    const normalizedTimezone = timezone.trim();

    if (!normalizedName) {
      nextErrors.displayName = t("profile.displayNameRequired");
    } else if (normalizedName.length > 120) {
      nextErrors.displayName = t("profile.displayNameTooLong");
    }

    if (!normalizedTimezone) {
      nextErrors.timezone = t("profile.timezoneRequired");
    } else if (
      normalizedTimezone.length > 64 ||
      !isValidTimeZone(normalizedTimezone)
    ) {
      nextErrors.timezone = t("profile.timezoneInvalid");
    }

    setFormErrors(nextErrors);
    if (nextErrors.displayName) {
      displayNameRef.current?.focus();
    } else if (nextErrors.timezone) {
      timezoneRef.current?.focus();
    }
    return Object.keys(nextErrors).length === 0;
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFeedback(null);
    if (!validateForm()) {
      return;
    }

    try {
      const response = await updateProfile.mutateAsync({
        avatar_object_key: avatarObjectKey,
        display_name: displayName.trim().normalize("NFC"),
        locale,
        timezone: timezone.trim(),
      });
      if (session.currentUser) {
        session.replaceCurrentUser({
          ...session.currentUser,
          user: response.user,
        });
      }
      setFeedback(t("profile.saved"));
    } catch {
      // Mutation state renders the actionable error below the form.
    }
  }

  const updateNeedsReauthentication =
    updateProfile.error instanceof APIRequestError &&
    updateProfile.error.status === 401;

  return (
    <form className="profile-form" noValidate onSubmit={handleSubmit}>
      <div className="profile-form__fields">
        <TextField
          autoComplete="name"
          error={formErrors.displayName}
          hint={t("profile.displayNameHint")}
          label={t("profile.displayNameLabel")}
          maxLength={120}
          onChange={(event) => {
            setDisplayName(event.target.value);
            setFormErrors((current) => ({
              ...current,
              displayName: undefined,
            }));
          }}
          ref={displayNameRef}
          value={displayName}
        />
        <SelectField
          ariaLabel={t("profile.localeLabel")}
          label={t("profile.localeLabel")}
          onValueChange={(value) => setLocale(value === "en" ? "en" : "vi")}
          options={localeOptions}
          value={locale}
        />
        <TextField
          autoComplete="off"
          error={formErrors.timezone}
          hint={t("profile.timezoneHint")}
          label={t("profile.timezoneLabel")}
          maxLength={64}
          onChange={(event) => {
            setTimezone(event.target.value);
            setFormErrors((current) => ({
              ...current,
              timezone: undefined,
            }));
          }}
          ref={timezoneRef}
          value={timezone}
        />
      </div>

      <div className="profile-avatar">
        <div>
          <h3>{t("profile.avatarTitle")}</h3>
          <p>{t("profile.avatarDescription")}</p>
        </div>
        <div className="profile-avatar__status">
          <StatusBadge tone={avatarObjectKey ? "success" : "neutral"}>
            {avatarObjectKey
              ? t("profile.avatarPresent")
              : t("profile.avatarEmpty")}
          </StatusBadge>
          {avatarObjectKey && (
            <Button
              onClick={() => setAvatarObjectKey(null)}
              size="sm"
              variant="quiet"
            >
              {t("profile.avatarRemove")}
            </Button>
          )}
        </div>
      </div>

      <div className="profile-form__footer">
        <Button
          leadingIcon={<Save />}
          loading={updateProfile.isPending}
          loadingLabel={t("profile.saving")}
          type="submit"
        >
          {t("profile.save")}
        </Button>
        {feedback && (
          <p
            className="profile-feedback profile-feedback--success"
            role="status"
          >
            {feedback}
          </p>
        )}
      </div>

      {updateProfile.isError && (
        <div className="profile-feedback profile-feedback--error" role="alert">
          <span>
            {updateNeedsReauthentication
              ? t("profile.reauthRequired")
              : errorMessage(updateProfile.error, t("profile.saveError"))}
          </span>
          {updateNeedsReauthentication && (
            <Button
              onClick={() => session.signIn("/app/settings")}
              size="sm"
              variant="secondary"
            >
              {t("auth.signInAgain")}
            </Button>
          )}
        </div>
      )}
    </form>
  );
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

  const start = useCallback((seconds: number | undefined) => {
    setRemaining(
      typeof seconds === "number" && Number.isFinite(seconds) && seconds > 0
        ? Math.min(Math.ceil(seconds), MAX_RETRY_AFTER_SECONDS)
        : 0,
    );
  }, []);

  return { remaining, start };
}

type PasswordField = "current" | "next" | "confirm";
type PasswordFieldErrors = Partial<Record<PasswordField, TranslationKey>>;

type PasswordFailure =
  | { kind: "reauth" }
  | { kind: "stepUp"; requireAal2: boolean }
  | { kind: "verifiedEmail" }
  | { kind: "rateLimited"; seconds?: number }
  | { kind: "busy"; seconds?: number }
  | { kind: "network" }
  | { kind: "generic" };

type PasswordErrorOutcome =
  { field: PasswordField; key: TranslationKey } | { failure: PasswordFailure };

function classifyPasswordError(error: unknown): PasswordErrorOutcome {
  if (!(error instanceof APIRequestError)) {
    return { failure: { kind: "network" } };
  }
  const code = error.problem?.code;
  switch (error.status) {
    case 400:
      if (code === "auth_password_unchanged") {
        return { field: "next", key: "profile.passwordUnchanged" };
      }
      if (code === "auth_password_policy") {
        return { field: "next", key: "profile.passwordPolicy" };
      }
      return { failure: { kind: "generic" } };
    case 401:
      return code === "auth_invalid_current_password"
        ? { field: "current", key: "profile.passwordInvalidCurrent" }
        : { failure: { kind: "reauth" } };
    case 403:
      // `auth_recent_authentication_required` (or a legacy 403 without a
      // code) means the session is older than the 10-minute step-up window;
      // AUTH-09A `auth_assurance_required` needs an AAL2 factor instead.
      return code === "auth_verified_email_required"
        ? { failure: { kind: "verifiedEmail" } }
        : {
            failure: {
              kind: "stepUp",
              requireAal2: stepUpRequirementFor(error)?.requireAal2 === true,
            },
          };
    case 429:
      return {
        failure: { kind: "rateLimited", seconds: error.retryAfterSeconds },
      };
    case 503:
      return { failure: { kind: "busy", seconds: error.retryAfterSeconds } };
    default:
      return { failure: { kind: "generic" } };
  }
}

function passwordMinLength(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.ceil(value)
    : DEFAULT_PASSWORD_MIN_LENGTH;
}

const PASSWORD_FIELD_ORDER: readonly PasswordField[] = [
  "current",
  "next",
  "confirm",
];

interface PasswordFormProps {
  dateFormatter: Intl.DateTimeFormat;
  onRequestStepUp: RequestStepUp;
  status: MyPasswordStatusResponse;
}

function PasswordForm({
  dateFormatter,
  onRequestStepUp,
  status,
}: PasswordFormProps) {
  const { t } = useI18n();
  const session = useSession();
  const providersQuery = useAuthProvidersQuery();
  const updatePassword = useUpdateMyPassword();
  const lock = useRetryLock();
  const disabledNoticeId = useId();
  const currentRef = useRef<HTMLInputElement>(null);
  const nextRef = useRef<HTMLInputElement>(null);
  const confirmRef = useRef<HTMLInputElement>(null);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [fieldErrors, setFieldErrors] = useState<PasswordFieldErrors>({});
  const [failure, setFailure] = useState<PasswordFailure | null>(null);
  const [feedback, setFeedback] = useState<TranslationKey | null>(null);
  const hasPassword = status.has_password;
  const enabled = status.password_enabled;
  const minLength = passwordMinLength(providersQuery.data?.password_min_length);
  const fieldRefs = { current: currentRef, next: nextRef, confirm: confirmRef };

  function clearFieldError(field: PasswordField) {
    setFieldErrors((errors) => {
      if (!errors[field]) return errors;
      const nextErrors = { ...errors };
      delete nextErrors[field];
      return nextErrors;
    });
  }

  function validate() {
    const errors: PasswordFieldErrors = {};
    if (hasPassword && !current) {
      errors.current = "profile.passwordCurrentRequired";
    }
    // Count code points like the server (which counts runes after NFKC).
    if (Array.from(next).length < minLength) {
      errors.next = "profile.passwordTooShort";
    } else if (hasPassword && next === current) {
      errors.next = "profile.passwordUnchanged";
    } else if (confirm !== next) {
      errors.confirm = "profile.passwordMismatch";
    }
    return errors;
  }

  function reauthenticate() {
    // Native-only deployments have no portal to step up through: sign out so
    // the user logs in again on /sign-in, then returns here within 10 minutes.
    if (providersQuery.data?.oidc_default_enabled) {
      session.signIn("/app/settings");
    } else {
      void session.signOut();
    }
  }

  /**
   * Sends one validated payload. After a step-up the same payload is sent
   * again, so the user does not retype the form.
   */
  async function submitPassword(
    payload: MyPasswordUpdateRequest,
    wasSet: boolean,
  ) {
    setFeedback(null);
    setFailure(null);
    try {
      await updatePassword.mutateAsync(payload);
    } catch (error) {
      const outcome = classifyPasswordError(error);
      if ("field" in outcome) {
        if (outcome.field === "current") {
          setCurrent("");
        } else {
          setNext("");
          setConfirm("");
        }
        setFieldErrors({ [outcome.field]: outcome.key });
        fieldRefs[outcome.field].current?.focus();
        return;
      }
      if (
        outcome.failure.kind === "rateLimited" ||
        outcome.failure.kind === "busy"
      ) {
        lock.start(outcome.failure.seconds);
      }
      setFailure(outcome.failure);
      if (outcome.failure.kind === "stepUp") {
        onRequestStepUp(() => submitPassword(payload, wasSet), {
          requireAal2: outcome.failure.requireAal2,
        });
      }
      return;
    }

    // Never keep a password in memory longer than the request needs it.
    setCurrent("");
    setNext("");
    setConfirm("");
    setFeedback(
      wasSet ? "profile.passwordChangedDone" : "profile.passwordSetDone",
    );
  }

  function submitForm() {
    if (!enabled || updatePassword.isPending || lock.remaining > 0) return;
    setFeedback(null);
    setFailure(null);
    const errors = validate();
    setFieldErrors(errors);
    const firstInvalid = PASSWORD_FIELD_ORDER.find((field) => errors[field]);
    if (firstInvalid) {
      fieldRefs[firstInvalid].current?.focus();
      return;
    }
    void submitPassword(
      hasPassword
        ? { current_password: current, new_password: next }
        : { new_password: next },
      hasPassword,
    );
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    submitForm();
  }

  function failureText(value: PasswordFailure) {
    switch (value.kind) {
      case "stepUp":
        return value.requireAal2
          ? t("profile.passwordAssuranceRequired")
          : t("profile.passwordReauthRequired");
      case "reauth":
        return t("profile.passwordReauthRequired");
      case "verifiedEmail":
        return t("profile.passwordVerifiedEmailRequired");
      case "rateLimited":
        return lock.remaining > 0
          ? t("profile.passwordRateLimitedWait", { seconds: lock.remaining })
          : t("profile.passwordRateLimited");
      case "busy":
        return lock.remaining > 0
          ? t("profile.passwordBusyWait", { seconds: lock.remaining })
          : t("profile.passwordBusy");
      case "network":
        return t("profile.passwordNetworkError");
      default:
        return t("profile.passwordSaveError");
    }
  }

  const submitText = hasPassword
    ? t("profile.passwordSubmitChange")
    : t("profile.passwordSubmitSet");
  const fieldError = (field: PasswordField) => {
    const key = fieldErrors[field];
    return key ? t(key, { min: minLength }) : undefined;
  };

  return (
    <>
      <div className="profile-password__status">
        <StatusBadge tone={hasPassword ? "success" : "neutral"}>
          {hasPassword ? t("profile.passwordSet") : t("profile.passwordNotSet")}
        </StatusBadge>
        <p>
          {hasPassword
            ? status.updated_at
              ? t("profile.passwordUpdatedAt", {
                  date: dateFormatter.format(new Date(status.updated_at)),
                })
              : null
            : t("profile.passwordNotSetDescription")}
        </p>
      </div>

      {!enabled && (
        <p className="profile-password__notice" id={disabledNoticeId}>
          {t("profile.passwordDisabled")}
        </p>
      )}

      <form className="profile-form" noValidate onSubmit={handleSubmit}>
        <fieldset
          aria-describedby={enabled ? undefined : disabledNoticeId}
          className="profile-password__fieldset"
          disabled={!enabled}
        >
          <legend className="profile-password__legend">
            {hasPassword
              ? t("profile.passwordFormChangeTitle")
              : t("profile.passwordFormSetTitle")}
          </legend>
          <div className="profile-password__fields">
            {hasPassword && (
              <TextField
                autoComplete="current-password"
                error={fieldError("current")}
                label={t("profile.passwordCurrentLabel")}
                name="current-password"
                onChange={(event) => {
                  setCurrent(event.target.value);
                  clearFieldError("current");
                }}
                ref={currentRef}
                type="password"
                value={current}
              />
            )}
            <TextField
              autoComplete="new-password"
              error={fieldError("next")}
              hint={t("profile.passwordNewHint", { min: minLength })}
              label={t("profile.passwordNewLabel")}
              name="new-password"
              onChange={(event) => {
                setNext(event.target.value);
                clearFieldError("next");
              }}
              ref={nextRef}
              type="password"
              value={next}
            />
            <TextField
              autoComplete="new-password"
              error={fieldError("confirm")}
              label={t("profile.passwordConfirmLabel")}
              name="confirm-new-password"
              onChange={(event) => {
                setConfirm(event.target.value);
                clearFieldError("confirm");
              }}
              ref={confirmRef}
              type="password"
              value={confirm}
            />
          </div>
          <div className="profile-form__footer">
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<KeyRound />}
              loading={updatePassword.isPending}
              loadingLabel={t("profile.passwordSaving")}
              type="submit"
            >
              {lock.remaining > 0
                ? t("profile.passwordSubmitWait", {
                    label: submitText,
                    seconds: lock.remaining,
                  })
                : submitText}
            </Button>
            {feedback && (
              <p
                className="profile-feedback profile-feedback--success"
                role="status"
              >
                {t(feedback)}
              </p>
            )}
          </div>
        </fieldset>

        {failure && (
          <div
            className="profile-feedback profile-feedback--error"
            role="alert"
          >
            <span>{failureText(failure)}</span>
            {failure.kind === "reauth" && (
              <Button onClick={reauthenticate} size="sm" variant="secondary">
                {t("auth.signInAgain")}
              </Button>
            )}
            {failure.kind === "stepUp" && (
              <Button
                leadingIcon={<ShieldCheck />}
                onClick={() =>
                  onRequestStepUp(submitForm, {
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
      </form>
    </>
  );
}

function PasswordSettingsCard({
  dateFormatter,
  onRequestStepUp,
}: {
  dateFormatter: Intl.DateTimeFormat;
  onRequestStepUp: RequestStepUp;
}) {
  const { t } = useI18n();
  const statusQuery = usePasswordStatusQuery();
  const forbidden =
    statusQuery.error instanceof APIRequestError &&
    [401, 403].includes(statusQuery.error.status);
  const retry = (
    <Button
      leadingIcon={<RefreshCw />}
      onClick={() => void statusQuery.refetch()}
      size="sm"
      variant="secondary"
    >
      {t("state.retry")}
    </Button>
  );

  return (
    <section className="profile-panel" aria-labelledby="profile-password-title">
      <div className="profile-panel__heading">
        <span className="profile-panel__icon" aria-hidden="true">
          <KeyRound />
        </span>
        <div>
          <h2 id="profile-password-title">{t("profile.passwordTitle")}</h2>
          <p>{t("profile.passwordDescription")}</p>
        </div>
      </div>

      {statusQuery.isPending && (
        <SkeletonGroup label={t("profile.passwordLoading")}>
          <Skeleton height={28} width="40%" />
          <Skeleton height={120} />
        </SkeletonGroup>
      )}

      {statusQuery.isError &&
        (forbidden ? (
          <ForbiddenState
            actions={retry}
            description={t("profile.passwordForbidden")}
            title={t("profile.passwordForbiddenTitle")}
          />
        ) : (
          <ErrorState
            actions={retry}
            description={t("profile.passwordLoadError")}
            title={t("profile.passwordLoadErrorTitle")}
          />
        ))}

      {statusQuery.isSuccess && (
        <PasswordForm
          dateFormatter={dateFormatter}
          onRequestStepUp={onRequestStepUp}
          status={statusQuery.data}
        />
      )}
    </section>
  );
}

/* ---------- AUTH-04B: security overview, sign-in methods, devices ---------- */

/** Largest delay `setTimeout` accepts. */
const MAX_TIMER_MS = 2_147_483_647;

function boundedRetryAfter(seconds: number | undefined) {
  return typeof seconds === "number" && Number.isFinite(seconds) && seconds > 0
    ? Math.min(Math.ceil(seconds), MAX_RETRY_AFTER_SECONDS)
    : 0;
}

/** Counts down from a value fixed at mount (key the caller to restart). */
function useCountdownFrom(initialSeconds: number) {
  const [remaining, setRemaining] = useState(initialSeconds);

  useEffect(() => {
    if (remaining <= 0) return;
    const timer = window.setTimeout(() => {
      setRemaining((value) => Math.max(0, value - 1));
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  return remaining;
}

/** True while `iso` lies in the future; flips once the instant passes. */
function useStillFuture(iso: string | null | undefined) {
  const [lapsed, setLapsed] = useState<string | null>(null);
  const valid = Boolean(iso) && Number.isFinite(Date.parse(iso ?? ""));

  useEffect(() => {
    if (!iso) return;
    const expiresAt = Date.parse(iso);
    if (!Number.isFinite(expiresAt)) return;
    const timer = window.setTimeout(
      () => setLapsed(iso),
      Math.min(Math.max(0, expiresAt - Date.now()), MAX_TIMER_MS),
    );
    return () => window.clearTimeout(timer);
  }, [iso]);

  return valid && lapsed !== iso;
}

interface SecurityQueryErrorProps {
  description: string;
  error: unknown;
  onRetry: () => void;
  title: string;
}

/** Shared error, forbidden and Retry-After view for the security queries. */
function SecurityQueryError({
  description,
  error,
  onRetry,
  title,
}: SecurityQueryErrorProps) {
  const { t } = useI18n();
  const status = error instanceof APIRequestError ? error.status : 0;
  const waitSeconds =
    status === 429 || status === 503
      ? boundedRetryAfter(
          error instanceof APIRequestError
            ? error.retryAfterSeconds
            : undefined,
        )
      : 0;
  const remaining = useCountdownFrom(waitSeconds);
  const retryLabel = t("state.retry");
  const retry = (
    <Button
      disabled={remaining > 0}
      leadingIcon={<RefreshCw />}
      onClick={onRetry}
      size="sm"
      variant="secondary"
    >
      {remaining > 0
        ? t("security.retryWait", { label: retryLabel, seconds: remaining })
        : retryLabel}
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
  let detail = description;
  if (waitSeconds > 0) {
    detail =
      status === 429
        ? t("security.rateLimited", { seconds: waitSeconds })
        : t("security.busy", { seconds: waitSeconds });
  } else if (!(error instanceof APIRequestError)) {
    detail = t("security.networkError");
  }
  return <ErrorState actions={retry} description={detail} title={title} />;
}

const OAUTH_PROVIDER_NAMES: Readonly<Record<string, string>> = {
  apple: "Apple",
  facebook: "Facebook",
  google: "Google",
  microsoft: "Microsoft",
};

type LoginMethodIdentity = MyLoginMethodsResponse["identities"][number];

/** Google's documented issuer spellings (fallback for servers without a slug). */
const GOOGLE_ISSUERS: ReadonlySet<string> = new Set([
  "https://accounts.google.com",
  "accounts.google.com",
]);

const IDENTITY_PROVIDER_KEYS: Readonly<Record<string, TranslationKey>> = {
  facebook: "profile.identityProviderFacebook",
  google: "profile.identityProviderGoogle",
  microsoft: "profile.identityProviderMicrosoft",
};

/** Federated slugs stored as kind "oidc" identities (AUTH-05/06/07). */
const OIDC_KIND_PROVIDER_SLUGS: ReadonlySet<string> = new Set([
  "facebook",
  "google",
  "microsoft",
]);

/**
 * `/me/identities` names the provider by registry slug (AUTH-06A). Legacy
 * OIDC identities (slug `oidc`) and older servers fall back to the issuer.
 */
function identityProviderLabel(
  identity: Pick<ExternalIdentity, "provider" | "provider_slug">,
  t: (key: TranslationKey, values?: Record<string, string | number>) => string,
) {
  const key = identity.provider_slug
    ? IDENTITY_PROVIDER_KEYS[identity.provider_slug]
    : undefined;
  if (key) return t(key);
  if (GOOGLE_ISSUERS.has(identity.provider)) {
    return t("profile.identityProviderGoogle");
  }
  return identity.provider;
}

function loginMethodLabel(
  identity: LoginMethodIdentity,
  t: (key: TranslationKey, values?: Record<string, string | number>) => string,
) {
  // AUTH-05/06/07 store Google, Facebook and Microsoft as kind "oidc" with
  // their slug.
  const slug = identity.provider_slug;
  if (slug && OIDC_KIND_PROVIDER_SLUGS.has(slug)) {
    return t("security.kindOauth", {
      provider: OAUTH_PROVIDER_NAMES[slug] ?? slug,
    });
  }
  switch (identity.kind) {
    case "email":
      return t("security.kindEmail");
    case "oidc":
      return t("security.kindOidc");
    case "oauth2": {
      if (!slug) return t("security.kindOauthGeneric");
      return t("security.kindOauth", {
        provider: OAUTH_PROVIDER_NAMES[slug] ?? slug,
      });
    }
    case "phone":
      return t("security.kindPhone");
    case "passkey":
      return t("security.kindPasskey");
    default:
      return t("security.kindOauthGeneric");
  }
}

const SESSION_AUTH_METHOD_KEYS: Readonly<Record<string, TranslationKey>> = {
  email_code: "security.authMethodEmailCode",
  fed: "security.authMethodFederated",
  oidc: "security.authMethodFederated",
  passkey: "security.authMethodPasskey",
  password: "security.authMethodPassword",
  // AUTH-10B: a session opened with an SMS code.
  phone_otp: "security.authMethodPhone",
  pwd: "security.authMethodPassword",
  totp: "security.authMethodTotp",
  webauthn: "security.authMethodPasskey",
};

function sessionMethodsText(
  methods: readonly string[],
  t: (key: TranslationKey) => string,
) {
  // Internal method codes never reach the UI: unknown ones get a generic name.
  const labels = methods.map((method) =>
    t(SESSION_AUTH_METHOD_KEYS[method] ?? "security.authMethodUnknown"),
  );
  return [...new Set(labels)].join(", ");
}

/**
 * Renders a translated line whose `{date}` placeholder becomes a semantic
 * <time> element, wherever the language places it.
 */
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

function LoginMethodsList({
  dateFormatter,
}: {
  dateFormatter: Intl.DateTimeFormat;
}) {
  const { t } = useI18n();
  const loginMethodsQuery = useMyLoginMethodsQuery();

  if (loginMethodsQuery.isPending) {
    return (
      <SkeletonGroup label={t("security.methodsLoading")}>
        <Skeleton height={64} />
        <Skeleton height={64} />
      </SkeletonGroup>
    );
  }
  if (loginMethodsQuery.isError) {
    return (
      <SecurityQueryError
        description={t("security.methodsError")}
        error={loginMethodsQuery.error}
        key={loginMethodsQuery.errorUpdatedAt}
        onRetry={() => void loginMethodsQuery.refetch()}
        title={t("security.methodsErrorTitle")}
      />
    );
  }

  const { identities, password } = loginMethodsQuery.data;
  const passkeyCount = loginMethodsQuery.data.passkey_count ?? 0;
  // AUTH-10B: the phone identity is described by its masked number.
  const phone = loginMethodsQuery.data.phone ?? null;
  if (!password.has_password && identities.length === 0 && passkeyCount === 0) {
    return (
      <EmptyState
        description={t("security.methodsEmptyDescription")}
        title={t("security.methodsEmpty")}
      />
    );
  }

  return (
    <ul className="security-methods__list">
      {password.has_password && (
        <li className="security-methods__row">
          <span className="security-methods__marker" aria-hidden="true">
            <KeyRound />
          </span>
          <div className="security-methods__body">
            <strong>{t("security.methodPassword")}</strong>
            <span>
              {password.updated_at ? (
                <DateLine
                  formatter={dateFormatter}
                  template={t("security.methodPasswordChanged")}
                  value={password.updated_at}
                />
              ) : (
                t("security.methodPasswordNoDate")
              )}
            </span>
          </div>
        </li>
      )}
      {passkeyCount > 0 && (
        <li className="security-methods__row">
          <span className="security-methods__marker" aria-hidden="true">
            <Fingerprint />
          </span>
          <div className="security-methods__body">
            <strong>{t("security.methodPasskeys")}</strong>
            <span>
              {t("security.methodPasskeyCount", { count: passkeyCount })}
            </span>
          </div>
        </li>
      )}
      {identities.map((identity) =>
        identity.kind === "phone" ? (
          <li className="security-methods__row" key={identity.id}>
            <span className="security-methods__marker" aria-hidden="true">
              <Smartphone />
            </span>
            <div className="security-methods__body">
              <div className="security-methods__title">
                <strong>{loginMethodLabel(identity, t)}</strong>
                {phone && (
                  <StatusBadge tone={phone.stale ? "warning" : "success"}>
                    {phone.stale
                      ? t("security.phoneStale")
                      : t("security.phoneVerified")}
                  </StatusBadge>
                )}
              </div>
              {phone && <p>{phone.masked_number}</p>}
              <span>
                {phone ? (
                  <DateLine
                    formatter={dateFormatter}
                    template={t("security.methodPhoneLastUsed")}
                    value={phone.last_used_at}
                  />
                ) : identity.last_authenticated_at ? (
                  <DateLine
                    formatter={dateFormatter}
                    template={t("security.methodLastUsed")}
                    value={identity.last_authenticated_at}
                  />
                ) : (
                  t("security.methodNeverUsed")
                )}
              </span>
            </div>
          </li>
        ) : (
          <li className="security-methods__row" key={identity.id}>
            <span className="security-methods__marker" aria-hidden="true">
              <Fingerprint />
            </span>
            <div className="security-methods__body">
              <div className="security-methods__title">
                <strong>{loginMethodLabel(identity, t)}</strong>
                <StatusBadge
                  tone={identity.email_verified ? "success" : "warning"}
                >
                  {identity.email_verified
                    ? t("security.emailVerified")
                    : t("security.emailUnverified")}
                </StatusBadge>
              </div>
              {identity.email_at_provider && (
                <p>{identity.email_at_provider}</p>
              )}
              <span>
                {identity.last_authenticated_at ? (
                  <DateLine
                    formatter={dateFormatter}
                    template={t("security.methodLastUsed")}
                    value={identity.last_authenticated_at}
                  />
                ) : (
                  t("security.methodNeverUsed")
                )}
              </span>
            </div>
          </li>
        ),
      )}
    </ul>
  );
}

function SecurityOverviewCard({
  dateFormatter,
  onRequestStepUp,
  passkeysEnabled,
  phoneEnabled,
}: {
  dateFormatter: Intl.DateTimeFormat;
  onRequestStepUp: RequestStepUp;
  passkeysEnabled: boolean;
  phoneEnabled: boolean;
}) {
  const { language, t } = useI18n();
  const securityQuery = useMySecurityQuery();
  const passkeyCount = securityQuery.data?.passkey_count ?? 0;
  const phone = securityQuery.data?.phone ?? null;
  const recentAuthActive = useStillFuture(
    securityQuery.data?.recent_auth_expires_at,
  );
  const recentHintId = useId();
  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        timeStyle: "short",
      }),
    [language],
  );

  return (
    <section
      aria-labelledby="security-overview-title"
      className="profile-panel"
    >
      <div className="profile-panel__heading">
        <span className="profile-panel__icon" aria-hidden="true">
          <ShieldCheck />
        </span>
        <div>
          <h2 id="security-overview-title">{t("security.overviewTitle")}</h2>
          <p>{t("security.overviewDescription")}</p>
        </div>
      </div>

      {securityQuery.isPending && (
        <SkeletonGroup label={t("security.overviewLoading")}>
          <Skeleton height={24} width="55%" />
          <Skeleton height={24} width="45%" />
          <Skeleton height={24} width="60%" />
        </SkeletonGroup>
      )}

      {securityQuery.isError && (
        <SecurityQueryError
          description={t("security.overviewError")}
          error={securityQuery.error}
          key={securityQuery.errorUpdatedAt}
          onRetry={() => void securityQuery.refetch()}
          title={t("security.overviewErrorTitle")}
        />
      )}

      {securityQuery.isSuccess && (
        <dl className="security-status">
          <div className="security-status__item">
            <dt>{t("security.emailLabel")}</dt>
            <dd>
              <StatusBadge
                tone={securityQuery.data.email_verified ? "success" : "warning"}
              >
                {securityQuery.data.email_verified
                  ? t("security.emailVerified")
                  : t("security.emailUnverified")}
              </StatusBadge>
            </dd>
          </div>
          <div className="security-status__item">
            <dt>{t("security.passwordLabel")}</dt>
            <dd>
              <StatusBadge
                tone={securityQuery.data.has_password ? "success" : "neutral"}
              >
                {securityQuery.data.has_password
                  ? t("security.passwordSet")
                  : t("security.passwordNotSet")}
              </StatusBadge>
            </dd>
          </div>
          {(passkeysEnabled || passkeyCount > 0) && (
            <div className="security-status__item">
              <dt>{t("security.passkeysLabel")}</dt>
              <dd>
                <StatusBadge tone={passkeyCount > 0 ? "success" : "neutral"}>
                  {passkeyCount > 0
                    ? t("security.passkeyCount", { count: passkeyCount })
                    : t("security.passkeyNone")}
                </StatusBadge>
              </dd>
            </div>
          )}
          {(phoneEnabled || phone) && (
            <div className="security-status__item">
              <dt>{t("security.phoneLabel")}</dt>
              <dd>
                {phone ? (
                  <>
                    <StatusBadge tone={phone.stale ? "warning" : "success"}>
                      {phone.stale
                        ? t("security.phoneStale")
                        : t("security.phoneVerified")}
                    </StatusBadge>
                    <span>{phone.masked_number}</span>
                  </>
                ) : (
                  <StatusBadge tone="neutral">
                    {t("security.phoneNone")}
                  </StatusBadge>
                )}
              </dd>
            </div>
          )}
          <div className="security-status__item">
            <dt>{t("security.mfaLabel")}</dt>
            <dd>
              {securityQuery.data.mfa_enabled ? (
                <>
                  <StatusBadge tone="success">
                    {t("security.mfaOn")}
                  </StatusBadge>
                  <StatusBadge
                    tone={
                      (securityQuery.data.recovery_codes_remaining ?? 0) <= 3
                        ? "warning"
                        : "neutral"
                    }
                  >
                    {t("security.mfaRecoveryRemaining", {
                      count: securityQuery.data.recovery_codes_remaining ?? 0,
                    })}
                  </StatusBadge>
                </>
              ) : (
                <StatusBadge tone="neutral">{t("security.mfaOff")}</StatusBadge>
              )}
            </dd>
          </div>
          <div className="security-status__item security-status__item--wide">
            <dt>{t("security.recentAuthLabel")}</dt>
            <dd>
              {recentAuthActive && securityQuery.data.recent_auth_expires_at ? (
                <StatusBadge tone="success">
                  {t("security.recentAuthActive", {
                    time: timeFormatter.format(
                      new Date(securityQuery.data.recent_auth_expires_at),
                    ),
                  })}
                </StatusBadge>
              ) : (
                <>
                  <StatusBadge tone="neutral">
                    {t("security.recentAuthInactive")}
                  </StatusBadge>
                  <Button
                    aria-describedby={recentHintId}
                    leadingIcon={<ShieldCheck />}
                    onClick={() => onRequestStepUp()}
                    size="sm"
                    variant="secondary"
                  >
                    {t("security.confirmNow")}
                  </Button>
                </>
              )}
              <p className="security-status__hint" id={recentHintId}>
                {t("security.recentAuthHint")}
              </p>
            </dd>
          </div>
        </dl>
      )}

      <div className="security-methods">
        <h3>{t("security.methodsTitle")}</h3>
        <LoginMethodsList dateFormatter={dateFormatter} />
      </div>
    </section>
  );
}

type SessionFailure =
  | { kind: "cannotRevokeCurrent" }
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "network" }
  | { kind: "generic" };

function classifySessionError(error: unknown): SessionFailure | "gone" {
  if (!(error instanceof APIRequestError)) return { kind: "network" };
  switch (error.status) {
    case 400:
      return error.problem?.code === "auth_cannot_revoke_current_session"
        ? { kind: "cannotRevokeCurrent" }
        : { kind: "generic" };
    case 404:
      return "gone";
    case 429:
      return {
        kind: "rateLimited",
        seconds: boundedRetryAfter(error.retryAfterSeconds),
      };
    case 503:
      return {
        kind: "busy",
        seconds: boundedRetryAfter(error.retryAfterSeconds),
      };
    default:
      return { kind: "generic" };
  }
}

function useSessionFailureText() {
  const { t } = useI18n();
  return (failure: SessionFailure) => {
    switch (failure.kind) {
      case "cannotRevokeCurrent":
        return t("security.sessionCannotRevokeCurrent");
      case "rateLimited":
        return failure.seconds > 0
          ? t("security.rateLimited", { seconds: failure.seconds })
          : t("security.sessionActionError");
      case "busy":
        return failure.seconds > 0
          ? t("security.busy", { seconds: failure.seconds })
          : t("security.sessionActionError");
      case "network":
        return t("security.networkError");
      default:
        return t("security.sessionActionError");
    }
  };
}

function DeviceSessionsCard({
  dateFormatter,
}: {
  dateFormatter: Intl.DateTimeFormat;
}) {
  const { t } = useI18n();
  const sessionsQuery = useMySessionsQuery();
  const revokeSession = useRevokeMySession();
  const revokeOthers = useRevokeOtherSessions();
  const lock = useRetryLock();
  const failureText = useSessionFailureText();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const noOthersHintId = useId();
  const [feedback, setFeedback] = useState<string | null>(null);
  const [failure, setFailure] = useState<SessionFailure | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [othersFailure, setOthersFailure] = useState<SessionFailure | null>(
    null,
  );

  const sessions = useMemo(() => {
    const list = sessionsQuery.data?.sessions ?? [];
    // Keep the server's recency order but list this device first.
    return [
      ...list.filter((item) => item.is_current),
      ...list.filter((item) => !item.is_current),
    ];
  }, [sessionsQuery.data]);
  const otherCount = sessions.filter((item) => !item.is_current).length;
  const deviceName = (item: MySession) =>
    item.device_label?.trim() || t("security.sessionUnknownDevice");

  /** Returns true when the targeted session no longer exists. */
  function applyFailure(
    error: unknown,
    target: (value: SessionFailure | null) => void,
  ) {
    const outcome = classifySessionError(error);
    if (outcome === "gone") {
      setFeedback(t("security.sessionGone"));
      return true;
    }
    if (outcome.kind === "rateLimited" || outcome.kind === "busy") {
      lock.start(outcome.seconds);
    }
    target(outcome);
    return false;
  }

  async function handleRevoke(item: MySession) {
    if (revokeSession.isPending || lock.remaining > 0) return;
    setFeedback(null);
    setFailure(null);
    try {
      await revokeSession.mutateAsync(item.id);
      setFeedback(t("security.sessionRevoked", { device: deviceName(item) }));
    } catch (error) {
      if (!applyFailure(error, setFailure)) return;
    }
    // The row and its button are gone: keep keyboard focus inside the card.
    headingRef.current?.focus();
  }

  async function confirmRevokeOthers() {
    if (revokeOthers.isPending || lock.remaining > 0) return;
    setFeedback(null);
    setFailure(null);
    setOthersFailure(null);
    try {
      const result = await revokeOthers.mutateAsync();
      setConfirmOpen(false);
      setFeedback(
        result.revoked_count > 0
          ? t("security.revokeOthersDone", { count: result.revoked_count })
          : t("security.revokeOthersNoneDone"),
      );
    } catch (error) {
      applyFailure(error, setOthersFailure);
    }
  }

  const revokeOthersLabel = t("security.revokeOthers");

  return (
    <section
      aria-labelledby="security-sessions-title"
      className="profile-panel"
    >
      <div className="profile-panel__heading profile-panel__heading--actions">
        <span className="profile-panel__icon" aria-hidden="true">
          <MonitorSmartphone />
        </span>
        <div>
          <h2 id="security-sessions-title" ref={headingRef} tabIndex={-1}>
            {t("security.sessionsTitle")}
          </h2>
          <p>{t("security.sessionsDescription")}</p>
        </div>
        {sessionsQuery.isSuccess && (
          <Dialog
            onOpenChange={(open) => {
              if (revokeOthers.isPending) return;
              setConfirmOpen(open);
              if (!open) setOthersFailure(null);
            }}
            open={confirmOpen}
          >
            <div className="security-sessions__bulk">
              <DialogTrigger asChild>
                <Button
                  aria-describedby={
                    otherCount === 0 ? noOthersHintId : undefined
                  }
                  disabled={otherCount === 0 || lock.remaining > 0}
                  leadingIcon={<LogOut />}
                  size="sm"
                  variant="secondary"
                >
                  {lock.remaining > 0
                    ? t("security.retryWait", {
                        label: revokeOthersLabel,
                        seconds: lock.remaining,
                      })
                    : revokeOthersLabel}
                </Button>
              </DialogTrigger>
              {otherCount === 0 && (
                <span id={noOthersHintId}>
                  {t("security.revokeOthersNone")}
                </span>
              )}
            </div>
            <DialogContent closeLabel={t("security.dialogCloseLabel")}>
              <DialogTitle>{t("security.revokeOthersTitle")}</DialogTitle>
              <DialogDescription>
                {t("security.revokeOthersDescription", { count: otherCount })}
              </DialogDescription>
              {othersFailure && (
                <p className="security-sessions__dialog-error" role="alert">
                  {failureText(othersFailure)}
                </p>
              )}
              <DialogFooter>
                <DialogClose asChild>
                  <Button disabled={revokeOthers.isPending} variant="secondary">
                    {t("security.cancel")}
                  </Button>
                </DialogClose>
                <Button
                  disabled={lock.remaining > 0}
                  leadingIcon={<LogOut />}
                  loading={revokeOthers.isPending}
                  loadingLabel={t("security.revokeOthersRunning")}
                  onClick={() => void confirmRevokeOthers()}
                  variant="danger"
                >
                  {t("security.revokeOthersConfirm")}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        )}
      </div>

      {sessionsQuery.isPending && (
        <SkeletonGroup label={t("security.sessionsLoading")}>
          <Skeleton height={76} />
          <Skeleton height={76} />
        </SkeletonGroup>
      )}

      {sessionsQuery.isError && (
        <SecurityQueryError
          description={t("security.sessionsError")}
          error={sessionsQuery.error}
          key={sessionsQuery.errorUpdatedAt}
          onRetry={() => void sessionsQuery.refetch()}
          title={t("security.sessionsErrorTitle")}
        />
      )}

      {sessionsQuery.isSuccess && sessions.length === 0 && (
        <EmptyState
          description={t("security.sessionsEmptyDescription")}
          title={t("security.sessionsEmpty")}
        />
      )}

      {sessions.length > 0 && (
        <ul className="session-list">
          {sessions.map((item) => {
            const name = deviceName(item);
            const methods = sessionMethodsText(item.auth_methods, t);
            const isRevoking =
              revokeSession.isPending && revokeSession.variables === item.id;
            return (
              <li className="session-row" key={item.id}>
                <span className="session-row__marker" aria-hidden="true">
                  <MonitorSmartphone />
                </span>
                <div className="session-row__body">
                  <div className="session-row__title">
                    <strong>{name}</strong>
                    {item.is_current && (
                      <StatusBadge tone="success">
                        {t("security.sessionCurrent")}
                      </StatusBadge>
                    )}
                  </div>
                  {methods && (
                    <p>{t("security.sessionMethods", { methods })}</p>
                  )}
                  <ul className="session-row__meta">
                    <li>
                      <DateLine
                        formatter={dateFormatter}
                        template={t("security.sessionCreated")}
                        value={item.created_at}
                      />
                    </li>
                    <li>
                      <DateLine
                        formatter={dateFormatter}
                        template={t("security.sessionLastSeen")}
                        value={item.last_seen_at}
                      />
                    </li>
                    <li>
                      <DateLine
                        formatter={dateFormatter}
                        template={t(
                          item.persistent
                            ? "security.sessionPersistent"
                            : "security.sessionExpires",
                        )}
                        value={item.expires_at}
                      />
                    </li>
                  </ul>
                </div>
                {!item.is_current && (
                  <div className="session-row__action">
                    <Button
                      aria-label={t("security.sessionRevokeFor", {
                        date: dateFormatter.format(new Date(item.created_at)),
                        device: name,
                      })}
                      disabled={
                        lock.remaining > 0 ||
                        (revokeSession.isPending && !isRevoking)
                      }
                      leadingIcon={<LogOut />}
                      loading={isRevoking}
                      loadingLabel={t("security.sessionRevoking")}
                      onClick={() => void handleRevoke(item)}
                      size="sm"
                      variant="quiet"
                    >
                      {t("security.sessionRevoke")}
                    </Button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}

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
        </div>
      )}
    </section>
  );
}

/* ---------- AUTH-08B: passkeys ---------- */

type PasskeyFailure =
  | { kind: "lastMethod" }
  | { kind: "exists" }
  | { kind: "unsupported" }
  | { kind: "notFound" }
  | { kind: "reauthRequired"; requireAal2: boolean }
  | { kind: "invalidName" }
  | { kind: "rateLimited"; seconds: number }
  | { kind: "busy"; seconds: number }
  | { kind: "network" }
  | { kind: "generic" };

const PASSKEY_LABEL_ERRORS: Readonly<
  Record<PasskeyLabelProblem, TranslationKey>
> = {
  invalid: "passkeys.nameInvalid",
  required: "passkeys.nameRequired",
  tooLong: "passkeys.nameTooLong",
};

/** Server problems and browser ceremony errors; a closed prompt is "cancelled". */
function classifyPasskeyError(error: unknown): PasskeyFailure | "cancelled" {
  if (error instanceof APIRequestError) {
    const code = error.problem?.code;
    switch (error.status) {
      case 400:
        return { kind: "invalidName" };
      case 403: {
        // AUTH-09A: with a second factor enrolled, passkey changes need AAL2.
        const requirement = stepUpRequirementFor(error);
        return requirement
          ? { kind: "reauthRequired", requireAal2: requirement.requireAal2 }
          : { kind: "generic" };
      }
      case 404:
        return { kind: "notFound" };
      case 409:
        if (code === "auth_last_identity") return { kind: "lastMethod" };
        return code === "auth_passkey_exists"
          ? { kind: "exists" }
          : { kind: "generic" };
      case 429:
        return {
          kind: "rateLimited",
          seconds: boundedRetryAfter(error.retryAfterSeconds),
        };
      case 503:
        return {
          kind: "busy",
          seconds: boundedRetryAfter(error.retryAfterSeconds),
        };
      default:
        return { kind: "generic" };
    }
  }
  if (
    error instanceof PasskeyCeremonyError ||
    (typeof DOMException !== "undefined" && error instanceof DOMException)
  ) {
    switch (classifyCeremonyError(error)) {
      case "cancelled":
        return "cancelled";
      case "exists":
        return { kind: "exists" };
      case "unsupported":
        return { kind: "unsupported" };
      default:
        return { kind: "generic" };
    }
  }
  // fetch() rejects with a TypeError when the network fails.
  return { kind: "network" };
}

function usePasskeyFailureText() {
  const { t } = useI18n();
  return (failure: PasskeyFailure) => {
    switch (failure.kind) {
      case "lastMethod":
        return t("passkeys.lastMethod");
      case "exists":
        return t("passkeys.exists");
      case "unsupported":
        return t("passkeys.unsupported");
      case "notFound":
        return t("passkeys.notFound");
      case "reauthRequired":
        return failure.requireAal2
          ? t("passkeys.assuranceRequired")
          : t("passkeys.reauthRequired");
      case "invalidName":
        return t("passkeys.invalidName");
      case "rateLimited":
        return failure.seconds > 0
          ? t("passkeys.rateLimited", { seconds: failure.seconds })
          : t("passkeys.rateLimitedNoWait");
      case "busy":
        return failure.seconds > 0
          ? t("passkeys.busy", { seconds: failure.seconds })
          : t("passkeys.busyNoWait");
      case "network":
        return t("passkeys.network");
      default:
        return t("passkeys.actionError");
    }
  };
}

/**
 * Lists, adds, renames and removes the user's passkeys. Adding, renaming and
 * removing need a recent authentication: a 403
 * `auth_recent_authentication_required` opens the shared step-up dialog and
 * the action runs again exactly once after it succeeds.
 */
function PasskeysCard({
  dateFormatter,
  onRequestStepUp,
}: {
  dateFormatter: Intl.DateTimeFormat;
  onRequestStepUp: RequestStepUp;
}) {
  const { t } = useI18n();
  const passkeysQuery = useMyPasskeysQuery();
  const register = useRegisterPasskey();
  const rename = useRenameMyPasskey();
  const revoke = useRevokeMyPasskey();
  const lock = useRetryLock();
  const failureText = usePasskeyFailureText();
  const [webAuthn] = useState(isWebAuthnSupported);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const addFieldRef = useRef<HTMLInputElement>(null);
  const editFieldRef = useRef<HTMLInputElement>(null);
  const retryRef = useRef<StepUpRetry | null>(null);
  const removedRef = useRef(false);
  const [adding, setAdding] = useState(false);
  const [addLabel, setAddLabel] = useState("");
  const [addError, setAddError] = useState<TranslationKey | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editLabel, setEditLabel] = useState("");
  const [editError, setEditError] = useState<TranslationKey | null>(null);
  const [removeTarget, setRemoveTarget] = useState<MyPasskey | null>(null);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [cancelled, setCancelled] = useState(false);
  const [failure, setFailure] = useState<PasskeyFailure | null>(null);
  const busy = register.isPending || rename.isPending || revoke.isPending;
  const passkeys = passkeysQuery.data?.passkeys ?? [];

  useEffect(() => {
    if (adding) addFieldRef.current?.focus();
  }, [adding]);

  useEffect(() => {
    if (editingId) editFieldRef.current?.focus();
  }, [editingId]);

  function clearMessages() {
    setFeedback(null);
    setCancelled(false);
    setFailure(null);
  }

  /**
   * Shows the outcome of a failed action. A recent-auth refusal requests the
   * step-up (once; a refusal after it only shows the error with its own
   * "Confirm it's you" button).
   */
  function applyFailure(
    error: unknown,
    retry: StepUpRetry,
    afterStepUp: boolean,
  ) {
    const outcome = classifyPasskeyError(error);
    if (outcome === "cancelled") {
      setCancelled(true);
      return;
    }
    if (outcome.kind === "reauthRequired") {
      retryRef.current = retry;
      if (!afterStepUp) {
        onRequestStepUp(retry, { requireAal2: outcome.requireAal2 });
      }
    }
    if (outcome.kind === "notFound") void passkeysQuery.refetch();
    if (outcome.kind === "rateLimited" || outcome.kind === "busy") {
      lock.start(outcome.seconds);
    }
    setFailure(outcome);
  }

  function openAdd() {
    clearMessages();
    setAddLabel(
      defaultPasskeyLabel(navigator.userAgent, t("passkeys.defaultName")),
    );
    setAddError(null);
    setAdding(true);
  }

  function closeAdd() {
    setAdding(false);
    setAddError(null);
    // The form (and its focus) is gone: hand focus back to its opener.
    window.setTimeout(() => addButtonRef.current?.focus(), 0);
  }

  async function runAdd(label: string, afterStepUp = false) {
    if (register.isPending || lock.remaining > 0) return;
    clearMessages();
    try {
      const created = await register.mutateAsync(label);
      setFeedback(t("passkeys.added", { label: created.label }));
      closeAdd();
    } catch (error) {
      applyFailure(error, () => runAdd(label, true), afterStepUp);
    } finally {
      register.reset();
    }
  }

  function handleAddSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const problem = validatePasskeyLabel(addLabel);
    if (problem) {
      setAddError(PASSKEY_LABEL_ERRORS[problem]);
      addFieldRef.current?.focus();
      return;
    }
    void runAdd(addLabel.trim());
  }

  function startEdit(item: MyPasskey) {
    clearMessages();
    setEditingId(item.id);
    setEditLabel(item.label);
    setEditError(null);
  }

  async function runRename(id: string, label: string, afterStepUp = false) {
    if (rename.isPending || lock.remaining > 0) return;
    clearMessages();
    try {
      await rename.mutateAsync({ id, label });
      setEditingId(null);
      setFeedback(t("passkeys.renamed"));
      headingRef.current?.focus();
    } catch (error) {
      applyFailure(error, () => runRename(id, label, true), afterStepUp);
    } finally {
      rename.reset();
    }
  }

  function handleRenameSubmit(event: FormEvent<HTMLFormElement>, id: string) {
    event.preventDefault();
    const problem = validatePasskeyLabel(editLabel);
    if (problem) {
      setEditError(PASSKEY_LABEL_ERRORS[problem]);
      editFieldRef.current?.focus();
      return;
    }
    void runRename(id, editLabel.trim());
  }

  async function runRemove(item: MyPasskey, afterStepUp = false) {
    if (revoke.isPending || lock.remaining > 0) return;
    clearMessages();
    try {
      await revoke.mutateAsync(item.id);
      removedRef.current = true;
      setRemoveTarget(null);
      setFeedback(t("passkeys.removed"));
    } catch (error) {
      // The confirmation already happened; close it so the step-up dialog
      // or the explanation is what the user sees next.
      setRemoveTarget(null);
      applyFailure(error, () => runRemove(item, true), afterStepUp);
    } finally {
      revoke.reset();
    }
  }

  const addLabelText = t("passkeys.add");

  return (
    <section
      aria-labelledby="security-passkeys-title"
      className="profile-panel"
    >
      <div className="profile-panel__heading profile-panel__heading--actions">
        <span className="profile-panel__icon" aria-hidden="true">
          <Fingerprint />
        </span>
        <div>
          <h2 id="security-passkeys-title" ref={headingRef} tabIndex={-1}>
            {t("passkeys.title")}
          </h2>
          <p>{t("passkeys.description")}</p>
        </div>
        {!adding && (
          <Button
            disabled={!webAuthn || busy || lock.remaining > 0}
            leadingIcon={<Plus />}
            onClick={openAdd}
            ref={addButtonRef}
            size="sm"
            variant="secondary"
          >
            {lock.remaining > 0
              ? t("security.retryWait", {
                  label: addLabelText,
                  seconds: lock.remaining,
                })
              : addLabelText}
          </Button>
        )}
      </div>

      {!webAuthn && (
        <p className="passkeys__note">{t("passkeys.unsupportedNote")}</p>
      )}

      {adding && (
        <form
          aria-label={t("passkeys.addTitle")}
          className="passkey-form"
          noValidate
          onSubmit={handleAddSubmit}
        >
          <TextField
            error={addError ? t(addError) : undefined}
            hint={t("passkeys.nameHint")}
            label={t("passkeys.nameLabel")}
            name="passkey-label"
            onChange={(event) => {
              setAddLabel(event.target.value);
              if (addError) setAddError(null);
            }}
            ref={addFieldRef}
            value={addLabel}
          />
          <div className="passkey-form__actions">
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<Fingerprint />}
              loading={register.isPending}
              loadingLabel={t("passkeys.creating")}
              type="submit"
            >
              {t("passkeys.create")}
            </Button>
            <Button
              disabled={register.isPending}
              onClick={closeAdd}
              variant="secondary"
            >
              {t("passkeys.cancel")}
            </Button>
          </div>
        </form>
      )}

      {passkeysQuery.isPending && (
        <SkeletonGroup label={t("passkeys.loading")}>
          <Skeleton height={76} />
          <Skeleton height={76} />
        </SkeletonGroup>
      )}

      {passkeysQuery.isError && (
        <SecurityQueryError
          description={t("passkeys.error")}
          error={passkeysQuery.error}
          key={passkeysQuery.errorUpdatedAt}
          onRetry={() => void passkeysQuery.refetch()}
          title={t("passkeys.errorTitle")}
        />
      )}

      {passkeysQuery.isSuccess && passkeys.length === 0 && (
        <EmptyState
          description={t("passkeys.emptyDescription")}
          title={t("passkeys.empty")}
        />
      )}

      {passkeys.length > 0 && (
        <ul className="session-list passkey-list">
          {passkeys.map((item) => {
            const editing = editingId === item.id;
            return (
              <li className="session-row passkey-row" key={item.id}>
                <span className="session-row__marker" aria-hidden="true">
                  <Fingerprint />
                </span>
                <div className="session-row__body">
                  {editing ? (
                    <form
                      aria-label={t("passkeys.renameFor", {
                        label: item.label,
                      })}
                      className="passkey-form passkey-form--inline"
                      noValidate
                      onSubmit={(event) => handleRenameSubmit(event, item.id)}
                    >
                      <TextField
                        error={editError ? t(editError) : undefined}
                        label={t("passkeys.nameLabel")}
                        name="passkey-rename"
                        onChange={(event) => {
                          setEditLabel(event.target.value);
                          if (editError) setEditError(null);
                        }}
                        ref={editFieldRef}
                        value={editLabel}
                      />
                      <div className="passkey-form__actions">
                        <Button
                          disabled={lock.remaining > 0}
                          leadingIcon={<Save />}
                          loading={rename.isPending}
                          loadingLabel={t("passkeys.saving")}
                          size="sm"
                          type="submit"
                        >
                          {t("passkeys.save")}
                        </Button>
                        <Button
                          disabled={rename.isPending}
                          onClick={() => setEditingId(null)}
                          size="sm"
                          variant="secondary"
                        >
                          {t("passkeys.cancel")}
                        </Button>
                      </div>
                    </form>
                  ) : (
                    <div className="session-row__title">
                      <strong>{item.label}</strong>
                      <StatusBadge
                        tone={item.backup_state ? "success" : "neutral"}
                      >
                        {item.backup_state
                          ? t("passkeys.synced")
                          : t("passkeys.deviceBound")}
                      </StatusBadge>
                    </div>
                  )}
                  <ul className="session-row__meta">
                    <li>
                      <DateLine
                        formatter={dateFormatter}
                        template={t("passkeys.created")}
                        value={item.created_at}
                      />
                    </li>
                    <li>
                      {item.last_used_at ? (
                        <DateLine
                          formatter={dateFormatter}
                          template={t("passkeys.lastUsed")}
                          value={item.last_used_at}
                        />
                      ) : (
                        t("passkeys.neverUsed")
                      )}
                    </li>
                  </ul>
                </div>
                {!editing && (
                  <div className="session-row__action passkey-row__actions">
                    <Button
                      aria-label={t("passkeys.renameFor", {
                        label: item.label,
                      })}
                      disabled={busy || lock.remaining > 0}
                      leadingIcon={<Pencil />}
                      onClick={() => startEdit(item)}
                      size="sm"
                      variant="quiet"
                    >
                      {t("passkeys.rename")}
                    </Button>
                    <Button
                      aria-label={t("passkeys.removeFor", {
                        label: item.label,
                      })}
                      disabled={busy || lock.remaining > 0}
                      leadingIcon={<Trash2 />}
                      onClick={() => {
                        clearMessages();
                        setRemoveTarget(item);
                      }}
                      size="sm"
                      variant="quiet"
                    >
                      {t("passkeys.remove")}
                    </Button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (revoke.isPending) return;
          if (!open) setRemoveTarget(null);
        }}
        open={removeTarget !== null}
      >
        <DialogContent
          closeLabel={t("security.dialogCloseLabel")}
          onCloseAutoFocus={(event) => {
            // The removed row (and its button) is gone: stay in the card.
            if (!removedRef.current) return;
            removedRef.current = false;
            event.preventDefault();
            headingRef.current?.focus();
          }}
        >
          <DialogTitle>{t("passkeys.removeTitle")}</DialogTitle>
          <DialogDescription>
            {t("passkeys.removeDescription", {
              label: removeTarget?.label ?? "",
            })}
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={revoke.isPending} variant="secondary">
                {t("passkeys.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={lock.remaining > 0}
              leadingIcon={<Trash2 />}
              loading={revoke.isPending}
              loadingLabel={t("passkeys.removing")}
              onClick={() => {
                if (removeTarget) void runRemove(removeTarget);
              }}
              variant="danger"
            >
              {t("passkeys.removeConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <p
        aria-live="polite"
        className={
          cancelled
            ? "profile-feedback passkeys__notice security-sessions__feedback"
            : "profile-feedback profile-feedback--success security-sessions__feedback"
        }
        role="status"
      >
        {cancelled ? t("passkeys.createCancelled") : feedback}
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

export function ProfileSettingsPage() {
  const { language, t } = useI18n();
  const profileQuery = useProfileQuery();
  const identitiesQuery = useIdentitiesQuery();
  const providersQuery = useAuthProvidersQuery();
  const beginIdentityLink = useBeginIdentityLink();
  const linkProviderIdentity = useLinkProviderIdentity();
  const unlinkIdentity = useUnlinkIdentity();
  const session = useSession();
  const stepUp = useStepUpRequest();
  const requestStepUp = stepUp.request;
  const identityRetryRef = useRef<StepUpRetry | null>(null);
  const location = useLocation();
  const navigate = useNavigate();
  // The Google/Microsoft/OIDC link callback returns to
  // /app/settings?identity=linked.
  const [identityFeedback, setIdentityFeedback] = useState<string | null>(() =>
    new URLSearchParams(location.search).get("identity") === "linked"
      ? t("profile.identityLinked")
      : null,
  );
  // AUTH-07B: /sign-in forwards a signed-in `?error=email_unverified` here
  // (a provider link whose provider withheld a trusted email, e.g. Microsoft
  // without `xms_edov`). Only the fixed value is read, never echoed.
  const [linkEmailUnverified, setLinkEmailUnverified] = useState(
    () =>
      new URLSearchParams(location.search).get("identity") ===
      "email_unverified",
  );
  // A missing capability flag means an older server: keep the new cards off
  // so the page never calls endpoints that would answer 503.
  const deviceManagement =
    providersQuery.data?.device_management_enabled === true;
  const googleLinkEnabled =
    providersQuery.data?.oauth_providers.includes("google") === true;
  const microsoftLinkEnabled =
    providersQuery.data?.oauth_providers.includes("microsoft") === true;
  // AUTH-08B: the passkey card and its endpoints exist only while enabled.
  const passkeysEnabled = providersQuery.data?.passkeys_enabled === true;
  // AUTH-09B: the authenticator-app card exists only while TOTP is enabled.
  const totpEnabled = providersQuery.data?.mfa_totp_enabled === true;
  const passkeysQuery = useMyPasskeysQuery(passkeysEnabled);
  const passkeyCount = passkeysEnabled
    ? (passkeysQuery.data?.passkeys.length ?? 0)
    : 0;
  // AUTH-10B: the phone card and its endpoints exist only while enabled.
  const phoneEnabled = providersQuery.data?.phone_enabled === true;
  const phoneCountries = providersQuery.data?.phone_allowed_countries ?? [];
  const phoneQuery = useMyPhoneQuery(phoneEnabled);
  // The sign-in inventory (device management) shows the account's own email
  // identity, which signs in with an emailed code or a verified password.
  const securityQuery = useMySecurityQuery(deviceManagement);
  const loginMethodsQuery = useMyLoginMethodsQuery(deviceManagement);
  const emailIdentity = loginMethodsQuery.data?.identities.find(
    (item) => item.kind === "email",
  );
  const anotherSignInMethod = hasAnotherSignInMethod({
    emailSignIn:
      emailIdentity !== undefined &&
      (providersQuery.data?.email_code_enabled === true ||
        (providersQuery.data?.password_enabled === true &&
          emailIdentity.email_verified &&
          loginMethodsQuery.data?.password.has_password === true)),
    // Enrolled passkeys count as a second factor even while sign-in with
    // them is off (the server reads the same rows).
    enrolledPasskeys: securityQuery.data?.passkey_count ?? passkeyCount,
    hasTotp: securityQuery.data?.mfa_enabled === true,
    mfaTotpEnabled: totpEnabled,
    passkeyCount,
    phone: phoneEnabled ? (phoneQuery.data?.phone ?? null) : null,
  });

  const hasIdentityParam = new URLSearchParams(location.search).has("identity");
  useEffect(() => {
    if (!hasIdentityParam) return;
    const params = new URLSearchParams(location.search);
    params.delete("identity");
    const search = params.toString();
    void navigate(
      { hash: location.hash, pathname: location.pathname, search },
      { replace: true },
    );
  }, [
    hasIdentityParam,
    location.hash,
    location.pathname,
    location.search,
    navigate,
  ]);

  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  function handleIdentityError(error: unknown, retry: StepUpRetry) {
    const requirement = stepUpRequirementFor(error);
    if (requirement) {
      identityRetryRef.current = retry;
      requestStepUp(retry, requirement);
    }
  }

  function clearIdentityNotices() {
    setIdentityFeedback(null);
    setLinkEmailUnverified(false);
  }

  async function handleBeginIdentityLink() {
    clearIdentityNotices();
    linkProviderIdentity.reset();
    try {
      const response = await beginIdentityLink.mutateAsync();
      window.location.assign(response.authorization_url);
    } catch (error) {
      // Mutation state renders the actionable error in the identity section.
      handleIdentityError(error, handleBeginIdentityLink);
    }
  }

  /**
   * 403 `auth_recent_authentication_required` opens the shared step-up dialog
   * and retries exactly once after it succeeds; a second refusal only shows
   * the error (with its own "Confirm it's you" button).
   */
  async function handleLinkProvider(
    provider: LinkableProvider,
    afterStepUp = false,
  ) {
    clearIdentityNotices();
    beginIdentityLink.reset();
    unlinkIdentity.reset();
    try {
      const response = await linkProviderIdentity.mutateAsync(provider);
      // Full-page navigation to the provider; the server set the flow cookie.
      window.location.assign(response.authorization_url);
    } catch (error) {
      const retry = () => handleLinkProvider(provider, true);
      const requirement = stepUpRequirementFor(error);
      if (requirement) {
        identityRetryRef.current = retry;
        if (!afterStepUp) requestStepUp(retry, requirement);
      }
    }
  }

  async function handleUnlink(identityID: string) {
    clearIdentityNotices();
    linkProviderIdentity.reset();
    try {
      await unlinkIdentity.mutateAsync(identityID);
      setIdentityFeedback(t("profile.identityUnlinked"));
    } catch (error) {
      handleIdentityError(error, () => handleUnlink(identityID));
    }
  }

  if (profileQuery.isPending) {
    return (
      <main className="page-content profile-settings">
        <SkeletonGroup label={t("profile.loading")}>
          <Skeleton height={32} width="42%" />
          <Skeleton height={18} width="68%" />
          <Skeleton height={320} />
        </SkeletonGroup>
      </main>
    );
  }

  if (profileQuery.isError || !profileQuery.data) {
    return (
      <main className="page-content profile-settings">
        <ErrorState
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void profileQuery.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={errorMessage(profileQuery.error, t("profile.loadError"))}
          title={t("profile.loadErrorTitle")}
        />
      </main>
    );
  }

  const identities = identitiesQuery.data?.identities ?? [];
  const identityError =
    beginIdentityLink.error ??
    linkProviderIdentity.error ??
    unlinkIdentity.error;
  const linkingProvider = linkProviderIdentity.isPending
    ? linkProviderIdentity.variables
    : undefined;
  const identityStepUp = stepUpRequirementFor(identityError);
  const identityNeedsReauthentication = identityStepUp !== null;

  return (
    <main className="page-content profile-settings">
      <header className="page-heading profile-settings__header">
        <p>{t("profile.kicker")}</p>
        <h1>{t("profile.title")}</h1>
        <span>{t("profile.description")}</span>
      </header>

      <div className="profile-settings__grid">
        <section
          className="profile-panel"
          aria-labelledby="profile-details-title"
        >
          <div className="profile-panel__heading">
            <span className="profile-panel__icon" aria-hidden="true">
              <UserRound />
            </span>
            <div>
              <h2 id="profile-details-title">{t("profile.detailsTitle")}</h2>
              <p>{t("profile.detailsDescription")}</p>
            </div>
          </div>

          <ProfileDetailsForm
            key={profileQuery.data.user.id}
            user={profileQuery.data.user}
          />
        </section>

        <section
          className="profile-panel"
          aria-labelledby="profile-identities-title"
        >
          <div className="profile-panel__heading profile-panel__heading--actions">
            <span className="profile-panel__icon" aria-hidden="true">
              <ShieldCheck />
            </span>
            <div>
              <h2 id="profile-identities-title">
                {t("profile.identityTitle")}
              </h2>
              <p>{t("profile.identityDescription")}</p>
            </div>
            <div className="profile-identity-actions">
              {googleLinkEnabled && (
                <Button
                  leadingIcon={<Link2 />}
                  loading={linkingProvider === "google"}
                  loadingLabel={t("profile.identityLinking")}
                  onClick={() => void handleLinkProvider("google")}
                  size="sm"
                  variant="secondary"
                >
                  {t("profile.identityLinkGoogle")}
                </Button>
              )}
              {microsoftLinkEnabled && (
                <Button
                  leadingIcon={<Link2 />}
                  loading={linkingProvider === "microsoft"}
                  loadingLabel={t("profile.identityLinking")}
                  onClick={() => void handleLinkProvider("microsoft")}
                  size="sm"
                  variant="secondary"
                >
                  {t("profile.identityLinkMicrosoft")}
                </Button>
              )}
              <Button
                leadingIcon={<Link2 />}
                loading={beginIdentityLink.isPending}
                loadingLabel={t("profile.identityLinking")}
                onClick={() => void handleBeginIdentityLink()}
                size="sm"
                variant="secondary"
              >
                {t("profile.identityLink")}
              </Button>
            </div>
          </div>

          {identitiesQuery.isPending && (
            <SkeletonGroup label={t("profile.identityLoading")}>
              <Skeleton height={82} />
              <Skeleton height={82} />
            </SkeletonGroup>
          )}

          {identitiesQuery.isError && (
            <ErrorState
              actions={
                <Button
                  onClick={() => void identitiesQuery.refetch()}
                  size="sm"
                  variant="secondary"
                >
                  {t("state.retry")}
                </Button>
              }
              description={errorMessage(
                identitiesQuery.error,
                t("profile.identityLoadError"),
              )}
              title={t("profile.identityLoadErrorTitle")}
            />
          )}

          {identitiesQuery.isSuccess && identities.length === 0 && (
            <EmptyState
              description={t("profile.identityEmptyDescription")}
              title={t("profile.identityEmpty")}
            />
          )}

          {identities.length > 0 && (
            <ul className="identity-list">
              {identities.map((identity) => {
                // An active passkey, a password or a usable phone number is
                // another sign-in method (the server counts every way and
                // stays authoritative: 409 auth_last_identity).
                const isLastIdentity =
                  identities.length === 1 && !anotherSignInMethod;
                const isUnlinking =
                  unlinkIdentity.isPending &&
                  unlinkIdentity.variables === identity.id;
                return (
                  <li className="identity-row" key={identity.id}>
                    <div className="identity-row__marker" aria-hidden="true">
                      <ShieldCheck />
                    </div>
                    <div className="identity-row__body">
                      <div className="identity-row__title">
                        <strong>{identityProviderLabel(identity, t)}</strong>
                        <StatusBadge
                          tone={identity.email_verified ? "success" : "warning"}
                        >
                          {identity.email_verified
                            ? t("profile.identityVerified")
                            : t("profile.identityUnverified")}
                        </StatusBadge>
                      </div>
                      {/* Facebook may not share an email address. */}
                      {identity.email ? <p>{identity.email}</p> : null}
                      <span>
                        {t("profile.identityLastUsed").replace(
                          "{date}",
                          dateFormatter.format(
                            new Date(identity.last_authenticated_at),
                          ),
                        )}
                      </span>
                    </div>
                    <div className="identity-row__action">
                      <Button
                        aria-describedby={
                          isLastIdentity
                            ? `identity-${identity.id}-protection`
                            : undefined
                        }
                        disabled={isLastIdentity}
                        leadingIcon={<Trash2 />}
                        loading={isUnlinking}
                        loadingLabel={t("profile.identityUnlinking")}
                        onClick={() => void handleUnlink(identity.id)}
                        size="sm"
                        variant="quiet"
                      >
                        {t("profile.identityUnlink")}
                      </Button>
                      {isLastIdentity && (
                        <span id={`identity-${identity.id}-protection`}>
                          {t("profile.identityLastProtected")}
                        </span>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          )}

          {identityFeedback && (
            <p
              className="profile-feedback profile-feedback--success"
              role="status"
            >
              {identityFeedback}
            </p>
          )}

          {linkEmailUnverified && (
            <div className="profile-identity-guidance" role="alert">
              <p className="profile-identity-guidance__title">
                {t("profile.identityEmailUnverifiedTitle")}
              </p>
              <p>{t("profile.identityEmailUnverified")}</p>
              {microsoftLinkEnabled && (
                <p>{t("profile.identityEmailUnverifiedMicrosoft")}</p>
              )}
              <p>{t("profile.identityEmailUnverifiedOther")}</p>
              <Button
                leadingIcon={<LogOut />}
                onClick={() => void session.signOut({ returnTo: "/sign-in" })}
                size="sm"
                variant="secondary"
              >
                {t("profile.identityEmailUnverifiedSignOut")}
              </Button>
            </div>
          )}

          {identityError && (
            <div
              className="profile-feedback profile-feedback--error"
              role="alert"
            >
              <span>
                {identityNeedsReauthentication
                  ? identityStepUp?.requireAal2
                    ? t("profile.identityAssuranceRequired")
                    : t("profile.identityReauthRequired")
                  : errorMessage(
                      identityError,
                      t("profile.identityActionError"),
                    )}
              </span>
              {identityNeedsReauthentication && (
                <Button
                  leadingIcon={<ShieldCheck />}
                  onClick={() =>
                    requestStepUp(
                      identityRetryRef.current ?? undefined,
                      identityStepUp ?? undefined,
                    )
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

        <PasswordSettingsCard
          dateFormatter={dateFormatter}
          onRequestStepUp={requestStepUp}
        />

        {deviceManagement && (
          <SecurityOverviewCard
            dateFormatter={dateFormatter}
            onRequestStepUp={requestStepUp}
            passkeysEnabled={passkeysEnabled}
            phoneEnabled={phoneEnabled}
          />
        )}

        {totpEnabled && (
          <TwoFactorSettingsCard onRequestStepUp={requestStepUp} />
        )}

        {deviceManagement && (
          <DeviceSessionsCard dateFormatter={dateFormatter} />
        )}

        {passkeysEnabled && (
          <PasskeysCard
            dateFormatter={dateFormatter}
            onRequestStepUp={requestStepUp}
          />
        )}

        {phoneEnabled && phoneCountries.length > 0 && (
          <PhoneSettingsCard
            allowedCountries={phoneCountries}
            dateFormatter={dateFormatter}
            onRequestStepUp={requestStepUp}
          />
        )}
      </div>

      <StepUpAuthDialog {...stepUp.dialog} returnTo="/app/settings" />
    </main>
  );
}
