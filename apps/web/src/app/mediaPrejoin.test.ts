import { afterEach, describe, expect, it, vi } from "vitest";
import {
  clearMediaRoomEscrow,
  finalizeMediaRoomEscrowClaim,
  mediaPrejoinErrorCode,
  MediaPrejoinController,
  probeMediaNetwork,
  putMediaRoomEscrow,
  takeMediaRoomEscrow,
  type MediaRoomEscrowValue,
} from "./mediaPrejoin";

function fakeTrack(kind: "audio" | "video", settings: MediaTrackSettings = {}) {
  return {
    kind,
    contentHint: "",
    stop: vi.fn(),
    getSettings: vi.fn(() => settings),
  } as unknown as MediaStreamTrack;
}

function fakeStream(audio = fakeTrack("audio"), video = fakeTrack("video")) {
  return {
    getTracks: () => [audio, video],
    getAudioTracks: () => [audio],
    getVideoTracks: () => [video],
  } as unknown as MediaStream;
}

function device(
  kind: MediaDeviceKind,
  deviceId: string,
  label: string,
): MediaDeviceInfo {
  return {
    kind,
    deviceId,
    groupId: "",
    label,
    toJSON: () => ({}),
  };
}

class FakeMediaDevices extends EventTarget {
  readonly getUserMedia = vi.fn<MediaDevices["getUserMedia"]>();
  readonly enumerateDevices = vi.fn<MediaDevices["enumerateDevices"]>();
  readonly getSupportedConstraints = vi.fn(() => ({
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
  }));
}

function controllerFor(mediaDevices: FakeMediaDevices) {
  return new MediaPrejoinController({
    mediaDevices: mediaDevices as unknown as MediaDevices,
    createAudioContext: () => null,
    requestFrame: () => 1,
    cancelFrame: () => undefined,
  });
}

describe("MediaPrejoinController", () => {
  afterEach(() => {
    vi.useRealTimers();
    clearMediaRoomEscrow();
  });

  it("does not enumerate or capture devices before an explicit preview action", () => {
    const mediaDevices = new FakeMediaDevices();
    const controller = controllerFor(mediaDevices);
    controller.connect();

    expect(controller.getSnapshot().status).toBe("idle");
    expect(controller.getSnapshot().microphones).toEqual([]);
    expect(mediaDevices.getUserMedia).not.toHaveBeenCalled();
    expect(mediaDevices.enumerateDevices).not.toHaveBeenCalled();
  });

  it("can reconnect after a reversible StrictMode-style effect cleanup", async () => {
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia.mockResolvedValue(fakeStream());
    mediaDevices.enumerateDevices.mockResolvedValue([]);
    const controller = controllerFor(mediaDevices);

    controller.connect();
    await controller.disconnect();
    controller.connect();
    await controller.startPreview();

    expect(controller.getSnapshot().status).toBe("preview_ready");
    expect(mediaDevices.getUserMedia).toHaveBeenCalledTimes(1);
    await controller.dispose();
  });

  it("requests speech processing as ideal, reveals labels only after permission and cleans up", async () => {
    const audio = fakeTrack("audio", {
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: false,
    });
    const video = fakeTrack("video");
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia.mockResolvedValue(fakeStream(audio, video));
    mediaDevices.enumerateDevices.mockResolvedValue([
      device("audioinput", "mic-a", "Desk microphone"),
      device("videoinput", "cam-a", "Front camera"),
      device("audiooutput", "speaker-a", "Headphones"),
    ]);
    const controller = controllerFor(mediaDevices);

    await controller.startPreview();

    expect(mediaDevices.getUserMedia).toHaveBeenCalledWith({
      audio: {
        echoCancellation: { ideal: true },
        noiseSuppression: { ideal: true },
        autoGainControl: { ideal: true },
      },
      video: {},
    });
    expect(controller.getSnapshot()).toMatchObject({
      status: "preview_ready",
      permissionGranted: true,
      selectedMicrophoneId: "mic-a",
      selectedCameraId: "cam-a",
      selectedSpeakerId: "speaker-a",
      actualAudioProcessing: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: false,
      },
    });
    expect(controller.getSnapshot().microphones[0]?.label).toBe(
      "Desk microphone",
    );

    await controller.dispose();
    expect(audio.stop).toHaveBeenCalledTimes(1);
    expect(video.stop).toHaveBeenCalledTimes(1);
  });

  it("uses original-sound constraints only after the user selects that mode", async () => {
    const audio = fakeTrack("audio");
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia.mockResolvedValue(fakeStream(audio));
    mediaDevices.enumerateDevices.mockResolvedValue([]);
    const controller = controllerFor(mediaDevices);
    controller.setAudioMode("original_sound");

    await controller.startPreview();

    expect(mediaDevices.getUserMedia).toHaveBeenCalledWith({
      audio: {
        echoCancellation: { ideal: false },
        noiseSuppression: { ideal: false },
        autoGainControl: { ideal: false },
      },
      video: {},
    });
    expect(audio.contentHint).toBe("music");
    await controller.dispose();
  });

  it("restarts an active preview when the audio profile changes", async () => {
    const firstAudio = fakeTrack("audio");
    const secondAudio = fakeTrack("audio");
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia
      .mockResolvedValueOnce(fakeStream(firstAudio))
      .mockResolvedValueOnce(fakeStream(secondAudio));
    mediaDevices.enumerateDevices.mockResolvedValue([]);
    const controller = controllerFor(mediaDevices);

    await controller.startPreview();
    controller.setAudioMode("original_sound");
    await vi.waitFor(() => {
      expect(mediaDevices.getUserMedia).toHaveBeenCalledTimes(2);
      expect(controller.getSnapshot().status).toBe("preview_ready");
    });

    expect(mediaDevices.getUserMedia).toHaveBeenLastCalledWith({
      audio: {
        echoCancellation: { ideal: false },
        noiseSuppression: { ideal: false },
        autoGainControl: { ideal: false },
      },
      video: {},
    });
    expect(firstAudio.stop).toHaveBeenCalledTimes(1);
    expect(secondAudio.contentHint).toBe("music");
    await controller.dispose();
  });

  it("stops a late stream after dispose invalidates the probe generation", async () => {
    const audio = fakeTrack("audio");
    const video = fakeTrack("video");
    let resolveStream: ((stream: MediaStream) => void) | undefined;
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia.mockReturnValue(
      new Promise<MediaStream>((resolve) => {
        resolveStream = resolve;
      }),
    );
    const controller = controllerFor(mediaDevices);

    const pending = controller.startPreview();
    await controller.dispose();
    resolveStream?.(fakeStream(audio, video));
    await pending;

    expect(audio.stop).toHaveBeenCalledTimes(1);
    expect(video.stop).toHaveBeenCalledTimes(1);
  });

  it("keeps join listen-only available after a bounded device error", async () => {
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia.mockRejectedValue(
      new DOMException("raw browser detail", "NotAllowedError"),
    );
    const controller = controllerFor(mediaDevices);

    await controller.startPreview();

    expect(controller.getSnapshot()).toMatchObject({
      status: "degraded",
      errorCode: "media_permission_denied_or_blocked",
    });
    expect(controller.choices(true)).toMatchObject({
      audioEnabled: false,
      videoEnabled: false,
      audioDeviceId: "",
      videoDeviceId: "",
    });
  });

  it("rescans bounded device changes and falls back once when a selected device disappears", async () => {
    vi.useFakeTimers();
    const mediaDevices = new FakeMediaDevices();
    mediaDevices.getUserMedia.mockResolvedValue(fakeStream());
    mediaDevices.enumerateDevices
      .mockResolvedValueOnce([
        device("audioinput", "mic-a", "Desk microphone"),
        device("videoinput", "cam-a", "Front camera"),
      ])
      .mockResolvedValue([
        device("audioinput", "mic-b", "Backup microphone"),
        device("videoinput", "cam-b", "Backup camera"),
      ]);
    const controller = controllerFor(mediaDevices);
    controller.connect();
    await controller.startPreview();

    for (let index = 0; index < 20; index += 1) {
      mediaDevices.dispatchEvent(new Event("devicechange"));
    }
    await vi.advanceTimersByTimeAsync(150);

    expect(mediaDevices.enumerateDevices).toHaveBeenCalledTimes(2);
    expect(controller.getSnapshot()).toMatchObject({
      selectedMicrophoneId: "mic-b",
      selectedCameraId: "cam-b",
      announcement: "media_device_selection_reset",
    });
    await controller.dispose();
  });

  it("releases every track across twenty explicit device-switch cycles", async () => {
    const mediaDevices = new FakeMediaDevices();
    const lifecycle = Array.from({ length: 21 }, () => ({
      audio: fakeTrack("audio"),
      video: fakeTrack("video"),
    }));
    const streams = lifecycle.map(({ audio, video }) =>
      fakeStream(audio, video),
    );
    mediaDevices.getUserMedia.mockImplementation(async () => {
      const stream = streams.shift();
      if (!stream) {
        throw new Error("unexpected media capture");
      }
      return stream;
    });
    mediaDevices.enumerateDevices.mockResolvedValue([
      device("audioinput", "mic-a", "Desk microphone"),
      device("audioinput", "mic-b", "Backup microphone"),
      device("videoinput", "cam-a", "Front camera"),
      device("videoinput", "cam-b", "Backup camera"),
    ]);
    const controller = controllerFor(mediaDevices);
    controller.connect();

    await controller.startPreview();
    for (let index = 0; index < 20; index += 1) {
      await controller.switchDevice(
        index % 2 === 0 ? "audioinput" : "videoinput",
        index % 4 < 2
          ? index % 2 === 0
            ? "mic-b"
            : "cam-b"
          : index % 2 === 0
            ? "mic-a"
            : "cam-a",
      );
    }
    await controller.dispose();

    expect(mediaDevices.getUserMedia).toHaveBeenCalledTimes(21);
    expect(mediaDevices.enumerateDevices).toHaveBeenCalledTimes(21);
    for (const { audio, video } of lifecycle) {
      expect(audio.stop).toHaveBeenCalledTimes(1);
      expect(video.stop).toHaveBeenCalledTimes(1);
    }

    mediaDevices.dispatchEvent(new Event("devicechange"));
    expect(mediaDevices.enumerateDevices).toHaveBeenCalledTimes(21);
  });

  it("owns an in-flight speaker probe synchronously and cancels late playback", async () => {
    const mediaDevices = new FakeMediaDevices();
    let resolveSink: (() => void) | undefined;
    const oscillator = {
      connect: vi.fn(),
      disconnect: vi.fn(),
      frequency: { value: 0 },
      start: vi.fn(),
      stop: vi.fn(),
    };
    const context = {
      close: vi.fn().mockResolvedValue(undefined),
      createMediaStreamDestination: vi.fn(() => ({
        stream: {} as MediaStream,
      })),
      createOscillator: vi.fn(() => oscillator),
      resume: vi.fn().mockResolvedValue(undefined),
    };
    const audio = {
      pause: vi.fn(),
      play: vi.fn().mockResolvedValue(undefined),
      setSinkId: vi.fn(
        () =>
          new Promise<void>((resolve) => {
            resolveSink = resolve;
          }),
      ),
      srcObject: null as MediaStream | null,
    };
    const createAudioContext = vi.fn(() => context as unknown as AudioContext);
    const controller = new MediaPrejoinController({
      mediaDevices: mediaDevices as unknown as MediaDevices,
      createAudioContext,
      createAudioElement: () =>
        audio as unknown as HTMLAudioElement & {
          setSinkId: (deviceId: string) => Promise<void>;
        },
      requestFrame: () => 1,
      cancelFrame: () => undefined,
    });
    controller.setSpeakerDevice("speaker-a");

    const first = controller.testSpeaker();
    const duplicate = controller.testSpeaker();
    await vi.waitFor(() => expect(audio.setSinkId).toHaveBeenCalledTimes(1));
    expect(createAudioContext).toHaveBeenCalledTimes(1);

    await controller.stopSpeakerTest();
    resolveSink?.();
    await Promise.all([first, duplicate]);

    expect(context.resume).not.toHaveBeenCalled();
    expect(audio.play).not.toHaveBeenCalled();
    expect(oscillator.start).not.toHaveBeenCalled();
    expect(oscillator.disconnect).toHaveBeenCalledTimes(1);
    expect(audio.pause).toHaveBeenCalledTimes(1);
    expect(context.close).toHaveBeenCalledTimes(1);
    expect(controller.getSnapshot().speakerTestStatus).toBe("idle");
  });
});

describe("media prejoin privacy and bounded failures", () => {
  it.each([
    ["NotAllowedError", "media_permission_denied_or_blocked"],
    ["NotFoundError", "media_device_not_found"],
    ["NotReadableError", "media_device_busy_or_unreadable"],
    ["OverconstrainedError", "media_constraints_unavailable"],
    ["AbortError", "media_probe_aborted"],
    ["TypeError", "media_capture_unsupported_or_policy_blocked"],
    ["NotSupportedError", "audio_output_selection_unsupported"],
    ["UnknownError", "media_device_unknown"],
  ])("maps %s without exposing raw browser detail", (name, expected) => {
    expect(
      mediaPrejoinErrorCode(new DOMException("private detail", name)),
    ).toBe(expected);
  });

  it("uses a one-time in-memory room escrow and rejects a scope change", () => {
    const value: MediaRoomEscrowValue = {
      scope: {
        tenantId: "tenant-a",
        userId: "user-a",
        spaceId: "space-a",
        roomInstanceId: "room-a",
      },
      credential: {
        access_token: "memory-only-token",
        server_url: "wss://media.example.test",
        participant_session_id: "participant-a",
        room_instance_id: "room-a",
        join_attempt_id: "attempt-a",
        instance_role: "attendee",
        can_publish_camera_microphone: true,
        can_share_screen: false,
        can_subscribe: true,
        expires_at: "2030-01-01T00:05:00Z",
      },
      choices: {
        audioEnabled: true,
        videoEnabled: false,
        audioDeviceId: "mic-a",
        videoDeviceId: "",
        speakerDeviceId: "speaker-a",
        audioMode: "speech",
      },
    };
    putMediaRoomEscrow(value);

    expect(
      takeMediaRoomEscrow({ ...value.scope, tenantId: "tenant-b" }),
    ).toBeNull();
    putMediaRoomEscrow(value);
    expect(takeMediaRoomEscrow(value.scope)).toEqual(value);
    expect(takeMediaRoomEscrow(value.scope)).toEqual(value);
    finalizeMediaRoomEscrowClaim(value.scope);
    expect(takeMediaRoomEscrow(value.scope)).toBeNull();
  });

  it("performs only a coarse same-origin health probe and handles offline", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response("{}", { status: 200 }));
    const clock = vi.fn().mockReturnValueOnce(0).mockReturnValueOnce(120);

    await expect(
      probeMediaNetwork({
        baseUrl: "/api",
        fetch: fetchMock,
        online: true,
        now: clock,
      }),
    ).resolves.toEqual({ status: "ready", latency: "fast" });
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/health",
      expect.objectContaining({ method: "GET", cache: "no-store" }),
    );
    expect(JSON.stringify(fetchMock.mock.calls)).not.toMatch(/ice|sdp|turn/i);

    await expect(
      probeMediaNetwork({ fetch: fetchMock, online: false }),
    ).resolves.toEqual({ status: "offline", latency: "unknown" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
