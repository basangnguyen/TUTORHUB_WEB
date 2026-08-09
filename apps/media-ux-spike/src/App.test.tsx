import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("isolated classroom media UX harness", () => {
  it("states its safety boundary and renders a bounded 25-person fixture", () => {
    render(<App />);

    expect(
      screen.getByRole("heading", {
        name: "Classroom media UX research harness",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(/Không credential/)).toBeInTheDocument();
    expect(screen.getByText("12 tile/rail đang hiển thị")).toBeInTheDocument();
    expect(screen.getByText("Giới hạn 12/25 video mock")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Trang sau" })).toBeEnabled();
  });

  it("switches to compact pagination and keeps controls reachable", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Hẹp 320px · 4 tile" }));
    expect(screen.getByText("4 tile/rail đang hiển thị")).toBeInTheDocument();
    expect(screen.getByText("Giới hạn 4/25 video mock")).toBeInTheDocument();
    expect(screen.getByText("1 / 7")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Trang sau" }));
    expect(screen.getByText("2 / 7")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Trang trước" })).toBeEnabled();
  });

  it("makes local pin override active speaker in the focused layout", () => {
    render(<App />);

    const pinButtons = screen.getAllByRole("button", { name: /^Ghim / });
    const pinCandidate = pinButtons[4];
    expect(pinCandidate).toBeDefined();
    fireEvent.click(pinCandidate as HTMLButtonElement);
    fireEvent.click(screen.getByRole("button", { name: "Người đang nói" }));

    const pressedPin = screen.getByRole("button", { name: /^Bỏ ghim / });
    expect(pressedPin).toHaveAttribute("aria-pressed", "true");
    expect(pressedPin.closest("article")).toHaveClass(
      "participant-tile--featured",
    );
    expect(screen.getByText("Giới hạn 7/25 video mock")).toBeInTheDocument();
  });

  it("keeps hand and reaction state keyboard-operable and bounded", () => {
    render(<App />);

    const handButton = screen.getByRole("button", { name: "Giơ tay" });
    fireEvent.click(handButton);
    expect(screen.getByText("seq 101")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Hạ tay của tôi" }));
    expect(screen.getByText("Chưa có ai giơ tay.")).toBeInTheDocument();

    const applause = screen.getByRole("button", { name: "Vỗ tay" });
    fireEvent.click(applause);
    fireEvent.click(applause);
    expect(screen.getByText("Vỗ tay × 2")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "+10 giây" }));
    expect(
      screen.getByText("Không có reaction đang hiển thị."),
    ).toBeInTheDocument();
  });

  it("labels effects as CSS-only and exposes deterministic fallback", () => {
    render(<App />);

    expect(
      screen.getByText(/không phải segmentation thật/i),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Rừng dịu" }));
    expect(screen.getByLabelText(/Preview mock: Rừng dịu/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "360p / 15fps" }));
    expect(
      screen.getByLabelText(/Preview mock: Không hiệu ứng, 360p\/15fps/),
    ).toBeInTheDocument();
  });
});
