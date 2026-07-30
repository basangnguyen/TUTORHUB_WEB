import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ClassRosterPage, SessionAudience } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import type { CalendarItemViewModel } from "./model";
import { SessionParticipationPanel } from "./SessionParticipationPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const sessionID = "3042cad1-d582-4c59-b821-eb599b27ebf7";
const seriesID = "4d7cd279-452e-48f8-913b-7897f71785a7";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const studentID = "80113acb-9865-46aa-a9bf-d7bde155208a";

const item: CalendarItemViewModel = {
  allDay: false,
  canCancel: false,
  canEdit: false,
  canReschedule: false,
  canView: true,
  classID,
  classTitle: "Advanced Mathematics",
  colorToken: "class-accent-1",
  displayTimezone: "Asia/Ho_Chi_Minh",
  endsAt: "2026-07-27T03:00:00Z",
  id: `class-session:${sessionID}:2026-07-27T02:00:00Z`,
  occurrenceKey: "2026-07-27T02:00:00Z",
  sourceID: sessionID,
  seriesID: null,
  sourceType: "class_session",
  startsAt: "2026-07-27T02:00:00Z",
  status: "scheduled",
  title: "Linear algebra",
  version: 3,
};

const selfAttendee = {
  business_role: "student" as const,
  id: "bcd16c31-c8b9-4c2f-a0e9-d87e4220d32a",
  is_self: true,
  participation_role: "required" as const,
  responded_at: null,
  rsvp_state: "needs_action" as const,
  user_id: userID,
  version: 2,
};

const selfAudience: SessionAudience = {
  attendees: [selfAttendee],
  audience_revision: 4,
  external_attendees: [],
  response_requested: true,
  viewer_access: {
    can_manage_attendees: false,
    can_respond: true,
    can_see_guest_list: false,
  },
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

function renderPanel(
  fetchMock: ReturnType<typeof vi.fn>,
  renderedItem: CalendarItemViewModel = item,
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
        <SessionParticipationPanel
          hourCycle="h23"
          item={renderedItem}
          locale="en-US"
          secondaryTimezone={null}
          tenantID={tenantID}
          userID={userID}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

describe("SessionParticipationPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("keeps an ordinary attendee private and submits a versioned RSVP", async () => {
    let audienceReads = 0;
    let currentAttendee: SessionAudience["attendees"][number] = selfAttendee;
    let responseBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${sessionID}/attendees`,
        )
      ) {
        audienceReads += 1;
        return jsonResponse({ ...selfAudience, attendees: [currentAttendee] });
      }
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "participation-csrf" });
      }
      if (
        request.method === "POST" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${sessionID}/responses`,
        )
      ) {
        responseBody = await request.clone().json();
        expect(request.headers.get("X-CSRF-Token")).toBe("participation-csrf");
        expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
          tenantID,
        );
        currentAttendee = {
          ...selfAttendee,
          responded_at: "2026-07-29T04:00:00Z",
          rsvp_state: "tentative",
          version: 3,
        };
        return jsonResponse({
          attendee: currentAttendee,
          replayed: false,
        });
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });

    renderPanel(fetchMock);

    expect(await screen.findByText("Your response")).toBeVisible();
    expect(screen.queryByText(/internal attendees/iu)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("list", { name: "Internal attendees" }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Tentative" }));

    expect(await screen.findByText("Response saved.")).toBeVisible();
    expect(responseBody).toMatchObject({
      expected_attendee_version: 2,
      state: "tentative",
    });
    expect(responseBody).toMatchObject({
      idempotency_key: expect.stringMatching(/^rsvp:/u),
    });
    await waitFor(() => expect(audienceReads).toBe(2));
    expect(screen.getByRole("button", { name: "Tentative" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("shows a privacy-safe manager summary without delivery data", async () => {
    const managerAudience: SessionAudience = {
      attendees: [
        { ...selfAttendee, business_role: "organizer" },
        {
          ...selfAttendee,
          business_role: "student",
          id: "85e4d84c-cf88-45b7-845c-6dd1b777a311",
          is_self: false,
          participation_role: "optional",
          rsvp_state: "accepted",
          user_id: "80113acb-9865-46aa-a9bf-d7bde155208a",
          version: 5,
        },
      ],
      audience_revision: 8,
      external_attendees: [],
      response_requested: true,
      viewer_access: {
        can_manage_attendees: true,
        can_respond: true,
        can_see_guest_list: true,
      },
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(managerAudience));

    renderPanel(fetchMock);

    expect(await screen.findByText("2 internal attendees")).toBeVisible();
    const list = screen.getByRole("list", { name: "Internal attendees" });
    expect(within(list).getByText("You")).toBeVisible();
    expect(within(list).getByText("User …e155208a")).toBeVisible();
    expect(within(list).getByText(/Organizer/u)).toBeVisible();
    expect(within(list).getByText(/Student/u)).toBeVisible();
    expect(document.body.textContent).not.toContain("@");
  });

  it("keeps one audience editor and clears its pending state after replacement", async () => {
    const studentAttendee = {
      ...selfAttendee,
      business_role: "student" as const,
      id: "85e4d84c-cf88-45b7-845c-6dd1b777a311",
      is_self: false,
      participation_role: "optional" as const,
      rsvp_state: "accepted" as const,
      user_id: studentID,
      version: 5,
    };
    const managerAudience: SessionAudience = {
      attendees: [
        { ...selfAttendee, business_role: "organizer" },
        studentAttendee,
      ],
      audience_revision: 8,
      external_attendees: [],
      response_requested: true,
      viewer_access: {
        can_manage_attendees: true,
        can_respond: true,
        can_see_guest_list: true,
      },
    };
    const managerRoster: ClassRosterPage = {
      class_owner: {
        class_role: "owner",
        user: {
          display_name: "Olivia Owner",
          email: "owner@example.test",
          id: userID,
        },
      },
      items: [
        {
          actions: {
            assignable_roles: [],
            can_remove: true,
            can_suspend: true,
          },
          enrollment: {
            class_id: classID,
            class_role: "student",
            created_at: "2026-07-20T01:00:00Z",
            enrolled_by: userID,
            id: "7426fdcf-9956-481c-9871-180a1f23bc29",
            joined_at: "2026-07-20T01:00:00Z",
            left_at: null,
            removed_at: null,
            status: "active",
            suspended_at: null,
            updated_at: "2026-07-20T01:00:00Z",
            user_id: studentID,
          },
          user: {
            display_name: "Ada Student",
            email: "ada@example.test",
            id: studentID,
          },
        },
      ],
      next_cursor: null,
    };
    let replacementBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      const audiencePath = `/api/v1/classes/${classID}/sessions/${sessionID}/attendees`;
      if (request.method === "GET" && url.pathname.endsWith(audiencePath)) {
        return jsonResponse(managerAudience);
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/api/v1/classes/${classID}/roster`)
      ) {
        return jsonResponse(managerRoster);
      }
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "audience-csrf" });
      }
      if (request.method === "PUT" && url.pathname.endsWith(audiencePath)) {
        replacementBody = await request.clone().json();
        return jsonResponse({
          audience: {
            ...managerAudience,
            attendees: [
              managerAudience.attendees[0],
              { ...studentAttendee, participation_role: "required" },
            ],
            audience_revision: 9,
          },
          replayed: false,
        });
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });

    renderPanel(fetchMock);

    const rosterList = await screen.findByRole("list", {
      name: "Class members available to invite",
    });
    const studentChoices = within(rosterList).getByRole("group", {
      name: "Attendance role for Ada Student",
    });
    fireEvent.click(
      within(studentChoices).getByRole("checkbox", { name: "Required" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save attendees" }));

    await waitFor(() => expect(replacementBody).toBeDefined());
    await waitFor(() => {
      expect(
        screen.getAllByRole("heading", { name: "Manage attendees" }),
      ).toHaveLength(1);
      expect(
        screen.queryByRole("button", { name: "Saving..." }),
      ).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "Save attendees" }),
    ).toBeDisabled();
    expect(replacementBody).toMatchObject({
      attendees: expect.arrayContaining([
        { participation_role: "required", user_id: studentID },
      ]),
      expected_audience_revision: 8,
    });
  });

  it("moves focus to reload after a stale RSVP conflict", async () => {
    let audienceReads = 0;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${sessionID}/attendees`,
        )
      ) {
        audienceReads += 1;
        return jsonResponse(selfAudience);
      }
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "participation-csrf" });
      }
      if (request.method === "POST") {
        return jsonResponse(
          {
            detail: "The attendee changed.",
            status: 409,
            title: "Conflict",
            type: "https://tutorhub.test/problems/conflict",
          },
          409,
        );
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });

    renderPanel(fetchMock);
    await screen.findByText("Your response");
    fireEvent.click(screen.getByRole("button", { name: "Decline" }));

    const reload = await screen.findByRole("button", {
      name: "Reload invitation",
    });
    await waitFor(() => expect(reload).toHaveFocus());
    fireEvent.click(reload);
    await waitFor(() => expect(audienceReads).toBe(2));
  });

  it("loads and responds to a recurring occurrence through its typed endpoint", async () => {
    const recurringItem: CalendarItemViewModel = {
      ...item,
      id: `class-session:${seriesID}:${item.occurrenceKey}`,
      seriesID,
      sourceID: seriesID,
    };
    const materializedAttendee = {
      ...selfAttendee,
      id: "882f6e31-9664-4c57-826f-8e727da8084e",
      responded_at: "2026-07-29T04:00:00Z",
      rsvp_state: "accepted" as const,
      version: 3,
    };
    let audienceReads = 0;
    let responseBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      const path = decodeURIComponent(url.pathname);
      const occurrenceBase = `/api/v1/classes/${classID}/session-series/${seriesID}/occurrences/${item.occurrenceKey}`;
      if (
        request.method === "GET" &&
        path.endsWith(`${occurrenceBase}/attendees`)
      ) {
        audienceReads += 1;
        return jsonResponse(
          audienceReads === 1
            ? { ...selfAudience, audience_revision: 0 }
            : {
                ...selfAudience,
                attendees: [materializedAttendee],
                audience_revision: 1,
              },
        );
      }
      if (path.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "participation-csrf" });
      }
      if (
        request.method === "POST" &&
        path.endsWith(`${occurrenceBase}/responses`)
      ) {
        responseBody = await request.clone().json();
        return jsonResponse({
          attendee: materializedAttendee,
          replayed: false,
        });
      }
      throw new Error(`unexpected request ${request.method} ${path}`);
    });

    renderPanel(fetchMock, recurringItem);
    await screen.findByText("Your response");
    fireEvent.click(screen.getByRole("button", { name: "Accept" }));

    expect(await screen.findByText("Response saved.")).toBeVisible();
    expect(responseBody).toMatchObject({
      expected_attendee_version: 2,
      state: "accepted",
    });
    await waitFor(() => expect(audienceReads).toBe(2));
    expect(screen.getByRole("button", { name: "Accept" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });
});
