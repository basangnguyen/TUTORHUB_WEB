import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  acceptMembershipInvitation,
  archiveClass,
  archiveTenant,
  beginIdentityLink,
  bulkMutateClassRoster,
  createClass,
  createClassEnrollment,
  createClassInviteCode,
  createMembershipInvitation,
  createTenant,
  getClass,
  getCurrentUser,
  getHealth,
  getLoginURL,
  getProfile,
  getTenant,
  getTenantCapabilities,
  issueClassMediaToken,
  joinClassInvitation,
  leaveClass,
  listClassInviteCodes,
  listClassRoster,
  listAuditEvents,
  listIdentities,
  listClasses,
  listMembershipInvitations,
  listTenants,
  logout,
  recordClassMediaEvent,
  previewMembershipInvitation,
  removeClassEnrollment,
  restoreClass,
  revokeClassInviteCode,
  revokeMembershipInvitation,
  rotateCSRFToken,
  switchActiveTenant,
  suspendClassEnrollment,
  transferClassOwnership,
  unlinkIdentity,
  updateClass,
  updateClassRosterRole,
  updateProfile,
  updateTenant,
  updateTenantFeatureControls,
} from "./index";
import type {
  ClassEnrollment,
  ClassInviteCode,
  ClassroomClass,
  CurrentUser,
  TenantCapabilities,
  UpdateClassRequest,
  UpdateTenantFeatureControlsRequest,
  UpdateTenantRequest,
} from "./index";

// @ts-expect-error expected_version alone is not a meaningful tenant mutation.
const invalidTenantUpdate: UpdateTenantRequest = { expected_version: 1 };
void invalidTenantUpdate;

// @ts-expect-error expected_version alone is not a meaningful class mutation.
const invalidClassUpdate: UpdateClassRequest = { expected_version: 1 };
void invalidClassUpdate;

const enrollmentLeavePermission: CurrentUser["permissions"][number] =
  "enrollment.leave";
const auditViewPermission: CurrentUser["permissions"][number] = "audit.view";
void auditViewPermission;

describe("listAuditEvents", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("binds tenant, filters, cursor, and abort signal to the generated contract", async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          items: [
            {
              id: "8f28f833-bc0e-47ce-b291-73aa0a926a36",
              tenant_id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
              actor: {
                type: "user",
                user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
                display_name: "Admin",
              },
              action: "class.enrollment.update_role",
              resource: {
                type: "class_enrollment",
                id: "20e36f1b-0b74-47d9-a942-3af1b3ee6356",
              },
              outcome: "succeeded",
              request_id: "request-123",
              metadata: { effect: "updated" },
              occurred_at: "2026-07-19T08:00:00Z",
            },
          ],
          next_cursor: "next-page",
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    await expect(
      listAuditEvents(
        "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
        {
          occurredFrom: "2026-07-01T00:00:00Z",
          occurredTo: "2026-08-01T00:00:00Z",
          action: "class.enrollment.update_role",
          resourceType: "class_enrollment",
          resourceID: "20e36f1b-0b74-47d9-a942-3af1b3ee6356",
          outcome: "succeeded",
          limit: 25,
          cursor: "page-one",
        },
        {
          baseUrl: "http://localhost/api",
          fetch: fetchMock,
          signal: controller.signal,
        },
      ),
    ).resolves.toMatchObject({ next_cursor: "next-page" });

    const request = fetchMock.mock.calls[0]?.[0] as Request;
    const url = new URL(request.url);
    expect(url.pathname).toBe(
      "/api/v1/tenants/4b18543a-74de-419f-9fe8-d0c3dfc991eb/audit-events",
    );
    expect(Object.fromEntries(url.searchParams)).toEqual({
      occurred_from: "2026-07-01T00:00:00Z",
      occurred_to: "2026-08-01T00:00:00Z",
      action: "class.enrollment.update_role",
      resource_type: "class_enrollment",
      resource_id: "20e36f1b-0b74-47d9-a942-3af1b3ee6356",
      outcome: "succeeded",
      limit: "25",
      cursor: "page-one",
    });
    controller.abort();
    expect(request.signal.aborted).toBe(true);
  });
});

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
          permissions: [enrollmentLeavePermission],
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
    ).resolves.toMatchObject({
      user: { email: "student@example.com" },
      permissions: ["enrollment.leave"],
    });
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

  it("gọi tenant lifecycle APIs với path, version và CSRF chính xác", async () => {
    const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
    const tenant = {
      id: tenantID,
      slug: "tutorhub-test",
      name: "TutorHub Test",
      locale: "vi",
      timezone: "Asia/Ho_Chi_Minh",
      status: "active" as const,
      version: 3,
      role: "org_admin" as const,
      is_active: true,
      created_at: "2026-07-18T01:00:00Z",
      updated_at: "2026-07-18T02:00:00Z",
      archived_at: null,
    };
    const updatedTenant = {
      ...tenant,
      slug: "tutorhub-engineering",
      name: "TutorHub Engineering",
      locale: "en",
      timezone: "Asia/Singapore",
      version: 4,
      updated_at: "2026-07-18T03:00:00Z",
    };
    const archivedPrincipal = {
      user: {
        id: "be85eb92-0f18-4163-85ba-50e4d343d632",
        email: "owner@example.com",
        display_name: "Owner",
        locale: "vi",
        timezone: "Asia/Ho_Chi_Minh",
      },
      active_tenant: null,
      memberships: [],
      permissions: [],
    };
    const responses = [
      new Response(JSON.stringify({ items: [tenant] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(tenant), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(updatedTenant), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(archivedPrincipal), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(listTenants(options)).resolves.toEqual({ items: [tenant] });
    await expect(getTenant(tenantID, options)).resolves.toEqual(tenant);
    await expect(
      updateTenant(
        tenantID,
        {
          expected_version: 3,
          name: "TutorHub Engineering",
          slug: "tutorhub-engineering",
          locale: "en",
          timezone: "Asia/Singapore",
        },
        "update-csrf",
        options,
      ),
    ).resolves.toEqual(updatedTenant);
    await expect(
      archiveTenant(tenantID, { expected_version: 4 }, "archive-csrf", options),
    ).resolves.toEqual(archivedPrincipal);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests[0]?.method).toBe("GET");
    expect(requests[0]?.url).toBe("http://localhost/api/v1/tenants");
    expect(requests[1]?.method).toBe("GET");
    expect(requests[1]?.url).toBe(
      `http://localhost/api/v1/tenants/${tenantID}`,
    );
    expect(requests[2]?.method).toBe("PATCH");
    expect(requests[2]?.url).toBe(
      `http://localhost/api/v1/tenants/${tenantID}`,
    );
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("update-csrf");
    await expect(requests[2]?.clone().json()).resolves.toEqual({
      expected_version: 3,
      name: "TutorHub Engineering",
      slug: "tutorhub-engineering",
      locale: "en",
      timezone: "Asia/Singapore",
    });
    expect(requests[3]?.method).toBe("POST");
    expect(requests[3]?.url).toBe(
      `http://localhost/api/v1/tenants/${tenantID}/archive`,
    );
    expect(requests[3]?.headers.get("X-CSRF-Token")).toBe("archive-csrf");
    await expect(requests[3]?.clone().json()).resolves.toEqual({
      expected_version: 4,
    });
  });

  it("gets tenant capabilities and updates feature controls with CSRF", async () => {
    const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
    const capabilities: TenantCapabilities = {
      tenant_id: tenantID,
      version: 7,
      can_manage_overrides: true,
      features: {
        membership_invitations: { enabled: true },
        class_management: { enabled: true },
        class_invite_links: { enabled: false },
      },
      quotas: {
        members: { limit: 100, used: 12, remaining: 88 },
        active_classes: { limit: 20, used: 4, remaining: 16 },
        invite_creations_per_hour: {
          limit: 30,
          used: 30,
          remaining: 0,
          reset_at: "2026-07-20T12:00:00Z",
        },
      },
      operations: {
        create_membership_invitation: {
          available: false,
          reason: "rate_limited",
        },
        accept_membership_invitation: {
          available: true,
          reason: "available",
        },
        create_class: { available: true, reason: "available" },
        activate_class: { available: true, reason: "available" },
        restore_active_class: { available: true, reason: "available" },
        create_class_invite_link: {
          available: false,
          reason: "feature_disabled",
        },
        join_class_invite_link: {
          available: false,
          reason: "feature_disabled",
        },
      },
    };
    const input: UpdateTenantFeatureControlsRequest = {
      expected_version: 7,
      features: {
        membership_invitations: true,
        class_management: true,
        class_invite_links: true,
      },
      quotas: {
        members: 120,
        active_classes: 25,
        invite_creations_per_hour: 40,
      },
    };
    const updatedCapabilities: TenantCapabilities = {
      ...capabilities,
      version: 8,
      features: {
        ...capabilities.features,
        class_invite_links: { enabled: true },
      },
      quotas: {
        members: { limit: 120, used: 12, remaining: 108 },
        active_classes: { limit: 25, used: 4, remaining: 21 },
        invite_creations_per_hour: {
          limit: 40,
          used: 30,
          remaining: 10,
          reset_at: "2026-07-20T12:00:00Z",
        },
      },
      operations: {
        ...capabilities.operations,
        create_membership_invitation: {
          available: true,
          reason: "available",
        },
        create_class_invite_link: {
          available: true,
          reason: "available",
        },
        join_class_invite_link: {
          available: true,
          reason: "available",
        },
      },
    };
    const responses = [
      new Response(JSON.stringify(capabilities), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(updatedCapabilities), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(getTenantCapabilities(tenantID, options)).resolves.toEqual(
      capabilities,
    );
    await expect(
      updateTenantFeatureControls(
        tenantID,
        input,
        "feature-controls-csrf",
        options,
      ),
    ).resolves.toEqual(updatedCapabilities);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.credentials)).toEqual([
      "include",
      "include",
    ]);
    expect(requests[0]?.method).toBe("GET");
    expect(requests[0]?.url).toBe(
      `http://localhost/api/v1/tenants/${tenantID}/capabilities`,
    );
    expect(requests[0]?.headers.get("X-CSRF-Token")).toBeNull();
    expect(requests[1]?.method).toBe("PUT");
    expect(requests[1]?.url).toBe(
      `http://localhost/api/v1/tenants/${tenantID}/feature-controls`,
    );
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe(
      "feature-controls-csrf",
    );
    await expect(requests[1]?.clone().json()).resolves.toEqual(input);
  });

  it("keeps membership invitation tokens in POST bodies and never request URLs", async () => {
    const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
    const invitationID = "711ca438-a597-49a3-ab21-e5ae04391883";
    const token = `thinv1_${"A".repeat(43)}`;
    const invitation = {
      id: invitationID,
      tenant_id: tenantID,
      email: "teacher@example.com",
      intended_role: "teacher" as const,
      status: "pending" as const,
      expires_at: "2026-07-25T04:00:00Z",
      accepted_at: null,
      revoked_at: null,
      created_at: "2026-07-18T04:00:00Z",
      updated_at: "2026-07-18T04:00:00Z",
    };
    const currentUser = {
      user: {
        id: "be85eb92-0f18-4163-85ba-50e4d343d632",
        email: "teacher@example.com",
        display_name: "Teacher",
        locale: "vi",
        timezone: "Asia/Ho_Chi_Minh",
      },
      active_tenant: null,
      memberships: [],
      permissions: [],
    };
    const responses = [
      new Response(JSON.stringify({ items: [invitation] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(
        JSON.stringify({
          invitation,
          accept_url: `https://web.example/invite#token=${token}`,
        }),
        {
          status: 201,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(
        JSON.stringify({ ...invitation, status: "revoked" as const }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(
        JSON.stringify({
          tenant_name: "TutorHub Test",
          masked_email: "t*****r@example.com",
          intended_role: "teacher",
          status: "pending",
          expires_at: invitation.expires_at,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(
        JSON.stringify({
          invitation: {
            ...invitation,
            status: "accepted",
            accepted_at: "2026-07-18T05:00:00Z",
          },
          current_user: currentUser,
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(listMembershipInvitations(tenantID, options)).resolves.toEqual(
      { items: [invitation] },
    );
    await expect(
      createMembershipInvitation(
        tenantID,
        { email: invitation.email, intended_role: "teacher" },
        "create-csrf",
        options,
      ),
    ).resolves.toMatchObject({ invitation: { id: invitationID } });
    await expect(
      revokeMembershipInvitation(
        tenantID,
        invitationID,
        "revoke-csrf",
        options,
      ),
    ).resolves.toMatchObject({ status: "revoked" });
    await expect(
      previewMembershipInvitation({ token }, options),
    ).resolves.toMatchObject({ masked_email: "t*****r@example.com" });
    await expect(
      acceptMembershipInvitation({ token }, "accept-csrf", options),
    ).resolves.toMatchObject({ current_user: currentUser });

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.url)).toEqual([
      `http://localhost/api/v1/tenants/${tenantID}/invitations`,
      `http://localhost/api/v1/tenants/${tenantID}/invitations`,
      `http://localhost/api/v1/tenants/${tenantID}/invitations/${invitationID}/revoke`,
      "http://localhost/api/v1/membership-invitations/preview",
      "http://localhost/api/v1/membership-invitations/accept",
    ]);
    expect(requests.every((request) => !request.url.includes(token))).toBe(
      true,
    );
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe("create-csrf");
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("revoke-csrf");
    expect(requests[4]?.headers.get("X-CSRF-Token")).toBe("accept-csrf");
    await expect(requests[3]?.clone().json()).resolves.toEqual({ token });
    await expect(requests[4]?.clone().json()).resolves.toEqual({ token });
  });

  it("gọi class lifecycle APIs theo contract tenant-scoped và versioned", async () => {
    const classItem: ClassroomClass = {
      id: "a912f628-f3d2-4c18-84c6-42a9e858dc8d",
      owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      code: "SEC101",
      title: "An toàn thông tin",
      description: "Lớp học kỳ 1",
      timezone: "Asia/Ho_Chi_Minh",
      status: "draft" as const,
      version: 1,
      viewer_access: {
        class_role: "owner",
        enrollment_status: null,
        can_update_class: true,
        can_archive_class: true,
        can_transfer_ownership: true,
        can_manage_enrollments: true,
        can_join_room: false,
        can_publish_media: false,
        can_leave: false,
      },
      created_at: "2026-07-14T04:00:00Z",
      updated_at: "2026-07-14T04:00:00Z",
      archived_at: null,
    };
    const responses = [
      new Response(
        JSON.stringify({ items: [classItem], next_cursor: "next-page" }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(JSON.stringify(classItem), {
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
      ...Array.from(
        { length: 4 },
        () =>
          new Response(JSON.stringify(classItem), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(
      listClasses(
        { status: "active", limit: 25, cursor: "current-page" },
        options,
      ),
    ).resolves.toEqual({
      items: [classItem],
      next_cursor: "next-page",
    });
    await expect(getClass(classItem.id, options)).resolves.toEqual(classItem);
    await expect(
      createClass(
        {
          code: "SEC101",
          title: "An toàn thông tin",
          description: "Lớp học kỳ 1",
          timezone: "Asia/Ho_Chi_Minh",
        },
        "create-csrf",
        options,
      ),
    ).resolves.toEqual(classItem);
    await expect(
      updateClass(
        classItem.id,
        {
          expected_version: 1,
          title: "An toàn thông tin nâng cao",
          status: "active",
        },
        "update-csrf",
        options,
      ),
    ).resolves.toEqual(classItem);
    await expect(
      archiveClass(
        classItem.id,
        { expected_version: 2 },
        "archive-csrf",
        options,
      ),
    ).resolves.toEqual(classItem);
    await expect(
      restoreClass(
        classItem.id,
        { expected_version: 3 },
        "restore-csrf",
        options,
      ),
    ).resolves.toEqual(classItem);
    await expect(
      transferClassOwnership(
        classItem.id,
        {
          expected_version: 4,
          new_owner_user_id: "0ca09673-415a-447c-90be-4b7639f76838",
        },
        "transfer-csrf",
        options,
      ),
    ).resolves.toEqual(classItem);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    const listURL = new URL(requests[0]?.url ?? "");
    expect(listURL.pathname).toBe("/api/v1/classes");
    expect(Object.fromEntries(listURL.searchParams)).toEqual({
      status: "active",
      limit: "25",
      cursor: "current-page",
    });
    expect(requests[1]?.url).toBe(
      `http://localhost/api/v1/classes/${classItem.id}`,
    );
    expect(requests[2]?.method).toBe("POST");
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("create-csrf");
    await expect(requests[2]?.clone().json()).resolves.toEqual({
      code: "SEC101",
      title: "An toàn thông tin",
      description: "Lớp học kỳ 1",
      timezone: "Asia/Ho_Chi_Minh",
    });
    expect(requests.slice(3).map((request) => request.url)).toEqual([
      `http://localhost/api/v1/classes/${classItem.id}`,
      `http://localhost/api/v1/classes/${classItem.id}/archive`,
      `http://localhost/api/v1/classes/${classItem.id}/restore`,
      `http://localhost/api/v1/classes/${classItem.id}/transfer-ownership`,
    ]);
    expect(requests[3]?.method).toBe("PATCH");
    expect(requests[3]?.headers.get("X-CSRF-Token")).toBe("update-csrf");
    await expect(requests[3]?.clone().json()).resolves.toEqual({
      expected_version: 1,
      title: "An toàn thông tin nâng cao",
      status: "active",
    });
    expect(requests[4]?.headers.get("X-CSRF-Token")).toBe("archive-csrf");
    await expect(requests[4]?.clone().json()).resolves.toEqual({
      expected_version: 2,
    });
    expect(requests[5]?.headers.get("X-CSRF-Token")).toBe("restore-csrf");
    await expect(requests[5]?.clone().json()).resolves.toEqual({
      expected_version: 3,
    });
    expect(requests[6]?.headers.get("X-CSRF-Token")).toBe("transfer-csrf");
    await expect(requests[6]?.clone().json()).resolves.toEqual({
      expected_version: 4,
      new_owner_user_id: "0ca09673-415a-447c-90be-4b7639f76838",
    });
  });

  it("manages direct enrollment transitions and self-leave with CSRF", async () => {
    const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
    const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
    const activeEnrollment: ClassEnrollment = {
      id: "63af7268-58db-4d40-a96f-c4f473a92350",
      class_id: classID,
      user_id: userID,
      class_role: "student",
      status: "active",
      enrolled_by: "be85eb92-0f18-4163-85ba-50e4d343d632",
      joined_at: "2026-07-19T02:00:00Z",
      suspended_at: null,
      left_at: null,
      removed_at: null,
      created_at: "2026-07-19T02:00:00Z",
      updated_at: "2026-07-19T02:00:00Z",
    };
    const suspendedEnrollment: ClassEnrollment = {
      ...activeEnrollment,
      status: "suspended",
      suspended_at: "2026-07-19T03:00:00Z",
      updated_at: "2026-07-19T03:00:00Z",
    };
    const removedEnrollment: ClassEnrollment = {
      ...activeEnrollment,
      status: "removed",
      removed_at: "2026-07-19T04:00:00Z",
      updated_at: "2026-07-19T04:00:00Z",
    };
    const leftEnrollment: ClassEnrollment = {
      ...activeEnrollment,
      status: "left",
      left_at: "2026-07-19T05:00:00Z",
      updated_at: "2026-07-19T05:00:00Z",
    };
    const responses = [
      new Response(JSON.stringify(activeEnrollment), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(suspendedEnrollment), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(removedEnrollment), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(JSON.stringify(leftEnrollment), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(
      createClassEnrollment(
        classID,
        { member_email: "student@example.com" },
        "enroll-csrf",
        options,
      ),
    ).resolves.toEqual(activeEnrollment);
    await expect(
      suspendClassEnrollment(classID, userID, "suspend-csrf", options),
    ).resolves.toEqual(suspendedEnrollment);
    await expect(
      removeClassEnrollment(classID, userID, "remove-csrf", options),
    ).resolves.toEqual(removedEnrollment);
    await expect(leaveClass(classID, "leave-csrf", options)).resolves.toEqual(
      leftEnrollment,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.url)).toEqual([
      `http://localhost/api/v1/classes/${classID}/enrollments`,
      `http://localhost/api/v1/classes/${classID}/enrollments/${userID}/suspend`,
      `http://localhost/api/v1/classes/${classID}/enrollments/${userID}/remove`,
      `http://localhost/api/v1/classes/${classID}/leave`,
    ]);
    expect(
      requests.map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual(["enroll-csrf", "suspend-csrf", "remove-csrf", "leave-csrf"]);
    await expect(requests[0]?.clone().json()).resolves.toEqual({
      member_email: "student@example.com",
    });
  });

  it("keeps class invite bearer tokens in the join body and out of request URLs", async () => {
    const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
    const codeID = "72299f70-e556-4878-b66d-c2dab2a2492f";
    const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
    const token = `thciv1_${"B".repeat(43)}`;
    const inviteCode: ClassInviteCode = {
      id: codeID,
      class_id: classID,
      status: "active",
      expires_at: "2026-07-26T02:00:00Z",
      usage_limit: 25,
      usage_count: 0,
      created_by: "be85eb92-0f18-4163-85ba-50e4d343d632",
      revoked_at: null,
      created_at: "2026-07-19T02:00:00Z",
      updated_at: "2026-07-19T02:00:00Z",
    };
    const enrollment: ClassEnrollment = {
      id: "63af7268-58db-4d40-a96f-c4f473a92350",
      class_id: classID,
      user_id: userID,
      class_role: "student",
      status: "active",
      enrolled_by: userID,
      joined_at: "2026-07-19T03:00:00Z",
      suspended_at: null,
      left_at: null,
      removed_at: null,
      created_at: "2026-07-19T03:00:00Z",
      updated_at: "2026-07-19T03:00:00Z",
    };
    const classroom: ClassroomClass = {
      id: classID,
      owner_user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      code: "SEC101",
      title: "Information Security",
      description: "Class joined with a bearer invite token.",
      timezone: "Asia/Ho_Chi_Minh",
      status: "active",
      version: 2,
      viewer_access: {
        class_role: "student",
        enrollment_status: "active",
        can_update_class: false,
        can_archive_class: false,
        can_transfer_ownership: false,
        can_manage_enrollments: false,
        can_join_room: true,
        can_publish_media: true,
        can_leave: true,
      },
      created_at: "2026-07-18T02:00:00Z",
      updated_at: "2026-07-19T03:00:00Z",
      archived_at: null,
    };
    const responses = [
      new Response(JSON.stringify({ items: [inviteCode] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(
        JSON.stringify({
          invite_code: inviteCode,
          join_url: `https://web.example/class-invite#token=${token}`,
        }),
        {
          status: 201,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(
        JSON.stringify({
          ...inviteCode,
          status: "revoked",
          revoked_at: "2026-07-19T04:00:00Z",
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
      new Response(JSON.stringify({ classroom, enrollment, joined: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(listClassInviteCodes(classID, options)).resolves.toEqual({
      items: [inviteCode],
    });
    await expect(
      createClassInviteCode(
        classID,
        { expires_in_seconds: 604800, usage_limit: 25 },
        "create-code-csrf",
        options,
      ),
    ).resolves.toMatchObject({ invite_code: inviteCode });
    await expect(
      revokeClassInviteCode(classID, codeID, "revoke-code-csrf", options),
    ).resolves.toMatchObject({ status: "revoked" });
    await expect(
      joinClassInvitation({ token }, "join-csrf", options),
    ).resolves.toEqual({ classroom, enrollment, joined: true });

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => request.url)).toEqual([
      `http://localhost/api/v1/classes/${classID}/invite-codes`,
      `http://localhost/api/v1/classes/${classID}/invite-codes`,
      `http://localhost/api/v1/classes/${classID}/invite-codes/${codeID}/revoke`,
      "http://localhost/api/v1/class-invitations/join",
    ]);
    expect(requests.every((request) => !request.url.includes(token))).toBe(
      true,
    );
    expect(requests[0]?.headers.get("X-CSRF-Token")).toBeNull();
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe("create-code-csrf");
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("revoke-code-csrf");
    expect(requests[3]?.headers.get("X-CSRF-Token")).toBe("join-csrf");
    await expect(requests[1]?.clone().json()).resolves.toEqual({
      expires_in_seconds: 604800,
      usage_limit: 25,
    });
    await expect(requests[3]?.clone().json()).resolves.toEqual({ token });
    expect(JSON.stringify(inviteCode)).not.toContain(token);
    expect(JSON.stringify(inviteCode)).not.toContain("hash");
  });

  it("lists and mutates a class roster through the tenant-bound contract", async () => {
    const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";
    const ownerID = "be85eb92-0f18-4163-85ba-50e4d343d632";
    const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
    const enrollment: ClassEnrollment = {
      id: "63af7268-58db-4d40-a96f-c4f473a92350",
      class_id: classID,
      user_id: userID,
      class_role: "student",
      status: "active",
      enrolled_by: ownerID,
      joined_at: "2026-07-19T03:00:00Z",
      suspended_at: null,
      left_at: null,
      removed_at: null,
      created_at: "2026-07-19T03:00:00Z",
      updated_at: "2026-07-19T03:00:00Z",
    };
    const page = {
      class_owner: {
        user: {
          id: ownerID,
          display_name: "Owner",
          email: "owner@example.test",
        },
        class_role: "owner" as const,
      },
      items: [
        {
          user: {
            id: userID,
            display_name: "Student",
            email: "student@example.test",
          },
          enrollment,
          actions: {
            assignable_roles: ["teaching_assistant" as const],
            can_suspend: true,
            can_remove: true,
          },
        },
      ],
      next_cursor: "thro1_next",
    };
    const updatedEnrollment = {
      ...enrollment,
      class_role: "teaching_assistant" as const,
    };
    const bulkResponse = {
      action: "suspend" as const,
      items: [
        {
          user_id: userID,
          outcome: "failed" as const,
          enrollment: null,
          failure: { code: "conflict" as const, detail: "State changed." },
        },
      ],
      requested_count: 1,
      updated_count: 0,
      unchanged_count: 0,
      failed_count: 1,
    };
    const responses = [
      new Response(JSON.stringify(page), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
      new Response(
        JSON.stringify({ outcome: "updated", enrollment: updatedEnrollment }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
      new Response(JSON.stringify(bulkResponse), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(responses.shift()));
    const options = { baseUrl: "http://localhost/api", fetch: fetchMock };

    await expect(
      listClassRoster(
        classID,
        {
          cursor: "thro1_current",
          limit: 25,
          search: "student one",
          status: "active",
        },
        options,
      ),
    ).resolves.toEqual(page);
    await expect(
      updateClassRosterRole(
        classID,
        userID,
        { class_role: "teaching_assistant" },
        "role-csrf",
        options,
      ),
    ).resolves.toMatchObject({ outcome: "updated" });
    await expect(
      bulkMutateClassRoster(
        classID,
        { action: "suspend", user_ids: [userID] },
        "bulk-csrf",
        options,
      ),
    ).resolves.toEqual(bulkResponse);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    const rosterURL = new URL(requests[0]?.url ?? "http://invalid");
    expect(rosterURL.pathname).toBe(`/api/v1/classes/${classID}/roster`);
    expect(Object.fromEntries(rosterURL.searchParams)).toEqual({
      cursor: "thro1_current",
      limit: "25",
      search: "student one",
      status: "active",
    });
    expect(requests[1]?.method).toBe("PATCH");
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe("role-csrf");
    await expect(requests[1]?.clone().json()).resolves.toEqual({
      class_role: "teaching_assistant",
    });
    expect(requests[2]?.url).toBe(
      `http://localhost/api/v1/classes/${classID}/roster/bulk`,
    );
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("bulk-csrf");
    await expect(requests[2]?.clone().json()).resolves.toEqual({
      action: "suspend",
      user_ids: [userID],
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
