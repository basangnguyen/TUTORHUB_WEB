import { useMemo, useState } from "react";
import { FullCalendarSurface } from "./calendar/FullCalendarSurface";
import {
  DST_FIXTURES,
  FIXTURE_DATE,
  FIXTURE_TIME_ZONE,
  makeFixtureItems,
  requestedFixtureCount,
} from "./calendar/fixtures";
import { createDemoCommitMutation } from "./calendar/CalendarSurface";
import { resolveCivilDateTime } from "./calendar/civilTime";
import type {
  CalendarItem,
  CalendarView,
  MutationAnnouncement,
} from "./calendar/domain";
import "./styles.css";

const FIXTURE_COUNT = requestedFixtureCount(window.location.search);
const INITIAL_ITEMS = [
  ...makeFixtureItems(FIXTURE_COUNT),
  ...DST_FIXTURES.filter(
    (dstItem) =>
      FIXTURE_COUNT <= 48 &&
      !makeFixtureItems(48).some((item) => item.id === dstItem.id),
  ),
];

function updateItem(
  items: readonly CalendarItem[],
  updated: CalendarItem,
): CalendarItem[] {
  return items.map((item) => (item.id === updated.id ? updated : item));
}

export function App() {
  const [items, setItems] = useState<CalendarItem[]>(INITIAL_ITEMS);
  const [view, setView] = useState<CalendarView>("timeGridWeek");
  const [announcement, setAnnouncement] = useState<MutationAnnouncement>({
    tone: "success",
    message: "Spike sẵn sàng. Có thể dùng kéo/thả hoặc chương trình bàn phím.",
  });
  const [readyMs, setReadyMs] = useState<number | null>(null);
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null);

  const commitMutation = useMemo(
    () =>
      createDemoCommitMutation(items, (updated) => {
        setItems((current) => updateItem(current, updated));
      }),
    [items],
  );

  const selectedItem = selectedEventId
    ? items.find((item) => item.id === selectedEventId)
    : undefined;

  const handleReady = (visibleEventCount: number) => {
    if (readyMs !== null) {
      return;
    }
    const startedAt = Number(document.body.dataset.calendarStartedAt ?? "0");
    setReadyMs(Math.max(0, Math.round(performance.now() - startedAt)));
    document.body.dataset.calendarVisibleEvents = String(visibleEventCount);
  };

  const handleAnnouncement = (next: MutationAnnouncement) => {
    setAnnouncement(next);
  };

  return (
    <main className="calendar-spike" data-testid="calendar-spike">
      <header className="calendar-spike__header">
        <div>
          <p className="calendar-spike__eyebrow">TutorHub V2 · P3-CAL-01</p>
          <h1>Calendar renderer &amp; recurrence spike</h1>
          <p className="calendar-spike__lede">
            Teams-inspired shell, Warm Academic palette, UTC instant + IANA
            timezone. Đây là package kiểm chứng kỹ thuật, chưa phải route
            production.
          </p>
        </div>
        <div className="calendar-spike__badges" aria-label="Trạng thái spike">
          <span>FullCalendar Standard 7.0.1</span>
          <span>Temporal 1.0.1</span>
          <span>{FIXTURE_COUNT} fixtures</span>
        </div>
      </header>

      <section className="calendar-spike__status-panel" aria-label="Kết quả đo">
        <div>
          <span className="calendar-spike__label">Render ready</span>
          <strong data-testid="calendar-ready-ms">
            {readyMs === null ? "Đang đo…" : `${readyMs} ms`}
          </strong>
        </div>
        <div>
          <span className="calendar-spike__label">Múi giờ fixture</span>
          <strong>{FIXTURE_TIME_ZONE}</strong>
        </div>
        <div>
          <span className="calendar-spike__label">Ngày kiểm chứng</span>
          <strong>{FIXTURE_DATE}</strong>
        </div>
        <div
          aria-live="polite"
          className={`calendar-spike__announcement calendar-spike__announcement--${announcement.tone}`}
          data-testid="calendar-announcement"
        >
          {announcement.message}
        </div>
      </section>

      <FullCalendarSurface
        commitMutation={commitMutation}
        items={items}
        onAnnouncement={handleAnnouncement}
        onReady={handleReady}
        onSelectedEvent={setSelectedEventId}
        onViewChange={setView}
        view={view}
      />

      <section className="calendar-spike__dst" aria-labelledby="dst-heading">
        <div>
          <p className="calendar-spike__eyebrow">Civil time contract</p>
          <h2 id="dst-heading">DST gap/overlap fixture</h2>
          <p>
            Server sẽ reject gap và yêu cầu lựa chọn rõ ràng ở overlap; không
            dùng timezone của máy người dùng để suy diễn.
          </p>
        </div>
        <div className="calendar-spike__dst-grid">
          <DstCheck
            label="America/New_York · 2026-03-08 02:30"
            input={{
              local: "2026-03-08T02:30",
              timeZone: "America/New_York",
              disambiguation: "reject",
            }}
          />
          <DstCheck
            label="America/New_York · overlap earlier"
            input={{
              local: "2026-11-01T01:30",
              timeZone: "America/New_York",
              disambiguation: "earlier",
            }}
          />
          <DstCheck
            label="America/New_York · overlap later"
            input={{
              local: "2026-11-01T01:30",
              timeZone: "America/New_York",
              disambiguation: "later",
            }}
          />
        </div>
      </section>

      {selectedItem ? (
        <aside
          aria-label="Chi tiết sự kiện"
          className="calendar-spike__detail"
          data-testid="calendar-detail"
        >
          <div>
            <span className="calendar-spike__label">Sự kiện đã chọn</span>
            <strong>{selectedItem.title}</strong>
          </div>
          <button onClick={() => setSelectedEventId(null)} type="button">
            Đóng chi tiết
          </button>
        </aside>
      ) : null}
    </main>
  );
}

interface DstCheckProps {
  label: string;
  input: Parameters<typeof resolveCivilDateTime>[0];
}

function DstCheck({ label, input }: DstCheckProps) {
  const outcome = (() => {
    try {
      const result = resolveCivilDateTime(input);
      return `${result.instant} (${result.offset})`;
    } catch (error) {
      return error instanceof Error ? `Từ chối: ${error.message}` : "Từ chối";
    }
  })();
  return (
    <article className="calendar-spike__dst-card">
      <h3>{label}</h3>
      <code>{outcome}</code>
    </article>
  );
}
