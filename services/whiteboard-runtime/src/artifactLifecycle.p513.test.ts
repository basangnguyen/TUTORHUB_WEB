import { createHash, createHmac } from "node:crypto";
import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  semanticHash,
  type CanonicalExcalidrawSceneV1,
} from "@tutorhub/collaboration-client";
import {
  MAX_ARTIFACT_BYTES,
  createArtifactEnvelope,
  verifyArtifactEnvelope,
} from "./artifactEnvelope.js";
import {
  ArtifactObjectStoreError,
  type ArtifactObjectBinding,
} from "./artifactObjectStore.js";
import type {
  ArtifactJob,
  ArtifactQueuePort,
  PublishedArtifact,
} from "./artifactQueue.js";
import { WhiteboardArtifactWorker } from "./artifactWorker.js";

const tenantId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const documentId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const bindingKey = {
  id: "p5-collab-13-binding-v1",
  secret: "p5-collab-13-test-binding-secret-at-least-32-bytes",
};

class GateQueue implements ArtifactQueuePort {
  readonly completed: Uint8Array[] = [];
  readonly failures: { code: string; disposition: string }[] = [];
  published: PublishedArtifact | undefined;

  constructor(
    private readonly jobs: ArtifactJob[],
    private readonly checkpoint: Uint8Array,
  ) {}

  async claim(): Promise<ArtifactJob | null> {
    return this.jobs.shift() ?? null;
  }

  async completeRestore(
    _job: ArtifactJob,
    providerState: Uint8Array,
  ): Promise<void> {
    this.completed.push(providerState);
  }

  async fail(
    _job: ArtifactJob,
    code: string,
    disposition: "failed" | "quarantined" | "retryable",
  ): Promise<void> {
    this.failures.push({ code, disposition });
  }

  async loadCheckpoint(): Promise<Uint8Array> {
    return this.checkpoint;
  }

  async publishSnapshot(
    _job: ArtifactJob,
    artifact: PublishedArtifact,
  ): Promise<void> {
    this.published = artifact;
  }
}

class GateObjectStore {
  bytes = new Uint8Array();
  getCalls = 0;
  unavailableReads = 0;

  async deleteVersion(): Promise<void> {}

  async getVerified(): Promise<Uint8Array> {
    this.getCalls += 1;
    if (this.unavailableReads > 0) {
      this.unavailableReads -= 1;
      throw new ArtifactObjectStoreError("artifact_object_unavailable");
    }
    return this.bytes;
  }

  async putVerified(bytes: Uint8Array): Promise<ArtifactObjectBinding> {
    this.bytes = bytes;
    return {
      objectKey: `wb/13/${"a".repeat(48)}`,
      objectVersionId: "4_z-p513-version",
    };
  }
}

describe("P5-COLLAB-13 snapshot/import/export/restore gates", () => {
  it("preserves immutable checksum and semantic hash through export and restore", async () => {
    const scene = createScene();
    const providerState = createProviderState(1, scene);
    const store = new GateObjectStore();
    const exportQueue = new GateQueue([createJob("export")], providerState);

    await createWorker(exportQueue, store).runOnce();

    const published = exportQueue.published;
    expect(published).toBeDefined();
    expect(published?.contentSha256).toBe(sha256(store.bytes));
    const verified = verifyArtifactEnvelope(
      store.bytes,
      { documentId, generation: 1, tenantId },
      new Map([[bindingKey.id, bindingKey.secret]]),
    );
    expect(verified.semanticHash).toBe(semanticHash(scene));

    const restoreQueue = new GateQueue(
      [createRestoreJob(published!, 1)],
      providerState,
    );
    await createWorker(restoreQueue, store).runOnce();

    expect(restoreQueue.failures).toEqual([]);
    expect(restoreQueue.completed).toHaveLength(1);
    expect(readSemanticHash(restoreQueue.completed[0]!, 2)).toBe(
      semanticHash(scene),
    );
  });

  it.each([
    {
      expectedCode: "artifact_corrupt",
      name: "corrupt",
      transform: () => new TextEncoder().encode("{"),
    },
    {
      expectedCode: "artifact_version_unsupported",
      name: "incompatible",
      transform: (bytes: Uint8Array) =>
        resignEnvelope(bytes, (unsigned) => {
          unsigned.formatVersion = 999;
        }),
    },
    {
      expectedCode: "artifact_too_large",
      name: "oversize",
      transform: () => new Uint8Array(MAX_ARTIFACT_BYTES + 1),
    },
    {
      expectedCode: "artifact_active_content_denied",
      name: "malicious active content",
      transform: (bytes: Uint8Array) =>
        resignEnvelope(bytes, (unsigned) => {
          const portable = unsigned.portableScene as {
            scene: { elements: Record<string, unknown>[] };
          };
          portable.scene.elements[0]!.link = "https://attacker.invalid/payload";
        }),
    },
  ])(
    "quarantines $name input without staging a restore",
    async ({ expectedCode, transform }) => {
      const state = createProviderState(1, createScene());
      const artifact = createArtifactEnvelope(
        { documentId, generation: 1, tenantId },
        state,
        bindingKey,
        "2026-08-23T00:00:00.000Z",
      );
      const store = new GateObjectStore();
      store.bytes = transform(artifact.bytes);
      const queue = new GateQueue(
        [createRestoreJob(sourceFromArtifact(artifact), 1)],
        state,
      );

      await createWorker(queue, store).runOnce();

      expect(queue.completed).toEqual([]);
      expect(queue.failures).toEqual([
        { code: expectedCode, disposition: "quarantined" },
      ]);
    },
  );

  it("retries a B2 outage and restores only the last verified artifact", async () => {
    const scene = createScene();
    const state = createProviderState(1, scene);
    const artifact = createArtifactEnvelope(
      { documentId, generation: 1, tenantId },
      state,
      bindingKey,
      "2026-08-23T00:00:00.000Z",
    );
    const store = new GateObjectStore();
    store.bytes = artifact.bytes;
    store.unavailableReads = 1;
    const source = sourceFromArtifact(artifact);
    const queue = new GateQueue(
      [createRestoreJob(source, 1), createRestoreJob(source, 2)],
      state,
    );
    const worker = createWorker(queue, store);

    await worker.runOnce();
    expect(queue.completed).toEqual([]);
    expect(queue.failures).toEqual([
      { code: "artifact_object_unavailable", disposition: "retryable" },
    ]);

    await worker.runOnce();
    expect(queue.completed).toHaveLength(1);
    expect(readSemanticHash(queue.completed[0]!, 2)).toBe(semanticHash(scene));
    expect(store.getCalls).toBe(2);
  });

  it("fences a stale generation before any artifact operation", async () => {
    const state = createProviderState(1, createScene());
    const store = new GateObjectStore();
    const queue = new GateQueue(
      [{ ...createJob("snapshot"), currentGeneration: 2 }],
      state,
    );

    await createWorker(queue, store).runOnce();

    expect(store.bytes).toHaveLength(0);
    expect(queue.published).toBeUndefined();
    expect(queue.failures).toEqual([
      { code: "artifact_generation_stale", disposition: "failed" },
    ]);
  });
});

function createWorker(
  queue: GateQueue,
  store: GateObjectStore,
): WhiteboardArtifactWorker {
  return new WhiteboardArtifactWorker(
    queue,
    store,
    bindingKey,
    undefined,
    30,
    100,
    { event() {} },
  );
}

function createJob(kind: "export" | "snapshot"): ArtifactJob {
  return {
    actorUserId: "22222222-2222-4222-8222-222222222222",
    attempts: 1,
    commandId: "11111111-1111-4111-8111-111111111111",
    currentGeneration: 1,
    documentId,
    generation: 1,
    kind,
    leaseToken: "55555555-5555-4555-8555-555555555555",
    providerDocumentName: `wb_${"b".repeat(22)}`,
    revokeGeneration: 1,
    tenantId,
  };
}

function createRestoreJob(
  source: PublishedArtifact,
  attempts: number,
): ArtifactJob {
  return {
    ...createJob("snapshot"),
    attempts,
    commandId: `33333333-3333-4333-8333-33333333333${attempts}`,
    kind: "restore",
    source: {
      ...source,
      generation: 1,
      snapshotId: "44444444-4444-4444-8444-444444444444",
    },
    targetGeneration: 2,
    targetProviderDocumentName: `wb_${"c".repeat(22)}`,
  };
}

function sourceFromArtifact(artifact: {
  bytes: Uint8Array;
  contentSha256: string;
}): PublishedArtifact {
  return {
    causalWatermarkSha256: "0".repeat(64),
    contentSha256: artifact.contentSha256,
    objectKey: `wb/13/${"a".repeat(48)}`,
    objectVersionId: "4_z-p513-source",
    sizeBytes: artifact.bytes.byteLength,
    verificationKeyId: bindingKey.id,
  };
}

function createScene(): CanonicalExcalidrawSceneV1 {
  return {
    elements: [
      {
        height: 40,
        id: "p513-text",
        text: "P5-COLLAB-13 immutable snapshot",
        type: "text",
        width: 260,
        x: 48,
        y: 72,
      },
    ],
    files: {},
    page: { backgroundColor: "#ffffff", id: "page-1", name: "Gate 13" },
    schemaVersion: 1,
  };
}

function createProviderState(
  generation: number,
  scene: CanonicalExcalidrawSceneV1,
): Uint8Array {
  const document = new Y.Doc();
  try {
    const authority = new CanonicalExcalidrawAuthority(
      document,
      { documentId, generation, tenantId },
      "p513-fixture",
    );
    authority.initialize(scene);
    authority.destroy();
    return Y.encodeStateAsUpdate(document);
  } finally {
    document.destroy();
  }
}

function readSemanticHash(state: Uint8Array, generation: number): string {
  const document = new Y.Doc();
  try {
    Y.applyUpdate(document, state);
    const authority = new CanonicalExcalidrawAuthority(
      document,
      { documentId, generation, tenantId },
      "p513-verifier",
    );
    const value = authority.getSemanticHash();
    authority.destroy();
    return value;
  } finally {
    document.destroy();
  }
}

function resignEnvelope(
  bytes: Uint8Array,
  mutate: (unsigned: Record<string, unknown>) => void,
): Uint8Array {
  const candidate = JSON.parse(new TextDecoder().decode(bytes)) as Record<
    string,
    unknown
  >;
  const scopeBinding = candidate.scopeBinding as Record<string, unknown>;
  const unsigned = { ...candidate };
  delete unsigned.scopeBinding;
  mutate(unsigned);
  const hmacSha256 = createHmac("sha256", bindingKey.secret)
    .update(stableStringify(unsigned))
    .digest("hex");
  return new TextEncoder().encode(
    stableStringify({
      ...unsigned,
      scopeBinding: { ...scopeBinding, hmacSha256 },
    }),
  );
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

function sha256(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}
