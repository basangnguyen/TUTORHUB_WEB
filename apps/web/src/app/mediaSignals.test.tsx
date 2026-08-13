// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildMediaSignalMutationRequest,
  mediaSignalIdempotencyKey,
  mediaSignalQueryKeys,
  parseMediaSignalSnapshot,
  useMediaSignalSnapshot,
  useMutateMediaSignal,
} from "./mediaSignals";

const apiMocks = vi.hoisted(() => ({
  listParticipants: vi.fn(),
  mutateSignal: vi.fn(),
  rotateCSRF: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/api-client")>();
  return {
    ...original,
    listMediaSpaceParticipants: apiMocks.listParticipants,
    mutateMediaSpaceSignal: apiMocks.mutateSignal,
    rotateCSRFToken: apiMocks.rotateCSRF,
  };
});

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const participantKey = "018f4c7b-9b0a-7a34-8a4c-96d26cb87221";

const snapshot = {
  room_instance_id: roomInstanceID,
  projection_version: 4,
  last_signal_sequence: 9,
  self_participant_key: participantKey,
  viewer_operations: {
    can_raise_hand: true,
    can_send_reaction: true,
    can_moderate_hands: false,
  },
  participants: [
    {
      participant_key: participantKey,
      roster_sequence: 1,
      display_name: "Student One",
      instance_role: "attendee",
      connection_state: "connected",
    },
  ],
  raised_hands: [],
  reaction_clusters: [],
  server_time: "2030-08-03T00:00:00Z",
} as const;

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

afterEach(() => {
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("P4-06 media signal query boundary", () => {
  it("does not fetch before the exact connected room scope is enabled", () => {
    const { wrapper } = testHarness();
    renderHook(
      () =>
        useMediaSignalSnapshot(tenantID, spaceID, roomInstanceID, 7, 3, false),
      { wrapper },
    );

    expect(apiMocks.listParticipants).not.toHaveBeenCalled();
  });

  it("polls the exact room projection and resyncs when a hidden tab resumes", async () => {
    apiMocks.listParticipants.mockResolvedValue(snapshot);
    const { wrapper } = testHarness();
    renderHook(
      () =>
        useMediaSignalSnapshot(tenantID, spaceID, roomInstanceID, 7, 3, true),
      { wrapper },
    );

    await waitFor(() => expect(apiMocks.listParticipants).toHaveBeenCalled());
    expect(apiMocks.listParticipants).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      roomInstanceID,
      7,
      3,
      { signal: expect.any(AbortSignal) },
    );
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "visible",
    });
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    await waitFor(() =>
      expect(apiMocks.listParticipants.mock.calls.length).toBeGreaterThan(1),
    );
  });

  it("rotates CSRF, sends exact concurrency data and stores only the server ACK", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.mutateSignal.mockResolvedValue({
      ...snapshot,
      projection_version: 5,
      last_signal_sequence: 10,
    });
    apiMocks.listParticipants.mockResolvedValue({
      ...snapshot,
      projection_version: 5,
      last_signal_sequence: 10,
    });
    const { queryClient, wrapper } = testHarness();
    const mutation = renderHook(
      () => useMutateMediaSignal(tenantID, spaceID, roomInstanceID, 7, 3),
      { wrapper },
    );

    await act(async () => {
      await mutation.result.current.mutateAsync({
        expectedProjectionVersion: 4,
        idempotencyKey: "media-signal-hand-raise-0001",
        kind: "hand_raise",
      });
    });

    expect(apiMocks.mutateSignal).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      {
        expected_room_instance_id: roomInstanceID,
        expected_space_version: 7,
        expected_room_instance_version: 3,
        expected_projection_version: 4,
        idempotency_key: "media-signal-hand-raise-0001",
        kind: "hand_raise",
      },
      "fresh-csrf",
    );
    expect(
      queryClient.getQueryData(
        mediaSignalQueryKeys.snapshot(tenantID, spaceID, roomInstanceID, 7, 3),
      ),
    ).toMatchObject({ projection_version: 5, last_signal_sequence: 10 });
  });

  it("builds only valid discriminator combinations", () => {
    expect(
      buildMediaSignalMutationRequest(roomInstanceID, 7, 3, {
        expectedProjectionVersion: 4,
        idempotencyKey: "media-signal-reaction-0001",
        kind: "reaction",
        reaction: "clap",
      }),
    ).toMatchObject({ kind: "reaction", reaction: "clap" });
    expect(() =>
      buildMediaSignalMutationRequest(roomInstanceID, 7, 3, {
        expectedProjectionVersion: 4,
        idempotencyKey: "media-signal-invalid-0001",
        kind: "hand_lower_all",
        targetParticipantKey: participantKey,
      }),
    ).toThrow(TypeError);
  });

  it("rejects malformed snapshots and creates no actor-bearing key", () => {
    expect(() =>
      parseMediaSignalSnapshot({
        ...snapshot,
        self_participant_key: "email@example.test",
      }),
    ).toThrow(TypeError);
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
      "a860f06d-34f9-4c57-89f8-1541bfb3b6d7",
    );
    expect(mediaSignalIdempotencyKey("hand_raise")).toBe(
      "media-signal-hand_raise-a860f06d-34f9-4c57-89f8-1541bfb3b6d7",
    );
  });
});
