import { getMediaSpace } from "@tutorhub/api-client";
import { useQuery } from "@tanstack/react-query";
import { DisconnectReason } from "livekit-client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  clearMediaRoomEscrow,
  finalizeMediaRoomEscrowClaim,
  takeMediaRoomEscrow,
} from "../app/mediaPrejoin";
import {
  mediaSignalIdempotencyKey,
  useMediaSignalSnapshot,
  useMutateMediaSignal,
} from "../app/mediaSignals";
import { useMediaModerationControls } from "../app/mediaModeration";
import { useSession } from "../app/session";
import { MediaLobbyPanel } from "../components/MediaLobbyPanel";
import { MediaSpaceChatPanel } from "../components/MediaSpaceChatPanel";
import {
  ClassroomLiveKitRoom,
  type ClassroomLiveKitRoomProps,
} from "../features/media/ClassroomLiveKitRoom";
import type {
  ClassroomConnectionStatus,
  ClassroomSignalControls,
} from "../features/media/ClassroomMediaShell";
import {
  projectClassroomSignalSnapshot,
  type ClassroomReactionType,
} from "../features/media/classroomSignals";

export function MediaSpaceRoomPage() {
  const { spaceId, roomInstanceId } = useParams();
  const session = useSession();
  const tenantId = session.currentUser?.active_tenant?.id ?? "";
  const userId = session.currentUser?.user.id ?? "";
  const scopeKey = `${tenantId}\u0000${userId}\u0000${spaceId ?? ""}\u0000${roomInstanceId ?? ""}`;

  return (
    <MediaSpaceRoomSession
      key={scopeKey}
      roomInstanceId={roomInstanceId}
      spaceId={spaceId}
      tenantId={tenantId}
      userId={userId}
    />
  );
}

function MediaSpaceRoomSession({
  roomInstanceId,
  spaceId,
  tenantId,
  userId,
}: {
  roomInstanceId: string | undefined;
  spaceId: string | undefined;
  tenantId: string;
  userId: string;
}) {
  const navigate = useNavigate();
  const { t } = useI18n();
  const [roomStatus, setRoomStatus] =
    useState<ClassroomConnectionStatus>("connecting");
  const [roomError, setRoomError] = useState<TranslationKey | null>(null);
  const disconnectRecoveryActive = useRef(false);
  const [signalClock, setSignalClock] = useState({
    dataUpdatedAt: 0,
    now: 0,
  });
  const mediaSpace = useQuery({
    enabled: Boolean(spaceId && tenantId),
    queryKey: ["media-space-room", tenantId, spaceId],
    queryFn: ({ signal }) => getMediaSpace(tenantId, spaceId ?? "", { signal }),
    retry: false,
  });
  const [handoff, setHandoff] = useState(() => {
    if (!spaceId || !roomInstanceId || !tenantId || !userId) {
      clearMediaRoomEscrow();
      return null;
    }
    return takeMediaRoomEscrow({
      tenantId,
      userId,
      spaceId,
      roomInstanceId,
    });
  });
  const handoffMatchesScope = Boolean(
    handoff &&
    handoff.scope.tenantId === tenantId &&
    handoff.scope.userId === userId &&
    handoff.scope.spaceId === spaceId &&
    handoff.scope.roomInstanceId === roomInstanceId,
  );
  const handoffExpired = Boolean(
    handoff && credentialExpired(handoff.credential.expires_at),
  );
  const activeRoom = mediaSpace.data?.active_room_instance;
  const viewerOperations = mediaP404ViewerOperations(
    mediaSpace.data?.viewer_operations,
  );
  const signalScopeReady = Boolean(
    tenantId &&
    spaceId &&
    activeRoom?.id === roomInstanceId &&
    activeRoom?.status === "active",
  );
  const signalQuery = useMediaSignalSnapshot(
    tenantId,
    spaceId ?? "",
    roomInstanceId ?? "",
    mediaSpace.data?.version ?? 0,
    activeRoom?.version ?? 0,
    signalScopeReady && roomStatus === "connected",
  );
  const signalMutation = useMutateMediaSignal(
    tenantId,
    spaceId ?? "",
    roomInstanceId ?? "",
    mediaSpace.data?.version ?? 0,
    activeRoom?.version ?? 0,
  );
  const signalProjection = useMemo(() => {
    if (!signalQuery.data) return null;
    const snapshotServerTime = Date.parse(signalQuery.data.server_time);
    const elapsedClientTime =
      signalClock.dataUpdatedAt === signalQuery.dataUpdatedAt
        ? Math.max(0, signalClock.now - signalQuery.dataUpdatedAt)
        : 0;
    return projectClassroomSignalSnapshot(
      signalQuery.data,
      snapshotServerTime + elapsedClientTime,
    );
  }, [signalClock, signalQuery.data, signalQuery.dataUpdatedAt]);

  useEffect(() => {
    if (!signalQuery.data || roomStatus !== "connected") return undefined;
    const dataUpdatedAt = signalQuery.dataUpdatedAt;
    const timer = globalThis.setInterval(() => {
      setSignalClock({ dataUpdatedAt, now: Date.now() });
    }, 500);
    return () => globalThis.clearInterval(timer);
  }, [roomStatus, signalQuery.data, signalQuery.dataUpdatedAt]);

  const runSignalMutation = useCallback(
    async (
      kind:
        | "hand_raise"
        | "hand_lower"
        | "hand_lower_one"
        | "hand_lower_all"
        | "reaction",
      options: {
        reaction?: ClassroomReactionType;
        targetParticipantKey?: string;
      } = {},
    ) => {
      if (!signalProjection) {
        throw new Error("Classroom signal projection is not ready.");
      }
      await signalMutation.mutateAsync({
        expectedProjectionVersion: signalProjection.projection_version,
        idempotencyKey: mediaSignalIdempotencyKey(kind),
        kind,
        reaction: options.reaction,
        targetParticipantKey: options.targetParticipantKey,
      });
    },
    [signalMutation, signalProjection],
  );

  const signalControls = useMemo<ClassroomSignalControls>(
    () => ({
      error: signalQuery.isError,
      loading: signalQuery.isPending,
      mutating: signalMutation.isPending,
      projection: signalProjection,
      refreshing: signalQuery.isFetching && !signalQuery.isPending,
      onLowerAllHands: () => runSignalMutation("hand_lower_all"),
      onLowerHand: (targetParticipantKey) =>
        runSignalMutation("hand_lower_one", { targetParticipantKey }),
      onResync: async () => signalQuery.refetch(),
      onSendReaction: (reaction) => runSignalMutation("reaction", { reaction }),
      onToggleHand: () => {
        const ownHand = signalProjection?.raised_hands.some(
          ({ participant_key }) =>
            participant_key === signalProjection.self_participant_key,
        );
        return runSignalMutation(ownHand ? "hand_lower" : "hand_raise");
      },
    }),
    [
      runSignalMutation,
      signalMutation.isPending,
      signalProjection,
      signalQuery,
    ],
  );

  const handleConnected = useCallback(() => {
    setRoomStatus("connected");
    setRoomError(null);
    if (signalScopeReady) void signalQuery.refetch();
  }, [signalQuery, signalScopeReady]);

  const handleReconnecting = useCallback(() => {
    setRoomStatus("reconnecting");
    setRoomError(null);
  }, []);

  const finishDisconnected = useCallback(
    (status: Extract<ClassroomConnectionStatus, "disconnected" | "failed">) => {
      setRoomStatus(status);
      setRoomError(
        status === "failed"
          ? "media.p403.providerUnavailable"
          : "media.room.disconnectedDescription",
      );
      setHandoff(null);
      clearMediaRoomEscrow();
    },
    [],
  );

  const handleDisconnected = useCallback(
    (reason?: DisconnectReason) => {
      const outcome = classroomDisconnectOutcome(reason);
      finishDisconnected("disconnected");
      setRoomError(outcome.messageKey);
      if (disconnectRecoveryActive.current) return;
      disconnectRecoveryActive.current = true;
      void mediaSpace
        .refetch()
        .then(({ data }) => {
          if (
            outcome.reauthorize &&
            data?.status === "open" &&
            data.active_room_instance?.status === "active" &&
            spaceId
          ) {
            void navigate(`/app/media/spaces/${spaceId}/prejoin`, {
              replace: true,
            });
          }
        })
        .finally(() => {
          disconnectRecoveryActive.current = false;
        });
    },
    [finishDisconnected, mediaSpace, navigate, spaceId],
  );

  const handleProviderError = useCallback(() => {
    finishDisconnected("failed");
  }, [finishDisconnected]);

  const handleLeave = useCallback(() => {
    finishDisconnected("disconnected");
    void navigate(
      spaceId ? `/app/media/spaces/${spaceId}/prejoin` : "/app/home",
      { replace: true },
    );
  }, [finishDisconnected, navigate, spaceId]);

  const moderationControls = useMediaModerationControls({
    enabled: signalScopeReady && roomStatus === "connected",
    tenantID: tenantId,
    spaceID: spaceId ?? "",
    roomInstanceID: roomInstanceId ?? "",
    expectedSpaceVersion: mediaSpace.data?.version ?? 0,
    expectedRoomInstanceVersion: activeRoom?.version ?? 0,
    projection: signalProjection,
    onRoomEnded: handleLeave,
  });

  useEffect(() => {
    if (!handoff || mediaSpace.data?.status !== "ended") return undefined;
    const frame = globalThis.requestAnimationFrame(handleLeave);
    return () => globalThis.cancelAnimationFrame(frame);
  }, [handoff, handleLeave, mediaSpace.data?.status]);

  useEffect(() => {
    if (
      handoffMatchesScope &&
      !handoffExpired &&
      spaceId &&
      roomInstanceId &&
      tenantId &&
      userId
    ) {
      finalizeMediaRoomEscrowClaim({
        tenantId,
        userId,
        spaceId,
        roomInstanceId,
      });
    }
  }, [
    handoffExpired,
    handoffMatchesScope,
    roomInstanceId,
    spaceId,
    tenantId,
    userId,
  ]);

  useEffect(() => {
    if (handoff && (!handoffMatchesScope || handoffExpired)) {
      clearMediaRoomEscrow();
    }
  }, [handoff, handoffExpired, handoffMatchesScope]);

  useEffect(
    () => () => {
      clearMediaRoomEscrow();
    },
    [],
  );

  if (
    !spaceId ||
    !roomInstanceId ||
    !handoff ||
    !handoffMatchesScope ||
    handoffExpired
  ) {
    return (
      <main className="media-p403-room-recovery">
        <h1>{t("media.room.recoveryTitle")}</h1>
        <p>{t(roomError ?? "media.room.credentialMissing")}</p>
        <Link
          to={spaceId ? `/app/media/spaces/${spaceId}/prejoin` : "/app/home"}
        >
          {t("media.room.returnToPrejoin")}
        </Link>
      </main>
    );
  }

  const lobby =
    spaceId &&
    activeRoom?.id === roomInstanceId &&
    activeRoom.status === "active" ? (
      <MediaLobbyPanel
        enabled={viewerOperations.canManageAdmissions}
        roomInstanceID={activeRoom.id}
        roomInstanceVersion={activeRoom.version}
        spaceID={spaceId}
        spaceVersion={mediaSpace.data?.version ?? 0}
        tenantID={tenantId}
      />
    ) : undefined;

  const roomProps: ClassroomLiveKitRoomProps = {
    choices: handoff.choices,
    chat: (
      <MediaSpaceChatPanel
        actorID={userId}
        enabled={signalScopeReady}
        mediaSpaceID={spaceId}
        tenantID={tenantId}
      />
    ),
    connectionStatus: roomStatus,
    credential: handoff.credential,
    lobby,
    ...(moderationControls ? { moderation: moderationControls } : {}),
    onConnected: handleConnected,
    onReconnecting: handleReconnecting,
    onDisconnected: handleDisconnected,
    onLeave: handleLeave,
    onProviderError: handleProviderError,
    signals: signalControls,
  };

  return (
    <main className="media-p403-room media-p405-room" data-theme="dark">
      <ClassroomLiveKitRoom {...roomProps} />
      {roomError && (
        <section className="media-p403-alert" role="alert">
          <p>{t(roomError)}</p>
          <Link to={`/app/media/spaces/${spaceId}/prejoin`}>
            {t("media.room.returnToPrejoin")}
          </Link>
        </section>
      )}
    </main>
  );
}

function credentialExpired(expiresAt: string): boolean {
  const parsed = Date.parse(expiresAt);
  return !Number.isFinite(parsed) || parsed <= Date.now();
}

function mediaP404ViewerOperations(value: unknown): {
  canManageAdmissions: boolean;
} {
  if (typeof value !== "object" || value === null) {
    return { canManageAdmissions: false };
  }
  return {
    canManageAdmissions:
      (value as Record<string, unknown>).can_manage_admissions === true,
  };
}

function classroomDisconnectOutcome(reason?: DisconnectReason): {
  messageKey: TranslationKey;
  reauthorize: boolean;
} {
  switch (reason) {
    case DisconnectReason.PARTICIPANT_REMOVED:
      return {
        messageKey: "media.p409.participantRemoved",
        reauthorize: false,
      };
    case DisconnectReason.ROOM_DELETED:
    case DisconnectReason.ROOM_CLOSED:
      return { messageKey: "media.p409.roomEnded", reauthorize: false };
    case DisconnectReason.DUPLICATE_IDENTITY:
      return {
        messageKey: "media.p409.duplicateIdentity",
        reauthorize: false,
      };
    case DisconnectReason.CLIENT_INITIATED:
      return {
        messageKey: "media.room.disconnectedDescription",
        reauthorize: false,
      };
    default:
      return {
        messageKey: "media.p409.reauthorizationRequired",
        reauthorize: true,
      };
  }
}
