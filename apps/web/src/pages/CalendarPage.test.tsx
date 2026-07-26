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
} from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  CalendarDisplayPreference,
  CalendarItem,
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

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    status,
  });
}

function LocationProbe() {
  const location = useLocation();
  return <output data-testid="location">{location.search}</output>;
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
        requests.filter((request) =>
          new URL(request.url).pathname.endsWith("/api/v1/calendar/items"),
        ),
      ).toHaveLength(2),
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
