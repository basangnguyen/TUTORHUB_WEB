import { describe, expect, it } from "vitest";
import {
  hasMediaDeviceSupport,
  mediaErrorCode,
  readMediaRoomState,
} from "./media";

describe("media room state", () => {
  it("detects browsers without the required media APIs", () => {
    expect(hasMediaDeviceSupport(undefined)).toBe(false);
    expect(
      hasMediaDeviceSupport({ mediaDevices: {} } as unknown as Navigator),
    ).toBe(false);
    expect(
      hasMediaDeviceSupport({
        mediaDevices: {
          getUserMedia: () => Promise.resolve(new MediaStream()),
          enumerateDevices: () => Promise.resolve([]),
        },
      } as unknown as Navigator),
    ).toBe(true);
  });

  it("accepts complete in-memory navigation state and rejects refresh state", () => {
    const state = {
      csrfToken: "csrf-token",
      credential: {
        access_token: "short-lived-token",
        server_url: "wss://staging.example.test",
        room_name: "th_tenant_class",
        participant_identity: "u_actor_s_session",
        participant_name: "Student",
        attempt_id: "16abf9c1-69af-44a7-a844-36a6a19e2db2",
        can_publish: true,
        expires_at: "2026-07-14T05:05:00Z",
      },
      choices: {
        videoEnabled: true,
        audioEnabled: true,
        videoDeviceId: "camera",
        audioDeviceId: "microphone",
        username: "Student",
      },
    };

    expect(readMediaRoomState(state)).toEqual(state);
    expect(readMediaRoomState(null)).toBeNull();
    expect(
      readMediaRoomState({ ...state, credential: { access_token: "" } }),
    ).toBeNull();
  });

  it("maps raw device errors to bounded telemetry codes", () => {
    expect(mediaErrorCode(new DOMException("denied", "NotAllowedError"))).toBe(
      "device_permission_denied",
    );
    expect(mediaErrorCode(new Error("connection failed"))).toBe(
      "livekit_error",
    );
  });
});
