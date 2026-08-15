import {
  exportMediaDiagnostics,
  getCurrentCSRFToken,
  recordMediaSpaceDiagnostic,
  rotateCSRFToken,
  type MediaDiagnosticErrorCode,
  type MediaDiagnosticExport,
  type MediaDiagnosticMediaPath,
  type MediaDiagnosticNetworkQuality,
  type MediaDiagnosticOutcome,
  type MediaDiagnosticStage,
} from "@tutorhub/api-client";
import type { MediaJoinChoices } from "./mediaPrejoin";

export interface MediaDiagnosticEvent {
  tenantID: string;
  spaceID: string;
  roomInstanceID: string;
  joinAttemptID: string;
  stage: MediaDiagnosticStage;
  outcome: MediaDiagnosticOutcome;
  errorCode?: MediaDiagnosticErrorCode;
  networkQuality?: MediaDiagnosticNetworkQuality;
  mediaPath?: MediaDiagnosticMediaPath;
  durationMS?: number;
}

export function mediaDiagnosticPath(
  choices: Pick<MediaJoinChoices, "audioEnabled" | "videoEnabled">,
): MediaDiagnosticMediaPath {
  if (choices.videoEnabled) return "audio_video";
  if (choices.audioEnabled) return "audio_only";
  return "listen_only";
}

export function mediaDiagnosticNetwork(
  latency: "fast" | "moderate" | "slow" | "unknown",
): MediaDiagnosticNetworkQuality {
  if (latency === "fast") return "good";
  if (latency === "moderate") return "degraded";
  if (latency === "slow") return "poor";
  return "unknown";
}

export async function recordBoundedMediaDiagnostic(
  event: MediaDiagnosticEvent,
): Promise<void> {
  if (
    !event.tenantID ||
    !event.spaceID ||
    !event.roomInstanceID ||
    !event.joinAttemptID
  ) {
    return;
  }
  try {
    const csrfToken = getCurrentCSRFToken();
    if (!csrfToken) return;
    await recordMediaSpaceDiagnostic(
      event.tenantID,
      event.spaceID,
      {
        event_id: globalThis.crypto.randomUUID(),
        room_instance_id: event.roomInstanceID,
        join_attempt_id: event.joinAttemptID,
        stage: event.stage,
        outcome: event.outcome,
        ...(event.errorCode ? { error_code: event.errorCode } : {}),
        network_quality: event.networkQuality ?? "unknown",
        media_path: event.mediaPath ?? "unknown",
        duration_ms: Math.min(
          600_000,
          Math.max(0, Math.round(event.durationMS ?? 0)),
        ),
      },
      csrfToken,
    );
  } catch {
    // Diagnostics are best-effort and must never block Join, Leave, or recovery.
  }
}

export async function loadMediaDiagnostics(
  tenantID: string,
  hours = 24,
): Promise<MediaDiagnosticExport> {
  const to = new Date();
  const from = new Date(
    to.getTime() - Math.min(31 * 24, Math.max(1, hours)) * 3_600_000,
  );
  const csrf = await rotateCSRFToken();
  return exportMediaDiagnostics(
    tenantID,
    { from: from.toISOString(), to: to.toISOString(), limit: 1000 },
    csrf.csrf_token,
  );
}
