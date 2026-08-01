import {
  clearFragmentTokenEscrow,
  consumeFragmentToken,
} from "./fragmentToken";

const publicAvailabilityPollEscrowKey = "public-availability-poll";

export function consumePublicAvailabilityPollToken(): string | null {
  return consumeFragmentToken(publicAvailabilityPollEscrowKey, 512);
}

/**
 * Runs during application bootstrap so a public poll capability is removed
 * before React mounts session queries or any route-level data loader.
 */
export function primePublicAvailabilityPollToken(): void {
  if (/^\/availability\/[^/]+\/?$/.test(window.location.pathname)) {
    consumePublicAvailabilityPollToken();
  }
}

export function clearPublicAvailabilityPollToken(): void {
  clearFragmentTokenEscrow(publicAvailabilityPollEscrowKey);
}
