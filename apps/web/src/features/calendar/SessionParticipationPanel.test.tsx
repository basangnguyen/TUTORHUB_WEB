import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { SessionAudience } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import type { CalendarItemViewModel } from "./model";
import { SessionParticipationPanel } from "./SessionParticipationPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const sessionID = "3042cad1-d582-4c59-b821-eb599b27ebf7";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";

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

function renderPanel(fetchMock: ReturnType<typeof vi.fn>) {
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
          item={item}
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
    let responseBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${sessionID}/attendees`,
        )
      ) {
        return jsonResponse(selfAudience);
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
        return jsonResponse({
          attendee: {
            ...selfAttendee,
            responded_at: "2026-07-29T04:00:00Z",
            rsvp_state: "tentative",
            version: 3,
          },
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
});
