import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  semanticHash,
  type CanonicalExcalidrawSceneV1,
} from "@tutorhub/collaboration-client";
import {
  ArtifactEnvelopeError,
  createArtifactEnvelope,
  verifyArtifactEnvelope,
} from "./artifactEnvelope.js";

const scope = {
  documentId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  generation: 3,
  tenantId: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
};
const bindingKey = {
  id: "snapshot-binding-v1",
  secret: "not-a-real-secret-but-long-enough-for-tests",
};

describe("whiteboard artifact envelope", () => {
  it("round-trips exact provider state and portable semantics", () => {
    const state = createProviderState(scope, createScene());
    const artifact = createArtifactEnvelope(
      scope,
      state,
      bindingKey,
      "2026-08-22T08:00:00.000Z",
    );

    const verified = verifyArtifactEnvelope(
      artifact.bytes,
      scope,
      new Map([[bindingKey.id, bindingKey.secret]]),
    );

    expect(verified.providerState).toEqual(state);
    expect(verified.semanticHash).toBe(semanticHash(createScene()));
    expect(artifact.contentSha256).toMatch(/^[a-f0-9]{64}$/);
    expect(artifact.causalWatermarkSha256).toMatch(/^[a-f0-9]{64}$/);
  });

  it("fails closed for tampering, unknown keys, and cross-scope restore", () => {
    const artifact = createArtifactEnvelope(
      scope,
      createProviderState(scope, createScene()),
      bindingKey,
      "2026-08-22T08:00:00.000Z",
    );
    const tamperedEnvelope = JSON.parse(
      new TextDecoder().decode(artifact.bytes),
    ) as Record<string, unknown>;
    tamperedEnvelope.semanticHash = "fnv1a64:0000000000000000";
    const tampered = new TextEncoder().encode(JSON.stringify(tamperedEnvelope));

    expectCode(
      () =>
        verifyArtifactEnvelope(
          tampered,
          scope,
          new Map([[bindingKey.id, bindingKey.secret]]),
        ),
      "artifact_binding_invalid",
    );
    expectCode(
      () => verifyArtifactEnvelope(artifact.bytes, scope, new Map()),
      "artifact_binding_invalid",
    );
    expectCode(
      () =>
        verifyArtifactEnvelope(
          artifact.bytes,
          { ...scope, tenantId: "cccccccc-cccc-4ccc-8ccc-cccccccccccc" },
          new Map([[bindingKey.id, bindingKey.secret]]),
        ),
      "artifact_scope_mismatch",
    );
  });
});

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
  authorityScope: typeof scope,
  scene: CanonicalExcalidrawSceneV1,
): Uint8Array {
  const document = new Y.Doc();
  try {
    const authority = new CanonicalExcalidrawAuthority(
      document,
      authorityScope,
      "artifact-envelope-test",
    );
    authority.initialize(scene);
    authority.destroy();
    return Y.encodeStateAsUpdate(document);
  } finally {
    document.destroy();
  }
}

function expectCode(
  callback: () => void,
  code: ArtifactEnvelopeError["code"],
): void {
  expect(callback).toThrowError(
    expect.objectContaining<Partial<ArtifactEnvelopeError>>({ code }),
  );
}
