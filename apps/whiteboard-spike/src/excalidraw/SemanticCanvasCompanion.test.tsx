import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  SemanticCanvasCompanion,
  canonicalSceneToSemanticItems,
  fixturesToSemanticItems,
} from "./SemanticCanvasCompanion";

describe("Gate E semantic canvas companion", () => {
  it("projects text, connections, image warnings, and bounded pagination", () => {
    const items = canonicalSceneToSemanticItems({
      elements: [
        rectangle("shape-1"),
        {
          ...rectangle("text-1"),
          text: "Nội dung bài học",
          type: "text",
        },
        {
          ...rectangle("arrow-1"),
          endBinding: { elementId: "shape-2" },
          startBinding: { elementId: "shape-1" },
          type: "arrow",
        },
        { ...rectangle("image-1"), type: "image" },
      ],
      files: {},
      page: { backgroundColor: "#fff", id: "page-1", name: "Bài 1" },
      schemaVersion: 1,
    });

    expect(items[1]?.description).toContain("Nội dung bài học");
    expect(items[2]?.description).toContain("shape-1");
    expect(items[2]?.description).toContain("shape-2");
    expect(items[3]?.warning).toBe("Hình ảnh chưa có mô tả thay thế.");

    const fixtures = fixturesToSemanticItems(
      Array.from({ length: 120 }, (_, index) => ({
        colorIndex: 0,
        height: 50,
        id: `fixture-${index}`,
        label: `Mục ${index + 1}`,
        width: 80,
        x: index,
        y: index,
      })),
    );
    const focusCanvas = vi.fn();
    render(
      <SemanticCanvasCompanion
        canvasAvailable
        hash="fnv1a64:test"
        items={fixtures}
        onFocusCanvas={focusCanvas}
        pageName="Bài kiểm tra"
      />,
    );

    expect(screen.getAllByRole("listitem")).toHaveLength(50);
    expect(screen.getByTestId("semantic-page-status")).toHaveTextContent(
      "trang 1 trên 3",
    );
    fireEvent.click(screen.getByRole("button", { name: "Trang sau" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(50);
    expect(screen.getByTestId("semantic-page-status")).toHaveTextContent(
      "trang 2 trên 3",
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Chuyển tiêu điểm tới canvas" }),
    );
    expect(focusCanvas).toHaveBeenCalledOnce();
  });
});

function rectangle(id: string) {
  return {
    height: 80,
    id,
    type: "rectangle",
    width: 120,
    x: 10,
    y: 20,
  };
}
