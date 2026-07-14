import { APIRequestError, type ClassroomClass } from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  Skeleton,
  SkeletonGroup,
  TextAreaField,
  TextField,
} from "@tutorhub/ui";
import { Plus, RotateCw } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useClassDetail, useClassList, useCreateClass } from "../app/classes";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";

const classCodePattern = /^[A-Z0-9][A-Z0-9_-]{2,31}$/;

export function ClassroomListPage() {
  const { t } = useI18n();
  const session = useSession();
  const activeTenant = session.currentUser?.active_tenant;
  const canCreate =
    session.currentUser?.permissions.includes("class.create") ?? false;
  const classesQuery = useClassList(activeTenant?.id);
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  return (
    <div className="page-content classroom-page">
      <header className="classroom-heading">
        <div>
          <p>{activeTenant?.name}</p>
          <h1>{t("classroom.title")}</h1>
          <span>{t("classroom.description")}</span>
        </div>
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
      </header>

      {canCreate && (
        <CreateClassDialog onOpenChange={setIsCreateOpen} open={isCreateOpen} />
      )}

      <section aria-labelledby="class-list-heading" className="classroom-index">
        <div className="classroom-index__heading">
          <div>
            <h2 id="class-list-heading">{t("classroom.listTitle")}</h2>
            <p>{t("classroom.listDescription")}</p>
          </div>
          {classesQuery.isSuccess && (
            <span>
              {t("classroom.classCount", {
                count: classesQuery.data.items.length,
              })}
            </span>
          )}
        </div>

        {classesQuery.isPending && <ClassListSkeleton />}
        {classesQuery.isError && (
          <ClassroomQueryError
            error={classesQuery.error}
            onRetry={() => void classesQuery.refetch()}
          />
        )}
        {classesQuery.isSuccess && classesQuery.data.items.length === 0 && (
          <ClassroomEmptyState
            canCreate={canCreate}
            onCreate={() => setIsCreateOpen(true)}
          />
        )}
        {classesQuery.isSuccess && classesQuery.data.items.length > 0 && (
          <ClassList classes={classesQuery.data.items} />
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
    classroom.status !== "archived" &&
    (session.currentUser?.permissions.includes("session.join") ?? false);
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
            <dt>{t("classroom.createdLabel")}</dt>
            <dd>{dateFormatter.format(new Date(classroom.created_at))}</dd>
          </div>
          <div>
            <dt>{t("classroom.updatedLabel")}</dt>
            <dd>{dateFormatter.format(new Date(classroom.updated_at))}</dd>
          </div>
        </dl>
      </section>
    </article>
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
  const createClass = useCreateClass(session.currentUser?.active_tenant?.id);
  const [code, setCode] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [validationError, setValidationError] = useState<TranslationKey | null>(
    null,
  );

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

    setValidationError(null);
    createClass.mutate(
      {
        code: normalizedCode,
        title: normalizedTitle,
        description: description.trim(),
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
  onCreate,
}: {
  canCreate: boolean;
  onCreate: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="classroom-empty-state">
      <span aria-hidden="true">01</span>
      <h3>{t("classroom.emptyTitle")}</h3>
      <p>{t("classroom.emptyDescription")}</p>
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
