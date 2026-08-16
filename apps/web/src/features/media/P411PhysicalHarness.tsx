import { useEffect, useMemo, useRef, useState } from "react";
import {
  MediaPrejoinController,
  type MediaInstanceCredentialProjection,
  type MediaJoinChoices,
  type MediaPrejoinSnapshot,
} from "../../app/mediaPrejoin";
import {
  projectClassroomSignalSnapshot,
  type ClassroomReactionType,
  type ClassroomSignalSnapshot,
} from "./classroomSignals";
import type { ClassroomSignalControls } from "./ClassroomMediaShell";
import {
  type ClassroomModerationAction,
  type ClassroomModerationControlsModel,
} from "./ClassroomModerationControls";
import { ClassroomLiveKitRoom } from "./ClassroomLiveKitRoom";

const boundaryURL = "http://127.0.0.1:4179";
const teacherKey = "e4f2f4a2-1e4b-4d5d-a920-0e32256998ec";
const studentKey = "14ad5f16-7c78-46d3-bc10-1563afcb51c7";

type HarnessRole = "teacher" | "student";
type VideoProfile = "720p" | "540p" | "360p";
type HarnessConnectionStatus =
  | "idle"
  | "requesting_credential"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "disconnected"
  | "failed";

interface HarnessMetadata {
  role: HarnessRole;
  display_name: string;
  self_participant_key: string;
  peer_participant_key: string;
  room_instance_id: string;
}

interface CredentialResponse {
  credential: MediaInstanceCredentialProjection;
  harness: HarnessMetadata;
}

interface ProviderStatus {
  room_exists: boolean;
  participant_count: number;
  cleanup_zero: boolean;
}

interface ProblemResponse {
  code?: string;
}

interface P411PhysicalHarnessProps {
  readonly localHostname?: string;
}

export function P411PhysicalHarness({
  localHostname,
}: P411PhysicalHarnessProps = {}) {
  const role = harnessRole(globalThis.location?.search ?? "");
  const allowedHost =
    (localHostname ?? globalThis.location?.hostname) === "127.0.0.1";
  const [controller] = useState<MediaPrejoinController | null>(() =>
    allowedHost && navigator.mediaDevices ? new MediaPrejoinController() : null,
  );
  const [prejoin, setPrejoin] = useState<MediaPrejoinSnapshot | null>(
    () => controller?.getSnapshot() ?? null,
  );
  const [credential, setCredential] =
    useState<MediaInstanceCredentialProjection | null>(null);
  const [metadata, setMetadata] = useState<HarnessMetadata | null>(null);
  const [choices, setChoices] = useState<MediaJoinChoices | null>(null);
  const [connectionStatus, setConnectionStatus] =
    useState<HarnessConnectionStatus>("idle");
  const [providerStatus, setProviderStatus] = useState<ProviderStatus | null>(
    null,
  );
  const [statusMessage, setStatusMessage] = useState(
    "Chưa yêu cầu quyền thiết bị.",
  );
  const [signalSnapshot, setSignalSnapshot] =
    useState<ClassroomSignalSnapshot | null>(null);
  const [moderationMessage, setModerationMessage] = useState("");
  const [videoProfile, setVideoProfile] = useState<VideoProfile>("720p");
  const previewRef = useRef<HTMLVideoElement>(null);
  const controllerGeneration = useRef(0);

  useEffect(() => {
    if (!controller) return undefined;
    const generation = ++controllerGeneration.current;
    controller.connect();
    const unsubscribe = controller.subscribe(() =>
      setPrejoin(controller.getSnapshot()),
    );
    return () => {
      unsubscribe();
      globalThis.queueMicrotask(() => {
        // StrictMode replays setup immediately; dispose only after a real unmount.
        // eslint-disable-next-line react-hooks/exhaustive-deps
        if (controllerGeneration.current === generation) {
          void controller.dispose();
        }
      });
    };
  }, [controller]);

  useEffect(() => {
    controller?.attachPreview(previewRef.current);
    return () => controller?.attachPreview(null);
  }, [controller]);

  const signalProjection = useMemo(
    () =>
      signalSnapshot
        ? projectClassroomSignalSnapshot(
            signalSnapshot,
            Date.parse(signalSnapshot.server_time),
          )
        : null,
    [signalSnapshot],
  );

  const signals = useMemo<ClassroomSignalControls | undefined>(() => {
    if (!signalProjection || !metadata) return undefined;
    return {
      error: false,
      loading: false,
      mutating: false,
      projection: signalProjection,
      refreshing: false,
      onLowerAllHands: async () => {
        updateSignals(setSignalSnapshot, (snapshot) => ({
          ...snapshot,
          raised_hands: [],
        }));
      },
      onLowerHand: async (participantKey) => {
        updateSignals(setSignalSnapshot, (snapshot) => ({
          ...snapshot,
          raised_hands: snapshot.raised_hands.filter(
            (hand) => hand.participant_key !== participantKey,
          ),
        }));
      },
      onResync: async () => {
        setModerationMessage(
          "Đã làm mới projection mô phỏng cho bài kiểm tra NVDA.",
        );
      },
      onSendReaction: async (reaction) => {
        addReaction(setSignalSnapshot, reaction);
      },
      onToggleHand: async () => {
        toggleHand(setSignalSnapshot, metadata.self_participant_key);
      },
    };
  }, [metadata, signalProjection]);

  const moderation = useMemo<
    ClassroomModerationControlsModel | undefined
  >(() => {
    if (role !== "teacher" || !signalSnapshot) return undefined;
    const announce = async (
      action: ClassroomModerationAction,
      target?: string,
    ) => {
      setModerationMessage(
        `Mô phỏng accessibility: ${action}${target ? " cho học viên" : ""}. Không gửi lệnh provider.`,
      );
    };
    return {
      roomLocked: Boolean(signalSnapshot.room_locked),
      canLockRoom: true,
      canEndRoom: true,
      participantOperations: [
        {
          participantKey: studentKey,
          canPromoteCoHost: true,
          canDemoteCoHost: false,
          canRemoteMute: true,
          canRemove: true,
        },
      ],
      mutationState: { status: "idle" },
      providerEffect: { status: "idle" },
      onSetRoomLocked: async (locked) => {
        updateSignals(setSignalSnapshot, (snapshot) => ({
          ...snapshot,
          room_locked: locked,
        }));
        await announce(locked ? "lock_room" : "unlock_room");
      },
      onEndRoom: () => announce("end_room"),
      onPromoteCoHost: (target) => announce("promote_co_host", target),
      onDemoteCoHost: (target) => announce("demote_co_host", target),
      onRemoteMute: (target) => announce("remote_mute", target),
      onRemoveParticipant: (target) => announce("remove_participant", target),
    };
  }, [role, signalSnapshot]);

  if (!allowedHost || !import.meta.env.DEV) {
    return (
      <main className="p411-physical">
        <h1>P4-11 physical harness bị khóa</h1>
        <p role="alert">
          Harness chỉ chạy trong Vite development tại 127.0.0.1; không có đường
          dẫn production.
        </p>
      </main>
    );
  }

  const startPreview = async () => {
    if (!controller) return;
    setStatusMessage("Đang yêu cầu quyền camera và microphone...");
    await controller.startPreview();
    const snapshot = controller.getSnapshot();
    setStatusMessage(
      snapshot.status === "preview_ready"
        ? "Preview đã sẵn sàng. Kiểm tra hình, mic meter và loa trước khi join."
        : `Preview chưa sẵn sàng: ${snapshot.errorCode ?? "unknown"}.`,
    );
  };

  const join = async () => {
    if (!controller || prejoin?.status !== "preview_ready") return;
    setConnectionStatus("requesting_credential");
    setStatusMessage("Đang lấy credential ngắn hạn từ boundary local...");
    const selectedChoices = controller.choices(false);
    try {
      const response = await fetch(`${boundaryURL}/v1/credential`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        cache: "no-store",
        referrerPolicy: "no-referrer",
        body: JSON.stringify({ role }),
      });
      if (!response.ok) throw new Error(await problemCode(response));
      const payload = (await response.json()) as CredentialResponse;
      await controller.stopPreview();
      setChoices(selectedChoices);
      setMetadata(payload.harness);
      setSignalSnapshot(initialSignalSnapshot(payload.harness));
      setCredential(payload.credential);
      setConnectionStatus("connecting");
      setStatusMessage("Đang kết nối LiveKit test...");
    } catch {
      setConnectionStatus("failed");
      setStatusMessage(
        "Không lấy được credential. Kiểm tra harness server rồi thử lại.",
      );
    }
  };

  const refreshStatus = async () => {
    try {
      const response = await fetch(`${boundaryURL}/v1/status`, {
        cache: "no-store",
        referrerPolicy: "no-referrer",
      });
      if (!response.ok) throw new Error(await problemCode(response));
      setProviderStatus((await response.json()) as ProviderStatus);
    } catch {
      setStatusMessage("Không đọc được trạng thái provider đã giới hạn.");
    }
  };

  const cleanup = async () => {
    try {
      const response = await fetch(`${boundaryURL}/v1/cleanup`, {
        method: "POST",
        cache: "no-store",
        referrerPolicy: "no-referrer",
      });
      if (!response.ok) throw new Error(await problemCode(response));
      const status = (await response.json()) as ProviderStatus;
      setProviderStatus(status);
      setStatusMessage(
        status.cleanup_zero
          ? "Cleanup PASS: phòng không còn tồn tại và participant_count = 0."
          : "Cleanup chưa về zero.",
      );
    } catch (error) {
      setStatusMessage(
        error instanceof Error && error.message === "participants_active"
          ? "Cleanup bị từ chối đúng thiết kế: vẫn còn participant đang kết nối."
          : "Cleanup thất bại; không hiển thị lỗi provider thô.",
      );
    }
  };

  const finishRoom = (message: string) => {
    setCredential(null);
    setChoices(null);
    setMetadata(null);
    setSignalSnapshot(null);
    setConnectionStatus("disconnected");
    setStatusMessage(message);
    void refreshStatus();
  };

  if (credential && choices) {
    return (
      <main>
        <p className="p411-physical-room-note">
          Harness local-only ·{" "}
          {role === "teacher" ? "Chrome / Teacher" : "Edge / Student"} ·
          hand/reaction/moderation chỉ là dữ liệu UI mô phỏng để đọc bằng NVDA.
        </p>
        <span aria-atomic="true" aria-live="polite" className="sr-only">
          {moderationMessage}
        </span>
        <ClassroomLiveKitRoom
          choices={choices}
          connectionStatus={roomConnectionStatus(connectionStatus)}
          credential={credential}
          moderation={moderation}
          signals={signals}
          videoCaptureOverride={{ resolution: videoResolution(videoProfile) }}
          onConnected={() => {
            setConnectionStatus("connected");
            setStatusMessage(
              "Đã kết nối. Thực hiện checklist A/V, layout và NVDA.",
            );
          }}
          onDisconnected={() =>
            finishRoom(
              "Đã mất kết nối; local credential đã được xóa khỏi React state.",
            )
          }
          onLeave={() =>
            finishRoom(
              "Đã rời phòng; local credential đã được xóa khỏi React state.",
            )
          }
          onProviderError={() =>
            finishRoom(
              "Provider error; local credential đã được xóa khỏi React state.",
            )
          }
          onReconnecting={() => {
            setConnectionStatus("reconnecting");
            setStatusMessage("Đang reconnect; kiểm tra thông báo bằng NVDA.");
          }}
        />
      </main>
    );
  }

  return (
    <main className="p411-physical">
      <header className="p411-physical-header">
        <h1>P4-11 Physical Chrome/Edge + NVDA Harness</h1>
        <p>
          Vai trò:{" "}
          <strong>
            {role === "teacher" ? "Teacher / Chrome" : "Student / Edge"}
          </strong>
        </p>
        <p className="p411-physical-notice">
          Trang local-only. Camera/microphone chỉ được yêu cầu sau khi bạn bấm
          “Bắt đầu preview”. Token không được lưu vào URL, DOM, localStorage hay
          sessionStorage.
        </p>
      </header>

      <section
        className="p411-physical-prejoin"
        aria-labelledby="prejoin-title"
      >
        <div>
          <h2 id="prejoin-title">Kiểm tra thiết bị thật</h2>
          <video
            aria-label="Camera preview"
            autoPlay
            className="p411-physical-preview"
            muted
            playsInline
            ref={previewRef}
          />
        </div>
        <div className="p411-physical-controls">
          <DeviceSelect
            disabled={!controller || prejoin?.status !== "preview_ready"}
            label="Microphone"
            options={prejoin?.microphones ?? []}
            value={prejoin?.selectedMicrophoneId ?? ""}
            onChange={(deviceId) =>
              void controller?.switchDevice("audioinput", deviceId)
            }
          />
          <DeviceSelect
            disabled={!controller || prejoin?.status !== "preview_ready"}
            label="Camera"
            options={prejoin?.cameras ?? []}
            value={prejoin?.selectedCameraId ?? ""}
            onChange={(deviceId) =>
              void controller?.switchDevice("videoinput", deviceId)
            }
          />
          <DeviceSelect
            disabled={!controller || prejoin?.status !== "preview_ready"}
            label="Loa"
            options={prejoin?.speakers ?? []}
            value={prejoin?.selectedSpeakerId ?? ""}
            onChange={(deviceId) => controller?.setSpeakerDevice(deviceId)}
          />
          <div className="p411-physical-meter">
            <label htmlFor="p411-mic-meter">Mức microphone</label>
            <progress
              id="p411-mic-meter"
              aria-label="Mức microphone"
              max={1}
              value={prejoin?.micLevel ?? 0}
            />
          </div>
          <label>
            Profile publish vật lý
            <select
              onChange={(event) =>
                setVideoProfile(event.target.value as VideoProfile)
              }
              value={videoProfile}
            >
              <option value="720p">720p · 1280×720</option>
              <option value="540p">540p · 960×540</option>
              <option value="360p">360p · 640×360</option>
            </select>
          </label>
          <p aria-live="polite">
            Speaker test: {prejoin?.speakerTestStatus ?? "idle"}
          </p>
          <div className="p411-physical-actions">
            <button
              disabled={
                !controller || prejoin?.status === "requesting_permission"
              }
              onClick={() => void startPreview()}
              type="button"
            >
              Bắt đầu preview
            </button>
            <button
              disabled={!controller || prejoin?.status !== "preview_ready"}
              onClick={() => void controller?.testSpeaker()}
              type="button"
            >
              Test loa
            </button>
            <button
              disabled={
                !controller ||
                prejoin?.status !== "preview_ready" ||
                connectionStatus === "requesting_credential"
              }
              onClick={() => void join()}
              type="button"
            >
              Join LiveKit test
            </button>
          </div>
        </div>
      </section>

      <section className="p411-physical-status" aria-labelledby="status-title">
        <h2 id="status-title">Trạng thái giới hạn</h2>
        <p aria-atomic="true" aria-live="polite" role="status">
          {statusMessage}
        </p>
        {providerStatus && (
          <p>
            room_exists={String(providerStatus.room_exists)} ·
            participant_count=
            {providerStatus.participant_count} · cleanup_zero=
            {String(providerStatus.cleanup_zero)}
          </p>
        )}
        <div className="p411-physical-actions">
          <button onClick={() => void refreshStatus()} type="button">
            Làm mới trạng thái
          </button>
          <button onClick={() => void cleanup()} type="button">
            Xác minh cleanup zero
          </button>
        </div>
      </section>
    </main>
  );
}

function DeviceSelect({
  disabled,
  label,
  onChange,
  options,
  value,
}: {
  disabled: boolean;
  label: string;
  onChange: (deviceId: string) => void;
  options: readonly { deviceId: string; label: string }[];
  value: string;
}) {
  return (
    <label>
      {label}
      <select
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        value={value}
      >
        {options.length === 0 && <option value="">Chưa có thiết bị</option>}
        {options.map((option) => (
          <option key={option.deviceId} value={option.deviceId}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function harnessRole(search: string): HarnessRole {
  return new URLSearchParams(search).get("role") === "student"
    ? "student"
    : "teacher";
}

function initialSignalSnapshot(
  metadata: HarnessMetadata,
): ClassroomSignalSnapshot {
  const now = new Date().toISOString();
  return {
    room_instance_id: metadata.room_instance_id,
    room_locked: false,
    projection_version: 1,
    last_signal_sequence: 0,
    self_participant_key: metadata.self_participant_key,
    viewer_operations: {
      can_raise_hand: true,
      can_send_reaction: true,
      can_moderate_hands: metadata.role === "teacher",
      can_lock_room: metadata.role === "teacher",
      can_end_room: metadata.role === "teacher",
    },
    participants: [
      {
        participant_key: teacherKey,
        roster_sequence: 1,
        display_name: "Giáo viên thử nghiệm",
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
        participant_key: studentKey,
        roster_sequence: 2,
        display_name: "Học viên thử nghiệm",
        instance_role: "attendee",
        connection_state: "connected",
        moderation_operations: {
          can_promote_co_host: metadata.role === "teacher",
          can_demote_co_host: false,
          can_remote_mute: metadata.role === "teacher",
          can_remove: metadata.role === "teacher",
        },
      },
    ],
    raised_hands: [],
    reaction_clusters: [],
    server_time: now,
  };
}

function updateSignals(
  setter: React.Dispatch<React.SetStateAction<ClassroomSignalSnapshot | null>>,
  update: (snapshot: ClassroomSignalSnapshot) => ClassroomSignalSnapshot,
) {
  setter((current) => {
    if (!current) return current;
    const next = update(current);
    return {
      ...next,
      projection_version: current.projection_version + 1,
      last_signal_sequence: current.last_signal_sequence + 1,
      server_time: new Date().toISOString(),
    };
  });
}

function toggleHand(
  setter: React.Dispatch<React.SetStateAction<ClassroomSignalSnapshot | null>>,
  participantKey: string,
) {
  updateSignals(setter, (snapshot) => {
    const exists = snapshot.raised_hands.some(
      (hand) => hand.participant_key === participantKey,
    );
    return {
      ...snapshot,
      raised_hands: exists
        ? snapshot.raised_hands.filter(
            (hand) => hand.participant_key !== participantKey,
          )
        : [
            ...snapshot.raised_hands,
            {
              participant_key: participantKey,
              signal_sequence: snapshot.last_signal_sequence + 1,
              raised_at: new Date().toISOString(),
            },
          ],
    };
  });
}

function addReaction(
  setter: React.Dispatch<React.SetStateAction<ClassroomSignalSnapshot | null>>,
  reaction: ClassroomReactionType,
) {
  updateSignals(setter, (snapshot) => {
    const acceptedAt = new Date();
    const sequence = snapshot.last_signal_sequence + 1;
    return {
      ...snapshot,
      reaction_clusters: [
        ...snapshot.reaction_clusters,
        {
          reaction,
          count: 1,
          first_signal_sequence: sequence,
          last_signal_sequence: sequence,
          accepted_at: acceptedAt.toISOString(),
          expires_at: new Date(acceptedAt.getTime() + 10_000).toISOString(),
        },
      ],
    };
  });
}

function roomConnectionStatus(
  status: HarnessConnectionStatus,
): "connecting" | "connected" | "reconnecting" | "disconnected" | "failed" {
  if (
    status === "connected" ||
    status === "reconnecting" ||
    status === "disconnected" ||
    status === "failed"
  ) {
    return status;
  }
  return "connecting";
}

async function problemCode(response: Response): Promise<string> {
  try {
    const problem = (await response.json()) as ProblemResponse;
    return problem.code ?? "request_failed";
  } catch {
    return "request_failed";
  }
}

function videoResolution(profile: VideoProfile) {
  switch (profile) {
    case "360p":
      return { width: 640, height: 360, frameRate: 30 };
    case "540p":
      return { width: 960, height: 540, frameRate: 30 };
    default:
      return { width: 1280, height: 720, frameRate: 30 };
  }
}
