import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSync } from "@tldraw/sync";
import {
  Tldraw,
  createShapeId,
  type Editor,
  type TLAssetStore,
  type TLStore,
} from "tldraw";
import {
  GrantProtocolSocket,
  type GateCapability,
} from "./GrantProtocolSocket";
import type { WhiteboardGateApi } from "./CollabApp";

const GATE_URL = "http://127.0.0.1:4179";

const noUploadAssets: TLAssetStore = {
  async upload() {
    throw new Error("asset_upload_disabled_in_isolated_gate");
  },
  resolve(asset) {
    return asset.props.src;
  },
};

interface LoadGateApi {
  connectedCount(): number;
  writerReady(): boolean;
  shapeCounts(): number[];
  createFixture(count: number): number;
}

declare global {
  interface Window {
    __TUTORHUB_WHITEBOARD_LOAD_GATE__?: LoadGateApi;
  }
}

export function LoadHarness() {
  const fixture = useMemo(readLoadFixture, []);
  const storesRef = useRef<Array<TLStore | null>>(
    Array.from({ length: fixture.clients }, () => null),
  );
  const statusesRef = useRef<string[]>(
    Array.from({ length: fixture.clients }, () => "loading"),
  );
  const editorRef = useRef<Editor | null>(null);
  const [version, setVersion] = useState(0);
  const handleStore = useCallback(
    (index: number, store: TLStore | null, status: string) => {
      storesRef.current[index] = store;
      statusesRef.current[index] = status;
      setVersion((current) => current + 1);
    },
    [],
  );
  const handleWriter = useCallback((editor: Editor) => {
    editorRef.current = editor;
    setVersion((current) => current + 1);
  }, []);

  useEffect(() => {
    const api: LoadGateApi = {
      connectedCount: () =>
        statusesRef.current.filter((status) => status === "synced-remote")
          .length,
      writerReady: () => editorRef.current !== null,
      shapeCounts: () =>
        storesRef.current.map((store) => {
          if (!store) return -1;
          return Object.values(store.serialize("document")).filter(
            (record) => record.typeName === "shape",
          ).length;
        }),
      createFixture: (count) => {
        const gate = editorRef.current
          ? createWriterApi(editorRef.current)
          : null;
        if (!gate) throw new Error("writer_not_ready");
        return gate.createFixture(count);
      },
    };
    window.__TUTORHUB_WHITEBOARD_LOAD_GATE__ = api;
    return () => {
      if (window.__TUTORHUB_WHITEBOARD_LOAD_GATE__ === api) {
        delete window.__TUTORHUB_WHITEBOARD_LOAD_GATE__;
      }
    };
  }, []);

  return (
    <main className="load-shell">
      <h1>P5 tldraw official-sync load harness</h1>
      <p>Isolated local profile: {fixture.clients} browser clients.</p>
      <output data-testid="load-connected">
        {
          statusesRef.current.filter((status) => status === "synced-remote")
            .length
        }
        /{fixture.clients}
      </output>
      {Array.from({ length: fixture.clients }, (_, index) => (
        <SyncProbe
          key={index}
          index={index}
          tenantId={fixture.tenantId}
          documentId={fixture.documentId}
          onStore={handleStore}
          onWriter={handleWriter}
        />
      ))}
      <span hidden>{version}</span>
    </main>
  );
}

function SyncProbe({
  index,
  tenantId,
  documentId,
  onStore,
  onWriter,
}: {
  index: number;
  tenantId: string;
  documentId: string;
  onStore: (index: number, store: TLStore | null, status: string) => void;
  onWriter: (editor: Editor) => void;
}) {
  const capability: GateCapability = index === 0 ? "edit" : "view";
  const actorId = `${tenantId}:${index === 0 ? "editor-load" : `load-${index}`}`;
  const connect = useCallback(
    ({ sessionId, storeId }: { sessionId: string; storeId: string }) =>
      new GrantProtocolSocket({
        baseUrl: GATE_URL,
        tenantId,
        documentId,
        actorId,
        requestedCapability: capability,
        sessionId: `${sessionId}:load-${index}`,
        storeId,
      }),
    [actorId, capability, documentId, index, tenantId],
  );
  const remoteStore = useSync({ connect, assets: noUploadAssets });
  const synchronizedStore =
    remoteStore.status === "synced-remote" ? remoteStore.store : null;

  useEffect(() => {
    if (synchronizedStore) {
      onStore(index, synchronizedStore, remoteStore.status);
      return synchronizedStore.listen(
        () => onStore(index, synchronizedStore, remoteStore.status),
        { scope: "document" },
      );
    }
    onStore(index, null, remoteStore.status);
  }, [index, onStore, remoteStore.status, synchronizedStore]);

  if (index !== 0) {
    return <span hidden data-probe={index} data-status={remoteStore.status} />;
  }

  return (
    <div className="load-writer-canvas" aria-hidden="true">
      <Tldraw store={remoteStore} onMount={onWriter} />
    </div>
  );
}

function createWriterApi(
  editor: Editor,
): Pick<WhiteboardGateApi, "createFixture"> {
  return {
    createFixture(count) {
      if (!Number.isInteger(count) || count < 1 || count > 2_000) {
        throw new Error("fixture_count_invalid");
      }
      const existingIds = new Set(
        editor.getCurrentPageShapes().map((shape) => shape.id),
      );
      const shapes = [];
      for (let index = 0; index < count; index += 1) {
        const id = createShapeId(`load-fixture-${index}`);
        if (existingIds.has(id)) continue;
        shapes.push({
          id,
          type: "geo" as const,
          x: (index % 40) * 120,
          y: Math.floor(index / 40) * 84,
          props: { geo: "rectangle" as const, w: 104, h: 68 },
        });
      }
      editor.createShapes(shapes);
      return shapes.length;
    },
  };
}

function readLoadFixture() {
  const params = new URLSearchParams(window.location.search);
  const clientsValue = Number(params.get("clients") ?? 2);
  const clients = clientsValue === 10 || clientsValue === 50 ? clientsValue : 2;
  const tenantId = params.get("tenant") ?? "tenant-gate";
  const documentId = params.get("document") ?? "document-load";
  if (!/^[a-z0-9][a-z0-9:_-]{2,95}$/.test(tenantId)) {
    throw new Error("tenant_invalid");
  }
  if (!/^[a-z0-9][a-z0-9:_-]{2,95}$/.test(documentId)) {
    throw new Error("document_invalid");
  }
  return { clients, tenantId, documentId };
}
