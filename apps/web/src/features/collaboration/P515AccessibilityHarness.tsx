import type {
  CanonicalElementV1,
  CanonicalExcalidrawSceneV1,
  CollaborationConnectionStatus,
} from "@tutorhub/collaboration-client";
import { semanticHash } from "@tutorhub/collaboration-client";
import {
  Button,
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerTitle,
  DrawerTrigger,
  IconButton,
} from "@tutorhub/ui";
import { PenTool } from "lucide-react";
import { useMemo, useState } from "react";
import {
  CanonicalExcalidrawCanvas,
  type WhiteboardCanvasAuthority,
} from "./LazyWhiteboardEngine";

const LOCAL_HOSTS = new Set(["127.0.0.1", "localhost", "::1"]);

export function P515AccessibilityHarness() {
  const authority = useMemo(() => createHarnessAuthority(), []);
  const [connectionStatus, setConnectionStatus] =
    useState<CollaborationConnectionStatus>("connected");
  const [open, setOpen] = useState(false);

  if (!import.meta.env.DEV || !LOCAL_HOSTS.has(globalThis.location.hostname)) {
    return (
      <main>
        <h1>P5-COLLAB-15 harness unavailable</h1>
        <p>This local-only accessibility harness is disabled.</p>
      </main>
    );
  }

  return (
    <main className="p515-harness" data-theme="dark">
      <section aria-labelledby="p515-title" className="p515-harness-card">
        <p className="eyebrow">Local-only production component harness</p>
        <h1 id="p515-title">P5-COLLAB-15 accessibility browser matrix</h1>
        <p>
          This page mounts TutorHub&apos;s production Drawer and canonical
          Excalidraw canvas. It never requests a collaboration grant and does
          not connect to Neon, B2, Render, or a shared environment.
        </p>
      </section>

      <div
        aria-label="Classroom tools"
        className="p515-harness-toolbar"
        role="toolbar"
      >
        <Drawer onOpenChange={setOpen} open={open}>
          <DrawerTrigger asChild>
            <IconButton
              aria-expanded={open}
              data-media-control="tool-whiteboard"
              id="p515-whiteboard-trigger"
              label="Open classroom whiteboard"
              variant="secondary"
            >
              <PenTool />
            </IconButton>
          </DrawerTrigger>
          <DrawerContent
            className="media-p506-tool-drawer media-p506-tool-whiteboard"
            closeLabel="Close classroom whiteboard"
            data-theme="dark"
          >
            <DrawerTitle>Classroom whiteboard</DrawerTitle>
            <div
              aria-label="Local connection announcement controls"
              className="p515-harness-actions"
              role="group"
            >
              <Button
                onClick={() => setConnectionStatus("reconnecting")}
                variant="secondary"
              >
                Simulate reconnect
              </Button>
              <Button
                onClick={() => setConnectionStatus("connected")}
                variant="secondary"
              >
                Restore connection
              </Button>
              <Button
                onClick={() => setConnectionStatus("failed")}
                variant="secondary"
              >
                Simulate connection failure
              </Button>
            </div>
            <CanonicalExcalidrawCanvas
              authority={authority}
              connectionStatus={connectionStatus}
              readOnly={false}
            />
            <DrawerClose asChild>
              <Button variant="secondary">Close classroom whiteboard</Button>
            </DrawerClose>
          </DrawerContent>
        </Drawer>
      </div>
    </main>
  );
}

function createHarnessAuthority(): WhiteboardCanvasAuthority {
  let scene: CanonicalExcalidrawSceneV1 = {
    elements: Array.from({ length: 55 }, (_, index) =>
      createRectangle(index + 1),
    ),
    files: {},
    page: {
      backgroundColor: "#ffffff",
      id: "p515-page-1",
      name: "Accessibility fixture",
    },
    schemaVersion: 1,
  };
  const listeners = new Set<(next: CanonicalExcalidrawSceneV1) => void>();
  return {
    getProjection: () => ({
      appState: { viewBackgroundColor: scene.page.backgroundColor },
      elements: scene.elements,
      files: scene.files,
      page: { id: scene.page.id, name: scene.page.name },
    }),
    getScene: () => scene,
    getSemanticHash: () => semanticHash(scene),
    redo: () => false,
    replaceScene: (next: CanonicalExcalidrawSceneV1) => {
      scene = next;
      listeners.forEach((listener) => listener(scene));
    },
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    undo: () => false,
  };
}

function createRectangle(sequence: number): CanonicalElementV1 {
  const x = 40 + ((sequence - 1) % 10) * 130;
  const y = 60 + Math.floor((sequence - 1) / 10) * 100;
  return {
    angle: 0,
    backgroundColor: sequence % 2 === 0 ? "#b2f2bb" : "#ffec99",
    boundElements: null,
    fillStyle: "solid",
    frameId: null,
    groupIds: [],
    height: 70,
    id: `p515-shape-${sequence}`,
    index: null,
    isDeleted: false,
    link: null,
    locked: false,
    opacity: 100,
    roughness: 1,
    roundness: null,
    seed: sequence + 10_000,
    strokeColor: "#1c3f60",
    strokeStyle: "solid",
    strokeWidth: 2,
    text: `Lesson shape ${sequence}`,
    type: "rectangle",
    updated: 1_788_000_000_000 + sequence,
    version: 1,
    versionNonce: sequence + 20_000,
    width: 110,
    x,
    y,
  };
}
