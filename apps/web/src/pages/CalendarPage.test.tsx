import {
  QueryClient,
  QueryClientProvider,
  useQueryClient,
} from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  CalendarDisplayPreference,
  CalendarItem,
  ClassSession,
  ClassroomClass,
  CurrentUser,
} from "@tutorhub/api-client";
import { I18nProvider } from "../app/i18n";
import {
  advancePrincipalGeneration,
  currentPrincipalGeneration,
} from "../app/queryClient";
import { SessionProvider, useSession } from "../app/session";
import { calendarQueryKeys } from "../features/calendar/queries";
import { CalendarPage } from "./CalendarPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "be85eb92-0f18-4163-85ba-50e4d343d632";
const classID = "1dcf37ff-4450-49ff-90aa-74dfbd551da2";
const secondTenantID = "fa84bf8c-8205-4162-8ee9-d86ca11ddf26";

const currentUser: CurrentUser = {
  active_tenant: {
    id: tenantID,
    is_active: true,
    name: "TutorHub Test",
    role: "teacher",
    slug: "tutorhub-test",
    status: "active",
    version: 2,
  },
  memberships: [],
  permissions: ["class.view"],
  user: {
    display_name: "TutorHub Teacher",
    email: "teacher@example.com",
    id: userID,
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
};

const secondTenantUser: CurrentUser = {
  ...currentUser,
  active_tenant: {
    ...currentUser.active_tenant!,
    id: secondTenantID,
    name: "TutorHub Second Tenant",
    slug: "tutorhub-second-tenant",
  },
};

const preference: CalendarDisplayPreference = {
  default_view: "week",
  density: "comfortable",
  locale: "en-US",
  secondary_timezone: null,
  time_format: "24h",
  time_scale_minutes: 30,
  updated_at: "2026-07-26T08:00:00Z",
  version: 3,
  viewer_timezone: "Asia/Ho_Chi_Minh",
  week_start: "monday",
};

const item: CalendarItem = {
  all_day: false,
  class_id: classID,
  class_title: "Advanced Mathematics",
  color_token: "class_session",
  display_timezone: "Asia/Ho_Chi_Minh",
  ends_at: "2026-07-27T03:00:00Z",
  id: "class_session:2b9c25a4-b01b-4b5b-9e87-a782675ed511",
  occurrence_key: "2b9c25a4-b01b-4b5b-9e87-a782675ed511",
  source_id: "2b9c25a4-b01b-4b5b-9e87-a782675ed511",
  source_type: "class_session",
  starts_at: "2026-07-27T02:00:00Z",
  status: "scheduled",
  title: "Linear algebra",
  version: 4,
  viewer_capabilities: {
    can_cancel: true,
    can_edit: true,
    can_reschedule: true,
    can_view: true,
  },
};

const classroom: ClassroomClass = {
  archived_at: null,
  code: "MATH101",
  created_at: "2026-07-20T02:00:00Z",
  description: "Calendar editor regression class.",
  id: classID,
  owner_user_id: userID,
  status: "active",
  timezone: "Asia/Ho_Chi_Minh",
  title: "Advanced Mathematics",
  updated_at: "2026-07-20T02:00:00Z",
  version: 2,
  viewer_access: {
    can_archive_class: true,
    can_join_room: true,
    can_leave: false,
    can_manage_enrollments: true,
    can_publish_media: true,
    can_schedule_sessions: true,
    can_transfer_ownership: true,
    can_update_class: true,
    class_role: "owner",
    enrollment_status: null,
  },
};

const classSession: ClassSession = {
  cancelled_at: null,
  cancelled_by: null,
  class_id: classID,
  created_at: "2026-07-20T02:00:00Z",
  created_by: userID,
  description: "Calendar editor regression session.",
  ends_at: item.ends_at,
  id: item.source_id,
  starts_at: item.starts_at,
  status: "scheduled",
  timezone: "Asia/Ho_Chi_Minh",
  title: item.title,
  updated_at: "2026-07-20T02:00:00Z",
  updated_by: userID,
  version: item.version,
  viewer_access: { can_cancel: true, can_update: true },
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    status,
  });
}

function LocationProbe() {
  const location = useLocation();
  return (
    <>
      <output data-testid="location">{location.search}</output>
      <output data-testid="pathname">{location.pathname}</output>
    </>
  );
}

function PrincipalSwitch({ nextUser }: { nextUser: CurrentUser }) {
  const queryClient = useQueryClient();
  const session = useSession();
  return (
    <button
      onClick={() => {
        advancePrincipalGeneration(queryClient);
        queryClient.setQueryData(["auth", "me"], nextUser);
        session.replaceCurrentUser(nextUser);
      }}
      type="button"
    >
      Switch principal
    </button>
  );
}

function renderCalendar(
  fetchMock: ReturnType<typeof vi.fn>,
  switchTo?: CurrentUser,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, retryDelay: 0 } },
  });
  queryClient.setQueryData(["auth", "me"], currentUser);
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ currentUser, kind: "static" }}>
          <MemoryRouter
            initialEntries={["/app/calendar?view=agenda&date=2026-07-26"]}
          >
            <CalendarPage />
            <LocationProbe />
            {switchTo && <PrincipalSwitch nextUser={switchTo} />}
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function deferredResponse() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}

describe("CalendarPage", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("loads the tenant-scoped projection and keeps view changes in URL state", async () => {
    const requests: Request[] = [];
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/api/v1/calendar/preferences/display")
      ) {
        return Promise.resolve(jsonResponse(preference));
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/api/v1/calendar/items")
      ) {
        return Promise.resolve(
          jsonResponse({ items: [item], next_cursor: null }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderCalendar(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "Linear algebra" }),
    ).toBeVisible();
    const itemRequest = requests.find((request) =>
      new URL(request.url).pathname.endsWith("/api/v1/calendar/items"),
    );
    expect(itemRequest?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    const itemURL = new URL(itemRequest?.url ?? "http://localhost");
    expect(itemURL.searchParams.get("viewer_timezone")).toBe(
      "Asia/Ho_Chi_Minh",
    );
    expect(itemURL.searchParams.get("from")).toBe("2026-07-25T17:00:00Z");
    expect(itemURL.searchParams.get("to")).toBe("2026-08-22T17:00:00Z");
    expect(
      queryClient.getQueriesData({
        queryKey: ["calendar", tenantID, userID, "items"],
      }),
    ).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Month" }));
    expect(screen.getByTestId("location")).toHaveTextContent(
      "?view=month&date=2026-07-26",
    );
    await waitFor(() =>
      expect(
        document.querySelector(
          '[data-calendar-renderer="fullcalendar-standard"]',
        ),
      ).not.toBeNull(),
    );
    expect(screen.getByText("Open keyboard-friendly agenda")).toBeVisible();
    await waitFor(() =>
      expect(
        requests.filter((request) =>
          new URL(request.url).pathname.endsWith("/api/v1/calendar/items"),
        ),
      ).toHaveLength(2),
    );
    fireEvent.click(screen.getByRole("button", { name: "Availability polls" }));
    expect(screen.getByTestId("pathname")).toHaveTextContent(
      "/app/calendar/availability-polls",
    );
  });

  it("replaces display preferences with CSRF and optimistic version", async () => {
    const requests: Request[] = [];
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "calendar-csrf" }));
      }
      if (
        url.pathname.endsWith("/api/v1/calendar/preferences/display") &&
        request.method === "GET"
      ) {
        return Promise.resolve(jsonResponse(preference));
      }
      if (
        url.pathname.endsWith("/api/v1/calendar/preferences/display") &&
        request.method === "PUT"
      ) {
        return request
          .clone()
          .json()
          .then((body) =>
            jsonResponse({
              ...preference,
              ...(body as object),
              updated_at: "2026-07-26T08:01:00Z",
              version: 4,
            }),
          );
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(jsonResponse({ items: [], next_cursor: null }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderCalendar(fetchMock);

    await screen.findByRole("heading", {
      name: "Nothing scheduled in this range",
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Display preferences" }),
    );
    const timezone = await screen.findByLabelText("Display timezone");
    fireEvent.change(timezone, { target: { value: "UTC" } });
    fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));

    expect(await screen.findByText("Display preferences saved.")).toBeVisible();
    const updateRequest = requests.find(
      (request) =>
        request.method === "PUT" &&
        new URL(request.url).pathname.endsWith(
          "/api/v1/calendar/preferences/display",
        ),
    );
    expect(updateRequest?.headers.get("X-CSRF-Token")).toBe("calendar-csrf");
    expect(updateRequest?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    await expect(updateRequest?.clone().json()).resolves.toMatchObject({
      expected_version: 3,
      viewer_timezone: "UTC",
    });
  });

  it("creates a one-time class session from the calendar", async () => {
    const requests: Request[] = [];
    let created = false;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "calendar-csrf" }));
      }
      if (url.pathname.endsWith("/api/v1/calendar/preferences/display")) {
        return Promise.resolve(jsonResponse(preference));
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(
          jsonResponse({ items: created ? [item] : [], next_cursor: null }),
        );
      }
      if (url.pathname.endsWith("/api/v1/classes")) {
        return Promise.resolve(
          jsonResponse({ items: [classroom], next_cursor: null }),
        );
      }
      if (
        request.method === "POST" &&
        url.pathname.endsWith(`/api/v1/classes/${classID}/sessions`)
      ) {
        created = true;
        return request
          .clone()
          .json()
          .then((body) =>
            jsonResponse({
              ...classSession,
              ...(body as object),
              version: 1,
            }),
          );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderCalendar(fetchMock);

    await screen.findByRole("heading", {
      name: "Nothing scheduled in this range",
    });
    fireEvent.click(screen.getByRole("button", { name: "New session" }));
    await screen.findByLabelText("Starts");
    const dialog = screen.getByRole("dialog", {
      name: "Schedule a session",
    });
    (within(dialog).getByLabelText("Starts") as HTMLInputElement).value =
      "2026-07-30T09:00";
    (within(dialog).getByLabelText("Ends") as HTMLInputElement).value =
      "2026-07-30T10:00";
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Save session" }),
    );

    await waitFor(() =>
      expect(
        requests.some(
          (request) =>
            request.method === "POST" &&
            new URL(request.url).pathname.endsWith(
              `/api/v1/classes/${classID}/sessions`,
            ),
        ),
      ).toBe(true),
    );
    const createRequest = requests.find(
      (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith(
          `/api/v1/classes/${classID}/sessions`,
        ),
    );
    expect(createRequest?.headers.get("X-CSRF-Token")).toBe("calendar-csrf");
    await expect(createRequest?.clone().json()).resolves.toMatchObject({
      timezone: "Asia/Ho_Chi_Minh",
      title: classroom.title,
    });
    expect(
      await screen.findByRole("heading", { name: item.title }),
    ).toBeVisible();
  });

  it("edits a one-time session with its latest optimistic version", async () => {
    const requests: Request[] = [];
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "calendar-csrf" }));
      }
      if (url.pathname.endsWith("/api/v1/calendar/preferences/display")) {
        return Promise.resolve(jsonResponse(preference));
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(
          jsonResponse({ items: [item], next_cursor: null }),
        );
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${item.source_id}`,
        )
      ) {
        return Promise.resolve(jsonResponse(classSession));
      }
      if (
        request.method === "PATCH" &&
        url.pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${item.source_id}`,
        )
      ) {
        return request
          .clone()
          .json()
          .then((body) =>
            jsonResponse({
              ...classSession,
              ...(body as object),
              version: classSession.version + 1,
            }),
          );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderCalendar(fetchMock);

    await screen.findByRole("heading", { name: item.title });
    fireEvent.click(screen.getByRole("button", { name: "View details" }));
    const detailDrawer = await screen.findByRole("dialog", {
      name: item.title,
    });
    fireEvent.click(
      within(detailDrawer).getByRole("button", { name: "Edit session" }),
    );
    await screen.findByRole("textbox", { name: "Session title" });
    const dialog = screen.getByRole("dialog", { name: "Edit session" });
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Session title" }),
      { target: { value: "Updated linear algebra" } },
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Save session" }),
    );

    const updateRequest = await waitFor(() => {
      const request = requests.find(
        (candidate) =>
          candidate.method === "PATCH" &&
          new URL(candidate.url).pathname.endsWith(
            `/api/v1/classes/${classID}/sessions/${item.source_id}`,
          ),
      );
      expect(request).toBeDefined();
      return request;
    });
    expect(updateRequest?.headers.get("X-CSRF-Token")).toBe("calendar-csrf");
    await expect(updateRequest?.clone().json()).resolves.toMatchObject({
      expected_version: item.version,
      title: "Updated linear algebra",
    });
  });

  it("opens projection details for a read-only calendar item", async () => {
    const readOnlyItem: CalendarItem = {
      ...item,
      viewer_capabilities: {
        can_cancel: false,
        can_edit: false,
        can_reschedule: false,
        can_view: true,
      },
    };
    const requests: Request[] = [];
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      requests.push(request);
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/calendar/preferences/display")) {
        return Promise.resolve(jsonResponse(preference));
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(
          jsonResponse({ items: [readOnlyItem], next_cursor: null }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    renderCalendar(fetchMock);

    await screen.findByRole("heading", { name: readOnlyItem.title });
    fireEvent.click(screen.getByRole("button", { name: "View details" }));

    const detailDrawer = await screen.findByRole("dialog", {
      name: readOnlyItem.title,
    });
    expect(
      within(detailDrawer).getByText(readOnlyItem.class_title),
    ).toBeVisible();
    expect(within(detailDrawer).getByText("Scheduled")).toBeVisible();
    expect(
      within(detailDrawer).queryByRole("button", { name: "Edit session" }),
    ).not.toBeInTheDocument();
    expect(
      requests.some((request) =>
        new URL(request.url).pathname.endsWith(
          `/api/v1/classes/${classID}/sessions/${item.source_id}`,
        ),
      ),
    ).toBe(false);
  });

  it("warns when cached calendar items are displayed while offline", async () => {
    vi.spyOn(window.navigator, "onLine", "get").mockReturnValue(false);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      if (url.pathname.endsWith("/api/v1/calendar/preferences/display")) {
        return Promise.resolve(jsonResponse(preference));
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(
          jsonResponse({ items: [item], next_cursor: null }),
        );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });

    renderCalendar(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "Linear algebra" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "You are offline; the calendar below is cached and may be out of date.",
      ),
    ).toBeVisible();
  });

  it("isolates an in-flight preference save when the principal changes", async () => {
    const pendingUpdate = deferredResponse();
    const secondPreference: CalendarDisplayPreference = {
      ...preference,
      viewer_timezone: "America/New_York",
      version: 7,
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const url = new URL(request.url);
      const requestTenant = request.headers.get(
        "X-TutorHub-Expected-Tenant-ID",
      );
      if (url.pathname.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "calendar-csrf" }));
      }
      if (
        request.method === "GET" &&
        url.pathname.endsWith("/api/v1/calendar/preferences/display")
      ) {
        return Promise.resolve(
          jsonResponse(
            requestTenant === secondTenantID ? secondPreference : preference,
          ),
        );
      }
      if (
        request.method === "PUT" &&
        url.pathname.endsWith("/api/v1/calendar/preferences/display")
      ) {
        return pendingUpdate.promise;
      }
      if (url.pathname.endsWith("/api/v1/calendar/items")) {
        return Promise.resolve(jsonResponse({ items: [], next_cursor: null }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    const queryClient = renderCalendar(fetchMock, secondTenantUser);

    await screen.findByRole("heading", {
      name: "Nothing scheduled in this range",
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Display preferences" }),
    );
    fireEvent.change(await screen.findByLabelText("Display timezone"), {
      target: { value: "UTC" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save preferences" }));
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([request]) =>
            (request as Request).method === "PUT" &&
            new URL((request as Request).url).pathname.endsWith(
              "/api/v1/calendar/preferences/display",
            ),
        ),
      ).toBe(true),
    );

    fireEvent.click(screen.getByText("Switch principal"));
    expect(
      screen.queryByRole("button", { name: "Save preferences" }),
    ).not.toBeInTheDocument();

    pendingUpdate.resolve(
      jsonResponse({
        ...preference,
        viewer_timezone: "UTC",
        updated_at: "2026-07-26T08:01:00Z",
        version: 4,
      }),
    );

    await waitFor(() =>
      expect(
        queryClient.getQueryData(
          calendarQueryKeys.preference(tenantID, userID),
        ),
      ).toBeUndefined(),
    );
    expect(currentPrincipalGeneration(queryClient)).toBe(1);

    fireEvent.click(
      screen.getByRole("button", { name: "Display preferences" }),
    );
    expect(await screen.findByLabelText("Display timezone")).toHaveValue(
      "America/New_York",
    );
    expect(
      screen.getByRole("button", { name: "Save preferences" }),
    ).not.toBeDisabled();
  });

  it.each([
    [403, "Calendar is unavailable"],
    [503, "Calendar could not be loaded"],
  ])(
    "shows a terminal state instead of a loading skeleton when preferences return %i",
    async (status, expectedHeading) => {
      const fetchMock = vi.fn().mockImplementation((request: Request) => {
        const url = new URL(request.url);
        if (url.pathname.endsWith("/api/v1/calendar/preferences/display")) {
          return Promise.resolve(
            jsonResponse(
              {
                code: status === 403 ? "forbidden" : "service_unavailable",
                message: "Calendar preference unavailable",
              },
              status,
            ),
          );
        }
        return Promise.reject(new Error(`Unexpected request: ${request.url}`));
      });
      renderCalendar(fetchMock);

      expect(
        await screen.findByRole("heading", { name: expectedHeading }),
      ).toBeVisible();
      expect(screen.queryByText("Loading calendar")).not.toBeInTheDocument();
      expect(
        fetchMock.mock.calls.some(([request]) =>
          new URL((request as Request).url).pathname.endsWith(
            "/api/v1/calendar/items",
          ),
        ),
      ).toBe(false);
    },
  );
});
