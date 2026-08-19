// @vitest-environment node

import { afterEach, describe, expect, it } from "vitest";
import { WebSocketStatus } from "@hocuspocus/provider";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  excalidrawSceneToCanonical,
} from "../excalidraw/canonicalAuthority";
import {
  createSpikeClient,
  getAuthorizedScope,
  NETWORK_SPIKE_LIMITS,
  type SpikeClient,
  type SpikeServer,
  startSpikeServer,
  waitForSyncedProvider,
  waitUntil,
} from "./hocuspocusHarness";

const resources: Array<SpikeClient | SpikeServer> = [];
const authorityResources: CanonicalExcalidrawAuthority[] = [];

afterEach(async () => {
  for (const authority of authorityResources.splice(0)) {
    authority.destroy();
  }
  for (const resource of resources.splice(0).reverse()) {
    await resource.destroy();
  }
});

describe("Yjs 13 + Hocuspocus 4.6 isolated network evidence", () => {
  it("carries the Excalidraw canonical adapter through concurrent, actor-undo, and offline provider sync", async () => {
    const server = await startSpikeServer();
    resources.push(server);
    const clientA = createSpikeClient({ url: server.url });
    const clientB = createSpikeClient({ url: server.url });
    resources.push(clientA, clientB);

    await Promise.all([
      waitForSyncedProvider(clientA.provider),
      waitForSyncedProvider(clientB.provider),
    ]);

    const scope = {
      documentId: "board-1",
      generation: 1,
      tenantId: "tenant-a",
    };
    const authorityA = new CanonicalExcalidrawAuthority(
      clientA.document,
      scope,
      "teacher-a",
    );
    const authorityB = new CanonicalExcalidrawAuthority(
      clientB.document,
      scope,
      "student-b",
    );
    authorityResources.push(authorityA, authorityB);
    authorityA.initialize(
      excalidrawSceneToCanonical({
        appState: { viewBackgroundColor: "#f8fafc" },
        elements: [canonicalNetworkRectangle("initial-shape", 100)],
        files: {},
        page: { id: "page-1", name: "Bài 1" },
      }),
    );

    await waitUntil(
      () =>
        authorityB.isInitialized() &&
        authorityB.getSemanticHash() === authorityA.getSemanticHash(),
      "canonical bootstrap did not replicate",
    );

    authorityA.putElement(canonicalNetworkRectangle("teacher-note", 300));
    authorityB.putElement(canonicalNetworkRectangle("student-note", 500));
    await waitUntil(
      () => authorityB.getSemanticHash() === authorityA.getSemanticHash(),
      "canonical concurrent edits did not converge",
    );

    expect(authorityA.undo()).toBe(true);
    await waitUntil(
      () => authorityB.getSemanticHash() === authorityA.getSemanticHash(),
      "canonical actor-local undo did not converge",
    );
    expect(
      authorityA.getScene().elements.map((element) => element.id),
    ).not.toContain("teacher-note");
    expect(
      authorityA.getScene().elements.map((element) => element.id),
    ).toContain("student-note");

    await clientB.disconnect();
    authorityA.putElement(canonicalNetworkRectangle("online-change", 700));
    authorityB.putElement(canonicalNetworkRectangle("offline-change", 900));
    await clientB.reconnect();
    await waitUntil(
      () => authorityB.getSemanticHash() === authorityA.getSemanticHash(),
      "canonical two-way offline edits did not reconcile",
    );
    expect(authorityA.getScene().elements.map((element) => element.id)).toEqual(
      expect.arrayContaining(["online-change", "offline-change"]),
    );
  }, 15_000);

  it("converges concurrent edits, recovers offline edits, and restores a binary snapshot", async () => {
    const server = await startSpikeServer();
    resources.push(server);
    const clientA = createSpikeClient({ url: server.url });
    const clientB = createSpikeClient({ url: server.url });
    resources.push(clientA, clientB);

    await Promise.all([
      waitForSyncedProvider(clientA.provider),
      waitForSyncedProvider(clientB.provider),
    ]);

    const boardA = clientA.document.getMap<string>("board");
    const boardB = clientB.document.getMap<string>("board");

    boardA.set("teacher-stroke", "stroke-a");
    boardB.set("student-note", "note-b");

    await waitUntil(
      () =>
        boardA.get("student-note") === "note-b" &&
        boardB.get("teacher-stroke") === "stroke-a",
      "concurrent edits did not converge",
    );

    await clientB.disconnect();
    boardA.set("while-b-offline", "server-side-change");
    boardB.set("offline-draft", "client-side-change");
    await clientB.reconnect();

    await waitUntil(
      () =>
        boardA.get("offline-draft") === "client-side-change" &&
        boardB.get("while-b-offline") === "server-side-change",
      "disconnect/reconnect did not reconcile both directions",
    );

    expect(boardA.toJSON()).toEqual(boardB.toJSON());

    const snapshot = Y.encodeStateAsUpdate(clientA.document);
    const restored = new Y.Doc();
    Y.applyUpdate(restored, snapshot);

    expect(snapshot.byteLength).toBeGreaterThan(0);
    expect(restored.getMap<string>("board").toJSON()).toEqual(boardA.toJSON());
    restored.destroy();
  }, 15_000);

  it("denies invalid/cross-tenant credentials and makes viewers receive-only", async () => {
    const server = await startSpikeServer();
    resources.push(server);
    const editor = createSpikeClient({ url: server.url });
    const viewer = createSpikeClient({ role: "viewer", url: server.url });
    resources.push(editor, viewer);

    await Promise.all([
      waitForSyncedProvider(editor.provider),
      waitForSyncedProvider(viewer.provider),
    ]);

    expect(getAuthorizedScope(viewer.provider)).toBe("readonly");

    const editorBoard = editor.document.getMap<string>("board");
    const viewerBoard = viewer.document.getMap<string>("board");
    viewerBoard.set("forged-viewer-write", "must-not-replicate");

    editorBoard.set("teacher-write", "replicates-to-viewer");
    await waitUntil(
      () => viewerBoard.get("teacher-write") === "replicates-to-viewer",
      "viewer did not receive an authorized editor update",
    );
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(editorBoard.has("forged-viewer-write")).toBe(false);

    const wrongCredential = createSpikeClient({
      token: "fixture-invalid",
      url: server.url,
    });
    const crossTenant = createSpikeClient({
      role: "editor",
      tenantId: "tenant-b",
      url: server.url,
    });
    resources.push(wrongCredential, crossTenant);
    await waitUntil(
      () =>
        wrongCredential.authenticationFailures.length > 0 &&
        crossTenant.authenticationFailures.length > 0,
      "wrong-credential/cross-tenant clients were not rejected",
    );

    expect(server.rejections.authentication).toBeGreaterThanOrEqual(1);
    expect(server.rejections.tenant).toBeGreaterThanOrEqual(1);
    expect(wrongCredential.provider.isAuthenticated).toBe(false);
    expect(crossTenant.provider.isAuthenticated).toBe(false);
  }, 15_000);

  it("accepts bounded updates and rejects Hocuspocus-frame and transport oversize payloads", async () => {
    const server = await startSpikeServer();
    resources.push(server);
    const observer = createSpikeClient({ url: server.url });
    const boundedWriter = createSpikeClient({ url: server.url });
    resources.push(observer, boundedWriter);

    await Promise.all([
      waitForSyncedProvider(observer.provider),
      waitForSyncedProvider(boundedWriter.provider),
    ]);

    const observerBoard = observer.document.getMap<string>("board");
    const boundedBoard = boundedWriter.document.getMap<string>("board");
    const boundedValue = "a".repeat(8 * 1024);
    boundedBoard.set("bounded", boundedValue);

    await waitUntil(
      () => observerBoard.get("bounded") === boundedValue,
      "bounded mutation did not replicate",
    );

    boundedWriter.socket.shouldConnect = false;
    boundedBoard.set(
      "frame-oversize",
      "b".repeat(NETWORK_SPIKE_LIMITS.maxInboundFrameBytes + 8 * 1024),
    );
    await waitUntil(
      () => server.rejections.frameBudget > 0,
      "inbound Hocuspocus frame budget was not enforced",
    );
    expect(observerBoard.has("frame-oversize")).toBe(false);

    const transportWriter = createSpikeClient({ url: server.url });
    resources.push(transportWriter);
    await waitForSyncedProvider(transportWriter.provider);
    transportWriter.socket.shouldConnect = false;
    const frameRejectionsBefore = server.rejections.frameBudget;
    transportWriter.document
      .getMap<string>("board")
      .set(
        "transport-oversize",
        "c".repeat(NETWORK_SPIKE_LIMITS.maxTransportPayloadBytes + 32 * 1024),
      );

    await waitUntil(
      () => transportWriter.socket.status === WebSocketStatus.Disconnected,
      "transport payload ceiling did not close the connection",
    );
    expect(server.rejections.frameBudget).toBe(frameRejectionsBefore);
    expect(observerBoard.has("transport-oversize")).toBe(false);
  }, 15_000);
});

function canonicalNetworkRectangle(id: string, x: number) {
  return {
    backgroundColor: "#a5d8ff",
    boundElements: null,
    fillStyle: "solid",
    frameId: null,
    groupIds: [],
    height: 100,
    id,
    index: null,
    isDeleted: false,
    link: null,
    locked: false,
    opacity: 100,
    roughness: 1,
    roundness: null,
    seed: x + 1,
    strokeColor: "#1c3f60",
    strokeStyle: "solid",
    strokeWidth: 2,
    type: "rectangle",
    updated: 1_786_000_000_000,
    version: 1,
    versionNonce: x + 2,
    width: 160,
    x,
    y: 120,
  };
}
