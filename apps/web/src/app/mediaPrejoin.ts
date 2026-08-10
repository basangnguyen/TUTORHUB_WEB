export const MEDIA_PREJOIN_ERROR_CODES = [
  "media_permission_denied_or_blocked",
  "media_device_not_found",
  "media_device_busy_or_unreadable",
  "media_constraints_unavailable",
  "media_probe_aborted",
  "media_capture_unsupported_or_policy_blocked",
  "audio_output_selection_unsupported",
  "media_playback_blocked",
  "media_device_unknown",
] as const;

export type MediaPrejoinErrorCode = (typeof MEDIA_PREJOIN_ERROR_CODES)[number];

export type MediaPrejoinStatus =
  | "idle"
  | "requesting_permission"
  | "preview_ready"
  | "switching_device"
  | "degraded"
  | "cleaning_up";

export type MediaAudioMode = "speech" | "original_sound";

export type SpeakerTestStatus =
  "idle" | "playing" | "success" | "blocked" | "unsupported" | "failed";

export interface MediaDeviceChoice {
  deviceId: string;
  label: string;
}

export interface MediaPrejoinSnapshot {
  status: MediaPrejoinStatus;
  permissionGranted: boolean;
  microphones: readonly MediaDeviceChoice[];
  cameras: readonly MediaDeviceChoice[];
  speakers: readonly MediaDeviceChoice[];
  selectedMicrophoneId: string;
  selectedCameraId: string;
  selectedSpeakerId: string;
  audioMode: MediaAudioMode;
  micLevel: number;
  actualAudioProcessing: {
    echoCancellation: boolean | null;
    noiseSuppression: boolean | null;
    autoGainControl: boolean | null;
  };
  errorCode: MediaPrejoinErrorCode | null;
  announcement: string;
  speakerTestStatus: SpeakerTestStatus;
}

export interface MediaJoinChoices {
  audioEnabled: boolean;
  videoEnabled: boolean;
  audioDeviceId: string;
  videoDeviceId: string;
  speakerDeviceId: string;
  audioMode: MediaAudioMode;
}

export interface MediaJoinAttemptProjection {
  join_attempt_id: string;
  participant_session_id: string;
  room_instance_id: string;
  status:
    | "waiting"
    | "admitted"
    | "joining"
    | "denied"
    | "cancelled"
    | "timeout"
    | "meeting_ended"
    | "provider_unavailable";
  version: number;
  admission_request_id?: string | null;
  admission_version?: number;
  expires_at?: string | null;
}

export interface MediaInstanceCredentialProjection {
  access_token: string;
  server_url: string;
  participant_session_id: string;
  room_instance_id: string;
  join_attempt_id: string;
  instance_role: "host" | "co_host" | "teaching_assistant" | "attendee";
  can_publish_camera_microphone: boolean;
  can_share_screen: boolean;
  can_subscribe: boolean;
  expires_at: string;
}

export interface MediaRoomEscrowScope {
  tenantId: string;
  userId: string;
  spaceId: string;
  roomInstanceId: string;
}

export interface MediaRoomEscrowValue {
  scope: MediaRoomEscrowScope;
  credential: MediaInstanceCredentialProjection;
  choices: MediaJoinChoices;
}

interface MediaRoomEscrowEntry {
  value: MediaRoomEscrowValue;
  claimed: boolean;
}

let mediaRoomEscrow: MediaRoomEscrowEntry | null = null;

export function putMediaRoomEscrow(value: MediaRoomEscrowValue): void {
  mediaRoomEscrow = { value, claimed: false };
}

export function takeMediaRoomEscrow(
  expected: MediaRoomEscrowScope,
): MediaRoomEscrowValue | null {
  const entry = mediaRoomEscrow;
  if (!entry) {
    return null;
  }
  if (!sameEscrowScope(entry.value.scope, expected)) {
    mediaRoomEscrow = null;
    return null;
  }
  // React StrictMode may evaluate a state initializer twice before the first
  // commit. Keep an exact-scope claim readable until that commit, then purge it
  // with finalizeMediaRoomEscrowClaim. The token never leaves process memory.
  entry.claimed = true;
  return entry.value;
}

export function finalizeMediaRoomEscrowClaim(
  expected: MediaRoomEscrowScope,
): void {
  const entry = mediaRoomEscrow;
  if (entry?.claimed && sameEscrowScope(entry.value.scope, expected)) {
    mediaRoomEscrow = null;
  }
}

export function clearMediaRoomEscrow(): void {
  mediaRoomEscrow = null;
}

function sameEscrowScope(
  left: MediaRoomEscrowScope,
  right: MediaRoomEscrowScope,
): boolean {
  return (
    left.tenantId === right.tenantId &&
    left.userId === right.userId &&
    left.spaceId === right.spaceId &&
    left.roomInstanceId === right.roomInstanceId
  );
}

type AudioOutputElement = Omit<HTMLAudioElement, "setSinkId"> & {
  setSinkId?: (deviceId: string) => Promise<void>;
};

interface MediaPrejoinEnvironment {
  mediaDevices: MediaDevices;
  createAudioContext: () => AudioContext | null;
  createAudioElement: () => AudioOutputElement;
  requestFrame: (callback: FrameRequestCallback) => number;
  cancelFrame: (handle: number) => void;
  setTimer: (callback: () => void, delay: number) => number;
  clearTimer: (handle: number) => void;
}

type SnapshotListener = () => void;

const emptyAudioProcessing = {
  echoCancellation: null,
  noiseSuppression: null,
  autoGainControl: null,
} as const;

export class MediaPrejoinController {
  private readonly environment: MediaPrejoinEnvironment;
  private readonly listeners = new Set<SnapshotListener>();
  private currentSnapshot: MediaPrejoinSnapshot = {
    status: "idle",
    permissionGranted: false,
    microphones: [],
    cameras: [],
    speakers: [],
    selectedMicrophoneId: "",
    selectedCameraId: "",
    selectedSpeakerId: "",
    audioMode: "speech",
    micLevel: 0,
    actualAudioProcessing: emptyAudioProcessing,
    errorCode: null,
    announcement: "",
    speakerTestStatus: "idle",
  };
  private stream: MediaStream | null = null;
  private previewElement: HTMLVideoElement | null = null;
  private meterContext: AudioContext | null = null;
  private meterSource: MediaStreamAudioSourceNode | null = null;
  private meterAnalyser: AnalyserNode | null = null;
  private meterFrame: number | null = null;
  private speakerContext: AudioContext | null = null;
  private speakerOscillator: OscillatorNode | null = null;
  private speakerElement: AudioOutputElement | null = null;
  private speakerTimer: number | null = null;
  private speakerGeneration = 0;
  private speakerTestInFlight = false;
  private deviceChangeTimer: number | null = null;
  private generation = 0;
  private connected = false;
  private disposed = false;

  constructor(environment?: Partial<MediaPrejoinEnvironment>) {
    const mediaDevices =
      environment?.mediaDevices ??
      (typeof navigator === "undefined" ? undefined : navigator.mediaDevices);
    if (!mediaDevices) {
      throw new Error("MediaDevices is unavailable");
    }
    this.environment = {
      mediaDevices,
      createAudioContext:
        environment?.createAudioContext ??
        (() => {
          const Constructor = globalThis.AudioContext;
          return Constructor ? new Constructor() : null;
        }),
      createAudioElement:
        environment?.createAudioElement ??
        (() => document.createElement("audio") as AudioOutputElement),
      requestFrame:
        environment?.requestFrame ??
        ((callback) => globalThis.requestAnimationFrame(callback)),
      cancelFrame:
        environment?.cancelFrame ??
        ((handle) => globalThis.cancelAnimationFrame(handle)),
      setTimer:
        environment?.setTimer ??
        ((callback, delay) => globalThis.setTimeout(callback, delay)),
      clearTimer:
        environment?.clearTimer ??
        ((handle) => globalThis.clearTimeout(handle)),
    };
  }

  getSnapshot = (): MediaPrejoinSnapshot => this.currentSnapshot;

  subscribe = (listener: SnapshotListener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  connect(): void {
    if (this.connected || this.disposed) {
      return;
    }
    this.environment.mediaDevices.addEventListener(
      "devicechange",
      this.handleDeviceChange,
    );
    this.connected = true;
  }

  attachPreview(element: HTMLVideoElement | null): void {
    if (this.previewElement && this.previewElement !== element) {
      this.previewElement.srcObject = null;
    }
    this.previewElement = element;
    if (element) {
      element.muted = true;
      element.srcObject = this.stream;
      if (this.stream) {
        void element.play().catch(() => undefined);
      }
    }
  }

  setAudioMode(mode: MediaAudioMode): void {
    if (this.currentSnapshot.audioMode === mode || this.disposed) {
      return;
    }
    this.update({ audioMode: mode });
    if (this.stream) {
      void this.startPreview();
    }
  }

  setSpeakerDevice(deviceId: string): void {
    if (this.disposed) {
      return;
    }
    this.update({ selectedSpeakerId: deviceId });
  }

  async startPreview(): Promise<void> {
    if (this.disposed) {
      return;
    }
    const generation = ++this.generation;
    const status = this.stream ? "switching_device" : "requesting_permission";
    this.update({ status, errorCode: null, announcement: "" });
    await this.releasePreviewResources();

    let stream: MediaStream;
    try {
      stream = await this.environment.mediaDevices.getUserMedia(
        this.mediaConstraints(),
      );
    } catch (error) {
      if (isOverconstrainedError(error)) {
        try {
          stream = await this.environment.mediaDevices.getUserMedia({
            audio: true,
            video: true,
          });
        } catch (fallbackError) {
          this.handleProbeFailure(generation, fallbackError);
          return;
        }
      } else {
        this.handleProbeFailure(generation, error);
        return;
      }
    }

    if (this.disposed || generation !== this.generation) {
      stopStream(stream);
      return;
    }
    this.stream = stream;
    const audioTrack = stream.getAudioTracks()[0];
    if (audioTrack && this.currentSnapshot.audioMode === "original_sound") {
      try {
        audioTrack.contentHint = "music";
      } catch {
        // contentHint is progressive enhancement only.
      }
    }
    this.attachPreview(this.previewElement);
    await this.refreshDevices(false);
    if (this.disposed || generation !== this.generation) {
      await this.releasePreviewResources();
      return;
    }
    this.startMeter(stream);
    const settings = audioTrack?.getSettings();
    this.update({
      status: "preview_ready",
      permissionGranted: true,
      errorCode: null,
      actualAudioProcessing: {
        echoCancellation: booleanSetting(settings?.echoCancellation),
        noiseSuppression: booleanSetting(settings?.noiseSuppression),
        autoGainControl: booleanSetting(settings?.autoGainControl),
      },
    });
  }

  async switchDevice(
    kind: "audioinput" | "videoinput",
    deviceId: string,
  ): Promise<void> {
    if (this.disposed) {
      return;
    }
    this.update(
      kind === "audioinput"
        ? { selectedMicrophoneId: deviceId }
        : { selectedCameraId: deviceId },
    );
    await this.startPreview();
  }

  async stopPreview(): Promise<void> {
    if (this.disposed) {
      return;
    }
    ++this.generation;
    this.update({ status: "cleaning_up" });
    await this.releasePreviewResources();
    this.update({
      status: "idle",
      micLevel: 0,
      actualAudioProcessing: emptyAudioProcessing,
      errorCode: null,
    });
  }

  async testSpeaker(): Promise<void> {
    if (
      this.disposed ||
      this.speakerTestInFlight ||
      this.currentSnapshot.speakerTestStatus === "playing"
    ) {
      return;
    }
    const generation = ++this.speakerGeneration;
    this.speakerTestInFlight = true;

    try {
      await this.releaseSpeakerResources();
      if (!this.speakerOperationIsCurrent(generation)) {
        return;
      }
      const context = this.environment.createAudioContext();
      if (!context) {
        this.update({ speakerTestStatus: "unsupported" });
        return;
      }
      this.speakerContext = context;
      if (!this.speakerOperationIsCurrent(generation)) {
        await this.releaseSpeakerResources();
        return;
      }
      const audio = this.environment.createAudioElement();
      this.speakerElement = audio;
      const selected = this.currentSnapshot.selectedSpeakerId;
      if (selected && typeof audio.setSinkId !== "function") {
        await this.releaseSpeakerResources();
        if (this.speakerOperationIsCurrent(generation)) {
          this.update({
            speakerTestStatus: "unsupported",
            errorCode: "audio_output_selection_unsupported",
          });
        }
        return;
      }
      const destination = context.createMediaStreamDestination();
      const oscillator = context.createOscillator();
      this.speakerOscillator = oscillator;
      oscillator.frequency.value = 440;
      oscillator.connect(destination);
      audio.srcObject = destination.stream;
      if (selected && audio.setSinkId) {
        await audio.setSinkId(selected);
      }
      if (!this.speakerOperationIsCurrent(generation)) {
        return;
      }
      await context.resume();
      if (!this.speakerOperationIsCurrent(generation)) {
        return;
      }
      await audio.play();
      if (!this.speakerOperationIsCurrent(generation)) {
        return;
      }
      oscillator.start();
      this.update({ speakerTestStatus: "playing", errorCode: null });
      this.speakerTimer = this.environment.setTimer(() => {
        void this.releaseSpeakerResources().then(() => {
          if (this.speakerOperationIsCurrent(generation)) {
            this.update({ speakerTestStatus: "success" });
          }
        });
      }, 700);
    } catch (error) {
      await this.releaseSpeakerResources();
      if (!this.speakerOperationIsCurrent(generation)) {
        return;
      }
      const code =
        (error instanceof DOMException || error instanceof Error) &&
        error.name === "NotAllowedError"
          ? "media_playback_blocked"
          : mediaPrejoinErrorCode(error);
      this.update({
        speakerTestStatus:
          code === "media_playback_blocked" ? "blocked" : "failed",
        errorCode: code,
      });
    } finally {
      if (generation === this.speakerGeneration) {
        this.speakerTestInFlight = false;
      }
    }
  }

  async stopSpeakerTest(): Promise<void> {
    ++this.speakerGeneration;
    this.speakerTestInFlight = false;
    await this.releaseSpeakerResources();
    if (!this.disposed) {
      this.update({ speakerTestStatus: "idle" });
    }
  }

  choices(listenOnly = false): MediaJoinChoices {
    return {
      audioEnabled:
        !listenOnly && Boolean(this.stream?.getAudioTracks().length),
      videoEnabled:
        !listenOnly && Boolean(this.stream?.getVideoTracks().length),
      audioDeviceId: listenOnly
        ? ""
        : this.currentSnapshot.selectedMicrophoneId,
      videoDeviceId: listenOnly ? "" : this.currentSnapshot.selectedCameraId,
      speakerDeviceId: this.currentSnapshot.selectedSpeakerId,
      audioMode: this.currentSnapshot.audioMode,
    };
  }

  async dispose(): Promise<void> {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    await this.disconnect();
    this.listeners.clear();
  }

  async disconnect(): Promise<void> {
    ++this.generation;
    ++this.speakerGeneration;
    this.speakerTestInFlight = false;
    if (this.deviceChangeTimer !== null) {
      this.environment.clearTimer(this.deviceChangeTimer);
      this.deviceChangeTimer = null;
    }
    if (this.connected) {
      this.environment.mediaDevices.removeEventListener(
        "devicechange",
        this.handleDeviceChange,
      );
      this.connected = false;
    }
    await Promise.all([
      this.releasePreviewResources(),
      this.releaseSpeakerResources(),
    ]);
  }

  private readonly handleDeviceChange = (): void => {
    if (!this.currentSnapshot.permissionGranted || this.disposed) {
      return;
    }
    if (this.deviceChangeTimer !== null) {
      this.environment.clearTimer(this.deviceChangeTimer);
    }
    this.deviceChangeTimer = this.environment.setTimer(() => {
      this.deviceChangeTimer = null;
      void this.refreshDevices(true);
    }, 150);
  };

  private async refreshDevices(announceFallback: boolean): Promise<void> {
    let devices: MediaDeviceInfo[];
    try {
      devices = await this.environment.mediaDevices.enumerateDevices();
    } catch {
      return;
    }
    if (this.disposed) {
      return;
    }
    const microphones = deviceChoices(devices, "audioinput", "Microphone");
    const cameras = deviceChoices(devices, "videoinput", "Camera");
    const speakers = deviceChoices(devices, "audiooutput", "Speaker");
    const microphone = retainDevice(
      this.currentSnapshot.selectedMicrophoneId,
      microphones,
    );
    const camera = retainDevice(this.currentSnapshot.selectedCameraId, cameras);
    const speaker = retainDevice(
      this.currentSnapshot.selectedSpeakerId,
      speakers,
    );
    const changed =
      Boolean(
        this.currentSnapshot.selectedMicrophoneId && !microphone.retained,
      ) ||
      Boolean(this.currentSnapshot.selectedCameraId && !camera.retained) ||
      Boolean(this.currentSnapshot.selectedSpeakerId && !speaker.retained);
    this.update({
      microphones,
      cameras,
      speakers,
      selectedMicrophoneId: microphone.deviceId,
      selectedCameraId: camera.deviceId,
      selectedSpeakerId: speaker.deviceId,
      announcement:
        announceFallback && changed ? "media_device_selection_reset" : "",
    });
  }

  private mediaConstraints(): MediaStreamConstraints {
    const supported = this.environment.mediaDevices.getSupportedConstraints();
    const original = this.currentSnapshot.audioMode === "original_sound";
    const audio: MediaTrackConstraints = {};
    if (this.currentSnapshot.selectedMicrophoneId) {
      audio.deviceId = { exact: this.currentSnapshot.selectedMicrophoneId };
    }
    if (supported.echoCancellation) {
      audio.echoCancellation = { ideal: !original };
    }
    if (supported.noiseSuppression) {
      audio.noiseSuppression = { ideal: !original };
    }
    if (supported.autoGainControl) {
      audio.autoGainControl = { ideal: !original };
    }
    const video: MediaTrackConstraints = {};
    if (this.currentSnapshot.selectedCameraId) {
      video.deviceId = { exact: this.currentSnapshot.selectedCameraId };
    }
    return { audio, video };
  }

  private handleProbeFailure(generation: number, error: unknown): void {
    if (this.disposed || generation !== this.generation) {
      return;
    }
    this.update({
      status: "degraded",
      errorCode: mediaPrejoinErrorCode(error),
      micLevel: 0,
      actualAudioProcessing: emptyAudioProcessing,
    });
  }

  private startMeter(stream: MediaStream): void {
    const context = this.environment.createAudioContext();
    if (!context || stream.getAudioTracks().length === 0) {
      if (context) {
        void context.close().catch(() => undefined);
      }
      return;
    }
    try {
      const source = context.createMediaStreamSource(stream);
      const analyser = context.createAnalyser();
      analyser.fftSize = 256;
      source.connect(analyser);
      this.meterContext = context;
      this.meterSource = source;
      this.meterAnalyser = analyser;
      const samples = new Uint8Array(analyser.fftSize);
      const sample = () => {
        if (this.disposed || this.meterAnalyser !== analyser) {
          return;
        }
        analyser.getByteTimeDomainData(samples);
        let peak = 0;
        for (const value of samples) {
          peak = Math.max(peak, Math.abs(value - 128));
        }
        this.update({ micLevel: Math.min(1, peak / 64) });
        this.meterFrame = this.environment.requestFrame(sample);
      };
      void context.resume().catch(() => undefined);
      this.meterFrame = this.environment.requestFrame(sample);
    } catch {
      void context.close().catch(() => undefined);
    }
  }

  private async releasePreviewResources(): Promise<void> {
    if (this.meterFrame !== null) {
      this.environment.cancelFrame(this.meterFrame);
      this.meterFrame = null;
    }
    this.meterSource?.disconnect();
    this.meterAnalyser?.disconnect();
    this.meterSource = null;
    this.meterAnalyser = null;
    const meterContext = this.meterContext;
    this.meterContext = null;
    if (meterContext) {
      await meterContext.close().catch(() => undefined);
    }
    if (this.stream) {
      stopStream(this.stream);
      this.stream = null;
    }
    if (this.previewElement) {
      this.previewElement.srcObject = null;
    }
  }

  private async releaseSpeakerResources(): Promise<void> {
    const timer = this.speakerTimer;
    this.speakerTimer = null;
    if (timer !== null) {
      this.environment.clearTimer(timer);
    }
    const oscillator = this.speakerOscillator;
    this.speakerOscillator = null;
    if (oscillator) {
      try {
        oscillator.stop();
      } catch {
        // Already stopped.
      }
      oscillator.disconnect();
    }
    const element = this.speakerElement;
    this.speakerElement = null;
    if (element) {
      element.pause();
      element.srcObject = null;
    }
    const context = this.speakerContext;
    this.speakerContext = null;
    if (context) {
      await context.close().catch(() => undefined);
    }
  }

  private speakerOperationIsCurrent(generation: number): boolean {
    return !this.disposed && generation === this.speakerGeneration;
  }

  private update(patch: Partial<MediaPrejoinSnapshot>): void {
    this.currentSnapshot = { ...this.currentSnapshot, ...patch };
    for (const listener of this.listeners) {
      listener();
    }
  }
}

export function mediaPrejoinErrorCode(error: unknown): MediaPrejoinErrorCode {
  const name =
    error instanceof DOMException || error instanceof Error ? error.name : "";
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      return "media_permission_denied_or_blocked";
    case "NotFoundError":
    case "DevicesNotFoundError":
      return "media_device_not_found";
    case "NotReadableError":
    case "TrackStartError":
      return "media_device_busy_or_unreadable";
    case "OverconstrainedError":
    case "ConstraintNotSatisfiedError":
      return "media_constraints_unavailable";
    case "AbortError":
      return "media_probe_aborted";
    case "TypeError":
      return "media_capture_unsupported_or_policy_blocked";
    case "NotSupportedError":
      return "audio_output_selection_unsupported";
    default:
      if (name === "NotAllowedPlaybackError") {
        return "media_playback_blocked";
      }
      return "media_device_unknown";
  }
}

export type MediaNetworkStatus =
  "checking" | "ready" | "offline" | "unavailable";

export interface MediaNetworkProbeResult {
  status: Exclude<MediaNetworkStatus, "checking">;
  latency: "fast" | "moderate" | "slow" | "unknown";
}

export async function probeMediaNetwork(
  options: {
    baseUrl?: string;
    fetch?: typeof globalThis.fetch;
    signal?: AbortSignal;
    online?: boolean;
    now?: () => number;
  } = {},
): Promise<MediaNetworkProbeResult> {
  const online =
    options.online ??
    (typeof navigator === "undefined" ? true : navigator.onLine);
  if (!online) {
    return { status: "offline", latency: "unknown" };
  }
  const fetcher = options.fetch ?? globalThis.fetch;
  if (typeof fetcher !== "function") {
    return { status: "unavailable", latency: "unknown" };
  }
  const now = options.now ?? (() => performance.now());
  const controller = new AbortController();
  const abort = () => controller.abort();
  options.signal?.addEventListener("abort", abort, { once: true });
  const timer = globalThis.setTimeout(abort, 5_000);
  const baseUrl = (
    options.baseUrl ??
    import.meta.env.VITE_API_BASE_URL ??
    "/api"
  ).replace(/\/$/, "");
  const startedAt = now();
  try {
    const response = await fetcher(`${baseUrl}/health`, {
      method: "GET",
      credentials: "include",
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal: controller.signal,
    });
    if (!response.ok) {
      return { status: "unavailable", latency: "unknown" };
    }
    const duration = Math.max(0, now() - startedAt);
    return {
      status: "ready",
      latency: duration < 350 ? "fast" : duration < 1_200 ? "moderate" : "slow",
    };
  } catch {
    return {
      status:
        controller.signal.aborted && options.signal?.aborted
          ? "offline"
          : "unavailable",
      latency: "unknown",
    };
  } finally {
    globalThis.clearTimeout(timer);
    options.signal?.removeEventListener("abort", abort);
  }
}

function deviceChoices(
  devices: readonly MediaDeviceInfo[],
  kind: MediaDeviceKind,
  genericLabel: string,
): MediaDeviceChoice[] {
  let index = 0;
  return devices
    .filter((device) => device.kind === kind)
    .map((device) => {
      index += 1;
      return {
        deviceId: device.deviceId,
        label: device.label.trim() || `${genericLabel} ${index}`,
      };
    });
}

function retainDevice(
  current: string,
  devices: readonly MediaDeviceChoice[],
): { deviceId: string; retained: boolean } {
  if (current && devices.some((device) => device.deviceId === current)) {
    return { deviceId: current, retained: true };
  }
  return { deviceId: devices[0]?.deviceId ?? "", retained: !current };
}

function stopStream(stream: MediaStream): void {
  for (const track of stream.getTracks()) {
    track.stop();
  }
}

function booleanSetting(value: boolean | undefined): boolean | null {
  return typeof value === "boolean" ? value : null;
}

function isOverconstrainedError(error: unknown): boolean {
  return (
    (error instanceof DOMException || error instanceof Error) &&
    (error.name === "OverconstrainedError" ||
      error.name === "ConstraintNotSatisfiedError")
  );
}
