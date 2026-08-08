import { APIRequestError } from "@tutorhub/api-client";

export const idempotentMutationRetryDelay = 400;

export function shouldRetryIdempotentMutation(
  failureCount: number,
  error: Error,
) {
  if (failureCount >= 1) {
    return false;
  }
  if (error instanceof APIRequestError) {
    return error.status >= 500 && error.status < 600;
  }
  return error instanceof TypeError;
}
