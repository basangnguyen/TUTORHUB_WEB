import type { CalendarItem, CalendarMutation, CommitMutation } from "./domain";

export interface SurfaceEvent {
  id: string;
  title: string;
  start: string;
  end: string;
  extendedProps: {
    timeZone: string;
    category: CalendarItem["category"];
    status: CalendarItem["status"];
    version: number;
  };
}

export function toSurfaceEvents(
  items: readonly CalendarItem[],
): SurfaceEvent[] {
  return items.map((item) => ({
    id: item.id,
    title: item.title,
    start: item.startsAt,
    end: item.endsAt,
    extendedProps: {
      timeZone: item.timeZone,
      category: item.category,
      status: item.status,
      version: item.version,
    },
  }));
}

export function mutationFromSurfaceEvent(
  event: {
    id: string;
    start: Date | null;
    end: Date | null;
    extendedProps: Record<string, unknown>;
  },
  source: CalendarMutation["source"],
): CalendarMutation | null {
  if (!event.start || !event.end) {
    return null;
  }
  const timeZone =
    typeof event.extendedProps.timeZone === "string"
      ? event.extendedProps.timeZone
      : "UTC";
  const version =
    typeof event.extendedProps.version === "number"
      ? event.extendedProps.version
      : 1;
  return {
    itemId: event.id,
    startsAt: event.start.toISOString(),
    endsAt: event.end.toISOString(),
    timeZone,
    expectedVersion: version,
    source,
  };
}

export function createDemoCommitMutation(
  items: readonly CalendarItem[],
  update: (item: CalendarItem) => void,
): CommitMutation {
  return async (mutation) => {
    await new Promise<void>((resolve) => {
      window.setTimeout(resolve, 12);
    });
    const item = items.find((candidate) => candidate.id === mutation.itemId);
    if (!item) {
      return {
        accepted: false,
        code: "validation",
        message: "Không tìm thấy sự kiện trong phạm vi hiện tại.",
      };
    }
    if (item.status === "conflict" || mutation.itemId === "hcm-conflict") {
      return {
        accepted: false,
        code: "conflict",
        message: "Phát hiện xung đột phiên bản (409). Lịch đã được hoàn tác.",
      };
    }
    if (item.version !== mutation.expectedVersion) {
      return {
        accepted: false,
        code: "stale",
        message: "Lịch đã thay đổi ở nơi khác. Lịch đã được hoàn tác.",
      };
    }
    const updated: CalendarItem = {
      ...item,
      startsAt: mutation.startsAt,
      endsAt: mutation.endsAt,
      version: item.version + 1,
    };
    update(updated);
    return { accepted: true, item: updated };
  };
}
