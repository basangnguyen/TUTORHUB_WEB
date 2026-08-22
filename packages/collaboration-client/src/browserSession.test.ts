import { WebSocketStatus } from "@hocuspocus/provider";
import { describe, expect, it } from "vitest";
import {
  classifyProviderClose,
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

  it("maps only bounded terminal close reasons to deterministic recovery actions", () => {
    expect(classifyProviderClose(4403, "authority_lost")).toBe(
      "authority_changed",
    );
    expect(classifyProviderClose(4403, "checkpoint_unavailable")).toBe(
      "recovery_required",
    );
    expect(classifyProviderClose(1008, "reconnect_storm_denied")).toBe(
      "reconnect_exhausted",
    );
    expect(classifyProviderClose(1006, "network_lost")).toBeNull();
    expect(classifyProviderClose(1000, "normal")).toBeNull();
  });
});
