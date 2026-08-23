import { afterEach, describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  type CanonicalAuthorityScope,
  type CanonicalElementV1,
} from "./canonicalAuthority.js";

const scope: CanonicalAuthorityScope = {
  documentId: "board-p512",
  generation: 12,
  tenantId: "tenant-p512",
};

interface Peer {
  actorId: string;
  authority: CanonicalExcalidrawAuthority;
  document: Y.Doc;
}

const peers: Peer[] = [];

afterEach(() => {
  for (const peer of peers.splice(0)) {
    peer.authority.destroy();
    peer.document.destroy();
  }
});

describe("P5-COLLAB-12 canonical convergence and actor-local history", () => {
  it("converges 25 offline cycles after duplicate and out-of-order delivery, undo/redo, and restore", () => {
    const teacher = createPeer("teacher-a");
    teacher.authority.initialize({
      elements: [],
      files: {},
      page: {
        backgroundColor: "#ffffff",
        id: "page-p512",
        name: "P5-12",
      },
      schemaVersion: 1,
    });
    const student = createPeer(
      "student-b",
      teacher.authority.encodeProviderState(),
    );
    const assistant = createPeer(
      "assistant-c",
      teacher.authority.encodeProviderState(),
    );
    const activePeers = [teacher, student, assistant];

    for (let cycle = 0; cycle < 25; cycle += 1) {
      expectConverged(activePeers);
      const commonWatermark = teacher.authority.encodeCausalWatermark();

      for (const [index, peer] of activePeers.entries()) {
        peer.authority.putElement(
          rectangle(`${peer.actorId}-${cycle}`, cycle * 100 + index * 20),
        );
      }

      const offlineUpdates = activePeers.map((peer) =>
        Y.encodeStateAsUpdate(peer.document, commonWatermark),
      );
      for (const [targetIndex, target] of activePeers.entries()) {
        const deliveryOrder = [
          (targetIndex + 2) % activePeers.length,
          targetIndex,
          (targetIndex + 1) % activePeers.length,
        ];
        for (const sourceIndex of deliveryOrder) {
          target.authority.applyRemoteUpdate(
            requireUpdate(offlineUpdates, sourceIndex),
          );
        }
        target.authority.applyRemoteUpdate(
          requireUpdate(offlineUpdates, deliveryOrder[0] ?? -1),
        );
      }
      expectConverged(activePeers);

      const historyPeer = activePeers[cycle % activePeers.length] as Peer;
      const localElementId = `${historyPeer.actorId}-${cycle}`;
      const remoteElementId = `${
        activePeers[(cycle + 1) % activePeers.length]?.actorId
      }-${cycle}`;
      expect(historyPeer.authority.undo()).toBe(true);
      expect(sceneIds(historyPeer)).not.toContain(localElementId);
      expect(sceneIds(historyPeer)).toContain(remoteElementId);

      if (cycle % 2 === 0) {
        expect(historyPeer.authority.redo()).toBe(true);
        expect(sceneIds(historyPeer)).toContain(localElementId);
      }

      for (const target of activePeers) {
        if (target === historyPeer) continue;
        const historyUpdate = Y.encodeStateAsUpdate(
          historyPeer.document,
          Y.encodeStateVector(target.document),
        );
        target.authority.applyRemoteUpdate(historyUpdate);
        target.authority.applyRemoteUpdate(historyUpdate);
      }
      expectConverged(activePeers);
    }

    const restored = createPeer(
      "restore-reader",
      teacher.authority.encodeProviderState(),
    );
    expect(restored.authority.getSemanticHash()).toBe(
      teacher.authority.getSemanticHash(),
    );
    expect(restored.authority.encodeCausalWatermark()).toEqual(
      teacher.authority.encodeCausalWatermark(),
    );
  }, 15_000);
});

function createPeer(actorId: string, initialState?: Uint8Array): Peer {
  const document = new Y.Doc();
  const authority = new CanonicalExcalidrawAuthority(document, scope, actorId);
  const peer = { actorId, authority, document };
  peers.push(peer);
  if (initialState !== undefined) authority.applyRemoteUpdate(initialState);
  return peer;
}

function rectangle(id: string, x: number): CanonicalElementV1 {
  return {
    height: 80,
    id,
    type: "rectangle",
    width: 120,
    x,
    y: 120,
  };
}

function sceneIds(peer: Peer): string[] {
  return peer.authority.getScene().elements.map((element) => element.id);
}

function expectConverged(candidates: Peer[]): void {
  const expectedHash = candidates[0]?.authority.getSemanticHash();
  expect(expectedHash).toBeDefined();
  for (const candidate of candidates) {
    expect(candidate.authority.getSemanticHash()).toBe(expectedHash);
  }
}

function requireUpdate(updates: Uint8Array[], index: number): Uint8Array {
  const update = updates[index];
  if (update === undefined) throw new Error("p512_fixture_update_missing");
  return update;
}
