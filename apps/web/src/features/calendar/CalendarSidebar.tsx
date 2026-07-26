import { Button } from "@tutorhub/ui";
import { Search, X } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { useI18n } from "../../app/i18n";
import { miniMonthDays } from "./dateRange";
import {
  hasActiveCalendarFilters,
  type CalendarFilters,
  type CalendarItemViewModel,
} from "./model";

interface CalendarSidebarProps {
  date: string;
  filters: CalendarFilters;
  items: readonly CalendarItemViewModel[];
  weekStartsOn: 0 | 1;
  onDateChange: (date: string) => void;
  onFiltersChange: (filters: CalendarFilters) => void;
}

function toggleValue(values: readonly string[], value: string) {
  return values.includes(value)
    ? values.filter((item) => item !== value)
    : [...values, value].sort((left, right) => left.localeCompare(right));
}

export function CalendarSidebar({
  date,
  filters,
  items,
  onDateChange,
  onFiltersChange,
  weekStartsOn,
}: CalendarSidebarProps) {
  const { language, t } = useI18n();
  const locale = language === "vi" ? "vi-VN" : "en-US";
  const [searchDraft, setSearchDraft] = useState({
    source: filters.search,
    value: filters.search,
  });
  const search =
    searchDraft.source === filters.search ? searchDraft.value : filters.search;
  const monthDays = miniMonthDays(date, weekStartsOn);
  const selectedDate = new Date(`${date}T12:00:00Z`);
  const monthTitle = new Intl.DateTimeFormat(locale, {
    month: "long",
    timeZone: "UTC",
    year: "numeric",
  }).format(selectedDate);
  const fullDateFormatter = new Intl.DateTimeFormat(locale, {
    dateStyle: "full",
    timeZone: "UTC",
  });
  const weekdayFormatter = new Intl.DateTimeFormat(locale, {
    timeZone: "UTC",
    weekday: "narrow",
  });
  const weekdayStart = weekStartsOn === 1 ? 5 : 4;
  const weekdayLabels = Array.from({ length: 7 }, (_, index) =>
    weekdayFormatter.format(new Date(Date.UTC(2026, 0, weekdayStart + index))),
  );

  const classOptions = useMemo(() => {
    const options = new Map<string, string>();
    for (const item of items) {
      if (item.classID) {
        options.set(item.classID, item.classTitle || item.classID);
      }
    }
    for (const classID of filters.classIDs) {
      if (!options.has(classID)) {
        options.set(classID, classID);
      }
    }
    return [...options].sort((left, right) =>
      left[1].localeCompare(right[1], locale),
    );
  }, [filters.classIDs, items, locale]);

  const submitSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onFiltersChange({ ...filters, search: search.trim().slice(0, 200) });
  };

  return (
    <aside aria-label={t("calendar.sidebarLabel")} className="calendar-sidebar">
      <section className="calendar-mini-month">
        <h2>{monthTitle}</h2>
        <div
          aria-label={t("calendar.miniMonthLabel")}
          className="calendar-mini-month__grid"
          role="group"
        >
          {weekdayLabels.map((label, index) => (
            <span
              aria-hidden="true"
              className="calendar-mini-month__weekday"
              key={`${label}-${index}`}
            >
              {label}
            </span>
          ))}
          {monthDays.map((day) => (
            <button
              aria-current={day.selected ? "date" : undefined}
              aria-label={fullDateFormatter.format(
                new Date(`${day.date}T12:00:00Z`),
              )}
              className={`calendar-mini-month__day${
                day.inMonth ? "" : " calendar-mini-month__day--outside"
              }${day.selected ? " calendar-mini-month__day--selected" : ""}`}
              key={day.date}
              onClick={() => onDateChange(day.date)}
              type="button"
            >
              {day.day}
            </button>
          ))}
        </div>
      </section>

      <form className="calendar-filter-search" onSubmit={submitSearch}>
        <label htmlFor="calendar-search">{t("calendar.searchLabel")}</label>
        <div>
          <Search aria-hidden="true" />
          <input
            id="calendar-search"
            maxLength={200}
            onChange={(event) =>
              setSearchDraft({
                source: filters.search,
                value: event.target.value,
              })
            }
            placeholder={t("calendar.searchPlaceholder")}
            type="search"
            value={search}
          />
          <Button size="sm" type="submit" variant="secondary">
            {t("calendar.applySearch")}
          </Button>
        </div>
      </form>

      <fieldset className="calendar-filter-group">
        <legend>{t("calendar.filterType")}</legend>
        <label>
          <input
            checked
            disabled
            onChange={() =>
              onFiltersChange({
                ...filters,
                types: toggleValue(filters.types, "class_session"),
              })
            }
            type="checkbox"
          />
          <span>{t("calendar.filter.class_session")}</span>
        </label>
      </fieldset>

      <fieldset className="calendar-filter-group">
        <legend>{t("calendar.filterStatus")}</legend>
        {(["scheduled", "cancelled"] as const).map((status) => (
          <label key={status}>
            <input
              checked={filters.statuses.includes(status)}
              onChange={() =>
                onFiltersChange({
                  ...filters,
                  statuses: toggleValue(filters.statuses, status),
                })
              }
              type="checkbox"
            />
            <span>{t(`calendar.filter.${status}`)}</span>
          </label>
        ))}
      </fieldset>

      <fieldset className="calendar-filter-group">
        <legend>{t("calendar.filterClass")}</legend>
        {classOptions.length === 0 ? (
          <p>{t("calendar.noClassFilters")}</p>
        ) : (
          classOptions.map(([classID, title]) => (
            <label key={classID}>
              <input
                checked={filters.classIDs.includes(classID)}
                onChange={() =>
                  onFiltersChange({
                    ...filters,
                    classIDs: toggleValue(filters.classIDs, classID),
                  })
                }
                type="checkbox"
              />
              <span>{title}</span>
            </label>
          ))
        )}
      </fieldset>

      {hasActiveCalendarFilters(filters) && (
        <Button
          leadingIcon={<X />}
          onClick={() =>
            onFiltersChange({
              classIDs: [],
              search: "",
              statuses: [],
              types: ["class_session"],
            })
          }
          size="sm"
          variant="quiet"
        >
          {t("calendar.clearFilters")}
        </Button>
      )}
    </aside>
  );
}
