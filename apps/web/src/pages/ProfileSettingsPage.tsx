import {
  APIRequestError,
  type MyPasswordStatusResponse,
  type UserProfile,
} from "@tutorhub/api-client";
import {
  Button,
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
  KeyRound,
  Link2,
  RefreshCw,
  Save,
  ShieldCheck,
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
import {
  useAuthProvidersQuery,
  useBeginIdentityLink,
  useIdentitiesQuery,
  usePasswordStatusQuery,
  useProfileQuery,
  useUnlinkIdentity,
  useUpdateMyPassword,
  useUpdateProfile,
} from "../app/profile";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import "./ProfileSettingsPage.css";

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
      // Without a code, 403 means the session is older than the step-up window.
      return code === "auth_verified_email_required"
        ? { failure: { kind: "verifiedEmail" } }
        : { failure: { kind: "reauth" } };
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
  status: MyPasswordStatusResponse;
}

function PasswordForm({ dateFormatter, status }: PasswordFormProps) {
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

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
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

    const wasSet = hasPassword;
    try {
      await updatePassword.mutateAsync(
        wasSet
          ? { current_password: current, new_password: next }
          : { new_password: next },
      );
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

  function failureText(value: PasswordFailure) {
    switch (value.kind) {
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
          </div>
        )}
      </form>
    </>
  );
}

function PasswordSettingsCard({
  dateFormatter,
}: {
  dateFormatter: Intl.DateTimeFormat;
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
        <PasswordForm dateFormatter={dateFormatter} status={statusQuery.data} />
      )}
    </section>
  );
}

export function ProfileSettingsPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const profileQuery = useProfileQuery();
  const identitiesQuery = useIdentitiesQuery();
  const beginIdentityLink = useBeginIdentityLink();
  const unlinkIdentity = useUnlinkIdentity();
  const [identityFeedback, setIdentityFeedback] = useState<string | null>(null);

  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  async function handleBeginIdentityLink() {
    setIdentityFeedback(null);
    try {
      const response = await beginIdentityLink.mutateAsync();
      window.location.assign(response.authorization_url);
    } catch {
      // Mutation state renders the actionable error in the identity section.
    }
  }

  async function handleUnlink(identityID: string) {
    setIdentityFeedback(null);
    try {
      await unlinkIdentity.mutateAsync(identityID);
      setIdentityFeedback(t("profile.identityUnlinked"));
    } catch {
      // Mutation state renders the actionable error in the identity section.
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
  const identityError = beginIdentityLink.error ?? unlinkIdentity.error;
  const identityNeedsReauthentication =
    identityError instanceof APIRequestError && identityError.status === 401;

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
                const isLastIdentity = identities.length === 1;
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
                        <strong>{identity.provider}</strong>
                        <StatusBadge
                          tone={identity.email_verified ? "success" : "warning"}
                        >
                          {identity.email_verified
                            ? t("profile.identityVerified")
                            : t("profile.identityUnverified")}
                        </StatusBadge>
                      </div>
                      <p>{identity.email}</p>
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

          {identityError && (
            <div
              className="profile-feedback profile-feedback--error"
              role="alert"
            >
              <span>
                {identityNeedsReauthentication
                  ? t("profile.reauthRequired")
                  : errorMessage(
                      identityError,
                      t("profile.identityActionError"),
                    )}
              </span>
              {identityNeedsReauthentication && (
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
        </section>

        <PasswordSettingsCard dateFormatter={dateFormatter} />
      </div>
    </main>
  );
}
