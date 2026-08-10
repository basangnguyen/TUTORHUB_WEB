import {
  cancelMediaJoinAttempt,
  getMediaJoinAttempt,
  inviteMediaSpaceMember,
  listMediaAdmissions,
  listMediaSpaceMembers,
  mutateMediaSpaceMember,
  resolveMediaAdmission,
  rotateCSRFToken,
  type MediaAdmission,
  type MediaAdmissionMutationRequest,
  type MediaAdmissionQueue,
  type MediaJoinAttemptCancelRequest,
  type MediaSpaceMember,
  type MediaSpaceMemberInviteRequest,
  type MediaSpaceMemberList,
  type MediaSpaceMemberMutationRequest,
} from "@tutorhub/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { MediaJoinAttemptProjection } from "./mediaPrejoin";

const participantPollIntervalMilliseconds = 2_000;
const moderatorPollIntervalMilliseconds = 2_500;

export type MediaAdmissionItem = MediaAdmission;
export type MediaAdmissionPage = MediaAdmissionQueue;
export type MediaSpaceMemberItem = MediaSpaceMember;
export type MediaSpaceMemberPage = MediaSpaceMemberList;
export type CancelMediaJoinAttemptInput = MediaJoinAttemptCancelRequest;
export type ResolveMediaAdmissionInput = MediaAdmissionMutationRequest;
export type InviteMediaSpaceMemberInput = MediaSpaceMemberInviteRequest;
export type MutateMediaSpaceMemberInput = MediaSpaceMemberMutationRequest;

export const mediaLobbyQueryKeys = {
  joinAttempt: (
    tenantID: string,
    spaceID: string,
    attemptID: string,
    roomInstanceID?: string,
    expectedSpaceVersion?: number,
    expectedRoomInstanceVersion?: number,
  ) =>
    [
      "media",
      tenantID,
      spaceID,
      "join-attempt",
      attemptID,
      ...(roomInstanceID === undefined
        ? []
        : [roomInstanceID, expectedSpaceVersion, expectedRoomInstanceVersion]),
    ] as const,
  admissions: (
    tenantID: string,
    spaceID: string,
    roomInstanceID: string,
    expectedSpaceVersion?: number,
    expectedRoomInstanceVersion?: number,
  ) =>
    [
      "media",
      tenantID,
      spaceID,
      "admissions",
      roomInstanceID,
      ...(expectedSpaceVersion === undefined
        ? []
        : [expectedSpaceVersion, expectedRoomInstanceVersion]),
    ] as const,
  members: (tenantID: string, spaceID: string, expectedSpaceVersion?: number) =>
    [
      "media",
      tenantID,
      spaceID,
      "members",
      ...(expectedSpaceVersion === undefined ? [] : [expectedSpaceVersion]),
    ] as const,
};

export function mediaLobbyIdempotencyKey(action: string): string {
  const normalized = action
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32);
  return `media-${normalized || "command"}-${globalThis.crypto.randomUUID()}`;
}

export function useMediaJoinAttemptStatus(
  tenantID: string,
  spaceID: string,
  attemptID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  enabled: boolean,
) {
  return useQuery<MediaJoinAttemptProjection>({
    enabled:
      enabled &&
      expectedSpaceVersion > 0 &&
      expectedRoomInstanceVersion > 0 &&
      Boolean(tenantID && spaceID && attemptID && roomInstanceID),
    queryKey: mediaLobbyQueryKeys.joinAttempt(
      tenantID,
      spaceID,
      attemptID,
      roomInstanceID,
      expectedSpaceVersion,
      expectedRoomInstanceVersion,
    ),
    queryFn: async ({ signal }) =>
      (await getMediaJoinAttempt(
        tenantID,
        spaceID,
        attemptID,
        roomInstanceID,
        expectedSpaceVersion,
        expectedRoomInstanceVersion,
        { signal },
      )) as MediaJoinAttemptProjection,
    refetchInterval: (query) =>
      query.state.data?.status === "waiting"
        ? participantPollIntervalMilliseconds
        : false,
    refetchIntervalInBackground: false,
    retry: false,
  });
}

export function useCancelMediaJoinAttempt(tenantID: string, spaceID: string) {
  const queryClient = useQueryClient();
  return useMutation<
    MediaJoinAttemptProjection,
    Error,
    { attemptID: string; input: CancelMediaJoinAttemptInput }
  >({
    mutationFn: async ({ attemptID, input }) => {
      const csrf = await rotateCSRFToken();
      return (await cancelMediaJoinAttempt(
        tenantID,
        spaceID,
        attemptID,
        input,
        csrf.csrf_token,
      )) as MediaJoinAttemptProjection;
    },
    onSuccess: async (attempt) => {
      await queryClient.invalidateQueries({
        queryKey: mediaLobbyQueryKeys.joinAttempt(
          tenantID,
          spaceID,
          attempt.join_attempt_id,
        ),
      });
    },
  });
}

export function useMediaAdmissions(
  tenantID: string,
  spaceID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  enabled: boolean,
) {
  return useQuery<MediaAdmissionPage>({
    enabled: enabled && Boolean(tenantID && spaceID && roomInstanceID),
    queryKey: mediaLobbyQueryKeys.admissions(
      tenantID,
      spaceID,
      roomInstanceID,
      expectedSpaceVersion,
      expectedRoomInstanceVersion,
    ),
    queryFn: async ({ signal }) =>
      (await listMediaAdmissions(
        tenantID,
        spaceID,
        roomInstanceID,
        expectedSpaceVersion,
        expectedRoomInstanceVersion,
        { signal },
      )) as MediaAdmissionPage,
    refetchInterval: enabled ? moderatorPollIntervalMilliseconds : false,
    refetchIntervalInBackground: false,
    retry: false,
  });
}

export function useResolveMediaAdmission(
  tenantID: string,
  spaceID: string,
  roomInstanceID: string,
) {
  const queryClient = useQueryClient();
  return useMutation<
    MediaAdmissionItem,
    Error,
    {
      action: "admit" | "deny" | "restore";
      admissionID: string;
      input: ResolveMediaAdmissionInput;
    }
  >({
    mutationFn: async ({ action, admissionID, input }) => {
      const csrf = await rotateCSRFToken();
      return (await resolveMediaAdmission(
        action,
        tenantID,
        spaceID,
        admissionID,
        input,
        csrf.csrf_token,
      )) as MediaAdmissionItem;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: mediaLobbyQueryKeys.admissions(
          tenantID,
          spaceID,
          roomInstanceID,
        ),
      });
    },
  });
}

export function useMediaSpaceMembers(
  tenantID: string,
  spaceID: string,
  expectedSpaceVersion: number,
  enabled: boolean,
) {
  return useQuery<MediaSpaceMemberPage>({
    enabled: enabled && Boolean(tenantID && spaceID),
    queryKey: mediaLobbyQueryKeys.members(
      tenantID,
      spaceID,
      expectedSpaceVersion,
    ),
    queryFn: async ({ signal }) =>
      (await listMediaSpaceMembers(tenantID, spaceID, expectedSpaceVersion, {
        signal,
      })) as MediaSpaceMemberPage,
    retry: false,
  });
}

export function useInviteMediaSpaceMember(tenantID: string, spaceID: string) {
  const queryClient = useQueryClient();
  return useMutation<MediaSpaceMemberItem, Error, InviteMediaSpaceMemberInput>({
    mutationFn: async (input) => {
      const csrf = await rotateCSRFToken();
      return (await inviteMediaSpaceMember(
        tenantID,
        spaceID,
        input,
        csrf.csrf_token,
      )) as MediaSpaceMemberItem;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: mediaLobbyQueryKeys.members(tenantID, spaceID),
        }),
        queryClient.invalidateQueries({
          queryKey: ["media-space-prejoin", tenantID, spaceID],
        }),
        queryClient.invalidateQueries({
          queryKey: ["media-space-room", tenantID, spaceID],
        }),
      ]);
    },
  });
}

export function useMutateMediaSpaceMember(tenantID: string, spaceID: string) {
  const queryClient = useQueryClient();
  return useMutation<
    MediaSpaceMemberItem,
    Error,
    {
      action: "revoke" | "restore";
      input: MutateMediaSpaceMemberInput;
      userID: string;
    }
  >({
    mutationFn: async ({ action, input, userID }) => {
      const csrf = await rotateCSRFToken();
      return (await mutateMediaSpaceMember(
        action,
        tenantID,
        spaceID,
        userID,
        input,
        csrf.csrf_token,
      )) as MediaSpaceMemberItem;
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: mediaLobbyQueryKeys.members(tenantID, spaceID),
        }),
        queryClient.invalidateQueries({
          queryKey: ["media-space-prejoin", tenantID, spaceID],
        }),
        queryClient.invalidateQueries({
          queryKey: ["media-space-room", tenantID, spaceID],
        }),
      ]);
    },
  });
}
