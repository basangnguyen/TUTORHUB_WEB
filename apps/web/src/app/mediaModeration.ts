import {
  APIRequestError,
  changeMediaParticipantRole,
  endMediaSpace,
  muteMediaParticipantMicrophone,
  removeMediaParticipant,
  rotateCSRFToken,
  setMediaSpaceLock,
  type MediaModerationResult,
  type MediaParticipantModerationRequest,
  type MediaParticipantRoleRequest,
  type MediaProviderConvergenceProblem,
  type MediaProviderEffectStatus,
  type MediaSpace,
  type MediaSpaceLockRequest,
} from "@tutorhub/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useRef, useState } from "react";
import type {
  ClassroomModerationAction,
  ClassroomModerationControlsModel,
  ClassroomModerationFailureReason,
  ClassroomModerationMutationState,
  ClassroomModerationParticipantOperations,
  ClassroomModerationProviderEffect,
} from "../features/media/ClassroomModerationControls";
import type { ClassroomSignalProjection } from "../features/media/classroomSignals";

export const mediaModerationQueryKeys = {
  space: (tenantID: string, spaceID: string) =>
    ["media-space-room", tenantID, spaceID] as const,
  signalScope: (tenantID: string, spaceID: string, roomInstanceID: string) =>
    ["media", tenantID, spaceID, "participants", roomInstanceID] as const,
};

export interface MediaModerationScope {
  readonly roomInstanceID: string;
  readonly expectedSpaceVersion: number;
  readonly expectedRoomInstanceVersion: number;
  readonly expectedProjectionVersion: number;
}

export type MediaModerationCommand =
  | (MediaModerationScope & {
      readonly action: "lock_room" | "unlock_room";
      readonly idempotencyKey: string;
      readonly locked: boolean;
    })
  | (MediaModerationScope & {
      readonly action: "promote_co_host" | "demote_co_host";
      readonly desiredRole: "co_host" | "attendee";
      readonly idempotencyKey: string;
      readonly targetParticipantKey: string;
    })
  | (MediaModerationScope & {
      readonly action: "remote_mute" | "remove_participant";
      readonly idempotencyKey: string;
      readonly targetParticipantKey: string;
    })
  | (MediaModerationScope & {
      readonly action: "end_room";
      readonly idempotencyKey: string;
    });

type MediaModerationMutationResult =
  | {
      readonly kind: "moderation";
      readonly result: MediaModerationResult;
    }
  | { readonly kind: "end"; readonly space: MediaSpace };

interface VersionedMutationState {
  readonly projectionVersion: number;
  readonly value: ClassroomModerationMutationState;
}

interface VersionedProviderEffect {
  readonly projectionVersion: number;
  readonly value: ClassroomModerationProviderEffect;
}

export interface UseMediaModerationControlsInput {
  readonly enabled: boolean;
  readonly tenantID: string;
  readonly spaceID: string;
  readonly roomInstanceID: string;
  readonly expectedSpaceVersion: number;
  readonly expectedRoomInstanceVersion: number;
  readonly projection: ClassroomSignalProjection | null;
  readonly onRoomEnded?: () => void;
}

interface ProjectedModerationCapabilities {
  readonly roomLocked: boolean;
  readonly canLockRoom: boolean;
  readonly canEndRoom: boolean;
  readonly participantOperations: readonly ClassroomModerationParticipantOperations[];
}

export function useMediaModerationControls({
  enabled,
  tenantID,
  spaceID,
  roomInstanceID,
  expectedSpaceVersion,
  expectedRoomInstanceVersion,
  projection,
  onRoomEnded,
}: UseMediaModerationControlsInput): ClassroomModerationControlsModel | null {
  const queryClient = useQueryClient();
  const idempotencyKeys = useRef(new Map<string, string>());
  const [mutationFeedback, setMutationFeedback] =
    useState<VersionedMutationState | null>(null);
  const [providerFeedback, setProviderFeedback] =
    useState<VersionedProviderEffect | null>(null);
  const [concealedProjectionVersion, setConcealedProjectionVersion] = useState<
    number | null
  >(null);

  const refreshScope = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({
        exact: true,
        queryKey: mediaModerationQueryKeys.space(tenantID, spaceID),
      }),
      queryClient.invalidateQueries({
        queryKey: mediaModerationQueryKeys.signalScope(
          tenantID,
          spaceID,
          roomInstanceID,
        ),
      }),
    ]);
  }, [queryClient, roomInstanceID, spaceID, tenantID]);

  const mutation = useMutation<
    MediaModerationMutationResult,
    Error,
    MediaModerationCommand
  >({
    mutationFn: async (command) => {
      const csrf = await rotateCSRFToken();
      if (command.action === "end_room") {
        const space = await endMediaSpace(
          tenantID,
          spaceID,
          buildEndMediaSpaceRequest(command),
          csrf.csrf_token,
        );
        if (space.id !== spaceID || space.status !== "ended") {
          throw new TypeError("The media-space end response is invalid.");
        }
        return { kind: "end", space };
      }

      const input = buildMediaModerationRequest(command);
      let result: MediaModerationResult;
      switch (command.action) {
        case "lock_room":
        case "unlock_room":
          result = await setMediaSpaceLock(
            tenantID,
            spaceID,
            input as MediaSpaceLockRequest,
            csrf.csrf_token,
          );
          break;
        case "promote_co_host":
        case "demote_co_host":
          result = await changeMediaParticipantRole(
            tenantID,
            spaceID,
            command.targetParticipantKey,
            input as MediaParticipantRoleRequest,
            csrf.csrf_token,
          );
          break;
        case "remote_mute":
          result = await muteMediaParticipantMicrophone(
            tenantID,
            spaceID,
            command.targetParticipantKey,
            input,
            csrf.csrf_token,
          );
          break;
        case "remove_participant":
          result = await removeMediaParticipant(
            tenantID,
            spaceID,
            command.targetParticipantKey,
            input,
            csrf.csrf_token,
          );
          break;
      }
      return {
        kind: "moderation",
        result: parseMediaModerationResult(result, spaceID, command),
      };
    },
    onMutate: (command) => {
      setMutationFeedback({
        projectionVersion: command.expectedProjectionVersion,
        value: commandState("submitting", command),
      });
      setProviderFeedback(null);
    },
    onSuccess: (response, command) => {
      if (response.kind === "end") {
        queryClient.setQueryData(
          mediaModerationQueryKeys.space(tenantID, spaceID),
          response.space,
        );
        setMutationFeedback({
          projectionVersion: command.expectedProjectionVersion,
          value: { status: "idle" },
        });
        setProviderFeedback({
          projectionVersion: command.expectedProjectionVersion,
          value: providerEffect("applied", command),
        });
        onRoomEnded?.();
        return;
      }

      const { result } = response;
      if (
        command.action === "remote_mute" &&
        result.provider_effect_status === "applied"
      ) {
        releaseModerationIdempotencyKey(
          idempotencyKeys.current,
          command.idempotencyKey,
        );
      }
      setMutationFeedback({
        projectionVersion: result.projection_version,
        value: { status: "idle" },
      });
      setProviderFeedback({
        projectionVersion: result.projection_version,
        value: providerEffect(result.provider_effect_status, command),
      });
    },
    onError: (error, command) => {
      const convergence =
        command.action === "end_room"
          ? committedEndConvergence(error, spaceID)
          : null;
      if (convergence) {
        setMutationFeedback({
          projectionVersion: command.expectedProjectionVersion,
          value: { status: "idle" },
        });
        setProviderFeedback({
          projectionVersion: command.expectedProjectionVersion,
          value: providerEffect(convergence.provider_effect_status, command),
        });
        onRoomEnded?.();
        return;
      }
      const reason = mediaModerationFailureReason(error);
      setMutationFeedback({
        projectionVersion: command.expectedProjectionVersion,
        value: commandState("failed", command, reason),
      });
      setProviderFeedback(null);
      if (
        error instanceof APIRequestError &&
        (error.status === 401 || error.status === 403 || error.status === 404)
      ) {
        setConcealedProjectionVersion(command.expectedProjectionVersion);
      }
    },
    onSettled: refreshScope,
  });

  const capabilities = useMemo(
    () => projectClassroomModerationCapabilities(projection),
    [projection],
  );
  if (
    !capabilities ||
    !projection ||
    projection.room_instance_id !== roomInstanceID
  ) {
    return null;
  }

  const projectionVersion = projection.projection_version;
  const mutationState =
    mutationFeedback?.projectionVersion === projectionVersion
      ? mutationFeedback.value
      : ({ status: "idle" } as const);
  const providerEffectState =
    providerFeedback?.projectionVersion === projectionVersion
      ? providerFeedback.value
      : ({ status: "idle" } as const);
  const concealed = concealedProjectionVersion === projectionVersion;
  const visibleCapabilities = concealed
    ? concealModerationCapabilities(capabilities)
    : capabilities;

  const run = (
    action: ClassroomModerationAction,
    options: {
      readonly locked?: boolean;
      readonly targetParticipantKey?: string;
    } = {},
  ): Promise<void> => {
    if (
      !enabled ||
      expectedSpaceVersion <= 0 ||
      expectedRoomInstanceVersion <= 0 ||
      !operationAuthorized(
        capabilities,
        projection.self_participant_key,
        action,
        options.targetParticipantKey,
      )
    ) {
      return Promise.reject(
        new TypeError("The moderation operation is not server-authorized."),
      );
    }
    const keyScope = [
      action,
      roomInstanceID,
      expectedSpaceVersion,
      expectedRoomInstanceVersion,
      projectionVersion,
      options.targetParticipantKey ?? "room",
      options.locked === undefined ? "" : String(options.locked),
    ].join(":");
    const idempotencyKey = stableModerationIdempotencyKey(
      idempotencyKeys.current,
      keyScope,
      action,
    );
    const scope = {
      roomInstanceID,
      expectedSpaceVersion,
      expectedRoomInstanceVersion,
      expectedProjectionVersion: projectionVersion,
      idempotencyKey,
    } as const;
    let command: MediaModerationCommand;
    switch (action) {
      case "lock_room":
      case "unlock_room":
        command = {
          ...scope,
          action,
          locked: options.locked === true,
        };
        break;
      case "promote_co_host":
      case "demote_co_host":
        command = {
          ...scope,
          action,
          desiredRole: action === "promote_co_host" ? "co_host" : "attendee",
          targetParticipantKey: options.targetParticipantKey ?? "",
        };
        break;
      case "remote_mute":
      case "remove_participant":
        command = {
          ...scope,
          action,
          targetParticipantKey: options.targetParticipantKey ?? "",
        };
        break;
      case "end_room":
        command = { ...scope, action };
        break;
    }
    return mutation.mutateAsync(command).then(() => undefined);
  };

  return {
    ...visibleCapabilities,
    mutationState,
    providerEffect: providerEffectState,
    onSetRoomLocked: (locked) =>
      run(locked ? "lock_room" : "unlock_room", { locked }),
    onEndRoom: () => run("end_room"),
    onPromoteCoHost: (targetParticipantKey) =>
      run("promote_co_host", { targetParticipantKey }),
    onDemoteCoHost: (targetParticipantKey) =>
      run("demote_co_host", { targetParticipantKey }),
    onRemoteMute: (targetParticipantKey) =>
      run("remote_mute", { targetParticipantKey }),
    onRemoveParticipant: (targetParticipantKey) =>
      run("remove_participant", { targetParticipantKey }),
  };
}

function committedEndConvergence(
  error: Error,
  expectedSpaceID: string,
): MediaProviderConvergenceProblem | null {
  if (!(error instanceof APIRequestError) || error.status !== 503) return null;
  const candidate = error.problem as
    | (Record<string, unknown> & Partial<MediaProviderConvergenceProblem>)
    | undefined;
  if (
    candidate?.code !== "media_provider_unavailable" ||
    candidate.business_committed !== true ||
    candidate.space_id !== expectedSpaceID ||
    candidate.resource_status !== "ended" ||
    !Number.isSafeInteger(candidate.resource_version) ||
    (candidate.provider_effect_status !== "pending" &&
      candidate.provider_effect_status !== "retryable_failed" &&
      candidate.provider_effect_status !== "permanent_failed")
  ) {
    return null;
  }
  return candidate as MediaProviderConvergenceProblem;
}

export function projectClassroomModerationCapabilities(
  projection: ClassroomSignalProjection | null,
): ProjectedModerationCapabilities | null {
  if (
    !projection ||
    typeof projection.room_locked !== "boolean" ||
    typeof projection.viewer_operations.can_lock_room !== "boolean" ||
    typeof projection.viewer_operations.can_end_room !== "boolean"
  ) {
    return null;
  }
  return {
    roomLocked: projection.room_locked,
    canLockRoom: projection.viewer_operations.can_lock_room,
    canEndRoom: projection.viewer_operations.can_end_room,
    participantOperations: projection.roster.map((participant) => {
      const operations = participant.moderation_operations;
      const complete = Boolean(
        operations &&
        typeof operations.can_promote_co_host === "boolean" &&
        typeof operations.can_demote_co_host === "boolean" &&
        typeof operations.can_remote_mute === "boolean" &&
        typeof operations.can_remove === "boolean",
      );
      const self =
        participant.participant_key === projection.self_participant_key;
      return {
        participantKey: participant.participant_key,
        canPromoteCoHost:
          !self && complete && operations?.can_promote_co_host === true,
        canDemoteCoHost:
          !self && complete && operations?.can_demote_co_host === true,
        canRemoteMute:
          !self && complete && operations?.can_remote_mute === true,
        canRemove: !self && complete && operations?.can_remove === true,
      };
    }),
  };
}

export function buildMediaModerationRequest(
  command: Exclude<MediaModerationCommand, { readonly action: "end_room" }>,
):
  | MediaParticipantModerationRequest
  | MediaSpaceLockRequest
  | MediaParticipantRoleRequest {
  const request: MediaParticipantModerationRequest = {
    expected_room_instance_id: command.roomInstanceID,
    expected_space_version: command.expectedSpaceVersion,
    expected_room_instance_version: command.expectedRoomInstanceVersion,
    expected_projection_version: command.expectedProjectionVersion,
    idempotency_key: command.idempotencyKey,
  };
  switch (command.action) {
    case "lock_room":
    case "unlock_room":
      return { ...request, locked: command.locked };
    case "promote_co_host":
    case "demote_co_host":
      return { ...request, desired_role: command.desiredRole };
    case "remote_mute":
    case "remove_participant":
      return request;
  }
}

export function buildEndMediaSpaceRequest(
  command: Extract<MediaModerationCommand, { readonly action: "end_room" }>,
): { readonly expected_version: number; readonly idempotency_key: string } {
  return {
    expected_version: command.expectedSpaceVersion,
    idempotency_key: command.idempotencyKey,
  };
}

export function mediaModerationFailureReason(
  error: unknown,
): ClassroomModerationFailureReason {
  if (!(error instanceof APIRequestError)) return "unknown";
  if (error.status === 401 || error.status === 403 || error.status === 404) {
    return "forbidden";
  }
  if (error.status === 409) return "conflict";
  if (error.status === 429) return "rate_limited";
  if (error.status === 503) return "provider_unavailable";
  return "unknown";
}

function parseMediaModerationResult(
  input: MediaModerationResult,
  expectedSpaceID: string,
  command: Exclude<MediaModerationCommand, { readonly action: "end_room" }>,
): MediaModerationResult {
  const providerStatuses: readonly MediaProviderEffectStatus[] = [
    "none",
    "pending",
    "applied",
    "retryable_failed",
    "permanent_failed",
  ];
  if (
    input.space_id !== expectedSpaceID ||
    input.room_instance_id !== command.roomInstanceID ||
    !Number.isSafeInteger(input.space_version) ||
    input.space_version < 1 ||
    !Number.isSafeInteger(input.room_instance_version) ||
    input.room_instance_version < command.expectedRoomInstanceVersion ||
    !Number.isSafeInteger(input.projection_version) ||
    input.projection_version < command.expectedProjectionVersion ||
    !providerStatuses.includes(input.provider_effect_status)
  ) {
    throw new TypeError("The media moderation response is invalid.");
  }
  const providerEffectRequired =
    command.action !== "lock_room" && command.action !== "unlock_room";
  if (
    (providerEffectRequired && input.provider_effect_status === "none") ||
    (!providerEffectRequired && input.provider_effect_status !== "none")
  ) {
    throw new TypeError("The media moderation provider effect is invalid.");
  }
  if (
    (command.action === "lock_room" || command.action === "unlock_room") &&
    (!("locked" in input) || input.locked !== command.locked)
  ) {
    throw new TypeError("The media-space lock response is invalid.");
  }
  if (
    "targetParticipantKey" in command &&
    (!("target_participant_key" in input) ||
      input.target_participant_key !== command.targetParticipantKey)
  ) {
    throw new TypeError("The media participant response target is invalid.");
  }
  if (
    (command.action === "promote_co_host" ||
      command.action === "demote_co_host") &&
    (!("target_instance_role" in input) ||
      input.target_instance_role !== command.desiredRole)
  ) {
    throw new TypeError("The media participant role response is invalid.");
  }
  return input;
}

function commandState(
  status: "submitting",
  command: MediaModerationCommand,
): ClassroomModerationMutationState;
function commandState(
  status: "failed",
  command: MediaModerationCommand,
  reason: ClassroomModerationFailureReason,
): ClassroomModerationMutationState;
function commandState(
  status: "submitting" | "failed",
  command: MediaModerationCommand,
  reason?: ClassroomModerationFailureReason,
): ClassroomModerationMutationState {
  const targetParticipantKey =
    "targetParticipantKey" in command
      ? command.targetParticipantKey
      : undefined;
  if (status === "failed") {
    return {
      status,
      action: command.action,
      reason: reason ?? "unknown",
      ...(targetParticipantKey ? { targetParticipantKey } : {}),
    };
  }
  return {
    status,
    action: command.action,
    ...(targetParticipantKey ? { targetParticipantKey } : {}),
  };
}

function providerEffect(
  status: MediaProviderEffectStatus | "applied",
  command: MediaModerationCommand,
): ClassroomModerationProviderEffect {
  if (status === "none") return { status: "idle" };
  const targetParticipantKey =
    "targetParticipantKey" in command
      ? command.targetParticipantKey
      : undefined;
  const mappedStatus =
    status === "pending"
      ? "pending"
      : status === "retryable_failed" || status === "permanent_failed"
        ? "reconcile_required"
        : "applied";
  return {
    status: mappedStatus,
    action: command.action,
    ...(targetParticipantKey ? { targetParticipantKey } : {}),
  };
}

function operationAuthorized(
  capabilities: ProjectedModerationCapabilities,
  selfParticipantKey: string,
  action: ClassroomModerationAction,
  targetParticipantKey?: string,
): boolean {
  if (action === "lock_room" || action === "unlock_room") {
    return capabilities.canLockRoom;
  }
  if (action === "end_room") return capabilities.canEndRoom;
  if (!targetParticipantKey || targetParticipantKey === selfParticipantKey) {
    return false;
  }
  const target = capabilities.participantOperations.find(
    (candidate) => candidate.participantKey === targetParticipantKey,
  );
  if (!target) return false;
  switch (action) {
    case "promote_co_host":
      return target.canPromoteCoHost;
    case "demote_co_host":
      return target.canDemoteCoHost;
    case "remote_mute":
      return target.canRemoteMute;
    case "remove_participant":
      return target.canRemove;
  }
}

function concealModerationCapabilities(
  capabilities: ProjectedModerationCapabilities,
): ProjectedModerationCapabilities {
  return {
    ...capabilities,
    canLockRoom: false,
    canEndRoom: false,
    participantOperations: capabilities.participantOperations.map(
      ({ participantKey }) => ({
        participantKey,
        canPromoteCoHost: false,
        canDemoteCoHost: false,
        canRemoteMute: false,
        canRemove: false,
      }),
    ),
  };
}

function stableModerationIdempotencyKey(
  keys: Map<string, string>,
  scope: string,
  action: ClassroomModerationAction,
): string {
  const existing = keys.get(scope);
  if (existing) return existing;
  if (keys.size >= 32) {
    const oldest = keys.keys().next().value as string | undefined;
    if (oldest) keys.delete(oldest);
  }
  const created = `media-moderation-${action}-${globalThis.crypto.randomUUID()}`;
  keys.set(scope, created);
  return created;
}

function releaseModerationIdempotencyKey(
  keys: Map<string, string>,
  idempotencyKey: string,
): void {
  for (const [scope, value] of keys) {
    if (value === idempotencyKey) {
      keys.delete(scope);
      return;
    }
  }
}
