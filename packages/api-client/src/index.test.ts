import { afterEach, describe, expect, it, vi } from "vitest";
import { getHealth } from "./index";

describe("getHealth", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("đọc health response hợp lệ", async () => {
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
          { status: 200 },
        ),
      ),
    );

    await expect(getHealth()).resolves.toMatchObject({ status: "ok" });
  });

  it("ném lỗi có status khi response thất bại", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 503 })),
    );

    await expect(getHealth()).rejects.toThrow("HTTP 503");
  });
});
