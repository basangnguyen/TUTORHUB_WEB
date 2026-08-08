import { afterEach, describe, expect, it, vi } from "vitest";
import { listHomeRecentFiles, searchAuthorizedResources } from "./index";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Home and search client", () => {
  it("binds recent files to the expected tenant header and limit", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [] }), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await listHomeRecentFiles("tenant-1", { limit: 4 }, { baseUrl: "/api" });

    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      "tenant-1",
    );
    expect(
      new URL(request.url, "https://tutorhub.test").searchParams.get("limit"),
    ).toBe("4");
  });

  it("sends search text only as an encoded query and keeps tenant scope", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [] }), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await searchAuthorizedResources(
      "tenant-2",
      { q: "100%_safe", limit: 12 },
      { baseUrl: "/api" },
    );

    const request = fetchMock.mock.calls[0]?.[0] as Request;
    const url = new URL(request.url, "https://tutorhub.test");
    expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      "tenant-2",
    );
    expect(url.searchParams.get("q")).toBe("100%_safe");
    expect(url.searchParams.get("limit")).toBe("12");
  });
});
