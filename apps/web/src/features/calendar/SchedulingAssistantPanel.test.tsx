import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type {
  CalendarAvailabilityQueryResponse,
  SessionAudience,
} from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import { SchedulingAssistantPanel } from "./SchedulingAssistantPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const optionalUserID = "80113acb-9865-46aa-a9bf-d7bde155208a";

const managerAudience: SessionAudience = {
  attendees: [
    {
      business_role: "organizer",
      id: "bcd16c31-c8b9-4c2f-a0e9-d87e4220d32a",
      is_self: true,
      participation_role: "required",
      responded_at: null,
      rsvp_state: "needs_action",
      user_id: userID,
      version: 2,
    },
    {
      business_role: "student",
      id: "85e4d84c-cf88-45b7-845c-6dd1b777a311",
      is_self: false,
      participation_role: "optional",
      responded_at: "2026-07-29T04:00:00Z",
      rsvp_state: "accepted",
      user_id: optionalUserID,
      version: 5,
    },
  ],
  audience_revision: 8,
  response_requested: true,
  viewer_access: {
    can_manage_attendees: true,
    can_respond: true,
    can_see_guest_list: true,
  },
};

const availabilityResponse: CalendarAvailabilityQueryResponse = {
  empty_suggestions_reason: null,
  participants: [
    {
      intervals: [
        {
          ends_at: "2026-07-30T03:00:00Z",
          starts_at: "2026-07-30T00:00:00Z",
          status: "unknown",
        },
      ],
      participant: { id: userID, kind: "internal_user" },
      role: "required",
      working_intervals: [
        {
          ends_at: "2026-07-30T10:00:00Z",
          starts_at: "2026-07-30T02:00:00Z",
        },
      ],
    },
    {
      intervals: [
        {
          ends_at: "2026-07-30T03:00:00Z",
          starts_at: "2026-07-30T00:00:00Z",
          status: "busy",
        },
      ],
      participant: { id: optionalUserID, kind: "internal_user" },
      role: "optional",
      working_intervals: [],
    },
  ],
  suggestions: [
    {
      ends_at: "2026-07-30T03:00:00Z",
      reason_breakdown: {
        optional_busy: 1,
        optional_out_of_office: 0,
        optional_outside_working: 0,
        optional_tentative: 0,
        optional_unknown: 0,
        required_busy: 0,
        required_out_of_office: 0,
        required_outside_working: 0,
        required_tentative: 0,
        required_unknown: 1,
      },
      stable_slot_key: "slot-1",
      starts_at: "2026-07-30T02:00:00Z",
    },
  ],
  timezone: "Asia/Ho_Chi_Minh",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderAssistant(
  audience: SessionAudience,
  fetchMock: ReturnType<typeof vi.fn>,
  onUseSuggestion = vi.fn(),
) {
  vi.stubGlobal("fetch", fetchMock);
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SchedulingAssistantPanel
          audience={audience}
          classID={classID}
          endsAt="2026-07-30T03:00:00Z"
          hourCycle="h23"
          locale="en-US"
          onUseSuggestion={onUseSuggestion}
          primaryTimezone="Asia/Ho_Chi_Minh"
          secondaryTimezone="Europe/London"
          startsAt="2026-07-30T02:00:00Z"
          tenantID={tenantID}
          userID={userID}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onUseSuggestion, queryClient };
}

describe("SchedulingAssistantPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("submits internal audience roles and presents accessible dual-timezone reasons", async () => {
    let requestBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "availability-csrf" });
      }
      if (url.pathname.endsWith("/api/v1/calendar/availability/query")) {
        requestBody = await request.clone().json();
        expect(request.headers.get("X-CSRF-Token")).toBe("availability-csrf");
        expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
          tenantID,
        );
        return jsonResponse(availabilityResponse);
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });
    const { onUseSuggestion } = renderAssistant(managerAudience, fetchMock);

    expect(
      screen.getByRole("form", { name: "Find-a-time options" }),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "Find available times" }),
    );

    const suggestionTable = await screen.findByRole("table", {
      name: "Suggested times",
    });
    expect(requestBody).toMatchObject({
      class_id: classID,
      duration_minutes: 60,
      max_candidates: 10,
      optional: [{ id: optionalUserID, kind: "internal_user" }],
      required: [{ id: userID, kind: "internal_user" }],
      step_minutes: 30,
      timezone: "Asia/Ho_Chi_Minh",
    });
    expect(
      within(suggestionTable).getByText("Required unknown: 1"),
    ).toBeVisible();
    expect(within(suggestionTable).getByText("Optional busy: 1")).toBeVisible();
    expect(within(suggestionTable).getByText("Asia/Ho_Chi_Minh")).toBeVisible();
    expect(within(suggestionTable).getByText("Europe/London")).toBeVisible();

    fireEvent.click(screen.getByText("Availability details for 2 attendees"));
    const participantTable = screen.getByRole("table", {
      name: "Availability status and working hours by attendee",
    });
    expect(within(participantTable).getByText("Unknown: 1")).toBeVisible();
    expect(
      within(participantTable).getByText("1 working-hours intervals"),
    ).toBeVisible();
    expect(
      within(participantTable).getByText(
        "No working hours in this search range",
      ),
    ).toBeVisible();

    fireEvent.click(
      within(suggestionTable).getByRole("button", {
        name: "Use this time",
      }),
    );
    expect(onUseSuggestion).toHaveBeenCalledWith(
      "2026-07-30T02:00:00Z",
      "2026-07-30T03:00:00Z",
    );
  });

  it("is absent when the server does not grant attendee management", () => {
    const fetchMock = vi.fn();
    renderAssistant(
      {
        ...managerAudience,
        viewer_access: {
          ...managerAudience.viewer_access,
          can_manage_attendees: false,
        },
      },
      fetchMock,
    );

    expect(
      screen.queryByRole("heading", { name: "Scheduling Assistant" }),
    ).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("supports retry and an explicit no-suggestions state", async () => {
    let availabilityCalls = 0;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "availability-csrf" });
      }
      if (url.pathname.endsWith("/api/v1/calendar/availability/query")) {
        availabilityCalls += 1;
        if (availabilityCalls === 1) {
          return jsonResponse(
            {
              detail: "Availability timed out.",
              status: 503,
              title: "Unavailable",
              type: "https://tutorhub.test/problems/unavailable",
            },
            503,
          );
        }
        return jsonResponse({
          ...availabilityResponse,
          empty_suggestions_reason: "no_valid_civil_slots",
          suggestions: [],
        });
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });
    renderAssistant(managerAudience, fetchMock);

    fireEvent.click(
      screen.getByRole("button", { name: "Find available times" }),
    );
    const alert = await screen.findByRole("alert");
    expect(
      within(alert).getByText(
        "Free/busy availability could not be checked. Try again.",
      ),
    ).toBeVisible();
    fireEvent.click(within(alert).getByRole("button", { name: "Try again" }));

    expect(await screen.findByText("No valid times found")).toBeVisible();
    await waitFor(() => expect(availabilityCalls).toBe(2));
  });
});
