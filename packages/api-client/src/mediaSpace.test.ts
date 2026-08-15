import { describe, expect, it, vi } from "vitest";
import {
  cancelMediaJoinAttempt,
  cancelMediaSpace,
  changeMediaParticipantRole,
  createMediaSpaceJoinAttempt,
  createMediaSpace,
  endMediaSpace,
  getMediaJoinAttempt,
  getMediaSpace,
  inviteMediaSpaceMember,
  issueMediaSpaceJoinCredential,
  listMediaAdmissions,
  listMediaSpaceParticipants,
  listMediaSpaceMembers,
  muteMediaParticipantMicrophone,
  mutateMediaSpaceSignal,
  mutateMediaSpaceMember,
  removeMediaParticipant,
  recordMediaSpaceDiagnostic,
  recoverMediaSpace,
  resolveMediaAdmission,
  setMediaSpaceLock,
  startMediaSpace,
  exportMediaDiagnostics,
} from "./index";
import type {
  CreateMediaSpaceRequest,
  MediaAdmission,
  MediaAdmissionQueue,
  MediaJoinAttempt,
  MediaInstanceCredential,
  MediaDiagnosticExport,
  MediaParticipantSnapshot,
  MediaParticipantModerationResult,
  MediaSignalMutationRequest,
  MediaSpace,
  MediaSpaceLockResult,
  MediaSpaceMember,
  MediaSpaceMemberList,
  MediaSpaceTransitionRequest,
  RecoverMediaSpaceRequest,
} from "./index";

const tenantID = "7f44c093-1cb2-46ae-8285-779b78728524";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const meetingID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const participantSessionID = "f680fd29-c7f1-4083-af9b-52ad1db14ba9";
const joinAttemptID = "a860f06d-34f9-4c57-89f8-1541bfb3b6d7";
const admissionID = "19f9b26c-52bf-4ef5-9651-f284d24f3e6c";
const memberID = "da655aa5-46aa-46db-a282-d39698bb83c3";
const selfParticipantOpaqueID = "b825ac7c-4541-4ca5-bbd2-e874de5f5d4e";
const targetParticipantOpaqueID = "97c47c02-f571-4a63-94f0-66975de0377d";

const space: MediaSpace = {
  id: spaceID,
  source: { kind: "study_meeting", study_meeting_id: meetingID },
  status: "scheduled",
  version: 1,
  active_room_instance: null,
  recovery_room_instance: null,
  viewer_operations: {
    can_start: true,
    can_end: false,
    can_cancel: true,
    can_manage_admissions: true,
    can_manage_invites: true,
    can_recover: false,
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
const recoveryInput = {
  expected_space_version: 7,
  expected_room_instance_id: roomInstanceID,
  expected_room_instance_version: 3,
  idempotency_key: "media-recover-client-0001",
} satisfies RecoverMediaSpaceRequest;

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
  it("reads and mutates only the exact privacy-safe participant projection", async () => {
    const snapshot: MediaParticipantSnapshot = {
      room_instance_id: roomInstanceID,
      projection_version: 4,
      last_signal_sequence: 9,
      room_locked: false,
      self_participant_key: selfParticipantOpaqueID,
      viewer_operations: {
        can_raise_hand: true,
        can_send_reaction: true,
        can_moderate_hands: true,
        can_lock_room: true,
        can_end_room: true,
      },
      participants: [
        {
          participant_key: selfParticipantOpaqueID,
          roster_sequence: 1,
          display_name: "Teacher",
          instance_role: "host",
          connection_state: "connected",
          moderation_operations: {
            can_promote_co_host: false,
            can_demote_co_host: false,
            can_remote_mute: false,
            can_remove: false,
          },
        },
        {
          participant_key: targetParticipantOpaqueID,
          roster_sequence: 2,
          display_name: "Learner",
          instance_role: "attendee",
          connection_state: "reconnecting",
          moderation_operations: {
            can_promote_co_host: true,
            can_demote_co_host: false,
            can_remote_mute: true,
            can_remove: true,
          },
        },
      ],
      raised_hands: [
        {
          participant_key: targetParticipantOpaqueID,
          signal_sequence: 8,
          raised_at: "2030-08-03T00:00:08Z",
        },
      ],
      reaction_clusters: [
        {
          reaction: "clap",
          count: 2,
          first_signal_sequence: 8,
          last_signal_sequence: 9,
          accepted_at: "2030-08-03T00:00:09Z",
          expires_at: "2030-08-03T00:00:19Z",
        },
      ],
      server_time: "2030-08-03T00:00:10Z",
    };
    const requestNonce = "signal-lower-one-0001";
    const input = {
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 7,
      expected_room_instance_version: 3,
      expected_projection_version: 4,
      idempotency_key: requestNonce,
      kind: "hand_lower_one",
      target_participant_key: targetParticipantOpaqueID,
    } satisfies MediaSignalMutationRequest;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(snapshot))
      .mockResolvedValueOnce(
        jsonResponse({
          ...snapshot,
          projection_version: 5,
          last_signal_sequence: 10,
          raised_hands: [],
        }),
      );
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await expect(
      listMediaSpaceParticipants(
        tenantID,
        spaceID,
        roomInstanceID,
        7,
        3,
        options,
      ),
    ).resolves.toEqual(snapshot);
    await expect(
      mutateMediaSpaceSignal(tenantID, spaceID, input, "signal-csrf", options),
    ).resolves.toMatchObject({
      projection_version: 5,
      last_signal_sequence: 10,
      raised_hands: [],
    });

    const [read, mutate] = fetchMock.mock.calls.map(
      (call) => call[0] as Request,
    );
    expect(read!.method).toBe("GET");
    expect(new URL(read!.url).pathname).toBe(
      `/api/v1/media/spaces/${spaceID}/participants`,
    );
    expect(new URL(read!.url).searchParams.get("room_instance_id")).toBe(
      roomInstanceID,
    );
    expect(new URL(read!.url).searchParams.get("expected_space_version")).toBe(
      "7",
    );
    expect(
      new URL(read!.url).searchParams.get("expected_room_instance_version"),
    ).toBe("3");
    expect(mutate!.method).toBe("POST");
    expect(new URL(mutate!.url).pathname).toBe(
      `/api/v1/media/spaces/${spaceID}/signals`,
    );
    expect(mutate!.headers.get("X-CSRF-Token")).toBe("signal-csrf");
    expect(mutate!.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(tenantID);
    const body = (await mutate!.clone().json()) as Record<string, unknown>;
    expect(body).toEqual(input);
    for (const forbidden of [
      "tenant_id",
      "actor_user_id",
      "participant_session_id",
      "provider_participant_identity",
      "signal_sequence",
      "accepted_at",
      "instance_role",
    ]) {
      expect(body).not.toHaveProperty(forbidden);
    }
    expect(JSON.stringify(snapshot)).not.toMatch(
      /email|user_id|participant_session_id|join_attempt_id|provider_/,
    );
  });

  it("sends exact tenant-scoped moderation commands without client-supplied authority", async () => {
    const baseResult = {
      space_id: spaceID,
      room_instance_id: roomInstanceID,
      space_version: 8,
      room_instance_version: 4,
      projection_version: 5,
    } as const;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse({
          ...baseResult,
          locked: true,
          provider_effect_status: "none",
        } satisfies MediaSpaceLockResult),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          ...baseResult,
          target_participant_key: targetParticipantOpaqueID,
          target_participant_version: 2,
          target_instance_role: "co_host",
          provider_effect_status: "pending",
        } satisfies MediaParticipantModerationResult),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          ...baseResult,
          target_participant_key: targetParticipantOpaqueID,
          target_participant_version: 3,
          provider_effect_status: "retryable_failed",
        } satisfies MediaParticipantModerationResult),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          ...baseResult,
          target_participant_key: targetParticipantOpaqueID,
          target_participant_version: 4,
          provider_effect_status: "applied",
        } satisfies MediaParticipantModerationResult),
      );
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };
    const expected = {
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 7,
      expected_room_instance_version: 3,
      expected_projection_version: 4,
    } as const;

    await setMediaSpaceLock(
      tenantID,
      spaceID,
      {
        ...expected,
        idempotency_key: "room-lock-command-0001",
        locked: true,
        reason_code: "host_locked",
      },
      "lock-csrf",
      options,
    );
    await changeMediaParticipantRole(
      tenantID,
      spaceID,
      targetParticipantOpaqueID,
      {
        ...expected,
        idempotency_key: "role-promote-command-0001",
        desired_role: "co_host",
        reason_code: "host_promoted",
      },
      "role-csrf",
      options,
    );
    await muteMediaParticipantMicrophone(
      tenantID,
      spaceID,
      targetParticipantOpaqueID,
      {
        ...expected,
        idempotency_key: "remote-mute-command-0001",
        reason_code: "host_muted",
      },
      "mute-csrf",
      options,
    );
    await removeMediaParticipant(
      tenantID,
      spaceID,
      targetParticipantOpaqueID,
      {
        ...expected,
        idempotency_key: "remove-command-0001",
        reason_code: "host_removed",
      },
      "remove-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => new URL(request.url).pathname)).toEqual([
      `/api/v1/media/spaces/${spaceID}/lock`,
      `/api/v1/media/spaces/${spaceID}/participants/${targetParticipantOpaqueID}/role`,
      `/api/v1/media/spaces/${spaceID}/participants/${targetParticipantOpaqueID}/mute`,
      `/api/v1/media/spaces/${spaceID}/participants/${targetParticipantOpaqueID}/remove`,
    ]);
    expect(
      requests.map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual(["lock-csrf", "role-csrf", "mute-csrf", "remove-csrf"]);
    for (const request of requests) {
      expect(request.method).toBe("POST");
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
      const body = (await request.clone().json()) as Record<string, unknown>;
      expect(body).toMatchObject(expected);
      for (const forbidden of [
        "tenant_id",
        "actor_user_id",
        "actor_role",
        "target_user_id",
        "provider_room_name",
        "provider_participant_identity",
        "provider_grants",
      ]) {
        expect(body).not.toHaveProperty(forbidden);
      }
    }
  });

  it("creates an authoritative join attempt without client supplied grants or device data", async () => {
    const attempt: MediaJoinAttempt = {
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      join_attempt_id: joinAttemptID,
      status: "admitted",
      version: 1,
      instance_role: "attendee",
      can_publish_camera_microphone: true,
      can_share_screen: false,
      can_subscribe: true,
      created_at: "2030-08-03T00:00:00Z",
      updated_at: "2030-08-03T00:00:00Z",
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(attempt, 201));

    await expect(
      createMediaSpaceJoinAttempt(
        tenantID,
        spaceID,
        {
          join_attempt_id: joinAttemptID,
          expected_room_instance_id: roomInstanceID,
          expected_space_version: 1,
        },
        "attempt-csrf",
        { baseUrl: "https://web.example.test/api", fetch: fetchMock },
      ),
    ).resolves.toEqual(attempt);

    const request = fetchMock.mock.calls[0]![0] as Request;
    expect(request.method).toBe("POST");
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/media/spaces/${spaceID}/join-attempts`,
    );
    expect(request.credentials).toBe("include");
    expect(request.headers.get("X-CSRF-Token")).toBe("attempt-csrf");
    expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(tenantID);
    const body = (await request.clone().json()) as Record<string, unknown>;
    expect(body).toEqual({
      join_attempt_id: joinAttemptID,
      expected_room_instance_id: roomInstanceID,
      expected_space_version: 1,
    });
    for (const forbidden of [
      "tenant_id",
      "role",
      "grant",
      "device_id",
      "device_label",
      "provider_room_name",
      "provider_participant_identity",
    ]) {
      expect(body).not.toHaveProperty(forbidden);
    }
  });

  it("polls and cancels only the exact self lobby attempt", async () => {
    const syntheticCancelMutationID = ["admission", "cancel", "0001"].join("-");
    const waiting: MediaJoinAttempt = {
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      admission_request_id: admissionID,
      admission_version: 1,
      join_attempt_id: joinAttemptID,
      status: "waiting",
      version: 1,
      instance_role: "attendee",
      can_publish_camera_microphone: true,
      can_share_screen: false,
      can_subscribe: true,
      created_at: "2030-08-03T00:00:00Z",
      updated_at: "2030-08-03T00:00:00Z",
      expires_at: "2030-08-03T00:10:00Z",
    };
    const cancelled = { ...waiting, status: "cancelled" as const, version: 2 };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(waiting))
      .mockResolvedValueOnce(jsonResponse(cancelled));
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await expect(
      getMediaJoinAttempt(
        tenantID,
        spaceID,
        joinAttemptID,
        roomInstanceID,
        7,
        3,
        options,
      ),
    ).resolves.toEqual(waiting);
    await expect(
      cancelMediaJoinAttempt(
        tenantID,
        spaceID,
        joinAttemptID,
        {
          expected_space_version: 7,
          expected_room_instance_id: roomInstanceID,
          expected_room_instance_version: 3,
          expected_admission_version: 1,
          idempotency_key: syntheticCancelMutationID,
        },
        "cancel-csrf",
        options,
      ),
    ).resolves.toEqual(cancelled);

    const [poll, cancel] = fetchMock.mock.calls.map(
      (call) => call[0] as Request,
    );
    expect(poll!.method).toBe("GET");
    expect(new URL(poll!.url).searchParams.get("room_instance_id")).toBe(
      roomInstanceID,
    );
    expect(new URL(poll!.url).searchParams.get("expected_space_version")).toBe(
      "7",
    );
    expect(cancel!.method).toBe("POST");
    expect(cancel!.headers.get("X-CSRF-Token")).toBe("cancel-csrf");
    expect(await cancel!.clone().json()).toEqual({
      expected_space_version: 7,
      expected_room_instance_id: roomInstanceID,
      expected_room_instance_version: 3,
      expected_admission_version: 1,
      idempotency_key: syntheticCancelMutationID,
    });
  });

  it("uses bounded moderator and explicit-member contracts without client provider authority", async () => {
    const admission: MediaAdmission = {
      id: admissionID,
      status: "waiting",
      version: 1,
      display_name: "Learner",
      created_at: "2030-08-03T00:00:00Z",
      expires_at: "2030-08-03T00:10:00Z",
    };
    const queue: MediaAdmissionQueue = { items: [admission] };
    const member: MediaSpaceMember = {
      user_id: memberID,
      display_name: "Learner",
      status: "active",
      version: 1,
      created_at: "2030-08-03T00:00:00Z",
      updated_at: "2030-08-03T00:00:00Z",
    };
    const members: MediaSpaceMemberList = { items: [member] };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(queue))
      .mockResolvedValueOnce(
        jsonResponse({ ...admission, status: "denied", version: 2 }),
      )
      .mockResolvedValueOnce(jsonResponse(members))
      .mockResolvedValueOnce(jsonResponse(member))
      .mockResolvedValueOnce(
        jsonResponse({ ...member, status: "revoked", version: 2 }),
      );
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await listMediaAdmissions(tenantID, spaceID, roomInstanceID, 7, 3, options);
    await resolveMediaAdmission(
      "deny",
      tenantID,
      spaceID,
      admissionID,
      {
        expected_space_version: 7,
        expected_room_instance_id: roomInstanceID,
        expected_room_instance_version: 3,
        expected_admission_version: 1,
        idempotency_key: "admission-deny-00001",
        reason_code: "host_denied",
      },
      "deny-csrf",
      options,
    );
    await listMediaSpaceMembers(tenantID, spaceID, 7, options);
    await inviteMediaSpaceMember(
      tenantID,
      spaceID,
      {
        target_member_email: "learner@example.test",
        expected_space_version: 7,
        idempotency_key: "member-invite-00001",
      },
      "invite-csrf",
      options,
    );
    await mutateMediaSpaceMember(
      "revoke",
      tenantID,
      spaceID,
      memberID,
      {
        expected_space_version: 7,
        expected_member_version: 1,
        idempotency_key: "member-revoke-00001",
        reason_code: "owner_revoked",
      },
      "revoke-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.method)).toEqual([
      "GET",
      "POST",
      "GET",
      "POST",
      "POST",
    ]);
    const inviteBody = (await requests[3]!.clone().json()) as Record<
      string,
      unknown
    >;
    expect(inviteBody).toEqual({
      target_member_email: "learner@example.test",
      expected_space_version: 7,
      idempotency_key: "member-invite-00001",
    });
    for (const request of requests) {
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
      const body =
        request.method === "POST" ? await request.clone().json() : {};
      for (const forbidden of [
        "tenant_id",
        "role",
        "provider_room_name",
        "provider_participant_identity",
        "participant_session_id",
        "join_attempt_id",
      ]) {
        expect(body).not.toHaveProperty(forbidden);
      }
    }
  });

  it("issues an exact tenant-scoped credential without client supplied provider authority", async () => {
    const credential: MediaInstanceCredential = {
      access_token: "memory-only-token",
      server_url: "wss://media.example.test",
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      join_attempt_id: joinAttemptID,
      instance_role: "attendee",
      can_publish_camera_microphone: true,
      can_share_screen: false,
      can_subscribe: true,
      expires_at: "2030-08-03T00:05:00Z",
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(credential));

    await expect(
      issueMediaSpaceJoinCredential(
        tenantID,
        spaceID,
        { join_attempt_id: joinAttemptID },
        "credential-csrf",
        { baseUrl: "https://web.example.test/api", fetch: fetchMock },
      ),
    ).resolves.toEqual(credential);

    const request = fetchMock.mock.calls[0]![0] as Request;
    expect(request.method).toBe("POST");
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/media/spaces/${spaceID}/join-credentials`,
    );
    expect(request.credentials).toBe("include");
    expect(request.headers.get("X-CSRF-Token")).toBe("credential-csrf");
    expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(tenantID);
    const body = (await request.clone().json()) as Record<string, unknown>;
    expect(body).toEqual({ join_attempt_id: joinAttemptID });
    for (const forbidden of [
      "tenant_id",
      "provider_room_name",
      "provider_participant_identity",
      "role",
      "grant",
      "can_share_screen",
    ]) {
      expect(body).not.toHaveProperty(forbidden);
    }
  });

  it("records and exports only the reviewed P4-10 diagnostics contract", async () => {
    const diagnosticExport: MediaDiagnosticExport = {
      from: "2030-08-02T00:00:00Z",
      to: "2030-08-03T00:00:00Z",
      items: [],
      metrics: {
        join_attempts: 1,
        successful_joins: 1,
        join_success_rate: 1,
        p95_time_to_media_ms: 1800,
        reconnect_succeeded: 0,
        reconnect_failed: 0,
      },
      truncated: false,
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(jsonResponse(diagnosticExport));
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };
    await recordMediaSpaceDiagnostic(
      tenantID,
      spaceID,
      {
        event_id: "e2df2ea2-72db-4a13-a436-8df24d43ef60",
        room_instance_id: roomInstanceID,
        join_attempt_id: joinAttemptID,
        stage: "disconnected",
        outcome: "failed",
        error_code: "transport_disconnected",
        network_quality: "offline",
        media_path: "audio_only",
        duration_ms: 1200,
      },
      "record-csrf",
      options,
    );
    await expect(
      exportMediaDiagnostics(
        tenantID,
        {
          from: "2030-08-02T00:00:00Z",
          to: "2030-08-03T00:00:00Z",
          limit: 1000,
        },
        "export-csrf",
        options,
      ),
    ).resolves.toEqual(diagnosticExport);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => new URL(request.url).pathname)).toEqual([
      `/api/v1/media/spaces/${spaceID}/diagnostics`,
      "/api/v1/media/diagnostics/export",
    ]);
    expect(
      requests.map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual(["record-csrf", "export-csrf"]);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
      const body = JSON.stringify(await request.clone().json());
      expect(body).not.toContain("token");
      expect(body).not.toContain("device");
      expect(body).not.toContain("participant_session_id");
      expect(body).not.toContain("provider");
    }
  });

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
    await recoverMediaSpace(
      tenantID,
      spaceID,
      recoveryInput,
      "recover-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.method)).toEqual([
      "POST",
      "GET",
      "POST",
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
      `/api/v1/media/spaces/${spaceID}/recover`,
    ]);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
    }
    expect(
      requests.map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual([
      "create-csrf",
      null,
      "start-csrf",
      "end-csrf",
      "cancel-csrf",
      "recover-csrf",
    ]);

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
    expect(await requests[5]!.clone().json()).toEqual(recoveryInput);
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

  it("preserves the exact committed end convergence projection", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          type: "urn:tutorhub:problem:http-503",
          title: "Media provider unavailable",
          status: 503,
          code: "media_provider_unavailable",
          business_committed: true,
          space_id: spaceID,
          resource_status: "ended",
          resource_version: 8,
          provider_effect_status: "retryable_failed",
        },
        503,
      ),
    );

    await expect(
      endMediaSpace(
        tenantID,
        spaceID,
        { ...transitionInput, idempotency_key: "media-end-converge-0001" },
        "end-csrf",
        { fetch: fetchMock },
      ),
    ).rejects.toMatchObject({
      status: 503,
      problem: {
        code: "media_provider_unavailable",
        business_committed: true,
        space_id: spaceID,
        resource_status: "ended",
        resource_version: 8,
        provider_effect_status: "retryable_failed",
      },
    });
  });
});
