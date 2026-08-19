import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { BrowserCanonicalSession } from "./browserCanonicalSession";
import type { BrowserAuthorizedSession } from "./browserAuthorizedSession";
import type { CollaborationCapability } from "./authorizationContract";
import type { CanonicalExcalidrawSceneV1 } from "./canonicalAuthority";
import {
  SemanticCanvasCompanion,
  canonicalSceneToSemanticItems,
  fixturesToSemanticItems,
} from "./SemanticCanvasCompanion";
import {
  createFixture,
  type BoardController,
  type FixtureSize,
} from "../model";

const ExcalidrawBoard = lazy(() => import("../adapters/ExcalidrawBoard"));

export function ExcalidrawCandidateApp() {
  const [opened, setOpened] = useState(false);
  const [session, setSession] = useState<
    BrowserCanonicalSession | BrowserAuthorizedSession | null
  >(null);
  const [boardController, setBoardController] =
    useState<BoardController | null>(null);
  const [canonicalState, setCanonicalState] = useState({
    count: 0,
    hash: "not-connected",
    ids: [] as string[],
  });
  const [canonicalScene, setCanonicalScene] =
    useState<CanonicalExcalidrawSceneV1 | null>(null);
  const [status, setStatus] = useState(
    "Engine chưa được tải. Bấm Mở bảng Excalidraw để bắt đầu.",
  );
  const sequenceRef = useRef(0);
  const boardRegionRef = useRef<HTMLElement>(null);
  const openButtonRef = useRef<HTMLButtonElement>(null);
  const semanticHeadingRef = useRef<HTMLHeadingElement>(null);
  const collaboration = useMemo(readCollaborationOptions, []);
  const fixtureSize = readFixtureSize();
  const fixtures = useMemo(() => createFixture(fixtureSize), [fixtureSize]);
  const semanticItems = useMemo(
    () =>
      canonicalScene
        ? canonicalSceneToSemanticItems(canonicalScene)
        : fixturesToSemanticItems(fixtures),
    [canonicalScene, fixtures],
  );

  useEffect(() => {
    if (!collaboration) {
      return;
    }
    let disposed = false;
    let activeSession:
      BrowserCanonicalSession | BrowserAuthorizedSession | null = null;
    setStatus("Đang kết nối canonical authority…");
    const createSession =
      collaboration.mode === "gate-c"
        ? import("./browserAuthorizedSession").then(
            ({ createBrowserAuthorizedSession }) =>
              createBrowserAuthorizedSession(collaboration),
          )
        : import("./browserCanonicalSession").then(
            ({ createBrowserCanonicalSession }) =>
              createBrowserCanonicalSession(collaboration),
          );
    void createSession
      .then((createdSession) => {
        if (disposed) {
          createdSession.destroy();
          return;
        }
        activeSession = createdSession;
        setSession(createdSession);
        setStatus(
          collaboration.mode === "gate-c"
            ? `authorization_ready:${sessionCapability(createdSession)}:generation_${sessionGeneration(createdSession)}`
            : "Canonical authority đã đồng bộ. Có thể mở bảng.",
        );
      })
      .catch((error: unknown) => {
        setStatus(
          error instanceof Error ? error.message : "gate_b_connect_failed",
        );
      });

    return () => {
      disposed = true;
      activeSession?.destroy();
    };
  }, [collaboration]);

  useEffect(() => {
    if (!session) {
      return;
    }
    const updateCanonicalState = () => {
      const scene = session.authority.getScene();
      setCanonicalScene(scene);
      setCanonicalState({
        count: scene.elements.length,
        hash: session.authority.getSemanticHash(),
        ids: scene.elements.map((element) => element.id),
      });
    };
    updateCanonicalState();
    return session.authority.subscribe(updateCanonicalState);
  }, [session]);

  const openBoard = () => {
    setOpened(true);
    setStatus("Đang tải engine Excalidraw…");
  };

  const closeBoard = () => {
    setBoardController(null);
    setOpened(false);
    setStatus("Đã đóng engine Excalidraw. Semantic fallback vẫn sẵn sàng.");
    window.setTimeout(() => openButtonRef.current?.focus(), 0);
  };

  const onReady = (controller: BoardController) => {
    setBoardController(controller);
    setStatus(
      `Excalidraw sẵn sàng với ${controller.getShapeCount().toLocaleString("vi-VN")} đối tượng.`,
    );
    boardRegionRef.current?.focus();
  };

  const onCanonicalStatus = useCallback((nextStatus: string) => {
    setStatus(nextStatus);
  }, []);

  const addActorElement = () => {
    if (!session || isReadOnlySession(session)) {
      return;
    }
    sequenceRef.current += 1;
    session.authority.putElement(
      createGateRectangle(session.actorId, sequenceRef.current),
    );
    setStatus("canonical_local_element_committed");
  };

  const addElementThroughExcalidraw = () => {
    if (!session || !boardController || isReadOnlySession(session)) {
      return;
    }
    const current = boardController.exportPayload();
    if (!Array.isArray(current)) {
      setStatus("excalidraw_projection_invalid");
      return;
    }
    sequenceRef.current += 1;
    boardController.restorePayload([
      ...current,
      createGateRectangle(session.actorId, sequenceRef.current),
    ]);
    setStatus("excalidraw_local_edit_requested");
  };

  return (
    <main className="spike-shell" data-testid="excalidraw-candidate-shell">
      <header className="spike-header">
        <div>
          <p className="eyebrow">
            P5-COLLAB-01 ·{" "}
            {collaboration?.mode === "gate-c"
              ? "Gate C"
              : collaboration
                ? "Gate B"
                : "Gate A"}
          </p>
          <h1>TutorHub Excalidraw-only candidate</h1>
          <p>
            {collaboration
              ? collaboration.mode === "gate-c"
                ? "Artifact cô lập cho one-time grant, capability, generation/revoke và abuse boundary."
                : "Artifact cô lập cho canonical authority, convergence và actor-local undo."
              : "Artifact cô lập cho dependency, license, lazy-load và bundle guard."}
          </p>
        </div>
        <span className="research-badge">@excalidraw/excalidraw 0.18.1</span>
      </header>

      <section
        className="control-panel"
        aria-labelledby="candidate-controls-title"
      >
        <h2 id="candidate-controls-title">Candidate controls</h2>
        <p>
          Fixture: {fixtureSize.toLocaleString("vi-VN")} đối tượng. Engine chỉ
          được tải sau hành động rõ ràng của người dùng.
        </p>
        <button
          ref={openButtonRef}
          type="button"
          disabled={collaboration !== null && session === null}
          onClick={opened ? closeBoard : openBoard}
        >
          {opened ? "Đóng bảng Excalidraw" : "Mở bảng Excalidraw"}
        </button>
        <button
          type="button"
          onClick={() => semanticHeadingRef.current?.focus()}
        >
          Đọc nội dung bảng
        </button>
      </section>

      <p className="status-line" role="status" aria-live="polite">
        {status}
      </p>

      {collaboration ? (
        <section
          className="control-panel"
          aria-label="Canonical Gate B controls"
          data-testid="canonical-controls"
        >
          <p
            data-testid="canonical-state"
            data-canonical-hash={canonicalState.hash}
            data-element-count={canonicalState.count}
            data-element-ids={canonicalState.ids.join(",")}
            data-capability={sessionCapability(session)}
            data-generation={sessionGeneration(session)}
          >
            Actor {collaboration.actorId}: {canonicalState.count} phần tử ·{" "}
            {canonicalState.hash}
          </p>
          <div className="engine-actions">
            <button
              type="button"
              disabled={!session || isReadOnlySession(session)}
              onClick={addActorElement}
            >
              Thêm phần tử của actor
            </button>
            <button
              type="button"
              disabled={
                !session || !boardController || isReadOnlySession(session)
              }
              onClick={addElementThroughExcalidraw}
            >
              Thêm qua Excalidraw
            </button>
            <button
              type="button"
              disabled={!session || isReadOnlySession(session)}
              onClick={() => {
                const undone = session?.authority.undo() ?? false;
                setStatus(`canonical_undo:${undone ? "applied" : "empty"}`);
              }}
            >
              Hoàn tác canonical
            </button>
            <button
              type="button"
              disabled={!session || isReadOnlySession(session)}
              onClick={() => {
                session?.authority.redo();
                setStatus("canonical_redo");
              }}
            >
              Làm lại canonical
            </button>
          </div>
        </section>
      ) : null}

      <SemanticCanvasCompanion
        ref={semanticHeadingRef}
        canvasAvailable={opened}
        hash={canonicalState.hash}
        items={semanticItems}
        onFocusCanvas={() => boardRegionRef.current?.focus()}
        pageName={canonicalScene?.page.name ?? "Fixture Excalidraw"}
      />

      {opened ? (
        <section
          ref={boardRegionRef}
          className="board-frame"
          aria-labelledby="excalidraw-canvas-title"
          data-engine="excalidraw"
          data-fixture-size={fixtureSize}
          tabIndex={-1}
        >
          <h2 id="excalidraw-canvas-title" className="visually-hidden">
            Bảng Excalidraw candidate
          </h2>
          <Suspense
            fallback={<div className="engine-loading">Đang tải engine…</div>}
          >
            <ExcalidrawBoard
              authority={session?.authority}
              fixtures={fixtures}
              readOnly={isReadOnlySession(session)}
              onCanonicalStatus={onCanonicalStatus}
              onReady={onReady}
            />
          </Suspense>
        </section>
      ) : (
        <section
          className="candidate-placeholder"
          data-testid="candidate-placeholder"
          aria-label="Excalidraw chưa được tải"
        >
          Excalidraw chưa nằm trong initial candidate shell.
        </section>
      )}
    </main>
  );
}

interface GateBCollaborationOptions {
  actorId: string;
  bootstrap: boolean;
  mode: "gate-b";
  providerUrl: string;
}

interface GateCCollaborationOptions {
  actorId: string;
  bootstrap: boolean;
  controlUrl: string;
  documentId: string;
  mode: "gate-c";
  providerUrl: string;
  requestedCapability: CollaborationCapability;
  sessionId: string;
  tenantId: string;
}

type CollaborationOptions =
  GateBCollaborationOptions | GateCCollaborationOptions;

function readCollaborationOptions(): CollaborationOptions | null {
  const search = new URLSearchParams(window.location.search);
  if (search.get("authz") === "1") {
    const actorId = search.get("actor");
    const capability = search.get("capability");
    const controlUrl = search.get("control");
    const documentId = search.get("document");
    const providerUrl = search.get("provider");
    const sessionId = search.get("session");
    const tenantId = search.get("tenant");
    if (
      (actorId !== "teacher-a" &&
        actorId !== "student-b" &&
        actorId !== "viewer-c") ||
      (capability !== "view" &&
        capability !== "edit" &&
        capability !== "present") ||
      controlUrl === null ||
      providerUrl === null ||
      tenantId !== "tenant-a" ||
      documentId !== "board-1" ||
      sessionId === null
    ) {
      throw new Error("gate_c_query_invalid");
    }
    return {
      actorId,
      bootstrap: search.get("bootstrap") === "1",
      controlUrl,
      documentId,
      mode: "gate-c",
      providerUrl,
      requestedCapability: capability,
      sessionId,
      tenantId,
    };
  }
  if (search.get("collab") !== "1") {
    return null;
  }
  const actorId = search.get("actor");
  const providerUrl = search.get("provider");
  if (
    (actorId !== "teacher-a" && actorId !== "student-b") ||
    providerUrl === null
  ) {
    throw new Error("gate_b_query_invalid");
  }
  return {
    actorId,
    bootstrap: search.get("bootstrap") === "1",
    mode: "gate-b",
    providerUrl,
  };
}

function isReadOnlySession(
  session: BrowserCanonicalSession | BrowserAuthorizedSession | null,
): boolean {
  return (
    session !== null && "capability" in session && session.capability === "view"
  );
}

function sessionCapability(
  session: BrowserCanonicalSession | BrowserAuthorizedSession | null,
): CollaborationCapability | "unrestricted-fixture" | "pending" {
  if (session === null) return "pending";
  return "capability" in session ? session.capability : "unrestricted-fixture";
}

function sessionGeneration(
  session: BrowserCanonicalSession | BrowserAuthorizedSession | null,
): number | "pending" {
  if (session === null) return "pending";
  return "generation" in session ? session.generation : 1;
}

function createGateRectangle(actorId: string, sequence: number) {
  const x = 260 + sequence * 180;
  return {
    angle: 0,
    backgroundColor: actorId === "teacher-a" ? "#b2f2bb" : "#ffec99",
    boundElements: null,
    fillStyle: "solid",
    frameId: null,
    groupIds: [],
    height: 100,
    id: `${actorId}-shape-${sequence}`,
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
    updated: 1_786_000_000_000 + sequence,
    version: 1,
    versionNonce: x + 2,
    width: 160,
    x,
    y: 120,
  };
}

function readFixtureSize(): FixtureSize {
  return new URLSearchParams(window.location.search).get("fixture") === "2000"
    ? 2000
    : 500;
}
