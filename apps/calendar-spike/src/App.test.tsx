import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("calendar spike surface", () => {
  it("renders an accessible agenda alternative and DST evidence", async () => {
    render(<App />);

    expect(
      screen.getByRole("heading", {
        name: /Calendar renderer & recurrence spike/i,
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("group", { name: "Chế độ xem lịch" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: "Chương trình thay thế cho thao tác kéo",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(/Từ chối:/)).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: /Dời sau 30 phút/ }).length,
    ).toBeGreaterThan(0);
  });
});
