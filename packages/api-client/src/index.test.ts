import { afterEach, describe, expect, it, vi } from "vitest";
import { APIRequestError, getHealth } from "./index";

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

    await expect(
      getHealth({ baseUrl: "http://localhost/api" }),
    ).resolves.toMatchObject({ status: "ok" });
  });

  it("chuẩn hóa base URL tương đối cho trình duyệt và JSDOM", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: "ok",
          service: "tutorhub-core-api",
          environment: "test",
          timestamp: "2026-07-12T00:00:00Z",
        }),
        { status: 200 },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await getHealth({ baseUrl: "/api" });

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0]?.[0]).toBeInstanceOf(Request);
    expect((fetchMock.mock.calls[0]?.[0] as Request).url).toBe(
      "http://localhost/api/health",
    );
  });

  it("ném lỗi có status khi response thất bại", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 503 })),
    );

    const request = getHealth({ baseUrl: "http://localhost/api" });
    await expect(request).rejects.toThrow("HTTP 503");
    await expect(request).rejects.toBeInstanceOf(APIRequestError);
    await expect(request).rejects.toMatchObject({ status: 503 });
  });
});
