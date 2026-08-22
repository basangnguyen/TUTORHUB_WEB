import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  semanticHash,
  type CanonicalExcalidrawSceneV1,
} from "@tutorhub/collaboration-client";
import { createArtifactEnvelope } from "./artifactEnvelope.js";
import type {
  ArtifactObjectBinding,
  ArtifactObjectExpectation,
} from "./artifactObjectStore.js";
import type {
  ArtifactJob,
  ArtifactQueuePort,
  PublishedArtifact,
} from "./artifactQueue.js";
import {
  type ArtifactWorkerLogger,
  WhiteboardArtifactWorker,
  validatePortableImport,
} from "./artifactWorker.js";

const tenantId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const documentId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const bindingKey = {
  id: "snapshot-binding-v1",
  secret: "not-a-real-secret-but-long-enough-for-tests",
};

class FakeQueue implements ArtifactQueuePort {
  completedRestore: Uint8Array | undefined;
  failed: { code: string; disposition: string } | undefined;
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
    this.completedRestore = providerState;
  }
  async fail(
    _job: ArtifactJob,
    code: string,
    disposition: "failed" | "quarantined" | "retryable",
  ): Promise<void> {
    this.failed = { code, disposition };
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

class FakeObjectStore {
  bytes = new Uint8Array();
  expectation: ArtifactObjectExpectation | undefined;

  async deleteVersion(): Promise<void> {}
  async getVerified(
    expectation: ArtifactObjectExpectation,
  ): Promise<Uint8Array> {
    this.expectation = expectation;
    return this.bytes;
  }
  async putVerified(bytes: Uint8Array): Promise<ArtifactObjectBinding> {
    this.bytes = bytes;
    return {
      objectKey: `wb/ab/${"a".repeat(48)}`,
      objectVersionId: "4_z-version",
    };
  }
}

describe("WhiteboardArtifactWorker", () => {
  it("publishes a verified snapshot then restores it into a fresh generation", async () => {
    const scene = createScene();
    const state = createProviderState(1, scene);
    const objects = new FakeObjectStore();
    const snapshotQueue = new FakeQueue([createSnapshotJob()], state);
    await createWorker(snapshotQueue, objects).runOnce();

    expect(snapshotQueue.published?.contentSha256).toMatch(/^[a-f0-9]{64}$/);
    expect(snapshotQueue.failed).toBeUndefined();

    const restoreJob: ArtifactJob = {
      ...createSnapshotJob(),
      commandId: "33333333-3333-4333-8333-333333333333",
      kind: "restore",
      source: {
        ...snapshotQueue.published!,
        generation: 1,
        snapshotId: "44444444-4444-4444-8444-444444444444",
      },
      targetGeneration: 2,
      targetProviderDocumentName: `wb_${"c".repeat(22)}`,
    };
    const restoreQueue = new FakeQueue([restoreJob], state);
    await createWorker(restoreQueue, objects).runOnce();

    expect(restoreQueue.failed).toBeUndefined();
    expect(restoreQueue.completedRestore).toBeDefined();
    expect(readSemanticHash(restoreQueue.completedRestore!, 2)).toBe(
      semanticHash(scene),
    );
  });

  it("fails a stale-generation job without publishing", async () => {
    const queue = new FakeQueue(
      [{ ...createSnapshotJob(), currentGeneration: 2 }],
      createProviderState(1, createScene()),
    );
    await createWorker(queue, new FakeObjectStore()).runOnce();

    expect(queue.published).toBeUndefined();
    expect(queue.failed).toEqual({
      code: "artifact_generation_stale",
      disposition: "failed",
    });
  });

  it("rejects active external content during portable import", () => {
    const scene = {
      ...createScene(),
      elements: [
        { ...createScene().elements[0], link: "https://example.invalid" },
      ],
    };
    const artifact = createArtifactEnvelope(
      { documentId, generation: 1, tenantId },
      createProviderState(1, createScene()),
      bindingKey,
      "2026-08-22T08:00:00.000Z",
    );
    const portable = JSON.parse(new TextDecoder().decode(artifact.bytes)) as {
      portableScene: Record<string, unknown>;
    };
    portable.portableScene.scene = scene;
    portable.portableScene.semanticHash = semanticHash(scene);

    expect(() =>
      validatePortableImport(
        new TextEncoder().encode(JSON.stringify(portable.portableScene)),
      ),
    ).toThrowError("portable_active_content_denied");
  });

  it("fails an unbound import command without emitting success telemetry", async () => {
    const queue = new FakeQueue(
      [{ ...createSnapshotJob(), kind: "import_validate" }],
      createProviderState(1, createScene()),
    );
    const events: Parameters<ArtifactWorkerLogger["event"]>[0][] = [];

    await createWorker(queue, new FakeObjectStore(), {
      event(event) {
        events.push(event);
      },
    }).runOnce();

    expect(queue.failed).toEqual({
      code: "artifact_import_unbound",
      disposition: "failed",
    });
    expect(events).toEqual([
      {
        event_code: "artifact_command",
        outcome: "failed",
        reason_code: "artifact_import_unbound",
      },
    ]);
  });
});

function createWorker(
  queue: FakeQueue,
  objects: FakeObjectStore,
  logger: ArtifactWorkerLogger = { event() {} },
): WhiteboardArtifactWorker {
  return new WhiteboardArtifactWorker(
    queue,
    objects,
    bindingKey,
    undefined,
    30,
    500,
    logger,
  );
}

function createSnapshotJob(): ArtifactJob {
  return {
    actorUserId: "22222222-2222-4222-8222-222222222222",
    attempts: 1,
    commandId: "11111111-1111-4111-8111-111111111111",
    currentGeneration: 1,
    documentId,
    generation: 1,
    kind: "snapshot",
    leaseToken: "55555555-5555-4555-8555-555555555555",
    providerDocumentName: `wb_${"b".repeat(22)}`,
    revokeGeneration: 1,
    tenantId,
  };
}

function createScene(): CanonicalExcalidrawSceneV1 {
  return {
    elements: [
      {
        height: 30,
        id: "text-1",
        text: "P5-COLLAB-07",
        type: "text",
        width: 140,
        x: 35,
        y: 55,
      },
    ],
    files: {},
    page: { backgroundColor: "#ffffff", id: "page-1", name: "Gate 07" },
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
      "artifact-worker-test",
    );
    authority.initialize(scene);
    authority.destroy();
    return Y.encodeStateAsUpdate(document);
  } finally {
    document.destroy();
  }
}

function readSemanticHash(
  providerState: Uint8Array,
  generation: number,
): string {
  const document = new Y.Doc();
  try {
    Y.applyUpdate(document, providerState);
    const authority = new CanonicalExcalidrawAuthority(
      document,
      { documentId, generation, tenantId },
      "artifact-worker-verifier",
    );
    const hash = authority.getSemanticHash();
    authority.destroy();
    return hash;
  } finally {
    document.destroy();
  }
}
