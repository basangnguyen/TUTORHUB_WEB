const classInvitationTokenPattern = /^thciv1_[A-Za-z0-9_-]{43}$/;

/**
 * Accepts either the raw bearer token or a copied invitation URL whose token
 * remains in the fragment. Query/path tokens are intentionally not accepted.
 */
export function parseClassInvitationToken(value: string): string | null {
  const candidate = value.normalize("NFC").trim();
  if (classInvitationTokenPattern.test(candidate)) {
    return candidate;
  }

  let invitationURL: URL;
  try {
    invitationURL = new URL(candidate);
  } catch {
    return null;
  }

  const fragment = invitationURL.hash.startsWith("#")
    ? invitationURL.hash.slice(1)
    : invitationURL.hash;
  const token = new URLSearchParams(fragment).get("token")?.trim() ?? "";
  return classInvitationTokenPattern.test(token) ? token : null;
}
