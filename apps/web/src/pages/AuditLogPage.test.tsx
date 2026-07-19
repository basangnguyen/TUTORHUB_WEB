import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { AuditEvent, CurrentUser } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "../app/i18n";
import { SessionProvider } from "../app/session";
import { AuditLogPage } from "./AuditLogPage";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const classID = "e5e39e5f-edcc-4c90-9f3b-8a3ce5eb6c0f";
const secondClassID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";

const currentUser: CurrentUser = {
  user: {
    id: "be85eb92-0f18-4163-85ba-50e4d343d632",
    email: "admin@example.com",
    display_name: "Admin User",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "audit-test",
    name: "Audit Test",
    role: "org_admin",
    status: "active",
    version: 1,
    is_active: true,
  },
  memberships: [],
  permissions: ["tenant.view", "audit.view"],
};

const firstEvent: AuditEvent = {
  id: "69e54da8-f91f-4836-bf43-c98548daf2ae",
  tenant_id: tenantID,
  actor: {
    type: "user",
    user_id: currentUser.user.id,
    display_name: "Admin User",
  },
  action: "class.update",
  resource: { type: "class", id: classID },
  outcome: "succeeded",
  request_id: "req-audit-first",
  metadata: { status: "active" },
  occurred_at: "2026-07-19T08:00:00Z",
};

const secondEvent: AuditEvent = {
  ...firstEvent,
  id: "39712cc2-5e4b-4be1-9fab-d19162f60729",
  actor: { type: "system", user_id: null, display_name: null },
  action: "class.invite_code.expire",
  resource: { type: "class_invite_code", id: secondClassID },
  outcome: "failed",
  request_id: "req-audit-second",
  metadata: {},
  occurred_at: "2026-07-19T07:00:00Z",
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

function renderPage(
  fetchMock: ReturnType<typeof vi.fn>,
  user: CurrentUser = currentUser,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  vi.stubGlobal("fetch", fetchMock);
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser: user }}>
          <MemoryRouter>
            <AuditLogPage />
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
  return queryClient;
}

function requestFrom(call: unknown[]) {
  return call[0] as Request;
}

describe("AuditLogPage", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("does not query audit history when the principal lacks audit.view", () => {
    const fetchMock = vi.fn();
    renderPage(fetchMock, { ...currentUser, permissions: ["tenant.view"] });

    expect(
      screen.getByRole("heading", {
        name: "Audit history is restricted to administrators",
      }),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("renders the safe event projection with semantic table labels", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ items: [firstEvent] }));
    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "Activity audit log" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("table", { name: "Workspace audit event list" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Admin User")).toBeInTheDocument();
    expect(screen.getByText(currentUser.user.id)).toBeInTheDocument();
    expect(screen.getAllByText("Update class")).toHaveLength(2);
    expect(screen.getByText("req-audit-first")).toBeInTheDocument();
    expect(screen.getByText("1 events loaded")).toBeInTheDocument();

    const request = requestFrom(fetchMock.mock.calls[0] ?? []);
    const url = new URL(request.url);
    expect(url.pathname).toBe(`/api/v1/tenants/${tenantID}/audit-events`);
    expect(url.searchParams.get("limit")).toBe("25");
  });

  it("validates dependent resource filters and sends normalized filter values", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "No audit events yet" }),
    ).toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox", { name: "Resource ID" }), {
      target: { value: classID.toUpperCase() },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(
      screen.getByText("Enter a resource type before filtering by its ID."),
    ).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);

    fireEvent.change(screen.getByRole("textbox", { name: "Resource type" }), {
      target: { value: " CLASS " },
    });
    fireEvent.change(screen.getByLabelText("Occurred from"), {
      target: { value: "2026-07-18T08:00" },
    });
    fireEvent.change(screen.getByLabelText("Occurred before"), {
      target: { value: "2026-07-20T08:00" },
    });
    const actionSelect = screen.getByRole("combobox", { name: "Action" });
    fireEvent.keyDown(actionSelect, { key: "ArrowDown" });
    fireEvent.click(
      await screen.findByRole("option", { name: "Update class" }),
    );
    const outcomeSelect = screen.getByRole("combobox", { name: "Outcome" });
    fireEvent.keyDown(outcomeSelect, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Denied" }));
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const request = requestFrom(fetchMock.mock.calls[1] ?? []);
    const search = new URL(request.url).searchParams;
    expect(search.get("action")).toBe("class.update");
    expect(search.get("outcome")).toBe("denied");
    expect(search.get("resource_type")).toBe("class");
    expect(search.get("resource_id")).toBe(classID);
    expect(search.get("occurred_from")).toBe(
      new Date("2026-07-18T08:00").toISOString(),
    );
    expect(search.get("occurred_to")).toBe(
      new Date("2026-07-20T08:00").toISOString(),
    );
  });

  it("keeps loaded events when the next page fails and retries the same cursor", async () => {
    let cursorRequests = 0;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const cursor = new URL(request.url).searchParams.get("cursor");
      if (!cursor) {
        return Promise.resolve(
          jsonResponse({ items: [firstEvent], next_cursor: "audit-page-2" }),
        );
      }
      cursorRequests += 1;
      if (cursorRequests === 1) {
        return Promise.resolve(
          jsonResponse(
            { title: "Invalid cursor", status: 400, detail: "retry" },
            400,
          ),
        );
      }
      return Promise.resolve(jsonResponse({ items: [secondEvent] }));
    });
    renderPage(fetchMock);

    expect(await screen.findByText("req-audit-first")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Load more events" }));
    expect(
      await screen.findByText(
        "The next page could not be loaded. Current events remain available.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("req-audit-first")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("req-audit-second")).toBeInTheDocument();
    expect(screen.getByText("System")).toBeInTheDocument();
    expect(cursorRequests).toBe(2);
  });

  it("maps a stale server-side permission denial to a forbidden state", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        jsonResponse(
          { title: "Forbidden", status: 403, detail: "access denied" },
          403,
        ),
      );
    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", {
        name: "Audit history is restricted to administrators",
      }),
    ).toBeInTheDocument();
  });

  it.each([401, 403, 404])(
    "hides cached audit PII and identifiers when refresh returns %s",
    async (status) => {
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(jsonResponse({ items: [firstEvent] }))
        .mockResolvedValueOnce(
          jsonResponse(
            {
              title: "Audit history unavailable",
              status,
              detail: "access boundary changed",
            },
            status,
          ),
        );
      renderPage(fetchMock);

      expect(await screen.findByText("req-audit-first")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

      await waitFor(() =>
        expect(screen.queryByText("req-audit-first")).not.toBeInTheDocument(),
      );
      expect(screen.queryByText("Admin User")).not.toBeInTheDocument();
      expect(screen.queryByText(currentUser.user.id)).not.toBeInTheDocument();
      expect(screen.queryByText(classID)).not.toBeInTheDocument();
      expect(
        screen.queryByRole("table", { name: "Workspace audit event list" }),
      ).not.toBeInTheDocument();
      expect(screen.queryByText("1 events loaded")).not.toBeInTheDocument();
      if (status === 403) {
        expect(
          screen.getByRole("heading", {
            name: "Audit history is restricted to administrators",
          }),
        ).toBeInTheDocument();
      } else {
        expect(
          screen.getByRole("button", { name: "Try again" }),
        ).toBeInTheDocument();
      }
    },
  );

  it("keeps cached events visible with a warning when refresh fails", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ items: [firstEvent] }))
      .mockResolvedValueOnce(
        jsonResponse(
          { title: "Bad request", status: 400, detail: "refresh failed" },
          400,
        ),
      );
    renderPage(fetchMock);

    expect(await screen.findByText("req-audit-first")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    expect(
      await screen.findByText(
        "Refresh failed. Previously loaded events may no longer be current.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("req-audit-first")).toBeInTheDocument();
  });
});
