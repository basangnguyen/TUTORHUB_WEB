import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSync } from "@tldraw/sync";
import {
  Tldraw,
  createShapeId,
  toRichText,
  type Editor,
  type TLAssetStore,
  type TLShape,
} from "tldraw";
import "tldraw/tldraw.css";
import {
  GrantProtocolSocket,
  type GateCapability,
} from "./GrantProtocolSocket";

const GATE_URL = "http://127.0.0.1:4179";

const noUploadAssets: TLAssetStore = {
  async upload() {
    throw new Error("asset_upload_disabled_in_isolated_gate");
  },
  resolve(asset) {
    return asset.props.src;
  },
};

interface CollabFixture {
  tenantId: string;
  documentId: string;
  actorId: string;
  capability: GateCapability;
  seed: number;
}

interface DocumentEvidence {
  digest: string;
  shapeCount: number;
  shapes: Array<Record<string, unknown>>;
}

export interface WhiteboardGateApi {
  connectionStatus(): string;
  createRect(id: string, x?: number, y?: number, label?: string): string;
  createFixture(count: number): number;
  moveRect(id: string, x: number, y: number): void;
  deleteShape(id: string): void;
  undo(): void;
  redo(): void;
  forceCreateRect(id: string): string;
  evidence(): DocumentEvidence;
  goOffline(): void;
  goOnline(): void;
  restart(): void;
}

declare global {
  interface Window {
    __TUTORHUB_WHITEBOARD_GATE__?: WhiteboardGateApi;
  }
}

export function CollabApp() {
  const fixture = useMemo(readFixture, []);
  const socketRef = useRef<GrantProtocolSocket | null>(null);
  const [socketStatus, setSocketStatus] = useState("offline");
  const [editor, setEditor] = useState<Editor | null>(null);

  const connect = useCallback(
    ({ sessionId, storeId }: { sessionId: string; storeId: string }) => {
      const socket = new GrantProtocolSocket({
        baseUrl: GATE_URL,
        ...fixture,
        requestedCapability: fixture.capability,
        sessionId,
        storeId,
      });
      socketRef.current = socket;
      socket.onStatusChange((event) => setSocketStatus(event.status));
      return socket;
    },
    [fixture],
  );

  const remoteStore = useSync({
    connect,
    assets: noUploadAssets,
  });

  useEffect(() => {
    if (!editor) return;
    const api = createGateApi(editor, socketRef);
    window.__TUTORHUB_WHITEBOARD_GATE__ = api;
    return () => {
      if (window.__TUTORHUB_WHITEBOARD_GATE__ === api) {
        delete window.__TUTORHUB_WHITEBOARD_GATE__;
      }
    };
  }, [editor]);

  return (
    <main className="collab-shell">
      <header className="collab-header">
        <div>
          <p className="eyebrow">P5-COLLAB-01 · isolated automated gate</p>
          <h1>tldraw official sync acceptance</h1>
        </div>
        <dl className="gate-identifiers">
          <div>
            <dt>Capability</dt>
            <dd>{fixture.capability}</dd>
          </div>
          <div>
            <dt>Connection</dt>
            <dd data-testid="socket-status">{socketStatus}</dd>
          </div>
          <div>
            <dt>Store</dt>
            <dd data-testid="store-status">{remoteStore.status}</dd>
          </div>
        </dl>
      </header>
      <p className="gate-note" role="status" aria-live="polite">
        Grant ngắn hạn không được đưa vào URL, DOM, localStorage hoặc
        sessionStorage.
      </p>
      <section className="collab-board" aria-label="Bảng cộng tác tldraw">
        <Tldraw
          store={remoteStore}
          onMount={(mountedEditor) => {
            setEditor(mountedEditor);
            if (
              fixture.seed > 0 &&
              fixture.capability !== "view" &&
              mountedEditor.getCurrentPageShapes().length === 0
            ) {
              createFixtureShapes(mountedEditor, fixture.seed);
            }
          }}
        />
      </section>
      <output className="gate-evidence" data-testid="shape-count">
        {editor?.getCurrentPageShapes().length ?? 0}
      </output>
    </main>
  );
}

function createGateApi(
  editor: Editor,
  socketRef: React.RefObject<GrantProtocolSocket | null>,
): WhiteboardGateApi {
  return {
    connectionStatus: () => socketRef.current?.connectionStatus ?? "offline",
    createRect(id, x = 80, y = 80, label = id) {
      const shapeId = createShapeId(normalizeExternalId(id));
      editor.createShape({
        id: shapeId,
        type: "geo",
        x,
        y,
        props: {
          geo: "rectangle",
          w: 160,
          h: 96,
          fill: "semi",
          color: "blue",
          richText: toRichText(label.slice(0, 120)),
        },
      });
      return shapeId;
    },
    createFixture(count) {
      return createFixtureShapes(editor, clampFixtureCount(count));
    },
    moveRect(id, x, y) {
      editor.updateShape({
        id: createShapeId(normalizeExternalId(id)),
        type: "geo",
        x,
        y,
      });
    },
    deleteShape(id) {
      editor.deleteShape(createShapeId(normalizeExternalId(id)));
    },
    undo: () => editor.undo(),
    redo: () => editor.redo(),
    forceCreateRect(id) {
      const wasReadonly = editor.getInstanceState().isReadonly;
      editor.updateInstanceState({ isReadonly: false });
      const shapeId = createShapeId(normalizeExternalId(id));
      editor.createShape({
        id: shapeId,
        type: "geo",
        x: 32,
        y: 32,
        props: { geo: "rectangle", w: 80, h: 80, fill: "solid" },
      });
      editor.updateInstanceState({ isReadonly: wasReadonly });
      return shapeId;
    },
    evidence: () => documentEvidence(editor.getCurrentPageShapes()),
    goOffline: () => socketRef.current?.goOffline(),
    goOnline: () => socketRef.current?.goOnline(),
    restart: () => socketRef.current?.restart(),
  };
}

function createFixtureShapes(editor: Editor, count: number): number {
  const boundedCount = clampFixtureCount(count);
  const existingIds = new Set(
    editor.getCurrentPageShapes().map((shape) => shape.id),
  );
  const shapes = [];
  for (let index = 0; index < boundedCount; index += 1) {
    const id = createShapeId(`fixture-${index}`);
    if (existingIds.has(id)) continue;
    shapes.push({
      id,
      type: "geo" as const,
      x: (index % 40) * 120,
      y: Math.floor(index / 40) * 84,
      props: {
        geo: "rectangle" as const,
        w: 104,
        h: 68,
        fill: "semi" as const,
        color: (index % 2 === 0 ? "blue" : "green") as "blue" | "green",
        richText: toRichText(`Fixture ${index}`),
      },
    });
  }
  editor.createShapes(shapes);
  return shapes.length;
}

function documentEvidence(shapes: TLShape[]): DocumentEvidence {
  const normalized = shapes
    .map((shape) => ({
      id: shape.id,
      type: shape.type,
      parentId: shape.parentId,
      index: shape.index,
      x: shape.x,
      y: shape.y,
      rotation: shape.rotation,
      opacity: shape.opacity,
      props: shape.props,
    }))
    .sort((left, right) => left.id.localeCompare(right.id));
  const serialized = stableStringify(normalized);
  return {
    digest: fnv1a(serialized),
    shapeCount: normalized.length,
    shapes: normalized,
  };
}

function stableStringify(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(stableStringify).join(",")}]`;
  }
  if (value && typeof value === "object") {
    return `{${Object.entries(value)
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([key, entry]) => `${JSON.stringify(key)}:${stableStringify(entry)}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

function fnv1a(value: string): string {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}

function normalizeExternalId(value: string): string {
  const normalized = value
    .toLowerCase()
    .replace(/[^a-z0-9_-]/g, "-")
    .slice(0, 80);
  if (!normalized) throw new Error("shape_id_invalid");
  return normalized;
}

function clampFixtureCount(value: number): number {
  if (!Number.isInteger(value) || value < 1 || value > 2_000) {
    throw new Error("fixture_count_invalid");
  }
  return value;
}

function readFixture(): CollabFixture {
  const params = new URLSearchParams(window.location.search);
  const tenantId = readBoundedParam(params, "tenant", "tenant-gate");
  const documentId = readBoundedParam(params, "document", "document-gate");
  const actorId = readBoundedParam(
    params,
    "actor",
    `${tenantId}:editor-browser`,
  );
  const capabilityValue = params.get("capability");
  const capability: GateCapability =
    capabilityValue === "view" || capabilityValue === "present"
      ? capabilityValue
      : "edit";
  const seedValue = Number(params.get("seed") ?? 0);
  const seed = seedValue === 500 || seedValue === 2_000 ? seedValue : 0;
  return { tenantId, documentId, actorId, capability, seed };
}

function readBoundedParam(
  params: URLSearchParams,
  key: string,
  fallback: string,
): string {
  const value = params.get(key) ?? fallback;
  if (!/^[a-z0-9][a-z0-9:_-]{2,95}$/.test(value)) {
    throw new Error(`${key}_invalid`);
  }
  return value;
}
