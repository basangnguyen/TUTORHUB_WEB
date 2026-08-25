import { APIRequestError } from "@tutorhub/api-client";
import {
  Button,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
} from "@tutorhub/ui";
import {
  ExternalLink,
  FlaskConical,
  RefreshCw,
  ShieldCheck,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { useI18n } from "../app/i18n";
import {
  usePrivateAlphaEnrollment,
  useUpdatePrivateAlphaEnrollment,
} from "../app/privateAlphaEnrollment";
import { WHITEBOARD_PRIVATE_ALPHA_SUPPORT_HREF } from "../features/collaboration/whiteboardPrivateAlpha";

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function isConflict(error: Error | null) {
  return error instanceof APIRequestError && error.status === 409;
}

export function TenantPrivateAlphaEnrollmentPanel({
  tenantID,
}: {
  tenantID: string;
}) {
  const { language, t } = useI18n();
  const enrollment = usePrivateAlphaEnrollment(tenantID);
  const updateEnrollment = useUpdatePrivateAlphaEnrollment(tenantID);
  const [acknowledged, setAcknowledged] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  if (enrollment.isPending) {
    return (
      <section className="workspace-management__panel private-alpha-enrollment">
        <SkeletonGroup label={t("privateAlpha.loading")}>
          <Skeleton height={28} width="52%" />
          <Skeleton height={112} />
          <Skeleton height={44} width="32%" />
        </SkeletonGroup>
      </section>
    );
  }

  if (enrollment.isError || !enrollment.data) {
    const State = isForbidden(enrollment.error) ? ForbiddenState : ErrorState;
    return (
      <section className="workspace-management__panel private-alpha-enrollment">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void enrollment.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={
            isForbidden(enrollment.error)
              ? t("privateAlpha.forbiddenDescription")
              : t("privateAlpha.errorDescription")
          }
          title={
            isForbidden(enrollment.error)
              ? t("privateAlpha.forbiddenTitle")
              : t("privateAlpha.errorTitle")
          }
        />
      </section>
    );
  }

  const current = enrollment.data;
  if (current.tenant_id !== tenantID) {
    return null;
  }

  const active = current.status === "active";
  const canManage = current.allowed_actions.manage_enrollment;
  const updatedAt = active ? current.accepted_at : current.withdrawn_at;

  const submit = (enrolled: boolean) => {
    setFeedback(null);
    updateEnrollment.reset();
    updateEnrollment.mutate(
      {
        enrolled,
        expected_revision: current.revision,
        notice_version: current.notice_version,
      },
      {
        onSuccess: () => {
          setAcknowledged(false);
          setFeedback(
            t(
              enrolled
                ? "privateAlpha.enrollSuccess"
                : "privateAlpha.withdrawSuccess",
            ),
          );
        },
      },
    );
  };

  const reload = async () => {
    setAcknowledged(false);
    setFeedback(null);
    updateEnrollment.reset();
    await enrollment.refetch();
  };

  return (
    <section
      aria-labelledby="private-alpha-enrollment-title"
      className="workspace-management__panel private-alpha-enrollment"
    >
      <div className="workspace-management__panel-heading private-alpha-enrollment__heading">
        <span aria-hidden="true">
          <FlaskConical />
        </span>
        <div>
          <p>{t("privateAlpha.kicker")}</p>
          <h2 id="private-alpha-enrollment-title">{t("privateAlpha.title")}</h2>
          <p>{t("privateAlpha.description")}</p>
        </div>
        <StatusBadge tone={active ? "success" : "neutral"}>
          {t(
            active
              ? "privateAlpha.statusActive"
              : current.status === "withdrawn"
                ? "privateAlpha.statusWithdrawn"
                : "privateAlpha.statusNotEnrolled",
          )}
        </StatusBadge>
      </div>

      <div className="private-alpha-enrollment__notice">
        <div>
          <ShieldCheck aria-hidden="true" />
          <strong>{t("privateAlpha.noticeTitle")}</strong>
        </div>
        <p>{t("privateAlpha.noticeDescription")}</p>
        <ul>
          <li>{t("privateAlpha.limitSingleInstance")}</li>
          <li>{t("privateAlpha.limitColdStart")}</li>
          <li>{t("privateAlpha.limitSynthetic")}</li>
          <li>{t("privateAlpha.limitAccessibility")}</li>
        </ul>
        <Link
          className="private-alpha-enrollment__support"
          to={WHITEBOARD_PRIVATE_ALPHA_SUPPORT_HREF}
        >
          {t("privateAlpha.support")}
          <ExternalLink aria-hidden="true" />
        </Link>
      </div>

      {updatedAt && (
        <p className="private-alpha-enrollment__timestamp">
          {t(active ? "privateAlpha.acceptedAt" : "privateAlpha.withdrawnAt")}{" "}
          <strong>{dateFormatter.format(new Date(updatedAt))}</strong>
        </p>
      )}

      {!active && canManage && (
        <label className="private-alpha-enrollment__acknowledgment">
          <input
            aria-describedby="private-alpha-acceptance-description"
            aria-labelledby="private-alpha-acceptance-label"
            checked={acknowledged}
            disabled={updateEnrollment.isPending}
            onChange={(event) => setAcknowledged(event.target.checked)}
            type="checkbox"
          />
          <span>
            <strong id="private-alpha-acceptance-label">
              {t("privateAlpha.acceptanceLabel")}
            </strong>
            <small id="private-alpha-acceptance-description">
              {t("privateAlpha.acceptanceDescription")}
            </small>
          </span>
        </label>
      )}

      {updateEnrollment.isError && (
        <div className="workspace-management__feedback" role="alert">
          <span>
            {isConflict(updateEnrollment.error)
              ? t("privateAlpha.conflict")
              : isForbidden(updateEnrollment.error)
                ? t("privateAlpha.updateForbidden")
                : t("privateAlpha.updateError")}
          </span>
          {isConflict(updateEnrollment.error) && (
            <Button onClick={() => void reload()} size="sm" variant="secondary">
              {t("privateAlpha.reload")}
            </Button>
          )}
        </div>
      )}

      {feedback && (
        <p className="workspace-management__feedback" role="status">
          {feedback}
        </p>
      )}

      {canManage && (
        <div className="private-alpha-enrollment__actions">
          {active ? (
            <Button
              loading={updateEnrollment.isPending}
              loadingLabel={t("privateAlpha.saving")}
              onClick={() => submit(false)}
              variant="danger"
            >
              {t("privateAlpha.withdraw")}
            </Button>
          ) : (
            <Button
              disabled={!acknowledged}
              loading={updateEnrollment.isPending}
              loadingLabel={t("privateAlpha.saving")}
              onClick={() => submit(true)}
            >
              {t("privateAlpha.enroll")}
            </Button>
          )}
        </div>
      )}
    </section>
  );
}
