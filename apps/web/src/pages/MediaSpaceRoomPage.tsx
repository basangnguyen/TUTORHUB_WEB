import { getMediaSpace } from "@tutorhub/api-client";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  clearMediaRoomEscrow,
  finalizeMediaRoomEscrowClaim,
  takeMediaRoomEscrow,
} from "../app/mediaPrejoin";
import { useSession } from "../app/session";
import { MediaLobbyPanel } from "../components/MediaLobbyPanel";
import {
  ClassroomLiveKitRoom,
  type ClassroomLiveKitRoomProps,
} from "../features/media/ClassroomLiveKitRoom";
import type { ClassroomConnectionStatus } from "../features/media/ClassroomMediaShell";

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

  const handleConnected = useCallback(() => {
    setRoomStatus("connected");
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

  const handleDisconnected = useCallback(() => {
    finishDisconnected("disconnected");
  }, [finishDisconnected]);

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
    connectionStatus: roomStatus,
    credential: handoff.credential,
    lobby,
    onConnected: handleConnected,
    onDisconnected: handleDisconnected,
    onLeave: handleLeave,
    onProviderError: handleProviderError,
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
