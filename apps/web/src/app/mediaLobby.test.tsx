// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  mediaLobbyIdempotencyKey,
  useCancelMediaJoinAttempt,
  useInviteMediaSpaceMember,
  useMediaAdmissions,
  useMediaJoinAttemptStatus,
  useMutateMediaSpaceMember,
  useResolveMediaAdmission,
} from "./mediaLobby";

const apiMocks = vi.hoisted(() => ({
  cancelJoinAttempt: vi.fn(),
  getJoinAttempt: vi.fn(),
  inviteMember: vi.fn(),
  listAdmissions: vi.fn(),
  listMembers: vi.fn(),
  mutateMember: vi.fn(),
  resolveAdmission: vi.fn(),
  rotateCSRF: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/api-client")>();
  return {
    ...original,
    cancelMediaJoinAttempt: apiMocks.cancelJoinAttempt,
    getMediaJoinAttempt: apiMocks.getJoinAttempt,
    inviteMediaSpaceMember: apiMocks.inviteMember,
    listMediaAdmissions: apiMocks.listAdmissions,
    listMediaSpaceMembers: apiMocks.listMembers,
    mutateMediaSpaceMember: apiMocks.mutateMember,
    resolveMediaAdmission: apiMocks.resolveAdmission,
    rotateCSRFToken: apiMocks.rotateCSRF,
  };
});

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const attemptID = "a860f06d-34f9-4c57-89f8-1541bfb3b6d7";
const admissionID = "d48a301d-c468-4f65-8da2-029fc379ee74";
const userID = "f680fd29-c7f1-4083-af9b-52ad1db14ba9";

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
});

describe("P4-04 media lobby query and mutation boundary", () => {
  it("builds opaque bounded idempotency keys", () => {
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue(
      "a860f06d-34f9-4c57-89f8-1541bfb3b6d7",
    );
    expect(mediaLobbyIdempotencyKey("Admit participant")).toBe(
      "media-admit-participant-a860f06d-34f9-4c57-89f8-1541bfb3b6d7",
    );
  });

  it("polls only the authenticated actor's exact join attempt", async () => {
    apiMocks.getJoinAttempt.mockResolvedValue({
      join_attempt_id: attemptID,
      participant_session_id: userID,
      room_instance_id: roomInstanceID,
      status: "waiting",
      version: 1,
    });
    const { wrapper } = testHarness();
    const { result } = renderHook(
      () =>
        useMediaJoinAttemptStatus(
          tenantID,
          spaceID,
          attemptID,
          roomInstanceID,
          4,
          2,
          true,
        ),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(apiMocks.getJoinAttempt).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      attemptID,
      roomInstanceID,
      4,
      2,
      { signal: expect.any(AbortSignal) },
    );
  });

  it("does not fetch participant or moderator state while disabled", () => {
    const { wrapper } = testHarness();
    renderHook(
      () =>
        useMediaJoinAttemptStatus(
          tenantID,
          spaceID,
          attemptID,
          roomInstanceID,
          4,
          2,
          false,
        ),
      { wrapper },
    );
    renderHook(
      () => useMediaAdmissions(tenantID, spaceID, roomInstanceID, 4, 2, false),
      { wrapper },
    );
    expect(apiMocks.getJoinAttempt).not.toHaveBeenCalled();
    expect(apiMocks.listAdmissions).not.toHaveBeenCalled();
  });

  it("rotates CSRF and forwards exact optimistic-concurrency inputs", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    apiMocks.cancelJoinAttempt.mockResolvedValue({
      join_attempt_id: attemptID,
      participant_session_id: userID,
      room_instance_id: roomInstanceID,
      status: "cancelled",
      version: 2,
    });
    apiMocks.resolveAdmission.mockResolvedValue({
      id: admissionID,
      status: "admitted",
      version: 2,
      display_name: "Student One",
      created_at: "2030-08-03T00:00:00Z",
    });
    const { wrapper } = testHarness();
    const cancel = renderHook(
      () => useCancelMediaJoinAttempt(tenantID, spaceID),
      { wrapper },
    );
    const resolve = renderHook(
      () => useResolveMediaAdmission(tenantID, spaceID, roomInstanceID),
      { wrapper },
    );

    await act(async () => {
      await cancel.result.current.mutateAsync({
        attemptID,
        input: {
          expected_space_version: 4,
          expected_room_instance_id: roomInstanceID,
          expected_room_instance_version: 3,
          expected_admission_version: 1,
          idempotency_key: "media-cancel-attempt-0001",
        },
      });
      await resolve.result.current.mutateAsync({
        action: "admit",
        admissionID,
        input: {
          expected_space_version: 4,
          expected_room_instance_id: roomInstanceID,
          expected_room_instance_version: 3,
          expected_admission_version: 1,
          idempotency_key: "media-admit-request-0001",
        },
      });
    });

    expect(apiMocks.cancelJoinAttempt).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      attemptID,
      expect.objectContaining({
        expected_space_version: 4,
        expected_room_instance_version: 3,
        expected_admission_version: 1,
      }),
      "fresh-csrf",
    );
    expect(apiMocks.resolveAdmission).toHaveBeenCalledWith(
      "admit",
      tenantID,
      spaceID,
      admissionID,
      expect.objectContaining({ expected_admission_version: 1 }),
      "fresh-csrf",
    );
  });

  it("keeps invite and member lifecycle commands server-resolved", async () => {
    apiMocks.rotateCSRF.mockResolvedValue({ csrf_token: "fresh-csrf" });
    const member = {
      user_id: userID,
      display_name: "Student One",
      status: "active",
      version: 1,
      created_at: "2030-08-03T00:00:00Z",
      updated_at: "2030-08-03T00:00:00Z",
    };
    apiMocks.inviteMember.mockResolvedValue(member);
    apiMocks.mutateMember.mockResolvedValue({
      ...member,
      status: "revoked",
      version: 2,
    });
    const { wrapper } = testHarness();
    const invite = renderHook(
      () => useInviteMediaSpaceMember(tenantID, spaceID),
      { wrapper },
    );
    const mutate = renderHook(
      () => useMutateMediaSpaceMember(tenantID, spaceID),
      { wrapper },
    );

    await act(async () => {
      await invite.result.current.mutateAsync({
        target_member_email: "student@example.test",
        expected_space_version: 4,
        idempotency_key: "media-invite-member-0001",
      });
      await mutate.result.current.mutateAsync({
        action: "revoke",
        userID,
        input: {
          expected_member_version: 1,
          expected_space_version: 4,
          idempotency_key: "media-revoke-member-0001",
          reason_code: "owner_revoked",
        },
      });
    });

    expect(apiMocks.inviteMember).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      expect.objectContaining({
        target_member_email: "student@example.test",
      }),
      "fresh-csrf",
    );
    expect(apiMocks.mutateMember).toHaveBeenCalledWith(
      "revoke",
      tenantID,
      spaceID,
      userID,
      expect.objectContaining({ expected_member_version: 1 }),
      "fresh-csrf",
    );
  });
});
