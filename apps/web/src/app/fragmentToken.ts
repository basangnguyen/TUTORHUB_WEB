interface FragmentTokenEscrow {
  cleanURL: string;
  token: string | null;
}

const fragmentTokenEscrows = new Map<string, FragmentTokenEscrow>();

/**
 * Consumes a bearer token from the URL fragment and removes the fragment from
 * browser history immediately. The short-lived in-memory escrow only exists to
 * survive React Strict Mode's repeated state initializer in development.
 */
export function consumeFragmentToken(
  escrowKey: string,
  maximumLength = 512,
): string | null {
  const cleanURL = `${window.location.pathname}${window.location.search}`;
  const hash = window.location.hash.startsWith("#")
    ? window.location.hash.slice(1)
    : window.location.hash;

  if (hash) {
    const candidate = new URLSearchParams(hash).get("token")?.trim() ?? "";
    const token =
      candidate.length > 0 && candidate.length <= maximumLength
        ? candidate
        : null;
    fragmentTokenEscrows.set(escrowKey, { cleanURL, token });
    window.history.replaceState(window.history.state, "", cleanURL);
    return token;
  }

  const escrow = fragmentTokenEscrows.get(escrowKey);
  return escrow?.cleanURL === cleanURL ? escrow.token : null;
}

export function clearFragmentTokenEscrow(escrowKey: string) {
  fragmentTokenEscrows.delete(escrowKey);
}
