import {
  issueClassMediaToken,
  recordClassMediaEvent,
  rotateCSRFToken,
  type MediaEventRequest,
  type MediaTokenResponse,
} from "@tutorhub/api-client";
import type { LocalUserChoices } from "@livekit/components-react";

export interface MediaJoinSetup {
  credential: MediaTokenResponse;
  csrfToken: string;
}

export interface MediaRoomNavigationState extends MediaJoinSetup {
  choices: LocalUserChoices;
}

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export async function requestMediaJoinSetup(
  classID: string,
  signal?: AbortSignal,
): Promise<MediaJoinSetup> {
  const baseUrl = getApiBaseUrl();
  const csrf = await rotateCSRFToken({ baseUrl, signal });
  const credential = await issueClassMediaToken(classID, csrf.csrf_token, {
    baseUrl,
    signal,
  });

  return { credential, csrfToken: csrf.csrf_token };
}

export async function recordMediaEvent(
  classID: string,
  csrfToken: string,
  event: MediaEventRequest,
): Promise<void> {
  await recordClassMediaEvent(classID, event, csrfToken, {
    baseUrl: getApiBaseUrl(),
  });
}

export function hasMediaDeviceSupport(target: typeof navigator | undefined) {
  return Boolean(
    target?.mediaDevices &&
    typeof target.mediaDevices.getUserMedia === "function" &&
    typeof target.mediaDevices.enumerateDevices === "function",
  );
}

export function readMediaRoomState(
  value: unknown,
): MediaRoomNavigationState | null {
  if (
    !isRecord(value) ||
    !isRecord(value.credential) ||
    !isRecord(value.choices)
  ) {
    return null;
  }

  const credential = value.credential;
  const choices = value.choices;
  if (
    typeof value.csrfToken !== "string" ||
    value.csrfToken.length === 0 ||
    typeof credential.access_token !== "string" ||
    credential.access_token.length === 0 ||
    typeof credential.server_url !== "string" ||
    !credential.server_url.startsWith("wss://") ||
    typeof credential.room_name !== "string" ||
    typeof credential.participant_identity !== "string" ||
    typeof credential.participant_name !== "string" ||
    typeof credential.attempt_id !== "string" ||
    typeof credential.can_publish !== "boolean" ||
    typeof credential.expires_at !== "string" ||
    typeof choices.videoEnabled !== "boolean" ||
    typeof choices.audioEnabled !== "boolean" ||
    typeof choices.videoDeviceId !== "string" ||
    typeof choices.audioDeviceId !== "string" ||
    typeof choices.username !== "string"
  ) {
    return null;
  }

  return value as unknown as MediaRoomNavigationState;
}

export function mediaErrorCode(error: unknown): string {
  if (error instanceof DOMException) {
    switch (error.name) {
      case "NotAllowedError":
      case "SecurityError":
        return "device_permission_denied";
      case "NotFoundError":
      case "DevicesNotFoundError":
        return "device_not_found";
      case "NotReadableError":
      case "TrackStartError":
        return "device_in_use";
      default:
        return "media_device_error";
    }
  }
  if (error instanceof Error) {
    const name = error.name.toLowerCase().replace(/[^a-z0-9.-]+/g, "_");
    return name ? `livekit_${name}`.slice(0, 64) : "livekit_error";
  }

  return "unknown_media_error";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
