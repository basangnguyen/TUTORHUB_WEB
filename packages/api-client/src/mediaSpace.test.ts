import { describe, expect, it, vi } from "vitest";
import {
  cancelMediaSpace,
  createMediaSpace,
  endMediaSpace,
  getMediaSpace,
  startMediaSpace,
} from "./index";
import type {
  CreateMediaSpaceRequest,
  MediaSpace,
  MediaSpaceTransitionRequest,
} from "./index";

const tenantID = "7f44c093-1cb2-46ae-8285-779b78728524";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const meetingID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";

const space: MediaSpace = {
  id: spaceID,
  source: { kind: "study_meeting", study_meeting_id: meetingID },
  status: "scheduled",
  version: 1,
  active_room_instance: null,
  viewer_operations: {
    can_start: true,
    can_end: false,
    can_cancel: true,
  },
  created_at: "2030-08-03T00:00:00Z",
  updated_at: "2030-08-03T00:00:00Z",
};

const createInput = {
  source: {
    kind: "instant",
    title: "Ôn tập nhanh",
    duration_minutes: 45,
    timezone: "Asia/Ho_Chi_Minh",
  },
  idempotency_key: "media-create-0001",
} satisfies CreateMediaSpaceRequest;

const transitionInput = {
  expected_version: 1,
  idempotency_key: "media-start-00001",
  reason_code: "owner_started",
} satisfies MediaSpaceTransitionRequest;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

describe("media-space API", () => {
  it("binds create, read, and lifecycle transitions to the active tenant and CSRF session", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(jsonResponse(space)));
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await createMediaSpace(tenantID, createInput, "create-csrf", options);
    await getMediaSpace(tenantID, spaceID, options);
    await startMediaSpace(
      tenantID,
      spaceID,
      transitionInput,
      "start-csrf",
      options,
    );
    await endMediaSpace(
      tenantID,
      spaceID,
      { ...transitionInput, idempotency_key: "media-end-0000001" },
      "end-csrf",
      options,
    );
    await cancelMediaSpace(
      tenantID,
      spaceID,
      { ...transitionInput, idempotency_key: "media-cancel-00001" },
      "cancel-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.method)).toEqual([
      "POST",
      "GET",
      "POST",
      "POST",
      "POST",
    ]);
    expect(requests.map((request) => new URL(request.url).pathname)).toEqual([
      "/api/v1/media/spaces",
      `/api/v1/media/spaces/${spaceID}`,
      `/api/v1/media/spaces/${spaceID}/start`,
      `/api/v1/media/spaces/${spaceID}/end`,
      `/api/v1/media/spaces/${spaceID}/cancel`,
    ]);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
    }
    expect(
      requests.map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual(["create-csrf", null, "start-csrf", "end-csrf", "cancel-csrf"]);

    const body = (await requests[0]!.clone().json()) as Record<string, unknown>;
    expect(body).toEqual(createInput);
    for (const forbidden of [
      "tenant_id",
      "owner_user_id",
      "role",
      "provider_room_name",
      "provider_room_sid",
      "grant",
    ]) {
      expect(body).not.toHaveProperty(forbidden);
    }
  });

  it("fails before transport without an expected tenant", async () => {
    const fetchMock = vi.fn();
    await expect(
      createMediaSpace(" ", createInput, "csrf", { fetch: fetchMock }),
    ).rejects.toThrow(/active tenant ID is required/i);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("preserves stable feature-control problems", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          type: "urn:tutorhub:problem:http-403",
          title: "Feature disabled",
          status: 403,
          code: "feature_disabled",
        },
        403,
      ),
    );
    await expect(
      createMediaSpace(tenantID, createInput, "csrf", { fetch: fetchMock }),
    ).rejects.toMatchObject({
      status: 403,
      problem: { code: "feature_disabled" },
    });
  });
});
