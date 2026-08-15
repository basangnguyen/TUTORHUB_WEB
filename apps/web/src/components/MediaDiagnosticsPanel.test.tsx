// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { MediaDiagnosticsPanel } from "./MediaDiagnosticsPanel";

const loadDiagnostics = vi.hoisted(() => vi.fn());

vi.mock("../app/mediaDiagnostics", () => ({
  loadMediaDiagnostics: loadDiagnostics,
}));

describe("MediaDiagnosticsPanel", () => {
  beforeEach(() => {
    loadDiagnostics.mockReset().mockResolvedValue({
      from: "2030-08-02T00:00:00Z",
      to: "2030-08-03T00:00:00Z",
      items: [],
      metrics: {
        join_attempts: 10,
        successful_joins: 9,
        join_success_rate: 0.9,
        p95_time_to_media_ms: 2400,
        reconnect_succeeded: 3,
        reconnect_failed: 1,
      },
      truncated: false,
    });
  });

  afterEach(() => cleanup());

  it("loads redacted aggregate metrics only on an explicit admin action", async () => {
    render(
      <I18nProvider initialLanguage="en">
        <MediaDiagnosticsPanel tenantID="tenant" />
      </I18nProvider>,
    );
    expect(loadDiagnostics).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("button", { name: "Load 24-hour metrics" }),
    );
    expect(await screen.findByText("90%")).toBeInTheDocument();
    expect(screen.getByText("2400 ms")).toBeInTheDocument();
    expect(screen.getByText("3/1")).toBeInTheDocument();
    expect(loadDiagnostics).toHaveBeenCalledWith("tenant");
    expect(
      screen.getByRole("button", { name: "Download redacted JSON" }),
    ).toBeInTheDocument();
  });

  it("exposes a retryable alert without leaking the failure", async () => {
    loadDiagnostics.mockRejectedValueOnce(new Error("private provider detail"));
    render(
      <I18nProvider initialLanguage="en">
        <MediaDiagnosticsPanel tenantID="tenant" />
      </I18nProvider>,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Load 24-hour metrics" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Diagnostics could not be loaded safely. Try again.",
    );
    expect(
      screen.queryByText("private provider detail"),
    ).not.toBeInTheDocument();
  });
});
