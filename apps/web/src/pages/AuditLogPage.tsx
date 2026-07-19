import {
  APIRequestError,
  type AuditAction,
  type AuditEvent,
  type AuditOutcome,
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
import { ChevronDown, RefreshCw, Search, ShieldCheck, X } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { useAuditEvents, type AuditFilters } from "../app/audit";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";

type AuditActionChoice = "all" | AuditAction;
type AuditOutcomeChoice = "all" | AuditOutcome;

interface AuditFilterDraft {
  action: AuditActionChoice;
  occurredFrom: string;
  occurredTo: string;
  outcome: AuditOutcomeChoice;
  resourceID: string;
  resourceType: string;
}

interface AuditFilterErrors {
  occurredTo?: string;
  resourceID?: string;
  resourceType?: string;
}

const emptyDraft: AuditFilterDraft = {
  action: "all",
  occurredFrom: "",
  occurredTo: "",
  outcome: "all",
  resourceID: "",
  resourceType: "",
};

const auditActions = [
  "tenant.create",
  "tenant.update",
  "tenant.archive",
  "tenant.switch",
  "membership.invitation.create",
  "membership.invitation.revoke",
  "membership.invitation.accept",
  "membership.invitation.expire",
  "class.create",
  "class.update",
  "class.archive",
  "class.restore",
  "class.transfer_ownership",
  "class.enrollment.enroll",
  "class.enrollment.suspend",
  "class.enrollment.remove",
  "class.enrollment.join",
  "class.enrollment.leave",
  "class.enrollment.update_role",
  "class.roster.bulk",
  "class.invite_code.create",
  "class.invite_code.revoke",
  "class.invite_code.expire",
  "class.invite_code.exhaust",
] as const satisfies readonly AuditAction[];

const actionKeys: Record<AuditAction, TranslationKey> = {
  "tenant.create": "audit.action.tenantCreate",
  "tenant.update": "audit.action.tenantUpdate",
  "tenant.archive": "audit.action.tenantArchive",
  "tenant.switch": "audit.action.tenantSwitch",
  "membership.invitation.create": "audit.action.membershipInvitationCreate",
  "membership.invitation.revoke": "audit.action.membershipInvitationRevoke",
  "membership.invitation.accept": "audit.action.membershipInvitationAccept",
  "membership.invitation.expire": "audit.action.membershipInvitationExpire",
  "class.create": "audit.action.classCreate",
  "class.update": "audit.action.classUpdate",
  "class.archive": "audit.action.classArchive",
  "class.restore": "audit.action.classRestore",
  "class.transfer_ownership": "audit.action.classTransferOwnership",
  "class.enrollment.enroll": "audit.action.classEnrollmentEnroll",
  "class.enrollment.suspend": "audit.action.classEnrollmentSuspend",
  "class.enrollment.remove": "audit.action.classEnrollmentRemove",
  "class.enrollment.join": "audit.action.classEnrollmentJoin",
  "class.enrollment.leave": "audit.action.classEnrollmentLeave",
  "class.enrollment.update_role": "audit.action.classEnrollmentUpdateRole",
  "class.roster.bulk": "audit.action.classRosterBulk",
  "class.invite_code.create": "audit.action.classInviteCodeCreate",
  "class.invite_code.revoke": "audit.action.classInviteCodeRevoke",
  "class.invite_code.expire": "audit.action.classInviteCodeExpire",
  "class.invite_code.exhaust": "audit.action.classInviteCodeExhaust",
};

const resourceTypeKeys: Readonly<Record<string, TranslationKey>> = {
  tenant: "audit.resource.tenant",
  membership_invitation: "audit.resource.membershipInvitation",
  class: "audit.resource.class",
  class_enrollment: "audit.resource.classEnrollment",
  class_invite_code: "audit.resource.classInviteCode",
  class_member: "audit.resource.classMember",
};

const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const resourceTypePattern = /^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$/;

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function outcomeKey(outcome: AuditOutcome): TranslationKey {
  return outcome === "succeeded"
    ? "audit.outcomeSucceeded"
    : outcome === "denied"
      ? "audit.outcomeDenied"
      : "audit.outcomeFailed";
}

function outcomeTone(outcome: AuditOutcome) {
  return outcome === "succeeded"
    ? ("success" as const)
    : outcome === "denied"
      ? ("warning" as const)
      : ("danger" as const);
}

function localDateTimeToISO(value: string) {
  if (!value) {
    return undefined;
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed.toISOString();
}

export function AuditLogPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const activeTenant = session.currentUser?.active_tenant;
  const canViewAudit =
    session.currentUser?.permissions.includes("audit.view") ?? false;
  const [draft, setDraft] = useState<AuditFilterDraft>(emptyDraft);
  const [filters, setFilters] = useState<AuditFilters>({});
  const [errors, setErrors] = useState<AuditFilterErrors>({});
  const auditQuery = useAuditEvents(activeTenant?.id, filters, canViewAudit);
  const events = useMemo(() => {
    const byID = new Map<string, AuditEvent>();
    for (const page of auditQuery.data?.pages ?? []) {
      for (const event of page.items) {
        if (!byID.has(event.id)) {
          byID.set(event.id, event);
        }
      }
    }
    return [...byID.values()];
  }, [auditQuery.data?.pages]);
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );
  const hasActiveFilters = Object.values(filters).some(Boolean);
  const auditAccessDenied = auditQuery.isError && isForbidden(auditQuery.error);
  const auditDataConcealed =
    auditQuery.isError && shouldConcealTenantScopedData(auditQuery.error);
  const hasStaleRefreshError =
    auditQuery.isError &&
    events.length > 0 &&
    !auditDataConcealed &&
    !auditQuery.isFetchNextPageError;

  const submitFilters = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const nextErrors: AuditFilterErrors = {};
    const occurredFrom = localDateTimeToISO(draft.occurredFrom);
    const occurredTo = localDateTimeToISO(draft.occurredTo);
    const resourceType = draft.resourceType.trim().toLowerCase();
    const resourceID = draft.resourceID.trim().toLowerCase();

    if (
      (draft.occurredFrom && !occurredFrom) ||
      (draft.occurredTo && !occurredTo) ||
      (occurredFrom && occurredTo && occurredFrom >= occurredTo)
    ) {
      nextErrors.occurredTo = t("audit.timeRangeError");
    }
    if (resourceType && !resourceTypePattern.test(resourceType)) {
      nextErrors.resourceType = t("audit.resourceTypeError");
    }
    if (resourceID && !resourceType) {
      nextErrors.resourceID = t("audit.resourceIDNeedsType");
    } else if (resourceID && !uuidPattern.test(resourceID)) {
      nextErrors.resourceID = t("audit.resourceIDError");
    }
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) {
      return;
    }

    setFilters({
      action: draft.action === "all" ? undefined : draft.action,
      occurredFrom,
      occurredTo,
      outcome: draft.outcome === "all" ? undefined : draft.outcome,
      resourceID: resourceID || undefined,
      resourceType: resourceType || undefined,
    });
  };

  const clearFilters = () => {
    setDraft(emptyDraft);
    setErrors({});
    setFilters({});
  };

  const actorLabel = (event: (typeof events)[number]) => {
    if (event.actor.type === "system") {
      return t("audit.systemActor");
    }
    return event.actor.display_name ?? t("audit.unknownActor");
  };

  const resourceTypeLabel = (resourceType: string) => {
    const key = resourceTypeKeys[resourceType];
    return key ? t(key) : resourceType;
  };

  return (
    <div className="page-content audit-log">
      <Link className="classroom-back-link" to="/app/workspace">
        {t("audit.backToWorkspace")}
      </Link>
      <header className="page-heading audit-log__header">
        <div>
          <p>{t("audit.kicker")}</p>
          <h1>{t("audit.title")}</h1>
          <span>{t("audit.description")}</span>
        </div>
        {canViewAudit && activeTenant && (
          <Button
            leadingIcon={<RefreshCw />}
            onClick={() => void auditQuery.refetch()}
            variant="secondary"
          >
            {t("audit.refresh")}
          </Button>
        )}
      </header>

      {!canViewAudit || !activeTenant ? (
        <ForbiddenState
          description={t("audit.forbiddenDescription")}
          title={t("audit.forbiddenTitle")}
        />
      ) : (
        <>
          <section
            aria-labelledby="audit-filter-title"
            className="audit-log__panel"
          >
            <div className="audit-log__panel-heading">
              <ShieldCheck aria-hidden="true" />
              <div>
                <h2 id="audit-filter-title">{t("audit.filterTitle")}</h2>
                <p>{t("audit.filterDescription")}</p>
              </div>
            </div>
            <form className="audit-log__filters" onSubmit={submitFilters}>
              <TextField
                autoComplete="off"
                label={t("audit.occurredFromLabel")}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    occurredFrom: event.target.value,
                  }))
                }
                type="datetime-local"
                value={draft.occurredFrom}
              />
              <TextField
                autoComplete="off"
                error={errors.occurredTo}
                label={t("audit.occurredToLabel")}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    occurredTo: event.target.value,
                  }))
                }
                type="datetime-local"
                value={draft.occurredTo}
              />
              <SelectField
                ariaLabel={t("audit.actionFilterLabel")}
                label={t("audit.actionFilterLabel")}
                onValueChange={(value) =>
                  setDraft((current) => ({
                    ...current,
                    action: value as AuditActionChoice,
                  }))
                }
                options={[
                  { label: t("audit.actionAll"), value: "all" },
                  ...auditActions.map((action) => ({
                    label: t(actionKeys[action]),
                    value: action,
                  })),
                ]}
                value={draft.action}
              />
              <SelectField
                ariaLabel={t("audit.outcomeFilterLabel")}
                label={t("audit.outcomeFilterLabel")}
                onValueChange={(value) =>
                  setDraft((current) => ({
                    ...current,
                    outcome: value as AuditOutcomeChoice,
                  }))
                }
                options={[
                  { label: t("audit.outcomeAll"), value: "all" },
                  { label: t("audit.outcomeSucceeded"), value: "succeeded" },
                  { label: t("audit.outcomeDenied"), value: "denied" },
                  { label: t("audit.outcomeFailed"), value: "failed" },
                ]}
                value={draft.outcome}
              />
              <TextField
                autoCapitalize="none"
                autoComplete="off"
                error={errors.resourceType}
                hint={t("audit.resourceTypeHint")}
                label={t("audit.resourceTypeLabel")}
                maxLength={80}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    resourceType: event.target.value,
                  }))
                }
                spellCheck={false}
                value={draft.resourceType}
              />
              <TextField
                autoCapitalize="none"
                autoComplete="off"
                error={errors.resourceID}
                hint={t("audit.resourceIDHint")}
                label={t("audit.resourceIDLabel")}
                maxLength={36}
                onChange={(event) =>
                  setDraft((current) => ({
                    ...current,
                    resourceID: event.target.value,
                  }))
                }
                spellCheck={false}
                value={draft.resourceID}
              />
              <div className="audit-log__filter-actions">
                <Button leadingIcon={<Search />} type="submit">
                  {t("audit.applyFilters")}
                </Button>
                <Button
                  leadingIcon={<X />}
                  onClick={clearFilters}
                  type="button"
                  variant="secondary"
                >
                  {t("audit.clearFilters")}
                </Button>
              </div>
            </form>
          </section>

          <section
            aria-labelledby="audit-results-title"
            className="audit-log__panel audit-log__results"
          >
            <div className="audit-log__results-heading">
              <div>
                <h2 id="audit-results-title">{t("audit.resultsTitle")}</h2>
                <p>{t("audit.resultsDescription")}</p>
              </div>
              {!auditDataConcealed && (
                <span aria-live="polite">
                  {t("audit.loadedCount", { count: events.length })}
                </span>
              )}
            </div>

            {auditQuery.isPending && (
              <SkeletonGroup label={t("audit.loading")}>
                <Skeleton height={44} />
                <Skeleton height={72} />
                <Skeleton height={72} />
              </SkeletonGroup>
            )}

            {auditAccessDenied && (
              <ForbiddenState
                description={t("audit.forbiddenDescription")}
                title={t("audit.forbiddenTitle")}
              />
            )}

            {auditQuery.isError &&
              (events.length === 0 || auditDataConcealed) &&
              !auditAccessDenied && (
                <ErrorState
                  actions={
                    <Button
                      leadingIcon={<RefreshCw />}
                      onClick={() => void auditQuery.refetch()}
                      size="sm"
                      variant="secondary"
                    >
                      {t("state.retry")}
                    </Button>
                  }
                  description={t("audit.errorDescription")}
                  title={t("audit.errorTitle")}
                />
              )}

            {hasStaleRefreshError && (
              <div className="audit-log__refresh-error" role="alert">
                <span>{t("audit.refreshError")}</span>
                <Button
                  leadingIcon={<RefreshCw />}
                  onClick={() => void auditQuery.refetch()}
                  size="sm"
                  variant="secondary"
                >
                  {t("state.retry")}
                </Button>
              </div>
            )}

            {auditQuery.isSuccess && events.length === 0 && (
              <EmptyState
                description={
                  hasActiveFilters
                    ? t("audit.filteredEmptyDescription")
                    : t("audit.emptyDescription")
                }
                title={
                  hasActiveFilters
                    ? t("audit.filteredEmptyTitle")
                    : t("audit.emptyTitle")
                }
              />
            )}

            {events.length > 0 && !auditDataConcealed && (
              <>
                <div className="audit-log__table-wrap">
                  <table className="audit-log__table">
                    <caption className="visually-hidden">
                      {t("audit.tableCaption")}
                    </caption>
                    <thead>
                      <tr>
                        <th scope="col">{t("audit.timeColumn")}</th>
                        <th scope="col">{t("audit.actorColumn")}</th>
                        <th scope="col">{t("audit.actionColumn")}</th>
                        <th scope="col">{t("audit.resourceColumn")}</th>
                        <th scope="col">{t("audit.outcomeColumn")}</th>
                        <th scope="col">{t("audit.requestIDColumn")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {events.map((event) => (
                        <tr key={event.id}>
                          <td>
                            <time dateTime={event.occurred_at}>
                              {dateFormatter.format(
                                new Date(event.occurred_at),
                              )}
                            </time>
                          </td>
                          <td>
                            <strong>{actorLabel(event)}</strong>
                            {event.actor.user_id && (
                              <code>{event.actor.user_id}</code>
                            )}
                          </td>
                          <td>{t(actionKeys[event.action])}</td>
                          <td>
                            <span>
                              {resourceTypeLabel(event.resource.type)}
                            </span>
                            <code>
                              {event.resource.id ??
                                t("audit.resourceUnavailable")}
                            </code>
                          </td>
                          <td>
                            <StatusBadge tone={outcomeTone(event.outcome)}>
                              {t(outcomeKey(event.outcome))}
                            </StatusBadge>
                          </td>
                          <td>
                            <code>{event.request_id}</code>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                {auditQuery.isFetchNextPageError && (
                  <div className="audit-log__pagination-error" role="alert">
                    <span>{t("audit.loadMoreError")}</span>
                    <Button
                      onClick={() => void auditQuery.fetchNextPage()}
                      size="sm"
                      variant="secondary"
                    >
                      {t("state.retry")}
                    </Button>
                  </div>
                )}
                {auditQuery.hasNextPage && !auditQuery.isFetchNextPageError && (
                  <div className="audit-log__pagination">
                    <Button
                      leadingIcon={<ChevronDown />}
                      loading={auditQuery.isFetchingNextPage}
                      loadingLabel={t("audit.loadingMore")}
                      onClick={() => void auditQuery.fetchNextPage()}
                      variant="secondary"
                    >
                      {t("audit.loadMore")}
                    </Button>
                  </div>
                )}
              </>
            )}
          </section>
        </>
      )}
    </div>
  );
}
