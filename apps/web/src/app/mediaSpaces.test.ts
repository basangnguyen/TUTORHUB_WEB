import { APIRequestError, type MediaSpace } from "@tutorhub/api-client";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MediaSpaceNotReadyError, launchMediaSpace } from "./mediaSpaces";

const apiMocks = vi.hoisted(() => ({
  create: vi.fn(),
  resolve: vi.fn(),
  rotateCSRF: vi.fn(),
  start: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/api-client")>();
  return {
    ...original,
    createMediaSpace: apiMocks.create,
    resolveMediaSpace: apiMocks.resolve,
    rotateCSRFToken: apiMocks.rotateCSRF,
    startMediaSpace: apiMocks.start,
  };
});

const tenantID = "7f44c093-1cb2-46ae-8285-779b78728524";
const sessionID = "9f8bf389-362c-48ca-8e67-53df4e558f4d";
const source = { kind: "class_session", class_session_id: sessionID } as const;
const scheduled: MediaSpace = {
  id: "c2dc1048-1d90-4c90-ae50-5fb436bfb607",
  source,
  status: "scheduled",
  version: 1,
  active_room_instance: null,
  recovery_room_instance: null,
  viewer_operations: {
    can_start: true,
    can_recover: false,
    can_end: false,
    can_cancel: true,
    can_manage_admissions: false,
    can_manage_invites: false,
  },
  created_at: "2026-08-16T01:00:00Z",
  updated_at: "2026-08-16T01:00:00Z",
};
const open: MediaSpace = { ...scheduled, status: "open", version: 2 };

describe("P4-12 media-space product launch", () => {
  beforeEach(() => {
    apiMocks.create.mockReset();
    apiMocks.resolve.mockReset();
    apiMocks.rotateCSRF.mockReset();
    apiMocks.start.mockReset();
  });

  it("lets an attendee resolve an already-open room without mutation", async () => {
    apiMocks.resolve.mockResolvedValue(open);

    await expect(
      launchMediaSpace(tenantID, { canStart: false, source }),
    ).resolves.toEqual(open);
    expect(apiMocks.create).not.toHaveBeenCalled();
    expect(apiMocks.start).not.toHaveBeenCalled();
    expect(apiMocks.rotateCSRF).not.toHaveBeenCalled();
  });

  it("lets an authorized host create and start the exact source", async () => {
    apiMocks.resolve.mockRejectedValue(new APIRequestError(404));
    apiMocks.rotateCSRF
      .mockResolvedValueOnce({ csrf_token: "create-csrf" })
      .mockResolvedValueOnce({ csrf_token: "start-csrf" });
    apiMocks.create.mockResolvedValue(scheduled);
    apiMocks.start.mockResolvedValue(open);

    await expect(
      launchMediaSpace(tenantID, { canStart: true, source }),
    ).resolves.toEqual(open);
    expect(apiMocks.create).toHaveBeenCalledWith(
      tenantID,
      expect.objectContaining({ source }),
      "create-csrf",
      { baseUrl: "/api" },
    );
    expect(apiMocks.start).toHaveBeenCalledWith(
      tenantID,
      scheduled.id,
      expect.objectContaining({
        expected_version: scheduled.version,
        reason_code: "product_launch",
      }),
      "start-csrf",
      { baseUrl: "/api" },
    );
  });

  it("never lets an attendee create a missing room", async () => {
    apiMocks.resolve.mockRejectedValue(new APIRequestError(404));

    await expect(
      launchMediaSpace(tenantID, { canStart: false, source }),
    ).rejects.toBeInstanceOf(MediaSpaceNotReadyError);
    expect(apiMocks.create).not.toHaveBeenCalled();
    expect(apiMocks.start).not.toHaveBeenCalled();
  });

  it("does not start a scheduled projection without server start authority", async () => {
    apiMocks.resolve.mockResolvedValue({
      ...scheduled,
      viewer_operations: { ...scheduled.viewer_operations, can_start: false },
    });

    await expect(
      launchMediaSpace(tenantID, { canStart: true, source }),
    ).rejects.toBeInstanceOf(MediaSpaceNotReadyError);
    expect(apiMocks.start).not.toHaveBeenCalled();
  });
});
