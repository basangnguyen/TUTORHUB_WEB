import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@tutorhub/ui";
import type { ClassSessionOccurrenceMutationRequest } from "@tutorhub/api-client";
import { useMemo, useState } from "react";
import {
  useCancelClassSessionSeries,
  useClassSessionSeriesMutationPreview,
  useUpdateClassSessionSeries,
} from "../../app/classSessions";
import { useSession } from "../../app/session";
import type { CalendarItemViewModel } from "./model";

type RecurringScope =
  "this_occurrence" | "this_and_following" | "entire_series";
type FuturePolicy = "carry" | "rebase" | "discard";

export interface RecurringScopeRequest {
  item: CalendarItemViewModel;
  operation?: "update" | "cancel";
  startsAt?: string;
  endsAt?: string;
  tenantID: string;
  onCancel: () => void;
  onSuccess: (item: CalendarItemViewModel) => void;
}

export function RecurringScopeDialog({
  request,
}: {
  request: RecurringScopeRequest | null;
}) {
  const [scope, setScope] = useState<RecurringScope>("this_occurrence");
  const [policy, setPolicy] = useState<FuturePolicy>("carry");
  const [overrideConflict, setOverrideConflict] = useState(false);
  const [overrideReason, setOverrideReason] = useState("");
  const session = useSession();
  const updateMutation = useUpdateClassSessionSeries(request?.tenantID);
  const cancelMutation = useCancelClassSessionSeries(request?.tenantID);
  const operation = request?.operation ?? "update";
  const mutation = operation === "cancel" ? cancelMutation : updateMutation;
  const canOverride = session.currentUser?.active_tenant?.role === "org_admin";
  const previewInput =
    useMemo<ClassSessionOccurrenceMutationRequest | null>(() => {
      if (
        !request?.item.seriesID ||
        !request.item.occurrenceKey ||
        (operation === "update" && (!request.startsAt || !request.endsAt))
      ) {
        return null;
      }
      return {
        scope,
        occurrence_key: request.item.occurrenceKey,
        expected_version: request.item.version,
        idempotency_key: `preview:${request.item.seriesID}`,
        ...(scope === "this_and_following"
          ? { following_exception_policy: policy }
          : {}),
        ...(operation === "update"
          ? {
              starts_at: request.startsAt,
              ends_at: request.endsAt,
              timezone: request.item.displayTimezone,
            }
          : {}),
      };
    }, [operation, policy, request, scope]);
  const preview = useClassSessionSeriesMutationPreview(
    request?.tenantID,
    request?.item.classID ?? undefined,
    request?.item.seriesID ?? undefined,
    previewInput,
  );

  if (!request || !request.item.seriesID || !request.item.occurrenceKey) {
    return null;
  }
  const seriesID = request.item.seriesID;

  const close = () => {
    if (!mutation.isPending) {
      mutation.reset();
      request.onCancel();
    }
  };

  const submit = () => {
    if (!previewInput) {
      return;
    }
    mutation.mutate(
      {
        classID: request.item.classID ?? "",
        seriesID,
        input: {
          ...previewInput,
          idempotency_key: crypto.randomUUID(),
          ...(canOverride && overrideConflict
            ? {
                override_schedule_conflict: true,
                schedule_conflict_reason: overrideReason.trim(),
              }
            : {}),
        },
      },
      {
        onSuccess: (result) => {
          request.onSuccess({
            ...request.item,
            ...(request.endsAt ? { endsAt: request.endsAt } : {}),
            ...(request.startsAt ? { startsAt: request.startsAt } : {}),
            sourceID: result.series.id,
            seriesID: result.series.id,
            status: operation === "cancel" ? "cancelled" : request.item.status,
            version: result.series.version,
          });
        },
      },
    );
  };

  return (
    <Dialog onOpenChange={(open) => !open && close()} open>
      <DialogContent closeLabel="Đóng chọn phạm vi">
        <DialogTitle>
          {operation === "cancel"
            ? "Hủy lịch học lặp"
            : "Áp dụng thay đổi cho lịch lặp"}
        </DialogTitle>
        <DialogDescription>
          Chọn phạm vi và xem trước số buổi, ngoại lệ bị ảnh hưởng trước khi xác
          nhận.
        </DialogDescription>
        <fieldset className="calendar-recurring-scope">
          <legend>Phạm vi thay đổi</legend>
          <label>
            <input
              checked={scope === "this_occurrence"}
              onChange={() => setScope("this_occurrence")}
              type="radio"
            />
            Chỉ buổi này
          </label>
          <label>
            <input
              checked={scope === "this_and_following"}
              onChange={() => setScope("this_and_following")}
              type="radio"
            />
            Buổi này và các buổi sau
          </label>
          <label>
            <input
              checked={scope === "entire_series"}
              onChange={() => setScope("entire_series")}
              type="radio"
            />
            Cả chuỗi
          </label>
        </fieldset>
        {scope === "this_and_following" && (
          <fieldset className="calendar-recurring-scope">
            <legend>Ngoại lệ các buổi sau</legend>
            <label>
              <input
                checked={policy === "carry"}
                onChange={() => setPolicy("carry")}
                type="radio"
              />
              Giữ nguyên theo ngày cũ
            </label>
            <label>
              <input
                checked={policy === "rebase"}
                onChange={() => setPolicy("rebase")}
                type="radio"
              />
              Tịnh tiến theo chuỗi mới
            </label>
            <label>
              <input
                checked={policy === "discard"}
                onChange={() => setPolicy("discard")}
                type="radio"
              />
              Loại bỏ ngoại lệ
            </label>
          </fieldset>
        )}
        <section
          aria-busy={preview.isFetching}
          aria-live="polite"
          className="calendar-recurring-preview"
        >
          <h3>Xem trước ảnh hưởng</h3>
          {preview.isFetching && <p>Đang tính các buổi bị ảnh hưởng…</p>}
          {preview.isError && (
            <p className="calendar-recurring-scope__error" role="alert">
              Không thể tính trước phạm vi. Hãy tải lại dữ liệu chuỗi rồi thử
              lại.
            </p>
          )}
          {preview.data && (
            <>
              <dl>
                <div>
                  <dt>Buổi bị ảnh hưởng</dt>
                  <dd>{preview.data.affected_occurrence_count}</dd>
                </div>
                <div>
                  <dt>Ngoại lệ tương lai</dt>
                  <dd>{preview.data.future_exception_count}</dd>
                </div>
                <div>
                  <dt>Ngoại lệ được giữ</dt>
                  <dd>{preview.data.retained_exception_count}</dd>
                </div>
                <div>
                  <dt>Ngoại lệ bị loại</dt>
                  <dd>{preview.data.discarded_exception_count}</dd>
                </div>
              </dl>
              {preview.data.conflicts.length > 0 && (
                <p className="calendar-recurring-scope__error" role="alert">
                  Có {preview.data.conflicts.length} xung đột lịch cứng trong
                  phạm vi đã chọn.
                </p>
              )}
            </>
          )}
        </section>
        {canOverride && (preview.data?.conflicts.length ?? 0) > 0 && (
          <fieldset className="calendar-recurring-scope">
            <legend>Quản trị tổ chức</legend>
            <label>
              <input
                checked={overrideConflict}
                onChange={(event) => setOverrideConflict(event.target.checked)}
                type="checkbox"
              />
              Ghi đè xung đột lịch cứng
            </label>
            {overrideConflict && (
              <label className="calendar-recurring-override-reason">
                Lý do ghi đè
                <textarea
                  maxLength={500}
                  minLength={3}
                  onChange={(event) => setOverrideReason(event.target.value)}
                  required
                  rows={3}
                  value={overrideReason}
                />
              </label>
            )}
          </fieldset>
        )}
        {mutation.error && (
          <p className="calendar-recurring-scope__error" role="alert">
            Không thể lưu thay đổi. Có thể chuỗi đã được sửa ở nơi khác hoặc bị
            xung đột lịch.
          </p>
        )}
        <div className="calendar-recurring-scope__actions">
          <Button onClick={close} variant="secondary">
            Hủy
          </Button>
          <Button
            disabled={
              preview.isFetching ||
              preview.isError ||
              !preview.data ||
              ((preview.data?.conflicts.length ?? 0) > 0 &&
                (!canOverride ||
                  !overrideConflict ||
                  overrideReason.trim().length < 3))
            }
            loading={mutation.isPending}
            onClick={submit}
            variant={operation === "cancel" ? "danger" : "primary"}
          >
            {operation === "cancel" ? "Xác nhận hủy" : "Lưu thay đổi"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
