import { Excalidraw } from "@excalidraw/excalidraw";
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import "@excalidraw/excalidraw/index.css";
import {
  createBrowserCollaborationSession,
  excalidrawSceneToCanonical,
  semanticHash,
  type BrowserCollaborationSession,
  type CanonicalExcalidrawAuthority,
  type CanonicalElementV1,
  type CollaborationConnectionStatus,
} from "@tutorhub/collaboration-client";
import type {
  WhiteboardCapability,
  WhiteboardDocument,
} from "@tutorhub/api-client";
import { Button } from "@tutorhub/ui";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import { useI18n } from "../../app/i18n";
import { requestWhiteboardGrant } from "../../app/whiteboards";

export default function LazyWhiteboardEngine({
  actorID,
  capability,
  document,
  onRecoveryRequired,
  tenantID,
}: {
  actorID: string;
  capability: WhiteboardCapability;
  document: WhiteboardDocument;
  onRecoveryRequired?: () => void;
  tenantID: string;
}) {
  const { t } = useI18n();
  const [attempt, setAttempt] = useState(0);
  const [connectionStatus, setConnectionStatus] =
    useState<CollaborationConnectionStatus>("connecting");
  const [error, setError] = useState(false);
  const [session, setSession] = useState<BrowserCollaborationSession | null>(
    null,
  );

  useEffect(() => {
    const abortController = new AbortController();
    let activeSession: BrowserCollaborationSession | null = null;
    void requestWhiteboardGrant(
      tenantID,
      document,
      capability,
      abortController.signal,
    )
      .then((grant) =>
        createBrowserCollaborationSession({
          actorId: actorID,
          grant: {
            capability: grant.capability,
            credential: grant.credential,
            documentId: grant.document_id,
            expiresAt: grant.expires_at,
            generation: grant.generation,
            providerUrl: grant.provider_url,
            revokeGeneration: grant.revoke_generation,
          },
          onStatus: setConnectionStatus,
          onTerminal: (reason) => {
            setError(true);
            if (reason === "authority_changed") onRecoveryRequired?.();
          },
          tenantId: tenantID,
        }),
      )
      .then((created) => {
        if (abortController.signal.aborted) {
          created.destroy();
          return;
        }
        activeSession = created;
        setSession(created);
      })
      .catch(() => {
        if (!abortController.signal.aborted) setError(true);
      });
    return () => {
      abortController.abort();
      activeSession?.destroy();
    };
  }, [actorID, attempt, capability, document, onRecoveryRequired, tenantID]);

  if (error) {
    return (
      <div className="whiteboard-tool-state" role="alert">
        <p>{t("whiteboard.engineError")}</p>
        <Button
          onClick={() => {
            setError(false);
            setSession(null);
            setConnectionStatus("connecting");
            setAttempt((value) => value + 1);
          }}
          variant="secondary"
        >
          {t("whiteboard.retry")}
        </Button>
      </div>
    );
  }
  if (session === null) {
    return (
      <div className="whiteboard-tool-state" role="status">
        <p>{t(`whiteboard.connection.${connectionStatus}`)}</p>
      </div>
    );
  }
  return (
    <CanonicalExcalidrawCanvas
      authority={session.authority}
      connectionStatus={connectionStatus}
      readOnly={session.capability === "view"}
    />
  );
}

const SEMANTIC_PAGE_SIZE = 50;

export type WhiteboardCanvasAuthority = Pick<
  CanonicalExcalidrawAuthority,
  | "getProjection"
  | "getScene"
  | "getSemanticHash"
  | "redo"
  | "replaceScene"
  | "subscribe"
  | "undo"
>;

export function CanonicalExcalidrawCanvas({
  authority,
  connectionStatus,
  readOnly,
}: {
  authority: WhiteboardCanvasAuthority;
  connectionStatus: CollaborationConnectionStatus;
  readOnly: boolean;
}) {
  const { t } = useI18n();
  const [api, setAPI] = useState<ExcalidrawImperativeAPI | null>(null);
  const [semanticRevision, setSemanticRevision] = useState(0);
  const applyingRemoteRef = useRef(false);
  const canvasRef = useRef<HTMLDivElement>(null);
  const semanticHeadingRef = useRef<HTMLHeadingElement>(null);
  const semanticDescriptionID = useId();
  const initialProjection = useMemo(
    () => authority.getProjection(),
    [authority],
  );

  useEffect(() => {
    const root = canvasRef.current;
    if (root === null) return;
    const normalizeExcalidrawSemantics = () => {
      const menuTrigger =
        root.querySelector<HTMLButtonElement>(".main-menu-trigger");
      if (menuTrigger && !menuTrigger.getAttribute("aria-label")) {
        menuTrigger.setAttribute("aria-label", t("whiteboard.menu"));
      }
      root.querySelectorAll("footer").forEach((footer) => {
        footer.setAttribute("aria-label", t("whiteboard.footerControls"));
        footer.setAttribute("role", "group");
      });
    };
    normalizeExcalidrawSemantics();
    const observer = new MutationObserver(normalizeExcalidrawSemantics);
    observer.observe(root, { childList: true, subtree: true });
    return () => observer.disconnect();
  }, [t]);

  useEffect(() => {
    if (api === null) return;
    const applyAuthority = () => {
      const projection = authority.getProjection();
      const current = excalidrawSceneToCanonical({
        appState: {
          viewBackgroundColor: api.getAppState().viewBackgroundColor,
        },
        elements: api
          .getSceneElements()
          .filter((element) => !element.isDeleted),
        files: api.getFiles(),
        page: projection.page,
      });
      if (semanticHash(current) === authority.getSemanticHash()) {
        api.history.clear();
        return;
      }
      applyingRemoteRef.current = true;
      api.addFiles(
        Object.values(projection.files) as unknown as Parameters<
          ExcalidrawImperativeAPI["addFiles"]
        >[0],
      );
      api.updateScene({
        appState: {
          viewBackgroundColor: projection.appState.viewBackgroundColor,
        },
        elements:
          projection.elements as unknown as readonly ExcalidrawElement[],
      });
      api.history.clear();
      setSemanticRevision((value) => value + 1);
      requestAnimationFrame(() => {
        applyingRemoteRef.current = false;
        api.history.clear();
      });
    };
    applyAuthority();
    return authority.subscribe(applyAuthority);
  }, [api, authority]);

  useEffect(() => {
    if (readOnly) return;
    const handleUndo = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        event.stopImmediatePropagation();
        if (event.shiftKey) authority.redo();
        else authority.undo();
      }
    };
    window.addEventListener("keydown", handleUndo, true);
    return () => window.removeEventListener("keydown", handleUndo, true);
  }, [authority, readOnly]);

  const semanticScene = authority.getScene();
  return (
    <div className="whiteboard-engine">
      <p
        aria-live="polite"
        className="whiteboard-connection-status"
        role="status"
      >
        {t(`whiteboard.connection.${connectionStatus}`)}
        {readOnly ? ` · ${t("whiteboard.readOnly")}` : ""}
      </p>
      <div className="whiteboard-semantic-actions">
        <Button
          onClick={() => semanticHeadingRef.current?.focus()}
          variant="secondary"
        >
          {t("whiteboard.readSemantic")}
        </Button>
      </div>
      <div
        aria-describedby={semanticDescriptionID}
        aria-label={t("whiteboard.canvasLabel")}
        className="whiteboard-engine-canvas"
        ref={canvasRef}
        role="region"
        tabIndex={-1}
      >
        <Excalidraw
          excalidrawAPI={setAPI}
          initialData={{
            appState: {
              viewBackgroundColor:
                initialProjection.appState.viewBackgroundColor,
            },
            elements:
              initialProjection.elements as unknown as readonly ExcalidrawElement[],
            scrollToContent: true,
          }}
          isCollaborating
          onChange={(elements, appState, files) => {
            if (readOnly || applyingRemoteRef.current) return;
            const canonical = excalidrawSceneToCanonical({
              appState: { viewBackgroundColor: appState.viewBackgroundColor },
              elements: elements.filter((element) => !element.isDeleted),
              files,
              page: authority.getProjection().page,
            });
            if (semanticHash(canonical) !== authority.getSemanticHash()) {
              authority.replaceScene(canonical);
              api?.history.clear();
              setSemanticRevision((value) => value + 1);
            }
          }}
          viewModeEnabled={readOnly}
        />
      </div>
      <WhiteboardSemanticFallback
        canvasRef={canvasRef}
        descriptionID={semanticDescriptionID}
        elements={semanticScene.elements}
        headingRef={semanticHeadingRef}
        revision={semanticRevision}
      />
    </div>
  );
}

export function WhiteboardSemanticFallback({
  canvasRef,
  descriptionID,
  elements,
  headingRef,
  revision,
}: {
  canvasRef: RefObject<HTMLDivElement | null>;
  descriptionID: string;
  elements: CanonicalElementV1[];
  headingRef: RefObject<HTMLHeadingElement | null>;
  revision: number;
}) {
  const { t } = useI18n();
  const [pageIndex, setPageIndex] = useState(0);
  const pageCount = Math.max(
    1,
    Math.ceil(elements.length / SEMANTIC_PAGE_SIZE),
  );
  const activePageIndex = Math.min(pageIndex, pageCount - 1);
  const visibleElements = useMemo(
    () =>
      elements.slice(
        activePageIndex * SEMANTIC_PAGE_SIZE,
        (activePageIndex + 1) * SEMANTIC_PAGE_SIZE,
      ),
    [activePageIndex, elements],
  );

  return (
    <section
      aria-labelledby="whiteboard-semantic-title"
      className="whiteboard-semantic-fallback"
      data-semantic-revision={revision}
      data-testid="whiteboard-semantic-fallback"
    >
      <h3 id="whiteboard-semantic-title" ref={headingRef} tabIndex={-1}>
        {t("whiteboard.semanticTitle")}
      </h3>
      <p id={descriptionID}>{t("whiteboard.semanticDescription")}</p>
      <p
        aria-atomic="true"
        aria-live="polite"
        data-testid="whiteboard-semantic-page"
      >
        {t("whiteboard.semanticPage")} {activePageIndex + 1}/{pageCount} ·{" "}
        {elements.length} {t("whiteboard.semanticElements")}
      </p>
      <div
        aria-label={t("whiteboard.semanticNavigation")}
        className="whiteboard-semantic-navigation"
        role="group"
      >
        <Button
          disabled={activePageIndex === 0}
          onClick={() => setPageIndex((current) => Math.max(0, current - 1))}
          variant="secondary"
        >
          {t("whiteboard.semanticPrevious")}
        </Button>
        <Button
          disabled={activePageIndex >= pageCount - 1}
          onClick={() =>
            setPageIndex((current) => Math.min(pageCount - 1, current + 1))
          }
          variant="secondary"
        >
          {t("whiteboard.semanticNext")}
        </Button>
        <Button onClick={() => canvasRef.current?.focus()} variant="secondary">
          {t("whiteboard.focusCanvas")}
        </Button>
      </div>
      {visibleElements.length === 0 ? (
        <p>{t("whiteboard.semanticEmpty")}</p>
      ) : (
        <ol
          className="whiteboard-semantic-list"
          start={activePageIndex * SEMANTIC_PAGE_SIZE + 1}
        >
          {visibleElements.map((element) => (
            <li key={element.id}>
              {semanticElementLabel(element, t("whiteboard.untitledElement"))}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function semanticElementLabel(element: CanonicalElementV1, fallback: string) {
  const text = typeof element.text === "string" ? element.text.trim() : "";
  const label = text ? truncateSemanticText(text) : fallback;
  return `${element.type}: ${label}; x ${Math.round(Number(element.x))}, y ${Math.round(Number(element.y))}; ${Math.round(Number(element.width))} × ${Math.round(Number(element.height))}`;
}

function truncateSemanticText(value: string) {
  const normalized = value.replace(/\s+/g, " ").trim();
  return normalized.length <= 240 ? normalized : `${normalized.slice(0, 239)}…`;
}
