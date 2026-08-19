// @vitest-environment node

import { createHash } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  CanonicalExcalidrawAuthority,
  type CanonicalExcalidrawSceneV1,
} from "../src/excalidraw/canonicalAuthority";
import { CollaborationControlPlane } from "./excalidrawAuthorizationHarness";
import {
  EXCALIDRAW_PROVIDER,
  EXCALIDRAW_SNAPSHOT_CREATOR,
  EXCALIDRAW_SNAPSHOT_FORMAT,
  EXCALIDRAW_SNAPSHOT_FORMAT_VERSION,
  DurableSnapshotStore,
  SnapshotRestoreCoordinator,
} from "./excalidrawSnapshotHarness";

const roots: string[] = [];
const resources: Array<CanonicalExcalidrawAuthority | Y.Doc> = [];
const bindingKey = new Uint8Array(32).fill(17);
const replacementBindingKey = new Uint8Array(32).fill(29);
const fixedNow = () => new Date("2026-08-19T04:05:06.000Z");
const origin = "https://tutorhub.example.test";

afterEach(async () => {
  for (const resource of resources.splice(0).reverse()) {
    resource.destroy();
  }
  for (const root of roots.splice(0)) {
    await rm(root, { force: true, recursive: true });
  }
});

describe("Excalidraw Gate D durable snapshot and restore", () => {
  it("recovers after restart and ignores an artifact interrupted before catalog publication", async () => {
    const root = await createRoot();
    const authority = createAuthority();
    const store = await openStore(root);
    const lastGoodHash = authority.getSemanticHash();
    const published = await store.createSnapshot(authority);

    authority.putElement(rectangle("after-last-good", 300));
    await expect(
      store.createSnapshot(authority, { faultAfterArtifactWrite: true }),
    ).rejects.toMatchObject({
      code: "snapshot_write_interrupted",
    });
    authority.putElement(rectangle("after-interruption", 500));

    const reopened = await openStore(root);
    expect(reopened.listSnapshots()).toEqual([
      expect.objectContaining({
        causalWatermark: published.causalWatermark,
        snapshotId: published.snapshotId,
        status: "published",
      }),
    ]);
    const recovered = await reopened.recoverProviderAuthority(
      published.snapshotId,
      "recovery-worker",
    );
    resources.push(recovered.authority, recovered.document);
    expect(recovered.authority.getSemanticHash()).toBe(lastGoodHash);
    expect(
      recovered.authority
        .getScene()
        .elements.some((element) => element.id === "after-last-good"),
    ).toBe(false);
  });

  it("publishes a versioned, bounded, checksummed envelope with an opaque object binding", async () => {
    const root = await createRoot();
    const authority = createAuthority();
    const store = await openStore(root);
    const entry = await store.createSnapshot(authority);
    const bytes = await readFile(join(root, entry.objectKey));
    const artifact = JSON.parse(bytes.toString("utf8")) as Record<
      string,
      unknown
    >;

    expect(entry.checksum).toBe(
      `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    );
    expect(entry.byteLength).toBe(bytes.byteLength);
    expect(entry.elementCount).toBe(2);
    expect(entry.fileCount).toBe(0);
    expect(entry.createdBy).toBe(EXCALIDRAW_SNAPSHOT_CREATOR);
    expect(entry.objectKey).toMatch(/^snapshots\/[A-Za-z0-9_-]{32}\.json$/);
    expect(entry.objectKey).not.toContain("tenant-a");
    expect(entry.objectKey).not.toContain("board-1");
    expect(bytes.toString("utf8")).not.toContain("tenant-a");
    expect(bytes.toString("utf8")).not.toContain("board-1");
    expect(artifact.format).toBe(EXCALIDRAW_SNAPSHOT_FORMAT);
    expect(artifact.formatVersion).toBe(EXCALIDRAW_SNAPSHOT_FORMAT_VERSION);
    expect(artifact.canonicalSchemaVersion).toBe(
      CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
    );
    expect(artifact.provider).toEqual(EXCALIDRAW_PROVIDER);
    expect(artifact.causalWatermark).toBe(entry.causalWatermark);
    expect(artifact.scopeBinding).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(artifact.scopeBindingKeyId).toBe("snapshot-key-v1");
    expect(typeof artifact.portableScene).toBe("string");
    expect(typeof artifact.providerState).toBe("string");
  });

  it("quarantines corruption, keeps last-good generation, then restores by atomic generation swap and denies stale grants", async () => {
    const root = await createRoot();
    const authority = createAuthority();
    const store = await openStore(root);
    const lastGood = await store.createSnapshot(authority);
    authority.putElement(rectangle("newer", 400));
    const corrupt = await store.createSnapshot(authority);
    await writeFile(join(root, corrupt.objectKey), '{"tampered":true}');

    const controlPlane = new CollaborationControlPlane(origin);
    const staleGrant = controlPlane.issueGrant(
      {
        actorId: "student-b",
        documentId: "board-1",
        expectedGeneration: 1,
        requestedCapability: "edit",
        sessionId: "student-session",
        tenantId: "tenant-a",
      },
      origin,
    );
    const coordinator = new SnapshotRestoreCoordinator(store, controlPlane);
    await expect(
      coordinator.restore({
        actorId: "restore-worker",
        documentId: "board-1",
        expectedGeneration: 1,
        snapshotId: corrupt.snapshotId,
        tenantId: "tenant-a",
      }),
    ).rejects.toMatchObject({
      code: "snapshot_quarantined",
    });
    expect(controlPlane.currentGeneration("tenant-a", "board-1")).toBe(1);
    expect(
      store
        .listSnapshots()
        .find((entry) => entry.snapshotId === corrupt.snapshotId),
    ).toEqual(
      expect.objectContaining({
        quarantineReason: "checksum_mismatch",
        status: "quarantined",
      }),
    );

    const restored = await coordinator.restore({
      actorId: "restore-worker",
      documentId: "board-1",
      expectedGeneration: 1,
      snapshotId: lastGood.snapshotId,
      tenantId: "tenant-a",
    });
    resources.push(restored.authority, restored.document);
    expect(restored.previousGeneration).toBe(1);
    expect(restored.generation).toBe(2);
    expect(restored.authority.scope.generation).toBe(2);
    expect(restored.authority.getSemanticHash()).toBe(lastGood.semanticHash);
    expect(() =>
      controlPlane.exchangeGrant(
        staleGrant.grant,
        origin,
        staleGrant.providerDocumentName,
      ),
    ).toThrowError("stale_generation");
  });

  it("serializes competing restores so only one authority can own the next generation", async () => {
    const root = await createRoot();
    const authority = createAuthority();
    const store = await openStore(root);
    const snapshot = await store.createSnapshot(authority);
    const controlPlane = new CollaborationControlPlane(origin);
    const coordinator = new SnapshotRestoreCoordinator(store, controlPlane);

    const first = coordinator.restore({
      actorId: "restore-worker-a",
      documentId: "board-1",
      expectedGeneration: 1,
      snapshotId: snapshot.snapshotId,
      tenantId: "tenant-a",
    });
    const second = coordinator.restore({
      actorId: "restore-worker-b",
      documentId: "board-1",
      expectedGeneration: 1,
      snapshotId: snapshot.snapshotId,
      tenantId: "tenant-a",
    });

    await expect(second).rejects.toMatchObject({
      code: "snapshot_restore_in_progress",
    });
    const restored = await first;
    resources.push(restored.authority, restored.document);
    expect(restored.generation).toBe(2);
    expect(controlPlane.currentGeneration("tenant-a", "board-1")).toBe(2);
  });

  it("keeps retained snapshots readable during binding-key rotation and fails closed after the old key retires", async () => {
    const root = await createRoot();
    const authority = createAuthority();
    const oldStore = await DurableSnapshotStore.open({
      activeScopeBindingKeyId: "snapshot-key-v1",
      now: fixedNow,
      rootDirectory: root,
      scopeBindingKey: bindingKey,
    });
    const oldSnapshot = await oldStore.createSnapshot(authority);

    const rotatingStore = await DurableSnapshotStore.open({
      activeScopeBindingKeyId: "snapshot-key-v2",
      historicalScopeBindingKeys: {
        "snapshot-key-v1": bindingKey,
      },
      now: fixedNow,
      rootDirectory: root,
      scopeBindingKey: replacementBindingKey,
    });
    await expect(
      rotatingStore.readPortableScene(oldSnapshot.snapshotId, {
        documentId: "board-1",
        tenantId: "tenant-a",
      }),
    ).resolves.toMatchObject({
      entry: { scopeBindingKeyId: "snapshot-key-v1", status: "published" },
    });
    const recoveredWithHistoricalKey =
      await rotatingStore.recoverProviderAuthority(
        oldSnapshot.snapshotId,
        "rotation-recovery-worker",
      );
    resources.push(
      recoveredWithHistoricalKey.authority,
      recoveredWithHistoricalKey.document,
    );
    expect(recoveredWithHistoricalKey.authority.getSemanticHash()).toBe(
      oldSnapshot.semanticHash,
    );
    const newSnapshot = await rotatingStore.createSnapshot(authority);
    expect(newSnapshot.scopeBindingKeyId).toBe("snapshot-key-v2");

    const currentOnlyStore = await DurableSnapshotStore.open({
      activeScopeBindingKeyId: "snapshot-key-v2",
      now: fixedNow,
      rootDirectory: root,
      scopeBindingKey: replacementBindingKey,
    });
    await expect(
      currentOnlyStore.readPortableScene(oldSnapshot.snapshotId, {
        documentId: "board-1",
        tenantId: "tenant-a",
      }),
    ).rejects.toMatchObject({ code: "snapshot_binding_invalid" });
    expect(
      currentOnlyStore
        .listSnapshots()
        .find((entry) => entry.snapshotId === oldSnapshot.snapshotId),
    ).toMatchObject({ status: "published" });
    await expect(
      currentOnlyStore.readPortableScene(newSnapshot.snapshotId, {
        documentId: "board-1",
        tenantId: "tenant-a",
      }),
    ).resolves.toMatchObject({
      entry: { scopeBindingKeyId: "snapshot-key-v2", status: "published" },
    });
  });
});

async function createRoot(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "tutorhub-p5-gate-d-"));
  roots.push(root);
  return root;
}

function openStore(root: string): Promise<DurableSnapshotStore> {
  return DurableSnapshotStore.open({
    now: fixedNow,
    rootDirectory: root,
    scopeBindingKey: bindingKey,
  });
}

function createAuthority(): CanonicalExcalidrawAuthority {
  const document = new Y.Doc();
  const authority = new CanonicalExcalidrawAuthority(
    document,
    { documentId: "board-1", generation: 1, tenantId: "tenant-a" },
    "teacher-a",
  );
  resources.push(authority, document);
  authority.initialize(createScene());
  return authority;
}

function createScene(): CanonicalExcalidrawSceneV1 {
  return {
    elements: [rectangle("shape-1", 20), text("text-1", "Gate D")],
    files: {},
    page: { backgroundColor: "#ffffff", id: "page-1", name: "Gate D" },
    schemaVersion: CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  };
}

function rectangle(id: string, x: number) {
  return { height: 80, id, type: "rectangle", width: 120, x, y: 40 };
}

function text(id: string, value: string) {
  return {
    height: 30,
    id,
    text: value,
    type: "text",
    width: 90,
    x: 35,
    y: 55,
  };
}
