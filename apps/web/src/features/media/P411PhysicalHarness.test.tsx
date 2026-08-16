import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import { P411PhysicalHarness } from "./P411PhysicalHarness";

const originalMediaDevices = Object.getOwnPropertyDescriptor(
  navigator,
  "mediaDevices",
);

afterEach(() => {
  if (originalMediaDevices) {
    Object.defineProperty(navigator, "mediaDevices", originalMediaDevices);
  } else {
    Reflect.deleteProperty(navigator, "mediaDevices");
  }
});

describe("P411PhysicalHarness", () => {
  it("does not capture during StrictMode mount and only probes after explicit preview", async () => {
    const getUserMedia = vi
      .fn()
      .mockRejectedValue(new DOMException("denied", "NotAllowedError"));
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
        addEventListener: vi.fn(),
        enumerateDevices: vi.fn().mockResolvedValue([]),
        getSupportedConstraints: vi.fn().mockReturnValue({}),
        getUserMedia,
        removeEventListener: vi.fn(),
      } satisfies Partial<MediaDevices>,
    });

    render(
      <StrictMode>
        <I18nProvider initialLanguage="vi">
          <P411PhysicalHarness localHostname="127.0.0.1" />
        </I18nProvider>
      </StrictMode>,
    );

    expect(getUserMedia).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Bắt đầu preview" }));
    await waitFor(() => expect(getUserMedia).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("status")).toHaveTextContent(
      "Preview chưa sẵn sàng",
    );
  });
});
