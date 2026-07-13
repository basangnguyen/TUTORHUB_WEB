import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("hiển thị trạng thái API sẵn sàng", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            status: "ok",
            service: "tutorhub-core-api",
            environment: "test",
            timestamp: "2026-07-12T00:00:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    render(<App />);

    expect(
      await screen.findByText(/TutorHub API đã sẵn sàng/),
    ).toBeInTheDocument();
  });

  it("hiển thị lỗi khi API không phản hồi", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new Error("Mất kết nối API.")),
    );

    render(<App />);

    expect(await screen.findByText("Mất kết nối API.")).toBeInTheDocument();
  });
});
