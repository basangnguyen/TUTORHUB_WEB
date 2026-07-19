import { APIRequestError, type ClassroomClass } from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  SelectField,
  Skeleton,
  SkeletonGroup,
  TextAreaField,
  TextField,
} from "@tutorhub/ui";
import { ChevronDown, Plus, RotateCw } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  useClassDetail,
  useClassList,
  useCreateClass,
  type ClassStatusFilter,
} from "../app/classes";
import { useLeaveClass } from "../app/classEnrollments";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { useTenantDetail } from "../app/workspaces";
import { ClassEnrollmentPanel } from "../components/ClassEnrollmentPanel";
import { ClassJoinDialog } from "../components/ClassJoinDialog";
import { ClassManagementPanel } from "../components/ClassManagementPanel";
import { ClassRosterPanel } from "../components/ClassRosterPanel";

const classCodePattern = /^[A-Z0-9][A-Z0-9_-]{2,31}$/;

function leaveClassErrorKey(error: Error | null): TranslationKey {
  if (error instanceof APIRequestError && error.status === 403) {
    return "classEnrollment.leaveForbidden";
  }
  if (error instanceof APIRequestError && error.status === 404) {
    return "classEnrollment.leaveNotFound";
  }
  if (error instanceof APIRequestError && error.status === 409) {
    return "classEnrollment.leaveConflict";
  }
  return "classEnrollment.leaveError";
}

export function ClassroomListPage() {
  const { t } = useI18n();
  const session = useSession();
  const activeTenant = session.currentUser?.active_tenant;
  const canCreate =
    session.currentUser?.permissions.includes("class.create") ?? false;
  const [statusFilter, setStatusFilter] = useState<ClassStatusFilter>("all");
  const classesQuery = useClassList(activeTenant?.id, statusFilter);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [joinedClass, setJoinedClass] = useState<{
    classroom: ClassroomClass;
    tenantID: string;
  } | null>(null);
  const classrooms = useMemo(() => {
    const byID = new Map<string, ClassroomClass>();
    for (const page of classesQuery.data?.pages ?? []) {
      for (const classroom of page.items) {
        if (!byID.has(classroom.id)) {
          byID.set(classroom.id, classroom);
        }
      }
    }
    return [...byID.values()];
  }, [classesQuery.data?.pages]);
  const classesConcealed = shouldConcealTenantScopedData(classesQuery.error);

  return (
    <div className="page-content classroom-page">
      <header className="classroom-heading">
        <div>
          <p>{activeTenant?.name}</p>
          <h1>{t("classroom.title")}</h1>
          <span>{t("classroom.description")}</span>
        </div>
        {!classesConcealed && (
          <div className="classroom-heading__actions">
            {activeTenant && (
              <ClassJoinDialog
                onJoined={(classroom, tenantID) => {
                  setStatusFilter("all");
                  setJoinedClass({ classroom, tenantID });
                }}
                tenantID={activeTenant.id}
              />
            )}
            {canCreate && (
              <Button
                aria-expanded={isCreateOpen}
                aria-haspopup="dialog"
                className="classroom-primary-action"
                leadingIcon={<Plus />}
                onClick={() => setIsCreateOpen(true)}
              >
                {t("classroom.createAction")}
              </Button>
            )}
          </div>
        )}
      </header>

      {!classesConcealed && canCreate && (
        <CreateClassDialog onOpenChange={setIsCreateOpen} open={isCreateOpen} />
      )}

      {!classesConcealed &&
        joinedClass &&
        joinedClass.tenantID === activeTenant?.id && (
          <p className="classroom-join-feedback" role="status">
            <span>
              {t("classInvitation.joinedSuccess", {
                title: joinedClass.classroom.title,
              })}
            </span>
            <Link to={`/app/classrooms/${joinedClass.classroom.id}`}>
              {t("classInvitation.openJoinedClass")}
            </Link>
          </p>
        )}

      <section aria-labelledby="class-list-heading" className="classroom-index">
        <div className="classroom-index__heading">
          <div>
            <h2 id="class-list-heading">{t("classroom.listTitle")}</h2>
            <p>{t("classroom.listDescription")}</p>
          </div>
          {!classesConcealed && classesQuery.data && (
            <span>
              {t("classroom.classCount", {
                count: classrooms.length,
              })}
            </span>
          )}
        </div>

        {!classesConcealed && (
          <div className="classroom-list-controls">
            <SelectField
              ariaLabel={t("classroom.statusFilterLabel")}
              label={t("classroom.statusFilterLabel")}
              onValueChange={(value) =>
                setStatusFilter(value as ClassStatusFilter)
              }
              options={[
                { label: t("classroom.statusFilterAll"), value: "all" },
                { label: t("classroom.statusDraft"), value: "draft" },
                { label: t("classroom.statusActive"), value: "active" },
                { label: t("classroom.statusArchived"), value: "archived" },
              ]}
              value={statusFilter}
            />
          </div>
        )}

        {classesQuery.isPending && <ClassListSkeleton />}
        {classesQuery.isError &&
          (classesConcealed || classrooms.length === 0) && (
            <ClassroomQueryError
              error={classesQuery.error}
              onRetry={() => void classesQuery.refetch()}
            />
          )}
        {!classesConcealed && classesQuery.data && classrooms.length === 0 && (
          <ClassroomEmptyState
            canCreate={canCreate}
            filtered={statusFilter !== "all"}
            onCreate={() => setIsCreateOpen(true)}
          />
        )}
        {!classesConcealed && classesQuery.data && classrooms.length > 0 && (
          <>
            <ClassList classes={classrooms} />
            {classesQuery.isFetchNextPageError && (
              <div className="classroom-pagination-error" role="alert">
                <span>{t("classroom.loadMoreError")}</span>
                <Button
                  onClick={() => void classesQuery.fetchNextPage()}
                  size="sm"
                  variant="secondary"
                >
                  {t("state.retry")}
                </Button>
              </div>
            )}
            {classesQuery.hasNextPage && !classesQuery.isFetchNextPageError && (
              <div className="classroom-pagination">
                <Button
                  leadingIcon={<ChevronDown />}
                  loading={classesQuery.isFetchingNextPage}
                  loadingLabel={t("classroom.loadingMore")}
                  onClick={() => void classesQuery.fetchNextPage()}
                  variant="secondary"
                >
                  {t("classroom.loadMore")}
                </Button>
              </div>
            )}
          </>
        )}
      </section>
    </div>
  );
}

export function ClassroomDetailPage() {
  const { classId } = useParams();
  const { language, t } = useI18n();
  const session = useSession();
  const activeTenant = session.currentUser?.active_tenant;
  const classQuery = useClassDetail(activeTenant?.id, classId);

  if (classQuery.isPending) {
    return (
      <div className="page-content classroom-page">
        <ClassDetailSkeleton />
      </div>
    );
  }
  if (classQuery.isError) {
    return (
      <div className="page-content classroom-page">
        <Link className="classroom-back-link" to="/app/classrooms">
          {t("classroom.backToList")}
        </Link>
        <ClassroomQueryError
          error={classQuery.error}
          onRetry={() => void classQuery.refetch()}
        />
      </div>
    );
  }

  const classroom = classQuery.data;
  const canJoin =
    classroom.status === "active" && classroom.viewer_access.can_join_room;
  const dateFormatter = new Intl.DateTimeFormat(
    language === "vi" ? "vi-VN" : "en-US",
    { dateStyle: "medium", timeStyle: "short" },
  );

  return (
    <article className="page-content classroom-page classroom-detail">
      <Link className="classroom-back-link" to="/app/classrooms">
        {t("classroom.backToList")}
      </Link>
      <header className="classroom-detail__header">
        <div>
          <div className="classroom-detail__identity">
            <code>{classroom.code}</code>
            <ClassStatus status={classroom.status} />
          </div>
          <h1>{classroom.title}</h1>
          <p>{classroom.description || t("classroom.noDescription")}</p>
        </div>
        {canJoin && (
          <Link
            className="classroom-live-action"
            to={`/app/classrooms/${classroom.id}/prejoin`}
          >
            <span aria-hidden="true" />
            {t("classroom.joinRoomAction")}
          </Link>
        )}
      </header>

      <section
        aria-labelledby="classroom-information-heading"
        className="classroom-detail__section"
      >
        <h2 id="classroom-information-heading">
          {t("classroom.informationTitle")}
        </h2>
        <dl className="classroom-detail__facts">
          <div>
            <dt>{t("classroom.workspaceLabel")}</dt>
            <dd>{activeTenant?.name}</dd>
          </div>
          <div>
            <dt>{t("classroom.ownerLabel")}</dt>
            <dd>
              {classroom.owner_user_id === session.currentUser?.user.id
                ? t("classroom.ownerYou")
                : t("classroom.ownerMember")}
            </dd>
          </div>
          <div>
            <dt>{t("classroom.timezoneLabel")}</dt>
            <dd>{classroom.timezone}</dd>
          </div>
          <div>
            <dt>{t("classroom.createdLabel")}</dt>
            <dd>{dateFormatter.format(new Date(classroom.created_at))}</dd>
          </div>
          <div>
            <dt>{t("classroom.updatedLabel")}</dt>
            <dd>{dateFormatter.format(new Date(classroom.updated_at))}</dd>
          </div>
          {classroom.archived_at && (
            <div>
              <dt>{t("classroom.archivedLabel")}</dt>
              <dd>{dateFormatter.format(new Date(classroom.archived_at))}</dd>
            </div>
          )}
        </dl>
      </section>

      <ClassManagementPanel
        classroom={classroom}
        onReload={async () => (await classQuery.refetch()).data}
      />
      <ClassRosterPanel classroom={classroom} />
      <ClassEnrollmentPanel classroom={classroom} />
      {classroom.viewer_access.can_leave && (
        <LeaveClassAction classroom={classroom} />
      )}
    </article>
  );
}

function LeaveClassAction({ classroom }: { classroom: ClassroomClass }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const session = useSession();
  const leaveClass = useLeaveClass();
  const [open, setOpen] = useState(false);
  const tenantID = session.currentUser?.active_tenant?.id;

  const leave = () => {
    if (!tenantID) {
      return;
    }
    leaveClass.mutate(
      { classID: classroom.id, tenantID },
      {
        onSuccess: () => {
          setOpen(false);
          navigate("/app/classrooms", { replace: true });
        },
      },
    );
  };

  return (
    <section
      aria-labelledby="classroom-leave-title"
      className="classroom-detail__section classroom-leave"
    >
      <div>
        <h2 id="classroom-leave-title">{t("classEnrollment.leaveAction")}</h2>
        <p>{t("classEnrollment.leaveDescription")}</p>
      </div>
      <Button onClick={() => setOpen(true)} variant="danger">
        {t("classEnrollment.leaveAction")}
      </Button>

      <Dialog
        onOpenChange={(nextOpen) => {
          if (!leaveClass.isPending) {
            setOpen(nextOpen);
            if (!nextOpen) {
              leaveClass.reset();
            }
          }
        }}
        open={open}
      >
        <DialogContent closeLabel={t("classEnrollment.closeDialog")}>
          <DialogTitle>{t("classEnrollment.leaveTitle")}</DialogTitle>
          <DialogDescription>
            {t("classEnrollment.leaveDescription")}
          </DialogDescription>
          {leaveClass.isError && (
            <p className="class-enrollments__error" role="alert">
              {t(leaveClassErrorKey(leaveClass.error))}
            </p>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={leaveClass.isPending} variant="secondary">
                {t("classEnrollment.cancelAction")}
              </Button>
            </DialogClose>
            <Button
              loading={leaveClass.isPending}
              loadingLabel={t("classEnrollment.leaving")}
              onClick={leave}
              variant="danger"
            >
              {t("classEnrollment.leaveConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function CreateClassDialog({
  onOpenChange,
  open,
}: {
  onOpenChange: (open: boolean) => void;
  open: boolean;
}) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const session = useSession();
  const activeTenantID = session.currentUser?.active_tenant?.id;
  const createClass = useCreateClass(activeTenantID);
  const activeTenant = useTenantDetail(open ? activeTenantID : undefined);
  const [code, setCode] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [timezone, setTimezone] = useState("");
  const [timezoneTouched, setTimezoneTouched] = useState(false);
  const [validationError, setValidationError] = useState<TranslationKey | null>(
    null,
  );

  const displayedTimezone = timezoneTouched
    ? timezone
    : (activeTenant.data?.timezone ??
      session.currentUser?.user.timezone ??
      "UTC");

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalizedCode = code.trim().toUpperCase();
    const normalizedTitle = title.trim();

    if (!classCodePattern.test(normalizedCode)) {
      setValidationError("classroom.codeError");
      return;
    }
    const normalizedTitleLength = Array.from(normalizedTitle).length;
    if (normalizedTitleLength < 1 || normalizedTitleLength > 200) {
      setValidationError("classroom.titleError");
      return;
    }
    if (Array.from(description.trim()).length > 4000) {
      setValidationError("classroom.descriptionError");
      return;
    }
    const normalizedTimezone = displayedTimezone.trim();
    if (
      !normalizedTimezone ||
      normalizedTimezone.length > 100 ||
      !isValidTimeZone(normalizedTimezone)
    ) {
      setValidationError("classroom.timezoneError");
      return;
    }

    setValidationError(null);
    createClass.mutate(
      {
        code: normalizedCode,
        title: normalizedTitle,
        description: description.trim(),
        timezone: normalizedTimezone,
      },
      {
        onSuccess: (created) => {
          onOpenChange(false);
          void navigate(`/app/classrooms/${created.id}`);
        },
      },
    );
  };

  return (
    <Dialog onOpenChange={onOpenChange} open={open}>
      <DialogContent
        className="class-create-dialog"
        closeLabel={t("classroom.closeCreate")}
      >
        <DialogTitle>{t("classroom.createTitle")}</DialogTitle>
        <DialogDescription>
          {t("classroom.createDescription")}
        </DialogDescription>

        <form className="class-create-form" onSubmit={submit}>
          <TextField
            autoComplete="off"
            hint={t("classroom.codeHelp")}
            label={t("classroom.codeLabel")}
            maxLength={32}
            onChange={(event) => setCode(event.target.value.toUpperCase())}
            placeholder={t("classroom.codePlaceholder")}
            required
            value={code}
          />
          <TextField
            label={t("classroom.titleLabel")}
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={t("classroom.titlePlaceholder")}
            required
            value={title}
          />
          <TextField
            autoComplete="off"
            hint={t("classroom.timezoneHelp")}
            label={t("classroom.timezoneLabel")}
            maxLength={100}
            onChange={(event) => {
              setTimezoneTouched(true);
              setTimezone(event.target.value);
            }}
            required
            value={displayedTimezone}
          />
          <TextAreaField
            className="class-create-form__description"
            hint={`${Array.from(description).length}/4000`}
            label={t("classroom.descriptionLabel")}
            maxLength={4000}
            onChange={(event) => setDescription(event.target.value)}
            placeholder={t("classroom.descriptionPlaceholder")}
            rows={4}
            value={description}
          />

          {(validationError || createClass.isError) && (
            <p className="class-create-form__error" role="alert">
              {validationError
                ? t(validationError)
                : getCreateErrorMessage(createClass.error, t)}
            </p>
          )}

          <DialogFooter className="class-create-form__actions">
            <DialogClose asChild>
              <Button variant="secondary">{t("classroom.cancelAction")}</Button>
            </DialogClose>
            <Button
              loading={createClass.isPending}
              loadingLabel={t("classroom.creating")}
              type="submit"
            >
              {t("classroom.createSubmit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ClassList({ classes }: { classes: readonly ClassroomClass[] }) {
  const { language, t } = useI18n();
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
      }),
    [language],
  );

  return (
    <ul className="class-list">
      {classes.map((classroom) => (
        <li key={classroom.id}>
          <Link to={`/app/classrooms/${classroom.id}`}>
            <span className="class-list__identity">
              <code>{classroom.code}</code>
              <strong>{classroom.title}</strong>
              <small>
                {classroom.description || t("classroom.noDescription")}
              </small>
            </span>
            <ClassStatus status={classroom.status} />
            <time dateTime={classroom.updated_at}>
              {t("classroom.updatedShort", {
                date: dateFormatter.format(new Date(classroom.updated_at)),
              })}
            </time>
            <span aria-hidden="true" className="class-list__arrow">
              →
            </span>
          </Link>
        </li>
      ))}
    </ul>
  );
}

function ClassStatus({ status }: { status: ClassroomClass["status"] }) {
  const { t } = useI18n();
  const key: TranslationKey =
    status === "active"
      ? "classroom.statusActive"
      : status === "archived"
        ? "classroom.statusArchived"
        : "classroom.statusDraft";

  return (
    <span className="class-status" data-status={status}>
      {t(key)}
    </span>
  );
}

function ClassroomEmptyState({
  canCreate,
  filtered,
  onCreate,
}: {
  canCreate: boolean;
  filtered: boolean;
  onCreate: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="classroom-empty-state">
      <span aria-hidden="true">01</span>
      <h3>
        {filtered
          ? t("classroom.emptyFilteredTitle")
          : t("classroom.emptyTitle")}
      </h3>
      <p>
        {filtered
          ? t("classroom.emptyFilteredDescription")
          : t("classroom.emptyDescription")}
      </p>
      {canCreate && (
        <Button leadingIcon={<Plus />} onClick={onCreate} size="sm">
          {t("classroom.createFirstAction")}
        </Button>
      )}
    </div>
  );
}

function ClassroomQueryError({
  error,
  onRetry,
}: {
  error: Error;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  const isForbidden = error instanceof APIRequestError && error.status === 403;
  const isNotFound = error instanceof APIRequestError && error.status === 404;
  return (
    <div className="classroom-error" role="alert">
      <div>
        <strong>
          {isForbidden
            ? t("classroom.forbiddenTitle")
            : isNotFound
              ? t("classroom.notFoundTitle")
              : t("classroom.errorTitle")}
        </strong>
        <p>
          {isForbidden
            ? t("classroom.forbiddenDescription")
            : isNotFound
              ? t("classroom.notFoundDescription")
              : t("classroom.errorDescription")}
        </p>
      </div>
      {!isNotFound && (
        <Button
          leadingIcon={<RotateCw />}
          onClick={onRetry}
          size="sm"
          variant="secondary"
        >
          {t("state.retry")}
        </Button>
      )}
    </div>
  );
}

function ClassListSkeleton() {
  const { t } = useI18n();
  return (
    <SkeletonGroup
      className="class-list-skeleton"
      label={t("classroom.loadingList")}
    >
      {[0, 1, 2].map((item) => (
        <Skeleton key={item} />
      ))}
    </SkeletonGroup>
  );
}

function ClassDetailSkeleton() {
  const { t } = useI18n();
  return (
    <SkeletonGroup
      className="class-detail-skeleton"
      label={t("classroom.loadingDetail")}
    >
      <Skeleton />
      <Skeleton />
      <Skeleton />
    </SkeletonGroup>
  );
}

function getCreateErrorMessage(
  error: Error | null,
  t: (key: TranslationKey) => string,
) {
  if (error instanceof APIRequestError && error.status === 409) {
    return t("classroom.duplicateCodeError");
  }
  if (error instanceof APIRequestError && error.status === 403) {
    return t("classroom.createForbiddenError");
  }
  return t("classroom.createError");
}

function isValidTimeZone(timezone: string) {
  try {
    new Intl.DateTimeFormat("en", { timeZone: timezone }).format();
    return true;
  } catch {
    return false;
  }
}
