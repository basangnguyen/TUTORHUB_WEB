import { useEffect, useMemo, useRef, useState } from "react";
import {
  Excalidraw,
  convertToExcalidrawElements,
} from "@excalidraw/excalidraw";
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import "@excalidraw/excalidraw/index.css";
import {
  canonicalSceneToExcalidraw,
  excalidrawSceneToCanonical,
  semanticHash,
  type CanonicalExcalidrawAuthority,
} from "../excalidraw/canonicalAuthority";
import type { BoardController, BoardFixtureShape } from "../model";

interface ExcalidrawBoardProps {
  authority?: CanonicalExcalidrawAuthority;
  fixtures: BoardFixtureShape[];
  readOnly: boolean;
  onCanonicalStatus?: (status: string) => void;
  onReady: (controller: BoardController) => void;
}

const colors = ["#a5d8ff", "#b2f2bb", "#ffec99", "#d0bfff", "#ffc9c9"];

export default function ExcalidrawBoard({
  authority,
  fixtures,
  readOnly,
  onCanonicalStatus,
  onReady,
}: ExcalidrawBoardProps) {
  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
  const [renderedElementCount, setRenderedElementCount] = useState(0);
  const engineRootRef = useRef<HTMLDivElement>(null);
  const applyingCanonicalRef = useRef(authority !== undefined);
  const canonicalBaselineRef = useRef(authority?.getScene());
  const programmaticProjectionPendingRef = useRef(authority !== undefined);
  const localMutationPendingRef = useRef(false);
  const deferredCanonicalRef = useRef(false);
  const applyCanonicalSceneRef = useRef<() => void>(() => undefined);
  const onReadyRef = useRef(onReady);
  onReadyRef.current = onReady;
  const initialProjection = useMemo(
    () => authority?.getProjection(),
    [authority],
  );
  const initialElements = useMemo(() => {
    if (initialProjection) {
      return initialProjection.elements as unknown as readonly ExcalidrawElement[];
    }
    return convertToExcalidrawElements(
      fixtures.map((shape) => ({
        type: "rectangle" as const,
        id: shape.id,
        x: shape.x,
        y: shape.y,
        width: shape.width,
        height: shape.height,
        backgroundColor: colors[shape.colorIndex] ?? colors[0],
        fillStyle: "solid" as const,
        strokeColor: "#1c3f60",
        label: { text: shape.label },
      })),
    );
  }, [fixtures, initialProjection]);

  useEffect(() => {
    const root = engineRootRef.current;
    if (!root) return;
    const repairUpstreamAccessibility = () => {
      const trigger = root.querySelector<HTMLButtonElement>(
        ".main-menu-trigger:not([aria-label])",
      );
      if (trigger) {
        trigger.setAttribute("aria-label", "Mở menu bảng vẽ");
        trigger.dataset.accessibilityPatch =
          "excalidraw-0.18.1-mobile-menu-name";
      }
      const nestedFooter = root.querySelector<HTMLElement>(
        "footer:not([data-accessibility-patch])",
      );
      if (nestedFooter) {
        nestedFooter.setAttribute("role", "group");
        nestedFooter.setAttribute("aria-label", "Điều khiển bảng vẽ");
        nestedFooter.dataset.accessibilityPatch =
          "excalidraw-0.18.1-nested-contentinfo";
      }
    };
    repairUpstreamAccessibility();
    const observer = new MutationObserver(repairUpstreamAccessibility);
    observer.observe(root, { childList: true, subtree: true });
    const markTrustedUserInteraction = (event: Event) => {
      if (event.isTrusted) {
        programmaticProjectionPendingRef.current = false;
      }
    };
    root.addEventListener("pointerdown", markTrustedUserInteraction, true);
    root.addEventListener("keydown", markTrustedUserInteraction, true);
    return () => {
      observer.disconnect();
      root.removeEventListener("pointerdown", markTrustedUserInteraction, true);
      root.removeEventListener("keydown", markTrustedUserInteraction, true);
    };
  }, []);

  useEffect(() => {
    if (!api) {
      return;
    }

    let animationFrame = 0;
    let cancelled = false;
    const reportWhenInitialized = () => {
      if (cancelled) {
        return;
      }
      if (api.getSceneElements().length === initialElements.length) {
        setRenderedElementCount(api.getSceneElements().length);
        window.requestAnimationFrame(() => {
          window.requestAnimationFrame(() => {
            applyingCanonicalRef.current = false;
            api.history.clear();
          });
        });
        onReadyRef.current(
          createController(
            api,
            (payload) => {
              if (!authority) {
                return payload;
              }
              programmaticProjectionPendingRef.current = true;
              localMutationPendingRef.current = true;
              applyingCanonicalRef.current = true;
              try {
                const page = authority.getProjection().page;
                const canonical = excalidrawSceneToCanonical({
                  appState: {
                    viewBackgroundColor: api.getAppState().viewBackgroundColor,
                  },
                  elements: payload.filter((element) => !element.isDeleted),
                  files: api.getFiles(),
                  page,
                });
                authority.applySceneDelta(
                  canonicalBaselineRef.current ?? authority.getScene(),
                  canonical,
                );
                const current = authority.getScene();
                canonicalBaselineRef.current = current;
                return canonicalSceneToExcalidraw(current)
                  .elements as unknown as readonly ExcalidrawElement[];
              } catch (error) {
                localMutationPendingRef.current = false;
                applyingCanonicalRef.current = false;
                throw error;
              }
            },
            () => {
              if (!authority) {
                return;
              }
              localMutationPendingRef.current = false;
              if (deferredCanonicalRef.current) {
                deferredCanonicalRef.current = false;
                applyCanonicalSceneRef.current();
              }
              window.requestAnimationFrame(() => {
                window.requestAnimationFrame(() => {
                  applyingCanonicalRef.current = false;
                  api.history.clear();
                });
              });
            },
          ),
        );
        return;
      }
      animationFrame = window.requestAnimationFrame(reportWhenInitialized);
    };
    animationFrame = window.requestAnimationFrame(reportWhenInitialized);

    return () => {
      cancelled = true;
      window.cancelAnimationFrame(animationFrame);
    };
  }, [api, authority, initialElements]);

  useEffect(() => {
    if (!api || !authority) {
      return;
    }

    const applyCanonicalScene = () => {
      if (localMutationPendingRef.current) {
        deferredCanonicalRef.current = true;
        return;
      }
      const canonicalScene = authority.getScene();
      canonicalBaselineRef.current = canonicalScene;
      const projection = canonicalSceneToExcalidraw(canonicalScene);
      const current = excalidrawSceneToCanonical({
        appState: {
          viewBackgroundColor: api.getAppState().viewBackgroundColor,
        },
        elements: api.getSceneElements(),
        files: api.getFiles(),
        page: projection.page,
      });
      const currentHash = semanticHash(current);
      if (currentHash === authority.getSemanticHash()) {
        setRenderedElementCount(api.getSceneElements().length);
        api.history.clear();
        return;
      }

      programmaticProjectionPendingRef.current = true;
      api.addFiles(
        Object.values(projection.files) as unknown as Parameters<
          ExcalidrawImperativeAPI["addFiles"]
        >[0],
      );
      applyingCanonicalRef.current = true;
      api.updateScene({
        appState: {
          viewBackgroundColor: projection.appState.viewBackgroundColor,
        },
        elements:
          projection.elements as unknown as readonly ExcalidrawElement[],
      });
      setRenderedElementCount(projection.elements.length);
      api.history.clear();
      window.requestAnimationFrame(() => {
        window.requestAnimationFrame(() => {
          applyingCanonicalRef.current = false;
          api.history.clear();
        });
      });
      onCanonicalStatus?.("canonical_scene_applied");
    };

    applyCanonicalSceneRef.current = applyCanonicalScene;
    applyCanonicalScene();
    const unsubscribe = authority.subscribe(applyCanonicalScene);
    return () => {
      applyCanonicalSceneRef.current = () => undefined;
      unsubscribe();
    };
  }, [api, authority, onCanonicalStatus]);

  useEffect(() => {
    if (!authority) {
      return;
    }
    const routeCanonicalUndo = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (event.shiftKey) {
          authority.redo();
          onCanonicalStatus?.("canonical_redo");
        } else {
          authority.undo();
          onCanonicalStatus?.("canonical_undo");
        }
      }
    };
    window.addEventListener("keydown", routeCanonicalUndo, true);
    return () =>
      window.removeEventListener("keydown", routeCanonicalUndo, true);
  }, [authority, onCanonicalStatus]);

  return (
    <div
      ref={engineRootRef}
      className="engine-canvas"
      data-testid="excalidraw-canvas"
      data-rendered-element-count={renderedElementCount}
    >
      <Excalidraw
        initialData={{
          elements: initialElements,
          appState: {
            viewBackgroundColor:
              initialProjection?.appState.viewBackgroundColor ?? "#f8fafc",
          },
          scrollToContent: true,
        }}
        isCollaborating={authority !== undefined}
        onChange={
          authority
            ? (elements, appState, files) => {
                if (applyingCanonicalRef.current) {
                  return;
                }
                try {
                  const page = authority.getProjection().page;
                  const canonical = excalidrawSceneToCanonical({
                    appState: {
                      viewBackgroundColor: appState.viewBackgroundColor,
                    },
                    elements: elements.filter((element) => !element.isDeleted),
                    files,
                    page,
                  });
                  const canonicalHash = semanticHash(canonical);
                  if (programmaticProjectionPendingRef.current) {
                    onCanonicalStatus?.("canonical_projection_echo_suppressed");
                  } else if (canonicalHash !== authority.getSemanticHash()) {
                    authority.applySceneDelta(
                      canonicalBaselineRef.current ?? authority.getScene(),
                      canonical,
                    );
                    canonicalBaselineRef.current = authority.getScene();
                    setRenderedElementCount(canonical.elements.length);
                    api?.history.clear();
                    onCanonicalStatus?.("excalidraw_scene_committed");
                  }
                } catch (error) {
                  onCanonicalStatus?.(
                    error instanceof Error ? error.message : "scene_invalid",
                  );
                } finally {
                  if (localMutationPendingRef.current) {
                    localMutationPendingRef.current = false;
                    if (deferredCanonicalRef.current) {
                      deferredCanonicalRef.current = false;
                      applyCanonicalSceneRef.current();
                    }
                  }
                }
              }
            : undefined
        }
        viewModeEnabled={readOnly}
        excalidrawAPI={setApi}
      />
    </div>
  );
}

function createController(
  api: ExcalidrawImperativeAPI,
  preparePayload: (
    payload: readonly ExcalidrawElement[],
  ) => readonly ExcalidrawElement[],
  finishPayload: () => void,
): BoardController {
  return {
    getShapeCount: () =>
      api.getSceneElements().filter((element) => element.type !== "text")
        .length,
    exportPayload: () => api.getSceneElements(),
    restorePayload: (payload) => {
      if (!isExcalidrawPayload(payload)) {
        throw new Error("Payload Excalidraw không hợp lệ.");
      }
      const preparedPayload = preparePayload(payload);
      api.updateScene({ elements: preparedPayload });
      api.scrollToContent(preparedPayload, { fitToContent: true });
      finishPayload();
    },
  };
}

function isExcalidrawPayload(
  value: unknown,
): value is readonly ExcalidrawElement[] {
  return (
    Array.isArray(value) &&
    value.every(
      (element) =>
        typeof element === "object" &&
        element !== null &&
        "id" in element &&
        typeof element.id === "string" &&
        "type" in element &&
        typeof element.type === "string",
    )
  );
}
