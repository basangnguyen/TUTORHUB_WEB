import {
  Button,
  EmptyState,
  ErrorState,
  ForbiddenState,
  OfflineState,
  Skeleton,
  SkeletonGroup,
} from "@tutorhub/ui";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useUpdateClassSession } from "../app/classSessions";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { CalendarPreferencesDrawer } from "../features/calendar/CalendarPreferencesDrawer";
import {
  CalendarQuickCreate,
  CalendarSessionEdit,
} from "../features/calendar/CalendarSessionEditors";
import {
  RecurringScopeDialog,
  type RecurringScopeRequest,
} from "../features/calendar/RecurringScopeDialog";
import { CalendarSidebar } from "../features/calendar/CalendarSidebar";
import { CalendarSurface } from "../features/calendar/CalendarSurface";
import type { CalendarRescheduleInput } from "../features/calendar/FullCalendarProjection";
import { SessionDetailDrawer } from "../features/calendar/SessionDetailDrawer";
import { CalendarToolbar } from "../features/calendar/CalendarToolbar";
import { WorkingScheduleDrawer } from "../features/calendar/WorkingScheduleDrawer";
import {
  calendarRangeForView,
  calendarRangeTitle,
  calendarToday,
  moveCalendarDate,
} from "../features/calendar/dateRange";
import {
  hasActiveCalendarFilters,
  stableCalendarItemOrder,
  type CalendarDisplayPreferenceViewModel,
  type CalendarFilters,
  type CalendarItemViewModel,
  type CalendarRouteState,
  type CalendarView,
} from "../features/calendar/model";
import {
  useCalendarDisplayPreference,
  useCalendarItems,
  useCalendarWorkingSchedule,
  useUpdateCalendarDisplayPreference,
  useUpdateCalendarWorkingSchedule,
} from "../features/calendar/queries";
import {
  calendarRouteSearch,
  parseCalendarRouteState,
} from "../features/calendar/urlState";
import "../features/calendar/calendar.css";

function useOnlineStatus() {
  const [online, setOnline] = useState(() => navigator.onLine);

  useEffect(() => {
    const markOnline = () => setOnline(true);
    const markOffline = () => setOnline(false);
    window.addEventListener("online", markOnline);
    window.addEventListener("offline", markOffline);
    return () => {
      window.removeEventListener("online", markOnline);
      window.removeEventListener("offline", markOffline);
    };
  }, []);

  return online;
}

function useMobileCalendarDefault() {
  const [mobile, setMobile] = useState(() =>
    typeof window.matchMedia === "function"
      ? window.matchMedia("(max-width: 48rem)").matches
      : false,
  );

  useEffect(() => {
    if (typeof window.matchMedia !== "function") {
      return;
    }
    const media = window.matchMedia("(max-width: 48rem)");
    const update = () => setMobile(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  return mobile;
}

function fallbackPreference(
  timezone: string,
  locale: "vi-VN" | "en-US",
): CalendarDisplayPreferenceViewModel {
  return {
    defaultView: "week",
    density: "comfortable",
    hourCycle: "h23",
    locale,
    secondaryTimezone: null,
    timeScaleMinutes: 30,
    updatedAt: "",
    version: 0,
    viewerTimezone: timezone,
    weekStartsOn: 1,
  };
}

interface CalendarPreferencesControllerProps {
  onOpenChange: (open: boolean) => void;
  open: boolean;
  preference: CalendarDisplayPreferenceViewModel;
  preferenceQuery: ReturnType<typeof useCalendarDisplayPreference>;
  tenantID: string | undefined;
  userID: string | undefined;
}

function CalendarPreferencesController({
  onOpenChange,
  open,
  preference,
  preferenceQuery,
  tenantID,
  userID,
}: CalendarPreferencesControllerProps) {
  const updatePreference = useUpdateCalendarDisplayPreference(tenantID, userID);

  return (
    <CalendarPreferencesDrawer
      error={updatePreference.error}
      onOpenChange={(nextOpen) => {
        onOpenChange(nextOpen);
        if (!nextOpen && !updatePreference.isPending) {
          updatePreference.reset();
        }
      }}
      onReload={async () => {
        const result = await preferenceQuery.refetch();
        updatePreference.reset();
        return result.data;
      }}
      onSave={(draft) => updatePreference.save(draft)}
      open={open}
      pending={updatePreference.isPending}
      preference={preference}
    />
  );
}

interface WorkingScheduleControllerProps {
  onOpenChange: (open: boolean) => void;
  open: boolean;
  tenantID: string | undefined;
  userID: string | undefined;
}

function WorkingScheduleController({
  onOpenChange,
  open,
  tenantID,
  userID,
}: WorkingScheduleControllerProps) {
  const scheduleQuery = useCalendarWorkingSchedule(tenantID, userID, open);
  const updateSchedule = useUpdateCalendarWorkingSchedule(tenantID, userID);

  return (
    <WorkingScheduleDrawer
      error={updateSchedule.error ?? scheduleQuery.error}
      loading={scheduleQuery.isPending || scheduleQuery.isFetching}
      onOpenChange={(nextOpen) => {
        onOpenChange(nextOpen);
        if (!nextOpen && !updateSchedule.isPending) {
          updateSchedule.reset();
        }
      }}
      onReload={async () => {
        const result = await scheduleQuery.refetch();
        updateSchedule.reset();
        return result.data;
      }}
      onSave={(input) => updateSchedule.save(input)}
      open={open}
      pending={updateSchedule.isPending}
      schedule={scheduleQuery.data}
    />
  );
}

export function CalendarPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const [searchParams, setSearchParams] = useSearchParams();
  const [preferencesPrincipal, setPreferencesPrincipal] = useState<
    string | null
  >(null);
  const [workingSchedulePrincipal, setWorkingSchedulePrincipal] = useState<
    string | null
  >(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [editingItem, setEditingItem] = useState<CalendarItemViewModel | null>(
    null,
  );
  const [selectedItem, setSelectedItem] =
    useState<CalendarItemViewModel | null>(null);
  const [recurringScopeRequest, setRecurringScopeRequest] =
    useState<RecurringScopeRequest | null>(null);
  const online = useOnlineStatus();
  const mobile = useMobileCalendarDefault();
  const tenantID = session.currentUser?.active_tenant?.id;
  const userID = session.currentUser?.user.id;
  const principalKey = `${tenantID ?? "inactive"}:${userID ?? "anonymous"}`;
  const preferencesOpen = preferencesPrincipal === principalKey;
  const workingScheduleOpen = workingSchedulePrincipal === principalKey;
  const userTimezone =
    session.currentUser?.user.timezone ||
    Intl.DateTimeFormat().resolvedOptions().timeZone ||
    "UTC";
  const interfaceLocale = language === "vi" ? "vi-VN" : "en-US";
  const preferenceQuery = useCalendarDisplayPreference(tenantID, userID);
  const updateSession = useUpdateClassSession(tenantID);
  const preference =
    preferenceQuery.data ?? fallbackPreference(userTimezone, interfaceLocale);

  const rescheduleSession = useCallback(
    async ({
      endsAt,
      expectedVersion,
      item,
      startsAt,
    }: CalendarRescheduleInput) => {
      if (!item.classID || !item.canReschedule) {
        throw new Error("This calendar item cannot be rescheduled.");
      }
      if (item.seriesID && item.occurrenceKey && tenantID) {
        return await new Promise<CalendarItemViewModel>((resolve, reject) => {
          setRecurringScopeRequest({
            item,
            startsAt,
            endsAt,
            tenantID,
            onCancel: () => {
              setRecurringScopeRequest(null);
              reject(new Error("Recurring schedule change cancelled."));
            },
            onSuccess: (updatedItem) => {
              setRecurringScopeRequest(null);
              resolve(updatedItem);
            },
          });
        });
      }
      const updated = await updateSession.mutateAsync({
        classID: item.classID,
        input: {
          ends_at: endsAt,
          expected_version: expectedVersion,
          starts_at: startsAt,
          timezone: item.displayTimezone,
        },
        sessionID: item.sourceID,
      });
      return {
        ...item,
        canCancel: updated.viewer_access.can_cancel,
        canEdit: updated.viewer_access.can_update,
        canReschedule: updated.viewer_access.can_update,
        displayTimezone: updated.timezone,
        endsAt: updated.ends_at,
        startsAt: updated.starts_at,
        status: updated.status,
        title: updated.title,
        version: updated.version,
      };
    },
    [tenantID, updateSession],
  );
  const defaultView: CalendarView = mobile ? "agenda" : preference.defaultView;
  const routeState = parseCalendarRouteState(searchParams, {
    date: calendarToday(preference.viewerTimezone),
    view: defaultView,
  });
  const range = calendarRangeForView(
    routeState.date,
    routeState.view,
    preference.viewerTimezone,
    preference.weekStartsOn,
  );
  const itemsQuery = useCalendarItems({
    enabled: preferenceQuery.isSuccess,
    filters: routeState.filters,
    range,
    tenantID,
    timezone: preference.viewerTimezone,
    userID,
  });
  const items = useMemo(() => {
    const seen = new Set<string>();
    return (itemsQuery.data?.pages ?? [])
      .flatMap((page) => page.items)
      .filter((item) => {
        if (seen.has(item.id)) {
          return false;
        }
        seen.add(item.id);
        return true;
      })
      .sort(stableCalendarItemOrder);
  }, [itemsQuery.data?.pages]);
  const rangeTitle = calendarRangeTitle(
    routeState.date,
    routeState.view,
    preference.viewerTimezone,
    preference.locale,
    preference.weekStartsOn,
  );

  const navigate = (state: CalendarRouteState, replace = false) => {
    setSearchParams(calendarRouteSearch(state), { replace });
  };
  const changeFilters = (filters: CalendarFilters) =>
    navigate({ ...routeState, filters });
  const preferenceConcealed = shouldConcealTenantScopedData(
    preferenceQuery.error,
  );
  const itemsConcealed = shouldConcealTenantScopedData(itemsQuery.error);
  const isForbidden = preferenceConcealed || itemsConcealed;
  const calendarPending =
    preferenceQuery.isPending ||
    (preferenceQuery.isSuccess && itemsQuery.isPending);
  const retryCalendar = () => {
    if (!preferenceQuery.isSuccess || preferenceQuery.isError) {
      void preferenceQuery.refetch();
      return;
    }
    void itemsQuery.refetch();
  };

  return (
    <div className="calendar-page">
      <header className="calendar-page__header">
        <div>
          <p>{t("calendar.kicker")}</p>
          <h1>{t("calendar.title")}</h1>
          <span>{t("calendar.description")}</span>
        </div>
      </header>

      <div className="calendar-frame">
        <CalendarSidebar
          date={routeState.date}
          filters={routeState.filters}
          items={items}
          onDateChange={(date) => navigate({ ...routeState, date })}
          onFiltersChange={changeFilters}
          weekStartsOn={preference.weekStartsOn}
        />

        <section className="calendar-main">
          <CalendarToolbar
            onCreateSession={() => setCreateOpen(true)}
            onNext={() =>
              navigate({
                ...routeState,
                date: moveCalendarDate(routeState.date, routeState.view, 1),
              })
            }
            onOpenPreferences={() => setPreferencesPrincipal(principalKey)}
            onOpenWorkingSchedule={() =>
              setWorkingSchedulePrincipal(principalKey)
            }
            onPrevious={() =>
              navigate({
                ...routeState,
                date: moveCalendarDate(routeState.date, routeState.view, -1),
              })
            }
            onToday={() =>
              navigate({
                ...routeState,
                date: calendarToday(preference.viewerTimezone),
              })
            }
            onViewChange={(view) => navigate({ ...routeState, view })}
            preferencesDisabled={!preferenceQuery.isSuccess}
            rangeTitle={rangeTitle}
            view={routeState.view}
            workingScheduleDisabled={!tenantID || !userID}
          />

          {items.length > 0 &&
            (!online || itemsQuery.isError || preferenceQuery.isError) && (
              <div className="calendar-degraded" role="alert">
                <p>
                  {online
                    ? t("calendar.degraded")
                    : t("calendar.offlineCached")}
                </p>
                <Button
                  leadingIcon={<RefreshCw />}
                  onClick={retryCalendar}
                  size="sm"
                  variant="secondary"
                >
                  {t("calendar.retry")}
                </Button>
              </div>
            )}

          <div className="calendar-content">
            {calendarPending && (
              <SkeletonGroup label={t("calendar.loading")}>
                <Skeleton height="4rem" />
                <Skeleton height="7rem" />
                <Skeleton height="7rem" />
                <Skeleton height="7rem" />
              </SkeletonGroup>
            )}

            {!calendarPending && isForbidden && (
              <ForbiddenState
                description={t("calendar.forbiddenDescription")}
                title={t("calendar.forbiddenTitle")}
              />
            )}

            {!calendarPending &&
              !isForbidden &&
              !online &&
              items.length === 0 && (
                <OfflineState
                  actions={
                    <Button
                      leadingIcon={<RefreshCw />}
                      onClick={retryCalendar}
                      variant="secondary"
                    >
                      {t("calendar.retry")}
                    </Button>
                  }
                  description={t("calendar.offlineDescription")}
                  title={t("calendar.offlineTitle")}
                />
              )}

            {!calendarPending &&
              online &&
              !isForbidden &&
              (preferenceQuery.isError || itemsQuery.isError) &&
              items.length === 0 && (
                <ErrorState
                  actions={
                    <Button
                      leadingIcon={<RefreshCw />}
                      onClick={retryCalendar}
                      variant="secondary"
                    >
                      {t("calendar.retry")}
                    </Button>
                  }
                  description={t("calendar.errorDescription")}
                  title={t("calendar.errorTitle")}
                />
              )}

            {!calendarPending &&
              preferenceQuery.isSuccess &&
              !preferenceQuery.isError &&
              !itemsQuery.isError &&
              items.length === 0 && (
                <EmptyState
                  actions={
                    hasActiveCalendarFilters(routeState.filters) ? (
                      <Button
                        onClick={() =>
                          changeFilters({
                            classIDs: [],
                            search: "",
                            statuses: [],
                            types: ["class_session"],
                          })
                        }
                        variant="secondary"
                      >
                        {t("calendar.clearFilters")}
                      </Button>
                    ) : undefined
                  }
                  description={
                    hasActiveCalendarFilters(routeState.filters)
                      ? t("calendar.filteredEmptyDescription")
                      : t("calendar.emptyDescription")
                  }
                  title={
                    hasActiveCalendarFilters(routeState.filters)
                      ? t("calendar.filteredEmptyTitle")
                      : t("calendar.emptyTitle")
                  }
                />
              )}

            {items.length > 0 && !preferenceConcealed && !itemsConcealed && (
              <>
                <CalendarSurface
                  date={routeState.date}
                  items={items}
                  locale={preference.locale}
                  onOpenItem={setSelectedItem}
                  onReschedule={rescheduleSession}
                  preference={preference}
                  view={routeState.view}
                />
                {itemsQuery.hasNextPage && (
                  <div className="calendar-load-more">
                    {itemsQuery.isFetchNextPageError && (
                      <p role="alert">{t("calendar.loadMoreError")}</p>
                    )}
                    <Button
                      disabled={!online}
                      loading={itemsQuery.isFetchingNextPage}
                      loadingLabel={t("calendar.loadingMore")}
                      onClick={() => void itemsQuery.fetchNextPage()}
                      variant="secondary"
                    >
                      {t("calendar.loadMore")}
                    </Button>
                  </div>
                )}
              </>
            )}
          </div>
        </section>
      </div>

      <CalendarPreferencesController
        key={principalKey}
        onOpenChange={(open) => {
          setPreferencesPrincipal(open ? principalKey : null);
        }}
        open={preferencesOpen}
        preference={preference}
        preferenceQuery={preferenceQuery}
        tenantID={tenantID}
        userID={userID}
      />
      <WorkingScheduleController
        key={`working-schedule:${principalKey}`}
        onOpenChange={(open) => {
          setWorkingSchedulePrincipal(open ? principalKey : null);
        }}
        open={workingScheduleOpen}
        tenantID={tenantID}
        userID={userID}
      />
      <SessionDetailDrawer
        hourCycle={preference.hourCycle}
        item={selectedItem}
        locale={preference.locale}
        onClose={() => setSelectedItem(null)}
        onEdit={(item) => {
          setSelectedItem(null);
          setEditingItem(item);
        }}
      />
      <CalendarQuickCreate
        onOpenChange={setCreateOpen}
        onSaved={() => void itemsQuery.refetch()}
        open={createOpen}
        tenantID={tenantID}
      />
      <CalendarSessionEdit
        item={editingItem}
        onCancelRecurring={(item) => {
          if (!tenantID) {
            return;
          }
          setEditingItem(null);
          setRecurringScopeRequest({
            item,
            operation: "cancel",
            tenantID,
            onCancel: () => setRecurringScopeRequest(null),
            onSuccess: () => {
              setRecurringScopeRequest(null);
              void itemsQuery.refetch();
            },
          });
        }}
        onClose={() => setEditingItem(null)}
        onSaved={() => void itemsQuery.refetch()}
        tenantID={tenantID}
      />
      <RecurringScopeDialog
        key={
          recurringScopeRequest
            ? [
                recurringScopeRequest.item.id,
                recurringScopeRequest.operation ?? "update",
                recurringScopeRequest.startsAt ?? "",
                recurringScopeRequest.endsAt ?? "",
              ].join(":")
            : "closed"
        }
        request={recurringScopeRequest}
      />
    </div>
  );
}
