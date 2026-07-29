import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { ExternalCalendarRSVPPage } from "./ExternalCalendarRSVPPage";

const projection = {
  attendee_version: 4,
  capability_expires_at: "2026-08-03T03:00:00Z",
  ends_at: "2026-07-30T03:00:00Z",
  invitation_sequence: 2,
  response_requested: true,
  rsvp_state: "needs_action",
  starts_at: "2026-07-30T02:00:00Z",
  timezone: "Asia/Ho_Chi_Minh",
  title: "English review",
} as const;

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    status,
  });
}

function renderPage(fetchMock: ReturnType<typeof vi.fn>) {
  vi.stubGlobal("fetch", fetchMock);
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <ExternalCalendarRSVPPage />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  window.history.replaceState({}, "", "/");
});

describe("ExternalCalendarRSVPPage", () => {
  it("erases fragment capabilities before resolving and requires explicit confirmation", async () => {
    const requests: Array<{ body: unknown; url: string }> = [];
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      const body = await request.clone().json();
      requests.push({ body, url: request.url });
      if ((body as { token?: string }).token === "resolve-secret") {
        expect(window.location.hash).toBe("");
        return jsonResponse(projection);
      }
      if ((body as { token?: string }).token === "respond-secret") {
        return jsonResponse({
          projection: {
            ...projection,
            attendee_version: 5,
            rsvp_state: "tentative",
          },
          replayed: false,
        });
      }
      throw new Error("Unexpected public RSVP request");
    });
    window.history.replaceState(
      { preserved: true },
      "",
      "/calendar/respond#resolve_token=resolve-secret&respond_token=respond-secret&tracking=discard",
    );

    renderPage(fetchMock);

    expect(window.location.pathname).toBe("/calendar/respond");
    expect(window.location.hash).toBe("");
    expect(window.history.state).toEqual({ preserved: true });
    expect(await screen.findByText("English review")).toBeVisible();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Respond to invitation",
    );

    const submit = screen.getByRole("button", { name: "Send response" });
    expect(submit).toBeDisabled();
    fireEvent.click(screen.getByRole("radio", { name: "Tentative" }));
    fireEvent.change(
      screen.getByRole("textbox", { name: /^Note \(optional\)/u }),
      {
        target: { value: "I may arrive a few minutes late." },
      },
    );
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I confirm this is my response/u,
      }),
    );
    expect(submit).toBeEnabled();
    fireEvent.click(submit);

    expect(await screen.findByText("Response saved")).toBeVisible();
    expect(requests).toHaveLength(2);
    expect(requests.every(({ url }) => !url.includes("-secret"))).toBe(true);
    expect(requests[1]?.body).toMatchObject({
      expected_attendee_version: 4,
      idempotency_key: expect.stringMatching(/^external-rsvp:/u),
      note: "I may arrive a few minutes late.",
      state: "tentative",
      token: "respond-secret",
    });
  });

  it("collapses an unavailable capability into a generic public state", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          status: 404,
          title: "Unavailable",
          type: "https://tutorhub.test/problems/not-found",
        },
        404,
      ),
    );
    window.history.replaceState(
      {},
      "",
      "/calendar/respond#resolve_token=unknown-secret&respond_token=unknown-respond",
    );

    renderPage(fetchMock);

    expect(
      await screen.findByRole("heading", { name: "Invitation unavailable" }),
    ).toBeVisible();
    expect(document.body.textContent).not.toContain("unknown-secret");
    expect(document.body.textContent).not.toContain("not-found");
  });

  it("does not make a network request when either purpose-bound capability is missing", async () => {
    const fetchMock = vi.fn();
    window.history.replaceState(
      {},
      "",
      "/calendar/respond#resolve_token=resolve-only",
    );

    renderPage(fetchMock);

    expect(
      screen.getByRole("heading", { name: "Invitation unavailable" }),
    ).toBeVisible();
    await waitFor(() => expect(fetchMock).not.toHaveBeenCalled());
  });
});
