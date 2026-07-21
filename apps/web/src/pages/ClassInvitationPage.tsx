import { APIRequestError } from "@tutorhub/api-client";
import {
  Button,
  ErrorState,
  OfflineState,
  Skeleton,
  SkeletonGroup,
} from "@tutorhub/ui";
import { LogIn, RefreshCw, School, UserPlus } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useJoinClassInvitation } from "../app/classEnrollments";
import {
  clearFragmentTokenEscrow,
  consumeFragmentToken,
} from "../app/fragmentToken";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import {
  tenantOperationAvailability,
  useTenantCapabilities,
} from "../app/tenantCapabilities";
import { TenantOperationNotice } from "../components/TenantOperationNotice";

const classInvitationEscrowKey = "class-invitation";

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

function joinErrorKey(error: Error | null): TranslationKey {
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
  if (error instanceof APIRequestError && error.status === 401) {
    return "classInvitation.sessionExpired";
  }
  if (error instanceof APIRequestError && error.status === 403) {
    return "classInvitation.forbidden";
  }
  if (error instanceof APIRequestError && error.status === 429) {
    return "classInvitation.rateLimited";
  }
  if (
    error instanceof APIRequestError &&
    [400, 404, 409, 410].includes(error.status)
  ) {
    return "classInvitation.unavailableDescription";
  }
  return "classInvitation.joinError";
}

export function ClassInvitationPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const session = useSession();
  const { isOnline, refresh: refreshOnlineStatus } = useOnlineStatus();
  const [token] = useState(() =>
    consumeFragmentToken(classInvitationEscrowKey),
  );
  const joinInvitation = useJoinClassInvitation(token);
  const activeTenantID = session.currentUser?.active_tenant?.id;
  const capabilitiesQuery = useTenantCapabilities(
    activeTenantID,
    session.status === "authenticated",
  );
  const joinAvailability = tenantOperationAvailability(
    capabilitiesQuery,
    "join_class_invite_link",
  );

  useEffect(
    () => () => {
      clearFragmentTokenEscrow(classInvitationEscrowKey);
    },
    [],
  );

  const join = async () => {
    if (!joinAvailability.available) {
      return;
    }
    try {
      const result = await joinInvitation.mutateAsync();
      navigate(`/app/classrooms/${result.classroom.id}`, { replace: true });
    } catch {
      // The mutation exposes a recoverable, token-safe error below.
    }
  };

  return (
    <main className="membership-invitation-page class-invitation-page">
      <section
        aria-labelledby="class-invitation-title"
        className="membership-invitation-card class-invitation-card"
      >
        <div className="class-invitation-card__icon" aria-hidden="true">
          <School />
        </div>
        <p className="membership-invitation-card__kicker">
          {t("brand.product")}
        </p>
        <h1 id="class-invitation-title">{t("classInvitation.title")}</h1>
        <p>{t("classInvitation.description")}</p>

        {!token && <InvalidClassInvitationState />}

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
            description={t("classInvitation.offlineDescription")}
            title={t("state.offlineTitle")}
          />
        )}

        {token && isOnline && session.status === "loading" && (
          <SkeletonGroup label={t("classInvitation.checkingSession")}>
            <Skeleton height={22} width="60%" />
            <Skeleton height={72} />
            <Skeleton height={42} />
          </SkeletonGroup>
        )}

        {token && isOnline && session.status === "error" && (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void session.refresh().catch(() => undefined)}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("auth.unavailableDescription")}
            title={t("auth.unavailableTitle")}
          />
        )}

        {token && isOnline && session.status === "unauthenticated" && (
          <div className="membership-invitation-card__action">
            <p>{t("classInvitation.signInDescription")}</p>
            <Button
              leadingIcon={<LogIn />}
              onClick={() => session.signIn("/app/home")}
            >
              {t("classInvitation.signInAction")}
            </Button>
            <small>{t("classInvitation.reopenLink")}</small>
          </div>
        )}

        {token && isOnline && session.status === "authenticated" && (
          <div className="membership-invitation-card__action">
            {!session.currentUser?.active_tenant && (
              <p className="membership-invitation-card__error" role="alert">
                {t("classInvitation.workspaceRequired")}
              </p>
            )}
            {session.currentUser?.active_tenant && (
              <TenantOperationNotice
                availability={joinAvailability}
                label={t("capabilities.operationJoinClass")}
                onRetry={() => void capabilitiesQuery.refetch()}
              />
            )}
            {joinInvitation.isError && (
              <p className="membership-invitation-card__error" role="alert">
                {t(joinErrorKey(joinInvitation.error))}
              </p>
            )}
            <Button
              disabled={
                !session.currentUser?.active_tenant ||
                !joinAvailability.available
              }
              leadingIcon={<UserPlus />}
              loading={joinInvitation.isPending}
              loadingLabel={t("classInvitation.joining")}
              onClick={() => void join()}
            >
              {joinInvitation.isError
                ? t("classInvitation.retryJoin")
                : t("classInvitation.joinAction")}
            </Button>
          </div>
        )}
      </section>
    </main>
  );
}

function InvalidClassInvitationState() {
  const { t } = useI18n();
  return (
    <ErrorState
      description={t("classInvitation.unavailableDescription")}
      title={t("classInvitation.unavailableTitle")}
    />
  );
}
