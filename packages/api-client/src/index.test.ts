import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  createTenant,
  getCurrentUser,
  getHealth,
  getLoginURL,
  logout,
  rotateCSRFToken,
  switchActiveTenant,
} from "./index";

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

  it("tạo login URL chỉ với return_to nội bộ", () => {
    expect(
      getLoginURL("/app/classes?filter=mine", {
        baseUrl: "https://web.example/api",
      }),
    ).toBe(
      "https://web.example/api/api/v1/auth/login?return_to=%2Fapp%2Fclasses%3Ffilter%3Dmine",
    );
  });

  it("gọi session và workspace APIs bằng cookie credentials", async () => {
    const activeUser = {
      user: {
        id: "be85eb92-0f18-4163-85ba-50e4d343d632",
        email: "student@example.com",
        display_name: "Student",
        locale: "vi",
        timezone: "Asia/Ho_Chi_Minh",
      },
      active_tenant: {
        id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
        slug: "tutorhub-test",
        name: "TutorHub Test",
        role: "org_admin",
        is_active: true,
      },
      memberships: [],
      permissions: ["tenant.manage"],
    };
    const responses = [
      new Response(
        JSON.stringify({
          user: {
            id: "be85eb92-0f18-4163-85ba-50e4d343d632",
            email: "student@example.com",
            display_name: "Student",
            locale: "vi",
            timezone: "Asia/Ho_Chi_Minh",
          },
          active_tenant: null,
          memberships: [],
          permissions: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
      new Response(JSON.stringify({ csrf_token: "csrf-token" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(
        JSON.stringify({ logout_url: "https://identity.example/logout" }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
      new Response(JSON.stringify(activeUser), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(activeUser), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));

    await expect(
      getCurrentUser({ baseUrl: "http://localhost/api", fetch: fetchMock }),
    ).resolves.toMatchObject({ user: { email: "student@example.com" } });
    await expect(
      rotateCSRFToken({ baseUrl: "http://localhost/api", fetch: fetchMock }),
    ).resolves.toEqual({ csrf_token: "csrf-token" });
    await expect(
      logout("csrf-token", {
        baseUrl: "http://localhost/api",
        fetch: fetchMock,
      }),
    ).resolves.toEqual({ logout_url: "https://identity.example/logout" });
    await expect(
      createTenant(
        { name: "TutorHub Test", slug: "tutorhub-test" },
        "create-csrf",
        { baseUrl: "http://localhost/api", fetch: fetchMock },
      ),
    ).resolves.toMatchObject({ active_tenant: { role: "org_admin" } });
    await expect(
      switchActiveTenant(
        "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
        "switch-csrf",
        { baseUrl: "http://localhost/api", fetch: fetchMock },
      ),
    ).resolves.toMatchObject({ active_tenant: { slug: "tutorhub-test" } });

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.credentials)).toEqual([
      "include",
      "include",
      "include",
      "include",
      "include",
    ]);
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("csrf-token");
    expect(requests[3]?.method).toBe("POST");
    expect(requests[3]?.headers.get("X-CSRF-Token")).toBe("create-csrf");
    await expect(requests[3]?.clone().json()).resolves.toEqual({
      name: "TutorHub Test",
      slug: "tutorhub-test",
    });
    expect(requests[4]?.method).toBe("PUT");
    expect(requests[4]?.headers.get("X-CSRF-Token")).toBe("switch-csrf");
    await expect(requests[4]?.clone().json()).resolves.toEqual({
      tenant_id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
    });
  });
});
