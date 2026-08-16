import { useMutation } from "@tanstack/react-query";
import {
  APIRequestError,
  createMediaSpace,
  resolveMediaSpace,
  rotateCSRFToken,
  startMediaSpace,
  type MediaSpace,
  type MediaSpaceSource,
} from "@tutorhub/api-client";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export class MediaSpaceNotReadyError extends Error {}

export interface LaunchMediaSpaceInput {
  canStart: boolean;
  source: MediaSpaceSource;
}

async function mutationCSRFToken() {
  const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
  return csrf.csrf_token;
}

export async function launchMediaSpace(
  tenantID: string,
  input: LaunchMediaSpaceInput,
): Promise<MediaSpace> {
  let space: MediaSpace;
  try {
    space = await resolveMediaSpace(tenantID, input.source, {
      baseUrl: getApiBaseUrl(),
    });
  } catch (error) {
    if (!(error instanceof APIRequestError) || error.status !== 404) {
      throw error;
    }
    if (!input.canStart) {
      throw new MediaSpaceNotReadyError(
        "The media space has not been created.",
      );
    }
    space = await createMediaSpace(
      tenantID,
      { idempotency_key: crypto.randomUUID(), source: input.source },
      await mutationCSRFToken(),
      { baseUrl: getApiBaseUrl() },
    );
  }

  if (space.status === "open") {
    return space;
  }
  if (
    space.status !== "scheduled" ||
    !input.canStart ||
    !space.viewer_operations.can_start
  ) {
    throw new MediaSpaceNotReadyError("The media space is not open.");
  }
  return startMediaSpace(
    tenantID,
    space.id,
    {
      expected_version: space.version,
      idempotency_key: crypto.randomUUID(),
      reason_code: "product_launch",
    },
    await mutationCSRFToken(),
    { baseUrl: getApiBaseUrl() },
  );
}

export function useLaunchMediaSpace(tenantID: string | undefined) {
  return useMutation<MediaSpace, Error, LaunchMediaSpaceInput>({
    mutationFn: (input) => {
      if (!tenantID) {
        throw new Error("An active workspace is required.");
      }
      return launchMediaSpace(tenantID, input);
    },
    retry: false,
  });
}
