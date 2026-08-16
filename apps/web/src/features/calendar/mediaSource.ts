import type { MediaSpaceSource } from "@tutorhub/api-client";
import type { CalendarItemViewModel } from "./model";

export function mediaSourceForCalendarItem(
  item: CalendarItemViewModel,
): MediaSpaceSource | null {
  if (item.sourceType !== "class_session") {
    return null;
  }
  if (item.seriesID && item.occurrenceKey) {
    return {
      kind: "class_session_occurrence",
      occurrence_key: item.occurrenceKey,
      series_id: item.seriesID,
    };
  }
  if (!item.seriesID && !item.occurrenceKey) {
    return { kind: "class_session", class_session_id: item.sourceID };
  }
  return null;
}
