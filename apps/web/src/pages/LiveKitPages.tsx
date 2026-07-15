import "@livekit/components-styles";

import {
  ControlBar,
  GridLayout,
  LayoutContextProvider,
  LiveKitRoom,
  ParticipantTile,
  PreJoin,
  RoomAudioRenderer,
  useParticipants,
  useRoomContext,
  useTracks,
  type LocalUserChoices,
} from "@livekit/components-react";
import { APIRequestError } from "@tutorhub/api-client";
import { Room, RoomEvent, Track } from "livekit-client";
import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router-dom";
import { useClassDetail } from "../app/classes";
import { useI18n } from "../app/i18n";
import {
  hasMediaDeviceSupport,
  mediaErrorCode,
  readMediaRoomState,
  recordMediaEvent,
  requestMediaJoinSetup,
  type MediaRoomNavigationState,
} from "../app/media";
import { useSession } from "../app/session";

type JoinStatus = "idle" | "joining" | "failed";
type RoomStatus =
  "connecting" | "connected" | "reconnecting" | "disconnected" | "failed";

export function ClassroomPreJoinPage() {
  const { classId } = useParams();
  const navigate = useNavigate();
  const { t } = useI18n();
  const session = useSession();
  const activeTenant = session.currentUser?.active_tenant;
  const classroom = useClassDetail(activeTenant?.id, classId);
  const canPublish =
    session.currentUser?.permissions.includes("media.publish") ?? false;
  const supported = hasMediaDeviceSupport(
    typeof navigator === "undefined" ? undefined : navigator,
  );
  const [joinStatus, setJoinStatus] = useState<JoinStatus>("idle");
  const [joinError, setJoinError] = useState<string | null>(null);

  const joinRoom = async (choices: LocalUserChoices) => {
    if (!classId || joinStatus === "joining") {
      return;
    }
    const startedAt = performance.now();
    setJoinStatus("joining");
    setJoinError(null);

    try {
      const setup = await requestMediaJoinSetup(classId);
      void recordMediaEvent(classId, setup.csrfToken, {
        attempt_id: setup.credential.attempt_id,
        stage: "token",
        outcome: "succeeded",
        error_code: "",
        duration_ms: Math.round(performance.now() - startedAt),
      }).catch(() => undefined);
      const state: MediaRoomNavigationState = {
        ...setup,
        choices: {
          ...choices,
          audioEnabled: setup.credential.can_publish && choices.audioEnabled,
          videoEnabled: setup.credential.can_publish && choices.videoEnabled,
        },
      };
      void navigate(`/app/classrooms/${classId}/room`, { state });
    } catch (error) {
      setJoinStatus("failed");
      setJoinError(joinErrorMessage(error, t("media.prejoin.joinError")));
    }
  };

  if (!classId) {
    return <PreJoinFailure message={t("media.prejoin.invalidClass")} />;
  }

  return (
    <div className="page-content media-prejoin-page">
      <Link className="classroom-back-link" to={`/app/classrooms/${classId}`}>
        {t("media.prejoin.backToClass")}
      </Link>

      <header className="media-prejoin-heading">
        <p>{t("media.prejoin.kicker")}</p>
        <h1>{classroom.data?.title ?? t("media.prejoin.title")}</h1>
        <span>{t("media.prejoin.description")}</span>
      </header>

      {classroom.isError && (
        <section className="media-prejoin-notice" role="alert">
          <strong>{t("media.prejoin.classError")}</strong>
          <button onClick={() => void classroom.refetch()} type="button">
            {t("state.retry")}
          </button>
        </section>
      )}

      {!supported && canPublish ? (
        <PreJoinFailure message={t("media.prejoin.unsupported")} />
      ) : canPublish ? (
        <section
          aria-busy={joinStatus === "joining"}
          className="media-prejoin-workspace"
        >
          <div className="media-prejoin-preview" data-lk-theme="default">
            <PreJoin
              camLabel={t("media.prejoin.camera")}
              defaults={{
                audioEnabled: true,
                videoEnabled: true,
                username:
                  session.currentUser?.user.display_name ?? "TutorHub member",
              }}
              joinLabel={
                joinStatus === "joining"
                  ? t("media.prejoin.joining")
                  : t("media.prejoin.join")
              }
              micLabel={t("media.prejoin.microphone")}
              onError={(error) => setJoinError(joinErrorMessage(error, ""))}
              onSubmit={(choices) => void joinRoom(choices)}
              persistUserChoices={false}
              userLabel={t("media.prejoin.displayName")}
            />
          </div>
          <DeviceReadinessPanel />
        </section>
      ) : (
        <ListenOnlyPreJoin
          displayName={
            session.currentUser?.user.display_name ?? "TutorHub member"
          }
          isJoining={joinStatus === "joining"}
          onJoin={(choices) => void joinRoom(choices)}
        />
      )}

      {joinError && (
        <section className="media-prejoin-error" role="alert">
          <strong>{t("media.prejoin.cannotJoin")}</strong>
          <p>{joinError}</p>
          <button
            onClick={() => {
              setJoinError(null);
              setJoinStatus("idle");
            }}
            type="button"
          >
            {t("state.retry")}
          </button>
        </section>
      )}
    </div>
  );
}

export function ClassroomRoomPage() {
  const { classId } = useParams();
  const location = useLocation();
  const navigate = useNavigate();
  const { t } = useI18n();
  const [roomState] = useState(() => readMediaRoomState(location.state));
  const [room] = useState(
    () => new Room({ adaptiveStream: true, dynacast: true }),
  );
  const [status, setStatus] = useState<RoomStatus>("connecting");
  const [roomError, setRoomError] = useState<string | null>(null);
  const connectStartedAt = useRef(0);

  useEffect(() => {
    if (location.state) {
      void navigate(location.pathname, { replace: true, state: null });
    }
  }, [location.pathname, location.state, navigate]);

  useEffect(
    () => () => {
      void room.disconnect();
    },
    [room],
  );

  useEffect(() => {
    if (!classId || !roomState || tokenIsExpired(roomState)) {
      return;
    }
    connectStartedAt.current = performance.now();
    void recordMediaEvent(classId, roomState.csrfToken, {
      attempt_id: roomState.credential.attempt_id,
      stage: "connect",
      outcome: "started",
      error_code: "",
      duration_ms: 0,
    }).catch(() => undefined);
  }, [classId, roomState]);

  if (!classId || !roomState || tokenIsExpired(roomState)) {
    return (
      <RoomRecoveryState
        classId={classId}
        message={t("media.room.credentialMissing")}
      />
    );
  }

  const report = (
    stage: "connect" | "connected" | "media" | "disconnected" | "leave",
    outcome: "started" | "succeeded" | "failed",
    errorCode = "",
    durationMS = 0,
  ) => {
    void recordMediaEvent(classId, roomState.csrfToken, {
      attempt_id: roomState.credential.attempt_id,
      stage,
      outcome,
      error_code: errorCode,
      duration_ms: Math.max(0, Math.round(durationMS)),
    }).catch(() => undefined);
  };

  return (
    <main className="media-room-page" data-lk-theme="default">
      <LiveKitRoom
        audio={
          roomState.credential.can_publish && roomState.choices.audioEnabled
            ? { deviceId: roomState.choices.audioDeviceId || undefined }
            : false
        }
        className="media-livekit-root"
        connect
        connectOptions={{ autoSubscribe: true }}
        onConnected={() => {
          setStatus("connected");
          report(
            "connected",
            "succeeded",
            "",
            performance.now() - connectStartedAt.current,
          );
        }}
        onDisconnected={() => {
          setStatus("disconnected");
          report("disconnected", "succeeded");
        }}
        onError={(error) => {
          setStatus("failed");
          setRoomError(
            joinErrorMessage(error, t("media.room.connectionError")),
          );
          report(
            "connect",
            "failed",
            mediaErrorCode(error),
            performance.now() - connectStartedAt.current,
          );
        }}
        onMediaDeviceFailure={() => {
          setRoomError(t("media.room.deviceError"));
          report("media", "failed", "media_device_failure");
        }}
        room={room}
        serverUrl={roomState.credential.server_url}
        token={roomState.credential.access_token}
        video={
          roomState.credential.can_publish && roomState.choices.videoEnabled
            ? { deviceId: roomState.choices.videoDeviceId || undefined }
            : false
        }
      >
        <RoomLifecycleObserver
          classId={classId}
          csrfToken={roomState.csrfToken}
          attemptID={roomState.credential.attempt_id}
          onStatusChange={setStatus}
        />
        <LayoutContextProvider>
          <ConferenceStage
            canPublish={roomState.credential.can_publish}
            onDeviceError={(error) => {
              setRoomError(
                joinErrorMessage(error, t("media.room.deviceError")),
              );
              report("media", "failed", mediaErrorCode(error));
            }}
            roomName={roomState.credential.room_name}
            status={status}
          />
        </LayoutContextProvider>
        <RoomAudioRenderer />
      </LiveKitRoom>

      {roomError && status !== "disconnected" && (
        <section className="media-room-alert" role="alert">
          <span>{roomError}</span>
          <button onClick={() => setRoomError(null)} type="button">
            {t("media.room.dismiss")}
          </button>
        </section>
      )}

      {status === "disconnected" && (
        <section className="media-room-disconnected" role="alert">
          <div>
            <p>{t("media.room.disconnectedKicker")}</p>
            <h1>{t("media.room.disconnectedTitle")}</h1>
            <span>{t("media.room.disconnectedDescription")}</span>
            <div>
              <Link to={`/app/classrooms/${classId}/prejoin`}>
                {t("media.room.rejoin")}
              </Link>
              <Link to={`/app/classrooms/${classId}`}>
                {t("media.room.backToClass")}
              </Link>
            </div>
          </div>
        </section>
      )}
    </main>
  );
}

function ConferenceStage({
  canPublish,
  onDeviceError,
  roomName,
  status,
}: {
  canPublish: boolean;
  onDeviceError: (error: Error) => void;
  roomName: string;
  status: RoomStatus;
}) {
  const { t } = useI18n();
  const participants = useParticipants();
  const tracks = useTracks(
    [
      { source: Track.Source.Camera, withPlaceholder: true },
      { source: Track.Source.ScreenShare, withPlaceholder: false },
    ],
    { onlySubscribed: false },
  );

  return (
    <div className="media-conference">
      <header className="media-conference__header">
        <div>
          <span className={`media-connection media-connection--${status}`}>
            {connectionLabel(status, t)}
          </span>
          <strong>{t("media.room.title")}</strong>
          <small>{roomName}</small>
        </div>
        <span className="media-participant-count">
          {t("media.room.participantCount", { count: participants.length })}
        </span>
      </header>

      <section
        aria-label={t("media.room.participantGrid")}
        className="media-conference__stage"
      >
        <GridLayout tracks={tracks}>
          <ParticipantTile />
        </GridLayout>
      </section>

      <footer className="media-conference__controls">
        {!canPublish && (
          <span className="media-listen-only">
            {t("media.room.listenOnly")}
          </span>
        )}
        <ControlBar
          controls={{
            camera: canPublish,
            microphone: canPublish,
            screenShare: canPublish,
            chat: false,
            leave: true,
            settings: canPublish,
          }}
          onDeviceError={({ error }) => onDeviceError(error)}
          saveUserChoices={false}
          variation="minimal"
        />
      </footer>
    </div>
  );
}

function RoomLifecycleObserver({
  attemptID,
  classId,
  csrfToken,
  onStatusChange,
}: {
  attemptID: string;
  classId: string;
  csrfToken: string;
  onStatusChange: (status: RoomStatus) => void;
}) {
  const room = useRoomContext();
  const reconnectStartedAt = useRef(0);

  useEffect(() => {
    const report = (
      stage: "reconnecting" | "reconnected",
      outcome: "started" | "succeeded",
      durationMS = 0,
    ) => {
      void recordMediaEvent(classId, csrfToken, {
        attempt_id: attemptID,
        stage,
        outcome,
        error_code: "",
        duration_ms: Math.max(0, Math.round(durationMS)),
      }).catch(() => undefined);
    };
    const reconnecting = () => {
      reconnectStartedAt.current = performance.now();
      onStatusChange("reconnecting");
      report("reconnecting", "started");
    };
    const reconnected = () => {
      onStatusChange("connected");
      report(
        "reconnected",
        "succeeded",
        performance.now() - reconnectStartedAt.current,
      );
    };

    room.on(RoomEvent.Reconnecting, reconnecting);
    room.on(RoomEvent.Reconnected, reconnected);
    return () => {
      room.off(RoomEvent.Reconnecting, reconnecting);
      room.off(RoomEvent.Reconnected, reconnected);
    };
  }, [attemptID, classId, csrfToken, onStatusChange, room]);

  return null;
}

function DeviceReadinessPanel() {
  const { t } = useI18n();
  const [speakerStatus, setSpeakerStatus] = useState<
    "idle" | "playing" | "failed"
  >("idle");

  const testSpeaker = async () => {
    setSpeakerStatus("playing");
    try {
      const context = new AudioContext();
      const oscillator = context.createOscillator();
      const gain = context.createGain();
      gain.gain.value = 0.06;
      oscillator.frequency.value = 660;
      oscillator.connect(gain);
      gain.connect(context.destination);
      oscillator.start();
      oscillator.stop(context.currentTime + 0.35);
      await new Promise<void>((resolve) => {
        oscillator.addEventListener("ended", () => resolve(), { once: true });
      });
      await context.close();
      setSpeakerStatus("idle");
    } catch {
      setSpeakerStatus("failed");
    }
  };

  return (
    <aside className="media-device-checklist">
      <div>
        <p>{t("media.prejoin.checkTitle")}</p>
        <h2>{t("media.prejoin.checkHeading")}</h2>
      </div>
      <ul>
        <li>{t("media.prejoin.checkCamera")}</li>
        <li>{t("media.prejoin.checkMicrophone")}</li>
        <li>{t("media.prejoin.checkNetwork")}</li>
      </ul>
      <button
        disabled={speakerStatus === "playing"}
        onClick={() => void testSpeaker()}
        type="button"
      >
        {speakerStatus === "playing"
          ? t("media.prejoin.speakerPlaying")
          : t("media.prejoin.speakerTest")}
      </button>
      {speakerStatus === "failed" && (
        <small role="alert">{t("media.prejoin.speakerError")}</small>
      )}
    </aside>
  );
}

function ListenOnlyPreJoin({
  displayName,
  isJoining,
  onJoin,
}: {
  displayName: string;
  isJoining: boolean;
  onJoin: (choices: LocalUserChoices) => void;
}) {
  const { t } = useI18n();
  return (
    <section className="media-listen-prejoin">
      <span aria-hidden="true">LIVE</span>
      <h2>{t("media.prejoin.listenOnlyTitle")}</h2>
      <p>{t("media.prejoin.listenOnlyDescription")}</p>
      <button
        disabled={isJoining}
        onClick={() =>
          onJoin({
            username: displayName,
            audioEnabled: false,
            videoEnabled: false,
            audioDeviceId: "",
            videoDeviceId: "",
          })
        }
        type="button"
      >
        {isJoining ? t("media.prejoin.joining") : t("media.prejoin.join")}
      </button>
    </section>
  );
}

function PreJoinFailure({ message }: { message: string }) {
  const { t } = useI18n();
  return (
    <section className="media-prejoin-failure" role="alert">
      <strong>{t("media.prejoin.unavailableTitle")}</strong>
      <p>{message}</p>
      <Link to="/app/classrooms">{t("classroom.backToList")}</Link>
    </section>
  );
}

function RoomRecoveryState({
  classId,
  message,
}: {
  classId: string | undefined;
  message: string;
}) {
  const { t } = useI18n();
  const target = classId
    ? `/app/classrooms/${classId}/prejoin`
    : "/app/classrooms";
  return (
    <main className="media-room-recovery">
      <section>
        <p>{t("media.room.recoveryKicker")}</p>
        <h1>{t("media.room.recoveryTitle")}</h1>
        <span>{message}</span>
        <Link to={target}>{t("media.room.returnToPrejoin")}</Link>
      </section>
    </main>
  );
}

function tokenIsExpired(state: MediaRoomNavigationState) {
  const expiresAt = Date.parse(state.credential.expires_at);
  return !Number.isFinite(expiresAt) || expiresAt <= Date.now() + 5_000;
}

function joinErrorMessage(error: unknown, fallback: string) {
  if (error instanceof APIRequestError) {
    return error.problem?.detail ?? fallback;
  }
  if (error instanceof DOMException && error.name === "NotAllowedError") {
    return "Camera or microphone permission was denied by the browser.";
  }
  return fallback;
}

function connectionLabel(
  status: RoomStatus,
  t: ReturnType<typeof useI18n>["t"],
) {
  switch (status) {
    case "connected":
      return t("media.room.connected");
    case "reconnecting":
      return t("media.room.reconnecting");
    case "disconnected":
      return t("media.room.disconnected");
    case "failed":
      return t("media.room.failed");
    default:
      return t("media.room.connecting");
  }
}
