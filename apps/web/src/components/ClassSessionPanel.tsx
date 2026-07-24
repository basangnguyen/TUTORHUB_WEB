import {
  APIRequestError,
  type CreateClassSessionRequest,
  type ClassSession,
  type ClassroomClass,
  type UpdateClassSessionRequest,
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
  OfflineState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextAreaField,
  TextField,
} from "@tutorhub/ui";
import { CalendarClock, Pencil, Plus, RotateCw, X } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import {
  civilInputFromInstant,
  isOrderedSessionRange,
  resolveCivilDateTime,
  type OverlapChoice,
} from "../app/classSessionTime";
import {
  useCancelClassSession,
  useClassSessionList,
  useCreateClassSession,
  useUpdateClassSession,
} from "../app/classSessions";
import { useI18n, type TranslationKey } from "../app/i18n";
import type { TenantOperationAvailability } from "../app/tenantCapabilities";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { useSession } from "../app/session";

interface ClassSessionPanelProps {
  classroom: ClassroomClass;
  schedulingAvailability: TenantOperationAvailability;
}

const rangeWindow = () => {
  const start = new Date();
  start.setUTCDate(start.getUTCDate() - 1);
  const end = new Date(start);
  end.setUTCDate(end.getUTCDate() + 92);
  return { start: start.toISOString(), end: end.toISOString() };
};

function sessionErrorKey(error: Error | null): TranslationKey {
  if (error instanceof APIRequestError && error.status === 403) {
    return "classSession.mutationForbidden";
  }
  if (error instanceof APIRequestError && error.status === 409) {
    return "classSession.conflict";
  }
  return "classSession.formError";
}

function formatSessionRange(
  session: ClassSession,
  viewerTimezone: string,
  locale: string,
) {
  const viewerFormatter = new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: viewerTimezone,
  });
  const classFormatter = new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: session.timezone,
  });
  const viewer = `${viewerFormatter.format(new Date(session.starts_at))} – ${viewerFormatter.format(new Date(session.ends_at))}`;
  const classTime =
    session.timezone === viewerTimezone
      ? undefined
      : `${classFormatter.format(new Date(session.starts_at))} – ${classFormatter.format(new Date(session.ends_at))}`;
  return { classTime, viewer };
}

export function ClassSessionPanel({
  classroom,
  schedulingAvailability,
}: ClassSessionPanelProps) {
  const { language, t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const viewerTimezone =
    session.currentUser?.user.timezone || classroom.timezone;
  const locale = language === "vi" ? "vi-VN" : "en-US";
  const range = useMemo(() => rangeWindow(), []);
  const sessionsQuery = useClassSessionList(
    tenantID,
    classroom.id,
    range.start,
    range.end,
  );
  const createMutation = useCreateClassSession(tenantID);
  const updateMutation = useUpdateClassSession(tenantID);
  const cancelMutation = useCancelClassSession(tenantID);
  const [editing, setEditing] = useState<ClassSession | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [cancelTarget, setCancelTarget] = useState<ClassSession | null>(null);

  const items = useMemo(() => {
    const seen = new Set<string>();
    return (sessionsQuery.data?.pages ?? [])
      .flatMap((page) => page.items)
      .filter((item) => {
        if (seen.has(item.id)) {
          return false;
        }
        seen.add(item.id);
        return true;
      });
  }, [sessionsQuery.data?.pages]);

  const canSchedule =
    schedulingAvailability.available &&
    classroom.viewer_access.can_schedule_sessions;
  const concealed = shouldConcealTenantScopedData(sessionsQuery.error);
  const isOffline = typeof navigator !== "undefined" && !navigator.onLine;

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };

  return (
    <section
      aria-labelledby="class-session-heading"
      className="classroom-detail__section class-session-panel"
    >
      <div className="class-session-panel__heading">
        <div>
          <h2 id="class-session-heading">
            <CalendarClock aria-hidden="true" /> {t("classSession.title")}
          </h2>
          <p>{t("classSession.description")}</p>
        </div>
        {canSchedule && (
          <Button leadingIcon={<Plus />} onClick={openCreate}>
            {t("classSession.scheduleAction")}
          </Button>
        )}
      </div>

      {sessionsQuery.isPending && (
        <SkeletonGroup
          label={t("classSession.loading")}
          className="class-session-list"
        >
          <Skeleton height="5rem" />
          <Skeleton height="5rem" />
        </SkeletonGroup>
      )}

      {!sessionsQuery.isPending &&
        sessionsQuery.isError &&
        concealed &&
        (sessionsQuery.error instanceof APIRequestError &&
        sessionsQuery.error.status === 403 ? (
          <ForbiddenState
            description={t("classSession.forbiddenDescription")}
            title={t("classSession.forbiddenTitle")}
          />
        ) : sessionsQuery.error instanceof APIRequestError &&
          sessionsQuery.error.status === 404 ? (
          <ErrorState
            description={t("classSession.notFoundDescription")}
            title={t("classSession.notFoundTitle")}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RotateCw />}
                onClick={() => void sessionsQuery.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("classSession.loadErrorDescription")}
            title={t("classSession.loadErrorTitle")}
          />
        ))}

      {!sessionsQuery.isPending && !concealed && isOffline && (
        <OfflineState
          actions={
            <Button
              leadingIcon={<RotateCw />}
              onClick={() => void sessionsQuery.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={t("classSession.loadErrorDescription")}
          title={t("state.offlineTitle")}
        />
      )}

      {!sessionsQuery.isPending &&
        !concealed &&
        !isOffline &&
        sessionsQuery.isError &&
        items.length === 0 && (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RotateCw />}
                onClick={() => void sessionsQuery.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("classSession.loadErrorDescription")}
            title={t("classSession.loadErrorTitle")}
          />
        )}

      {!sessionsQuery.isPending &&
        !concealed &&
        !sessionsQuery.isError &&
        items.length === 0 && (
          <EmptyState
            actions={
              canSchedule ? (
                <Button leadingIcon={<Plus />} onClick={openCreate}>
                  {t("classSession.scheduleAction")}
                </Button>
              ) : undefined
            }
            description={t("classSession.emptyDescription")}
            title={t("classSession.emptyTitle")}
          />
        )}

      {!concealed && items.length > 0 && (
        <div className="class-session-list">
          {items.map((item) => {
            const rangeText = formatSessionRange(item, viewerTimezone, locale);
            const editable = canSchedule && item.viewer_access.can_update;
            const cancellable = canSchedule && item.viewer_access.can_cancel;
            return (
              <article className="class-session-card" key={item.id}>
                <div className="class-session-card__body">
                  <div className="class-session-card__title-row">
                    <h3>{item.title}</h3>
                    <StatusBadge
                      tone={item.status === "cancelled" ? "danger" : "success"}
                    >
                      {item.status === "cancelled"
                        ? t("classSession.cancelled")
                        : t("classSession.scheduled")}
                    </StatusBadge>
                  </div>
                  <time dateTime={item.starts_at}>{rangeText.viewer}</time>
                  <small>{t("classSession.viewerTime")}</small>
                  {rangeText.classTime && (
                    <small>
                      {t("classSession.classTime", {
                        timezone: item.timezone,
                      })}
                      : {rangeText.classTime}
                    </small>
                  )}
                  {item.description && <p>{item.description}</p>}
                </div>
                {(editable || cancellable) && item.status === "scheduled" && (
                  <div className="class-session-card__actions">
                    {editable && (
                      <Button
                        leadingIcon={<Pencil />}
                        onClick={() => {
                          setEditing(item);
                          setFormOpen(true);
                        }}
                        size="sm"
                        variant="secondary"
                      >
                        {t("classroom.editTitle")}
                      </Button>
                    )}
                    {cancellable && (
                      <Button
                        leadingIcon={<X />}
                        onClick={() => setCancelTarget(item)}
                        size="sm"
                        variant="danger"
                      >
                        {t("classSession.cancelAction")}
                      </Button>
                    )}
                  </div>
                )}
              </article>
            );
          })}
        </div>
      )}

      {!concealed && sessionsQuery.hasNextPage && (
        <div className="class-session-panel__load-more">
          {sessionsQuery.isFetchNextPageError && (
            <p role="alert">{t("classSession.loadMoreError")}</p>
          )}
          <Button
            disabled={isOffline}
            loading={sessionsQuery.isFetchingNextPage}
            loadingLabel={t("classSession.loadingMore")}
            onClick={() => void sessionsQuery.fetchNextPage()}
            variant="secondary"
          >
            {t("classSession.loadMore")}
          </Button>
        </div>
      )}

      <SessionDialog
        classroom={classroom}
        error={createMutation.error ?? updateMutation.error ?? null}
        initial={editing}
        onOpenChange={(open) => {
          if (!open && !createMutation.isPending && !updateMutation.isPending) {
            setFormOpen(false);
            createMutation.reset();
            updateMutation.reset();
          }
        }}
        onCreate={(input) => {
          createMutation.mutate(
            { classID: classroom.id, input },
            {
              onSuccess: () => setFormOpen(false),
            },
          );
        }}
        onUpdate={(input) => {
          if (!editing) {
            return;
          }
          updateMutation.mutate(
            { classID: classroom.id, sessionID: editing.id, input },
            {
              onSuccess: () => {
                setFormOpen(false);
                setEditing(null);
              },
            },
          );
        }}
        open={formOpen}
        pending={createMutation.isPending || updateMutation.isPending}
      />

      <Dialog
        onOpenChange={(open) => {
          if (!cancelMutation.isPending) {
            setCancelTarget(open ? cancelTarget : null);
            if (!open) {
              cancelMutation.reset();
            }
          }
        }}
        open={Boolean(cancelTarget)}
      >
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{t("classSession.cancelConfirmTitle")}</DialogTitle>
          <DialogDescription>
            {t("classSession.cancelConfirmDescription")}
          </DialogDescription>
          {cancelMutation.error && (
            <p className="form-error" role="alert">
              {t(sessionErrorKey(cancelMutation.error))}
            </p>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={cancelMutation.isPending} variant="secondary">
                {t("classSession.cancel")}
              </Button>
            </DialogClose>
            <Button
              loading={cancelMutation.isPending}
              loadingLabel={t("classSession.cancelling")}
              onClick={() => {
                if (cancelTarget) {
                  cancelMutation.mutate(
                    {
                      classID: classroom.id,
                      sessionID: cancelTarget.id,
                      input: { expected_version: cancelTarget.version },
                    },
                    {
                      onSuccess: () => setCancelTarget(null),
                    },
                  );
                }
              }}
              variant="danger"
            >
              {t("classSession.cancelConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

interface SessionDialogProps {
  classroom: ClassroomClass;
  error: Error | null;
  initial: ClassSession | null;
  onCreate: (input: CreateClassSessionRequest) => void;
  onOpenChange: (open: boolean) => void;
  onUpdate: (input: UpdateClassSessionRequest) => void;
  open: boolean;
  pending: boolean;
}

function SessionDialog({
  classroom,
  error,
  initial,
  onCreate,
  onOpenChange,
  onUpdate,
  open,
  pending,
}: SessionDialogProps) {
  const { t } = useI18n();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [startsAt, setStartsAt] = useState("");
  const [endsAt, setEndsAt] = useState("");
  const [timezone, setTimezone] = useState(classroom.timezone);
  const [overlapChoice, setOverlapChoice] = useState<OverlapChoice | "">("");
  const [formError, setFormError] = useState<TranslationKey | null>(null);
  const isEditing = Boolean(initial);

  const syncInitial = () => {
    setTitle(initial?.title ?? classroom.title);
    setDescription(initial?.description ?? "");
    setTimezone(initial?.timezone ?? classroom.timezone);
    setStartsAt(
      initial ? civilInputFromInstant(initial.starts_at, initial.timezone) : "",
    );
    setEndsAt(
      initial ? civilInputFromInstant(initial.ends_at, initial.timezone) : "",
    );
    setOverlapChoice("");
    setFormError(null);
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const start = resolveCivilDateTime(
      startsAt,
      timezone,
      overlapChoice || undefined,
    );
    const end = resolveCivilDateTime(
      endsAt,
      timezone,
      overlapChoice || undefined,
    );
    if (start.kind === "overlap" || end.kind === "overlap") {
      setFormError("classSession.timeOverlap");
      return;
    }
    if (start.kind === "invalid" || end.kind === "invalid") {
      const isGap =
        (start.kind === "invalid" && start.reason === "gap") ||
        (end.kind === "invalid" && end.reason === "gap");
      setFormError(isGap ? "classSession.timeGap" : "classSession.formError");
      return;
    }
    if (!isOrderedSessionRange(start.value, end.value)) {
      setFormError("classSession.invalidRange");
      return;
    }
    setFormError(null);
    if (isEditing && initial) {
      onUpdate({
        description,
        ends_at: end.value,
        expected_version: initial.version,
        starts_at: start.value,
        timezone,
        title: title.trim(),
      });
      return;
    }
    onCreate({
      description,
      ends_at: end.value,
      starts_at: start.value,
      timezone,
      title: title.trim(),
    });
  };

  return (
    <Dialog
      onOpenChange={(nextOpen) => {
        if (nextOpen) {
          syncInitial();
        }
        onOpenChange(nextOpen);
      }}
      open={open}
    >
      <DialogContent closeLabel={t("classSession.closeDialog")}>
        <DialogTitle>
          {isEditing
            ? t("classSession.editTitle")
            : t("classSession.createTitle")}
        </DialogTitle>
        <DialogDescription>
          {t("classSession.formDescription")}
        </DialogDescription>
        <form className="class-session-form" onSubmit={submit}>
          <TextField
            id="class-session-title"
            label={t("classSession.titleLabel")}
            maxLength={200}
            onChange={(event) => setTitle(event.target.value)}
            required
            value={title}
          />
          <TextAreaField
            id="class-session-description"
            label={t("classSession.descriptionLabel")}
            maxLength={4000}
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
          <div className="class-session-form__grid">
            <TextField
              id="class-session-starts"
              label={t("classSession.startsAtLabel")}
              onChange={(event) => setStartsAt(event.target.value)}
              required
              type="datetime-local"
              value={startsAt}
            />
            <TextField
              id="class-session-ends"
              label={t("classSession.endsAtLabel")}
              onChange={(event) => setEndsAt(event.target.value)}
              required
              type="datetime-local"
              value={endsAt}
            />
          </div>
          <TextField
            id="class-session-timezone"
            label={t("classSession.timezoneLabel")}
            maxLength={100}
            onChange={(event) => setTimezone(event.target.value)}
            required
            value={timezone}
          />
          <label
            className="class-session-form__select"
            htmlFor="class-session-overlap"
          >
            {t("classSession.overlapLabel")}
            <select
              id="class-session-overlap"
              onChange={(event) =>
                setOverlapChoice(event.target.value as OverlapChoice | "")
              }
              value={overlapChoice}
            >
              <option value="">—</option>
              <option value="earlier">
                {t("classSession.overlapEarlier")}
              </option>
              <option value="later">{t("classSession.overlapLater")}</option>
            </select>
          </label>
          {(formError || error) && (
            <p className="form-error" role="alert">
              {formError ? t(formError) : t(sessionErrorKey(error))}
            </p>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={pending} variant="secondary">
                {t("classSession.cancel")}
              </Button>
            </DialogClose>
            <Button
              loading={pending}
              loadingLabel={t("classSession.saving")}
              type="submit"
            >
              {t("classSession.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
