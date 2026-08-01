import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createAvailabilityPollRequest,
  generatePollSlots,
  listAvailabilityPollsRequest,
  type CreateAvailabilityPollRequest,
} from "./availabilityPollManagement";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("availability poll management API", () => {
  it("scopes authenticated reads to the active tenant assertion", async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ polls: [] }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(listAvailabilityPollsRequest(tenantID)).resolves.toEqual({
      polls: [],
    });

    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.method).toBe("GET");
    expect(new URL(request.url).pathname).toBe(
      "/api/v1/calendar/availability-polls",
    );
    expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(tenantID);
  });

  it("rotates CSRF before create and sends generated contract fields", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ csrf_token: "csrf-poll-create" }))
      .mockResolvedValueOnce(
        jsonResponse({ id: "2f1df9c5-85f6-43a4-96a1-0f78660dd08a" }, 201),
      );
    vi.stubGlobal("fetch", fetchMock);
    const input: CreateAvailabilityPollRequest = {
      class_id: null,
      deadline_at: "2030-08-04T12:00:00Z",
      description: "Pick a time",
      duration_minutes: 60,
      idempotency_key: "e86375b2-3e7c-4bdc-85f2-8ed0f17f6d2f",
      participants: [],
      range_end: "2030-08-05",
      range_start: "2030-08-05",
      share_mode: "anyone_with_link",
      slot_granularity_minutes: 30,
      slots: [
        {
          ends_at: "2030-08-05T03:00:00Z",
          starts_at: "2030-08-05T02:00:00Z",
        },
      ],
      timezone: "UTC",
      title: "Study group",
      working_day_end: "17:00",
      working_day_start: "09:00",
    };

    await createAvailabilityPollRequest(tenantID, input);

    const csrfRequest = fetchMock.mock.calls[0]?.[0] as Request;
    const createRequest = fetchMock.mock.calls[1]?.[0] as Request;
    expect(new URL(csrfRequest.url).pathname).toBe("/api/v1/auth/csrf");
    expect(createRequest.method).toBe("POST");
    expect(createRequest.headers.get("X-CSRF-Token")).toBe("csrf-poll-create");
    expect(createRequest.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    await expect(createRequest.clone().json()).resolves.toMatchObject({
      share_mode: "anyone_with_link",
      slots: input.slots,
      timezone: "UTC",
    });
  });
});

describe("availability poll slot editor", () => {
  it("generates overlapping half-hour candidates with exact real durations", () => {
    const result = generatePollSlots({
      durationMinutes: 60,
      granularityMinutes: 30,
      rangeEnd: "2030-08-05",
      rangeStart: "2030-08-05",
      timezone: "UTC",
      workingEnd: "11:00",
      workingStart: "09:00",
    });

    expect(result.error).toBeNull();
    expect(result.slots).toHaveLength(3);
    for (const slot of result.slots) {
      expect(
        new Date(slot.ends_at).getTime() - new Date(slot.starts_at).getTime(),
      ).toBe(60 * 60 * 1000);
    }
  });

  it("skips nonexistent civil starts across a DST gap", () => {
    const result = generatePollSlots({
      durationMinutes: 30,
      granularityMinutes: 30,
      rangeEnd: "2030-03-10",
      rangeStart: "2030-03-10",
      timezone: "America/New_York",
      workingEnd: "04:00",
      workingStart: "01:30",
    });

    expect(result.error).toBeNull();
    expect(result.slots).toHaveLength(3);
    expect(result.slots.every((slot) => !slot.starts_at.includes("02:"))).toBe(
      true,
    );
  });

  it("rejects invalid timezones without generating fallback local slots", () => {
    expect(
      generatePollSlots({
        durationMinutes: 60,
        granularityMinutes: 30,
        rangeEnd: "2030-08-05",
        rangeStart: "2030-08-05",
        timezone: "local",
        workingEnd: "11:00",
        workingStart: "09:00",
      }),
    ).toEqual({ error: "invalid_timezone", slots: [] });
  });
});
