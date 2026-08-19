import { lazy, Suspense, useMemo, useRef, useState } from "react";
import {
  canMutate,
  canPresent,
  createFixture,
  parseSnapshot,
  serializeSnapshot,
  type BoardCapability,
  type BoardController,
  type EngineId,
  type FixtureSize,
} from "./model";

const TldrawBoard = lazy(() => import("./adapters/TldrawBoard"));
const ExcalidrawBoard = lazy(() => import("./adapters/ExcalidrawBoard"));

export function App() {
  const [engine, setEngine] = useState<EngineId>(readInitialEngine);
  const [fixtureSize, setFixtureSize] =
    useState<FixtureSize>(readInitialFixture);
  const [capability, setCapability] = useState<BoardCapability>("edit");
  const [savedSnapshot, setSavedSnapshot] = useState<string | null>(null);
  const [status, setStatus] = useState(
    () => `Đang tải engine ${readInitialEngine()}.`,
  );
  const controllerRef = useRef<BoardController | null>(null);
  const fixtures = useMemo(() => createFixture(fixtureSize), [fixtureSize]);

  const switchEngine = (nextEngine: EngineId) => {
    controllerRef.current = null;
    setSavedSnapshot(null);
    setEngine(nextEngine);
    setStatus(`Đang tải engine ${nextEngine}.`);
  };

  const onReady = (controller: BoardController) => {
    controllerRef.current = controller;
    setStatus(
      `${engine} sẵn sàng với ${controller.getShapeCount().toLocaleString("vi-VN")} đối tượng.`,
    );
  };

  const exportSnapshot = () => {
    const controller = controllerRef.current;
    if (!controller) {
      setStatus("Engine chưa sẵn sàng.");
      return;
    }
    try {
      const snapshot = serializeSnapshot(engine, controller);
      setSavedSnapshot(snapshot);
      setStatus(
        `Đã chụp snapshot ${new TextEncoder().encode(snapshot).byteLength.toLocaleString("vi-VN")} byte.`,
      );
    } catch (error) {
      setStatus(errorMessage(error));
    }
  };

  const restoreSnapshot = () => {
    if (!canMutate(capability)) {
      setStatus("Quyền view không được khôi phục snapshot.");
      return;
    }
    if (!savedSnapshot || !controllerRef.current) {
      setStatus("Chưa có snapshot để khôi phục.");
      return;
    }
    try {
      const snapshot = parseSnapshot(savedSnapshot, engine);
      controllerRef.current.restorePayload(snapshot.payload);
      setStatus(
        `Khôi phục snapshot ${snapshot.shapeCount.toLocaleString("vi-VN")} đối tượng thành công.`,
      );
    } catch (error) {
      setStatus(`Từ chối snapshot: ${errorMessage(error)}`);
    }
  };

  const corruptSnapshot = () => {
    if (!savedSnapshot) {
      setStatus("Hãy chụp snapshot trước khi tạo corruption fixture.");
      return;
    }
    setSavedSnapshot(`${savedSnapshot.slice(0, -1)}!`);
    setStatus(
      "Đã tạo corruption fixture; lần restore kế tiếp phải fail closed.",
    );
  };

  return (
    <main className="spike-shell">
      <header className="spike-header">
        <div>
          <p className="eyebrow">P5-COLLAB-00 · local research only</p>
          <h1>Whiteboard engine evidence harness</h1>
          <p>
            Cùng fixture và cùng snapshot gate; không kết nối route production,
            credential, LiveKit hay Core API.
          </p>
        </div>
        <span className="research-badge">Không phải quyết định production</span>
      </header>

      <section className="control-panel" aria-labelledby="controls-title">
        <h2 id="controls-title">Ma trận thử nghiệm</h2>
        <fieldset>
          <legend>Engine</legend>
          <button
            type="button"
            aria-pressed={engine === "tldraw"}
            onClick={() => switchEngine("tldraw")}
          >
            tldraw 5.3.1
          </button>
          <button
            type="button"
            aria-pressed={engine === "excalidraw"}
            onClick={() => switchEngine("excalidraw")}
          >
            Excalidraw 0.18.1
          </button>
        </fieldset>

        <label>
          Fixture
          <select
            value={fixtureSize}
            onChange={(event) => {
              controllerRef.current = null;
              setSavedSnapshot(null);
              setFixtureSize(Number(event.target.value) as FixtureSize);
              setStatus(`Đang tạo fixture ${event.target.value} đối tượng.`);
            }}
          >
            <option value={500}>500 đối tượng</option>
            <option value={2000}>2.000 đối tượng</option>
          </select>
        </label>

        <label>
          Capability mô phỏng
          <select
            value={capability}
            onChange={(event) =>
              setCapability(event.target.value as BoardCapability)
            }
          >
            <option value="view">view · chỉ xem</option>
            <option value="edit">edit · chỉnh sửa</option>
            <option value="present">present · sửa và trình bày</option>
          </select>
        </label>

        <div className="capability-summary" aria-label="Quyền hiệu lực">
          mutate={String(canMutate(capability))} · present=
          {String(canPresent(capability))}
        </div>

        <div className="snapshot-actions" aria-label="Snapshot test controls">
          <button type="button" onClick={exportSnapshot}>
            Chụp snapshot
          </button>
          <button
            type="button"
            disabled={!canMutate(capability)}
            onClick={restoreSnapshot}
          >
            Khôi phục
          </button>
          <button type="button" onClick={corruptSnapshot}>
            Làm hỏng snapshot
          </button>
        </div>
      </section>

      <p className="status-line" role="status" aria-live="polite">
        {status}
      </p>

      <section
        className="board-frame"
        aria-label={`Bảng thử nghiệm ${engine}`}
        data-engine={engine}
        data-fixture-size={fixtureSize}
      >
        <Suspense
          fallback={<div className="engine-loading">Đang tải engine…</div>}
        >
          {engine === "tldraw" ? (
            <TldrawBoard
              key={`tldraw-${fixtureSize}`}
              fixtures={fixtures}
              readOnly={!canMutate(capability)}
              onReady={onReady}
            />
          ) : (
            <ExcalidrawBoard
              key={`excalidraw-${fixtureSize}`}
              fixtures={fixtures}
              readOnly={!canMutate(capability)}
              onReady={onReady}
            />
          )}
        </Suspense>
      </section>
    </main>
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Lỗi không xác định.";
}

function readInitialEngine(): EngineId {
  return new URLSearchParams(window.location.search).get("engine") ===
    "excalidraw"
    ? "excalidraw"
    : "tldraw";
}

function readInitialFixture(): FixtureSize {
  return new URLSearchParams(window.location.search).get("fixture") === "2000"
    ? 2000
    : 500;
}
