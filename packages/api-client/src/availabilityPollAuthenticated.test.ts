import { describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  cancelAvailabilityPoll,
  cancelStudyMeeting,
  closeAvailabilityPoll,
  createAvailabilityPoll,
  createAvailabilityPollCapability,
  createStudyMeeting,
  finalizeAvailabilityPoll,
  getAvailabilityPoll,
  getAvailabilityPollSummary,
  getStudyMeeting,
  listAvailabilityPollIndividualResponses,
  listAvailabilityPolls,
  listStudyMeetings,
  openAvailabilityPoll,
  reopenAvailabilityPoll,
  respondToAvailabilityPoll,
  revokeAvailabilityPollCapability,
  updateAvailabilityPoll,
  updateStudyMeeting,
} from "./index";
import type {
  AvailabilityPoll,
  AvailabilityPollCapability,
  AvailabilityPollSummary,
  CreateAvailabilityPollRequest,
  CreateStudyMeetingRequest,
  StudyMeeting,
  UpdateAvailabilityPollRequest,
  UpdateStudyMeetingRequest,
} from "./index";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const pollID = "8818c018-b6c5-4f44-a844-7cbec84a986d";
const slotID = "4d43ece2-f474-49bd-88e8-524340738bc8";
const capabilityID = "a5beb068-f7ec-47d5-a3ec-a82023d67c9a";
const meetingID = "440660f5-7c21-4826-a28c-b4c23d005ea7";
const csrfToken = "csrf-test-token";
const individualResponseID = "ed340c4a-11e2-4e52-a13e-2499458b37e3";

const poll: AvailabilityPoll = {
  class_id: null,
  created_at: "2030-08-01T00:00:00Z",
  deadline_at: "2030-08-04T12:00:00Z",
  description: "",
  duration_minutes: 60,
  id: pollID,
  my_response: null,
  outcome: null,
  owner_user_id: "ef26f99e-e8f7-4aeb-be47-f2fcd0e292ab",
  participants: [],
  public_id: "a7555891-aa2f-4f29-9b93-f5910420ab27",
  range_end: "2030-08-04",
  range_start: "2030-08-01",
  share_mode: "invited_only",
  slot_granularity_minutes: 30,
  slots: [
    {
      ends_at: "2030-08-02T02:00:00Z",
      id: slotID,
      ordinal: 0,
      starts_at: "2030-08-02T01:00:00Z",
    },
  ],
  status: "draft",
  timezone: "Asia/Ho_Chi_Minh",
  title: "Study session",
  updated_at: "2030-08-01T00:00:00Z",
  version: 1,
  viewer_capabilities: {
    can_finalize_class_session: false,
    can_finalize_study_meeting: true,
    can_manage: true,
    can_respond: true,
    can_share: true,
    can_view_exact_aggregate: true,
    can_view_individual_responses: true,
  },
  working_day_end: "18:00:00",
  working_day_start: "08:00:00",
};

const summary: AvailabilityPollSummary = {
  poll_id: pollID,
  poll_version: 1,
  ranked_slots: [],
  response_count: 0,
  status: "draft",
};

const capability: AvailabilityPollCapability = {
  created_at: "2030-08-01T00:00:00Z",
  expires_at: "2030-08-04T12:00:00Z",
  id: capabilityID,
  participant_id: null,
  poll_id: pollID,
  revoked_at: null,
  scope: "public_link",
};

const meeting: StudyMeeting = {
  cancelled_at: null,
  class_id: null,
  created_at: "2030-08-01T00:00:00Z",
  ends_at: "2030-08-02T02:00:00Z",
  id: meetingID,
  owner_user_id: poll.owner_user_id,
  source_poll_id: null,
  starts_at: "2030-08-02T01:00:00Z",
  status: "scheduled",
  timezone: "Asia/Ho_Chi_Minh",
  title: "Study session",
  updated_at: "2030-08-01T00:00:00Z",
  version: 1,
};

const createPollInput: CreateAvailabilityPollRequest = {
  class_id: null,
  deadline_at: poll.deadline_at,
  description: poll.description,
  duration_minutes: poll.duration_minutes,
  idempotency_key: "poll:create:test",
  participants: [],
  range_end: poll.range_end,
  range_start: poll.range_start,
  share_mode: poll.share_mode,
  slot_granularity_minutes: poll.slot_granularity_minutes,
  slots: [
    {
      ends_at: poll.slots[0]!.ends_at,
      starts_at: poll.slots[0]!.starts_at,
    },
  ],
  timezone: poll.timezone,
  title: poll.title,
  working_day_end: poll.working_day_end,
  working_day_start: poll.working_day_start,
};

const updatePollInput: UpdateAvailabilityPollRequest = {
  ...createPollInput,
  expected_version: poll.version,
};

const createMeetingInput: CreateStudyMeetingRequest = {
  class_id: null,
  ends_at: meeting.ends_at,
  idempotency_key: "meeting:create:test",
  starts_at: meeting.starts_at,
  timezone: meeting.timezone,
  title: meeting.title,
};

const updateMeetingInput: UpdateStudyMeetingRequest = {
  class_id: null,
  ends_at: meeting.ends_at,
  expected_version: meeting.version,
  starts_at: meeting.starts_at,
  timezone: meeting.timezone,
  title: "Updated study session",
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("authenticated availability poll and StudyMeeting API", () => {
  it("binds every request to the expected tenant and CSRF-protects mutations", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const path = new URL(request.url).pathname;
      if (path.endsWith("/summary")) {
        return Promise.resolve(jsonResponse(summary));
      }
      if (path.includes("/capabilities/") && path.endsWith("/revoke")) {
        return Promise.resolve(jsonResponse(capability));
      }
      if (path.endsWith("/capabilities")) {
        return Promise.resolve(
          jsonResponse({
            capability,
            raw_token: "v1.copy-once-token",
            share_url: `https://web.example.test/availability/${poll.public_id}#v1.copy-once-token`,
          }),
        );
      }
      if (path.endsWith("/responses/me") || path.endsWith("/finalize")) {
        return Promise.resolve(jsonResponse({ poll, summary }));
      }
      if (request.method === "GET" && path.endsWith("/responses")) {
        return Promise.resolve(
          jsonResponse({
            next_cursor: "thapir1_next",
            responses: [
              {
                actor_type: "internal_member",
                answers: [{ slot_id: slotID, state: "preferred" }],
                internal_user_id: poll.owner_user_id,
                participant_id: null,
                response_id: individualResponseID,
                submitted_at: "2030-08-01T01:00:00Z",
                version: 1,
              },
            ],
          }),
        );
      }
      if (path.includes("/study-meetings")) {
        if (request.method === "GET" && path.endsWith("/study-meetings")) {
          return Promise.resolve(jsonResponse({ meetings: [meeting] }));
        }
        return Promise.resolve(jsonResponse(meeting));
      }
      if (request.method === "GET" && path.endsWith("/availability-polls")) {
        return Promise.resolve(jsonResponse({ polls: [poll] }));
      }
      return Promise.resolve(jsonResponse(poll));
    });
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await listAvailabilityPolls(
      tenantID,
      { limit: 25, status: "open" },
      options,
    );
    await getAvailabilityPoll(tenantID, pollID, options);
    await createAvailabilityPoll(tenantID, createPollInput, csrfToken, options);
    await updateAvailabilityPoll(
      tenantID,
      pollID,
      updatePollInput,
      csrfToken,
      options,
    );
    await openAvailabilityPoll(
      tenantID,
      pollID,
      { expected_version: 1 },
      csrfToken,
      options,
    );
    await closeAvailabilityPoll(
      tenantID,
      pollID,
      { expected_version: 2 },
      csrfToken,
      options,
    );
    await reopenAvailabilityPoll(
      tenantID,
      pollID,
      { deadline_at: poll.deadline_at, expected_version: 3 },
      csrfToken,
      options,
    );
    await cancelAvailabilityPoll(
      tenantID,
      pollID,
      { expected_version: 4, reason: "No longer needed" },
      csrfToken,
      options,
    );
    await respondToAvailabilityPoll(
      tenantID,
      pollID,
      {
        answers: [{ slot_id: slotID, state: "preferred" }],
        expected_response_version: 0,
        idempotency_key: "poll:respond:test",
      },
      csrfToken,
      options,
    );
    const individualPage = await listAvailabilityPollIndividualResponses(
      tenantID,
      pollID,
      { cursor: "thapir1_current", limit: 17 },
      options,
    );
    expect(individualPage.responses[0]?.response_id).toBe(individualResponseID);
    await getAvailabilityPollSummary(tenantID, pollID, options);
    await createAvailabilityPollCapability(
      tenantID,
      pollID,
      {
        expected_version: 5,
        expires_at: capability.expires_at,
        participant_id: null,
        scope: "public_link",
      },
      csrfToken,
      options,
    );
    await revokeAvailabilityPollCapability(
      tenantID,
      pollID,
      capabilityID,
      { expected_version: 6, reason: "Rotated" },
      csrfToken,
      options,
    );
    await finalizeAvailabilityPoll(
      tenantID,
      pollID,
      {
        class_id: null,
        expected_version: 7,
        idempotency_key: "poll:finalize:test",
        outcome_type: "study_meeting",
        slot_id: slotID,
      },
      csrfToken,
      options,
    );
    await listStudyMeetings(
      tenantID,
      { from: "2030-08-01T00:00:00Z", limit: 25, to: "2030-09-01T00:00:00Z" },
      options,
    );
    await getStudyMeeting(tenantID, meetingID, options);
    await createStudyMeeting(tenantID, createMeetingInput, csrfToken, options);
    await updateStudyMeeting(
      tenantID,
      meetingID,
      updateMeetingInput,
      csrfToken,
      options,
    );
    await cancelStudyMeeting(
      tenantID,
      meetingID,
      { expected_version: 2, reason: "Rescheduled" },
      csrfToken,
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests).toHaveLength(19);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
      if (request.method === "GET") {
        expect(request.headers.get("X-CSRF-Token")).toBeNull();
      } else {
        expect(request.headers.get("X-CSRF-Token")).toBe(csrfToken);
      }
    }

    expect(new URL(requests[0]!.url).searchParams.get("status")).toBe("open");
    expect(new URL(requests[0]!.url).searchParams.get("limit")).toBe("25");
    const individualRequest = requests.find((request) =>
      new URL(request.url).pathname.endsWith("/responses"),
    );
    expect(new URL(individualRequest!.url).searchParams.get("cursor")).toBe(
      "thapir1_current",
    );
    expect(new URL(individualRequest!.url).searchParams.get("limit")).toBe(
      "17",
    );
    expect(
      requests.map(
        (request) => `${request.method} ${new URL(request.url).pathname}`,
      ),
    ).toContain(
      `POST /api/v1/calendar/availability-polls/${pollID}/capabilities/${capabilityID}/revoke`,
    );
    expect(
      requests.map(
        (request) => `${request.method} ${new URL(request.url).pathname}`,
      ),
    ).toContain(`PATCH /api/v1/calendar/study-meetings/${meetingID}`);
  });

  it("rejects an empty tenant before sending and maps problem responses", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          detail: "Poll not found.",
          status: 404,
          title: "Not found",
          type: "https://tutorhub.example/problems/not-found",
        },
        404,
      ),
    );
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await expect(listAvailabilityPolls("   ", {}, options)).rejects.toThrow(
      TypeError,
    );
    expect(fetchMock).not.toHaveBeenCalled();

    await expect(
      getAvailabilityPoll(tenantID, pollID, options),
    ).rejects.toMatchObject({
      name: "APIRequestError",
      status: 404,
      message: "Poll not found.",
    } satisfies Partial<APIRequestError>);
  });
});
