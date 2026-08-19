// @vitest-environment node

import { afterEach, describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  CanonicalExcalidrawAuthority,
  semanticHash,
  type CanonicalExcalidrawSceneV1,
} from "./canonicalAuthority";
import {
  PORTABLE_EXCALIDRAW_FORMAT_VERSION,
  PortableSceneError,
  exportPortableScene,
  importPortableScene,
} from "./portableScene";

const resources: Array<CanonicalExcalidrawAuthority | Y.Doc> = [];

afterEach(() => {
  for (const resource of resources.splice(0).reverse()) {
    resource.destroy();
  }
});

describe("portable Excalidraw provider-exit artifact", () => {
  it("round-trips through provider-neutral canonical JSON with the same semantic hash", () => {
    const scene = createScene();
    const bytes = exportPortableScene(scene, "2026-08-19T03:04:05.000Z");
    const serialized = new TextDecoder().decode(bytes);
    const envelope = JSON.parse(serialized) as Record<string, unknown>;

    expect(envelope.formatVersion).toBe(PORTABLE_EXCALIDRAW_FORMAT_VERSION);
    expect(serialized).not.toContain("providerState");
    expect(serialized).not.toContain("yjs");

    const imported = importPortableScene(bytes);
    expect(semanticHash(imported)).toBe(semanticHash(scene));

    const targetDocument = track(new Y.Doc());
    const targetAuthority = track(
      new CanonicalExcalidrawAuthority(
        targetDocument,
        { documentId: "board-exit", generation: 2, tenantId: "tenant-a" },
        "provider-exit-worker",
      ),
    );
    targetAuthority.initialize(imported);
    expect(targetAuthority.getSemanticHash()).toBe(semanticHash(scene));
  });

  it("fails closed for tampering, unsupported versions, active content, and external fetch input", () => {
    const bytes = exportPortableScene(
      createScene(),
      "2026-08-19T03:04:05.000Z",
    );
    const envelope = JSON.parse(new TextDecoder().decode(bytes)) as {
      formatVersion: number;
      scene: CanonicalExcalidrawSceneV1;
      semanticHash: string;
    };

    expectPortableError(
      () =>
        importPortableScene(
          encodeJson({ ...envelope, semanticHash: "fnv1a64:0000000000000000" }),
        ),
      "portable_hash_mismatch",
    );
    expectPortableError(
      () => importPortableScene(encodeJson({ ...envelope, formatVersion: 2 })),
      "portable_format_unsupported",
    );

    const externalScene: CanonicalExcalidrawSceneV1 = {
      ...envelope.scene,
      elements: envelope.scene.elements.map((element, index) =>
        index === 0
          ? { ...element, link: "https://example.invalid/a" }
          : element,
      ),
    };
    expectPortableError(
      () =>
        importPortableScene(
          encodeJson({
            ...envelope,
            scene: externalScene,
            semanticHash: semanticHash(externalScene),
          }),
        ),
      "portable_active_content_denied",
    );

    const activeScene: CanonicalExcalidrawSceneV1 = {
      ...envelope.scene,
      files: {
        "file-1": {
          dataURL: "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",
          id: "file-1",
          mimeType: "image/svg+xml",
        },
      },
    };
    expectPortableError(
      () =>
        importPortableScene(
          encodeJson({
            ...envelope,
            scene: activeScene,
            semanticHash: semanticHash(activeScene),
          }),
        ),
      "portable_active_content_denied",
    );
  });
});

function createScene(): CanonicalExcalidrawSceneV1 {
  return {
    elements: [
      {
        height: 80,
        id: "shape-1",
        type: "rectangle",
        width: 120,
        x: 20,
        y: 40,
      },
      {
        height: 30,
        id: "text-1",
        text: "Gate D",
        type: "text",
        width: 90,
        x: 35,
        y: 55,
      },
    ],
    files: {},
    page: { backgroundColor: "#ffffff", id: "page-1", name: "Gate D" },
    schemaVersion: CANONICAL_EXCALIDRAW_SCHEMA_VERSION,
  };
}

function track<T extends CanonicalExcalidrawAuthority | Y.Doc>(resource: T): T {
  resources.push(resource);
  return resource;
}

function encodeJson(value: unknown): Uint8Array {
  return new TextEncoder().encode(JSON.stringify(value));
}

function expectPortableError(
  callback: () => void,
  code: PortableSceneError["code"],
): void {
  expect(callback).toThrowError(
    expect.objectContaining<Partial<PortableSceneError>>({ code }),
  );
}
