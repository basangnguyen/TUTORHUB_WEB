import {
  APIRequestError,
  type MembershipInvitationPreview,
} from "@tutorhub/api-client";
import {
  Button,
  ErrorState,
  OfflineState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
} from "@tutorhub/ui";
import { CheckCircle2, LogIn, RefreshCw, UserPlus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
  useAcceptMembershipInvitation,
  useMembershipInvitationPreview,
} from "../app/invitations";
import {
  clearFragmentTokenEscrow,
  consumeFragmentToken,
} from "../app/fragmentToken";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import { useWorkspaceActions } from "../app/workspaces";

const membershipInvitationEscrowKey = "membership-invitation";

function useOnlineStatus() {
  const [isOnline, setIsOnline] = useState(() => navigator.onLine);

  useEffect(() => {
    const markOnline = () => setIsOnline(true);
    const markOffline = () => setIsOnline(false);
    window.addEventListener("online", markOnline);
    window.addEventListener("offline", markOffline);
    return () => {
      window.removeEventListener("online", markOnline);
      window.removeEventListener("offline", markOffline);
    };
  }, []);

  return {
    isOnline,
    refresh: () => setIsOnline(navigator.onLine),
  };
}

function previewRoleKey(
  role: MembershipInvitationPreview["intended_role"],
): TranslationKey {
  if (role === "teacher") {
    return "shell.role.teacher";
  }
  if (role === "student") {
    return "shell.role.student";
  }
  return "shell.role.guest";
}

function publicInvitationErrorKey(error: Error | null): TranslationKey {
  if (
    error instanceof APIRequestError &&
    error.problem?.code === "feature_disabled"
  ) {
    return "capabilities.reasonFeatureDisabled";
  }
  if (
    error instanceof APIRequestError &&
    error.problem?.code === "quota_exceeded"
  ) {
    return error.status === 429
      ? "capabilities.reasonRateLimited"
      : "capabilities.reasonQuotaExhausted";
  }
  if (isAccountMismatch(error)) {
    return "invitation.publicMismatch";
  }
  if (
    error instanceof APIRequestError &&
    [404, 409, 410].includes(error.status)
  ) {
    return "invitation.publicUnavailableDescription";
  }
  if (error instanceof APIRequestError && error.status === 401) {
    return "invitation.publicSessionExpired";
  }
  return "invitation.publicAcceptError";
}

function isAccountMismatch(error: Error | null) {
  return (
    error instanceof APIRequestError &&
    error.status === 403 &&
    error.problem?.title === "Invitation identity mismatch"
  );
}

function isTerminalInvitationError(error: Error | null) {
  return (
    error instanceof APIRequestError && [404, 409, 410].includes(error.status)
  );
}

export function MembershipInvitationPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const navigate = useNavigate();
  const [token] = useState(() =>
    consumeFragmentToken(membershipInvitationEscrowKey),
  );
  const { isOnline, refresh: refreshOnlineStatus } = useOnlineStatus();
  const preview = useMembershipInvitationPreview(token, isOnline);
  const acceptInvitation = useAcceptMembershipInvitation(token);
  const previewDataConcealed =
    preview.isError &&
    preview.error instanceof APIRequestError &&
    [400, 404, 410].includes(preview.error.status);
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "long",
        timeStyle: "short",
      }),
    [language],
  );

  useEffect(
    () => () => {
      clearFragmentTokenEscrow(membershipInvitationEscrowKey);
    },
    [],
  );

  const accept = async () => {
    if (!preview.data) {
      return;
    }
    try {
      const response = await acceptInvitation.mutateAsync();
      navigate("/invite/accepted", {
        replace: true,
        state: {
          tenantID: response.invitation.tenant_id,
          tenantName: preview.data.tenant_name,
        },
      });
    } catch {
      // The mutation exposes a recoverable error below the invitation facts.
    }
  };

  return (
    <main className="membership-invitation-page">
      <section
        aria-labelledby="membership-invitation-title"
        className="membership-invitation-card"
      >
        <div className="membership-invitation-card__brand" aria-hidden="true">
          TH
        </div>
        <p className="membership-invitation-card__kicker">
          {t("brand.product")}
        </p>
        <h1 id="membership-invitation-title">{t("invitation.publicTitle")}</h1>

        {!token && <InvalidInvitationState />}

        {token && !isOnline && (
          <OfflineState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={refreshOnlineStatus}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("invitation.publicOfflineDescription")}
            title={t("state.offlineTitle")}
          />
        )}

        {token && isOnline && preview.isPending && (
          <SkeletonGroup label={t("invitation.publicLoading")}>
            <Skeleton height={22} width="62%" />
            <Skeleton height={86} />
            <Skeleton height={42} />
          </SkeletonGroup>
        )}

        {token &&
          isOnline &&
          preview.isError &&
          (previewDataConcealed ? (
            <InvalidInvitationState />
          ) : (
            <ErrorState
              actions={
                <Button
                  leadingIcon={<RefreshCw />}
                  onClick={() => void preview.refetch()}
                  variant="secondary"
                >
                  {t("state.retry")}
                </Button>
              }
              description={t("invitation.publicLoadErrorDescription")}
              title={t("invitation.publicLoadErrorTitle")}
            />
          ))}

        {preview.data && !previewDataConcealed && (
          <div className="membership-invitation-card__content">
            <div className="membership-invitation-card__tenant">
              <div>
                <span>{t("invitation.publicWorkspaceLabel")}</span>
                <strong>{preview.data.tenant_name}</strong>
              </div>
              <StatusBadge tone="info">
                {t(previewRoleKey(preview.data.intended_role))}
              </StatusBadge>
            </div>
            <dl className="membership-invitation-card__facts">
              <div>
                <dt>{t("invitation.publicEmailLabel")}</dt>
                <dd>{preview.data.masked_email}</dd>
              </div>
              <div>
                <dt>{t("invitation.expiresLabel")}</dt>
                <dd>
                  <time dateTime={preview.data.expires_at}>
                    {dateFormatter.format(new Date(preview.data.expires_at))}
                  </time>
                </dd>
              </div>
            </dl>

            {session.status === "loading" && (
              <div className="membership-invitation-card__action">
                <Button disabled>
                  {t("invitation.publicCheckingSession")}
                </Button>
              </div>
            )}

            {session.status === "unauthenticated" && (
              <div className="membership-invitation-card__action">
                <p>{t("invitation.publicSignInDescription")}</p>
                <Button
                  leadingIcon={<LogIn />}
                  onClick={() => session.signIn("/app/home")}
                >
                  {t("invitation.publicSignInAction")}
                </Button>
                <small>{t("invitation.publicReopenLink")}</small>
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
                    {t("state.retry")}
                  </Button>
                }
                description={t("auth.unavailableDescription")}
                title={t("auth.unavailableTitle")}
              />
            )}

            {session.status === "authenticated" && (
              <div className="membership-invitation-card__action">
                {acceptInvitation.isError && (
                  <p className="membership-invitation-card__error" role="alert">
                    {t(publicInvitationErrorKey(acceptInvitation.error))}
                  </p>
                )}
                {isAccountMismatch(acceptInvitation.error) ? (
                  <Button
                    onClick={() => void session.signOut()}
                    size="sm"
                    variant="secondary"
                  >
                    {t("invitation.publicUseAnotherAccount")}
                  </Button>
                ) : isTerminalInvitationError(acceptInvitation.error) ? (
                  <Link
                    className="membership-invitation-card__continue"
                    to="/app/home"
                  >
                    {t("invitation.publicContinueAction")}
                  </Link>
                ) : (
                  <Button
                    leadingIcon={<UserPlus />}
                    loading={acceptInvitation.isPending}
                    loadingLabel={t("invitation.publicAccepting")}
                    onClick={() => void accept()}
                  >
                    {acceptInvitation.isError
                      ? t("invitation.publicRetryAccept")
                      : t("invitation.publicAcceptAction")}
                  </Button>
                )}
              </div>
            )}
          </div>
        )}
      </section>
    </main>
  );
}

function InvalidInvitationState() {
  const { t } = useI18n();
  return (
    <ErrorState
      actions={
        <Link className="membership-invitation-card__continue" to="/app/home">
          {t("invitation.publicContinueAction")}
        </Link>
      }
      description={t("invitation.publicUnavailableDescription")}
      title={t("invitation.publicUnavailableTitle")}
    />
  );
}

export function MembershipInvitationAcceptedPage() {
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const session = useSession();
  const state = location.state as {
    tenantID?: string;
    tenantName?: string;
  } | null;
  const { switchWorkspace } = useWorkspaceActions({
    onSwitchSuccess: () => navigate("/app/home", { replace: true }),
  });
  const alreadyActive =
    Boolean(state?.tenantID) &&
    session.currentUser?.active_tenant?.id === state?.tenantID;
  const switchSessionExpired =
    switchWorkspace.error instanceof APIRequestError &&
    switchWorkspace.error.status === 401;

  if (!state?.tenantID) {
    return (
      <main className="membership-invitation-page">
        <section className="membership-invitation-card">
          <InvalidInvitationState />
        </section>
      </main>
    );
  }

  return (
    <main className="membership-invitation-page">
      <section
        aria-labelledby="membership-invitation-accepted-title"
        className="membership-invitation-card membership-invitation-card--success"
      >
        <CheckCircle2 aria-hidden="true" />
        <p>{t("brand.product")}</p>
        <h1 id="membership-invitation-accepted-title">
          {t("invitation.publicAcceptedTitle")}
        </h1>
        <p>
          {t("invitation.publicAcceptedDescription", {
            tenant:
              state?.tenantName ?? t("invitation.publicWorkspaceFallback"),
          })}
        </p>
        {(session.status === "unauthenticated" || switchSessionExpired) && (
          <p className="membership-invitation-card__error" role="alert">
            {t("invitation.publicAcceptedSessionExpired")}
          </p>
        )}
        {session.status === "error" && (
          <p className="membership-invitation-card__error" role="alert">
            {t("auth.unavailableDescription")}
          </p>
        )}
        {switchWorkspace.isError && !switchSessionExpired && (
          <p className="membership-invitation-card__error" role="alert">
            {t("workspace.selectError")}
          </p>
        )}
        <div className="membership-invitation-card__actions">
          {session.status === "loading" && (
            <Button disabled>{t("invitation.publicCheckingSession")}</Button>
          )}
          {session.status === "error" && (
            <Button
              onClick={() => void session.refresh().catch(() => undefined)}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          )}
          {(session.status === "unauthenticated" || switchSessionExpired) && (
            <Button onClick={() => session.signIn("/app/home")}>
              {t("invitation.publicSignInAction")}
            </Button>
          )}
          {session.status === "authenticated" &&
            !switchSessionExpired &&
            !alreadyActive && (
              <Button
                loading={switchWorkspace.isPending}
                loadingLabel={t("workspace.switching")}
                onClick={() => switchWorkspace.mutate(state.tenantID ?? "")}
              >
                {switchWorkspace.isError
                  ? t("state.retry")
                  : t("invitation.publicSwitchAction")}
              </Button>
            )}
          <Link className="membership-invitation-card__continue" to="/app/home">
            {t("invitation.publicContinueAction")}
          </Link>
        </div>
      </section>
    </main>
  );
}
