import { describe, expect, it } from "vitest";
import {
  MutationBudget,
  canMutate,
  canPresent,
  createFixture,
  parseSnapshot,
  serializeSnapshot,
  type BoardController,
} from "./model";

describe("whiteboard spike model", () => {
  it.each([500, 2000] as const)("tạo fixture %i đối tượng ổn định", (count) => {
    const first = createFixture(count);
    const second = createFixture(count);

    expect(first).toHaveLength(count);
    expect(second).toEqual(first);
    expect(new Set(first.map((shape) => shape.id)).size).toBe(count);
  });

  it("phân tách view, edit và present capability", () => {
    expect(canMutate("view")).toBe(false);
    expect(canPresent("view")).toBe(false);
    expect(canMutate("edit")).toBe(true);
    expect(canPresent("edit")).toBe(false);
    expect(canMutate("present")).toBe(true);
    expect(canPresent("present")).toBe(true);
  });

  it("round-trip snapshot và fail closed với corruption/engine mismatch", () => {
    const controller: BoardController = {
      getShapeCount: () => 500,
      exportPayload: () => ({ records: ["safe-fixture"] }),
      restorePayload: () => undefined,
    };
    const snapshot = serializeSnapshot("tldraw", controller);

    expect(parseSnapshot(snapshot, "tldraw").shapeCount).toBe(500);
    expect(() => parseSnapshot("{broken", "tldraw")).toThrow(
      "Snapshot không phải JSON hợp lệ.",
    );
    expect(() => parseSnapshot(snapshot, "excalidraw")).toThrow(
      "Snapshot không thuộc engine đang mở.",
    );
  });

  it("chặn payload logic vượt shape cap", () => {
    const payload = JSON.stringify({
      schemaVersion: 1,
      engine: "tldraw",
      shapeCount: 2001,
      payload: {},
    });
    expect(() => parseSnapshot(payload, "tldraw")).toThrow(
      "Snapshot có shapeCount không hợp lệ.",
    );
  });

  it("chặn snapshot vượt byte cap ngay khi serialize", () => {
    const controller: BoardController = {
      getShapeCount: () => 1,
      exportPayload: () => ({ content: "x".repeat(16 * 1024 * 1024) }),
      restorePayload: () => undefined,
    };

    expect(() => serializeSnapshot("tldraw", controller)).toThrow(
      "Snapshot vượt giới hạn 16 MiB.",
    );
  });

  it("thực thi mutation budget fail closed", () => {
    const budget = new MutationBudget(2);
    budget.consume();
    budget.consume();
    expect(budget.available()).toBe(0);
    expect(() => budget.consume()).toThrow("Đã vượt mutation budget.");
    budget.reset();
    expect(budget.available()).toBe(2);
  });
});
