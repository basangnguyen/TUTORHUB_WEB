import {
  APIRequestError,
  type ClassStatus,
  type ClassroomClass,
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
  SelectField,
  TextAreaField,
  TextField,
} from "@tutorhub/ui";
import { Archive, RotateCcw, Save } from "lucide-react";
import { useState, type FormEvent } from "react";
import {
  useArchiveClass,
  useRestoreClass,
  useUpdateClass,
} from "../app/classes";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import type { TenantOperationAvailability } from "../app/tenantCapabilities";
import { TenantOperationNotice } from "./TenantOperationNotice";

const classCodePattern = /^[A-Z0-9][A-Z0-9_-]{2,31}$/;
type EditableClassStatus = Extract<ClassStatus, "draft" | "active">;

interface ClassDraft {
  base: {
    classID: string;
    code: string;
    description: string;
    status: EditableClassStatus;
    timezone: string;
    title: string;
    version: number;
  };
  code: string;
  description: string;
  status: EditableClassStatus;
  timezone: string;
  title: string;
}

interface ClassFormErrors {
  code?: string;
  description?: string;
  timezone?: string;
  title?: string;
}

function classDraft(classroom: ClassroomClass): ClassDraft {
  const status = classroom.status === "active" ? "active" : "draft";
  return {
    base: {
      classID: classroom.id,
      code: classroom.code,
      description: classroom.description,
      status,
      timezone: classroom.timezone,
      title: classroom.title,
      version: classroom.version,
    },
    code: classroom.code,
    description: classroom.description,
    status,
    timezone: classroom.timezone,
    title: classroom.title,
  };
}

function classDraftChanged(draft: ClassDraft) {
  return (
    draft.code.trim().toUpperCase() !== draft.base.code ||
    draft.description.trim() !== draft.base.description ||
    draft.status !== draft.base.status ||
    draft.timezone.trim() !== draft.base.timezone ||
    draft.title.trim() !== draft.base.title
  );
}

function isValidTimeZone(timezone: string) {
  try {
    new Intl.DateTimeFormat("en", { timeZone: timezone }).format();
    return true;
  } catch {
    return false;
  }
}

function isConflict(error: Error | null) {
  return (
    error instanceof APIRequestError &&
    error.status === 409 &&
    error.problem?.code !== "quota_exceeded"
  );
}

function isDuplicateCode(error: Error | null) {
  return (
    error instanceof APIRequestError &&
    error.status === 409 &&
    error.problem?.title === "Class code already exists"
  );
}

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function expansionControlErrorKey(error: Error | null): TranslationKey | null {
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
  return null;
}

export function ClassManagementPanel({
  activateAvailability,
  classroom,
  onReload,
  onRetryCapabilities,
  restoreAvailability,
}: {
  activateAvailability: TenantOperationAvailability;
  classroom: ClassroomClass;
  onReload: () => Promise<ClassroomClass | undefined>;
  onRetryCapabilities: () => void;
  restoreAvailability: TenantOperationAvailability;
}) {
  const canEdit =
    classroom.status !== "archived" && classroom.viewer_access.can_update_class;
  const canManageLifecycle = classroom.viewer_access.can_archive_class;

  if (!canEdit && !canManageLifecycle) {
    return null;
  }

  return (
    <div className="class-management">
      {canEdit && (
        <ClassEditPanel
          activateAvailability={activateAvailability}
          classroom={classroom}
          onReload={onReload}
          onRetryCapabilities={onRetryCapabilities}
        />
      )}
      {canManageLifecycle && (
        <ClassLifecyclePanel
          classroom={classroom}
          onReload={onReload}
          onRetryCapabilities={onRetryCapabilities}
          restoreAvailability={restoreAvailability}
        />
      )}
    </div>
  );
}

function ClassEditPanel({
  activateAvailability,
  classroom,
  onReload,
  onRetryCapabilities,
}: {
  activateAvailability: TenantOperationAvailability;
  classroom: ClassroomClass;
  onReload: () => Promise<ClassroomClass | undefined>;
  onRetryCapabilities: () => void;
}) {
  const { t } = useI18n();
  const activeTenantID = useSession().currentUser?.active_tenant?.id;
  const updateClass = useUpdateClass(activeTenantID);
  const [draftOverride, setDraftOverride] = useState<ClassDraft | null>(null);
  const [errors, setErrors] = useState<ClassFormErrors>({});
  const [feedback, setFeedback] = useState<string | null>(null);

  const draft =
    draftOverride?.base.classID === classroom.id &&
    classDraftChanged(draftOverride)
      ? draftOverride
      : classDraft(classroom);
  const changed = classDraftChanged(draft);
  const activatesClass =
    draft.base.status === "draft" && draft.status === "active";

  const updateDraft = (change: Partial<Omit<ClassDraft, "base">>) => {
    setDraftOverride((current) => {
      const source =
        current?.base.classID === classroom.id && classDraftChanged(current)
          ? current
          : classDraft(classroom);
      return { ...source, ...change };
    });
    setFeedback(null);
    if (updateClass.isError) {
      updateClass.reset();
    }
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const code = draft.code.trim().toUpperCase();
    const title = draft.title.trim();
    const description = draft.description.trim();
    const timezone = draft.timezone.trim();
    const nextErrors: ClassFormErrors = {};

    if (!classCodePattern.test(code)) {
      nextErrors.code = t("classroom.codeError");
    }
    const titleLength = Array.from(title).length;
    if (titleLength < 1 || titleLength > 200) {
      nextErrors.title = t("classroom.titleError");
    }
    if (Array.from(description).length > 4000) {
      nextErrors.description = t("classroom.descriptionError");
    }
    if (!timezone || timezone.length > 100 || !isValidTimeZone(timezone)) {
      nextErrors.timezone = t("classroom.timezoneError");
    }

    setErrors(nextErrors);
    setFeedback(null);
    if (
      Object.keys(nextErrors).length > 0 ||
      !changed ||
      (activatesClass && !activateAvailability.available)
    ) {
      return;
    }

    updateClass.mutate(
      {
        classID: classroom.id,
        input: {
          code,
          description,
          expected_version: draft.base.version,
          status: draft.status,
          timezone,
          title,
        },
      },
      {
        onSuccess: () => {
          setDraftOverride(null);
          setFeedback(t("classroom.updateSuccess"));
        },
      },
    );
  };

  const reloadLatest = async () => {
    const latest = await onReload();
    if (latest) {
      setDraftOverride(null);
      setErrors({});
    }
    updateClass.reset();
    setFeedback(null);
  };

  return (
    <section
      aria-labelledby="class-edit-heading"
      className="class-management__panel"
    >
      <div className="class-management__panel-heading">
        <div>
          <h2 id="class-edit-heading">{t("classroom.editTitle")}</h2>
          <p>{t("classroom.editDescription")}</p>
        </div>
      </div>

      <form className="class-management__form" onSubmit={submit}>
        <TextField
          autoComplete="off"
          error={errors.code}
          hint={t("classroom.codeHelp")}
          label={t("classroom.codeLabel")}
          maxLength={32}
          onChange={(event) =>
            updateDraft({ code: event.target.value.toUpperCase() })
          }
          required
          value={draft.code}
        />
        <TextField
          error={errors.title}
          label={t("classroom.titleLabel")}
          maxLength={200}
          onChange={(event) => updateDraft({ title: event.target.value })}
          required
          value={draft.title}
        />
        <TextField
          autoComplete="off"
          error={errors.timezone}
          hint={t("classroom.timezoneHelp")}
          label={t("classroom.timezoneLabel")}
          maxLength={100}
          onChange={(event) => updateDraft({ timezone: event.target.value })}
          required
          value={draft.timezone}
        />
        {classroom.status === "draft" && (
          <>
            <SelectField
              ariaLabel={t("classroom.statusLabel")}
              hint={t("classroom.statusHelp")}
              label={t("classroom.statusLabel")}
              onValueChange={(value) =>
                updateDraft({ status: value as EditableClassStatus })
              }
              options={[
                { label: t("classroom.statusDraft"), value: "draft" },
                {
                  disabled: !activateAvailability.available,
                  label: t("classroom.statusActive"),
                  value: "active",
                },
              ]}
              value={draft.status}
            />
            <TenantOperationNotice
              availability={activateAvailability}
              label={t("capabilities.operationActivateClass")}
              onRetry={onRetryCapabilities}
            />
          </>
        )}
        <TextAreaField
          className="class-management__description"
          error={errors.description}
          hint={`${Array.from(draft.description).length}/4000`}
          label={t("classroom.descriptionLabel")}
          maxLength={4000}
          onChange={(event) => updateDraft({ description: event.target.value })}
          rows={4}
          value={draft.description}
        />

        {updateClass.isError && (
          <div className="class-management__feedback" role="alert">
            <span>
              {expansionControlErrorKey(updateClass.error)
                ? t(expansionControlErrorKey(updateClass.error)!)
                : isDuplicateCode(updateClass.error)
                  ? t("classroom.duplicateCodeError")
                  : isConflict(updateClass.error)
                    ? t("classroom.updateConflict")
                    : isForbidden(updateClass.error)
                      ? t("classroom.updateForbidden")
                      : t("classroom.updateError")}
            </span>
            {isConflict(updateClass.error) &&
              !isDuplicateCode(updateClass.error) && (
                <Button
                  onClick={() => void reloadLatest()}
                  size="sm"
                  variant="secondary"
                >
                  {t("classroom.reloadLatest")}
                </Button>
              )}
          </div>
        )}

        {feedback && (
          <p className="class-management__success" role="status">
            {feedback}
          </p>
        )}

        <div className="class-management__form-actions">
          <Button
            disabled={
              !changed || (activatesClass && !activateAvailability.available)
            }
            leadingIcon={<Save />}
            loading={updateClass.isPending}
            loadingLabel={t("classroom.updating")}
            type="submit"
          >
            {t("classroom.updateAction")}
          </Button>
        </div>
      </form>
    </section>
  );
}

function ClassLifecyclePanel({
  classroom,
  onReload,
  onRetryCapabilities,
  restoreAvailability,
}: {
  classroom: ClassroomClass;
  onReload: () => Promise<ClassroomClass | undefined>;
  onRetryCapabilities: () => void;
  restoreAvailability: TenantOperationAvailability;
}) {
  const { t } = useI18n();
  const activeTenantID = useSession().currentUser?.active_tenant?.id;
  const archiveClass = useArchiveClass(activeTenantID);
  const restoreClass = useRestoreClass(activeTenantID);
  const [open, setOpen] = useState(false);
  const isRestore = classroom.status === "archived";
  const mutation = isRestore ? restoreClass : archiveClass;

  const reloadLatest = async () => {
    await onReload();
    archiveClass.reset();
    restoreClass.reset();
    setOpen(false);
  };

  const mutate = () => {
    if (isRestore && !restoreAvailability.available) {
      return;
    }
    const variables = {
      classID: classroom.id,
      input: { expected_version: classroom.version },
    };
    const options = {
      onSuccess: () => setOpen(false),
    };
    if (isRestore) {
      restoreClass.mutate(variables, options);
    } else {
      archiveClass.mutate(variables, options);
    }
  };

  return (
    <section
      aria-labelledby="class-lifecycle-heading"
      className={`class-management__panel${
        isRestore ? "" : " class-management__panel--danger"
      }`}
    >
      <div className="class-management__lifecycle-copy">
        <h2 id="class-lifecycle-heading">
          {isRestore
            ? t("classroom.restoreTitle")
            : t("classroom.archiveTitle")}
        </h2>
        <p>
          {isRestore
            ? t("classroom.restoreDescription")
            : t("classroom.archiveDescription")}
        </p>
      </div>

      {isRestore && (
        <TenantOperationNotice
          availability={restoreAvailability}
          label={t("capabilities.operationRestoreClass")}
          onRetry={onRetryCapabilities}
        />
      )}

      <Dialog
        onOpenChange={(nextOpen) => {
          if (!mutation.isPending) {
            setOpen(nextOpen);
            if (!nextOpen) {
              archiveClass.reset();
              restoreClass.reset();
            }
          }
        }}
        open={open}
      >
        <DialogTrigger asChild>
          <Button
            disabled={isRestore && !restoreAvailability.available}
            leadingIcon={isRestore ? <RotateCcw /> : <Archive />}
            variant={isRestore ? "secondary" : "danger"}
          >
            {isRestore
              ? t("classroom.restoreAction")
              : t("classroom.archiveAction")}
          </Button>
        </DialogTrigger>
        <DialogContent
          closeLabel={
            isRestore
              ? t("classroom.restoreCloseLabel")
              : t("classroom.archiveCloseLabel")
          }
        >
          <DialogTitle>
            {isRestore
              ? t("classroom.restoreConfirmTitle")
              : t("classroom.archiveConfirmTitle")}
          </DialogTitle>
          <DialogDescription>
            {isRestore
              ? t("classroom.restoreConfirmDescription", {
                  name: classroom.title,
                })
              : t("classroom.archiveConfirmDescription", {
                  name: classroom.title,
                })}
          </DialogDescription>
          <p className="class-management__lifecycle-warning">
            {isRestore
              ? t("classroom.restoreWarning")
              : t("classroom.archiveWarning")}
          </p>

          {mutation.isError && (
            <div className="class-management__feedback" role="alert">
              <span>
                {expansionControlErrorKey(mutation.error)
                  ? t(expansionControlErrorKey(mutation.error)!)
                  : isConflict(mutation.error)
                    ? t("classroom.lifecycleConflict")
                    : isForbidden(mutation.error)
                      ? t("classroom.lifecycleForbidden")
                      : isRestore
                        ? t("classroom.restoreError")
                        : t("classroom.archiveError")}
              </span>
              {isConflict(mutation.error) && (
                <Button
                  onClick={() => void reloadLatest()}
                  size="sm"
                  variant="secondary"
                >
                  {t("classroom.reloadLatest")}
                </Button>
              )}
            </div>
          )}

          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={mutation.isPending} variant="secondary">
                {t("classroom.cancelAction")}
              </Button>
            </DialogClose>
            <Button
              disabled={isRestore && !restoreAvailability.available}
              loading={mutation.isPending}
              loadingLabel={
                isRestore ? t("classroom.restoring") : t("classroom.archiving")
              }
              onClick={mutate}
              variant={isRestore ? "primary" : "danger"}
            >
              {isRestore
                ? t("classroom.restoreConfirmAction")
                : t("classroom.archiveConfirmAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
