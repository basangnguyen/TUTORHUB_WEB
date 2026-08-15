import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  loadMediaDiagnostics,
  mediaDiagnosticNetwork,
  mediaDiagnosticPath,
  recordBoundedMediaDiagnostic,
} from "./mediaDiagnostics";

const apiMocks = vi.hoisted(() => ({
  exportDiagnostics: vi.fn(),
  getCurrentCSRF: vi.fn(),
  recordDiagnostic: vi.fn(),
  rotateCSRF: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/api-client")>();
  return {
    ...original,
    exportMediaDiagnostics: apiMocks.exportDiagnostics,
    getCurrentCSRFToken: apiMocks.getCurrentCSRF,
    recordMediaSpaceDiagnostic: apiMocks.recordDiagnostic,
    rotateCSRFToken: apiMocks.rotateCSRF,
  };
});

describe("privacy-bounded media diagnostics", () => {
  beforeEach(() => {
    apiMocks.getCurrentCSRF.mockReset().mockReturnValue("csrf");
    apiMocks.rotateCSRF.mockReset().mockResolvedValue({ csrf_token: "csrf" });
    apiMocks.recordDiagnostic.mockReset().mockResolvedValue(undefined);
    apiMocks.exportDiagnostics.mockReset().mockResolvedValue({
      from: "2030-08-02T00:00:00Z",
      to: "2030-08-03T00:00:00Z",
      items: [],
      metrics: {
        join_attempts: 0,
        successful_joins: 0,
        join_success_rate: 0,
        p95_time_to_media_ms: 0,
        reconnect_succeeded: 0,
        reconnect_failed: 0,
      },
      truncated: false,
    });
  });

  it("projects device choices only to a coarse media path", () => {
    expect(
      mediaDiagnosticPath({ audioEnabled: true, videoEnabled: true }),
    ).toBe("audio_video");
    expect(
      mediaDiagnosticPath({ audioEnabled: true, videoEnabled: false }),
    ).toBe("audio_only");
    expect(
      mediaDiagnosticPath({ audioEnabled: false, videoEnabled: false }),
    ).toBe("listen_only");
  });

  it("projects latency without addresses or device identity", () => {
    expect(mediaDiagnosticNetwork("fast")).toBe("good");
    expect(mediaDiagnosticNetwork("moderate")).toBe("degraded");
    expect(mediaDiagnosticNetwork("slow")).toBe("poor");
    expect(mediaDiagnosticNetwork("unknown")).toBe("unknown");
  });

  it("sends only the closed coarse schema and never blocks the room flow", async () => {
    await recordBoundedMediaDiagnostic({
      tenantID: "tenant",
      spaceID: "space",
      roomInstanceID: "room",
      joinAttemptID: "attempt",
      stage: "disconnected",
      outcome: "failed",
      errorCode: "transport_disconnected",
      networkQuality: "offline",
      mediaPath: "audio_only",
      durationMS: 800_000,
    });
    expect(apiMocks.recordDiagnostic).toHaveBeenCalledWith(
      "tenant",
      "space",
      expect.objectContaining({
        room_instance_id: "room",
        join_attempt_id: "attempt",
        error_code: "transport_disconnected",
        duration_ms: 600_000,
      }),
      "csrf",
    );
    const payload = apiMocks.recordDiagnostic.mock.calls.at(0)?.[2];
    expect(payload).toBeDefined();
    expect(Object.keys(payload as object).sort()).toEqual([
      "duration_ms",
      "error_code",
      "event_id",
      "join_attempt_id",
      "media_path",
      "network_quality",
      "outcome",
      "room_instance_id",
      "stage",
    ]);
    expect(apiMocks.rotateCSRF).not.toHaveBeenCalled();

    apiMocks.getCurrentCSRF.mockReturnValueOnce(null);
    await recordBoundedMediaDiagnostic({
      tenantID: "tenant",
      spaceID: "space",
      roomInstanceID: "room",
      joinAttemptID: "attempt",
      stage: "media",
      outcome: "succeeded",
    });
    expect(apiMocks.recordDiagnostic).toHaveBeenCalledTimes(1);

    apiMocks.recordDiagnostic.mockRejectedValueOnce(new Error("offline"));
    await expect(
      recordBoundedMediaDiagnostic({
        tenantID: "tenant",
        spaceID: "space",
        roomInstanceID: "room",
        joinAttemptID: "attempt",
        stage: "leave",
        outcome: "succeeded",
      }),
    ).resolves.toBeUndefined();
  });

  it("caps support export to 31 days and 1000 rows", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2030-08-03T00:00:00Z"));
    await loadMediaDiagnostics("tenant", 9999);
    expect(apiMocks.exportDiagnostics).toHaveBeenCalledWith(
      "tenant",
      {
        from: "2030-07-03T00:00:00.000Z",
        to: "2030-08-03T00:00:00.000Z",
        limit: 1000,
      },
      "csrf",
    );
    vi.useRealTimers();
  });
});
