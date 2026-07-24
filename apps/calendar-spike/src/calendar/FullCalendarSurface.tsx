import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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

import { mutationFromSurfaceEvent, toSurfaceEvents } from "./CalendarSurface";
import { addMinutesToInstant } from "./civilTime";
import type {
  CalendarItem,
  CalendarMutation,
  CalendarView,
  CommitMutation,
  MutationAnnouncement,
} from "./domain";
import { commitWithRevert } from "./domain";

const PLUGINS = [
  dayGridPlugin,
  timeGridPlugin,
  listPlugin,
  interactionPlugin,
  classicThemePlugin,
];

const AGENDA_PAGE_SIZE = 24;

const VIEW_LABELS: ReadonlyArray<{
  view: CalendarView;
  label: string;
}> = [
  { view: "timeGridDay", label: "Ngày" },
  { view: "timeGridWeek", label: "Tuần" },
  { view: "dayGridMonth", label: "Tháng" },
  { view: "listWeek", label: "Chương trình" },
];

type FullCalendarEvent = EventDropInfo["event"];
export interface FullCalendarSurfaceProps {
  items: readonly CalendarItem[];
  view: CalendarView;
  commitMutation: CommitMutation;
  onViewChange: (view: CalendarView) => void;
  onAnnouncement: (announcement: MutationAnnouncement) => void;
  onReady: (visibleEventCount: number) => void;
  onSelectedEvent: (itemId: string) => void;
}

function mutationForEvent(
  event: FullCalendarEvent,
  source: CalendarMutation["source"],
): CalendarMutation | null {
  return mutationFromSurfaceEvent(
    {
      id: event.id,
      start: event.start,
      end: event.end,
      extendedProps: event.extendedProps,
    },
    source,
  );
}

function eventLabel(item: CalendarItem): string {
  return `${item.title}. ${item.category === "class" ? "Lớp học" : "Tự học"}. Múi giờ ${item.timeZone}.`;
}

export function FullCalendarSurface({
  items,
  view,
  commitMutation,
  onViewChange,
  onAnnouncement,
  onReady,
  onSelectedEvent,
}: FullCalendarSurfaceProps) {
  const calendarRef = useRef<CalendarRef>(null);
  const [visibleAgendaCount, setVisibleAgendaCount] = useState(() =>
    Math.min(AGENDA_PAGE_SIZE, items.length),
  );
  const surfaceEvents = useMemo(() => toSurfaceEvents(items), [items]);
  const itemsById = useMemo(
    () => new Map(items.map((item) => [item.id, item])),
    [items],
  );
  const visibleAgendaItems = items.slice(0, visibleAgendaCount);
  const remainingAgendaCount = Math.max(
    0,
    items.length - visibleAgendaItems.length,
  );

  useEffect(() => {
    setVisibleAgendaCount((current) =>
      Math.min(
        items.length,
        Math.max(current, Math.min(AGENDA_PAGE_SIZE, items.length)),
      ),
    );
  }, [items.length]);

  const changeView = useCallback(
    (nextView: CalendarView) => {
      calendarRef.current?.getApi().changeView(nextView);
      onViewChange(nextView);
    },
    [onViewChange],
  );

  const handleDrop = useCallback(
    (info: EventDropInfo) => {
      const mutation = mutationForEvent(info.event, "drag");
      if (!mutation) {
        info.revert();
        onAnnouncement({
          tone: "error",
          message: "Sự kiện không có thời điểm kết thúc hợp lệ.",
        });
        return;
      }
      void commitWithRevert(info, mutation, commitMutation, onAnnouncement);
    },
    [commitMutation, onAnnouncement],
  );

  const handleResize = useCallback(
    (info: EventResizeDoneInfo) => {
      const mutation = mutationForEvent(info.event, "resize");
      if (!mutation) {
        info.revert();
        onAnnouncement({
          tone: "error",
          message: "Không thể xác định thời lượng sự kiện.",
        });
        return;
      }
      void commitWithRevert(info, mutation, commitMutation, onAnnouncement);
    },
    [commitMutation, onAnnouncement],
  );

  const options: CalendarOptions = {
    plugins: PLUGINS,
    locales: [viLocale],
    locale: "vi",
    timeZone: "Asia/Ho_Chi_Minh",
    initialDate: "2026-07-23",
    initialView: view,
    headerToolbar: false,
    allDaySlot: false,
    height: "auto",
    contentHeight: 560,
    expandRows: true,
    nowIndicator: true,
    navLinks: true,
    editable: true,
    eventStartEditable: true,
    eventDurationEditable: true,
    eventInteractive: true,
    eventResizableFromStart: true,
    dragRevertDuration: 140,
    eventDisplay: "block",
    eventOrder: "start,title",
    dayMaxEventRows: 6,
    eventMaxStack: 6,
    moreLinkClick: "popover",
    events: surfaceEvents,
    datesSet: (info) => {
      document.body.dataset.calendarRenderedView = info.view.type;
    },
    eventDrop: handleDrop,
    eventResize: handleResize,
    eventClick: (info) => {
      info.jsEvent.preventDefault();
      onSelectedEvent(info.event.id);
    },
    eventDidMount: (info) => {
      const item = itemsById.get(info.event.id);
      info.el.dataset.calendarEventId = info.event.id;
      info.el.setAttribute(
        "aria-label",
        item ? eventLabel(item) : info.event.title,
      );
      if (item) {
        info.el.dataset.calendarEventCategory = item.category;
      }
    },
    eventClass: (info) => {
      const item = itemsById.get(info.event.id);
      return (
        item
          ? [
              "calendar-spike-event",
              `calendar-spike-event--${item.category}`,
              item.status === "conflict"
                ? "calendar-spike-event--conflict"
                : "",
            ]
          : ["calendar-spike-event"]
      )
        .filter(Boolean)
        .join(" ");
    },
    eventsSet: (events) => {
      window.requestAnimationFrame(() => onReady(events.length));
    },
  };

  return (
    <section
      aria-label="Lịch tương tác FullCalendar"
      className="calendar-spike__renderer"
      data-calendar-renderer="fullcalendar-standard"
    >
      <div
        className="calendar-spike__view-switcher"
        role="group"
        aria-label="Chế độ xem lịch"
      >
        {VIEW_LABELS.map((option) => (
          <button
            aria-pressed={view === option.view}
            className="calendar-spike__view-button"
            key={option.view}
            onClick={() => changeView(option.view)}
            type="button"
          >
            {option.label}
          </button>
        ))}
      </div>
      <div className="calendar-spike__calendar" data-calendar-ready="pending">
        <FullCalendar ref={calendarRef} {...options} />
      </div>
      <div className="calendar-spike__agenda-alternative">
        <div className="calendar-spike__agenda-heading">
          <h2 id="calendar-spike-agenda-heading">
            Chương trình thay thế cho thao tác kéo
          </h2>
          <span aria-live="polite" data-testid="agenda-count">
            Hiển thị {visibleAgendaItems.length}/{items.length} mục
          </span>
        </div>
        <p className="calendar-spike__helper">
          Dùng nút bên dưới để dời lịch mà không cần chuột. Đây là đường tương
          đương cho bàn phím và thiết bị hỗ trợ.
        </p>
        <ol
          aria-labelledby="calendar-spike-agenda-heading"
          id="calendar-spike-agenda-list"
        >
          {visibleAgendaItems.map((item) => (
            <AgendaItem
              item={item}
              key={item.id}
              onAnnouncement={onAnnouncement}
              onSelectedEvent={onSelectedEvent}
              commitMutation={commitMutation}
            />
          ))}
        </ol>
        {remainingAgendaCount > 0 ? (
          <div className="calendar-spike__agenda-pagination">
            <button
              aria-controls="calendar-spike-agenda-list"
              onClick={() =>
                setVisibleAgendaCount((current) =>
                  Math.min(items.length, current + AGENDA_PAGE_SIZE),
                )
              }
              type="button"
            >
              Hiển thị thêm {Math.min(AGENDA_PAGE_SIZE, remainingAgendaCount)}{" "}
              mục
            </button>
            <span>
              Còn {remainingAgendaCount} mục trong chương trình bàn phím
            </span>
          </div>
        ) : null}
      </div>
    </section>
  );
}

interface AgendaItemProps {
  item: CalendarItem;
  commitMutation: CommitMutation;
  onAnnouncement: (announcement: MutationAnnouncement) => void;
  onSelectedEvent: (itemId: string) => void;
}

function AgendaItem({
  item,
  commitMutation,
  onAnnouncement,
  onSelectedEvent,
}: AgendaItemProps) {
  const moveByKeyboard = () => {
    const mutation: CalendarMutation = {
      itemId: item.id,
      startsAt: addMinutesToInstant(item.startsAt, 30),
      endsAt: addMinutesToInstant(item.endsAt, 30),
      timeZone: item.timeZone,
      expectedVersion: item.version,
      source: "keyboard",
    };
    void commitWithRevert(
      { revert: () => undefined },
      mutation,
      commitMutation,
      onAnnouncement,
    );
  };

  return (
    <li className="calendar-spike__agenda-item" data-agenda-event-id={item.id}>
      <div>
        <strong>{item.title}</strong>
        <span>
          {item.timeZone} · {item.category === "class" ? "Lớp học" : "Tự học"}
        </span>
      </div>
      <div className="calendar-spike__agenda-actions">
        <button onClick={() => onSelectedEvent(item.id)} type="button">
          Xem chi tiết
        </button>
        <button onClick={moveByKeyboard} type="button">
          Dời sau 30 phút
        </button>
      </div>
    </li>
  );
}
