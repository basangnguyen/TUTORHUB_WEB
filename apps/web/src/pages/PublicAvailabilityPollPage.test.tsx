import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PublicAvailabilityPoll } from "@tutorhub/api-client";
import { I18nProvider } from "../app/i18n";
import { clearPublicAvailabilityPollToken } from "../app/publicAvailabilityPollToken";
import { PublicAvailabilityPollPage } from "./PublicAvailabilityPollPage";

const publicID = "8818c018-b6c5-4f44-a844-7cbec84a986d";
const slotID = "7d84f838-e788-4ae1-894a-a02984f58826";
const capabilityToken = "v1.fragment-only-broad-capability";
const responseToken = "v1.memory-only-response-capability";

const slot = {
  id: slotID,
  starts_at: "2030-08-05T02:00:00Z",
  ends_at: "2030-08-05T03:00:00Z",
  ordinal: 0,
} as const;
const secondSlot = {
  id: "ebec4de8-64af-4a52-b01e-b97013abb83d",
  starts_at: "2030-08-05T03:00:00Z",
  ends_at: "2030-08-05T04:00:00Z",
  ordinal: 1,
} as const;

const openPoll: PublicAvailabilityPoll = {
  deadline_at: "2030-08-04T12:00:00Z",
  description: "Choose a time for our study session.",
  my_response: null,
  public_id: publicID,
  ranked_slots: [
    {
      aggregate_bucket: "medium",
      available_count: null,
      cohort_satisfied: true,
      preferred_count: null,
      rank: 1,
      slot,
      unavailable_count: null,
    },
  ],
  slots: [slot],
  status: "open",
  timezone: "UTC",
  title: "Project study session",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderPage(queryClient = new QueryClient()) {
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <I18nProvider initialLanguage="en">
          <MemoryRouter initialEntries={[`/availability/${publicID}`]}>
            <Routes>
              <Route
                element={<PublicAvailabilityPollPage />}
                path="/availability/:publicId"
              />
            </Routes>
          </MemoryRouter>
        </I18nProvider>
      </QueryClientProvider>,
    ),
  };
}

afterEach(() => {
  cleanup();
  clearPublicAvailabilityPollToken();
  window.history.replaceState({}, "", "/");
  vi.unstubAllGlobals();
});

describe("public availability poll", () => {
  it("erases the fragment before resolving and keeps capabilities out of URLs and query keys", async () => {
    window.history.replaceState(
      { preserved: true },
      "",
      `/availability/${publicID}#token=${capabilityToken}`,
    );
    const hashesAtRequest: string[] = [];
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      hashesAtRequest.push(window.location.hash);
      if (request.url.endsWith("/availability-polls/resolve")) {
        return Promise.resolve(
          jsonResponse({
            poll: openPoll,
            response_token: responseToken,
            response_token_expires_at: "2030-08-05T01:30:00Z",
          }),
        );
      }
      if (request.url.endsWith("/availability-polls/respond")) {
        return request
          .clone()
          .json()
          .then((body: { answers: unknown }) =>
            jsonResponse({
              poll: {
                ...openPoll,
                my_response: {
                  answers: body.answers,
                  submitted_at: "2030-08-01T00:00:00Z",
                  version: 1,
                },
              },
            }),
          );
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { queryClient } = renderPage();

    expect(window.location.hash).toBe("");
    expect(window.history.state).toEqual({ preserved: true });
    expect(
      await screen.findByRole("heading", { name: "Project study session" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/Group fit: Good fit/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("radio", { name: "Preferred" }));
    fireEvent.click(screen.getByRole("button", { name: "Save choices" }));

    expect(
      await screen.findByText("Your choices have been saved."),
    ).toBeInTheDocument();
    expect(hashesAtRequest).toEqual(["", ""]);
    expect(
      queryClient
        .getQueryCache()
        .getAll()
        .map((query) => query.queryKey),
    ).toEqual([["public-availability-poll", publicID]]);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.url)).toEqual([
      `${window.location.origin}/api/v1/calendar/availability-polls/resolve`,
      `${window.location.origin}/api/v1/calendar/availability-polls/respond`,
    ]);
    for (const request of requests) {
      expect(request.url).not.toContain(capabilityToken);
      expect(request.url).not.toContain(responseToken);
      expect(request.credentials).toBe("omit");
      expect(request.cache).toBe("no-store");
      expect(request.referrerPolicy).toBe("no-referrer");
      expect(request.headers.get("Origin")).toBe(window.location.origin);
    }
    await expect(requests[0]?.clone().json()).resolves.toEqual({
      public_id: publicID,
      token: capabilityToken,
    });
    await expect(requests[1]?.clone().json()).resolves.toMatchObject({
      answers: [{ slot_id: slotID, state: "preferred" }],
      expected_response_version: 0,
      public_id: publicID,
      response_token: responseToken,
    });
  });

  it("renders privacy-safe unavailable, empty, and closed states", async () => {
    window.history.replaceState(
      {},
      "",
      `/availability/${publicID}#token=${capabilityToken}`,
    );
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        poll: { ...openPoll, slots: [], ranked_slots: [] },
        response_token: responseToken,
        response_token_expires_at: "2030-08-05T01:30:00Z",
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    renderPage();

    expect(
      await screen.findByRole("heading", { name: "No time slots yet" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("radio")).not.toBeInTheDocument();

    cleanup();
    clearPublicAvailabilityPollToken();
    window.history.replaceState(
      {},
      "",
      `/availability/${publicID}#token=${capabilityToken}`,
    );
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        poll: { ...openPoll, status: "closed" },
        response_token: responseToken,
        response_token_expires_at: "2030-08-05T01:30:00Z",
      }),
    );
    renderPage();

    expect(
      await screen.findByText(
        "This poll is closed and no longer accepts responses.",
      ),
    ).toBeInTheDocument();
    for (const radio of screen.getAllByRole("radio")) {
      expect(radio).toBeDisabled();
    }

    cleanup();
    clearPublicAvailabilityPollToken();
    window.history.replaceState(
      {},
      "",
      `/availability/${publicID}#token=${capabilityToken}`,
    );
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          type: "about:blank",
          title: "Not found",
          status: 404,
        },
        404,
      ),
    );
    renderPage();

    expect(
      await screen.findByRole("heading", { name: "Poll unavailable" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Not found")).not.toBeInTheDocument();
  });

  it("keeps native radio controls keyboard-addressable after loading", async () => {
    window.history.replaceState(
      {},
      "",
      `/availability/${publicID}#token=${capabilityToken}`,
    );
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          poll: openPoll,
          response_token: responseToken,
          response_token_expires_at: "2030-08-05T01:30:00Z",
        }),
      ),
    );

    renderPage();

    const preferred = await screen.findByRole("radio", { name: "Preferred" });
    preferred.focus();
    expect(preferred).toHaveFocus();
    expect(preferred).toHaveAttribute("name", `availability-${slotID}`);
    expect(screen.getByText(/Tab and the arrow keys/)).toBeInTheDocument();
  });

  it("paints one answer state across desktop slots without replacing keyboard controls", async () => {
    window.history.replaceState(
      {},
      "",
      `/availability/${publicID}#token=${capabilityToken}`,
    );
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          poll: {
            ...openPoll,
            slots: [slot, secondSlot],
            ranked_slots: [
              ...openPoll.ranked_slots,
              {
                ...openPoll.ranked_slots[0],
                rank: 2,
                slot: secondSlot,
              },
            ],
          },
          response_token: responseToken,
          response_token_expires_at: "2030-08-05T01:30:00Z",
        }),
      ),
    );

    renderPage();

    const preferred = await screen.findAllByRole("radio", {
      name: "Preferred",
    });
    const firstLabel = preferred[0]?.closest("label");
    const secondLabel = preferred[1]?.closest("label");
    expect(firstLabel).not.toBeNull();
    expect(secondLabel).not.toBeNull();

    fireEvent.pointerDown(firstLabel!, {
      button: 0,
      buttons: 1,
      pointerType: "mouse",
    });
    fireEvent.pointerEnter(secondLabel!, {
      buttons: 1,
      pointerType: "mouse",
    });
    fireEvent.pointerUp(window, { button: 0, pointerType: "mouse" });

    expect(preferred[0]).toBeChecked();
    expect(preferred[1]).toBeChecked();
  });
});
