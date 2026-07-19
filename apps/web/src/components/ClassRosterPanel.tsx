import {
  APIRequestError,
  type ClassEnrollmentRole,
  type ClassEnrollmentStatus,
  type ClassroomClass,
  type ClassRosterBulkAction,
  type ClassRosterBulkResponse,
  type ClassRosterMember,
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
  Select,
  SelectField,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import { ChevronDown, RefreshCw, Search, UsersRound } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import {
  useBulkMutateClassRoster,
  useClassRoster,
  useUpdateClassRosterRole,
  type RosterStatusFilter,
} from "../app/classRoster";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";

const maximumBulkSelection = 50;

type BulkChoice =
  | "suspend"
  | "remove"
  | "role:co_teacher"
  | "role:teaching_assistant"
  | "role:student";

interface PendingRosterOperation {
  action: ClassRosterBulkAction;
  classRole?: ClassEnrollmentRole;
  displayName?: string;
  isSingleRoleUpdate: boolean;
  userIDs: string[];
}

interface RosterFeedback {
  message: string;
  tone: "error" | "success";
}

function roleKey(role: "owner" | ClassEnrollmentRole): TranslationKey {
  switch (role) {
    case "owner":
      return "classRoster.roleOwner";
    case "co_teacher":
      return "classRoster.roleCoTeacher";
    case "teaching_assistant":
      return "classRoster.roleTeachingAssistant";
    default:
      return "classRoster.roleStudent";
  }
}

function statusKey(status: ClassEnrollmentStatus): TranslationKey {
  switch (status) {
    case "invited":
      return "classRoster.statusInvited";
    case "suspended":
      return "classRoster.statusSuspended";
    case "left":
      return "classRoster.statusLeft";
    case "removed":
      return "classRoster.statusRemoved";
    default:
      return "classRoster.statusActive";
  }
}

function statusTone(status: ClassEnrollmentStatus) {
  if (status === "active") {
    return "success" as const;
  }
  if (status === "suspended" || status === "removed") {
    return "danger" as const;
  }
  return "neutral" as const;
}

function mutationErrorKey(error: Error | null): TranslationKey {
  if (error instanceof APIRequestError && error.status === 403) {
    return "classRoster.mutationForbidden";
  }
  if (error instanceof APIRequestError && error.status === 404) {
    return "classRoster.mutationNotFound";
  }
  if (error instanceof APIRequestError && error.status === 409) {
    return "classRoster.mutationConflict";
  }
  return "classRoster.mutationError";
}

function bulkChoiceOperation(choice: BulkChoice, userIDs: string[]) {
  if (choice !== "suspend" && choice !== "remove") {
    return {
      action: "update_role" as const,
      classRole: choice.slice("role:".length) as ClassEnrollmentRole,
      isSingleRoleUpdate: false,
      userIDs,
    };
  }
  return {
    action: choice,
    isSingleRoleUpdate: false,
    userIDs,
  } satisfies PendingRosterOperation;
}

export function ClassRosterPanel({ classroom }: { classroom: ClassroomClass }) {
  const { t } = useI18n();
  const tenantID = useSession().currentUser?.active_tenant?.id;
  const canManage = classroom.viewer_access.can_manage_enrollments;
  const [searchDraft, setSearchDraft] = useState("");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<RosterStatusFilter>("all");
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [bulkChoice, setBulkChoice] = useState<BulkChoice>("suspend");
  const [pending, setPending] = useState<PendingRosterOperation | null>(null);
  const [feedback, setFeedback] = useState<RosterFeedback | null>(null);
  const roster = useClassRoster(
    tenantID,
    classroom.id,
    search,
    status,
    canManage,
  );
  const updateRole = useUpdateClassRosterRole(tenantID, classroom.id);
  const bulkMutate = useBulkMutateClassRoster(tenantID, classroom.id);

  const members = useMemo(() => {
    const byUserID = new Map<string, ClassRosterMember>();
    for (const page of roster.data?.pages ?? []) {
      for (const member of page.items) {
        if (!byUserID.has(member.user.id)) {
          byUserID.set(member.user.id, member);
        }
      }
    }
    return [...byUserID.values()];
  }, [roster.data?.pages]);
  const classOwner = roster.data?.pages[0]?.class_owner;
  const mutation = pending?.isSingleRoleUpdate ? updateRole : bulkMutate;
  const mutationPending = updateRole.isPending || bulkMutate.isPending;

  if (!canManage || !tenantID) {
    return null;
  }

  const submitSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSelected(new Set());
    setSearch(searchDraft);
  };

  const toggleSelected = (userID: string, checked: boolean) => {
    setFeedback(null);
    setSelected((current) => {
      const next = new Set(current);
      if (checked) {
        if (next.size >= maximumBulkSelection) {
          return current;
        }
        next.add(userID);
      } else {
        next.delete(userID);
      }
      return next;
    });
  };

  const openSingleRole = (
    member: ClassRosterMember,
    classRole: ClassEnrollmentRole,
  ) => {
    if (classRole === member.enrollment.class_role) {
      return;
    }
    updateRole.reset();
    bulkMutate.reset();
    setFeedback(null);
    setPending({
      action: "update_role",
      classRole,
      displayName: member.user.display_name,
      isSingleRoleUpdate: true,
      userIDs: [member.user.id],
    });
  };

  const openSingleStatus = (
    member: ClassRosterMember,
    action: Extract<ClassRosterBulkAction, "suspend" | "remove">,
  ) => {
    updateRole.reset();
    bulkMutate.reset();
    setFeedback(null);
    setPending({
      action,
      displayName: member.user.display_name,
      isSingleRoleUpdate: false,
      userIDs: [member.user.id],
    });
  };

  const openBulk = () => {
    if (selected.size === 0) {
      return;
    }
    updateRole.reset();
    bulkMutate.reset();
    setFeedback(null);
    setPending(bulkChoiceOperation(bulkChoice, [...selected]));
  };

  const finishBulk = (result: ClassRosterBulkResponse) => {
    const message = t("classRoster.bulkResult", {
      failed: result.failed_count,
      unchanged: result.unchanged_count,
      updated: result.updated_count,
    });
    setFeedback({
      message,
      tone: result.failed_count > 0 ? "error" : "success",
    });
    setSelected(new Set());
    setPending(null);
  };

  const confirmPending = () => {
    if (!pending) {
      return;
    }
    if (pending.isSingleRoleUpdate && pending.classRole) {
      updateRole.mutate(
        {
          classID: classroom.id,
          classRole: pending.classRole,
          userID: pending.userIDs[0] ?? "",
        },
        {
          onSuccess: (result) => {
            setFeedback({
              message:
                result.outcome === "updated"
                  ? t("classRoster.roleUpdated")
                  : t("classRoster.unchanged"),
              tone: "success",
            });
            setPending(null);
          },
        },
      );
      return;
    }
    bulkMutate.mutate(
      {
        classID: classroom.id,
        input: {
          action: pending.action,
          class_role: pending.classRole,
          user_ids: pending.userIDs,
        },
      },
      { onSuccess: finishBulk },
    );
  };

  const operationLabel = pending
    ? pending.action === "update_role" && pending.classRole
      ? t(roleKey(pending.classRole))
      : pending.action === "suspend"
        ? t("classRoster.suspendAction")
        : t("classRoster.removeAction")
    : "";

  return (
    <section
      aria-labelledby="class-roster-title"
      className="classroom-detail__section class-roster"
    >
      <div className="class-roster__heading">
        <div>
          <h2 id="class-roster-title">{t("classRoster.title")}</h2>
          <p>{t("classRoster.description")}</p>
        </div>
        <span className="class-roster__count">
          <UsersRound aria-hidden="true" />
          {t("classRoster.loadedCount", { count: members.length })}
        </span>
      </div>

      {classroom.status !== "active" && (
        <p className="class-roster__notice" role="status">
          {t("classRoster.archivedNotice")}
        </p>
      )}

      <form className="class-roster__filters" onSubmit={submitSearch}>
        <TextField
          autoComplete="off"
          label={t("classRoster.searchLabel")}
          maxLength={200}
          onChange={(event) => setSearchDraft(event.target.value)}
          placeholder={t("classRoster.searchPlaceholder")}
          type="search"
          value={searchDraft}
        />
        <SelectField
          ariaLabel={t("classRoster.statusFilter")}
          label={t("classRoster.statusFilter")}
          onValueChange={(value) => {
            setSelected(new Set());
            setStatus(value as RosterStatusFilter);
          }}
          options={[
            { label: t("classRoster.statusAll"), value: "all" },
            { label: t("classRoster.statusActive"), value: "active" },
            { label: t("classRoster.statusInvited"), value: "invited" },
            { label: t("classRoster.statusSuspended"), value: "suspended" },
            { label: t("classRoster.statusLeft"), value: "left" },
            { label: t("classRoster.statusRemoved"), value: "removed" },
          ]}
          value={status}
        />
        <Button leadingIcon={<Search />} type="submit" variant="secondary">
          {t("classRoster.searchAction")}
        </Button>
      </form>

      {feedback && (
        <p
          className={`class-roster__feedback class-roster__feedback--${feedback.tone}`}
          role={feedback.tone === "error" ? "alert" : "status"}
        >
          {feedback.message}
        </p>
      )}

      {roster.isPending && (
        <SkeletonGroup label={t("classRoster.loading")}>
          <Skeleton height={92} />
          <Skeleton height={220} />
        </SkeletonGroup>
      )}

      {roster.isError &&
        (roster.error instanceof APIRequestError &&
        roster.error.status === 403 ? (
          <ForbiddenState
            description={t("classRoster.forbiddenDescription")}
            title={t("classRoster.forbiddenTitle")}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void roster.refetch()}
                size="sm"
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("classRoster.errorDescription")}
            title={t("classRoster.errorTitle")}
          />
        ))}

      {roster.data && classOwner && (
        <div className="class-roster__owner">
          <div>
            <span className="class-roster__eyebrow">
              {t("classRoster.pinnedOwner")}
            </span>
            <strong>{classOwner.user.display_name}</strong>
            <span>{classOwner.user.email}</span>
          </div>
          <StatusBadge tone="info">{t(roleKey("owner"))}</StatusBadge>
        </div>
      )}

      {roster.data && members.length === 0 && (
        <EmptyState
          description={
            search || status !== "all"
              ? t("classRoster.filteredEmptyDescription")
              : t("classRoster.emptyDescription")
          }
          title={t("classRoster.emptyTitle")}
        />
      )}

      {members.length > 0 && (
        <>
          <div
            className="class-roster__bulk"
            aria-label={t("classRoster.bulkTitle")}
          >
            <span aria-live="polite">
              {t("classRoster.selectedCount", { count: selected.size })}
            </span>
            <Select
              ariaLabel={t("classRoster.bulkActionLabel")}
              disabled={classroom.status !== "active"}
              onValueChange={(value) => setBulkChoice(value as BulkChoice)}
              options={[
                { label: t("classRoster.suspendAction"), value: "suspend" },
                { label: t("classRoster.removeAction"), value: "remove" },
                {
                  label: t("classRoster.assignCoTeacher"),
                  value: "role:co_teacher",
                },
                {
                  label: t("classRoster.assignTeachingAssistant"),
                  value: "role:teaching_assistant",
                },
                {
                  label: t("classRoster.assignStudent"),
                  value: "role:student",
                },
              ]}
              value={bulkChoice}
            />
            <Button
              disabled={selected.size === 0 || classroom.status !== "active"}
              onClick={openBulk}
              size="sm"
              variant={bulkChoice === "remove" ? "danger" : "secondary"}
            >
              {t("classRoster.applyBulk")}
            </Button>
          </div>
          {selected.size >= maximumBulkSelection && (
            <p className="class-roster__selection-limit" role="status">
              {t("classRoster.selectionLimit")}
            </p>
          )}

          <div className="class-roster__table-wrap">
            <table className="class-roster__table">
              <thead>
                <tr>
                  <th scope="col">{t("classRoster.selectColumn")}</th>
                  <th scope="col">{t("classRoster.memberColumn")}</th>
                  <th scope="col">{t("classRoster.roleColumn")}</th>
                  <th scope="col">{t("classRoster.statusColumn")}</th>
                  <th scope="col">{t("classRoster.actionsColumn")}</th>
                </tr>
              </thead>
              <tbody>
                {members.map((member) => {
                  const canSelect =
                    member.actions.assignable_roles.length > 0 ||
                    member.actions.can_suspend ||
                    member.actions.can_remove;
                  const roleOptions = [
                    member.enrollment.class_role,
                    ...member.actions.assignable_roles,
                  ].filter(
                    (role, index, roles) => roles.indexOf(role) === index,
                  );
                  return (
                    <tr key={member.user.id}>
                      <td>
                        <input
                          aria-label={t("classRoster.selectMember", {
                            name: member.user.display_name,
                          })}
                          checked={selected.has(member.user.id)}
                          disabled={!canSelect || classroom.status !== "active"}
                          onChange={(event) =>
                            toggleSelected(member.user.id, event.target.checked)
                          }
                          type="checkbox"
                        />
                      </td>
                      <td>
                        <strong>{member.user.display_name}</strong>
                        <span>{member.user.email}</span>
                      </td>
                      <td>
                        {roleOptions.length > 1 ? (
                          <Select
                            ariaLabel={t("classRoster.changeRoleFor", {
                              name: member.user.display_name,
                            })}
                            disabled={classroom.status !== "active"}
                            onValueChange={(value) =>
                              openSingleRole(
                                member,
                                value as ClassEnrollmentRole,
                              )
                            }
                            options={roleOptions.map((role) => ({
                              label: t(roleKey(role)),
                              value: role,
                            }))}
                            value={member.enrollment.class_role}
                          />
                        ) : (
                          <span>
                            {t(roleKey(member.enrollment.class_role))}
                          </span>
                        )}
                      </td>
                      <td>
                        <StatusBadge
                          tone={statusTone(member.enrollment.status)}
                        >
                          {t(statusKey(member.enrollment.status))}
                        </StatusBadge>
                      </td>
                      <td>
                        <div className="class-roster__row-actions">
                          {member.actions.can_suspend && (
                            <Button
                              onClick={() =>
                                openSingleStatus(member, "suspend")
                              }
                              size="sm"
                              variant="quiet"
                            >
                              {t("classRoster.suspendAction")}
                            </Button>
                          )}
                          {member.actions.can_remove && (
                            <Button
                              onClick={() => openSingleStatus(member, "remove")}
                              size="sm"
                              variant="danger"
                            >
                              {t("classRoster.removeAction")}
                            </Button>
                          )}
                          {!canSelect && (
                            <span>{t("classRoster.noActions")}</span>
                          )}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          {roster.isFetchNextPageError && (
            <div className="class-roster__pagination-error" role="alert">
              <span>{t("classRoster.loadMoreError")}</span>
              <Button
                onClick={() => void roster.fetchNextPage()}
                size="sm"
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            </div>
          )}
          {roster.hasNextPage && !roster.isFetchNextPageError && (
            <div className="class-roster__pagination">
              <Button
                leadingIcon={<ChevronDown />}
                loading={roster.isFetchingNextPage}
                loadingLabel={t("classRoster.loadingMore")}
                onClick={() => void roster.fetchNextPage()}
                variant="secondary"
              >
                {t("classRoster.loadMore")}
              </Button>
            </div>
          )}
        </>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (!open && !mutationPending) {
            setPending(null);
            updateRole.reset();
            bulkMutate.reset();
          }
        }}
        open={Boolean(pending)}
      >
        <DialogContent closeLabel={t("classRoster.closeDialog")}>
          <DialogTitle>{t("classRoster.confirmTitle")}</DialogTitle>
          <DialogDescription>
            {pending?.displayName
              ? t("classRoster.confirmSingle", {
                  action: operationLabel,
                  name: pending.displayName,
                })
              : t("classRoster.confirmBulk", {
                  action: operationLabel,
                  count: pending?.userIDs.length ?? 0,
                })}
          </DialogDescription>
          {mutation.isError && (
            <p
              className="class-roster__feedback class-roster__feedback--error"
              role="alert"
            >
              {t(mutationErrorKey(mutation.error))}
            </p>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={mutationPending} variant="secondary">
                {t("classRoster.cancelAction")}
              </Button>
            </DialogClose>
            <Button
              loading={mutationPending}
              loadingLabel={t("classRoster.applying")}
              onClick={confirmPending}
              variant={pending?.action === "remove" ? "danger" : "primary"}
            >
              {t("classRoster.confirmAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
