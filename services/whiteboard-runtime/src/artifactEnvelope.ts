import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import {
  CanonicalExcalidrawAuthority,
  exportPortableScene,
  importPortableScene,
  semanticHash,
  type CanonicalAuthorityScope,
  type CanonicalExcalidrawSceneV1,
} from "@tutorhub/collaboration-client";
import * as Y from "yjs";

export const ARTIFACT_FORMAT = "tutorhub.whiteboard.snapshot-envelope" as const;
export const ARTIFACT_FORMAT_VERSION = 2 as const;
export const MAX_ARTIFACT_BYTES = 32 * 1024 * 1024;
export const MAX_PROVIDER_STATE_BYTES = 20 * 1024 * 1024;

const MAX_JSON_DEPTH = 28;

export type ArtifactEnvelopeErrorCode =
  | "artifact_active_content_denied"
  | "artifact_binding_invalid"
  | "artifact_checksum_mismatch"
  | "artifact_corrupt"
  | "artifact_scope_mismatch"
  | "artifact_too_large"
  | "artifact_version_unsupported";

export class ArtifactEnvelopeError extends Error {
  constructor(readonly code: ArtifactEnvelopeErrorCode) {
    super(code);
    this.name = "ArtifactEnvelopeError";
  }
}

export interface ArtifactBindingKey {
  id: string;
  secret: string;
}

export interface ArtifactEnvelopeResult {
  bytes: Uint8Array;
  causalWatermarkSha256: string;
  contentSha256: string;
  semanticHash: string;
}

export interface VerifiedArtifactEnvelope {
  providerState: Uint8Array;
  scene: CanonicalExcalidrawSceneV1;
  semanticHash: string;
}

interface UnsignedArtifactEnvelope {
  authority: { name: "yjs"; version: "13.6.27" };
  causalWatermark: string;
  causalWatermarkSha256: string;
  createdAt: string;
  engine: { name: "excalidraw"; version: "0.18.1" };
  format: typeof ARTIFACT_FORMAT;
  formatVersion: typeof ARTIFACT_FORMAT_VERSION;
  portableScene: unknown;
  providerState: string;
  providerStateSha256: string;
  provenance: { kind: "service"; name: "tutorhub-whiteboard-artifact-worker" };
  schemaVersion: 1;
  scope: CanonicalAuthorityScope;
  semanticHash: string;
}

export function createArtifactEnvelope(
  scope: CanonicalAuthorityScope,
  providerState: Uint8Array,
  bindingKey: ArtifactBindingKey,
  createdAt = new Date().toISOString(),
): ArtifactEnvelopeResult {
  validateBindingKey(bindingKey);
  if (providerState.byteLength === 0) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  if (providerState.byteLength > MAX_PROVIDER_STATE_BYTES) {
    throw new ArtifactEnvelopeError("artifact_too_large");
  }
  if (!isIsoDate(createdAt)) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }

  const document = new Y.Doc();
  try {
    Y.applyUpdate(document, providerState);
    const authority = new CanonicalExcalidrawAuthority(
      document,
      scope,
      "artifact-worker",
    );
    const scene = authority.getScene();
    const portableBytes = exportPortableScene(scene, createdAt);
    const portableScene = parseJson(portableBytes);
    const causalWatermark = Y.encodeStateVector(document);
    const unsigned: UnsignedArtifactEnvelope = {
      authority: { name: "yjs", version: "13.6.27" },
      causalWatermark: Buffer.from(causalWatermark).toString("base64"),
      causalWatermarkSha256: sha256(causalWatermark),
      createdAt,
      engine: { name: "excalidraw", version: "0.18.1" },
      format: ARTIFACT_FORMAT,
      formatVersion: ARTIFACT_FORMAT_VERSION,
      portableScene,
      providerState: Buffer.from(providerState).toString("base64"),
      providerStateSha256: sha256(providerState),
      provenance: {
        kind: "service",
        name: "tutorhub-whiteboard-artifact-worker",
      },
      schemaVersion: 1,
      scope,
      semanticHash: semanticHash(scene),
    };
    const signature = hmac(unsigned, bindingKey.secret);
    const bytes = new TextEncoder().encode(
      stableStringify({
        ...unsigned,
        scopeBinding: {
          hmacSha256: signature,
          verificationKeyId: bindingKey.id,
        },
      }),
    );
    if (bytes.byteLength > MAX_ARTIFACT_BYTES) {
      throw new ArtifactEnvelopeError("artifact_too_large");
    }
    return {
      bytes,
      causalWatermarkSha256: unsigned.causalWatermarkSha256,
      contentSha256: sha256(bytes),
      semanticHash: unsigned.semanticHash,
    };
  } catch (error) {
    if (error instanceof ArtifactEnvelopeError) throw error;
    throw new ArtifactEnvelopeError("artifact_corrupt");
  } finally {
    document.destroy();
  }
}

export function verifyArtifactEnvelope(
  bytes: Uint8Array,
  expectedScope: CanonicalAuthorityScope,
  keys: ReadonlyMap<string, string>,
): VerifiedArtifactEnvelope {
  if (bytes.byteLength === 0) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  if (bytes.byteLength > MAX_ARTIFACT_BYTES) {
    throw new ArtifactEnvelopeError("artifact_too_large");
  }
  const candidate = parseJson(bytes);
  validateJsonDepth(candidate);
  if (!isRecord(candidate) || !isRecord(candidate.scopeBinding)) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  const verificationKeyId = candidate.scopeBinding.verificationKeyId;
  const hmacSha256 = candidate.scopeBinding.hmacSha256;
  if (
    typeof verificationKeyId !== "string" ||
    typeof hmacSha256 !== "string" ||
    !/^[a-f0-9]{64}$/.test(hmacSha256)
  ) {
    throw new ArtifactEnvelopeError("artifact_binding_invalid");
  }
  const secret = keys.get(verificationKeyId);
  if (!secret) {
    throw new ArtifactEnvelopeError("artifact_binding_invalid");
  }
  const unsignedCandidate = { ...candidate };
  delete unsignedCandidate.scopeBinding;
  const actualBinding = hmac(
    unsignedCandidate as unknown as UnsignedArtifactEnvelope,
    secret,
  );
  if (!hashesEqual(actualBinding, hmacSha256)) {
    throw new ArtifactEnvelopeError("artifact_binding_invalid");
  }
  const unsigned = validateUnsigned(unsignedCandidate, expectedScope);
  const providerState = decodeBase64(unsigned.providerState);
  const watermark = decodeBase64(unsigned.causalWatermark);
  if (providerState.byteLength > MAX_PROVIDER_STATE_BYTES) {
    throw new ArtifactEnvelopeError("artifact_too_large");
  }
  if (
    sha256(providerState) !== unsigned.providerStateSha256 ||
    sha256(watermark) !== unsigned.causalWatermarkSha256
  ) {
    throw new ArtifactEnvelopeError("artifact_checksum_mismatch");
  }

  let scene: CanonicalExcalidrawSceneV1;
  try {
    const portableBytes = new TextEncoder().encode(
      JSON.stringify(unsigned.portableScene),
    );
    scene = importPortableScene(portableBytes);
  } catch (error) {
    const code =
      error instanceof Error &&
      error.message === "portable_active_content_denied"
        ? "artifact_active_content_denied"
        : "artifact_corrupt";
    throw new ArtifactEnvelopeError(code);
  }

  const document = new Y.Doc();
  try {
    Y.applyUpdate(document, providerState);
    const authority = new CanonicalExcalidrawAuthority(
      document,
      expectedScope,
      "artifact-verifier",
    );
    const providerScene = authority.getScene();
    const providerWatermark = Y.encodeStateVector(document);
    if (
      semanticHash(scene) !== unsigned.semanticHash ||
      semanticHash(providerScene) !== unsigned.semanticHash ||
      sha256(providerWatermark) !== unsigned.causalWatermarkSha256
    ) {
      throw new ArtifactEnvelopeError("artifact_checksum_mismatch");
    }
    return { providerState, scene, semanticHash: unsigned.semanticHash };
  } catch (error) {
    if (error instanceof ArtifactEnvelopeError) throw error;
    throw new ArtifactEnvelopeError("artifact_corrupt");
  } finally {
    document.destroy();
  }
}

function validateUnsigned(
  candidate: Record<string, unknown>,
  expectedScope: CanonicalAuthorityScope,
): UnsignedArtifactEnvelope {
  if (
    candidate.format !== ARTIFACT_FORMAT ||
    candidate.formatVersion !== ARTIFACT_FORMAT_VERSION ||
    candidate.schemaVersion !== 1
  ) {
    throw new ArtifactEnvelopeError("artifact_version_unsupported");
  }
  if (
    !isRecord(candidate.engine) ||
    candidate.engine.name !== "excalidraw" ||
    candidate.engine.version !== "0.18.1" ||
    !isRecord(candidate.authority) ||
    candidate.authority.name !== "yjs" ||
    candidate.authority.version !== "13.6.27" ||
    !isRecord(candidate.provenance) ||
    candidate.provenance.kind !== "service" ||
    candidate.provenance.name !== "tutorhub-whiteboard-artifact-worker"
  ) {
    throw new ArtifactEnvelopeError("artifact_version_unsupported");
  }
  if (!isRecord(candidate.scope)) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  if (
    candidate.scope.tenantId !== expectedScope.tenantId ||
    candidate.scope.documentId !== expectedScope.documentId ||
    candidate.scope.generation !== expectedScope.generation
  ) {
    throw new ArtifactEnvelopeError("artifact_scope_mismatch");
  }
  for (const name of [
    "causalWatermark",
    "causalWatermarkSha256",
    "createdAt",
    "providerState",
    "providerStateSha256",
    "semanticHash",
  ]) {
    if (typeof candidate[name] !== "string") {
      throw new ArtifactEnvelopeError("artifact_corrupt");
    }
  }
  if (
    !isIsoDate(candidate.createdAt as string) ||
    !/^[a-f0-9]{64}$/.test(candidate.causalWatermarkSha256 as string) ||
    !/^[a-f0-9]{64}$/.test(candidate.providerStateSha256 as string)
  ) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  return candidate as unknown as UnsignedArtifactEnvelope;
}

function validateBindingKey(key: ArtifactBindingKey): void {
  if (
    !/^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/.test(key.id) ||
    key.secret.length < 32
  ) {
    throw new ArtifactEnvelopeError("artifact_binding_invalid");
  }
}

function parseJson(bytes: Uint8Array): unknown {
  try {
    return JSON.parse(
      new TextDecoder("utf-8", { fatal: true }).decode(bytes),
    ) as unknown;
  } catch {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
}

function decodeBase64(value: string): Uint8Array {
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(value)) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  const bytes = Buffer.from(value, "base64");
  if (bytes.toString("base64") !== value) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  return new Uint8Array(bytes);
}

function hmac(value: UnsignedArtifactEnvelope, secret: string): string {
  return createHmac("sha256", secret)
    .update(stableStringify(value))
    .digest("hex");
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

function hashesEqual(left: string, right: string): boolean {
  if (!/^[a-f0-9]{64}$/.test(left) || !/^[a-f0-9]{64}$/.test(right)) {
    return false;
  }
  return timingSafeEqual(Buffer.from(left, "hex"), Buffer.from(right, "hex"));
}

function validateJsonDepth(value: unknown, depth = 0): void {
  if (depth > MAX_JSON_DEPTH) {
    throw new ArtifactEnvelopeError("artifact_corrupt");
  }
  if (Array.isArray(value)) {
    for (const item of value) validateJsonDepth(item, depth + 1);
  } else if (isRecord(value)) {
    for (const [key, item] of Object.entries(value)) {
      if (key === "__proto__" || key === "constructor" || key === "prototype") {
        throw new ArtifactEnvelopeError("artifact_active_content_denied");
      }
      validateJsonDepth(item, depth + 1);
    }
  }
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  const record = value as Record<string, unknown>;
  return `{${Object.keys(record)
    .sort()
    .map((key) => `${JSON.stringify(key)}:${stableStringify(record[key])}`)
    .join(",")}}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isIsoDate(value: string): boolean {
  const timestamp = Date.parse(value);
  return (
    Number.isFinite(timestamp) && new Date(timestamp).toISOString() === value
  );
}
