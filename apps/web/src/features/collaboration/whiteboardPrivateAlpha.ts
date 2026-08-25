/**
 * Same-origin fallback for private-alpha support.
 *
 * Keeping this route internal avoids hard-coding a personal email address or
 * sending classroom details to a public issue tracker. The settings surface
 * can later route the report to the tenant's configured support channel.
 */
export const WHITEBOARD_PRIVATE_ALPHA_SUPPORT_HREF =
  "/app/settings?source=whiteboard-private-alpha";
