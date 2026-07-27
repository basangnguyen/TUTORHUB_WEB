import {
  type ClassroomClass,
  type CreateClassSessionRequest,
  type CreateClassSessionSeriesRequest,
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
  useClassSessionSeriesDetail,
  useCreateClassSession,
  useCreateClassSessionSeries,
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
  const createSeriesMutation = useCreateClassSessionSeries(tenantID);
  const [recurring, setRecurring] = useState(false);
  const [frequency, setFrequency] =
    useState<CreateClassSessionSeriesRequest["rule"]["frequency"]>("weekly");
  const [interval, setInterval] = useState(1);
  const [count, setCount] = useState(8);
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
    if (!createMutation.isPending && !createSeriesMutation.isPending) {
      createMutation.reset();
      createSeriesMutation.reset();
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
          <label className="calendar-recurring-create">
            <input
              checked={recurring}
              onChange={(event) => setRecurring(event.target.checked)}
              type="checkbox"
            />
            Lặp lại theo chuỗi
          </label>
          {recurring && (
            <div className="calendar-recurring-create__fields">
              <label>
                Tần suất
                <select
                  onChange={(event) =>
                    setFrequency(
                      event.target
                        .value as CreateClassSessionSeriesRequest["rule"]["frequency"],
                    )
                  }
                  value={frequency}
                >
                  <option value="weekly">Hàng tuần</option>
                  <option value="daily">Hàng ngày</option>
                  <option value="monthly">Hàng tháng</option>
                  <option value="yearly">Hàng năm</option>
                </select>
              </label>
              <label>
                Mỗi
                <input
                  min={1}
                  max={366}
                  onChange={(event) =>
                    setInterval(Math.max(1, Number(event.target.value) || 1))
                  }
                  type="number"
                  value={interval}
                />
              </label>
              <label>
                Số buổi
                <input
                  min={1}
                  max={512}
                  onChange={(event) =>
                    setCount(Math.max(1, Number(event.target.value) || 1))
                  }
                  type="number"
                  value={count}
                />
              </label>
            </div>
          )}
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
      error={recurring ? createSeriesMutation.error : createMutation.error}
      initial={null}
      key={selectedClass.id}
      onCreate={(input: CreateClassSessionRequest) => {
        if (recurring) {
          const seriesInput: CreateClassSessionSeriesRequest = {
            title: input.title,
            description: input.description,
            starts_at: input.starts_at,
            ends_at: input.ends_at,
            timezone: input.timezone,
            overlap_policy: "reject",
            rule: {
              frequency,
              interval,
              ends: { type: "after_count", count },
            },
          };
          createSeriesMutation.mutate(
            { classID: selectedClass.id, input: seriesInput },
            {
              onSuccess: () => {
                onOpenChange(false);
                onSaved();
              },
            },
          );
          return;
        }
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
      pending={createMutation.isPending || createSeriesMutation.isPending}
    />
  );
}

interface CalendarSessionEditProps {
  item: CalendarItemViewModel | null;
  onCancelRecurring: (item: CalendarItemViewModel) => void;
  onClose: () => void;
  onSaved: () => void;
  tenantID: string | undefined;
}

export function CalendarSessionEdit({
  item,
  onCancelRecurring,
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
  const seriesDetail = useClassSessionSeriesDetail(
    tenantID,
    item?.classID ?? undefined,
    item?.seriesID ?? undefined,
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

  if (item.seriesID) {
    if (seriesDetail.isPending) {
      return (
        <Dialog onOpenChange={(open) => !open && close()} open>
          <DialogContent closeLabel={t("classSession.closeDialog")}>
            <DialogTitle>Lịch học lặp</DialogTitle>
            <DialogDescription>
              {t("calendar.loadingSession")}
            </DialogDescription>
            <SkeletonGroup label={t("calendar.loadingSession")}>
              <Skeleton height="3rem" />
              <Skeleton height="8rem" />
            </SkeletonGroup>
          </DialogContent>
        </Dialog>
      );
    }
    if (seriesDetail.isError || !seriesDetail.data) {
      return (
        <Dialog onOpenChange={(open) => !open && close()} open>
          <DialogContent closeLabel={t("classSession.closeDialog")}>
            <DialogTitle>Lịch học lặp</DialogTitle>
            <ErrorState
              actions={
                <Button
                  leadingIcon={<RotateCw />}
                  onClick={() => void seriesDetail.refetch()}
                  variant="secondary"
                >
                  {t("calendar.retry")}
                </Button>
              }
              description="Không thể tải chuỗi lịch học."
              title="Không tải được chuỗi lịch"
            />
          </DialogContent>
        </Dialog>
      );
    }
    const series = seriesDetail.data;
    return (
      <Dialog onOpenChange={(open) => !open && close()} open>
        <DialogContent closeLabel={t("classSession.closeDialog")}>
          <DialogTitle>{series.title}</DialogTitle>
          <DialogDescription>
            Chuỗi {series.rule.frequency}, mỗi {series.rule.interval}. Đang hiển
            thị buổi đã chọn; kéo/thả để chọn phạm vi thay đổi.
          </DialogDescription>
          <dl className="calendar-series-summary">
            <div>
              <dt>Múi giờ</dt>
              <dd>{series.timezone}</dd>
            </div>
            <div>
              <dt>Số buổi</dt>
              <dd>
                {series.rule.ends.type === "after_count"
                  ? series.rule.ends.count
                  : `đến ${series.rule.ends.date}`}
              </dd>
            </div>
            <div>
              <dt>Trạng thái</dt>
              <dd>{series.status}</dd>
            </div>
          </dl>
          <div className="calendar-recurring-scope__actions">
            <Button onClick={close} variant="secondary">
              Đóng
            </Button>
            {series.viewer_access.can_cancel && (
              <Button onClick={() => onCancelRecurring(item)} variant="danger">
                Hủy lịch lặp…
              </Button>
            )}
          </div>
        </DialogContent>
      </Dialog>
    );
  }

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
