import { APIRequestError } from "@tutorhub/api-client";
import FullCalendar, {
  type CalendarOptions,
  type CalendarRef,
  type EventDropInfo,
  type EventResizeDoneInfo,
} from "@fullcalendar/react";
import dayGridPlugin from "@fullcalendar/react/daygrid";
import interactionPlugin from "@fullcalendar/react/interaction";
import listPlugin from "@fullcalendar/react/list";
import timeGridPlugin from "@fullcalendar/react/timegrid";
import classicThemePlugin from "@fullcalendar/react/themes/classic";
import viLocale from "@fullcalendar/react/locales/vi";
import "@fullcalendar/react/skeleton.css";
import "@fullcalendar/react/themes/classic/theme.css";
import "@fullcalendar/react/themes/classic/palette.css";
import { Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@tutorhub/ui";
import { useI18n } from "../../app/i18n";
import type {
  CalendarDisplayPreferenceViewModel,
  CalendarItemViewModel,
  CalendarView,
} from "./model";

const plugins = [
  dayGridPlugin,
  timeGridPlugin,
  listPlugin,
  interactionPlugin,
  classicThemePlugin,
];

const fullCalendarViews: Record<CalendarView, string> = {
  agenda: "listWeek",
  day: "timeGridDay",
  month: "dayGridMonth",
  week: "timeGridWeek",
  work_week: "timeGridWeek",
};

export interface CalendarRescheduleInput {
  expectedVersion: number;
  item: CalendarItemViewModel;
  endsAt: string;
  startsAt: string;
}

export type CalendarReschedule = (
  input: CalendarRescheduleInput,
) => Promise<CalendarItemViewModel>;

interface FullCalendarProjectionProps {
  date: string;
  items: readonly CalendarItemViewModel[];
  locale: string;
  onEditItem?: (item: CalendarItemViewModel) => void;
  onItemChanged: (item: CalendarItemViewModel) => void;
  onReschedule: CalendarReschedule;
  preference: CalendarDisplayPreferenceViewModel;
  view: Exclude<CalendarView, "agenda">;
}

interface UndoAction {
  item: CalendarItemViewModel;
  previousEndsAt: string;
  previousStartsAt: string;
  updatedItem: CalendarItemViewModel;
}

function calendarEventLabel(
  item: CalendarItemViewModel,
  locale: string,
  hourCycle: "h12" | "h23",
) {
  const formatter = new Intl.DateTimeFormat(locale, {
    hour: "numeric",
    hour12: hourCycle === "h12",
    minute: "2-digit",
    timeZone: item.displayTimezone,
  });
  const time = `${formatter.format(new Date(item.startsAt))} – ${formatter.format(
    new Date(item.endsAt),
  )}`;
  return `${item.title}. ${time}. ${item.classTitle ?? "Lớp học"}.`;
}

function messageForError(
  error: unknown,
  conflictMessage: string,
  fallback: string,
) {
  if (error instanceof APIRequestError && error.status === 409) {
    return conflictMessage;
  }
  return fallback;
}

function eventMutation(
  info: EventDropInfo | EventResizeDoneInfo,
  itemsByID: ReadonlyMap<string, CalendarItemViewModel>,
) {
  const item = itemsByID.get(info.event.id);
  const startsAt = info.event.start?.toISOString();
  const endsAt = info.event.end?.toISOString();
  if (!item || !startsAt || !endsAt || !item.canReschedule) {
    return null;
  }
  return { endsAt, item, startsAt };
}

export function FullCalendarProjection({
  date,
  items,
  locale,
  onEditItem,
  onItemChanged,
  onReschedule,
  preference,
  view,
}: FullCalendarProjectionProps) {
  const { t } = useI18n();
  const calendarRef = useRef<CalendarRef>(null);
  const [undoAction, setUndoAction] = useState<UndoAction | null>(null);
  const itemsByID = useMemo(
    () => new Map(items.map((item) => [item.id, item])),
    [items],
  );

  useEffect(() => {
    const api = calendarRef.current?.getApi();
    if (!api) {
      return;
    }
    api.changeView(fullCalendarViews[view], date);
  }, [date, view]);

  const announce = useCallback((message: string, tone: "error" | "success") => {
    const node = document.querySelector<HTMLElement>(
      "[data-calendar-announcement]",
    );
    if (node) {
      node.dataset.tone = tone;
      node.textContent = message;
    }
  }, []);

  const saveInteraction = useCallback(
    async (info: EventDropInfo | EventResizeDoneInfo) => {
      const mutation = eventMutation(info, itemsByID);
      if (!mutation) {
        info.revert();
        announce("Sự kiện này không thể thay đổi thời gian.", "error");
        return;
      }
      try {
        const updatedItem = await onReschedule({
          endsAt: mutation.endsAt,
          expectedVersion: mutation.item.version,
          item: mutation.item,
          startsAt: mutation.startsAt,
        });
        onItemChanged(updatedItem);
        setUndoAction({
          item: mutation.item,
          previousEndsAt: mutation.item.endsAt,
          previousStartsAt: mutation.item.startsAt,
          updatedItem,
        });
        announce("Đã cập nhật thời gian buổi học.", "success");
      } catch (error) {
        info.revert();
        announce(
          messageForError(
            error,
            "Phát hiện xung đột phiên bản (409). Lịch đã được hoàn tác.",
            "Không thể lưu thay đổi. Lịch đã được hoàn tác.",
          ),
          "error",
        );
      }
    },
    [announce, itemsByID, onItemChanged, onReschedule],
  );

  const undo = useCallback(async () => {
    if (!undoAction) {
      return;
    }
    const event = calendarRef.current
      ?.getApi()
      .getEventById(undoAction.item.id);
    const currentStartsAt = event?.start?.toISOString();
    const currentEndsAt = event?.end?.toISOString();
    try {
      const restoredItem = await onReschedule({
        endsAt: undoAction.previousEndsAt,
        expectedVersion: undoAction.updatedItem.version,
        item: undoAction.updatedItem,
        startsAt: undoAction.previousStartsAt,
      });
      onItemChanged(restoredItem);
      setUndoAction(null);
      announce("Đã hoàn tác thay đổi thời gian.", "success");
    } catch (error) {
      if (event && currentStartsAt && currentEndsAt) {
        event.setDates(new Date(currentStartsAt), new Date(currentEndsAt));
      }
      announce(
        messageForError(
          error,
          "Không thể hoàn tác vì lịch đã được thay đổi ở nơi khác.",
          "Không thể hoàn tác thay đổi thời gian.",
        ),
        "error",
      );
    }
  }, [announce, onItemChanged, onReschedule, undoAction]);

  const events = useMemo(
    () =>
      items.map((item) => ({
        allDay: item.allDay,
        classNames: [
          "calendar-event",
          `calendar-event--${item.status}`,
          item.canReschedule ? "" : "calendar-event--readonly",
        ].filter(Boolean),
        editable: item.canReschedule && item.status === "scheduled",
        end: item.endsAt,
        id: item.id,
        start: item.startsAt,
        title: item.title,
      })),
    [items],
  );

  const options: CalendarOptions = {
    allDaySlot: false,
    contentHeight: "auto",
    dayMaxEventRows: 6,
    editable: true,
    eventClick: (info) => {
      info.jsEvent.preventDefault();
      const item = itemsByID.get(info.event.id);
      if (item?.canEdit) {
        onEditItem?.(item);
      }
    },
    eventDidMount: (info) => {
      const item = itemsByID.get(info.event.id);
      if (!item) {
        return;
      }
      info.el.dataset.calendarEventId = item.id;
      info.el.dataset.calendarEventCategory = "class_session";
      info.el.setAttribute(
        "aria-label",
        calendarEventLabel(item, locale, preference.hourCycle),
      );
      info.el.title = calendarEventLabel(item, locale, preference.hourCycle);
    },
    eventMaxStack: 6,
    eventOrder: "start,title",
    eventResize: (info) => void saveInteraction(info),
    eventResizableFromStart: true,
    eventStartEditable: true,
    eventDurationEditable: true,
    events,
    expandRows: true,
    headerToolbar: false,
    height: "auto",
    locale: locale === "vi-VN" ? "vi" : "en",
    locales: [viLocale],
    navLinks: true,
    nowIndicator: true,
    plugins,
    scrollTime: "08:00:00",
    slotMinTime: "06:00:00",
    slotMaxTime: "23:00:00",
    timeZone: preference.viewerTimezone,
    weekends: view !== "work_week",
    eventDrop: (info) => void saveInteraction(info),
    initialDate: date,
    initialView: fullCalendarViews[view],
    datesSet: (info) => {
      document.body.dataset.calendarRenderedView = info.view.type;
    },
    eventsSet: (visibleEvents) => {
      document.body.dataset.calendarVisibleEvents = String(
        visibleEvents.length,
      );
    },
  };

  return (
    <section
      aria-label="Lịch tương tác"
      className="calendar-fullcalendar"
      data-calendar-ready="ready"
      data-calendar-renderer="fullcalendar-standard"
    >
      <FullCalendar ref={calendarRef} {...options} />
      <div
        aria-live="polite"
        className="calendar-surface__announcement"
        data-calendar-announcement="true"
        role="status"
      />
      {undoAction && (
        <div className="calendar-undo" role="status">
          <span>{t("calendar.undoAvailable")}</span>
          <Button leadingIcon={<Undo2 />} onClick={() => void undo()} size="sm">
            {t("calendar.undo")}
          </Button>
        </div>
      )}
    </section>
  );
}
