import { fireEvent, render, screen } from "@testing-library/react";
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
    ).toBe(24);
    expect(screen.getByTestId("agenda-count")).toHaveTextContent(
      "Hiển thị 24/51 mục",
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Hiển thị thêm 24 mục" }),
    );
    expect(
      screen.getAllByRole("button", { name: /Dời sau 30 phút/ }).length,
    ).toBe(48);

    fireEvent.click(
      screen.getByRole("button", { name: "Hiển thị thêm 3 mục" }),
    );
    expect(
      screen.getAllByRole("button", { name: /Dời sau 30 phút/ }).length,
    ).toBe(51);
    expect(screen.getByTestId("agenda-count")).toHaveTextContent(
      "Hiển thị 51/51 mục",
    );
    expect(
      screen.queryByRole("button", { name: /Hiển thị thêm/ }),
    ).not.toBeInTheDocument();
  });
});
