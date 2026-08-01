import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  createTutorHubClient,
  listAvailabilityPollIndividualResponses,
  rotateCSRFToken,
  type APIRequestOptions,
} from "@tutorhub/api-client";
import { Temporal } from "temporal-polyfill";
import type { components } from "../../../../packages/api-client/src/generated/schema";
import { invalidateTenantAudit } from "./audit";
import { resolveCivilDateTime } from "./classSessionTime";
import { invalidateTenantCapabilities } from "./tenantCapabilities";

export type AvailabilityPoll = components["schemas"]["AvailabilityPoll"];
export type AvailabilityPollAnswerInput =
  components["schemas"]["AvailabilityPollAnswerInput"];
export type AvailabilityPollAnswerState =
  components["schemas"]["AvailabilityPollAnswerState"];
export type AvailabilityPollCapabilitySecret =
  components["schemas"]["AvailabilityPollCapabilitySecret"];
export type AvailabilityPollListResponse =
  components["schemas"]["AvailabilityPollListResponse"];
export type AvailabilityPollIndividualResponse =
  components["schemas"]["AvailabilityPollIndividualResponse"];
export type AvailabilityPollIndividualResponsePage =
  components["schemas"]["AvailabilityPollIndividualResponsePage"];
export type AvailabilityPollMutationResponse =
  components["schemas"]["AvailabilityPollMutationResponse"];
export type AvailabilityPollStatus =
  components["schemas"]["AvailabilityPollStatus"];
export type AvailabilityPollSummary =
  components["schemas"]["AvailabilityPollSummary"];
export type CancelAvailabilityPollRequest =
  components["schemas"]["CancelAvailabilityPollRequest"];
export type CreateAvailabilityPollCapabilityRequest =
  components["schemas"]["CreateAvailabilityPollCapabilityRequest"];
export type CreateAvailabilityPollRequest =
  components["schemas"]["CreateAvailabilityPollRequest"];
export type CreateStudyMeetingRequest =
  components["schemas"]["CreateStudyMeetingRequest"];
export type FinalizeAvailabilityPollRequest =
  components["schemas"]["FinalizeAvailabilityPollRequest"];
export type ReopenAvailabilityPollRequest =
  components["schemas"]["ReopenAvailabilityPollRequest"];
export type RespondAvailabilityPollRequest =
  components["schemas"]["RespondAvailabilityPollRequest"];
export type StudyMeeting = components["schemas"]["StudyMeeting"];
export type StudyMeetingListResponse =
  components["schemas"]["StudyMeetingListResponse"];

export interface PollSlotGenerationInput {
  durationMinutes: number;
  granularityMinutes: 15 | 30 | 60;
  rangeEnd: string;
  rangeStart: string;
  timezone: string;
  workingEnd: string;
  workingStart: string;
}

export type PollSlotGenerationResult =
  | {
      error: null;
      slots: CreateAvailabilityPollRequest["slots"];
    }
  | {
      error:
        | "invalid_range"
        | "invalid_hours"
        | "invalid_timezone"
        | "no_slots"
        | "too_many_slots";
      slots: readonly [];
    };

function serializeInstant(value: Temporal.Instant) {
  return value.toString({ fractionalSecondDigits: "auto" });
}

export function generatePollSlots(
  input: PollSlotGenerationInput,
): PollSlotGenerationResult {
  let startDate: Temporal.PlainDate;
  let endDate: Temporal.PlainDate;
  let workingStart: Temporal.PlainTime;
  let workingEnd: Temporal.PlainTime;
  try {
    startDate = Temporal.PlainDate.from(input.rangeStart);
    endDate = Temporal.PlainDate.from(input.rangeEnd);
    workingStart = Temporal.PlainTime.from(input.workingStart);
    workingEnd = Temporal.PlainTime.from(input.workingEnd);
    Temporal.Now.instant().toZonedDateTimeISO(input.timezone);
  } catch {
    try {
      Temporal.PlainDate.from(input.rangeStart);
      Temporal.PlainDate.from(input.rangeEnd);
      Temporal.PlainTime.from(input.workingStart);
      Temporal.PlainTime.from(input.workingEnd);
    } catch {
      return { error: "invalid_range", slots: [] };
    }
    return { error: "invalid_timezone", slots: [] };
  }

  if (
    Temporal.PlainDate.compare(startDate, endDate) > 0 ||
    startDate.until(endDate, { largestUnit: "days" }).days + 1 > 90
  ) {
    return { error: "invalid_range", slots: [] };
  }
  if (
    Temporal.PlainTime.compare(workingStart, workingEnd) >= 0 ||
    input.durationMinutes < 15 ||
    input.durationMinutes > 480
  ) {
    return { error: "invalid_hours", slots: [] };
  }

  const slots: { starts_at: string; ends_at: string }[] = [];
  let date = startDate;
  while (Temporal.PlainDate.compare(date, endDate) <= 0) {
    let localStart = date.toPlainDateTime(workingStart);
    const localBoundary = date.toPlainDateTime(workingEnd);
    while (Temporal.PlainDateTime.compare(localStart, localBoundary) < 0) {
      const resolved = resolveCivilDateTime(
        localStart.toString({ smallestUnit: "minute" }),
        input.timezone,
        "earlier",
      );
      if (resolved.kind === "resolved") {
        const startInstant = Temporal.Instant.from(resolved.value);
        const endInstant = startInstant.add({ minutes: input.durationMinutes });
        const localEnd = endInstant.toZonedDateTimeISO(input.timezone);
        if (
          localEnd.toPlainDate().equals(date) &&
          Temporal.PlainDateTime.compare(
            localEnd.toPlainDateTime(),
            localStart,
          ) > 0 &&
          Temporal.PlainDateTime.compare(
            localEnd.toPlainDateTime(),
            localBoundary,
          ) <= 0
        ) {
          slots.push({
            ends_at: serializeInstant(endInstant),
            starts_at: serializeInstant(startInstant),
          });
          if (slots.length > 1000) {
            return { error: "too_many_slots", slots: [] };
          }
        }
      }
      localStart = localStart.add({ minutes: input.granularityMinutes });
    }
    date = date.add({ days: 1 });
  }
  return slots.length > 0
    ? { error: null, slots }
    : { error: "no_slots", slots: [] };
}

const pollListLimit = 100;
const meetingListLimit = 100;
const individualResponsePageSize = 25;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

function requireTenantID(tenantID: string) {
  if (!tenantID.trim()) {
    throw new Error("An active tenant is required for calendar requests.");
  }
}

function requireData<T>(data: T | undefined, response: Response): T {
  if (!response.ok || data === undefined) {
    throw new APIRequestError(response.status);
  }
  return data;
}

function shouldRetryQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

async function csrfToken() {
  return (
    await rotateCSRFToken({
      baseUrl: getApiBaseUrl(),
    })
  ).csrf_token;
}

function queryOptions(options: APIRequestOptions = {}) {
  return {
    baseUrl: getApiBaseUrl(),
    signal: options.signal,
  } satisfies APIRequestOptions;
}

export const availabilityPollManagementQueryKeys = {
  tenant: (tenantID: string) =>
    ["availability-poll-management", tenantID] as const,
  polls: (tenantID: string) =>
    ["availability-poll-management", tenantID, "polls"] as const,
  poll: (tenantID: string, pollID: string) =>
    ["availability-poll-management", tenantID, "polls", pollID] as const,
  summary: (tenantID: string, pollID: string) =>
    [
      "availability-poll-management",
      tenantID,
      "polls",
      pollID,
      "summary",
    ] as const,
  responses: (tenantID: string, pollID: string) =>
    [
      "availability-poll-management",
      tenantID,
      "polls",
      pollID,
      "responses",
    ] as const,
  meetings: (tenantID: string) =>
    ["availability-poll-management", tenantID, "study-meetings"] as const,
};

export async function listAvailabilityPollsRequest(
  tenantID: string,
  options: APIRequestOptions = {},
) {
  requireTenantID(tenantID);
  const { data, response } = await createTutorHubClient(
    queryOptions(options),
  ).GET("/api/v1/calendar/availability-polls", {
    params: {
      header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      query: { limit: pollListLimit },
    },
    headers: { Accept: "application/json" },
    signal: options.signal,
  });
  return requireData(data, response) as AvailabilityPollListResponse;
}

export async function getAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  options: APIRequestOptions = {},
) {
  requireTenantID(tenantID);
  const { data, response } = await createTutorHubClient(
    queryOptions(options),
  ).GET("/api/v1/calendar/availability-polls/{poll_id}", {
    params: {
      header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      path: { poll_id: pollID },
    },
    headers: { Accept: "application/json" },
    signal: options.signal,
  });
  return requireData(data, response) as AvailabilityPoll;
}

export async function createAvailabilityPollRequest(
  tenantID: string,
  input: CreateAvailabilityPollRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPoll;
}

async function openAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  expectedVersion: number,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls/{poll_id}/open", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: { expected_version: expectedVersion },
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPoll;
}

async function closeAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  expectedVersion: number,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls/{poll_id}/close", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: { expected_version: expectedVersion },
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPoll;
}

async function reopenAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  input: ReopenAvailabilityPollRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls/{poll_id}/reopen", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPoll;
}

async function cancelAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  input: CancelAvailabilityPollRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls/{poll_id}/cancel", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPoll;
}

export async function respondAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  input: RespondAvailabilityPollRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).PUT("/api/v1/calendar/availability-polls/{poll_id}/responses/me", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPollMutationResponse;
}

export async function listAvailabilityPollIndividualResponsesRequest(
  tenantID: string,
  pollID: string,
  cursor: string | undefined,
  options: APIRequestOptions = {},
) {
  requireTenantID(tenantID);
  return listAvailabilityPollIndividualResponses(
    tenantID,
    pollID,
    {
      cursor,
      limit: individualResponsePageSize,
    },
    queryOptions(options),
  );
}

export async function getAvailabilityPollSummaryRequest(
  tenantID: string,
  pollID: string,
  options: APIRequestOptions = {},
) {
  requireTenantID(tenantID);
  const { data, response } = await createTutorHubClient(
    queryOptions(options),
  ).GET("/api/v1/calendar/availability-polls/{poll_id}/summary", {
    params: {
      header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      path: { poll_id: pollID },
    },
    headers: { Accept: "application/json" },
    signal: options.signal,
  });
  return requireData(data, response) as AvailabilityPollSummary;
}

export async function finalizeAvailabilityPollRequest(
  tenantID: string,
  pollID: string,
  input: FinalizeAvailabilityPollRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls/{poll_id}/finalize", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPollMutationResponse;
}

export async function createAvailabilityPollCapabilityRequest(
  tenantID: string,
  pollID: string,
  input: CreateAvailabilityPollCapabilityRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/availability-polls/{poll_id}/capabilities", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { poll_id: pollID },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as AvailabilityPollCapabilitySecret;
}

export async function listStudyMeetingsRequest(
  tenantID: string,
  options: APIRequestOptions = {},
) {
  requireTenantID(tenantID);
  const { data, response } = await createTutorHubClient(
    queryOptions(options),
  ).GET("/api/v1/calendar/study-meetings", {
    params: {
      header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      query: { limit: meetingListLimit },
    },
    headers: { Accept: "application/json" },
    signal: options.signal,
  });
  return requireData(data, response) as StudyMeetingListResponse;
}

export async function createStudyMeetingRequest(
  tenantID: string,
  input: CreateStudyMeetingRequest,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/study-meetings", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
    },
    body: input,
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as StudyMeeting;
}

export async function cancelStudyMeetingRequest(
  tenantID: string,
  meetingID: string,
  expectedVersion: number,
  reason: string,
) {
  requireTenantID(tenantID);
  const csrf = await csrfToken();
  const { data, response } = await createTutorHubClient({
    baseUrl: getApiBaseUrl(),
  }).POST("/api/v1/calendar/study-meetings/{meeting_id}/cancel", {
    params: {
      header: {
        "X-CSRF-Token": csrf,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
      path: { meeting_id: meetingID },
    },
    body: { expected_version: expectedVersion, reason },
    headers: { Accept: "application/json" },
  });
  return requireData(data, response) as StudyMeeting;
}

async function synchronizePoll(
  queryClient: QueryClient,
  tenantID: string,
  poll: AvailabilityPoll,
) {
  queryClient.setQueryData(
    availabilityPollManagementQueryKeys.poll(tenantID, poll.id),
    poll,
  );
  await Promise.all([
    queryClient.invalidateQueries({
      queryKey: availabilityPollManagementQueryKeys.polls(tenantID),
    }),
    queryClient.invalidateQueries({
      queryKey: availabilityPollManagementQueryKeys.summary(tenantID, poll.id),
    }),
    queryClient.invalidateQueries({
      queryKey: availabilityPollManagementQueryKeys.responses(
        tenantID,
        poll.id,
      ),
    }),
  ]);
}

async function invalidateMutationSideEffects(
  queryClient: QueryClient,
  tenantID: string | undefined,
) {
  await Promise.all([
    invalidateTenantAudit(queryClient, tenantID),
    invalidateTenantCapabilities(queryClient, tenantID),
  ]);
}

export function useAvailabilityPollList(tenantID: string | undefined) {
  return useQuery({
    queryKey: availabilityPollManagementQueryKeys.polls(tenantID ?? "inactive"),
    queryFn: ({ signal }) =>
      listAvailabilityPollsRequest(tenantID ?? "", { signal }),
    enabled: Boolean(tenantID),
    retry: shouldRetryQuery,
    staleTime: 15_000,
  });
}

export function useAvailabilityPollDetail(
  tenantID: string | undefined,
  pollID: string | undefined,
) {
  return useQuery({
    queryKey: availabilityPollManagementQueryKeys.poll(
      tenantID ?? "inactive",
      pollID ?? "unselected",
    ),
    queryFn: ({ signal }) =>
      getAvailabilityPollRequest(tenantID ?? "", pollID ?? "", { signal }),
    enabled: Boolean(tenantID && pollID),
    retry: shouldRetryQuery,
    staleTime: 10_000,
  });
}

export function useAvailabilityPollSummary(
  tenantID: string | undefined,
  pollID: string | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: availabilityPollManagementQueryKeys.summary(
      tenantID ?? "inactive",
      pollID ?? "unselected",
    ),
    queryFn: ({ signal }) =>
      getAvailabilityPollSummaryRequest(tenantID ?? "", pollID ?? "", {
        signal,
      }),
    enabled: enabled && Boolean(tenantID && pollID),
    retry: shouldRetryQuery,
    staleTime: 10_000,
  });
}

export function useAvailabilityPollIndividualResponses(
  tenantID: string | undefined,
  pollID: string | undefined,
  enabled: boolean,
) {
  return useInfiniteQuery({
    queryKey: availabilityPollManagementQueryKeys.responses(
      tenantID ?? "inactive",
      pollID ?? "unselected",
    ),
    queryFn: ({ pageParam, signal }) =>
      listAvailabilityPollIndividualResponsesRequest(
        tenantID ?? "",
        pollID ?? "",
        pageParam,
        { signal },
      ),
    enabled: enabled && Boolean(tenantID && pollID),
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryQuery,
    staleTime: 10_000,
  });
}

export function useStudyMeetingList(tenantID: string | undefined) {
  return useQuery({
    queryKey: availabilityPollManagementQueryKeys.meetings(
      tenantID ?? "inactive",
    ),
    queryFn: ({ signal }) =>
      listStudyMeetingsRequest(tenantID ?? "", { signal }),
    enabled: Boolean(tenantID),
    retry: shouldRetryQuery,
    staleTime: 15_000,
  });
}

export function useCreateAvailabilityPoll(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateAvailabilityPollRequest) =>
      createAvailabilityPollRequest(tenantID ?? "", input),
    onSuccess: (poll) =>
      tenantID
        ? synchronizePoll(queryClient, tenantID, poll)
        : Promise.resolve(),
    onSettled: () => invalidateMutationSideEffects(queryClient, tenantID),
    retry: false,
  });
}

export type AvailabilityPollLifecycleAction =
  | { kind: "open"; expectedVersion: number }
  | { kind: "close"; expectedVersion: number }
  | {
      kind: "reopen";
      expectedVersion: number;
      deadlineAt: string;
    }
  | { kind: "cancel"; expectedVersion: number; reason: string };

export function useAvailabilityPollLifecycle(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      action,
      pollID,
    }: {
      action: AvailabilityPollLifecycleAction;
      pollID: string;
    }) => {
      const activeTenantID = tenantID ?? "";
      requireTenantID(activeTenantID);
      switch (action.kind) {
        case "open":
          return openAvailabilityPollRequest(
            activeTenantID,
            pollID,
            action.expectedVersion,
          );
        case "close":
          return closeAvailabilityPollRequest(
            activeTenantID,
            pollID,
            action.expectedVersion,
          );
        case "reopen":
          return reopenAvailabilityPollRequest(activeTenantID, pollID, {
            deadline_at: action.deadlineAt,
            expected_version: action.expectedVersion,
          });
        case "cancel":
          return cancelAvailabilityPollRequest(activeTenantID, pollID, {
            expected_version: action.expectedVersion,
            reason: action.reason,
          });
      }
    },
    onSuccess: (poll) =>
      tenantID
        ? synchronizePoll(queryClient, tenantID, poll)
        : Promise.resolve(),
    onSettled: () => invalidateMutationSideEffects(queryClient, tenantID),
    retry: false,
  });
}

export function useRespondAvailabilityPoll(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      input,
      pollID,
    }: {
      input: RespondAvailabilityPollRequest;
      pollID: string;
    }) => respondAvailabilityPollRequest(tenantID ?? "", pollID, input),
    onSuccess: (result: AvailabilityPollMutationResponse) =>
      tenantID
        ? synchronizePoll(queryClient, tenantID, result.poll)
        : Promise.resolve(),
    onSettled: () => invalidateTenantAudit(queryClient, tenantID),
    retry: false,
  });
}

export function useFinalizeAvailabilityPoll(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      input,
      pollID,
    }: {
      input: FinalizeAvailabilityPollRequest;
      pollID: string;
    }) => finalizeAvailabilityPollRequest(tenantID ?? "", pollID, input),
    onSuccess: async (result: AvailabilityPollMutationResponse) => {
      if (!tenantID) return;
      await Promise.all([
        synchronizePoll(queryClient, tenantID, result.poll),
        queryClient.invalidateQueries({
          queryKey: availabilityPollManagementQueryKeys.meetings(tenantID),
        }),
      ]);
    },
    onSettled: () => invalidateMutationSideEffects(queryClient, tenantID),
    retry: false,
  });
}

export function useCreateAvailabilityPollCapability(
  tenantID: string | undefined,
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      input,
      pollID,
    }: {
      input: CreateAvailabilityPollCapabilityRequest;
      pollID: string;
    }) =>
      createAvailabilityPollCapabilityRequest(tenantID ?? "", pollID, input),
    onSuccess: async (secret: AvailabilityPollCapabilitySecret) => {
      if (!tenantID) return;
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: availabilityPollManagementQueryKeys.poll(
            tenantID,
            secret.capability.poll_id,
          ),
        }),
        queryClient.invalidateQueries({
          queryKey: availabilityPollManagementQueryKeys.polls(tenantID),
        }),
      ]);
    },
    onSettled: () => invalidateMutationSideEffects(queryClient, tenantID),
    retry: false,
  });
}

export function useCreateStudyMeeting(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateStudyMeetingRequest) =>
      createStudyMeetingRequest(tenantID ?? "", input),
    onSuccess: () =>
      tenantID
        ? queryClient.invalidateQueries({
            queryKey: availabilityPollManagementQueryKeys.meetings(tenantID),
          })
        : Promise.resolve(),
    onSettled: () => invalidateMutationSideEffects(queryClient, tenantID),
    retry: false,
  });
}

export function useCancelStudyMeeting(tenantID: string | undefined) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      meetingID,
      reason,
      version,
    }: {
      meetingID: string;
      reason: string;
      version: number;
    }) => cancelStudyMeetingRequest(tenantID ?? "", meetingID, version, reason),
    onSuccess: () =>
      tenantID
        ? queryClient.invalidateQueries({
            queryKey: availabilityPollManagementQueryKeys.meetings(tenantID),
          })
        : Promise.resolve(),
    onSettled: () => invalidateTenantAudit(queryClient, tenantID),
    retry: false,
  });
}
