// @vitest-environment jsdom

import {
  APIRequestError,
  type MediaProviderConvergenceProblem,
} from "@tutorhub/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ClassroomSignalProjection } from "../features/media/classroomSignals";
import {
  buildEndMediaSpaceRequest,
  buildMediaModerationRequest,
  mediaModerationFailureReason,
  mediaModerationQueryKeys,
  projectClassroomModerationCapabilities,
  useMediaModerationControls,
  type MediaModerationCommand,
} from "./mediaModeration";

const apiMocks = vi.hoisted(() => ({
  changeRole: vi.fn(),
  endSpace: vi.fn(),
  muteMicrophone: vi.fn(),
  removeParticipant: vi.fn(),
  rotateCSRF: vi.fn(),
  setLock: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/api-client")>();
  return {
    ...original,
    changeMediaParticipantRole: apiMocks.changeRole,
    endMediaSpace: apiMocks.endSpace,
    muteMediaParticipantMicrophone: apiMocks.muteMicrophone,
    removeMediaParticipant: apiMocks.removeParticipant,
    rotateCSRFToken: apiMocks.rotateCSRF,
    setMediaSpaceLock: apiMocks.setLock,
  };
});

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const selfParticipantKey = "018f4c7b-9b0a-7a34-8a4c-96d26cb87221";
const targetParticipantID = "028f4c7b-9b0a-7a34-8a4c-96d26cb87222";

const projection: ClassroomSignalProjection = {
  room_instance_id: roomInstanceID,
  room_locked: false,
  projection_version: 9,
  last_signal_sequence: 12,
  self_participant_key: selfParticipantKey,
  viewer_operations: {
    can_raise_hand: true,
    can_send_reaction: true,
    can_moderate_hands: true,
    can_lock_room: true,
    can_end_room: true,
  },
  roster: [
    {
      participant_key: selfParticipantKey,
      roster_sequence: 1,
      display_name: "Host",
      instance_role: "host",
      connection_state: "connected",
      // Even a malformed server projection must never expose self-moderation.
      moderation_operations: {
        can_promote_co_host: true,
        can_demote_co_host: true,
        can_remote_mute: true,
        can_remove: true,
      },
    },
    {
      participant_key: targetParticipantID,
      roster_sequence: 2,
      display_name: "Student",
      instance_role: "attendee",
      connection_state: "connected",
      moderation_operations: {
        can_promote_co_host: true,
        can_demote_co_host: false,
        can_remote_mute: true,
        can_remove: true,
      },
    },
  ],
  raised_hands: [],
  reactions: { clusters: [], summary: [], hidden_cluster_count: 0 },
  server_time: "2030-08-03T00:00:00Z",
};

function testHarness() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  return {
    queryClient,
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  };
}

function renderModeration(
  currentProjection: ClassroomSignalProjection | null = projection,
  onRoomEnded?: () => void,
) {
  const harness = testHarness();
  const hook = renderHook(
    () =>
      useMediaModerationControls({
        enabled: true,
        tenantID,
        spaceID,
        roomInstanceID,
        expectedSpaceVersion: 7,
        expectedRoomInstanceVersion: 3,
        projection: currentProjection,
        onRoomEnded,
      }),
    { wrapper: harness.wrapper },
  );
  return { ...harness, ...hook };
}

function moderationResult(overrides: Record<string, unknown> = {}) {
  return {
    space_id: spaceID,
    room_instance_id: roomInstanceID,
    space_version: 8,
    room_instance_version: 4,
    projection_version: 9,
    provider_effect_status: "none",
    ...overrides,
  };
}

afterEach(() => {
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("P4-07 moderation projection boundary", () => {
  it("fails closed when any required room-level projection is absent", () => {
    expect(
      projectClassroomModerationCapabilities({
        ...projection,
        room_locked: undefined,
      }),
    ).toBeNull();
    expect(
      projectClassroomModerationCapabilities({
        ...projection,
        viewer_operations: {
          ...projection.viewer_operations,
          can_lock_room: undefined,
        },
      }),
    ).toBeNull();
    expect(
      projectClassroomModerationCapabilities({
        ...projection,
        viewer_operations: {
          ...projection.viewer_operations,
          can_end_room: undefined,
        },
      }),
    ).toBeNull();
  });

  it("never infers participant authority and always protects self", () => {
    const projected = projectClassroomModerationCapabilities({
      ...projection,
      roster: [
        projection.roster[0]!,
        { ...projection.roster[1]!, moderation_operations: undefined },
      ],
    });

    expect(projected?.participantOperations).toEqual([
      {
        participantKey: selfParticipantKey,
        canPromoteCoHost: false,
        canDemoteCoHost: false,
        canRemoteMute: false,
        canRemove: false,
      },
      {
        participantKey: targetParticipantID,
        canPromoteCoHost: false,
        canDemoteCoHost: false,
        canRemoteMute: false,
        canRemove: false,
      },
    ]);
  });
});

describe("P4-07 moderation request boundary", () => {
  const scope = {
    roomInstanceID,
    expectedSpaceVersion: 7,
    expectedRoomInstanceVersion: 3,
    expectedProjectionVersion: 9,
    idempotencyKey: "media-moderation-request-0001",
  } as const;

  it("builds exact lock, role and target requests with every concurrency guard", () => {
    expect(
      buildMediaModerationRequest({
        ...scope,
        action: "lock_room",
        locked: true,
      }),
    ).toEqual({
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 7,
      expected_room_instance_version: 3,
      expected_projection_version: 9,
      idempotency_key: "media-moderation-request-0001",
      locked: true,
    });
    expect(
      buildMediaModerationRequest({
        ...scope,
        action: "promote_co_host",
        desiredRole: "co_host",
        targetParticipantKey: targetParticipantID,
      }),
    ).toEqual({
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 7,
      expected_room_instance_version: 3,
      expected_projection_version: 9,
      idempotency_key: "media-moderation-request-0001",
      desired_role: "co_host",
    });
    expect(
      buildMediaModerationRequest({
        ...scope,
        action: "remove_participant",
        targetParticipantKey: targetParticipantID,
      }),
    ).toEqual({
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 7,
      expected_room_instance_version: 3,
      expected_projection_version: 9,
      idempotency_key: "media-moderation-request-0001",
    });
  });

  it("keeps end-room bound to the exact MediaSpace version", () => {
    expect(
      buildEndMediaSpaceRequest({
        ...scope,
        action: "end_room",
      }),
    ).toEqual({
      expected_version: 7,
      idempotency_key: "media-moderation-request-0001",
    });
  });
});

describe("P4-07 moderation mutation boundary", () => {
  it("rotates CSRF, reuses an idempotency key and waits for server refetch", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.setLock.mockResolvedValue(moderationResult({ locked: true }));
    const { queryClient, result } = renderModeration();
    const cachedSpace = { id: spaceID, version: 7, locked: false };
    const cachedSignal = { marker: "authoritative-server-snapshot" };
    queryClient.setQueryData(
      mediaModerationQueryKeys.space(tenantID, spaceID),
      cachedSpace,
    );
    queryClient.setQueryData(
      mediaModerationQueryKeys.signalScope(tenantID, spaceID, roomInstanceID),
      cachedSignal,
    );

    await act(async () => {
      await result.current?.onSetRoomLocked(true);
      await result.current?.onSetRoomLocked(true);
    });

    expect(apiMocks.rotateCSRF).toHaveBeenCalledTimes(2);
    expect(apiMocks.setLock).toHaveBeenCalledTimes(2);
    const firstRequest = apiMocks.setLock.mock.calls[0]?.[2];
    const secondRequest = apiMocks.setLock.mock.calls[1]?.[2];
    expect(firstRequest).toEqual({
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 7,
      expected_room_instance_version: 3,
      expected_projection_version: 9,
      idempotency_key: expect.stringMatching(/^media-moderation-lock_room-/),
      locked: true,
    });
    expect(secondRequest.idempotency_key).toBe(firstRequest.idempotency_key);
    expect(result.current?.providerEffect).toEqual({ status: "idle" });
    expect(apiMocks.setLock).toHaveBeenLastCalledWith(
      tenantID,
      spaceID,
      expect.any(Object),
      "fresh-csrf",
    );

    // No optimistic authority/state is written from the mutation ACK. The
    // projected controls stay on the current server snapshot until refetch.
    expect(result.current?.roomLocked).toBe(false);
    expect(
      queryClient.getQueryData(
        mediaModerationQueryKeys.space(tenantID, spaceID),
      ),
    ).toBe(cachedSpace);
    expect(
      queryClient.getQueryData(
        mediaModerationQueryKeys.signalScope(tenantID, spaceID, roomInstanceID),
      ),
    ).toBe(cachedSignal);
    await waitFor(() =>
      expect(
        queryClient.getQueryState(
          mediaModerationQueryKeys.space(tenantID, spaceID),
        )?.isInvalidated,
      ).toBe(true),
    );
  });

  it.each([403, 404])(
    "conceals all capabilities after a hidden-resource HTTP %s",
    async (status) => {
      apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
      apiMocks.setLock.mockRejectedValue(new APIRequestError(status));
      const { result } = renderModeration();

      await act(async () => {
        await expect(
          result.current?.onSetRoomLocked(true),
        ).rejects.toBeInstanceOf(APIRequestError);
      });

      expect(result.current?.mutationState).toMatchObject({
        status: "failed",
        action: "lock_room",
        reason: "forbidden",
      });
      expect(result.current?.canLockRoom).toBe(false);
      expect(result.current?.canEndRoom).toBe(false);
      expect(result.current?.participantOperations).toEqual(
        projection.roster.map(({ participant_key }) => ({
          participantKey: participant_key,
          canPromoteCoHost: false,
          canDemoteCoHost: false,
          canRemoteMute: false,
          canRemove: false,
        })),
      );
    },
  );

  it("rejects a command that the exact server projection did not authorize", async () => {
    const deniedProjection: ClassroomSignalProjection = {
      ...projection,
      viewer_operations: {
        ...projection.viewer_operations,
        can_lock_room: false,
      },
    };
    const { result } = renderModeration(deniedProjection);

    await expect(result.current?.onSetRoomLocked(true)).rejects.toThrow(
      "not server-authorized",
    );
    expect(apiMocks.rotateCSRF).not.toHaveBeenCalled();
    expect(apiMocks.setLock).not.toHaveBeenCalled();
  });

  it("surfaces provider reconciliation without claiming optimistic success", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.muteMicrophone.mockResolvedValue(
      moderationResult({
        target_participant_key: targetParticipantID,
        provider_effect_status: "retryable_failed",
      }),
    );
    const { result } = renderModeration();

    await act(async () => {
      await result.current?.onRemoteMute(targetParticipantID);
      await result.current?.onRemoteMute(targetParticipantID);
    });

    const firstRequest = apiMocks.muteMicrophone.mock.calls[0]?.[3];
    const secondRequest = apiMocks.muteMicrophone.mock.calls[1]?.[3];
    expect(secondRequest.idempotency_key).toBe(firstRequest.idempotency_key);

    expect(result.current?.providerEffect).toEqual({
      status: "reconcile_required",
      action: "remote_mute",
      targetParticipantKey: targetParticipantID,
    });
    expect(result.current?.mutationState).toEqual({ status: "idle" });
  });

  it("rotates the mute idempotency key only after confirmed provider application", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.muteMicrophone.mockResolvedValue(
      moderationResult({
        target_participant_key: targetParticipantID,
        provider_effect_status: "applied",
      }),
    );
    const { result } = renderModeration();

    await act(async () => {
      await result.current?.onRemoteMute(targetParticipantID);
      await result.current?.onRemoteMute(targetParticipantID);
    });

    const firstRequest = apiMocks.muteMicrophone.mock.calls[0]?.[3];
    const secondRequest = apiMocks.muteMicrophone.mock.calls[1]?.[3];
    expect(firstRequest.idempotency_key).toMatch(
      /^media-moderation-remote_mute-/,
    );
    expect(secondRequest.idempotency_key).not.toBe(
      firstRequest.idempotency_key,
    );
    expect(result.current?.providerEffect).toEqual({
      status: "applied",
      action: "remote_mute",
      targetParticipantKey: targetParticipantID,
    });
  });

  it("rejects provider-none responses for every provider-backed participant action", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.changeRole.mockResolvedValue(
      moderationResult({
        target_participant_key: targetParticipantID,
        target_instance_role: "co_host",
      }),
    );
    apiMocks.muteMicrophone.mockResolvedValue(
      moderationResult({ target_participant_key: targetParticipantID }),
    );
    apiMocks.removeParticipant.mockResolvedValue(
      moderationResult({ target_participant_key: targetParticipantID }),
    );
    const { result } = renderModeration();

    await act(async () => {
      await expect(
        result.current?.onPromoteCoHost(targetParticipantID),
      ).rejects.toThrow("provider effect is invalid");
      await expect(
        result.current?.onRemoteMute(targetParticipantID),
      ).rejects.toThrow("provider effect is invalid");
      await expect(
        result.current?.onRemoveParticipant(targetParticipantID),
      ).rejects.toThrow("provider effect is invalid");
    });

    expect(result.current?.providerEffect).toEqual({ status: "idle" });
    expect(result.current?.mutationState).toMatchObject({
      status: "failed",
      action: "remove_participant",
      reason: "unknown",
    });
  });

  it("accepts only an exact committed end convergence problem", async () => {
    const onRoomEnded = vi.fn();
    const problem: MediaProviderConvergenceProblem = {
      type: "urn:tutorhub:problem:http-503",
      title: "Media provider unavailable",
      status: 503,
      code: "media_provider_unavailable",
      detail: "Provider cleanup is still converging.",
      business_committed: true,
      space_id: spaceID,
      resource_status: "ended",
      resource_version: 8,
      provider_effect_status: "retryable_failed",
    };
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.endSpace.mockRejectedValue(new APIRequestError(503, problem));
    const { result } = renderModeration(projection, onRoomEnded);

    await act(async () => {
      await expect(result.current?.onEndRoom()).rejects.toBeInstanceOf(
        APIRequestError,
      );
    });

    expect(result.current?.mutationState).toEqual({ status: "idle" });
    expect(result.current?.providerEffect).toEqual({
      status: "reconcile_required",
      action: "end_room",
    });
    expect(onRoomEnded).toHaveBeenCalledTimes(1);
  });

  it("does not infer a committed end from a generic provider 503", async () => {
    const onRoomEnded = vi.fn();
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.endSpace.mockRejectedValue(new APIRequestError(503));
    const { result } = renderModeration(projection, onRoomEnded);

    await act(async () => {
      await expect(result.current?.onEndRoom()).rejects.toBeInstanceOf(
        APIRequestError,
      );
    });

    expect(result.current?.mutationState).toMatchObject({
      status: "failed",
      action: "end_room",
      reason: "provider_unavailable",
    });
    expect(result.current?.providerEffect).toEqual({ status: "idle" });
    expect(onRoomEnded).not.toHaveBeenCalled();
  });
});

describe("P4-07 moderation error mapping", () => {
  it.each([
    [403, "forbidden"],
    [404, "forbidden"],
    [409, "conflict"],
    [429, "rate_limited"],
    [503, "provider_unavailable"],
  ] as const)("maps HTTP %s to %s", (status, expected) => {
    expect(mediaModerationFailureReason(new APIRequestError(status))).toBe(
      expected,
    );
  });

  it("fails unknown and non-API errors closed", () => {
    expect(mediaModerationFailureReason(new APIRequestError(500))).toBe(
      "unknown",
    );
    expect(
      mediaModerationFailureReason(new TypeError("invalid response")),
    ).toBe("unknown");
  });
});

// Compile-time exhaustiveness guard for the request builder's discriminated
// union; it intentionally has no runtime behavior.
void (undefined as MediaModerationCommand | undefined);
