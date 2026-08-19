export type EngineId = "tldraw" | "excalidraw";
export type BoardCapability = "view" | "edit" | "present";
export type FixtureSize = 500 | 2000;

export interface BoardFixtureShape {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
  label: string;
  colorIndex: number;
}

export interface BoardController {
  getShapeCount: () => number;
  exportPayload: () => unknown;
  restorePayload: (payload: unknown) => void;
}

export interface SnapshotEnvelope {
  schemaVersion: 1;
  engine: EngineId;
  shapeCount: number;
  payload: unknown;
}

const MAX_SNAPSHOT_BYTES = 16 * 1024 * 1024;
const MAX_FIXTURE_SHAPES = 2_000;

export function createFixture(count: FixtureSize): BoardFixtureShape[] {
  const columns = 40;
  const width = 112;
  const height = 72;
  const gap = 20;

  return Array.from({ length: count }, (_, index) => ({
    id: `fixture-${index.toString().padStart(4, "0")}`,
    x: (index % columns) * (width + gap),
    y: Math.floor(index / columns) * (height + gap),
    width,
    height,
    label: `Mục ${index + 1}`,
    colorIndex: index % 5,
  }));
}

export function canMutate(capability: BoardCapability): boolean {
  return capability === "edit" || capability === "present";
}

export function canPresent(capability: BoardCapability): boolean {
  return capability === "present";
}

export function serializeSnapshot(
  engine: EngineId,
  controller: BoardController,
): string {
  const shapeCount = controller.getShapeCount();
  if (shapeCount < 0 || shapeCount > MAX_FIXTURE_SHAPES) {
    throw new Error("Số lượng đối tượng vượt giới hạn spike.");
  }

  const serialized = JSON.stringify({
    schemaVersion: 1,
    engine,
    shapeCount,
    payload: controller.exportPayload(),
  } satisfies SnapshotEnvelope);

  if (new TextEncoder().encode(serialized).byteLength > MAX_SNAPSHOT_BYTES) {
    throw new Error("Snapshot vượt giới hạn 16 MiB.");
  }

  return serialized;
}

export function parseSnapshot(
  value: string,
  expectedEngine: EngineId,
): SnapshotEnvelope {
  if (new TextEncoder().encode(value).byteLength > MAX_SNAPSHOT_BYTES) {
    throw new Error("Snapshot vượt giới hạn 16 MiB.");
  }

  let candidate: unknown;
  try {
    candidate = JSON.parse(value);
  } catch {
    throw new Error("Snapshot không phải JSON hợp lệ.");
  }

  if (!isRecord(candidate) || candidate.schemaVersion !== 1) {
    throw new Error("Snapshot dùng schema không được hỗ trợ.");
  }
  if (candidate.engine !== expectedEngine) {
    throw new Error("Snapshot không thuộc engine đang mở.");
  }
  if (
    typeof candidate.shapeCount !== "number" ||
    !Number.isInteger(candidate.shapeCount) ||
    candidate.shapeCount < 0 ||
    candidate.shapeCount > MAX_FIXTURE_SHAPES
  ) {
    throw new Error("Snapshot có shapeCount không hợp lệ.");
  }
  if (!("payload" in candidate)) {
    throw new Error("Snapshot thiếu payload.");
  }

  return candidate as unknown as SnapshotEnvelope;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export class MutationBudget {
  private remaining: number;

  constructor(private readonly limit: number) {
    if (!Number.isInteger(limit) || limit <= 0) {
      throw new Error("Mutation limit phải là số nguyên dương.");
    }
    this.remaining = limit;
  }

  consume(): void {
    if (this.remaining === 0) {
      throw new Error("Đã vượt mutation budget.");
    }
    this.remaining -= 1;
  }

  reset(): void {
    this.remaining = this.limit;
  }

  available(): number {
    return this.remaining;
  }
}
