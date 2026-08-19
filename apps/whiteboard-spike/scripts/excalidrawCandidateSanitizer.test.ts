import { describe, expect, it } from "vitest";

import { sanitizeExcalidrawCandidateSource } from "./excalidrawCandidateSanitizer";

describe("sanitizeExcalidrawCandidateSource", () => {
  it("removes release-demo configuration without retaining its values", () => {
    const source = [
      `const key = "AIza${"x".repeat(35)}";`,
      `const hosts = ["one.firebaseio.com", "two.firebaseapp.com"];`,
      `const rooms = "excalidraw-room/excalidraw-room/excalidraw-room/excalidraw-room/excalidraw-room";`,
    ].join("\n");

    const result = sanitizeExcalidrawCandidateSource(source);

    expect(result.counts).toEqual({
      googleApiKey: 1,
      firebaseHost: 2,
      demoRoomName: 5,
    });
    expect(result.code).not.toMatch(/\bAIza[0-9A-Za-z_-]{30,}\b/);
    expect(result.code).not.toMatch(/firebaseio\.com|firebaseapp\.com/);
    expect(result.code).not.toContain("excalidraw-room");
  });
});
