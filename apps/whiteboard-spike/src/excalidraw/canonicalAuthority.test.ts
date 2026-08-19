// @vitest-environment node

import { afterEach, describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CANONICAL_EXCALIDRAW_LIMITS,
  CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  CanonicalExcalidrawAuthority,
  CanonicalSceneError,
  canonicalSceneToExcalidraw,
  excalidrawSceneToCanonical,
  semanticHash,
  type CanonicalAuthorityScope,
  type CanonicalExcalidrawSceneV1,
  type JsonObject,
} from "./canonicalAuthority";

const documents: Y.Doc[] = [];
const authorities: CanonicalExcalidrawAuthority[] = [];
const scope: CanonicalAuthorityScope = {
  documentId: "board-1",
  generation: 7,
  tenantId: "tenant-a",
};

afterEach(() => {
  for (const authority of authorities.splice(0)) {
    authority.destroy();
  }
  for (const document of documents.splice(0)) {
    document.destroy();
  }
});

describe("Excalidraw scene and canonical Y.Doc authority", () => {
  it("round-trips the supported shape/text/binding/image/page projection losslessly", () => {
    const projection = createProjectionFixture();
    const canonical = excalidrawSceneToCanonical(projection);
    const roundTrip = canonicalSceneToExcalidraw(canonical);

    expect(roundTrip).toEqual(projection);
    expect(canonical.schemaVersion).toBe(CANONICAL_EXCALIDRAW_SCHEMA_VERSION);
    expect(canonical.elements.map((element) => element.id)).toEqual([
      "shape-1",
      "text-1",
      "arrow-1",
      "image-1",
    ]);

    const sameSemanticsWithDifferentObjectKeyOrder = {
      files: canonical.files,
      schemaVersion: canonical.schemaVersion,
      page: {
        name: canonical.page.name,
        id: canonical.page.id,
        backgroundColor: canonical.page.backgroundColor,
      },
      elements: canonical.elements.map((element) =>
        Object.fromEntries(Object.entries(element).reverse()),
      ),
    };
    expect(semanticHash(sameSemanticsWithDifferentObjectKeyOrder)).toBe(
      semanticHash(canonical),
    );

    const changedZOrder = {
      ...canonical,
      elements: [...canonical.elements].reverse(),
    };
    expect(semanticHash(changedZOrder)).not.toBe(semanticHash(canonical));
  });

  it("converges concurrent element edits and preserves remote work during actor-local undo/redo", () => {
    const documentA = createDocument();
    const documentB = createDocument();
    const authorityA = createAuthority(documentA, "teacher-a");
    const authorityB = createAuthority(documentB, "student-b");
    authorityA.initialize(createCanonicalFixture());
    authorityB.applyRemoteUpdate(Y.encodeStateAsUpdate(documentA));

    const updatesA: Uint8Array[] = [];
    const updatesB: Uint8Array[] = [];
    documentA.on("update", (update: Uint8Array, origin: unknown) => {
      if (origin !== null) {
        updatesA.push(update);
      }
    });
    documentB.on("update", (update: Uint8Array, origin: unknown) => {
      if (origin !== null) {
        updatesB.push(update);
      }
    });

    authorityA.putElement(createRectangle("teacher-note", 640));
    authorityB.putElement(createRectangle("student-note", 760));
    const teacherUpdate = updatesA.at(-1);
    const studentUpdate = updatesB.at(-1);
    expect(teacherUpdate).toBeDefined();
    expect(studentUpdate).toBeDefined();
    authorityA.applyRemoteUpdate(studentUpdate as Uint8Array);
    authorityB.applyRemoteUpdate(teacherUpdate as Uint8Array);

    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());
    expect(sceneIds(authorityA.getScene())).toEqual(
      expect.arrayContaining(["teacher-note", "student-note"]),
    );

    updatesA.length = 0;
    expect(authorityA.undo()).toBe(true);
    const undoUpdate = updatesA.at(-1);
    expect(undoUpdate).toBeDefined();
    authorityB.applyRemoteUpdate(undoUpdate as Uint8Array);
    expect(sceneIds(authorityA.getScene())).not.toContain("teacher-note");
    expect(sceneIds(authorityA.getScene())).toContain("student-note");
    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());

    updatesA.length = 0;
    expect(authorityA.redo()).toBe(true);
    const redoUpdate = updatesA.at(-1);
    expect(redoUpdate).toBeDefined();
    authorityB.applyRemoteUpdate(redoUpdate as Uint8Array);
    expect(sceneIds(authorityA.getScene())).toEqual(
      expect.arrayContaining(["teacher-note", "student-note"]),
    );
    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());
  });

  it("skips projection-only duplicate revisions during semantic undo and redo", () => {
    const document = createDocument();
    const authority = createAuthority(document, "teacher-a");
    authority.initialize(createCanonicalFixture());
    const duplicateProjection = createRectangle("teacher-note", 640);

    authority.putElement(duplicateProjection);
    authority.putElement(duplicateProjection);
    expect(sceneIds(authority.getScene())).toContain("teacher-note");

    expect(authority.undo()).toBe(true);
    expect(sceneIds(authority.getScene())).not.toContain("teacher-note");

    expect(authority.redo()).toBe(true);
    expect(sceneIds(authority.getScene())).toContain("teacher-note");
  });

  it("keeps the remote actor version when local undo targets a concurrently edited element", () => {
    const documentA = createDocument();
    const documentB = createDocument();
    const authorityA = createAuthority(documentA, "teacher-a");
    const authorityB = createAuthority(documentB, "student-b");
    authorityA.initialize(createCanonicalFixture());
    authorityB.applyRemoteUpdate(Y.encodeStateAsUpdate(documentA));

    const updatesA: Uint8Array[] = [];
    const updatesB: Uint8Array[] = [];
    documentA.on("update", (update: Uint8Array, origin: unknown) => {
      if (origin !== null) {
        updatesA.push(update);
      }
    });
    documentB.on("update", (update: Uint8Array, origin: unknown) => {
      if (origin !== null) {
        updatesB.push(update);
      }
    });

    authorityA.putElement(createRectangle("shape-1", 320));
    authorityB.putElement(createRectangle("shape-1", 540));
    const actorAUpdate = updatesA.at(-1) as Uint8Array;
    const actorBUpdate = updatesB.at(-1) as Uint8Array;
    authorityA.applyRemoteUpdate(actorBUpdate);
    authorityB.applyRemoteUpdate(actorAUpdate);
    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());

    updatesA.length = 0;
    expect(authorityA.undo()).toBe(true);
    const undoUpdate = updatesA.at(-1);
    if (undoUpdate !== undefined) {
      authorityB.applyRemoteUpdate(undoUpdate);
    }
    const elementA = authorityA
      .getScene()
      .elements.find((element) => element.id === "shape-1");
    const elementB = authorityB
      .getScene()
      .elements.find((element) => element.id === "shape-1");
    expect(elementA?.x).toBe(540);
    expect(elementB?.x).toBe(540);
    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());
  });

  it("applies a stale Excalidraw projection as an actor delta without deleting a concurrent remote element", () => {
    const documentA = createDocument();
    const documentB = createDocument();
    const authorityA = createAuthority(documentA, "teacher-a");
    const authorityB = createAuthority(documentB, "student-b");
    authorityA.initialize(createCanonicalFixture());
    authorityB.applyRemoteUpdate(Y.encodeStateAsUpdate(documentA));
    const baselineA = authorityA.getScene();
    const baselineB = authorityB.getScene();

    authorityB.applySceneDelta(baselineB, {
      ...baselineB,
      elements: [...baselineB.elements, createRectangle("student-note", 760)],
    });
    authorityA.applyRemoteUpdate(Y.encodeStateAsUpdate(documentB));
    authorityA.applySceneDelta(baselineA, {
      ...baselineA,
      elements: [...baselineA.elements, createRectangle("teacher-note", 640)],
    });
    authorityB.applyRemoteUpdate(Y.encodeStateAsUpdate(documentA));

    expect(sceneIds(authorityA.getScene())).toEqual(
      expect.arrayContaining(["teacher-note", "student-note"]),
    );
    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());
  });

  it("converges two-way offline edits after duplicate/out-of-order delivery and compaction", () => {
    const documentA = createDocument();
    const documentB = createDocument();
    const authorityA = createAuthority(documentA, "teacher-a");
    const authorityB = createAuthority(documentB, "student-b");
    authorityA.initialize(createCanonicalFixture());
    authorityB.applyRemoteUpdate(Y.encodeStateAsUpdate(documentA));

    const updatesA: Uint8Array[] = [];
    const updatesB: Uint8Array[] = [];
    documentA.on("update", (update: Uint8Array, origin: unknown) => {
      if (origin !== null) {
        updatesA.push(update);
      }
    });
    documentB.on("update", (update: Uint8Array, origin: unknown) => {
      if (origin !== null) {
        updatesB.push(update);
      }
    });

    authorityA.putElement(createRectangle("offline-a-1", 900));
    authorityA.putElement(createRectangle("offline-a-2", 1_020));
    authorityB.putElement(createRectangle("offline-b-1", 900));
    authorityB.putElement(createRectangle("offline-b-2", 1_020));

    const actorAUpdates = [...updatesA];
    const actorBUpdates = [...updatesB];
    for (const update of [...actorAUpdates].reverse()) {
      authorityB.applyRemoteUpdate(update);
    }
    authorityB.applyRemoteUpdate(actorAUpdates[0] as Uint8Array);
    for (const update of [...actorBUpdates].reverse()) {
      authorityA.applyRemoteUpdate(update);
    }
    authorityA.applyRemoteUpdate(actorBUpdates[0] as Uint8Array);

    expect(authorityA.getSemanticHash()).toBe(authorityB.getSemanticHash());
    expect(sceneIds(authorityA.getScene())).toEqual(
      expect.arrayContaining([
        "offline-a-1",
        "offline-a-2",
        "offline-b-1",
        "offline-b-2",
      ]),
    );

    const compacted = Y.encodeStateAsUpdate(documentA);
    const restoredDocument = createDocument();
    const restoredAuthority = createAuthority(
      restoredDocument,
      "restore-reader",
    );
    restoredAuthority.applyRemoteUpdate(compacted);
    expect(restoredAuthority.getSemanticHash()).toBe(
      authorityA.getSemanticHash(),
    );
  });

  it("fails closed with bounded errors for unsupported, corrupt, deep, oversized, and wrong-scope input", () => {
    const canonical = createCanonicalFixture();
    expectErrorCode(
      () =>
        excalidrawSceneToCanonical({
          ...createProjectionFixture(),
          elements: [createElement("iframe", "unsupported-1", 0)],
        }),
      "scene_element_unsupported",
    );
    expectErrorCode(
      () =>
        semanticHash({
          ...canonical,
          schemaVersion: 2,
        }),
      "scene_schema_unsupported",
    );
    expectErrorCode(
      () =>
        semanticHash({
          ...canonical,
          elements: [canonical.elements[0], canonical.elements[0]],
        }),
      "scene_duplicate_element",
    );
    expectErrorCode(
      () =>
        semanticHash({
          ...canonical,
          elements: Array.from(
            { length: CANONICAL_EXCALIDRAW_LIMITS.maxElements + 1 },
            (_, index) => createRectangle(`oversized-${index}`, index),
          ),
        }),
      "scene_element_limit",
    );

    const cyclic: JsonObject = {};
    cyclic.self = cyclic;
    expectErrorCode(
      () => semanticHash({ ...canonical, files: { cyclic } }),
      "scene_corrupt",
    );

    let nested: JsonObject = {};
    for (
      let index = 0;
      index < CANONICAL_EXCALIDRAW_LIMITS.maxDepth + 2;
      index += 1
    ) {
      nested = { nested };
    }
    expectErrorCode(
      () =>
        semanticHash({
          ...canonical,
          elements: [{ ...canonical.elements[0], customData: nested }],
        }),
      "scene_too_deep",
    );

    const document = createDocument();
    const authority = createAuthority(document, "teacher-a");
    authority.initialize(canonical);
    expectErrorCode(
      () =>
        authority.applyRemoteUpdate(
          new Uint8Array(CANONICAL_EXCALIDRAW_LIMITS.maxUpdateBytes + 1),
        ),
      "update_too_large",
    );
    expectErrorCode(
      () => authority.applyRemoteUpdate(new Uint8Array([255, 255, 255])),
      "update_corrupt",
    );

    const wrongScopeDocument = createDocument();
    const wrongScopeAuthority = createAuthority(
      wrongScopeDocument,
      "intruder",
      { ...scope, tenantId: "tenant-b" },
    );
    expectErrorCode(
      () =>
        wrongScopeAuthority.applyRemoteUpdate(Y.encodeStateAsUpdate(document)),
      "authority_scope_mismatch",
    );

    document.getMap<string>("tutorhub.excalidraw.page.v1").set("state", "{");
    expectErrorCode(() => authority.getScene(), "scene_storage_corrupt");
  });
});

function createDocument(): Y.Doc {
  const document = new Y.Doc();
  documents.push(document);
  return document;
}

function createAuthority(
  document: Y.Doc,
  actorId: string,
  authorityScope = scope,
): CanonicalExcalidrawAuthority {
  const authority = new CanonicalExcalidrawAuthority(
    document,
    authorityScope,
    actorId,
  );
  authorities.push(authority);
  return authority;
}

function createProjectionFixture() {
  return {
    appState: { viewBackgroundColor: "#f8fafc" },
    elements: [
      {
        ...createElement("rectangle", "shape-1", 100),
        boundElements: [{ id: "text-1", type: "text" }],
        backgroundColor: "#a5d8ff",
        fillStyle: "solid",
      },
      {
        ...createElement("text", "text-1", 120),
        containerId: "shape-1",
        originalText: "Phương trình bậc hai",
        text: "Phương trình bậc hai",
      },
      {
        ...createElement("arrow", "arrow-1", 300),
        endBinding: { elementId: "shape-1", focus: 0, gap: 4 },
        points: [
          [0, 0],
          [120, 80],
        ],
        startBinding: null,
      },
      {
        ...createElement("image", "image-1", 480),
        fileId: "file-1",
        status: "saved",
      },
    ],
    files: {
      "file-1": {
        created: 1_786_000_000_000,
        dataURL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB",
        id: "file-1",
        mimeType: "image/png",
      },
    },
    page: { id: "page-1", name: "Bài 1" },
  };
}

function createCanonicalFixture(): CanonicalExcalidrawSceneV1 {
  return excalidrawSceneToCanonical(createProjectionFixture());
}

function createRectangle(id: string, x: number) {
  return {
    ...createElement("rectangle", id, x),
    backgroundColor: "#b2f2bb",
    fillStyle: "solid",
  };
}

function createElement(type: string, id: string, x: number) {
  return {
    angle: 0,
    height: 100,
    id,
    index: null,
    isDeleted: false,
    frameId: null,
    groupIds: [],
    link: null,
    locked: false,
    opacity: 100,
    roughness: 1,
    roundness: null,
    seed: x + 1,
    strokeColor: "#1c3f60",
    strokeStyle: "solid",
    strokeWidth: 2,
    type,
    updated: 1_786_000_000_000,
    version: 1,
    versionNonce: x + 2,
    width: 160,
    x,
    y: 120,
  };
}

function sceneIds(scene: CanonicalExcalidrawSceneV1): string[] {
  return scene.elements.map((element) => element.id);
}

function expectErrorCode(
  callback: () => void,
  code: CanonicalSceneError["code"],
): void {
  expect(callback).toThrowError(
    expect.objectContaining<Partial<CanonicalSceneError>>({ code }),
  );
}
