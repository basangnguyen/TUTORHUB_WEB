import { APIRequestError, type UserProfile } from "@tutorhub/api-client";
import {
  Button,
  EmptyState,
  ErrorState,
  SelectField,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import {
  Link2,
  RefreshCw,
  Save,
  ShieldCheck,
  Trash2,
  UserRound,
} from "lucide-react";
import { useMemo, useRef, useState, type FormEvent } from "react";
import {
  useBeginIdentityLink,
  useIdentitiesQuery,
  useProfileQuery,
  useUnlinkIdentity,
  useUpdateProfile,
} from "../app/profile";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";

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
      </div>
    </main>
  );
}
