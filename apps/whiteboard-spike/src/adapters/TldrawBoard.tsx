import { useEffect, useRef } from "react";
import {
  Tldraw,
  createShapeId,
  getSnapshot,
  loadSnapshot,
  toRichText,
  type Editor,
  type TLStoreSnapshot,
} from "tldraw";
import "tldraw/tldraw.css";
import type { BoardController, BoardFixtureShape } from "../model";

interface TldrawBoardProps {
  fixtures: BoardFixtureShape[];
  readOnly: boolean;
  onReady: (controller: BoardController) => void;
}

const colors = ["blue", "green", "orange", "violet", "red"] as const;

export default function TldrawBoard({
  fixtures,
  readOnly,
  onReady,
}: TldrawBoardProps) {
  const editorRef = useRef<Editor | null>(null);

  useEffect(() => {
    editorRef.current?.updateInstanceState({ isReadonly: readOnly });
  }, [readOnly]);

  return (
    <div className="engine-canvas" data-testid="tldraw-canvas">
      <Tldraw
        onMount={(editor) => {
          editorRef.current = editor;
          editor.updateInstanceState({ isReadonly: false });
          editor.createShapes(
            fixtures.map((shape) => ({
              id: createShapeId(shape.id),
              type: "geo" as const,
              x: shape.x,
              y: shape.y,
              props: {
                geo: "rectangle" as const,
                w: shape.width,
                h: shape.height,
                color: colors[shape.colorIndex] ?? "blue",
                fill: "semi" as const,
                richText: toRichText(shape.label),
              },
            })),
          );
          editor.zoomToFit();
          editor.updateInstanceState({ isReadonly: readOnly });

          onReady({
            getShapeCount: () => editor.getCurrentPageShapes().length,
            exportPayload: () => getSnapshot(editor.store),
            restorePayload: (payload) => {
              if (!isTldrawSnapshot(payload)) {
                throw new Error("Payload tldraw không hợp lệ.");
              }
              loadSnapshot(editor.store, payload);
            },
          });
        }}
      />
    </div>
  );
}

function isTldrawSnapshot(value: unknown): value is TLStoreSnapshot {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  return "document" in value && "session" in value;
}
