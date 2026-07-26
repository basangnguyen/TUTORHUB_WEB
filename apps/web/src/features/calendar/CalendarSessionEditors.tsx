import {
  type ClassroomClass,
  type CreateClassSessionRequest,
  type UpdateClassSessionRequest,
} from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  EmptyState,
  ErrorState,
  Skeleton,
  SkeletonGroup,
} from "@tutorhub/ui";
import { RotateCw } from "lucide-react";
import { useMemo, useState } from "react";
import { useClassList } from "../../app/classes";
import {
  useClassSessionDetail,
  useCreateClassSession,
  useUpdateClassSession,
} from "../../app/classSessions";
import { useI18n } from "../../app/i18n";
import { ClassSessionEditorDialog } from "../../components/ClassSessionPanel";
import type { CalendarItemViewModel } from "./model";

interface CalendarQuickCreateProps {
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
  open: boolean;
  tenantID: string | undefined;
}

export function CalendarQuickCreate({
  onOpenChange,
  onSaved,
  open,
  tenantID,
}: CalendarQuickCreateProps) {
  const { t } = useI18n();
  const classesQuery = useClassList(tenantID, "active", open);
  const createMutation = useCreateClassSession(tenantID);
  const [classID, setClassID] = useState("");
  const classes = useMemo(() => {
    const result = new Map<string, ClassroomClass>();
    for (const classroom of (classesQuery.data?.pages ?? []).flatMap(
      (page) => page.items,
    )) {
      if (classroom.viewer_access.can_schedule_sessions) {
        result.set(classroom.id, classroom);
      }
    }
    return [...result.values()];
  }, [classesQuery.data?.pages]);

  const selectedClass =
    classes.find((classroom) => classroom.id === classID) ?? classes[0];
  const close = () => {
    if (!createMutation.isPending) {
      createMutation.reset();
      onOpenChange(false);
    }
  };

  if (!open) {
    return null;
  }

  if (classesQuery.isPending || (!selectedClass && classesQuery.isFetching)) {
    return (
      <Dialog onOpenChange={(nextOpen) => !nextOpen && close()} open>
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{t("classSession.createTitle")}</DialogTitle>
          <DialogDescription>{t("calendar.loadingClasses")}</DialogDescription>
          <SkeletonGroup label={t("calendar.loadingClasses")}>
            <Skeleton height="3rem" />
            <Skeleton height="12rem" />
          </SkeletonGroup>
        </DialogContent>
      </Dialog>
    );
  }

  if (classesQuery.isError) {
    return (
      <Dialog onOpenChange={(nextOpen) => !nextOpen && close()} open>
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{t("classSession.createTitle")}</DialogTitle>
          <ErrorState
            actions={
              <Button
                leadingIcon={<RotateCw />}
                onClick={() => void classesQuery.refetch()}
                variant="secondary"
              >
                {t("calendar.retry")}
              </Button>
            }
            description={t("calendar.classesErrorDescription")}
            title={t("calendar.classesErrorTitle")}
          />
        </DialogContent>
      </Dialog>
    );
  }

  if (!selectedClass) {
    return (
      <Dialog onOpenChange={(nextOpen) => !nextOpen && close()} open>
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{t("classSession.createTitle")}</DialogTitle>
          <EmptyState
            description={t("calendar.noSchedulableClassDescription")}
            title={t("calendar.noSchedulableClassTitle")}
          />
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <ClassSessionEditorDialog
      classTitle={selectedClass.title}
      classTimezone={selectedClass.timezone}
      context={
        <div className="calendar-session-context">
          <label htmlFor="calendar-session-class">
            {t("calendar.classLabel")}
            <select
              id="calendar-session-class"
              onChange={(event) => setClassID(event.target.value)}
              value={selectedClass.id}
            >
              {classes.map((classroom) => (
                <option key={classroom.id} value={classroom.id}>
                  {classroom.title}
                </option>
              ))}
            </select>
          </label>
          {classesQuery.hasNextPage && (
            <Button
              loading={classesQuery.isFetchingNextPage}
              loadingLabel={t("calendar.loadingClasses")}
              onClick={() => void classesQuery.fetchNextPage()}
              size="sm"
              type="button"
              variant="quiet"
            >
              {t("calendar.loadMoreClasses")}
            </Button>
          )}
        </div>
      }
      error={createMutation.error}
      initial={null}
      key={selectedClass.id}
      onCreate={(input: CreateClassSessionRequest) => {
        createMutation.mutate(
          { classID: selectedClass.id, input },
          {
            onSuccess: () => {
              onOpenChange(false);
              onSaved();
            },
          },
        );
      }}
      onOpenChange={(nextOpen) => !nextOpen && close()}
      onUpdate={() => undefined}
      open
      pending={createMutation.isPending}
    />
  );
}

interface CalendarSessionEditProps {
  item: CalendarItemViewModel | null;
  onClose: () => void;
  onSaved: () => void;
  tenantID: string | undefined;
}

export function CalendarSessionEdit({
  item,
  onClose,
  onSaved,
  tenantID,
}: CalendarSessionEditProps) {
  const { t } = useI18n();
  const detail = useClassSessionDetail(
    tenantID,
    item?.classID ?? undefined,
    item?.sourceID,
  );
  const updateMutation = useUpdateClassSession(tenantID);

  if (!item || !item.classID) {
    return null;
  }

  const close = () => {
    if (!updateMutation.isPending) {
      updateMutation.reset();
      onClose();
    }
  };

  if (detail.isPending) {
    return (
      <Dialog onOpenChange={(open) => !open && close()} open>
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{t("classSession.editTitle")}</DialogTitle>
          <SkeletonGroup label={t("calendar.loadingSession")}>
            <Skeleton height="3rem" />
            <Skeleton height="12rem" />
          </SkeletonGroup>
        </DialogContent>
      </Dialog>
    );
  }

  if (detail.isError || !detail.data) {
    return (
      <Dialog onOpenChange={(open) => !open && close()} open>
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{t("classSession.editTitle")}</DialogTitle>
          <ErrorState
            actions={
              <Button
                leadingIcon={<RotateCw />}
                onClick={() => void detail.refetch()}
                variant="secondary"
              >
                {t("calendar.retry")}
              </Button>
            }
            description={t("calendar.sessionErrorDescription")}
            title={t("calendar.sessionErrorTitle")}
          />
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <ClassSessionEditorDialog
      classTitle={item.classTitle ?? item.title}
      classTimezone={detail.data.timezone}
      error={updateMutation.error}
      initial={detail.data}
      key={`${detail.data.id}:${detail.data.version}`}
      onCreate={() => undefined}
      onOpenChange={(open) => !open && close()}
      onUpdate={(input: UpdateClassSessionRequest) => {
        updateMutation.mutate(
          {
            classID: item.classID ?? "",
            input,
            sessionID: item.sourceID,
          },
          {
            onSuccess: () => {
              onClose();
              onSaved();
            },
          },
        );
      }}
      open
      pending={updateMutation.isPending}
    />
  );
}
