import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  beginIdentityLink,
  createClass,
  createTenant,
  getClass,
  getCurrentUser,
  getHealth,
  getLoginURL,
  getProfile,
  issueClassMediaToken,
  listIdentities,
  listClasses,
  logout,
  recordClassMediaEvent,
  rotateCSRFToken,
  switchActiveTenant,
  unlinkIdentity,
  updateProfile,
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

  it("removes every trailing slash from the API base URL", async () => {
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

    await getHealth({
      baseUrl: "https://api.example.test////",
      fetch: fetchMock,
    });

    expect((fetchMock.mock.calls[0]?.[0] as Request).url).toBe(
      "https://api.example.test/health",
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
      "https://web.example/api/v1/auth/login?return_to=%2Fapp%2Fclasses%3Ffilter%3Dmine",
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

  it("gọi class list, detail và create theo contract tenant-scoped", async () => {
    const classItem = {
      id: "a912f628-f3d2-4c18-84c6-42a9e858dc8d",
      owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      code: "SEC101",
      title: "An toàn thông tin",
      description: "Lớp học kỳ 1",
      status: "draft" as const,
      created_at: "2026-07-14T04:00:00Z",
      updated_at: "2026-07-14T04:00:00Z",
    };
    const responses = [
      new Response(JSON.stringify({ items: [classItem] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(classItem), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(classItem), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(listClasses(25, options)).resolves.toEqual({
      items: [classItem],
    });
    await expect(getClass(classItem.id, options)).resolves.toEqual(classItem);
    await expect(
      createClass(
        {
          code: "SEC101",
          title: "An toàn thông tin",
          description: "Lớp học kỳ 1",
        },
        "csrf-token",
        options,
      ),
    ).resolves.toEqual(classItem);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests[0]?.url).toBe("http://localhost/api/v1/classes?limit=25");
    expect(requests[1]?.url).toBe(
      `http://localhost/api/v1/classes/${classItem.id}`,
    );
    expect(requests[2]?.method).toBe("POST");
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("csrf-token");
    await expect(requests[2]?.clone().json()).resolves.toEqual({
      code: "SEC101",
      title: "An toàn thông tin",
      description: "Lớp học kỳ 1",
    });
  });

  it("issues an in-memory media token and records bounded join telemetry", async () => {
    const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
    const attemptID = "16abf9c1-69af-44a7-a844-36a6a19e2db2";
    const credential = {
      access_token: "short-lived-livekit-token",
      server_url: "wss://staging.example.test",
      room_name: `th_tenant_${classID}`,
      participant_identity: "u_actor_s_session",
      participant_name: "Student",
      attempt_id: attemptID,
      can_publish: true,
      expires_at: "2026-07-14T05:05:00Z",
    };
    const responses = [
      new Response(JSON.stringify(credential), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(null, { status: 204 }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(
      issueClassMediaToken(classID, "token-csrf", options),
    ).resolves.toEqual(credential);
    await expect(
      recordClassMediaEvent(
        classID,
        {
          attempt_id: attemptID,
          stage: "connect",
          outcome: "succeeded",
          error_code: "",
          duration_ms: 842,
        },
        "event-csrf",
        options,
      ),
    ).resolves.toBeUndefined();

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests[0]?.url).toBe(
      `http://localhost/api/v1/classes/${classID}/media-token`,
    );
    expect(requests[0]?.headers.get("X-CSRF-Token")).toBe("token-csrf");
    expect(requests[1]?.url).toBe(
      `http://localhost/api/v1/classes/${classID}/media-events`,
    );
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe("event-csrf");
    await expect(requests[1]?.clone().json()).resolves.toEqual({
      attempt_id: attemptID,
      stage: "connect",
      outcome: "succeeded",
      error_code: "",
      duration_ms: 842,
    });
  });

  it("manages the current profile and linked identities", async () => {
    const user = {
      id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      email: "student@example.com",
      display_name: "Nguyen Ba Sang",
      locale: "vi" as const,
      timezone: "Asia/Ho_Chi_Minh",
      avatar_object_key:
        "avatars/be85eb92-0f18-4163-85ba-50e4d343d632/avatar.webp",
    };
    const identity = {
      id: "f25085c5-e88a-4859-a586-5a232032710a",
      provider: "zitadel",
      email: "student@example.com",
      email_verified: true,
      created_at: "2026-07-17T01:00:00Z",
      last_authenticated_at: "2026-07-17T02:00:00Z",
    };
    const responses = [
      new Response(JSON.stringify({ user }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(
        JSON.stringify({ user: { ...user, display_name: "Ba Sang" } }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(JSON.stringify({ identities: [identity] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(
        JSON.stringify({
          authorization_url: "https://identity.example/authorize?state=opaque",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(null, { status: 204 }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(getProfile(options)).resolves.toEqual({ user });
    await expect(
      updateProfile(
        {
          display_name: "Ba Sang",
          locale: "vi",
          timezone: "Asia/Ho_Chi_Minh",
          avatar_object_key: null,
        },
        "profile-csrf",
        options,
      ),
    ).resolves.toMatchObject({ user: { display_name: "Ba Sang" } });
    await expect(listIdentities(options)).resolves.toEqual({
      identities: [identity],
    });
    await expect(
      beginIdentityLink("link-csrf", options),
    ).resolves.toMatchObject({
      authorization_url: "https://identity.example/authorize?state=opaque",
    });
    await expect(
      unlinkIdentity(identity.id, "unlink-csrf", options),
    ).resolves.toBeUndefined();

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.url)).toEqual([
      "http://localhost/api/v1/me/profile",
      "http://localhost/api/v1/me/profile",
      "http://localhost/api/v1/me/identities",
      "http://localhost/api/v1/me/identities/link",
      `http://localhost/api/v1/me/identities/${identity.id}`,
    ]);
    expect(requests[1]?.method).toBe("PATCH");
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe("profile-csrf");
    await expect(requests[1]?.clone().json()).resolves.toEqual({
      display_name: "Ba Sang",
      locale: "vi",
      timezone: "Asia/Ho_Chi_Minh",
      avatar_object_key: null,
    });
    expect(requests[3]?.method).toBe("POST");
    expect(requests[3]?.headers.get("X-CSRF-Token")).toBe("link-csrf");
    expect(requests[4]?.method).toBe("DELETE");
    expect(requests[4]?.headers.get("X-CSRF-Token")).toBe("unlink-csrf");
  });
});
