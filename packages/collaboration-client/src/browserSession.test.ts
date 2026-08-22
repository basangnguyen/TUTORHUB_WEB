import { WebSocketStatus } from "@hocuspocus/provider";
import { describe, expect, it } from "vitest";
import {
  deriveProviderDocumentName,
  projectProviderStatus,
} from "./browserSession";

describe("browser collaboration session contract", () => {
  it("derives the private provider document name without exposing it in the API", () => {
    expect(
      deriveProviderDocumentName("8c40ee79-5d4a-4d53-8845-6d295958dd42"),
    ).toBe("wb_8c40ee795d4a4d5388456d295958dd42");
    expect(() => deriveProviderDocumentName("not-a-document-id")).toThrow(
      "collaboration_document_id_invalid",
    );
  });

  it("projects initial connection and reconnect states deterministically", () => {
    expect(projectProviderStatus(WebSocketStatus.Connecting, false)).toBe(
      "connecting",
    );
    expect(projectProviderStatus(WebSocketStatus.Connected, false)).toBe(
      "connected",
    );
    expect(projectProviderStatus(WebSocketStatus.Disconnected, true)).toBe(
      "reconnecting",
    );
  });
});
