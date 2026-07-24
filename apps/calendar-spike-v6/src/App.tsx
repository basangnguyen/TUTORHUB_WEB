import { useMemo, useRef, useState } from "react";
import FullCalendar from "@fullcalendar/react";
import type { CalendarApi } from "@fullcalendar/core";
import dayGridPlugin from "@fullcalendar/daygrid";
import interactionPlugin from "@fullcalendar/interaction";
import listPlugin from "@fullcalendar/list";
import timeGridPlugin from "@fullcalendar/timegrid";
import { makeFixtureEvents, requestedFixtureCount } from "./fixtures";
import "./styles.css";

const FIXTURE_COUNT = requestedFixtureCount(window.location.search);

export function App() {
  const calendarApi = useRef<CalendarApi | null>(null);
  const [readyMs, setReadyMs] = useState<number | null>(null);
  const events = useMemo(() => makeFixtureEvents(FIXTURE_COUNT), []);

  const changeView = (view: "timeGridWeek" | "dayGridMonth") => {
    calendarApi.current?.changeView(view);
  };

  return (
    <main>
      <header>
        <p>TutorHub V2 · isolated fallback comparator</p>
        <h1>FullCalendar Standard 6.1.21</h1>
        <strong>{FIXTURE_COUNT} fixtures</strong>
        <output data-testid="calendar-ready-ms">
          {readyMs === null ? "Đang đo…" : `${readyMs} ms`}
        </output>
      </header>
      <nav aria-label="Chế độ xem lịch">
        <button onClick={() => changeView("timeGridWeek")} type="button">
          Tuần
        </button>
        <button onClick={() => changeView("dayGridMonth")} type="button">
          Tháng
        </button>
      </nav>
      <section data-calendar-renderer="fullcalendar-standard-v6">
        <FullCalendar
          allDaySlot={false}
          contentHeight={560}
          dayMaxEventRows={6}
          datesSet={(info) => {
            calendarApi.current = info.view.calendar;
            document.body.dataset.calendarRenderedView = info.view.type;
          }}
          eventDisplay="block"
          eventMaxStack={6}
          eventOrder="start,title"
          events={events}
          eventsSet={() => {
            if (readyMs !== null) {
              return;
            }
            window.requestAnimationFrame(() => {
              const startedAt = Number(
                document.body.dataset.calendarStartedAt ?? "0",
              );
              setReadyMs(
                Math.max(0, Math.round(performance.now() - startedAt)),
              );
            });
          }}
          expandRows
          headerToolbar={false}
          height="auto"
          initialDate="2026-07-23"
          initialView="timeGridWeek"
          locale="vi"
          moreLinkClick="popover"
          navLinks
          nowIndicator
          plugins={[
            dayGridPlugin,
            timeGridPlugin,
            listPlugin,
            interactionPlugin,
          ]}
          timeZone="Asia/Ho_Chi_Minh"
        />
      </section>
    </main>
  );
}
