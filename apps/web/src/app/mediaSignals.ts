import {
  listMediaSpaceParticipants,
  mutateMediaSpaceSignal,
  rotateCSRFToken,
  type MediaParticipantSnapshot,
  type MediaReaction,
  type MediaSignalKind,
  type MediaSignalMutationRequest,
} from "@tutorhub/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import {
  identifyClassroomSignalSequenceGap,
  validateClassroomSignalSnapshot,
  type ClassroomSignalSnapshot,
} from "../features/media/classroomSignals";

export const MEDIA_SIGNAL_POLL_INTERVAL_MS = 2_000;

export const mediaSignalQueryKeys = {
  snapshot: (
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
      "participants",
      roomInstanceID,
      ...(expectedSpaceVersion === undefined
        ? []
        : [expectedSpaceVersion, expectedRoomInstanceVersion]),
    ] as const,
};

export interface MediaSignalMutationVariables {
  readonly expectedProjectionVersion: number;
  readonly idempotencyKey: string;
  readonly kind: MediaSignalKind;
  readonly reaction?: MediaReaction;
  readonly targetParticipantKey?: string;
}

export function mediaSignalIdempotencyKey(kind: MediaSignalKind): string {
  return `media-signal-${kind}-${globalThis.crypto.randomUUID()}`;
}

export function useMediaSignalSnapshot(
  tenantID: string,
  spaceID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  enabled: boolean,
) {
  const queryClient = useQueryClient();
  const previousSequence = useRef<number | null>(null);
  const queryKey = useMemo(
    () =>
      mediaSignalQueryKeys.snapshot(
        tenantID,
        spaceID,
        roomInstanceID,
        expectedSpaceVersion,
        expectedRoomInstanceVersion,
      ),
    [
      expectedRoomInstanceVersion,
      expectedSpaceVersion,
      roomInstanceID,
      spaceID,
      tenantID,
    ],
  );
  const query = useQuery<ClassroomSignalSnapshot>({
    enabled:
      enabled &&
      expectedSpaceVersion > 0 &&
      expectedRoomInstanceVersion > 0 &&
      Boolean(tenantID && spaceID && roomInstanceID),
    queryKey,
    queryFn: async ({ signal }) =>
      parseMediaSignalSnapshot(
        await listMediaSpaceParticipants(
          tenantID,
          spaceID,
          roomInstanceID,
          expectedSpaceVersion,
          expectedRoomInstanceVersion,
          { signal },
        ),
      ),
    refetchInterval: enabled ? MEDIA_SIGNAL_POLL_INTERVAL_MS : false,
    refetchIntervalInBackground: false,
    refetchOnReconnect: true,
    refetchOnWindowFocus: true,
    retry: false,
    structuralSharing: (oldData, newData) =>
      keepNewestMediaSignalSnapshot(
        oldData as ClassroomSignalSnapshot | undefined,
        newData as ClassroomSignalSnapshot,
      ),
  });

  useEffect(() => {
    if (!query.data) return;
    const previous = previousSequence.current;
    previousSequence.current = query.data.last_signal_sequence;
    if (
      previous !== null &&
      query.data.last_signal_sequence > previous &&
      identifyClassroomSignalSequenceGap(
        previous,
        query.data.last_signal_sequence,
      )
    ) {
      void queryClient.invalidateQueries({ exact: true, queryKey });
    }
  }, [query.data, queryClient, queryKey]);

  useEffect(() => {
    if (!enabled) return undefined;
    const resyncOnResume = () => {
      if (document.visibilityState !== "visible") return;
      void queryClient.invalidateQueries({ exact: true, queryKey });
    };
    document.addEventListener("visibilitychange", resyncOnResume);
    return () =>
      document.removeEventListener("visibilitychange", resyncOnResume);
  }, [enabled, queryClient, queryKey]);

  return query;
}

export function useMutateMediaSignal(
  tenantID: string,
  spaceID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
) {
  const queryClient = useQueryClient();
  const exactQueryKey = mediaSignalQueryKeys.snapshot(
    tenantID,
    spaceID,
    roomInstanceID,
    expectedSpaceVersion,
    expectedRoomInstanceVersion,
  );
  return useMutation<
    ClassroomSignalSnapshot,
    Error,
    MediaSignalMutationVariables
  >({
    mutationFn: async (variables) => {
      const input = buildMediaSignalMutationRequest(
        roomInstanceID,
        expectedSpaceVersion,
        expectedRoomInstanceVersion,
        variables,
      );
      const csrf = await rotateCSRFToken();
      return parseMediaSignalSnapshot(
        await mutateMediaSpaceSignal(tenantID, spaceID, input, csrf.csrf_token),
      );
    },
    onSuccess: (snapshot) => {
      queryClient.setQueryData<ClassroomSignalSnapshot>(
        exactQueryKey,
        (current) => keepNewestMediaSignalSnapshot(current, snapshot),
      );
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({
        exact: true,
        queryKey: exactQueryKey,
      });
    },
  });
}

export function buildMediaSignalMutationRequest(
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  variables: MediaSignalMutationVariables,
): MediaSignalMutationRequest {
  const base = {
    expected_room_instance_id: roomInstanceID,
    expected_space_version: expectedSpaceVersion,
    expected_room_instance_version: expectedRoomInstanceVersion,
    expected_projection_version: variables.expectedProjectionVersion,
    idempotency_key: variables.idempotencyKey,
    kind: variables.kind,
  } satisfies MediaSignalMutationRequest;

  if (variables.kind === "reaction") {
    if (!variables.reaction || variables.targetParticipantKey) {
      throw new TypeError(
        "A reaction requires only an allowlisted reaction value.",
      );
    }
    return { ...base, reaction: variables.reaction };
  }
  if (variables.kind === "hand_lower_one") {
    if (!variables.targetParticipantKey || variables.reaction) {
      throw new TypeError(
        "hand_lower_one requires only a target participant key.",
      );
    }
    return {
      ...base,
      target_participant_key: variables.targetParticipantKey,
    };
  }
  if (variables.reaction || variables.targetParticipantKey) {
    throw new TypeError(
      "This hand command cannot include a target or reaction.",
    );
  }
  return base;
}

export function parseMediaSignalSnapshot(
  input: MediaParticipantSnapshot | unknown,
): ClassroomSignalSnapshot {
  const validation = validateClassroomSignalSnapshot(input);
  if (!validation.valid) {
    throw new TypeError("The classroom signal snapshot is invalid.");
  }
  return validation.snapshot;
}

export function keepNewestMediaSignalSnapshot(
  current: ClassroomSignalSnapshot | undefined,
  incoming: ClassroomSignalSnapshot,
): ClassroomSignalSnapshot {
  if (!current) return incoming;
  if (incoming.projection_version < current.projection_version) return current;
  if (
    incoming.projection_version === current.projection_version &&
    incoming.last_signal_sequence < current.last_signal_sequence
  ) {
    return current;
  }
  return incoming;
}
