import { describe, expect, it, vi } from "vitest";
import {
  resolvePublicAvailabilityPoll,
  respondPublicAvailabilityPoll,
} from "./index";

const publicID = "8818c018-b6c5-4f44-a844-7cbec84a986d";

describe("public availability poll API", () => {
  it("uses no-store, credential-free POST bodies for resolve and respond", async () => {
    const poll = {
      deadline_at: "2030-08-04T12:00:00Z",
      description: "",
      my_response: null,
      public_id: publicID,
      ranked_slots: [],
      slots: [],
      status: "open",
      timezone: "UTC",
      title: "Study session",
    };
    const responses = [
      new Response(
        JSON.stringify({
          poll,
          response_token: "response-v1",
          response_token_expires_at: "2030-08-04T12:30:00Z",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
      new Response(JSON.stringify({ poll }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await resolvePublicAvailabilityPoll(
      { public_id: publicID, token: "broad-v1" },
      options,
    );
    await respondPublicAvailabilityPoll(
      {
        answers: [{ slot_id: publicID, state: "available" }],
        expected_response_version: 0,
        idempotency_key: "public-response:test-key",
        public_id: publicID,
        response_token: "response-v1",
      },
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.url)).toEqual([
      "https://web.example.test/api/v1/calendar/availability-polls/resolve",
      "https://web.example.test/api/v1/calendar/availability-polls/respond",
    ]);
    for (const request of requests) {
      expect(request.method).toBe("POST");
      expect(request.credentials).toBe("omit");
      expect(request.cache).toBe("no-store");
      expect(request.referrerPolicy).toBe("no-referrer");
      expect(request.headers.get("Origin")).toBe("https://web.example.test");
      expect(request.headers.get("X-CSRF-Token")).toBeNull();
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBeNull();
    }
    expect(requests[0]?.url).not.toContain("broad-v1");
    expect(requests[1]?.url).not.toContain("response-v1");
    await expect(requests[0]?.clone().json()).resolves.toEqual({
      public_id: publicID,
      token: "broad-v1",
    });
    await expect(requests[1]?.clone().json()).resolves.toMatchObject({
      public_id: publicID,
      response_token: "response-v1",
    });
  });
});
