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
import { useEffect, useMemo, useRef, useState } from "react";
import { useI18n } from "../../app/i18n";
import { requestWhiteboardGrant } from "../../app/whiteboards";

export default function LazyWhiteboardEngine({
  actorID,
  capability,
  document,
  tenantID,
}: {
  actorID: string;
  capability: WhiteboardCapability;
  document: WhiteboardDocument;
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
  }, [actorID, attempt, capability, document, tenantID]);

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

function CanonicalExcalidrawCanvas({
  authority,
  connectionStatus,
  readOnly,
}: {
  authority: CanonicalExcalidrawAuthority;
  connectionStatus: CollaborationConnectionStatus;
  readOnly: boolean;
}) {
  const { t } = useI18n();
  const [api, setAPI] = useState<ExcalidrawImperativeAPI | null>(null);
  const [semanticRevision, setSemanticRevision] = useState(0);
  const applyingRemoteRef = useRef(false);
  const initialProjection = useMemo(
    () => authority.getProjection(),
    [authority],
  );

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
      <div className="whiteboard-engine-canvas">
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
      <section
        aria-label={t("whiteboard.semanticTitle")}
        className="whiteboard-semantic-fallback"
        data-semantic-revision={semanticRevision}
      >
        <h3>{t("whiteboard.semanticTitle")}</h3>
        {semanticScene.elements.length === 0 ? (
          <p>{t("whiteboard.semanticEmpty")}</p>
        ) : (
          <ol>
            {semanticScene.elements.map((element) => (
              <li key={element.id}>
                {semanticElementLabel(element, t("whiteboard.untitledElement"))}
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}

function semanticElementLabel(element: CanonicalElementV1, fallback: string) {
  const text = typeof element.text === "string" ? element.text.trim() : "";
  return `${element.type}: ${text || fallback} (${Math.round(Number(element.x))}, ${Math.round(Number(element.y))})`;
}
