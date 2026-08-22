// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { WhiteboardDocument } from "@tutorhub/api-client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import LazyWhiteboardEngine from "./LazyWhiteboardEngine";

const engineMocks = vi.hoisted(() => ({
  createSession: vi.fn(),
  grant: vi.fn(),
  status: null as null | ((status: string) => void),
  terminal: null as null | ((reason: string) => void),
}));

vi.mock("@excalidraw/excalidraw", () => ({
  Excalidraw: () => <div>Excalidraw canvas</div>,
}));

vi.mock("@tutorhub/collaboration-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/collaboration-client")>();
  return {
    ...original,
    createBrowserCollaborationSession: engineMocks.createSession,
  };
});

vi.mock("../../app/whiteboards", () => ({
  requestWhiteboardGrant: engineMocks.grant,
}));

const document: WhiteboardDocument = {
  created_at: "2030-08-22T00:00:00Z",
  current_generation: 1,
  id: "21aac229-f1f6-4f21-85cf-3ecfe6f43529",
  media_space_id: "0e0f92c4-347c-4022-b4d1-c81208a972e3",
  revoke_generation: 1,
  status: "open",
  updated_at: "2030-08-22T00:00:00Z",
  version: 1,
  viewer_capabilities: {
    can_close: false,
    can_create_snapshot: false,
    can_exchange_grant: true,
    can_export: false,
    can_open: false,
    can_restore: false,
    can_resume: false,
    can_suspend: false,
    capability: "view",
  },
};

const authority = {
  getProjection: () => ({
    appState: { viewBackgroundColor: "#ffffff" },
    elements: [],
    files: {},
    page: { backgroundColor: "#ffffff", id: "page-1", name: "Page 1" },
  }),
  getScene: () => ({ elements: [], files: {}, metadata: {}, page: {} }),
  getSemanticHash: () => "empty",
  subscribe: () => () => undefined,
};

describe("LazyWhiteboardEngine", () => {
  beforeEach(() => {
    engineMocks.status = null;
    engineMocks.terminal = null;
    engineMocks.grant.mockResolvedValue({
      capability: "view",
      credential: "opaque-credential-that-is-long-enough",
      document_id: document.id,
      expires_at: "2030-08-22T00:05:00Z",
      generation: 1,
      provider_url: "wss://collaboration.example.test",
      revoke_generation: 1,
    });
    engineMocks.createSession.mockImplementation(async (options) => {
      engineMocks.status = options.onStatus;
      engineMocks.terminal = options.onTerminal;
      options.onStatus?.("connected");
      return {
        authority,
        capability: "view",
        destroy: vi.fn(),
        generation: 1,
      };
    });
  });

  afterEach(() => cleanup());

  it("announces read-only and reconnect states while retaining the semantic fallback", async () => {
    render(
      <I18nProvider initialLanguage="en">
        <LazyWhiteboardEngine
          actorID="b0f39fdb-0789-454d-8517-ae313dd81ca9"
          capability="view"
          document={document}
          tenantID="2a388dc1-e3a5-4888-8d65-d9bd57fd4fc7"
        />
      </I18nProvider>,
    );

    expect(
      await screen.findByText(/Whiteboard connected.*View only/),
    ).toBeInTheDocument();
    expect(screen.getByText("The whiteboard is empty.")).toBeInTheDocument();
    expect(screen.getByText("Excalidraw canvas")).toBeInTheDocument();

    act(() => engineMocks.status?.("reconnecting"));
    expect(
      screen.getByText(/Restoring the whiteboard connection.*View only/),
    ).toBeInTheDocument();
  });

  it("requests a control-plane refresh after a terminal generation fence", async () => {
    const onRecoveryRequired = vi.fn();
    render(
      <I18nProvider initialLanguage="en">
        <LazyWhiteboardEngine
          actorID="b0f39fdb-0789-454d-8517-ae313dd81ca9"
          capability="view"
          document={document}
          onRecoveryRequired={onRecoveryRequired}
          tenantID="2a388dc1-e3a5-4888-8d65-d9bd57fd4fc7"
        />
      </I18nProvider>,
    );

    expect(await screen.findByText(/Whiteboard connected/)).toBeInTheDocument();
    act(() => engineMocks.terminal?.("authority_changed"));
    expect(onRecoveryRequired).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "The drawing tools could not connect",
    );
  });
});
