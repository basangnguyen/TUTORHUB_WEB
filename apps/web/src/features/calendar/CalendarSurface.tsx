import { Info } from "lucide-react";
import { useI18n } from "../../app/i18n";
import type {
  CalendarDisplayPreferenceViewModel,
  CalendarItemViewModel,
  CalendarView,
} from "./model";
import { CalendarAgenda } from "./CalendarAgenda";

interface CalendarSurfaceProps {
  items: readonly CalendarItemViewModel[];
  locale: string;
  onEditItem?: (item: CalendarItemViewModel) => void;
  preference: CalendarDisplayPreferenceViewModel;
  view: CalendarView;
}

export function CalendarSurface({
  items,
  locale,
  onEditItem,
  preference,
  view,
}: CalendarSurfaceProps) {
  const { t } = useI18n();

  return (
    <div
      className={`calendar-surface calendar-surface--${preference.density}`}
      data-time-scale={preference.timeScaleMinutes}
    >
      {view !== "agenda" && (
        <section className="calendar-renderer-gate" role="status">
          <Info aria-hidden="true" />
          <div>
            <h2>{t("calendar.fallbackTitle")}</h2>
            <p>{t("calendar.fallbackDescription")}</p>
          </div>
        </section>
      )}
      <CalendarAgenda
        hourCycle={preference.hourCycle}
        items={items}
        locale={locale}
        onEditItem={onEditItem}
        secondaryTimezone={preference.secondaryTimezone}
        timezone={preference.viewerTimezone}
      />
    </div>
  );
}
