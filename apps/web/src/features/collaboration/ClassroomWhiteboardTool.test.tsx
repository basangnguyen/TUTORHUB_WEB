// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { APIRequestError, type WhiteboardDocument } from "@tutorhub/api-client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import { ClassroomWhiteboardTool } from "./ClassroomWhiteboardTool";

const whiteboardMocks = vi.hoisted(() => ({
  prepare: vi.fn(),
  tool: vi.fn(),
  transition: vi.fn(),
}));

vi.mock("../../app/whiteboards", () => ({
  usePrepareWhiteboard: whiteboardMocks.prepare,
  useTransitionWhiteboard: whiteboardMocks.transition,
  useWhiteboardTool: whiteboardMocks.tool,
}));

vi.mock("./LazyWhiteboardEngine", () => ({
  default: ({ capability }: { capability: string }) => (
    <p>Canonical engine capability: {capability}</p>
  ),
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

describe("ClassroomWhiteboardTool", () => {
  beforeEach(() => {
    whiteboardMocks.prepare.mockReturnValue({
      isError: false,
      isPending: false,
      mutate: vi.fn(),
    });
    whiteboardMocks.transition.mockReturnValue({
      isError: false,
      isPending: false,
      mutate: vi.fn(),
    });
  });

  afterEach(() => cleanup());

  it("projects a disabled tool as feature-off without querying the server", () => {
    whiteboardMocks.tool.mockReturnValue({
      isError: false,
      isPending: true,
    });

    render(
      <I18nProvider initialLanguage="en">
        <ClassroomWhiteboardTool
          actorID="b0f39fdb-0789-454d-8517-ae313dd81ca9"
          enabled={false}
          mediaSpaceID="0e0f92c4-347c-4022-b4d1-c81208a972e3"
          tenantID="2a388dc1-e3a5-4888-8d65-d9bd57fd4fc7"
        />
      </I18nProvider>,
    );

    expect(screen.getByRole("status")).toHaveTextContent(
      "Whiteboard is currently off for this environment.",
    );
    expect(whiteboardMocks.tool).toHaveBeenLastCalledWith(
      "2a388dc1-e3a5-4888-8d65-d9bd57fd4fc7",
      "0e0f92c4-347c-4022-b4d1-c81208a972e3",
      false,
    );
  });

  it.each([
    [503, "Whiteboard is currently off for this environment."],
    [403, "You do not have access to this whiteboard."],
  ])("projects HTTP %i without exposing a retry action", (status, message) => {
    whiteboardMocks.tool.mockReturnValue({
      error: new APIRequestError(status),
      isError: true,
      isPending: false,
      refetch: vi.fn(),
    });

    renderTool();

    expect(screen.getByText(message)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Retry" }),
    ).not.toBeInTheDocument();
  });

  it("keeps loading and retry states keyboard reachable", () => {
    const refetch = vi.fn();
    whiteboardMocks.tool.mockReturnValue({
      isError: false,
      isPending: true,
    });
    const view = renderTool();
    expect(screen.getByRole("status")).toHaveTextContent(
      "Checking whiteboard access",
    );

    whiteboardMocks.tool.mockReturnValue({
      error: new Error("bounded failure"),
      isError: true,
      isPending: false,
      refetch,
    });
    view.rerender(toolElement());
    const retry = screen.getByRole("button", { name: "Retry" });
    retry.focus();
    expect(retry).toHaveFocus();
    retry.click();
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("uses only server-projected create permission in the empty state", () => {
    whiteboardMocks.tool.mockReturnValue({
      data: { can_create: false, document: null },
      isError: false,
      isPending: false,
    });
    const view = renderTool();
    expect(
      screen.queryByRole("button", { name: "Prepare whiteboard" }),
    ).not.toBeInTheDocument();

    whiteboardMocks.tool.mockReturnValue({
      data: { can_create: true, document: null },
      isError: false,
      isPending: false,
    });
    view.rerender(toolElement());
    expect(
      screen.getByRole("button", { name: "Prepare whiteboard" }),
    ).toBeInTheDocument();
  });

  it("passes the exact server-projected view capability to the lazy engine", async () => {
    whiteboardMocks.tool.mockReturnValue({
      data: { can_create: false, document },
      isError: false,
      isPending: false,
    });

    renderTool();

    expect(
      await screen.findByText("Canonical engine capability: view"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Suspend" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Presenting")).toBeInTheDocument();
  });
});

function renderTool() {
  return render(toolElement());
}

function toolElement() {
  return (
    <I18nProvider initialLanguage="en">
      <ClassroomWhiteboardTool
        actorID="b0f39fdb-0789-454d-8517-ae313dd81ca9"
        enabled
        mediaSpaceID="0e0f92c4-347c-4022-b4d1-c81208a972e3"
        tenantID="2a388dc1-e3a5-4888-8d65-d9bd57fd4fc7"
      />
    </I18nProvider>
  );
}
