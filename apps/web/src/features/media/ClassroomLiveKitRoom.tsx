import "@livekit/components-styles";

import {
  RoomContext,
  RoomAudioRenderer,
  StartAudio,
  setLogLevel as setComponentsLogLevel,
} from "@livekit/components-react";
import {
  DisconnectReason,
  LogLevel,
  Room,
  RoomEvent,
  getLogger,
} from "livekit-client";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useI18n } from "../../app/i18n";
import type {
  MediaInstanceCredentialProjection,
  MediaJoinChoices,
} from "../../app/mediaPrejoin";
import {
  ClassroomMediaShell,
  type ClassroomConnectionStatus,
  type ClassroomSignalControls,
} from "./ClassroomMediaShell";
import type { ClassroomModerationControlsModel } from "./ClassroomModerationControls";

const manualSubscriptionConnectOptions = { autoSubscribe: false } as const;
const canonicalRoomLoggerName = "tutorhub-canonical-media-room";

export interface ClassroomLiveKitRoomProps {
  choices: MediaJoinChoices;
  connectionStatus: ClassroomConnectionStatus;
  chat?: ReactNode;
  credential: MediaInstanceCredentialProjection;
  lobby?: ReactNode;
  moderation?: ClassroomModerationControlsModel;
  signals?: ClassroomSignalControls;
  onConnected: () => void;
  onReconnecting: () => void;
  onDisconnected: (reason?: DisconnectReason) => void;
  onLeave: () => void;
  onProviderError: () => void;
}

interface CanonicalRoomLifecycle {
  disconnect: () => Promise<void>;
  markConnected: () => void;
  markDisconnected: () => void;
  room: Room;
}

export function ClassroomLiveKitRoom(props: ClassroomLiveKitRoomProps) {
  const { t } = useI18n();
  const creationGeneration = useRef(0);
  const lifecycleRef = useRef<CanonicalRoomLifecycle | null>(null);
  const [lifecycle, setLifecycle] = useState<CanonicalRoomLifecycle | null>(
    null,
  );
  const isCurrentCreationGeneration = useCallback(
    (generation: number) => creationGeneration.current === generation,
    [],
  );

  useEffect(() => {
    const generation = ++creationGeneration.current;
    let active = true;
    // Construct only after StrictMode's synthetic effect replay has settled.
    globalThis.queueMicrotask(() => {
      if (!active || !isCurrentCreationGeneration(generation)) return;
      const current = lifecycleRef.current ?? createCanonicalRoomLifecycle();
      lifecycleRef.current = current;
      setLifecycle(current);
    });
    return () => {
      active = false;
      globalThis.queueMicrotask(() => {
        if (!isCurrentCreationGeneration(generation)) return;
        const current = lifecycleRef.current;
        lifecycleRef.current = null;
        if (current) void current.disconnect();
      });
    };
  }, [isCurrentCreationGeneration]);

  if (!lifecycle) {
    return (
      <p aria-live="polite" className="media-p405-notice" role="status">
        {t("media.room.connecting")}
      </p>
    );
  }

  return <ConnectedClassroomLiveKitRoom {...props} lifecycle={lifecycle} />;
}

function ConnectedClassroomLiveKitRoom({
  choices,
  connectionStatus,
  chat,
  credential,
  lifecycle,
  lobby,
  moderation,
  signals,
  onConnected,
  onReconnecting,
  onDisconnected,
  onLeave,
  onProviderError,
}: ClassroomLiveKitRoomProps & { lifecycle: CanonicalRoomLifecycle }) {
  const { t } = useI18n();
  const { room } = lifecycle;
  const [controlAbortController] = useState(() => new AbortController());
  const intentionalLeave = useRef(false);
  const lifecycleActive = useRef(false);
  const connectPromise = useRef<Promise<void> | null>(null);
  const initialPublishPromise = useRef<Promise<void> | null>(null);
  const terminalAction = useRef<"disconnect" | "error" | "leave" | null>(null);
  const terminalCallbackFired = useRef(false);
  const lifecycleGeneration = useRef(0);
  const canPublishCameraMicrophone = credential.can_publish_camera_microphone;
  const audioCaptureOptions = useMemo(
    () =>
      canPublishCameraMicrophone ? roomAudioCaptureOptions(choices) : false,
    [canPublishCameraMicrophone, choices],
  );
  const videoCaptureOptions = useMemo<false | { deviceId?: string }>(
    () =>
      canPublishCameraMicrophone && choices.videoEnabled
        ? { deviceId: choices.videoDeviceId || undefined }
        : false,
    [canPublishCameraMicrophone, choices.videoDeviceId, choices.videoEnabled],
  );

  const disconnectRoom = useCallback(() => lifecycle.disconnect(), [lifecycle]);
  const stopOwnedLocalTracks = useCallback(async () => {
    const publications = [...room.localParticipant.trackPublications.values()];
    await Promise.all(
      publications.map(async ({ track }) => {
        if (!track) return;
        try {
          await room.localParticipant.unpublishTrack(track, true);
        } catch {
          // Terminal cleanup intentionally hides provider details.
        } finally {
          try {
            track.detach();
          } catch {
            // A provider may already have detached the track.
          }
          try {
            track.stop();
          } catch {
            // A provider may already have stopped the track.
          }
        }
      }),
    );
  }, [room]);
  const isCurrentLifecycleGeneration = useCallback(
    (generation: number) => lifecycleGeneration.current === generation,
    [],
  );

  const handleConnected = useCallback(() => {
    if (terminalAction.current) {
      void disconnectRoom();
      return;
    }
    lifecycle.markConnected();
    onConnected();
    if (!terminalAction.current && choices.speakerDeviceId) {
      void room
        .switchActiveDevice("audiooutput", choices.speakerDeviceId)
        .catch(() => undefined);
    }
  }, [choices.speakerDeviceId, disconnectRoom, lifecycle, onConnected, room]);

  const handleDisconnected = useCallback(
    (reason?: DisconnectReason) => {
      if (intentionalLeave.current || terminalAction.current) return;
      terminalAction.current = "disconnect";
      lifecycle.markDisconnected();
      controlAbortController.abort();
      terminalCallbackFired.current = true;
      onDisconnected(reason);
    },
    [controlAbortController, lifecycle, onDisconnected],
  );

  const finishTerminalAction = useCallback(
    (action: "error" | "leave", callback: () => void) => {
      if (terminalAction.current) return;
      const generation = lifecycleGeneration.current;
      terminalAction.current = action;
      intentionalLeave.current = true;
      controlAbortController.abort();
      void disconnectRoom().finally(() => {
        if (
          lifecycleActive.current &&
          isCurrentLifecycleGeneration(generation) &&
          terminalAction.current === action &&
          !terminalCallbackFired.current
        ) {
          terminalCallbackFired.current = true;
          callback();
        }
      });
    },
    [controlAbortController, disconnectRoom, isCurrentLifecycleGeneration],
  );

  const handleProviderError = useCallback(() => {
    finishTerminalAction("error", onProviderError);
  }, [finishTerminalAction, onProviderError]);

  const handleLeave = useCallback(() => {
    finishTerminalAction("leave", onLeave);
  }, [finishTerminalAction, onLeave]);

  const publishInitialTracks = useEffectEvent(() => {
    if (
      !lifecycleActive.current ||
      terminalAction.current ||
      initialPublishPromise.current
    ) {
      return;
    }
    const microphonePublish = room.localParticipant
      .setMicrophoneEnabled(
        audioCaptureOptions !== false,
        audioCaptureOptions === false ? undefined : audioCaptureOptions,
      )
      .finally(() => {
        if (
          audioCaptureOptions !== false &&
          (!lifecycleActive.current || terminalAction.current)
        ) {
          return stopOwnedLocalTracks();
        }
      });
    const cameraPublish = room.localParticipant
      .setCameraEnabled(
        videoCaptureOptions !== false,
        videoCaptureOptions === false ? undefined : videoCaptureOptions,
      )
      .finally(() => {
        if (
          videoCaptureOptions !== false &&
          (!lifecycleActive.current || terminalAction.current)
        ) {
          return stopOwnedLocalTracks();
        }
      });
    initialPublishPromise.current = Promise.all([
      microphonePublish,
      cameraPublish,
    ])
      .then(() => undefined)
      .catch(() => {
        if (lifecycleActive.current && !terminalAction.current) {
          handleProviderError();
        }
      });
  });
  const notifyConnected = useEffectEvent(handleConnected);
  const notifyReconnecting = useEffectEvent(onReconnecting);
  const notifyDisconnected = useEffectEvent(handleDisconnected);
  const notifyProviderError = useEffectEvent(handleProviderError);

  useEffect(() => {
    const handleSignalConnected = () => publishInitialTracks();
    const handleRoomConnected = () => notifyConnected();
    const handleRoomReconnected = () => notifyConnected();
    const handleRoomReconnecting = () => notifyReconnecting();
    const handleRoomDisconnected = (reason?: DisconnectReason) =>
      notifyDisconnected(reason);

    room
      .on(RoomEvent.SignalConnected, handleSignalConnected)
      .on(RoomEvent.Connected, handleRoomConnected)
      .on(RoomEvent.Reconnected, handleRoomReconnected)
      .on(RoomEvent.Reconnecting, handleRoomReconnecting)
      .on(RoomEvent.Disconnected, handleRoomDisconnected);

    return () => {
      room
        .off(RoomEvent.SignalConnected, handleSignalConnected)
        .off(RoomEvent.Connected, handleRoomConnected)
        .off(RoomEvent.Reconnected, handleRoomReconnected)
        .off(RoomEvent.Reconnecting, handleRoomReconnecting)
        .off(RoomEvent.Disconnected, handleRoomDisconnected);
    };
  }, [room]);

  useEffect(() => {
    const generation = ++lifecycleGeneration.current;
    lifecycleActive.current = true;
    intentionalLeave.current = false;
    terminalAction.current = null;
    terminalCallbackFired.current = false;
    if (!connectPromise.current) {
      connectPromise.current = room
        .connect(
          credential.server_url,
          credential.access_token,
          manualSubscriptionConnectOptions,
        )
        .catch(() => {
          if (lifecycleActive.current && !terminalAction.current) {
            notifyProviderError();
          }
        });
    }
    return () => {
      lifecycleActive.current = false;
      globalThis.queueMicrotask(() => {
        if (!isCurrentLifecycleGeneration(generation)) return;
        intentionalLeave.current = true;
        void disconnectRoom();
      });
    };
  }, [
    credential.access_token,
    credential.server_url,
    disconnectRoom,
    isCurrentLifecycleGeneration,
    room,
  ]);

  return (
    <RoomContext.Provider value={room}>
      <div className="lk-room-container">
        <ClassroomMediaShell
          canPublishCameraMicrophone={canPublishCameraMicrophone}
          canShareScreen={credential.can_share_screen}
          canSubscribe={credential.can_subscribe}
          chat={chat}
          connectionStatus={connectionStatus}
          controlAbortSignal={controlAbortController.signal}
          lobby={lobby}
          moderation={moderation}
          signals={signals}
          onLeave={handleLeave}
          onTerminalMediaCleanup={stopOwnedLocalTracks}
        />
        <RoomAudioRenderer />
        <StartAudio
          className="media-p405-start-audio"
          label={t("media.room.enableAudio")}
          room={room}
        />
      </div>
    </RoomContext.Provider>
  );
}

function createCanonicalRoom(): Room {
  setComponentsLogLevel(LogLevel.silent, {
    liveKitClientLogLevel: LogLevel.silent,
  });
  getLogger(canonicalRoomLoggerName).setLevel(LogLevel.silent);
  return new Room({
    adaptiveStream: true,
    dynacast: true,
    loggerName: canonicalRoomLoggerName,
  });
}

function createCanonicalRoomLifecycle(): CanonicalRoomLifecycle {
  const room = createCanonicalRoom();
  let disconnectPromise: Promise<void> | null = null;
  return {
    room,
    disconnect() {
      if (!disconnectPromise) {
        disconnectPromise = room.disconnect(true).catch(() => undefined);
      }
      return disconnectPromise;
    },
    markConnected() {
      disconnectPromise = null;
    },
    markDisconnected() {
      disconnectPromise ??= Promise.resolve();
    },
  };
}

function roomAudioCaptureOptions(choices: MediaJoinChoices):
  | false
  | {
      deviceId?: string;
      echoCancellation: { ideal: boolean };
      noiseSuppression: { ideal: boolean };
      autoGainControl: { ideal: boolean };
    } {
  if (!choices.audioEnabled) {
    return false;
  }
  const speech = choices.audioMode === "speech";
  return {
    deviceId: choices.audioDeviceId || undefined,
    echoCancellation: { ideal: speech },
    noiseSuppression: { ideal: speech },
    autoGainControl: { ideal: speech },
  };
}
