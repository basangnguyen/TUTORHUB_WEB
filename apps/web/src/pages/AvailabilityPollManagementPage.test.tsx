import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { CurrentUser } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import type {
  AvailabilityPoll,
  AvailabilityPollSummary,
} from "../app/availabilityPollManagement";
import { availableTenantCapabilities } from "../test/tenantCapabilities";
import { AvailabilityPollManagementPage } from "./AvailabilityPollManagementPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const pollID = "2f1df9c5-85f6-43a4-96a1-0f78660dd08a";
const publicID = "8818c018-b6c5-4f44-a844-7cbec84a986d";
const slotID = "7d84f838-e788-4ae1-894a-a02984f58826";

const currentUser: CurrentUser = {
  user: {
    id: userID,
    email: "student@example.com",
    display_name: "Student Owner",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "poll-test",
    name: "Poll Test",
    role: "student",
    status: "active",
    version: 1,
    is_active: true,
  },
  memberships: [],
  permissions: [
    "tenant.view",
    "availability.poll.create",
    "availability.poll.manage_own",
    "study_meeting.schedule_own",
  ],
};

const slot = {
  id: slotID,
  starts_at: "2030-08-05T02:00:00Z",
  ends_at: "2030-08-05T03:00:00Z",
  ordinal: 0,
} as const;

function pollFixture(
  status: AvailabilityPoll["status"] = "open",
): AvailabilityPoll {
  return {
    class_id: null,
    created_at: "2026-08-01T00:00:00Z",
    deadline_at: "2030-08-04T12:00:00Z",
    description: "Choose a time for our study session.",
    duration_minutes: 60,
    id: pollID,
    my_response: null,
    outcome: null,
    owner_user_id: userID,
    participants: [],
    public_id: publicID,
    range_end: "2030-08-05",
    range_start: "2030-08-05",
    share_mode: "anyone_with_link",
    slot_granularity_minutes: 30,
    slots: [slot],
    status,
    timezone: "UTC",
    title: "Project study session",
    updated_at: "2026-08-01T00:00:00Z",
    version: status === "closed" ? 2 : 1,
    viewer_capabilities: {
      can_finalize_class_session: false,
      can_finalize_study_meeting: true,
      can_manage: true,
      can_respond: true,
      can_share: true,
      can_view_exact_aggregate: true,
      can_view_individual_responses: false,
    },
    working_day_end: "17:00:00",
    working_day_start: "09:00:00",
  };
}

function summaryFixture(poll: AvailabilityPoll): AvailabilityPollSummary {
  return {
    poll_id: poll.id,
    poll_version: poll.version,
    ranked_slots: [
      {
        aggregate_bucket: null,
        available_count: 0,
        cohort_satisfied: true,
        preferred_count: poll.my_response ? 1 : 0,
        rank: 1,
        slot,
        unavailable_count: 0,
      },
    ],
    response_count: poll.my_response ? 1 : 0,
    status: poll.status,
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderPage(fetchMock: ReturnType<typeof vi.fn>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <AvailabilityPollManagementPage />
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function requestFrom(call: unknown[] | undefined) {
  return call?.[0] as Request;
}

function staticPollFetch(poll: AvailabilityPoll) {
  return vi.fn().mockImplementation((request: Request) => {
    const url = new URL(request.url);
    if (url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
      return Promise.resolve(
        jsonResponse(availableTenantCapabilities(tenantID)),
      );
    }
    if (
      request.method === "GET" &&
      url.pathname.endsWith("/calendar/availability-polls")
    ) {
      return Promise.resolve(jsonResponse({ polls: [poll] }));
    }
    if (
      request.method === "GET" &&
      url.pathname.endsWith(`/calendar/availability-polls/${pollID}`)
    ) {
      return Promise.resolve(jsonResponse(poll));
    }
    if (
      request.method === "GET" &&
      url.pathname.endsWith(`/calendar/availability-polls/${pollID}/summary`)
    ) {
      return Promise.resolve(jsonResponse(summaryFixture(poll)));
    }
    if (
      request.method === "GET" &&
      url.pathname.endsWith("/calendar/study-meetings")
    ) {
      return Promise.resolve(jsonResponse({ meetings: [] }));
    }
    return Promise.reject(new Error(`Unexpected request: ${request.url}`));
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AvailabilityPollManagementPage", () => {
  it("renders accessible empty, editor, and StudyMeeting states", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(
          jsonResponse(availableTenantCapabilities(tenantID)),
        );
      }
      if (url.pathname.endsWith("/calendar/availability-polls")) {
        return Promise.resolve(jsonResponse({ polls: [] }));
      }
      if (url.pathname.endsWith("/calendar/study-meetings")) {
        return Promise.resolve(jsonResponse({ meetings: [] }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "Availability polls" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "No availability polls yet" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", {
        name: "No study meetings scheduled",
      }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Poll title")).toBeRequired();
    expect(screen.getByLabelText("First date")).toHaveAttribute("type", "date");
    expect(
      screen.getByRole("button", { name: "Create draft poll" }),
    ).toBeEnabled();
  });

  it("supports labelled radio response controls and owner close with tenant-scoped CSRF", async () => {
    let poll = pollFixture("open");
    const mutationRequests: Request[] = [];
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return jsonResponse(availableTenantCapabilities(tenantID));
      }
      if (url.pathname.endsWith("/auth/csrf")) {
        return jsonResponse({ csrf_token: "csrf-poll-owner" });
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/calendar/availability-polls")
      ) {
        return jsonResponse({ polls: [poll] });
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}`)
      ) {
        return jsonResponse(poll);
      }
      if (
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}/summary`)
      ) {
        return jsonResponse(summaryFixture(poll));
      }
      if (url.pathname.endsWith("/calendar/study-meetings")) {
        return jsonResponse({ meetings: [] });
      }
      if (
        request.method === "PUT" &&
        url.pathname.endsWith(
          `/calendar/availability-polls/${pollID}/responses/me`,
        )
      ) {
        mutationRequests.push(request);
        const body = (await request.clone().json()) as {
          answers: readonly { slot_id: string; state: "preferred" }[];
        };
        poll = {
          ...poll,
          my_response: {
            answers: body.answers,
            submitted_at: "2026-08-01T01:00:00Z",
            version: 1,
          },
        };
        return jsonResponse({ poll, summary: summaryFixture(poll) });
      }
      if (
        request.method === "POST" &&
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}/close`)
      ) {
        mutationRequests.push(request);
        poll = {
          ...poll,
          status: "closed",
          updated_at: "2026-08-01T02:00:00Z",
          version: 2,
        };
        return jsonResponse(poll);
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPage(fetchMock);

    const preferred = await screen.findByRole("radio", { name: "Preferred" });
    expect(preferred).not.toBeChecked();
    fireEvent.click(preferred);
    expect(preferred).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Save my response" }));

    await waitFor(() => expect(mutationRequests).toHaveLength(1));
    const responseRequest = mutationRequests[0];
    expect(responseRequest?.headers.get("X-CSRF-Token")).toBe(
      "csrf-poll-owner",
    );
    expect(responseRequest?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    await expect(responseRequest?.clone().json()).resolves.toMatchObject({
      answers: [{ slot_id: slotID, state: "preferred" }],
      expected_response_version: 0,
    });

    fireEvent.click(await screen.findByRole("button", { name: "Close poll" }));
    await waitFor(() => expect(mutationRequests).toHaveLength(2));
    const closeRequest = mutationRequests[1];
    await expect(closeRequest?.clone().json()).resolves.toEqual({
      expected_version: 1,
    });
    expect(
      await screen.findByRole("button", { name: "Reopen poll" }),
    ).toBeInTheDocument();
  });

  it("shows privacy-safe individual response labels and textual slot states when authorized", async () => {
    const poll: AvailabilityPoll = {
      ...pollFixture("open"),
      viewer_capabilities: {
        ...pollFixture("open").viewer_capabilities,
        can_view_individual_responses: true,
      },
    };
    const responseRequests: Request[] = [];
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(
          jsonResponse(availableTenantCapabilities(tenantID)),
        );
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/calendar/availability-polls")
      ) {
        return Promise.resolve(jsonResponse({ polls: [poll] }));
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(
          `/calendar/availability-polls/${pollID}/responses`,
        )
      ) {
        responseRequests.push(request);
        return Promise.resolve(
          url.searchParams.get("cursor") === "next-page"
            ? jsonResponse({
                next_cursor: null,
                responses: [
                  {
                    actor_type: "external_session",
                    answers: [],
                    internal_user_id: null,
                    participant_id: null,
                    response_id: "5a700eb6-664e-405d-bc4a-eb97d8a685f4",
                    submitted_at: "2026-08-01T01:30:00Z",
                    version: 1,
                  },
                ],
              })
            : jsonResponse({
                next_cursor: "next-page",
                responses: [
                  {
                    actor_type: "internal_member",
                    answers: [{ slot_id: slotID, state: "preferred" }],
                    internal_user_id: userID,
                    participant_id: null,
                    response_id: "6bbff2bf-bf95-47f6-b2ea-1479d3e3df47",
                    submitted_at: "2026-08-01T01:00:00Z",
                    version: 1,
                  },
                  {
                    actor_type: "external_session",
                    answers: [{ slot_id: slotID, state: "unavailable" }],
                    internal_user_id: null,
                    participant_id: "ed1df03a-5c98-4581-87f1-27ad79956848",
                    response_id: "5910aa45-2271-4764-9296-4439863530c5",
                    submitted_at: "2026-08-01T01:15:00Z",
                    version: 1,
                  },
                ],
              }),
        );
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}`)
      ) {
        return Promise.resolve(jsonResponse(poll));
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}/summary`)
      ) {
        return Promise.resolve(jsonResponse(summaryFixture(poll)));
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/calendar/study-meetings")
      ) {
        return Promise.resolve(jsonResponse({ meetings: [] }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPage(fetchMock);

    const region = await screen.findByRole("region", {
      name: "Individual responses",
    });
    const scrollRegion = await within(region).findByRole("region", {
      name: "Individual response slot states",
    });
    expect(scrollRegion).toHaveAttribute("tabindex", "0");
    const table = within(region).getByRole("table", {
      name: "Individual response slot states",
    });
    let rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(3);
    expect(within(rows[1]!).getByText("Internal user 1")).toBeVisible();
    expect(within(rows[1]!).getByText("Preferred")).toBeVisible();
    expect(within(rows[2]!).getByText("Participant 2")).toBeVisible();
    expect(within(rows[2]!).getByText("Unavailable")).toBeVisible();

    expect(new URL(responseRequests[0]!.url).searchParams.get("limit")).toBe(
      "25",
    );
    fireEvent.click(
      within(region).getByRole("button", { name: "Load more responses" }),
    );
    await waitFor(() => expect(responseRequests).toHaveLength(2));
    expect(new URL(responseRequests[1]!.url).searchParams.get("cursor")).toBe(
      "next-page",
    );
    rows = within(table).getAllByRole("row");
    expect(rows).toHaveLength(4);
    expect(within(rows[3]!).getByText("Anonymous respondent 3")).toBeVisible();
    expect(within(rows[3]!).getByText("Not answered")).toBeVisible();
    expect(screen.queryByText(currentUser.user.email)).not.toBeInTheDocument();
  });

  it("conceals individual responses when the viewer capability is false", async () => {
    const poll = pollFixture("open");
    const fetchMock = staticPollFetch(poll);
    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", { name: poll.title }),
    ).toBeVisible();
    expect(
      screen.queryByRole("heading", { name: "Individual responses" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Internal user 1")).not.toBeInTheDocument();
    expect(
      fetchMock.mock.calls.some((call) =>
        new URL(requestFrom(call).url).pathname.endsWith(
          `/calendar/availability-polls/${pollID}/responses`,
        ),
      ),
    ).toBe(false);
  });

  it("shows a copy-once public link and finalizes one StudyMeeting outcome", async () => {
    let poll = pollFixture("closed");
    const mutationRequests: Request[] = [];
    const meeting = {
      cancelled_at: null,
      class_id: null,
      created_at: "2026-08-01T03:00:00Z",
      ends_at: slot.ends_at,
      id: "36364294-1e7e-4765-8ac0-7a05d7203e0b",
      owner_user_id: userID,
      source_poll_id: pollID,
      starts_at: slot.starts_at,
      status: "scheduled",
      timezone: "UTC",
      title: poll.title,
      updated_at: "2026-08-01T03:00:00Z",
      version: 1,
    } as const;
    let meetings: readonly (typeof meeting)[] = [];
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return jsonResponse(availableTenantCapabilities(tenantID));
      }
      if (url.pathname.endsWith("/auth/csrf")) {
        return jsonResponse({ csrf_token: "csrf-poll-finalize" });
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/calendar/availability-polls")
      ) {
        return jsonResponse({ polls: [poll] });
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}`)
      ) {
        return jsonResponse(poll);
      }
      if (
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}/summary`)
      ) {
        return jsonResponse(summaryFixture(poll));
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/calendar/study-meetings")
      ) {
        return jsonResponse({ meetings });
      }
      if (
        request.method === "POST" &&
        url.pathname.endsWith(
          `/calendar/availability-polls/${pollID}/capabilities`,
        )
      ) {
        mutationRequests.push(request);
        poll = { ...poll, version: 3 };
        return jsonResponse(
          {
            capability: {
              created_at: "2026-08-01T02:00:00Z",
              expires_at: "2026-08-08T02:00:00Z",
              id: "a8b90e63-a756-43de-91a7-b978bf5b667b",
              participant_id: null,
              poll_id: pollID,
              revoked_at: null,
              scope: "public_link",
            },
            raw_token: "v1.copy-once-secret",
            share_url: `https://example.test/availability/${publicID}#token=v1.copy-once-secret`,
          },
          201,
        );
      }
      if (
        request.method === "POST" &&
        url.pathname.endsWith(`/calendar/availability-polls/${pollID}/finalize`)
      ) {
        mutationRequests.push(request);
        meetings = [meeting];
        poll = {
          ...poll,
          outcome: { id: meeting.id, type: "study_meeting" },
          status: "finalized",
          version: 4,
        };
        return jsonResponse({ poll, summary: summaryFixture(poll) });
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderPage(fetchMock);

    fireEvent.click(
      await screen.findByRole("button", { name: "Create a secure link" }),
    );
    expect(
      await screen.findByDisplayValue(
        `https://example.test/availability/${publicID}#token=v1.copy-once-secret`,
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/shown once/i)).toBeInTheDocument();

    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Schedule selected time" }),
      ).toBeEnabled(),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Schedule selected time" }),
    );
    await waitFor(() => expect(mutationRequests).toHaveLength(2));

    const shareRequest = requestFrom(
      fetchMock.mock.calls.find((call) => {
        const request = requestFrom(call);
        return request.url.endsWith(
          `/availability-polls/${pollID}/capabilities`,
        );
      }),
    );
    await expect(shareRequest.clone().json()).resolves.toMatchObject({
      expected_version: 2,
      participant_id: null,
      scope: "public_link",
    });
    const finalizeRequest = mutationRequests[1];
    await expect(finalizeRequest?.clone().json()).resolves.toMatchObject({
      class_id: null,
      expected_version: 3,
      outcome_type: "study_meeting",
      slot_id: slotID,
    });
    expect(await screen.findAllByText(meeting.title)).toHaveLength(3);
  });

  it("conceals a stale forbidden workspace response", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)) {
        return Promise.resolve(
          jsonResponse(availableTenantCapabilities(tenantID)),
        );
      }
      return Promise.resolve(
        jsonResponse(
          { detail: "denied", status: 403, title: "Forbidden" },
          403,
        ),
      );
    });
    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", {
        name: "Availability polls are unavailable",
      }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Project study session")).not.toBeInTheDocument();
  });
});
