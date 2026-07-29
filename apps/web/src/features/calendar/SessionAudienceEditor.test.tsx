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
import type { ClassSessionParticipationSource } from "../../app/classSessionParticipation";
import { I18nProvider } from "../../app/i18n";
import { SessionAudienceEditor } from "./SessionAudienceEditor";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
const sessionID = "3042cad1-d582-4c59-b821-eb599b27ebf7";
const seriesID = "4d7cd279-452e-48f8-913b-7897f71785a7";
const occurrenceKey = "2026-07-27T02:00:00Z";
const ownerID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const studentID = "80113acb-9865-46aa-a9bf-d7bde155208a";

const audience: SessionAudience = {
  attendees: [
    {
      business_role: "organizer",
      id: "bcd16c31-c8b9-4c2f-a0e9-d87e4220d32a",
      is_self: true,
      participation_role: "required",
      responded_at: null,
      rsvp_state: "needs_action",
      user_id: ownerID,
      version: 2,
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

const roster: ClassRosterPage = {
  class_owner: {
    class_role: "owner",
    user: {
      display_name: "Olivia Owner",
      email: "owner@example.test",
      id: ownerID,
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
        enrolled_by: ownerID,
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

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function renderEditor(
  fetchMock: ReturnType<typeof vi.fn>,
  onReloadAudience = vi.fn().mockResolvedValue(undefined),
  source: ClassSessionParticipationSource = { kind: "session", sessionID },
  renderedAudience: SessionAudience = audience,
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
        <SessionAudienceEditor
          audience={renderedAudience}
          classID={classID}
          onReloadAudience={onReloadAudience}
          source={source}
          tenantID={tenantID}
          userID={ownerID}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return { onReloadAudience, queryClient };
}

describe("SessionAudienceEditor", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("replaces the internal audience from the active roster with CAS and idempotency", async () => {
    let replacementBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/api/v1/classes/${classID}/roster`)
      ) {
        expect(url.searchParams.get("status")).toBe("active");
        return jsonResponse(roster);
      }
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "audience-csrf" });
      }
      if (
        request.method === "PUT" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${sessionID}/attendees`,
        )
      ) {
        replacementBody = await request.clone().json();
        expect(request.headers.get("X-CSRF-Token")).toBe("audience-csrf");
        expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
          tenantID,
        );
        return jsonResponse({
          audience: {
            ...audience,
            attendees: [
              audience.attendees[0],
              {
                ...audience.attendees[0],
                business_role: "student",
                id: "85e4d84c-cf88-45b7-845c-6dd1b777a311",
                is_self: false,
                participation_role: "optional",
                user_id: studentID,
                version: 1,
              },
            ],
            audience_revision: 9,
          },
          replayed: false,
        });
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });

    renderEditor(fetchMock);

    const rosterList = await screen.findByRole("list", {
      name: "Class members available to invite",
    });
    expect(within(rosterList).getByText("Owner")).toBeVisible();
    expect(within(rosterList).getByText("Student")).toBeVisible();
    expect(document.body.textContent).not.toContain("@example.test");

    const studentChoices = within(rosterList).getByRole("group", {
      name: "Attendance role for Ada Student",
    });
    fireEvent.click(
      within(studentChoices).getByRole("checkbox", { name: "Optional" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save attendees" }));

    expect(await screen.findByText("Attendees updated.")).toBeVisible();
    expect(replacementBody).toEqual({
      attendees: [
        { participation_role: "required", user_id: ownerID },
        { participation_role: "optional", user_id: studentID },
      ],
      expected_audience_revision: 8,
      idempotency_key: expect.stringMatching(/^audience:/u),
      response_requested: true,
    });
  });

  it("moves focus to reload after a stale audience conflict", async () => {
    let rosterReads = 0;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname.endsWith(`/api/v1/classes/${classID}/roster`)
      ) {
        rosterReads += 1;
        return jsonResponse(roster);
      }
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "audience-csrf" });
      }
      if (request.method === "PUT") {
        return jsonResponse(
          {
            detail: "The audience changed.",
            status: 409,
            title: "Conflict",
            type: "https://tutorhub.test/problems/conflict",
          },
          409,
        );
      }
      throw new Error(`unexpected request ${request.method} ${url.pathname}`);
    });
    const reloadAudience = vi.fn().mockResolvedValue(undefined);

    renderEditor(fetchMock, reloadAudience);
    const studentChoices = await screen.findByRole("group", {
      name: "Attendance role for Ada Student",
    });
    fireEvent.click(
      within(studentChoices).getByRole("checkbox", { name: "Required" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save attendees" }));

    const reload = await screen.findByRole("button", {
      name: "Reload attendee list",
    });
    await waitFor(() => expect(reload).toHaveFocus());
    fireEvent.click(reload);
    await waitFor(() => {
      expect(reloadAudience).toHaveBeenCalledTimes(1);
      expect(rosterReads).toBe(2);
    });
  });

  it("creates an occurrence override from inherited audience revision zero", async () => {
    const inheritedAudience: SessionAudience = {
      ...audience,
      audience_revision: 0,
    };
    let replacementBody: unknown;
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const url = new URL(request.url);
      const path = decodeURIComponent(url.pathname);
      if (
        request.method === "GET" &&
        path.endsWith(`/api/v1/classes/${classID}/roster`)
      ) {
        return jsonResponse(roster);
      }
      if (path.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "audience-csrf" });
      }
      if (
        request.method === "PUT" &&
        path.endsWith(
          `/api/v1/classes/${classID}/session-series/${seriesID}/occurrences/${occurrenceKey}/attendees`,
        )
      ) {
        replacementBody = await request.clone().json();
        return jsonResponse({
          audience: {
            ...inheritedAudience,
            audience_revision: 1,
          },
          replayed: false,
        });
      }
      throw new Error(`unexpected request ${request.method} ${path}`);
    });

    renderEditor(
      fetchMock,
      undefined,
      { kind: "occurrence", occurrenceKey, seriesID },
      inheritedAudience,
    );
    const studentChoices = await screen.findByRole("group", {
      name: "Attendance role for Ada Student",
    });
    fireEvent.click(
      within(studentChoices).getByRole("checkbox", { name: "Optional" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save attendees" }));

    expect(await screen.findByText("Attendees updated.")).toBeVisible();
    expect(replacementBody).toMatchObject({ expected_audience_revision: 0 });
  });

  it("conceals roster identity when server-side access is revoked", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          detail: "Roster access was revoked.",
          status: 403,
          title: "Forbidden",
          type: "https://tutorhub.test/problems/forbidden",
        },
        403,
      ),
    );

    renderEditor(fetchMock);

    expect(
      await screen.findByText(
        "Your access to view or manage the class roster has changed.",
      ),
    ).toBeVisible();
    expect(screen.queryByText("Olivia Owner")).not.toBeInTheDocument();
    expect(screen.queryByText("Ada Student")).not.toBeInTheDocument();
  });
});
