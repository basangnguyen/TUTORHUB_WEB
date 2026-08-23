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
import { ControlPlaneError, HttpControlPlane } from "./controlPlane.js";
import {
  RuntimeReadinessCoordinator,
  RuntimeReadinessError,
} from "./runtimeReadinessCoordinator.js";

const scope = {
  documentId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  generation: 1,
  tenantId: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
};
const fixtureCredential = (label: string) =>
  ["p516", "fixture", label, "x".repeat(32)].join("-");
const previousBindingKey = {
  id: "p5-collab-16-binding-r1",
  secret: fixtureCredential("binding-r1"),
};
const currentBindingKey = {
  id: "p5-collab-16-binding-r2",
  secret: fixtureCredential("binding-r2"),
};

describe("P5-COLLAB-16 failure, outage and provider-exit gates", () => {
  it("times out an unavailable control authority and fails a new room closed", async () => {
    const fetcher: typeof fetch = async (_input, init) =>
      await new Promise<Response>((_resolve, reject) => {
        const signal = init?.signal;
        if (signal?.aborted) {
          reject(new Error("aborted"));
          return;
        }
        signal?.addEventListener("abort", () => reject(new Error("aborted")), {
          once: true,
        });
      });
    const control = new HttpControlPlane(
      "https://control.invalid",
      fixtureCredential("service-token"),
      5,
      fetcher,
    );

    await expect(
      control.exchangeGrant({
        documentName: `wb_${"a".repeat(22)}`,
        grant: "p5-collab-16-one-time-test-grant",
        origin: "https://app.example.test",
      }),
    ).rejects.toEqual(new ControlPlaneError("control_plane_unavailable"));
  });

  it("keeps an existing room readable before forcing all admission off", async () => {
    const readiness = new RuntimeReadinessCoordinator(() => undefined);
    readiness.activate();
    await readiness.refreshDependencies(async () => ({
      authorityGuard: true,
      controlPlane: true,
      mode: "enabled",
      persistence: true,
    }));
    expect(readiness.assertAdmissionReady({ write: true })).toBe("enabled");

    await readiness.refreshDependencies(async () => ({
      authorityGuard: true,
      controlPlane: true,
      mode: "read_only",
      persistence: true,
    }));
    expect(readiness.assertAdmissionReady()).toBe("read_only");
    expect(() => readiness.assertAdmissionReady({ write: true })).toThrow(
      new RuntimeReadinessError("write_disabled"),
    );

    await readiness.refreshDependencies(async () => ({
      authorityGuard: true,
      controlPlane: true,
      mode: "off",
      persistence: true,
    }));
    expect(readiness.readiness()).toMatchObject({
      authorityMode: "off",
      ready: false,
      reason: "runtime_off",
    });
    expect(() => readiness.assertAdmissionReady()).toThrow(
      new RuntimeReadinessError("runtime_not_ready"),
    );
  });

  it("rotates binding keys and restores a portable scene outside the old provider", () => {
    const scene = createScene();
    const providerState = createProviderState(scope.generation, scene);
    const lastGood = createArtifactEnvelope(
      scope,
      providerState,
      previousBindingKey,
      "2026-08-23T00:00:00.000Z",
    );

    const verifiedLastGood = verifyArtifactEnvelope(
      lastGood.bytes,
      scope,
      new Map([
        [currentBindingKey.id, currentBindingKey.secret],
        [previousBindingKey.id, previousBindingKey.secret],
      ]),
    );
    expect(verifiedLastGood.semanticHash).toBe(semanticHash(scene));
    expect(() =>
      verifyArtifactEnvelope(
        lastGood.bytes,
        scope,
        new Map([[currentBindingKey.id, currentBindingKey.secret]]),
      ),
    ).toThrow(new ArtifactEnvelopeError("artifact_binding_invalid"));

    const nextScope = { ...scope, generation: scope.generation + 1 };
    const providerIndependentState = createProviderState(
      nextScope.generation,
      verifiedLastGood.scene,
    );
    const providerExitArtifact = createArtifactEnvelope(
      nextScope,
      providerIndependentState,
      currentBindingKey,
      "2026-08-23T00:05:00.000Z",
    );
    const verifiedExit = verifyArtifactEnvelope(
      providerExitArtifact.bytes,
      nextScope,
      new Map([[currentBindingKey.id, currentBindingKey.secret]]),
    );

    expect(verifiedExit.semanticHash).toBe(verifiedLastGood.semanticHash);
    expect(semanticHash(verifiedExit.scene)).toBe(semanticHash(scene));
  });
});

function createScene(): CanonicalExcalidrawSceneV1 {
  return {
    elements: [
      {
        height: 44,
        id: "p516-provider-exit-text",
        text: "Portable provider-exit checkpoint",
        type: "text",
        width: 320,
        x: 64,
        y: 96,
      },
    ],
    files: {},
    page: {
      backgroundColor: "#ffffff",
      id: "page-1",
      name: "P5-COLLAB-16",
    },
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
      { ...scope, generation },
      `p516-provider-${generation}`,
    );
    authority.initialize(scene);
    authority.destroy();
    return Y.encodeStateAsUpdate(document);
  } finally {
    document.destroy();
  }
}
