export type CalendarView =
  "dayGridMonth" | "timeGridWeek" | "timeGridDay" | "listWeek";

export interface CalendarItem {
  id: string;
  title: string;
  startsAt: string;
  endsAt: string;
  timeZone: string;
  category: "class" | "study";
  status: "scheduled" | "conflict";
  version: number;
}

export interface CalendarMutation {
  itemId: string;
  startsAt: string;
  endsAt: string;
  timeZone: string;
  expectedVersion: number;
  source: "drag" | "resize" | "keyboard";
}

export interface AcceptedMutation {
  accepted: true;
  item: CalendarItem;
}

export interface RejectedMutation {
  accepted: false;
  code: "conflict" | "stale" | "validation";
  message: string;
}

export type MutationResult = AcceptedMutation | RejectedMutation;
export type CommitMutation = (
  mutation: CalendarMutation,
) => Promise<MutationResult>;

export interface RevertableInteraction {
  revert: () => void;
}

export interface MutationAnnouncement {
  tone: "success" | "error";
  message: string;
}

/**
 * The renderer is optimistic. Domain code owns the mutation result and this
 * helper guarantees that any rejected server result restores the renderer.
 * It is deliberately independent of FullCalendar types.
 */
export async function commitWithRevert(
  interaction: RevertableInteraction,
  mutation: CalendarMutation,
  commit: CommitMutation,
  announce: (announcement: MutationAnnouncement) => void,
): Promise<MutationResult> {
  try {
    const result = await commit(mutation);
    if (!result.accepted) {
      interaction.revert();
      announce({ tone: "error", message: result.message });
      return result;
    }

    announce({
      tone: "success",
      message:
        mutation.source === "keyboard"
          ? "Đã cập nhật thời gian bằng bàn phím."
          : "Đã cập nhật thời gian. Nếu có xung đột, lịch đã được hoàn tác.",
    });
    return result;
  } catch {
    interaction.revert();
    const result: RejectedMutation = {
      accepted: false,
      code: "validation",
      message: "Không thể lưu thay đổi. Lịch đã được hoàn tác.",
    };
    announce({ tone: "error", message: result.message });
    return result;
  }
}
