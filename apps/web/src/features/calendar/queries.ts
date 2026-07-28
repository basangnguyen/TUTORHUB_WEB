import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  getCalendarDisplayPreference,
  getCalendarWorkingSchedule,
  listCalendarItems,
  queryCalendarAvailability,
  rotateCSRFToken,
  updateCalendarDisplayPreference,
  updateCalendarWorkingSchedule,
  type CalendarAvailabilityQueryRequest,
  type CalendarAvailabilityQueryResponse,
  type CalendarDisplayPreference,
  type CalendarItem,
  type CalendarItemStatus,
  type CalendarSourceType,
  type CalendarWorkingSchedule,
  type CurrentUser,
  type UpdateCalendarDisplayPreferenceRequest,
  type UpdateCalendarWorkingScheduleRequest,
} from "@tutorhub/api-client";
import { currentPrincipalGeneration } from "../../app/queryClient";
import type {
  CalendarDisplayPreferenceDraft,
  CalendarDisplayPreferenceViewModel,
  CalendarFilters,
  CalendarItemViewModel,
  CalendarRange,
} from "./model";

const calendarPageSize = 200;
const allowedStatuses = new Set<CalendarItemStatus>([
  "cancelled",
  "ended",
  "live",
  "scheduled",
]);

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function normalize(values: readonly string[]) {
  return [...new Set(values.filter(Boolean))].sort((left, right) =>
    left.localeCompare(right),
  );
}

function calendarStatuses(values: readonly string[]) {
  return normalize(values).filter((value): value is CalendarItemStatus =>
    allowedStatuses.has(value as CalendarItemStatus),
  );
}

function calendarTypes(values: readonly string[]) {
  return normalize(values).filter(
    (value): value is CalendarSourceType => value === "class_session",
  );
}

export const calendarQueryKeys = {
  all: ["calendar"] as const,
  principal: (tenantID: string, userID: string) =>
    ["calendar", tenantID, userID] as const,
  items: (
    tenantID: string,
    userID: string,
    timezone: string,
    range: CalendarRange,
    filters: CalendarFilters,
  ) =>
    [
      "calendar",
      tenantID,
      userID,
      "items",
      timezone,
      range.from,
      range.to,
      filters.search.trim(),
      normalize(filters.types).join(","),
      normalize(filters.classIDs).join(","),
      normalize(filters.statuses).join(","),
    ] as const,
  itemLists: (tenantID: string, userID: string) =>
    ["calendar", tenantID, userID, "items"] as const,
  preference: (tenantID: string, userID: string) =>
    ["calendar", tenantID, userID, "display-preference"] as const,
  workingSchedule: (tenantID: string, userID: string) =>
    ["calendar", tenantID, userID, "working-schedule"] as const,
};

function shouldRetryCalendarQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function mapCalendarItem(item: CalendarItem): CalendarItemViewModel {
  return {
    allDay: item.all_day,
    canCancel: item.viewer_capabilities.can_cancel,
    canEdit: item.viewer_capabilities.can_edit,
    canReschedule: item.viewer_capabilities.can_reschedule,
    canView: item.viewer_capabilities.can_view,
    classID: item.class_id,
    classTitle: item.class_title,
    colorToken: item.color_token,
    displayTimezone: item.display_timezone,
    endsAt: item.ends_at,
    id: item.id,
    occurrenceKey: item.occurrence_key,
    sourceID: item.source_id,
    seriesID: item.series_id ?? null,
    sourceType: item.source_type,
    startsAt: item.starts_at,
    status: item.status,
    title: item.title,
    version: item.version,
  };
}

export function mapCalendarPreference(
  preference: CalendarDisplayPreference,
): CalendarDisplayPreferenceViewModel {
  return {
    defaultView: preference.default_view,
    density: preference.density,
    hourCycle: preference.time_format === "12h" ? "h12" : "h23",
    locale: preference.locale,
    secondaryTimezone: preference.secondary_timezone,
    timeScaleMinutes: preference.time_scale_minutes,
    updatedAt: preference.updated_at,
    version: preference.version,
    viewerTimezone: preference.viewer_timezone,
    weekStartsOn: preference.week_start === "sunday" ? 0 : 1,
  };
}

export interface CalendarItemPageViewModel {
  items: readonly CalendarItemViewModel[];
  nextCursor: string | null;
}

interface CalendarItemsQueryInput {
  enabled?: boolean;
  filters: CalendarFilters;
  range: CalendarRange;
  tenantID: string | undefined;
  timezone: string;
  userID: string | undefined;
}

export function useCalendarItems({
  enabled = true,
  filters,
  range,
  tenantID,
  timezone,
  userID,
}: CalendarItemsQueryInput) {
  return useInfiniteQuery({
    queryKey: calendarQueryKeys.items(
      tenantID ?? "inactive",
      userID ?? "anonymous",
      timezone,
      range,
      filters,
    ),
    queryFn: async ({
      pageParam,
      signal,
    }): Promise<CalendarItemPageViewModel> => {
      const page = await listCalendarItems(
        tenantID ?? "",
        {
          class_ids: normalize(filters.classIDs),
          cursor: pageParam ?? undefined,
          from: range.from,
          limit: calendarPageSize,
          search: filters.search.trim() || undefined,
          statuses: calendarStatuses(filters.statuses),
          to: range.to,
          types: calendarTypes(filters.types),
          viewer_timezone: timezone,
        },
        { baseUrl: getApiBaseUrl(), signal },
      );
      return {
        items: page.items.map(mapCalendarItem),
        nextCursor: page.next_cursor,
      };
    },
    enabled: enabled && Boolean(tenantID && userID && timezone),
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryCalendarQuery,
    staleTime: 20_000,
  });
}

export function useCalendarDisplayPreference(
  tenantID: string | undefined,
  userID: string | undefined,
) {
  return useQuery({
    queryKey: calendarQueryKeys.preference(
      tenantID ?? "inactive",
      userID ?? "anonymous",
    ),
    queryFn: ({ signal }) =>
      getCalendarDisplayPreference(tenantID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }).then(mapCalendarPreference),
    enabled: Boolean(tenantID && userID),
    retry: shouldRetryCalendarQuery,
    staleTime: 60_000,
  });
}

export function useCalendarWorkingSchedule(
  tenantID: string | undefined,
  userID: string | undefined,
  enabled = true,
) {
  return useQuery({
    queryKey: calendarQueryKeys.workingSchedule(
      tenantID ?? "inactive",
      userID ?? "anonymous",
    ),
    queryFn: ({ signal }) =>
      getCalendarWorkingSchedule(tenantID ?? "", {
        baseUrl: getApiBaseUrl(),
        signal,
      }),
    enabled: enabled && Boolean(tenantID && userID),
    retry: shouldRetryCalendarQuery,
    staleTime: 60_000,
  });
}

function preferenceRequest(
  draft: CalendarDisplayPreferenceDraft,
): UpdateCalendarDisplayPreferenceRequest {
  return {
    default_view: draft.defaultView,
    density: draft.density,
    expected_version: draft.version,
    locale: draft.locale === "en-US" ? "en-US" : "vi-VN",
    secondary_timezone: draft.secondaryTimezone,
    time_format: draft.hourCycle === "h12" ? "12h" : "24h",
    time_scale_minutes: draft.timeScaleMinutes,
    viewer_timezone: draft.viewerTimezone,
    week_start: draft.weekStartsOn === 0 ? "sunday" : "monday",
  };
}

async function invalidateCalendarItems(
  queryClient: QueryClient,
  tenantID: string,
  userID: string,
) {
  await queryClient.invalidateQueries({
    queryKey: calendarQueryKeys.itemLists(tenantID, userID),
  });
}

export interface CalendarPrincipalSnapshot {
  generation: number;
  tenantID: string;
  userID: string;
}

export function isCurrentCalendarPrincipal(
  queryClient: QueryClient,
  { generation, tenantID, userID }: CalendarPrincipalSnapshot,
) {
  const currentUser = queryClient.getQueryData<CurrentUser>(["auth", "me"]);
  return Boolean(
    currentUser &&
    currentPrincipalGeneration(queryClient) === generation &&
    currentUser.active_tenant?.id === tenantID &&
    currentUser.user.id === userID,
  );
}

export function useUpdateCalendarDisplayPreference(
  tenantID: string | undefined,
  userID: string | undefined,
) {
  const queryClient = useQueryClient();

  interface MutationVariables extends CalendarPrincipalSnapshot {
    draft: CalendarDisplayPreferenceDraft;
  }

  interface MutationResult extends Omit<MutationVariables, "draft"> {
    preference: CalendarDisplayPreferenceViewModel;
  }

  const mutation = useMutation<MutationResult, Error, MutationVariables>({
    mutationFn: async ({ draft, generation, tenantID, userID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const preference = await updateCalendarDisplayPreference(
        tenantID,
        preferenceRequest(draft),
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      ).then(mapCalendarPreference);
      return { generation, preference, tenantID, userID };
    },
    onSuccess: async (result) => {
      if (!isCurrentCalendarPrincipal(queryClient, result)) {
        queryClient.removeQueries({
          queryKey: calendarQueryKeys.principal(result.tenantID, result.userID),
        });
        return;
      }
      queryClient.setQueryData(
        calendarQueryKeys.preference(result.tenantID, result.userID),
        result.preference,
      );
      await invalidateCalendarItems(
        queryClient,
        result.tenantID,
        result.userID,
      );
    },
    onSettled: (_result, error, variables) => {
      if (!error) {
        return Promise.resolve();
      }
      if (!isCurrentCalendarPrincipal(queryClient, variables)) {
        queryClient.removeQueries({
          queryKey: calendarQueryKeys.principal(
            variables.tenantID,
            variables.userID,
          ),
        });
        return Promise.resolve();
      }
      return queryClient.invalidateQueries({
        exact: true,
        queryKey: calendarQueryKeys.preference(
          variables.tenantID,
          variables.userID,
        ),
      });
    },
    retry: false,
  });

  return {
    ...mutation,
    data: mutation.data?.preference,
    save: async (draft: CalendarDisplayPreferenceDraft) => {
      if (!tenantID || !userID) {
        throw new Error("Calendar preference principal is unavailable.");
      }
      const result = await mutation.mutateAsync({
        draft,
        generation: currentPrincipalGeneration(queryClient),
        tenantID,
        userID,
      });
      return result.preference;
    },
  };
}

export function useUpdateCalendarWorkingSchedule(
  tenantID: string | undefined,
  userID: string | undefined,
) {
  const queryClient = useQueryClient();

  interface MutationVariables extends CalendarPrincipalSnapshot {
    input: UpdateCalendarWorkingScheduleRequest;
  }

  interface MutationResult extends Omit<MutationVariables, "input"> {
    schedule: CalendarWorkingSchedule;
  }

  const mutation = useMutation<MutationResult, Error, MutationVariables>({
    mutationFn: async ({ generation, input, tenantID, userID }) => {
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const schedule = await updateCalendarWorkingSchedule(
        tenantID,
        input,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
      return { generation, schedule, tenantID, userID };
    },
    onSuccess: (result) => {
      if (!isCurrentCalendarPrincipal(queryClient, result)) {
        queryClient.removeQueries({
          queryKey: calendarQueryKeys.principal(result.tenantID, result.userID),
        });
        return;
      }
      queryClient.setQueryData(
        calendarQueryKeys.workingSchedule(result.tenantID, result.userID),
        result.schedule,
      );
    },
    onSettled: (_result, error, variables) => {
      if (!error) {
        return Promise.resolve();
      }
      if (!isCurrentCalendarPrincipal(queryClient, variables)) {
        queryClient.removeQueries({
          queryKey: calendarQueryKeys.principal(
            variables.tenantID,
            variables.userID,
          ),
        });
        return Promise.resolve();
      }
      return queryClient.invalidateQueries({
        exact: true,
        queryKey: calendarQueryKeys.workingSchedule(
          variables.tenantID,
          variables.userID,
        ),
      });
    },
    retry: false,
  });

  return {
    ...mutation,
    data: mutation.data?.schedule,
    save: async (input: UpdateCalendarWorkingScheduleRequest) => {
      if (!tenantID || !userID) {
        throw new Error("Calendar working-schedule principal is unavailable.");
      }
      const result = await mutation.mutateAsync({
        generation: currentPrincipalGeneration(queryClient),
        input,
        tenantID,
        userID,
      });
      return result.schedule;
    },
  };
}

export function useCalendarAvailabilityQuery(tenantID: string | undefined) {
  return useMutation<
    CalendarAvailabilityQueryResponse,
    Error,
    CalendarAvailabilityQueryRequest
  >({
    mutationFn: async (input) => {
      if (!tenantID) {
        throw new Error("Calendar availability tenant is unavailable.");
      }
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return queryCalendarAvailability(tenantID, input, csrf.csrf_token, {
        baseUrl: getApiBaseUrl(),
      });
    },
    retry: false,
  });
}
