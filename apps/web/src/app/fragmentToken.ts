interface FragmentTokenEscrow {
  cleanURL: string;
  tokens: Readonly<Record<string, string | null>>;
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
  return consumeFragmentTokens(escrowKey, ["token"], maximumLength).token;
}

/**
 * Consumes a fixed set of bearer values from one URL fragment and erases the
 * complete fragment before React starts any network request. Unknown fragment
 * fields are intentionally discarded instead of being retained in history.
 */
export function consumeFragmentTokens<const Name extends string>(
  escrowKey: string,
  names: readonly Name[],
  maximumLength = 512,
): Readonly<Record<Name, string | null>> {
  const cleanURL = `${window.location.pathname}${window.location.search}`;
  const hash = window.location.hash.startsWith("#")
    ? window.location.hash.slice(1)
    : window.location.hash;

  if (hash) {
    const parameters = new URLSearchParams(hash);
    const tokens = Object.fromEntries(
      names.map((name) => {
        const candidate = parameters.get(name)?.trim() ?? "";
        return [
          name,
          candidate.length > 0 && candidate.length <= maximumLength
            ? candidate
            : null,
        ];
      }),
    ) as Record<Name, string | null>;
    fragmentTokenEscrows.set(escrowKey, { cleanURL, tokens });
    window.history.replaceState(window.history.state, "", cleanURL);
    return tokens;
  }

  const escrow = fragmentTokenEscrows.get(escrowKey);
  if (escrow?.cleanURL === cleanURL) {
    return Object.fromEntries(
      names.map((name) => [name, escrow.tokens[name] ?? null]),
    ) as Record<Name, string | null>;
  }
  return Object.fromEntries(names.map((name) => [name, null])) as Record<
    Name,
    string | null
  >;
}

export function clearFragmentTokenEscrow(escrowKey: string) {
  fragmentTokenEscrows.delete(escrowKey);
}
