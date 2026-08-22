import {
  CANONICAL_EXCALIDRAW_LIMITS,
  CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  semanticHash,
  validateCanonicalScene,
  type CanonicalExcalidrawSceneV1,
  type JsonValue,
} from "./canonicalAuthority.js";

export const PORTABLE_EXCALIDRAW_FORMAT =
  "tutorhub.excalidraw.portable-scene" as const;
export const PORTABLE_EXCALIDRAW_FORMAT_VERSION = 1 as const;
export const EXCALIDRAW_ENGINE_VERSION = "0.18.1" as const;

export const PORTABLE_EXCALIDRAW_LIMITS = {
  maxBytes: 16 * 1024 * 1024,
  maxDepth: CANONICAL_EXCALIDRAW_LIMITS.maxDepth + 4,
} as const;

export type PortableSceneErrorCode =
  | "portable_active_content_denied"
  | "portable_corrupt"
  | "portable_format_unsupported"
  | "portable_hash_mismatch"
  | "portable_too_large";

export class PortableSceneError extends Error {
  constructor(readonly code: PortableSceneErrorCode) {
    super(code);
    this.name = "PortableSceneError";
  }
}

export interface PortableExcalidrawSceneV1 {
  canonicalSchemaVersion: typeof CANONICAL_EXCALIDRAW_SCHEMA_VERSION;
  engine: {
    name: "excalidraw";
    version: typeof EXCALIDRAW_ENGINE_VERSION;
  };
  exportedAt: string;
  format: typeof PORTABLE_EXCALIDRAW_FORMAT;
  formatVersion: typeof PORTABLE_EXCALIDRAW_FORMAT_VERSION;
  scene: CanonicalExcalidrawSceneV1;
  semanticHash: string;
}

export function exportPortableScene(
  scene: unknown,
  exportedAt = new Date().toISOString(),
): Uint8Array {
  const canonical = validateCanonicalScene(scene);
  if (!isIsoDate(exportedAt)) {
    throw new PortableSceneError("portable_corrupt");
  }
  assertNoActiveContent(canonical);
  const envelope: PortableExcalidrawSceneV1 = {
    canonicalSchemaVersion: CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
    engine: { name: "excalidraw", version: EXCALIDRAW_ENGINE_VERSION },
    exportedAt,
    format: PORTABLE_EXCALIDRAW_FORMAT,
    formatVersion: PORTABLE_EXCALIDRAW_FORMAT_VERSION,
    scene: canonical,
    semanticHash: semanticHash(canonical),
  };
  const bytes = new TextEncoder().encode(JSON.stringify(envelope));
  if (bytes.byteLength > PORTABLE_EXCALIDRAW_LIMITS.maxBytes) {
    throw new PortableSceneError("portable_too_large");
  }
  return bytes;
}

export function importPortableScene(
  bytes: Uint8Array,
): CanonicalExcalidrawSceneV1 {
  if (bytes.byteLength === 0) {
    throw new PortableSceneError("portable_corrupt");
  }
  if (bytes.byteLength > PORTABLE_EXCALIDRAW_LIMITS.maxBytes) {
    throw new PortableSceneError("portable_too_large");
  }
  let candidate: unknown;
  try {
    candidate = JSON.parse(
      new TextDecoder("utf-8", { fatal: true }).decode(bytes),
    );
  } catch {
    throw new PortableSceneError("portable_corrupt");
  }
  if (!isRecord(candidate)) {
    throw new PortableSceneError("portable_corrupt");
  }
  if (
    candidate.format !== PORTABLE_EXCALIDRAW_FORMAT ||
    candidate.formatVersion !== PORTABLE_EXCALIDRAW_FORMAT_VERSION ||
    candidate.canonicalSchemaVersion !== CANONICAL_EXCALIDRAW_SCHEMA_VERSION ||
    !isRecord(candidate.engine) ||
    candidate.engine.name !== "excalidraw" ||
    candidate.engine.version !== EXCALIDRAW_ENGINE_VERSION
  ) {
    throw new PortableSceneError("portable_format_unsupported");
  }
  if (
    typeof candidate.exportedAt !== "string" ||
    !isIsoDate(candidate.exportedAt) ||
    typeof candidate.semanticHash !== "string"
  ) {
    throw new PortableSceneError("portable_corrupt");
  }
  assertNoActiveContent(candidate.scene);
  let scene: CanonicalExcalidrawSceneV1;
  try {
    scene = validateCanonicalScene(candidate.scene);
  } catch {
    throw new PortableSceneError("portable_corrupt");
  }
  if (semanticHash(scene) !== candidate.semanticHash) {
    throw new PortableSceneError("portable_hash_mismatch");
  }
  return scene;
}

function assertNoActiveContent(value: unknown, depth = 0): void {
  if (depth > PORTABLE_EXCALIDRAW_LIMITS.maxDepth) {
    throw new PortableSceneError("portable_corrupt");
  }
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase();
    if (
      normalized.startsWith("http:") ||
      normalized.startsWith("https:") ||
      normalized.startsWith("//") ||
      normalized.startsWith("file:") ||
      normalized.startsWith("javascript:") ||
      normalized.startsWith("vbscript:") ||
      normalized.includes("<script") ||
      normalized.includes("<iframe") ||
      normalized.includes("<svg") ||
      normalized.includes("../") ||
      normalized.includes("..\\")
    ) {
      throw new PortableSceneError("portable_active_content_denied");
    }
    if (
      normalized.startsWith("data:") &&
      !/^data:image\/(png|jpeg|webp|gif);base64,[a-z0-9+/=]+$/i.test(value)
    ) {
      throw new PortableSceneError("portable_active_content_denied");
    }
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) assertNoActiveContent(item, depth + 1);
    return;
  }
  if (isRecord(value)) {
    for (const [key, item] of Object.entries(value)) {
      if (key === "__proto__" || key === "constructor" || key === "prototype") {
        throw new PortableSceneError("portable_active_content_denied");
      }
      assertNoActiveContent(item as JsonValue, depth + 1);
    }
  }
}

function isIsoDate(value: string): boolean {
  const timestamp = Date.parse(value);
  return (
    Number.isFinite(timestamp) && new Date(timestamp).toISOString() === value
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
