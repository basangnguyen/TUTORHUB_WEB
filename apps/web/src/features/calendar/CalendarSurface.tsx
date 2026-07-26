import { useMemo, useState } from "react";
import { useI18n } from "../../app/i18n";
import type {
  CalendarDisplayPreferenceViewModel,
  CalendarItemViewModel,
  CalendarView,
} from "./model";
import { CalendarAgenda } from "./CalendarAgenda";
import {
  FullCalendarProjection,
  type CalendarReschedule,
} from "./FullCalendarProjection";

interface CalendarSurfaceProps {
  date: string;
  items: readonly CalendarItemViewModel[];
  locale: string;
  onEditItem?: (item: CalendarItemViewModel) => void;
  onReschedule: CalendarReschedule;
  preference: CalendarDisplayPreferenceViewModel;
  view: CalendarView;
}

export function CalendarSurface({
  date,
  items,
  locale,
  onEditItem,
  onReschedule,
  preference,
  view,
}: CalendarSurfaceProps) {
  const { t } = useI18n();
  const itemsKey = useMemo(
    () => items.map((item) => `${item.id}:${item.version}`).join("|"),
    [items],
  );
  const [surfaceOverride, setSurfaceOverride] = useState<{
    itemsKey: string;
    items: readonly CalendarItemViewModel[];
  } | null>(null);
  const surfaceItems =
    surfaceOverride?.itemsKey === itemsKey ? surfaceOverride.items : items;

  const updateSurfaceItem = (updatedItem: CalendarItemViewModel) => {
    setSurfaceOverride({
      items: surfaceItems.map((item) =>
        item.id === updatedItem.id ? updatedItem : item,
      ),
      itemsKey,
    });
  };

  return (
    <div
      className={`calendar-surface calendar-surface--${preference.density}`}
      data-time-scale={preference.timeScaleMinutes}
    >
      {view === "agenda" ? (
        <CalendarAgenda
          hourCycle={preference.hourCycle}
          items={surfaceItems}
          locale={locale}
          onEditItem={onEditItem}
          secondaryTimezone={preference.secondaryTimezone}
          timezone={preference.viewerTimezone}
        />
      ) : (
        <>
          <FullCalendarProjection
            date={date}
            items={surfaceItems}
            locale={locale}
            onEditItem={onEditItem}
            onItemChanged={updateSurfaceItem}
            onReschedule={onReschedule}
            preference={preference}
            view={view}
          />
          <details className="calendar-agenda-alternative">
            <summary>{t("calendar.keyboardAlternative")}</summary>
            <CalendarAgenda
              hourCycle={preference.hourCycle}
              items={surfaceItems}
              locale={locale}
              onEditItem={onEditItem}
              secondaryTimezone={preference.secondaryTimezone}
              timezone={preference.viewerTimezone}
            />
          </details>
        </>
      )}
    </div>
  );
}
