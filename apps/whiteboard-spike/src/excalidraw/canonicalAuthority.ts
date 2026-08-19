import * as Y from "yjs";

export const CANONICAL_EXCALIDRAW_SCHEMA_VERSION = 1 as const;

export const CANONICAL_EXCALIDRAW_LIMITS = {
  maxDepth: 20,
  maxDocumentBytes: 16 * 1024 * 1024,
  maxElements: 2_000,
  maxFiles: 256,
  maxIdentifierLength: 128,
  maxStringLength: 12 * 1024 * 1024,
  maxUpdateBytes: 4 * 1024 * 1024,
} as const;

const ROOT_METADATA = "tutorhub.excalidraw.metadata.v1";
const ROOT_PAGE = "tutorhub.excalidraw.page.v1";
const ROOT_ELEMENTS = "tutorhub.excalidraw.elements.v1";
const ROOT_ELEMENT_ORDER = "tutorhub.excalidraw.element-order.v1";
const ROOT_FILES = "tutorhub.excalidraw.files.v1";
const BOOTSTRAP_ACTOR_ID = "bootstrap";
const BOOTSTRAP_ORIGIN = Object.freeze({ kind: "canonical-bootstrap" });
const REMOTE_UPDATE_ORIGIN = Object.freeze({ kind: "canonical-remote-update" });

const SUPPORTED_ELEMENT_TYPES = new Set([
  "arrow",
  "diamond",
  "ellipse",
  "frame",
  "freedraw",
  "image",
  "line",
  "rectangle",
  "text",
]);

export type CanonicalSceneErrorCode =
  | "authority_already_initialized"
  | "authority_not_initialized"
  | "authority_scope_invalid"
  | "authority_scope_mismatch"
  | "scene_corrupt"
  | "scene_duplicate_element"
  | "scene_element_limit"
  | "scene_element_unsupported"
  | "scene_file_limit"
  | "scene_invalid"
  | "scene_schema_unsupported"
  | "scene_storage_corrupt"
  | "scene_too_deep"
  | "scene_too_large"
  | "update_corrupt"
  | "update_too_large";

export class CanonicalSceneError extends Error {
  constructor(readonly code: CanonicalSceneErrorCode) {
    super(code);
    this.name = "CanonicalSceneError";
  }
}

export type JsonPrimitive = boolean | null | number | string;
export type JsonValue = JsonPrimitive | JsonValue[] | JsonObject;
export interface JsonObject {
  [key: string]: JsonValue;
}

export interface CanonicalAuthorityScope {
  tenantId: string;
  documentId: string;
  generation: number;
}

export interface CanonicalPageV1 extends JsonObject {
  backgroundColor: string;
  id: string;
  name: string;
}

export interface CanonicalElementV1 extends JsonObject {
  height: number;
  id: string;
  type: string;
  width: number;
  x: number;
  y: number;
}

export interface CanonicalExcalidrawSceneV1 extends JsonObject {
  elements: CanonicalElementV1[];
  files: Record<string, JsonObject>;
  page: CanonicalPageV1;
  schemaVersion: typeof CANONICAL_EXCALIDRAW_SCHEMA_VERSION;
}

interface CanonicalElementEnvelopeV1 extends JsonObject {
  actorId: string;
  elementId: string;
  revision: number;
  tombstone: boolean;
  value: CanonicalElementV1 | null;
}

export interface ExcalidrawSceneProjection {
  appState: {
    viewBackgroundColor: string;
  };
  elements: CanonicalElementV1[];
  files: Record<string, JsonObject>;
  page: {
    id: string;
    name: string;
  };
}

interface CanonicalRoots {
  elementOrder: Y.Array<string>;
  elements: Y.Map<string>;
  files: Y.Map<string>;
  metadata: Y.Map<number | string>;
  page: Y.Map<string>;
}

export class CanonicalExcalidrawAuthority {
  private readonly localOrigin: Readonly<{
    actorId: string;
    kind: "canonical-local";
  }>;

  private readonly roots: CanonicalRoots;
  private readonly undoManager: Y.UndoManager;

  constructor(
    private readonly document: Y.Doc,
    readonly scope: CanonicalAuthorityScope,
    private readonly actorId: string,
  ) {
    validateScope(scope);
    validateIdentifier(actorId, "authority_scope_invalid");
    this.localOrigin = Object.freeze({ actorId, kind: "canonical-local" });
    this.roots = getRoots(document);
    this.undoManager = new Y.UndoManager(
      [
        this.roots.metadata,
        this.roots.page,
        this.roots.elements,
        this.roots.elementOrder,
        this.roots.files,
      ],
      {
        captureTimeout: 0,
        trackedOrigins: new Set([this.localOrigin]),
      },
    );

    if (this.isInitialized()) {
      assertStoredScope(this.roots, this.scope);
    }
  }

  initialize(scene: unknown): void {
    if (this.isInitialized()) {
      throw new CanonicalSceneError("authority_already_initialized");
    }

    const canonical = validateCanonicalScene(scene);
    this.document.transact(() => {
      writeScope(this.roots, this.scope);
      initializeRoots(this.roots, canonical);
    }, BOOTSTRAP_ORIGIN);
    this.clearHistory();
  }

  isInitialized(): boolean {
    return this.roots.metadata.has("schemaVersion");
  }

  getScene(): CanonicalExcalidrawSceneV1 {
    if (!this.isInitialized()) {
      throw new CanonicalSceneError("authority_not_initialized");
    }
    return readScene(this.roots, this.scope);
  }

  getProjection(): ExcalidrawSceneProjection {
    return canonicalSceneToExcalidraw(this.getScene());
  }

  getSemanticHash(): string {
    return semanticHash(this.getScene());
  }

  encodeProviderState(): Uint8Array {
    this.assertInitializedScope();
    return Y.encodeStateAsUpdate(this.document);
  }

  encodeCausalWatermark(): Uint8Array {
    this.assertInitializedScope();
    return Y.encodeStateVector(this.document);
  }

  replaceScene(scene: unknown): void {
    const canonical = validateCanonicalScene(scene);
    this.assertInitializedScope();
    this.runLocalTransaction(() =>
      replaceSceneForActor(this.roots, canonical, this.actorId),
    );
  }

  replaceProjection(projection: unknown): void {
    this.replaceScene(excalidrawSceneToCanonical(projection));
  }

  applySceneDelta(baseline: unknown, nextScene: unknown): void {
    this.assertInitializedScope();
    const previous = validateCanonicalScene(baseline);
    const next = validateCanonicalScene(nextScene);
    if (
      previous.schemaVersion !== next.schemaVersion ||
      previous.page.id !== next.page.id
    ) {
      throw new CanonicalSceneError("scene_invalid");
    }
    const current = this.getScene();
    const previousElements = new Map(
      previous.elements.map((element) => [element.id, element]),
    );
    const nextElements = new Map(
      next.elements.map((element) => [element.id, element]),
    );

    this.runLocalTransaction(() => {
      for (const element of next.elements) {
        const previousElement = previousElements.get(element.id);
        if (
          previousElement === undefined ||
          stableStringify(previousElement) !== stableStringify(element)
        ) {
          writeElementEnvelope(this.roots, element.id, this.actorId, element);
        }
      }
      for (const element of previous.elements) {
        if (!nextElements.has(element.id)) {
          writeElementEnvelope(this.roots, element.id, this.actorId, null);
        }
      }

      const localOrderChanged =
        stableStringify(previous.elements.map((element) => element.id)) !==
        stableStringify(next.elements.map((element) => element.id));
      if (localOrderChanged) {
        const remoteOnlyIds = current.elements
          .map((element) => element.id)
          .filter(
            (elementId) =>
              !previousElements.has(elementId) && !nextElements.has(elementId),
          );
        const mergedOrder = [
          ...next.elements.map((element) => element.id),
          ...remoteOnlyIds,
        ];
        this.roots.elementOrder.delete(0, this.roots.elementOrder.length);
        this.roots.elementOrder.insert(0, mergedOrder);
      }

      if (stableStringify(previous.page) !== stableStringify(next.page)) {
        this.roots.page.set("state", stableStringify(next.page));
      }
      const previousFiles = previous.files;
      for (const [fileId, file] of Object.entries(next.files)) {
        if (
          !(fileId in previousFiles) ||
          stableStringify(previousFiles[fileId] as JsonValue) !==
            stableStringify(file)
        ) {
          this.roots.files.set(fileId, stableStringify(file));
        }
      }
      for (const fileId of Object.keys(previousFiles)) {
        if (!(fileId in next.files)) {
          this.roots.files.delete(fileId);
        }
      }
    });
  }

  putElement(element: unknown): void {
    this.assertInitializedScope();
    const current = this.getScene();
    const normalized = validateElement(element);
    const existingIndex = current.elements.findIndex(
      (candidate) => candidate.id === normalized.id,
    );
    const candidateElements = [...current.elements];
    if (existingIndex >= 0) {
      candidateElements[existingIndex] = normalized;
    } else {
      candidateElements.push(normalized);
    }
    validateCanonicalScene({ ...current, elements: candidateElements });

    this.runLocalTransaction(() => {
      writeElementEnvelope(this.roots, normalized.id, this.actorId, normalized);
      if (existingIndex < 0) {
        this.roots.elementOrder.push([normalized.id]);
      }
    });
  }

  deleteElement(elementId: string): void {
    this.assertInitializedScope();
    validateIdentifier(elementId, "scene_invalid");
    this.runLocalTransaction(() => {
      writeElementEnvelope(this.roots, elementId, this.actorId, null);
    });
  }

  applyRemoteUpdate(update: Uint8Array): void {
    if (update.byteLength > CANONICAL_EXCALIDRAW_LIMITS.maxUpdateBytes) {
      throw new CanonicalSceneError("update_too_large");
    }

    const probe = new Y.Doc();
    try {
      Y.applyUpdate(probe, Y.encodeStateAsUpdate(this.document));
      Y.applyUpdate(probe, update);
      readScene(getRoots(probe), this.scope);
      Y.applyUpdate(this.document, update, REMOTE_UPDATE_ORIGIN);
    } catch (error) {
      if (error instanceof CanonicalSceneError) {
        throw error;
      }
      throw new CanonicalSceneError("update_corrupt");
    } finally {
      probe.destroy();
    }
  }

  undo(): boolean {
    this.assertInitializedScope();
    const initialHash = this.getSemanticHash();
    const initialLocalSignature = this.getLocalActorSemanticSignature();
    while (this.undoManager.canUndo()) {
      this.undoManager.undo();
      if (this.getLocalActorSemanticSignature() !== initialLocalSignature) {
        return true;
      }
    }
    return this.getSemanticHash() !== initialHash;
  }

  redo(): boolean {
    this.assertInitializedScope();
    const initialHash = this.getSemanticHash();
    const initialLocalSignature = this.getLocalActorSemanticSignature();
    while (this.undoManager.canRedo()) {
      this.undoManager.redo();
      if (this.getLocalActorSemanticSignature() !== initialLocalSignature) {
        return true;
      }
    }
    return this.getSemanticHash() !== initialHash;
  }

  clearHistory(): void {
    this.undoManager.clear();
    this.undoManager.stopCapturing();
  }

  subscribe(listener: (scene: CanonicalExcalidrawSceneV1) => void): () => void {
    const handleUpdate = () => listener(this.getScene());
    this.document.on("update", handleUpdate);
    return () => this.document.off("update", handleUpdate);
  }

  destroy(): void {
    this.undoManager.destroy();
  }

  private assertInitializedScope(): void {
    if (!this.isInitialized()) {
      throw new CanonicalSceneError("authority_not_initialized");
    }
    assertStoredScope(this.roots, this.scope);
  }

  private runLocalTransaction(callback: () => void): void {
    this.undoManager.stopCapturing();
    this.document.transact(callback, this.localOrigin);
    this.undoManager.stopCapturing();
  }

  private getLocalActorSemanticSignature(): string {
    return stableStringify(
      readElementEnvelopes(this.roots)
        .filter((envelope) => envelope.actorId === this.actorId)
        .map(({ elementId, tombstone, value }) => ({
          elementId,
          tombstone,
          value,
        }))
        .sort((left, right) => left.elementId.localeCompare(right.elementId)),
    );
  }
}

export function excalidrawSceneToCanonical(
  projection: unknown,
): CanonicalExcalidrawSceneV1 {
  const normalized = toJsonValue(projection);
  if (!isJsonObject(normalized)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  const appState = normalized.appState;
  const page = normalized.page;
  if (!isJsonObject(appState) || !isJsonObject(page)) {
    throw new CanonicalSceneError("scene_invalid");
  }

  return validateCanonicalScene({
    elements: normalized.elements,
    files: normalized.files ?? {},
    page: {
      backgroundColor: appState.viewBackgroundColor,
      id: page.id,
      name: page.name,
    },
    schemaVersion: CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  });
}

export function canonicalSceneToExcalidraw(
  scene: unknown,
): ExcalidrawSceneProjection {
  const canonical = validateCanonicalScene(scene);
  return {
    appState: {
      viewBackgroundColor: canonical.page.backgroundColor,
    },
    elements: cloneJson(canonical.elements),
    files: cloneJson(canonical.files),
    page: {
      id: canonical.page.id,
      name: canonical.page.name,
    },
  };
}

export function semanticHash(scene: unknown): string {
  const canonical = validateCanonicalScene(scene);
  const input = stableStringify(canonical);
  let hash = 0xcbf29ce484222325n;
  const prime = 0x100000001b3n;
  const bytes = new TextEncoder().encode(input);
  for (const byte of bytes) {
    hash ^= BigInt(byte);
    hash = BigInt.asUintN(64, hash * prime);
  }
  return `fnv1a64:${hash.toString(16).padStart(16, "0")}`;
}

export function validateCanonicalScene(
  candidate: unknown,
): CanonicalExcalidrawSceneV1 {
  const normalized = toJsonValue(candidate);
  validateJsonBounds(normalized);
  if (!isJsonObject(normalized)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  if (normalized.schemaVersion !== CANONICAL_EXCALIDRAW_SCHEMA_VERSION) {
    throw new CanonicalSceneError("scene_schema_unsupported");
  }
  if (!Array.isArray(normalized.elements)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  if (normalized.elements.length > CANONICAL_EXCALIDRAW_LIMITS.maxElements) {
    throw new CanonicalSceneError("scene_element_limit");
  }

  const elements = normalized.elements.map(validateElement);
  const elementIds = new Set<string>();
  for (const element of elements) {
    if (elementIds.has(element.id)) {
      throw new CanonicalSceneError("scene_duplicate_element");
    }
    elementIds.add(element.id);
  }

  if (!isJsonObject(normalized.files)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  const fileEntries = Object.entries(normalized.files);
  if (fileEntries.length > CANONICAL_EXCALIDRAW_LIMITS.maxFiles) {
    throw new CanonicalSceneError("scene_file_limit");
  }
  const files: Record<string, JsonObject> = {};
  for (const [fileId, file] of fileEntries) {
    validateIdentifier(fileId, "scene_invalid");
    if (!isJsonObject(file)) {
      throw new CanonicalSceneError("scene_invalid");
    }
    files[fileId] = cloneJson(file);
  }

  if (!isJsonObject(normalized.page)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  const { backgroundColor, id, name } = normalized.page;
  validateIdentifier(id, "scene_invalid");
  if (
    typeof backgroundColor !== "string" ||
    backgroundColor.length === 0 ||
    backgroundColor.length > 64 ||
    typeof name !== "string" ||
    name.length === 0 ||
    name.length > 256
  ) {
    throw new CanonicalSceneError("scene_invalid");
  }

  const canonical: CanonicalExcalidrawSceneV1 = {
    elements,
    files,
    page: { backgroundColor, id, name },
    schemaVersion: CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  };
  const bytes = new TextEncoder().encode(stableStringify(canonical)).byteLength;
  if (bytes > CANONICAL_EXCALIDRAW_LIMITS.maxDocumentBytes) {
    throw new CanonicalSceneError("scene_too_large");
  }
  return canonical;
}

function validateElement(candidate: unknown): CanonicalElementV1 {
  const normalized = toJsonValue(candidate);
  if (!isJsonObject(normalized)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  const { height, id, type, width, x, y } = normalized;
  validateIdentifier(id, "scene_invalid");
  if (typeof type !== "string" || !SUPPORTED_ELEMENT_TYPES.has(type)) {
    throw new CanonicalSceneError("scene_element_unsupported");
  }
  for (const value of [x, y, width, height]) {
    if (typeof value !== "number" || !Number.isFinite(value)) {
      throw new CanonicalSceneError("scene_invalid");
    }
  }
  if (type === "text" && typeof normalized.text !== "string") {
    throw new CanonicalSceneError("scene_invalid");
  }
  if (
    type === "image" &&
    normalized.fileId !== null &&
    typeof normalized.fileId !== "string"
  ) {
    throw new CanonicalSceneError("scene_invalid");
  }
  if (
    (type === "line" || type === "arrow") &&
    !Array.isArray(normalized.points)
  ) {
    throw new CanonicalSceneError("scene_invalid");
  }
  return cloneJson(normalized) as CanonicalElementV1;
}

function getRoots(document: Y.Doc): CanonicalRoots {
  return {
    elementOrder: document.getArray<string>(ROOT_ELEMENT_ORDER),
    elements: document.getMap<string>(ROOT_ELEMENTS),
    files: document.getMap<string>(ROOT_FILES),
    metadata: document.getMap<number | string>(ROOT_METADATA),
    page: document.getMap<string>(ROOT_PAGE),
  };
}

function writeScope(
  roots: CanonicalRoots,
  scope: CanonicalAuthorityScope,
): void {
  roots.metadata.set("schemaVersion", CANONICAL_EXCALIDRAW_SCHEMA_VERSION);
  roots.metadata.set("tenantId", scope.tenantId);
  roots.metadata.set("documentId", scope.documentId);
  roots.metadata.set("generation", scope.generation);
}

function initializeRoots(
  roots: CanonicalRoots,
  scene: CanonicalExcalidrawSceneV1,
): void {
  roots.page.set("state", stableStringify(scene.page));
  syncStringMap(
    roots.elements,
    new Map(
      scene.elements.map((element) => [
        elementEnvelopeKey(BOOTSTRAP_ACTOR_ID, element.id),
        stableStringify(
          createElementEnvelope(BOOTSTRAP_ACTOR_ID, element.id, 0, element),
        ),
      ]),
    ),
  );
  roots.elementOrder.delete(0, roots.elementOrder.length);
  roots.elementOrder.insert(
    0,
    scene.elements.map((element) => element.id),
  );
  syncStringMap(
    roots.files,
    new Map(
      Object.entries(scene.files).map(([id, file]) => [
        id,
        stableStringify(file),
      ]),
    ),
  );
}

function replaceSceneForActor(
  roots: CanonicalRoots,
  scene: CanonicalExcalidrawSceneV1,
  actorId: string,
): void {
  const currentElements = resolveElementMap(roots);
  const desiredIds = new Set(scene.elements.map((element) => element.id));
  for (const element of scene.elements) {
    const current = currentElements.get(element.id);
    if (
      current === undefined ||
      stableStringify(current) !== stableStringify(element)
    ) {
      writeElementEnvelope(roots, element.id, actorId, element);
    }
  }
  for (const elementId of currentElements.keys()) {
    if (!desiredIds.has(elementId)) {
      writeElementEnvelope(roots, elementId, actorId, null);
    }
  }

  const desiredOrder = scene.elements.map((element) => element.id);
  const currentOrder = roots.elementOrder.toArray();
  if (stableStringify(currentOrder) !== stableStringify(desiredOrder)) {
    roots.elementOrder.delete(0, roots.elementOrder.length);
    roots.elementOrder.insert(0, desiredOrder);
  }
  roots.page.set("state", stableStringify(scene.page));
  syncStringMap(
    roots.files,
    new Map(
      Object.entries(scene.files).map(([id, file]) => [
        id,
        stableStringify(file),
      ]),
    ),
  );
}

function writeElementEnvelope(
  roots: CanonicalRoots,
  elementId: string,
  actorId: string,
  value: CanonicalElementV1 | null,
): void {
  const revisions = readElementEnvelopes(roots)
    .filter((envelope) => envelope.elementId === elementId)
    .map((envelope) => envelope.revision);
  const revision = revisions.length === 0 ? 1 : Math.max(...revisions) + 1;
  const envelope = createElementEnvelope(actorId, elementId, revision, value);
  roots.elements.set(
    elementEnvelopeKey(actorId, elementId),
    stableStringify(envelope),
  );
}

function createElementEnvelope(
  actorId: string,
  elementId: string,
  revision: number,
  value: CanonicalElementV1 | null,
): CanonicalElementEnvelopeV1 {
  return {
    actorId,
    elementId,
    revision,
    tombstone: value === null,
    value,
  };
}

function elementEnvelopeKey(actorId: string, elementId: string): string {
  return `${actorId.length}:${actorId}${elementId}`;
}

function readElementEnvelopes(
  roots: CanonicalRoots,
): CanonicalElementEnvelopeV1[] {
  const envelopes: CanonicalElementEnvelopeV1[] = [];
  for (const serialized of roots.elements.values()) {
    const candidate = parseStoredJson(serialized);
    if (!isJsonObject(candidate)) {
      throw new CanonicalSceneError("scene_storage_corrupt");
    }
    const { actorId, elementId, revision, tombstone, value } = candidate;
    validateIdentifier(actorId, "scene_storage_corrupt");
    validateIdentifier(elementId, "scene_storage_corrupt");
    if (
      !Number.isSafeInteger(revision) ||
      typeof revision !== "number" ||
      revision < 0 ||
      typeof tombstone !== "boolean" ||
      (tombstone && value !== null) ||
      (!tombstone && value === null)
    ) {
      throw new CanonicalSceneError("scene_storage_corrupt");
    }
    envelopes.push({
      actorId,
      elementId,
      revision,
      tombstone,
      value: value === null ? null : validateElement(value),
    });
  }
  return envelopes;
}

function resolveElementMap(
  roots: CanonicalRoots,
): Map<string, CanonicalElementV1> {
  const winners = new Map<string, CanonicalElementEnvelopeV1>();
  for (const envelope of readElementEnvelopes(roots)) {
    const current = winners.get(envelope.elementId);
    if (
      current === undefined ||
      envelope.revision > current.revision ||
      (envelope.revision === current.revision &&
        envelope.actorId.localeCompare(current.actorId) > 0)
    ) {
      winners.set(envelope.elementId, envelope);
    }
  }
  const elements = new Map<string, CanonicalElementV1>();
  for (const [elementId, envelope] of winners) {
    if (!envelope.tombstone && envelope.value !== null) {
      elements.set(elementId, envelope.value);
    }
  }
  return elements;
}

function syncStringMap(
  target: Y.Map<string>,
  values: Map<string, string>,
): void {
  for (const key of target.keys()) {
    if (!values.has(key)) {
      target.delete(key);
    }
  }
  for (const [key, value] of values) {
    target.set(key, value);
  }
}

function readScene(
  roots: CanonicalRoots,
  scope: CanonicalAuthorityScope,
): CanonicalExcalidrawSceneV1 {
  assertStoredScope(roots, scope);
  const page = parseStoredJson(roots.page.get("state"));
  const resolvedElements = resolveElementMap(roots);
  const order: string[] = [];
  const orderSet = new Set<string>();
  for (const id of roots.elementOrder.toArray()) {
    if (!orderSet.has(id) && resolvedElements.has(id)) {
      order.push(id);
      orderSet.add(id);
    }
  }
  const extraElementIds = [...resolvedElements.keys()]
    .filter((id) => !orderSet.has(id))
    .sort();
  const elements = [...order, ...extraElementIds].map((id) => {
    return resolvedElements.get(id) as CanonicalElementV1;
  });
  const files: Record<string, JsonValue> = {};
  for (const id of [...roots.files.keys()].sort()) {
    files[id] = parseStoredJson(roots.files.get(id));
  }
  try {
    return validateCanonicalScene({
      elements,
      files,
      page,
      schemaVersion: roots.metadata.get("schemaVersion"),
    });
  } catch (error) {
    if (error instanceof CanonicalSceneError) {
      throw error;
    }
    throw new CanonicalSceneError("scene_storage_corrupt");
  }
}

function parseStoredJson(serialized: string | undefined): JsonValue {
  if (serialized === undefined) {
    throw new CanonicalSceneError("scene_storage_corrupt");
  }
  try {
    return JSON.parse(serialized) as JsonValue;
  } catch {
    throw new CanonicalSceneError("scene_storage_corrupt");
  }
}

function assertStoredScope(
  roots: CanonicalRoots,
  expected: CanonicalAuthorityScope,
): void {
  if (
    roots.metadata.get("schemaVersion") !==
      CANONICAL_EXCALIDRAW_SCHEMA_VERSION ||
    roots.metadata.get("tenantId") !== expected.tenantId ||
    roots.metadata.get("documentId") !== expected.documentId ||
    roots.metadata.get("generation") !== expected.generation
  ) {
    throw new CanonicalSceneError("authority_scope_mismatch");
  }
}

function validateScope(scope: CanonicalAuthorityScope): void {
  validateIdentifier(scope.tenantId, "authority_scope_invalid");
  validateIdentifier(scope.documentId, "authority_scope_invalid");
  if (!Number.isSafeInteger(scope.generation) || scope.generation < 1) {
    throw new CanonicalSceneError("authority_scope_invalid");
  }
}

function validateIdentifier(
  value: unknown,
  code: CanonicalSceneErrorCode,
): asserts value is string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    value.length > CANONICAL_EXCALIDRAW_LIMITS.maxIdentifierLength ||
    !/^[A-Za-z0-9._:-]+$/.test(value)
  ) {
    throw new CanonicalSceneError(code);
  }
}

function toJsonValue(value: unknown): JsonValue {
  let serialized: string | undefined;
  try {
    serialized = JSON.stringify(value);
  } catch {
    throw new CanonicalSceneError("scene_corrupt");
  }
  if (serialized === undefined) {
    throw new CanonicalSceneError("scene_corrupt");
  }
  if (
    new TextEncoder().encode(serialized).byteLength >
    CANONICAL_EXCALIDRAW_LIMITS.maxDocumentBytes
  ) {
    throw new CanonicalSceneError("scene_too_large");
  }
  try {
    return JSON.parse(serialized) as JsonValue;
  } catch {
    throw new CanonicalSceneError("scene_corrupt");
  }
}

function validateJsonBounds(value: JsonValue, depth = 0): void {
  if (depth > CANONICAL_EXCALIDRAW_LIMITS.maxDepth) {
    throw new CanonicalSceneError("scene_too_deep");
  }
  if (typeof value === "string") {
    if (value.length > CANONICAL_EXCALIDRAW_LIMITS.maxStringLength) {
      throw new CanonicalSceneError("scene_too_large");
    }
    return;
  }
  if (typeof value === "number" && !Number.isFinite(value)) {
    throw new CanonicalSceneError("scene_invalid");
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      validateJsonBounds(item, depth + 1);
    }
    return;
  }
  if (isJsonObject(value)) {
    for (const item of Object.values(value)) {
      validateJsonBounds(item, depth + 1);
    }
  }
}

function stableStringify(value: JsonValue): string {
  if (value === null || typeof value !== "object") {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }
  return `{${Object.keys(value)
    .sort()
    .map(
      (key) =>
        `${JSON.stringify(key)}:${stableStringify(value[key] as JsonValue)}`,
    )
    .join(",")}}`;
}

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function cloneJson<T extends JsonValue>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
