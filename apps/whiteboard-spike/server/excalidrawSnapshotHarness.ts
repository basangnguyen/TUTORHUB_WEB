import { createHash, createHmac, randomBytes, randomUUID } from "node:crypto";
import { mkdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import { basename, join } from "node:path";
import * as Y from "yjs";
import {
  CANONICAL_EXCALIDRAW_LIMITS,
  CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  CanonicalExcalidrawAuthority,
  semanticHash,
  type CanonicalAuthorityScope,
  type CanonicalExcalidrawSceneV1,
} from "../src/excalidraw/canonicalAuthority";
import {
  EXCALIDRAW_ENGINE_VERSION,
  exportPortableScene,
  importPortableScene,
} from "../src/excalidraw/portableScene";
import { CollaborationControlPlane } from "./excalidrawAuthorizationHarness";

export const EXCALIDRAW_SNAPSHOT_FORMAT =
  "tutorhub.excalidraw.immutable-snapshot" as const;
export const EXCALIDRAW_SNAPSHOT_FORMAT_VERSION = 2 as const;
export const EXCALIDRAW_PROVIDER = {
  model: "yjs",
  version: "13.6.27",
} as const;
export const EXCALIDRAW_SNAPSHOT_CREATOR =
  "whiteboard-snapshot-worker" as const;

export const EXCALIDRAW_SNAPSHOT_LIMITS = {
  maxArtifactBytes: 32 * 1024 * 1024,
  maxProviderStateBytes: 20 * 1024 * 1024,
} as const;

export type SnapshotErrorCode =
  | "snapshot_binding_invalid"
  | "snapshot_catalog_corrupt"
  | "snapshot_generation_conflict"
  | "snapshot_not_found"
  | "snapshot_quarantined"
  | "snapshot_restore_in_progress"
  | "snapshot_too_large"
  | "snapshot_write_interrupted";

type SnapshotQuarantineReason =
  | "artifact_corrupt"
  | "artifact_missing"
  | "binding_mismatch"
  | "checksum_mismatch"
  | "format_unsupported"
  | "metadata_mismatch"
  | "provider_state_corrupt"
  | "size_limit";

export class SnapshotError extends Error {
  constructor(readonly code: SnapshotErrorCode) {
    super(code);
    this.name = "SnapshotError";
  }
}

export interface SnapshotCatalogEntry {
  byteLength: number;
  causalWatermark: string;
  checksum: string;
  createdAt: string;
  createdBy: typeof EXCALIDRAW_SNAPSHOT_CREATOR;
  documentId: string;
  elementCount: number;
  fileCount: number;
  generation: number;
  objectKey: string;
  quarantineObjectKey?: string;
  quarantineReason?: SnapshotQuarantineReason;
  semanticHash: string;
  snapshotId: string;
  scopeBindingKeyId: string;
  status: "published" | "quarantined";
  tenantId: string;
}

interface SnapshotArtifactV2 {
  canonicalSchemaVersion: typeof CANONICAL_EXCALIDRAW_SCHEMA_VERSION;
  causalWatermark: string;
  createdAt: string;
  createdBy: typeof EXCALIDRAW_SNAPSHOT_CREATOR;
  elementCount: number;
  engine: {
    name: "excalidraw";
    version: typeof EXCALIDRAW_ENGINE_VERSION;
  };
  fileCount: number;
  format: typeof EXCALIDRAW_SNAPSHOT_FORMAT;
  formatVersion: typeof EXCALIDRAW_SNAPSHOT_FORMAT_VERSION;
  portableScene: string;
  provider: typeof EXCALIDRAW_PROVIDER;
  providerState: string;
  scopeBinding: string;
  scopeBindingKeyId: string;
  semanticHash: string;
}

interface SnapshotCatalogV2 {
  entries: SnapshotCatalogEntry[];
  format: "tutorhub.excalidraw.snapshot-catalog";
  version: 2;
}

export interface CreateSnapshotOptions {
  faultAfterArtifactWrite?: boolean;
}

export interface RestoredAuthority {
  authority: CanonicalExcalidrawAuthority;
  document: Y.Doc;
  generation: number;
  previousGeneration: number;
  semanticHash: string;
}

export class DurableSnapshotStore {
  private readonly catalogPath: string;
  private readonly entries = new Map<string, SnapshotCatalogEntry>();
  private readonly scopeBindingKeys = new Map<string, Uint8Array>();

  private constructor(
    private readonly rootDirectory: string,
    private readonly activeScopeBindingKeyId: string,
    scopeBindingKey: Uint8Array,
    historicalScopeBindingKeys: Readonly<Record<string, Uint8Array>>,
    private readonly now: () => Date,
  ) {
    if (!isKeyId(activeScopeBindingKeyId) || scopeBindingKey.byteLength < 32) {
      throw new SnapshotError("snapshot_binding_invalid");
    }
    this.scopeBindingKeys.set(activeScopeBindingKeyId, scopeBindingKey.slice());
    for (const [keyId, key] of Object.entries(historicalScopeBindingKeys)) {
      if (
        !isKeyId(keyId) ||
        key.byteLength < 32 ||
        keyId === activeScopeBindingKeyId
      ) {
        throw new SnapshotError("snapshot_binding_invalid");
      }
      this.scopeBindingKeys.set(keyId, key.slice());
    }
    this.catalogPath = join(rootDirectory, "catalog.json");
  }

  static async open({
    activeScopeBindingKeyId = "snapshot-key-v1",
    historicalScopeBindingKeys = {},
    now = () => new Date(),
    rootDirectory,
    scopeBindingKey,
  }: {
    activeScopeBindingKeyId?: string;
    historicalScopeBindingKeys?: Readonly<Record<string, Uint8Array>>;
    now?: () => Date;
    rootDirectory: string;
    scopeBindingKey: Uint8Array;
  }): Promise<DurableSnapshotStore> {
    const store = new DurableSnapshotStore(
      rootDirectory,
      activeScopeBindingKeyId,
      scopeBindingKey,
      historicalScopeBindingKeys,
      now,
    );
    await mkdir(join(rootDirectory, "snapshots"), { recursive: true });
    await mkdir(join(rootDirectory, "quarantine"), { recursive: true });
    await store.loadCatalog();
    return store;
  }

  listSnapshots(
    scope?: Partial<CanonicalAuthorityScope>,
  ): SnapshotCatalogEntry[] {
    return [...this.entries.values()]
      .filter(
        (entry) =>
          (scope?.tenantId === undefined ||
            entry.tenantId === scope.tenantId) &&
          (scope?.documentId === undefined ||
            entry.documentId === scope.documentId) &&
          (scope?.generation === undefined ||
            entry.generation === scope.generation),
      )
      .map(cloneCatalogEntry)
      .sort((left, right) => left.createdAt.localeCompare(right.createdAt));
  }

  async createSnapshot(
    authority: CanonicalExcalidrawAuthority,
    options: CreateSnapshotOptions = {},
  ): Promise<SnapshotCatalogEntry> {
    const scene = authority.getScene();
    const semanticSceneHash = authority.getSemanticHash();
    const providerState = authority.encodeProviderState();
    if (
      providerState.byteLength >
      EXCALIDRAW_SNAPSHOT_LIMITS.maxProviderStateBytes
    ) {
      throw new SnapshotError("snapshot_too_large");
    }
    const createdAt = this.now().toISOString();
    const portableScene = exportPortableScene(scene, createdAt);
    const causalWatermark = encodeBase64Url(authority.encodeCausalWatermark());
    const artifact: SnapshotArtifactV2 = {
      canonicalSchemaVersion: CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
      causalWatermark,
      createdAt,
      createdBy: EXCALIDRAW_SNAPSHOT_CREATOR,
      elementCount: scene.elements.length,
      engine: { name: "excalidraw", version: EXCALIDRAW_ENGINE_VERSION },
      fileCount: Object.keys(scene.files).length,
      format: EXCALIDRAW_SNAPSHOT_FORMAT,
      formatVersion: EXCALIDRAW_SNAPSHOT_FORMAT_VERSION,
      portableScene: encodeBase64Url(portableScene),
      provider: EXCALIDRAW_PROVIDER,
      providerState: encodeBase64Url(providerState),
      scopeBinding: this.scopeBinding(
        authority.scope,
        this.activeScopeBindingKeyId,
      ),
      scopeBindingKeyId: this.activeScopeBindingKeyId,
      semanticHash: semanticSceneHash,
    };
    const artifactBytes = Buffer.from(JSON.stringify(artifact), "utf8");
    if (
      artifactBytes.byteLength > EXCALIDRAW_SNAPSHOT_LIMITS.maxArtifactBytes
    ) {
      throw new SnapshotError("snapshot_too_large");
    }

    const snapshotId = randomUUID();
    const objectKey = `snapshots/${randomBytes(24).toString("base64url")}.json`;
    const finalPath = join(this.rootDirectory, objectKey);
    const temporaryPath = `${finalPath}.${randomUUID()}.tmp`;
    await writeFile(temporaryPath, artifactBytes, { flag: "wx" });
    const verificationBytes = await readFile(temporaryPath);
    const checksum = sha256(verificationBytes);
    if (
      verificationBytes.byteLength !== artifactBytes.byteLength ||
      checksum !== sha256(artifactBytes)
    ) {
      throw new SnapshotError("snapshot_write_interrupted");
    }
    await rename(temporaryPath, finalPath);
    if (options.faultAfterArtifactWrite === true) {
      throw new SnapshotError("snapshot_write_interrupted");
    }

    const entry: SnapshotCatalogEntry = {
      byteLength: artifactBytes.byteLength,
      causalWatermark,
      checksum,
      createdAt,
      createdBy: EXCALIDRAW_SNAPSHOT_CREATOR,
      documentId: authority.scope.documentId,
      elementCount: scene.elements.length,
      fileCount: Object.keys(scene.files).length,
      generation: authority.scope.generation,
      objectKey,
      semanticHash: semanticSceneHash,
      snapshotId,
      scopeBindingKeyId: this.activeScopeBindingKeyId,
      status: "published",
      tenantId: authority.scope.tenantId,
    };
    this.entries.set(snapshotId, entry);
    try {
      await this.persistCatalog();
    } catch {
      this.entries.delete(snapshotId);
      throw new SnapshotError("snapshot_write_interrupted");
    }
    return cloneCatalogEntry(entry);
  }

  async recoverProviderAuthority(
    snapshotId: string,
    actorId: string,
  ): Promise<{ authority: CanonicalExcalidrawAuthority; document: Y.Doc }> {
    const { artifact, entry, portableScene } =
      await this.readVerifiedArtifact(snapshotId);
    const providerState = decodeBase64Url(artifact.providerState);
    if (
      providerState === undefined ||
      providerState.byteLength >
        EXCALIDRAW_SNAPSHOT_LIMITS.maxProviderStateBytes
    ) {
      await this.quarantine(entry, "provider_state_corrupt");
    }
    const document = new Y.Doc();
    let authority: CanonicalExcalidrawAuthority | undefined;
    try {
      Y.applyUpdate(document, providerState as Uint8Array);
      authority = new CanonicalExcalidrawAuthority(
        document,
        scopeFromEntry(entry),
        actorId,
      );
      if (
        !authority.isInitialized() ||
        authority.getSemanticHash() !== entry.semanticHash ||
        semanticHash(portableScene) !== entry.semanticHash
      ) {
        throw new Error("provider_state_mismatch");
      }
      return { authority, document };
    } catch {
      authority?.destroy();
      document.destroy();
      await this.quarantine(entry, "provider_state_corrupt");
      throw new SnapshotError("snapshot_quarantined");
    }
  }

  async readPortableScene(
    snapshotId: string,
    expectedScope: Pick<CanonicalAuthorityScope, "documentId" | "tenantId">,
  ): Promise<{
    entry: SnapshotCatalogEntry;
    scene: CanonicalExcalidrawSceneV1;
  }> {
    const { entry, portableScene } =
      await this.readVerifiedArtifact(snapshotId);
    if (
      entry.tenantId !== expectedScope.tenantId ||
      entry.documentId !== expectedScope.documentId
    ) {
      throw new SnapshotError("snapshot_not_found");
    }
    return { entry: cloneCatalogEntry(entry), scene: portableScene };
  }

  private async readVerifiedArtifact(snapshotId: string): Promise<{
    artifact: SnapshotArtifactV2;
    entry: SnapshotCatalogEntry;
    portableScene: CanonicalExcalidrawSceneV1;
  }> {
    const entry = this.entries.get(snapshotId);
    if (entry === undefined) {
      throw new SnapshotError("snapshot_not_found");
    }
    if (entry.status === "quarantined") {
      throw new SnapshotError("snapshot_quarantined");
    }
    let bytes: Buffer;
    try {
      bytes = await readFile(join(this.rootDirectory, entry.objectKey));
    } catch {
      return this.quarantine(entry, "artifact_missing");
    }
    if (bytes.byteLength > EXCALIDRAW_SNAPSHOT_LIMITS.maxArtifactBytes) {
      await this.quarantine(entry, "size_limit");
    }
    if (
      bytes.byteLength !== entry.byteLength ||
      sha256(bytes) !== entry.checksum
    ) {
      await this.quarantine(entry, "checksum_mismatch");
    }
    let candidate: unknown;
    try {
      candidate = JSON.parse(bytes.toString("utf8"));
    } catch {
      await this.quarantine(entry, "artifact_corrupt");
      throw new SnapshotError("snapshot_quarantined");
    }
    if (!isSnapshotArtifact(candidate)) {
      await this.quarantine(entry, "format_unsupported");
    }
    const artifact = candidate as SnapshotArtifactV2;
    if (artifact.scopeBindingKeyId !== entry.scopeBindingKeyId) {
      await this.quarantine(entry, "metadata_mismatch");
    }
    if (!this.scopeBindingKeys.has(artifact.scopeBindingKeyId)) {
      throw new SnapshotError("snapshot_binding_invalid");
    }
    if (
      artifact.scopeBinding !==
      this.scopeBinding(scopeFromEntry(entry), artifact.scopeBindingKeyId)
    ) {
      await this.quarantine(entry, "binding_mismatch");
    }
    if (
      artifact.createdAt !== entry.createdAt ||
      artifact.createdBy !== entry.createdBy ||
      artifact.causalWatermark !== entry.causalWatermark ||
      artifact.elementCount !== entry.elementCount ||
      artifact.fileCount !== entry.fileCount ||
      artifact.semanticHash !== entry.semanticHash
    ) {
      await this.quarantine(entry, "metadata_mismatch");
    }
    const portableBytes = decodeBase64Url(artifact.portableScene);
    if (portableBytes === undefined) {
      await this.quarantine(entry, "artifact_corrupt");
    }
    let portableScene: CanonicalExcalidrawSceneV1 | undefined;
    try {
      portableScene = importPortableScene(portableBytes as Uint8Array);
    } catch {
      await this.quarantine(entry, "artifact_corrupt");
      throw new SnapshotError("snapshot_quarantined");
    }
    if (
      portableScene.elements.length !== entry.elementCount ||
      Object.keys(portableScene.files).length !== entry.fileCount ||
      semanticHash(portableScene) !== entry.semanticHash
    ) {
      await this.quarantine(entry, "metadata_mismatch");
    }
    return {
      artifact,
      entry,
      portableScene,
    };
  }

  private async quarantine(
    entry: SnapshotCatalogEntry,
    reason: SnapshotQuarantineReason,
  ): Promise<never> {
    const sourcePath = join(this.rootDirectory, entry.objectKey);
    const quarantineObjectKey = `quarantine/${basename(entry.objectKey)}.${randomUUID()}.bad`;
    try {
      await stat(sourcePath);
      await rename(sourcePath, join(this.rootDirectory, quarantineObjectKey));
      entry.quarantineObjectKey = quarantineObjectKey;
    } catch {
      // Missing/unreadable artifacts are still quarantined in metadata.
    }
    entry.quarantineReason = reason;
    entry.status = "quarantined";
    await this.persistCatalog();
    throw new SnapshotError("snapshot_quarantined");
  }

  private async loadCatalog(): Promise<void> {
    let bytes: Buffer;
    try {
      bytes = await readFile(this.catalogPath);
    } catch (error) {
      if (isNotFoundError(error)) return;
      throw new SnapshotError("snapshot_catalog_corrupt");
    }
    let candidate: unknown;
    try {
      candidate = JSON.parse(bytes.toString("utf8"));
    } catch {
      throw new SnapshotError("snapshot_catalog_corrupt");
    }
    if (!isSnapshotCatalog(candidate)) {
      throw new SnapshotError("snapshot_catalog_corrupt");
    }
    for (const entry of candidate.entries) {
      if (this.entries.has(entry.snapshotId)) {
        throw new SnapshotError("snapshot_catalog_corrupt");
      }
      this.entries.set(entry.snapshotId, entry);
    }
  }

  private async persistCatalog(): Promise<void> {
    const catalog: SnapshotCatalogV2 = {
      entries: [...this.entries.values()],
      format: "tutorhub.excalidraw.snapshot-catalog",
      version: 2,
    };
    const temporaryPath = `${this.catalogPath}.${randomUUID()}.tmp`;
    await writeFile(temporaryPath, JSON.stringify(catalog), { flag: "wx" });
    await rename(temporaryPath, this.catalogPath);
  }

  private scopeBinding(scope: CanonicalAuthorityScope, keyId: string): string {
    const key = this.scopeBindingKeys.get(keyId);
    if (key === undefined) {
      throw new SnapshotError("snapshot_binding_invalid");
    }
    return createHmac("sha256", key)
      .update(
        `${scope.tenantId.length}:${scope.tenantId}:${scope.documentId.length}:${scope.documentId}:${scope.generation}`,
      )
      .digest("base64url");
  }
}

export class SnapshotRestoreCoordinator {
  private readonly restoring = new Set<string>();

  constructor(
    private readonly store: DurableSnapshotStore,
    private readonly controlPlane: CollaborationControlPlane,
  ) {}

  async restore({
    actorId,
    documentId,
    expectedGeneration,
    snapshotId,
    tenantId,
  }: {
    actorId: string;
    documentId: string;
    expectedGeneration: number;
    snapshotId: string;
    tenantId: string;
  }): Promise<RestoredAuthority> {
    const restoreKey = `${tenantId.length}:${tenantId}:${documentId}`;
    if (this.restoring.has(restoreKey)) {
      throw new SnapshotError("snapshot_restore_in_progress");
    }
    this.restoring.add(restoreKey);
    try {
      const { entry, scene } = await this.store.readPortableScene(snapshotId, {
        documentId,
        tenantId,
      });
      const previousGeneration = this.controlPlane.currentGeneration(
        tenantId,
        documentId,
      );
      if (previousGeneration !== expectedGeneration) {
        throw new SnapshotError("snapshot_generation_conflict");
      }
      if (entry.generation > previousGeneration) {
        throw new SnapshotError("snapshot_generation_conflict");
      }
      const generation = previousGeneration + 1;
      const document = new Y.Doc();
      const authority = new CanonicalExcalidrawAuthority(
        document,
        { documentId, generation, tenantId },
        actorId,
      );
      try {
        authority.initialize(scene);
        if (authority.getSemanticHash() !== entry.semanticHash) {
          throw new SnapshotError("snapshot_quarantined");
        }
        const transition = this.controlPlane.transitionDocument({
          action: "restore",
          documentId,
          tenantId,
        });
        if (transition.nextGeneration !== generation) {
          throw new SnapshotError("snapshot_generation_conflict");
        }
        return {
          authority,
          document,
          generation,
          previousGeneration,
          semanticHash: entry.semanticHash,
        };
      } catch (error) {
        authority.destroy();
        document.destroy();
        throw error;
      }
    } finally {
      this.restoring.delete(restoreKey);
    }
  }
}

function isSnapshotArtifact(value: unknown): value is SnapshotArtifactV2 {
  if (
    !isRecord(value) ||
    !isRecord(value.engine) ||
    !isRecord(value.provider)
  ) {
    return false;
  }
  return (
    value.format === EXCALIDRAW_SNAPSHOT_FORMAT &&
    value.formatVersion === EXCALIDRAW_SNAPSHOT_FORMAT_VERSION &&
    value.canonicalSchemaVersion === CANONICAL_EXCALIDRAW_SCHEMA_VERSION &&
    value.engine.name === "excalidraw" &&
    value.engine.version === EXCALIDRAW_ENGINE_VERSION &&
    value.provider.model === EXCALIDRAW_PROVIDER.model &&
    value.provider.version === EXCALIDRAW_PROVIDER.version &&
    value.createdBy === EXCALIDRAW_SNAPSHOT_CREATOR &&
    typeof value.createdAt === "string" &&
    isIsoDate(value.createdAt) &&
    typeof value.causalWatermark === "string" &&
    decodeBase64Url(value.causalWatermark) !== undefined &&
    isBoundedCount(
      value.elementCount,
      CANONICAL_EXCALIDRAW_LIMITS.maxElements,
    ) &&
    isBoundedCount(value.fileCount, CANONICAL_EXCALIDRAW_LIMITS.maxFiles) &&
    typeof value.portableScene === "string" &&
    decodeBase64Url(value.portableScene) !== undefined &&
    typeof value.providerState === "string" &&
    decodeBase64Url(value.providerState) !== undefined &&
    typeof value.scopeBinding === "string" &&
    /^[A-Za-z0-9_-]{43}$/.test(value.scopeBinding) &&
    typeof value.scopeBindingKeyId === "string" &&
    isKeyId(value.scopeBindingKeyId) &&
    typeof value.semanticHash === "string" &&
    /^fnv1a64:[0-9a-f]{16}$/.test(value.semanticHash)
  );
}

function isSnapshotCatalog(value: unknown): value is SnapshotCatalogV2 {
  return (
    isRecord(value) &&
    value.format === "tutorhub.excalidraw.snapshot-catalog" &&
    value.version === 2 &&
    Array.isArray(value.entries) &&
    value.entries.every(isSnapshotCatalogEntry)
  );
}

function isSnapshotCatalogEntry(value: unknown): value is SnapshotCatalogEntry {
  return (
    isRecord(value) &&
    Number.isSafeInteger(value.byteLength) &&
    typeof value.byteLength === "number" &&
    value.byteLength > 0 &&
    value.byteLength <= EXCALIDRAW_SNAPSHOT_LIMITS.maxArtifactBytes &&
    typeof value.causalWatermark === "string" &&
    decodeBase64Url(value.causalWatermark) !== undefined &&
    typeof value.checksum === "string" &&
    /^sha256:[0-9a-f]{64}$/.test(value.checksum) &&
    typeof value.createdAt === "string" &&
    isIsoDate(value.createdAt) &&
    value.createdBy === EXCALIDRAW_SNAPSHOT_CREATOR &&
    typeof value.documentId === "string" &&
    isIdentifier(value.documentId) &&
    isBoundedCount(
      value.elementCount,
      CANONICAL_EXCALIDRAW_LIMITS.maxElements,
    ) &&
    isBoundedCount(value.fileCount, CANONICAL_EXCALIDRAW_LIMITS.maxFiles) &&
    Number.isSafeInteger(value.generation) &&
    typeof value.generation === "number" &&
    value.generation >= 1 &&
    typeof value.objectKey === "string" &&
    /^snapshots\/[A-Za-z0-9_-]{32}\.json$/.test(value.objectKey) &&
    typeof value.semanticHash === "string" &&
    /^fnv1a64:[0-9a-f]{16}$/.test(value.semanticHash) &&
    typeof value.snapshotId === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
      value.snapshotId,
    ) &&
    typeof value.scopeBindingKeyId === "string" &&
    isKeyId(value.scopeBindingKeyId) &&
    (value.status === "published" || value.status === "quarantined") &&
    typeof value.tenantId === "string" &&
    isIdentifier(value.tenantId) &&
    (value.quarantineObjectKey === undefined ||
      (typeof value.quarantineObjectKey === "string" &&
        /^quarantine\/[A-Za-z0-9_.-]+\.bad$/.test(
          value.quarantineObjectKey,
        ))) &&
    (value.quarantineReason === undefined ||
      isQuarantineReason(value.quarantineReason)) &&
    (value.status !== "quarantined" || value.quarantineReason !== undefined)
  );
}

function isKeyId(value: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9._:-]{2,63}$/.test(value);
}

function scopeFromEntry(entry: SnapshotCatalogEntry): CanonicalAuthorityScope {
  return {
    documentId: entry.documentId,
    generation: entry.generation,
    tenantId: entry.tenantId,
  };
}

function cloneCatalogEntry(entry: SnapshotCatalogEntry): SnapshotCatalogEntry {
  return { ...entry };
}

function encodeBase64Url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64url");
}

function decodeBase64Url(value: string): Uint8Array | undefined {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) return undefined;
  const decoded = Buffer.from(value, "base64url");
  return decoded.toString("base64url") === value
    ? new Uint8Array(decoded)
    : undefined;
}

function sha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isBoundedCount(value: unknown, maximum: number): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= 0 &&
    value <= maximum
  );
}

function isIdentifier(value: string): boolean {
  return /^[A-Za-z0-9._:-]{1,128}$/.test(value);
}

function isIsoDate(value: string): boolean {
  const timestamp = Date.parse(value);
  return (
    Number.isFinite(timestamp) && new Date(timestamp).toISOString() === value
  );
}

function isQuarantineReason(value: unknown): value is SnapshotQuarantineReason {
  return (
    value === "artifact_corrupt" ||
    value === "artifact_missing" ||
    value === "binding_mismatch" ||
    value === "checksum_mismatch" ||
    value === "format_unsupported" ||
    value === "metadata_mismatch" ||
    value === "provider_state_corrupt" ||
    value === "size_limit"
  );
}

function isNotFoundError(error: unknown): boolean {
  return (
    isRecord(error) &&
    "code" in error &&
    (error as { code?: unknown }).code === "ENOENT"
  );
}
