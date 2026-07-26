import { Button, IconButton, Select } from "@tutorhub/ui";
import { ChevronLeft, ChevronRight, SlidersHorizontal } from "lucide-react";
import { useI18n, type TranslationKey } from "../../app/i18n";
import { calendarViews, type CalendarView } from "./model";

const viewTranslationKeys: Record<CalendarView, TranslationKey> = {
  agenda: "calendar.view.agenda",
  day: "calendar.view.day",
  month: "calendar.view.month",
  week: "calendar.view.week",
  work_week: "calendar.view.work_week",
};

interface CalendarToolbarProps {
  rangeTitle: string;
  view: CalendarView;
  preferencesDisabled?: boolean;
  onNext: () => void;
  onOpenPreferences: () => void;
  onPrevious: () => void;
  onToday: () => void;
  onViewChange: (view: CalendarView) => void;
}

export function CalendarToolbar({
  onNext,
  onOpenPreferences,
  onPrevious,
  onToday,
  onViewChange,
  preferencesDisabled = false,
  rangeTitle,
  view,
}: CalendarToolbarProps) {
  const { t } = useI18n();
  const viewOptions = calendarViews.map((value) => ({
    label: t(viewTranslationKeys[value]),
    value,
  }));

  return (
    <div aria-label={t("calendar.viewLabel")} className="calendar-toolbar">
      <div className="calendar-toolbar__navigation">
        <Button onClick={onToday} size="sm" variant="secondary">
          {t("calendar.today")}
        </Button>
        <IconButton
          label={t("calendar.previous")}
          onClick={onPrevious}
          size="sm"
          variant="secondary"
        >
          <ChevronLeft />
        </IconButton>
        <IconButton
          label={t("calendar.next")}
          onClick={onNext}
          size="sm"
          variant="secondary"
        >
          <ChevronRight />
        </IconButton>
        <h2 aria-live="polite" className="calendar-toolbar__range">
          {rangeTitle}
        </h2>
      </div>

      <div className="calendar-toolbar__actions">
        <div className="calendar-toolbar__view-buttons">
          {calendarViews.map((value) => (
            <Button
              aria-pressed={view === value}
              className={
                view === value ? "calendar-toolbar__view--active" : undefined
              }
              key={value}
              onClick={() => onViewChange(value)}
              size="sm"
              variant="quiet"
            >
              {t(viewTranslationKeys[value])}
            </Button>
          ))}
        </div>
        <div className="calendar-toolbar__view-select">
          <Select
            ariaLabel={t("calendar.viewLabel")}
            onValueChange={(value) => onViewChange(value as CalendarView)}
            options={viewOptions}
            value={view}
          />
        </div>
        <Button
          disabled={preferencesDisabled}
          leadingIcon={<SlidersHorizontal />}
          onClick={onOpenPreferences}
          size="sm"
          variant="secondary"
        >
          {t("calendar.settings")}
        </Button>
      </div>
    </div>
  );
}
