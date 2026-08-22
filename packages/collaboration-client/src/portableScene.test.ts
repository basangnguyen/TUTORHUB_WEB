import { describe, expect, it } from "vitest";
import type { CanonicalExcalidrawSceneV1 } from "./canonicalAuthority.js";
import {
  exportPortableScene,
  importPortableScene,
  PORTABLE_EXCALIDRAW_LIMITS,
} from "./portableScene.js";

describe("portable Excalidraw scene", () => {
  it("round-trips one bounded canonical scene", () => {
    const scene = createScene();
    const bytes = exportPortableScene(scene, "2026-08-22T00:00:00.000Z");
    expect(importPortableScene(bytes)).toEqual(scene);
  });

  it.each([
    [
      "external URL",
      {
        elements: [
          { ...createScene().elements[0], link: "https://example.invalid/a" },
        ],
      },
    ],
    ["path traversal", { page: { ...createScene().page, name: "../private" } }],
    [
      "active SVG",
      {
        files: { "file-1": { dataURL: "data:image/svg+xml;base64,PHN2Zz4=" } },
      },
    ],
    [
      "active HTML",
      { elements: [{ ...createScene().elements[0], text: "<iframe src=x>" }] },
    ],
  ])("rejects %s without external resolution", (_name, override) => {
    const scene = { ...createScene(), ...override };
    expect(() => exportPortableScene(scene)).toThrowError(
      "portable_active_content_denied",
    );
  });

  it("rejects an oversized import before parsing", () => {
    expect(() =>
      importPortableScene(
        new Uint8Array(PORTABLE_EXCALIDRAW_LIMITS.maxBytes + 1),
      ),
    ).toThrowError("portable_too_large");
  });
});

function createScene(): CanonicalExcalidrawSceneV1 {
  return {
    elements: [
      {
        height: 40,
        id: "element-1",
        text: "TutorHub",
        type: "text",
        width: 120,
        x: 10,
        y: 20,
      },
    ],
    files: {},
    page: { backgroundColor: "#ffffff", id: "page-1", name: "Lesson" },
    schemaVersion: 1,
  };
}
