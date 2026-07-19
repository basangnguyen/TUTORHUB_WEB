import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { CurrentUser, Tenant } from "@tutorhub/api-client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SessionProvider, useSession } from "./session";
import {
  tenantQueryKeys,
  useArchiveTenant,
  useTenantDetail,
  useTenantList,
  useUpdateTenant,
  useWorkspaceActions,
} from "./workspaces";

const user = {
  id: "be85eb92-0f18-4163-85ba-50e4d343d632",
  email: "teacher@example.com",
  display_name: "TutorHub Teacher",
  locale: "vi",
  timezone: "Asia/Ho_Chi_Minh",
};

const tenantA = {
  id: "4b18543a-74de-419f-9fe8-d0c3dfc991eb",
  slug: "workspace-a",
  name: "Workspace A",
  role: "teacher" as const,
  is_active: true,
  status: "active" as const,
  version: 1,
};

const tenantB = {
  id: "d53466c6-fb22-49bb-8dcb-e0399896a6c8",
  slug: "workspace-b",
  name: "Workspace B",
  role: "student" as const,
  is_active: true,
  status: "active" as const,
  version: 1,
};

const tenantC = {
  id: "8d08d79d-5b50-4ddf-bbe7-87b13654c908",
  slug: "workspace-c",
  name: "Workspace C",
  role: "teacher" as const,
  is_active: true,
  status: "active" as const,
  version: 1,
};

const tenantRecordA: Tenant = {
  ...tenantA,
  locale: "vi",
  timezone: "Asia/Ho_Chi_Minh",
  created_at: "2026-07-18T01:00:00Z",
  updated_at: "2026-07-18T02:00:00Z",
  archived_at: null,
};

function sessionFor(
  activeTenant: typeof tenantA | typeof tenantB | typeof tenantC | null,
): CurrentUser {
  return {
    user,
    active_tenant: activeTenant,
    memberships: [
      { ...tenantA, is_active: activeTenant?.id === tenantA.id },
      { ...tenantB, is_active: activeTenant?.id === tenantB.id },
      { ...tenantC, is_active: activeTenant?.id === tenantC.id },
    ],
    permissions: ["class.view"],
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function WorkspaceActionsProbe() {
  const session = useSession();
  const { createWorkspace, switchWorkspace } = useWorkspaceActions();

  return (
    <>
      <output>{session.currentUser?.active_tenant?.id ?? "none"}</output>
      <button
        onClick={() =>
          createWorkspace.mutate({ name: tenantA.name, slug: tenantA.slug })
        }
        type="button"
      >
        create
      </button>
      <button onClick={() => switchWorkspace.mutate(tenantB.id)} type="button">
        switch-b
      </button>
      <button onClick={() => switchWorkspace.mutate(tenantC.id)} type="button">
        switch-c
      </button>
    </>
  );
}

function TenantQueryProbe() {
  const session = useSession();
  const list = useTenantList();
  const detail = useTenantDetail(session.currentUser?.active_tenant?.id);

  return (
    <>
      <output data-testid="tenant-list-name">
        {list.data?.items[0]?.name ?? "loading-list"}
      </output>
      <output data-testid="tenant-detail-name">
        {detail.data?.name ?? "loading-detail"}
      </output>
    </>
  );
}

function TenantMutationProbe() {
  const session = useSession();
  const updateTenant = useUpdateTenant();
  const archiveTenant = useArchiveTenant();

  return (
    <>
      <output data-testid="active-tenant-name">
        {session.currentUser?.active_tenant
          ? `${session.currentUser.active_tenant.name}:${session.currentUser.active_tenant.version}`
          : "none"}
      </output>
      <button
        onClick={() =>
          updateTenant.mutate({
            tenantID: tenantA.id,
            input: {
              expected_version: tenantA.version,
              name: "Workspace A Updated",
            },
          })
        }
        type="button"
      >
        update-tenant
      </button>
      <button
        onClick={() =>
          archiveTenant.mutate({
            tenantID: tenantA.id,
            input: { expected_version: tenantA.version },
          })
        }
        type="button"
      >
        archive-tenant
      </button>
    </>
  );
}

function renderWorkspaceActions(
  queryClient: QueryClient,
  mode:
    { kind: "remote" } | { kind: "static"; currentUser: CurrentUser | null },
) {
  return render(
    <QueryClientProvider client={queryClient}>
      <SessionProvider mode={mode}>
        <WorkspaceActionsProbe />
      </SessionProvider>
    </QueryClientProvider>,
  );
}

function endpointCalls(fetchMock: ReturnType<typeof vi.fn>, endpoint: string) {
  return fetchMock.mock.calls.filter((call) =>
    (call[0] as Request).url.endsWith(endpoint),
  );
}

describe("workspace actions", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("uses the switch response immediately and removes tenant-scoped cache", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(["classes", tenantA.id, "list"], ["class-a"]);
    queryClient.setQueryData(["media", tenantA.id, "room"], "room-a");
    queryClient.setQueryData(["audit", tenantA.id, "list"], ["event-a"]);
    queryClient.setQueryData(["tenants", "mine"], [tenantA]);
    queryClient.setQueryData(["profile", "detail"], user);
    queryClient.setQueryData(["core-api", "health"], { status: "ok" });

    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/me")) {
        return Promise.resolve(jsonResponse(sessionFor(tenantA)));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (request.url.endsWith("/api/v1/session/active-tenant")) {
        return Promise.resolve(jsonResponse(sessionFor(tenantB)));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWorkspaceActions(queryClient, { kind: "remote" });
    expect(await screen.findByText(tenantA.id)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "switch-b" }));

    expect(await screen.findByText(tenantB.id)).toBeInTheDocument();
    expect(endpointCalls(fetchMock, "/api/v1/me")).toHaveLength(1);
    expect(
      queryClient.getQueryData(["classes", tenantA.id, "list"]),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(["media", tenantA.id, "room"]),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(["audit", tenantA.id, "list"]),
    ).toBeUndefined();
    expect(queryClient.getQueryData(["tenants", "mine"])).toBeUndefined();
    expect(queryClient.getQueryData(["profile", "detail"])).toEqual(user);
    expect(queryClient.getQueryData(["core-api", "health"])).toEqual({
      status: "ok",
    });
  });

  it("uses the create response without a mandatory session refetch", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const emptySession: CurrentUser = {
      user,
      active_tenant: null,
      memberships: [],
      permissions: [],
    };
    const createdSession = sessionFor(tenantA);
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/me")) {
        return Promise.resolve(jsonResponse(emptySession));
      }
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (request.url.endsWith("/api/v1/tenants")) {
        return Promise.resolve(jsonResponse(createdSession, 201));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWorkspaceActions(queryClient, { kind: "remote" });
    expect(await screen.findByText("none")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "create" }));

    expect(await screen.findByText(tenantA.id)).toBeInTheDocument();
    expect(endpointCalls(fetchMock, "/api/v1/me")).toHaveLength(1);
  });

  it("does not let an older rapid-switch response replace the latest principal", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");
    let resolveTenantB: ((response: Response) => void) | undefined;
    let resolveTenantC: ((response: Response) => void) | undefined;
    const tenantBResponse = new Promise<Response>((resolve) => {
      resolveTenantB = resolve;
    });
    const tenantCResponse = new Promise<Response>((resolve) => {
      resolveTenantC = resolve;
    });
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "csrf-token" });
      }
      if (request.url.endsWith("/api/v1/session/active-tenant")) {
        const body = (await request.clone().json()) as { tenant_id: string };
        return body.tenant_id === tenantB.id
          ? tenantBResponse
          : tenantCResponse;
      }
      throw new Error(`Unexpected request: ${request.url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWorkspaceActions(queryClient, {
      kind: "static",
      currentUser: sessionFor(tenantA),
    });
    fireEvent.click(screen.getByRole("button", { name: "switch-b" }));
    fireEvent.click(screen.getByRole("button", { name: "switch-c" }));

    await waitFor(() =>
      expect(
        endpointCalls(fetchMock, "/api/v1/session/active-tenant"),
      ).toHaveLength(2),
    );
    await act(async () => {
      resolveTenantC?.(jsonResponse(sessionFor(tenantC)));
    });
    expect(await screen.findByText(tenantC.id)).toBeInTheDocument();
    expect(cancelQueries).toHaveBeenCalledTimes(2);
    expect(cancelQueries).toHaveBeenCalledWith({
      exact: true,
      queryKey: ["auth", "me"],
    });

    await act(async () => {
      resolveTenantB?.(jsonResponse(sessionFor(tenantB)));
    });
    await waitFor(() =>
      expect(screen.getByText(tenantC.id)).toBeInTheDocument(),
    );
    expect(cancelQueries).toHaveBeenCalledTimes(2);
  });

  it("applies an older successful switch when every newer intent fails", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    let resolveTenantB: ((response: Response) => void) | undefined;
    let resolveTenantC: ((response: Response) => void) | undefined;
    const tenantBResponse = new Promise<Response>((resolve) => {
      resolveTenantB = resolve;
    });
    const tenantCResponse = new Promise<Response>((resolve) => {
      resolveTenantC = resolve;
    });
    const fetchMock = vi.fn().mockImplementation(async (request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return jsonResponse({ csrf_token: "csrf-token" });
      }
      if (request.url.endsWith("/api/v1/session/active-tenant")) {
        const body = (await request.clone().json()) as { tenant_id: string };
        return body.tenant_id === tenantB.id
          ? tenantBResponse
          : tenantCResponse;
      }
      throw new Error(`Unexpected request: ${request.url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWorkspaceActions(queryClient, {
      kind: "static",
      currentUser: sessionFor(tenantA),
    });
    fireEvent.click(screen.getByRole("button", { name: "switch-b" }));
    fireEvent.click(screen.getByRole("button", { name: "switch-c" }));

    await waitFor(() =>
      expect(
        endpointCalls(fetchMock, "/api/v1/session/active-tenant"),
      ).toHaveLength(2),
    );
    await act(async () => {
      resolveTenantB?.(jsonResponse(sessionFor(tenantB)));
    });
    expect(screen.getByText(tenantA.id)).toBeInTheDocument();

    await act(async () => {
      resolveTenantC?.(
        jsonResponse({ status: 409, title: "Workspace context changed" }, 409),
      );
    });
    expect(await screen.findByText(tenantB.id)).toBeInTheDocument();
  });

  it("loads the tenant list and active-tenant detail through scoped query keys", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith(`/api/v1/tenants/${tenantA.id}`)) {
        return Promise.resolve(jsonResponse(tenantRecordA));
      }
      if (request.url.endsWith("/api/v1/tenants")) {
        return Promise.resolve(jsonResponse({ items: [tenantRecordA] }));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <QueryClientProvider client={queryClient}>
        <SessionProvider
          mode={{ kind: "static", currentUser: sessionFor(tenantA) }}
        >
          <TenantQueryProbe />
        </SessionProvider>
      </QueryClientProvider>,
    );

    await waitFor(() =>
      expect(screen.getByTestId("tenant-list-name")).toHaveTextContent(
        tenantRecordA.name,
      ),
    );
    expect(screen.getByTestId("tenant-detail-name")).toHaveTextContent(
      tenantRecordA.name,
    );
    expect(
      queryClient.getQueryData(tenantQueryKeys.detail(tenantA.id)),
    ).toEqual(tenantRecordA);
  });

  it("synchronizes update results across detail, list and session caches", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const cancelQueries = vi.spyOn(queryClient, "cancelQueries");
    const updatedTenant: Tenant = {
      ...tenantRecordA,
      name: "Workspace A Updated",
      version: 2,
      updated_at: "2026-07-18T03:00:00Z",
    };
    queryClient.setQueryData(tenantQueryKeys.detail(tenantA.id), tenantRecordA);
    queryClient.setQueryData(tenantQueryKeys.list, {
      items: [tenantRecordA],
    });
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (
        request.url.endsWith(`/api/v1/tenants/${tenantA.id}`) &&
        request.method === "PATCH"
      ) {
        return Promise.resolve(jsonResponse(updatedTenant));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <QueryClientProvider client={queryClient}>
        <SessionProvider
          mode={{
            kind: "static",
            currentUser: {
              ...sessionFor(tenantA),
              permissions: ["tenant.manage"],
            },
          }}
        >
          <TenantMutationProbe />
        </SessionProvider>
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "update-tenant" }));

    expect(await screen.findByTestId("active-tenant-name")).toHaveTextContent(
      "Workspace A Updated:2",
    );
    expect(
      queryClient.getQueryData(tenantQueryKeys.detail(tenantA.id)),
    ).toEqual(updatedTenant);
    expect(queryClient.getQueryData(tenantQueryKeys.list)).toEqual({
      items: [updatedTenant],
    });
    expect(cancelQueries).toHaveBeenCalledWith({
      exact: true,
      queryKey: ["auth", "me"],
    });
    const updateRequest = fetchMock.mock.calls
      .map((call) => call[0] as Request)
      .find((request) => request.method === "PATCH");
    await expect(updateRequest?.clone().json()).resolves.toEqual({
      expected_version: 1,
      name: "Workspace A Updated",
    });
  });

  it("applies the archive principal and clears every tenant-scoped cache", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    queryClient.setQueryData(tenantQueryKeys.detail(tenantA.id), tenantRecordA);
    queryClient.setQueryData(tenantQueryKeys.list, {
      items: [tenantRecordA],
    });
    queryClient.setQueryData(["classes", tenantA.id, "list"], ["class-a"]);
    queryClient.setQueryData(["media", tenantA.id, "room"], "room-a");
    queryClient.setQueryData(["audit", tenantA.id, "list"], ["event-a"]);
    queryClient.setQueryData(["profile", "detail"], user);
    const archivedPrincipal: CurrentUser = {
      ...sessionFor(null),
      memberships: [{ ...tenantB, is_active: false }],
      permissions: [],
    };
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      if (request.url.endsWith("/api/v1/auth/csrf")) {
        return Promise.resolve(jsonResponse({ csrf_token: "csrf-token" }));
      }
      if (request.url.endsWith(`/api/v1/tenants/${tenantA.id}/archive`)) {
        return Promise.resolve(jsonResponse(archivedPrincipal));
      }
      return Promise.reject(new Error(`Unexpected request: ${request.url}`));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(
      <QueryClientProvider client={queryClient}>
        <SessionProvider
          mode={{
            kind: "static",
            currentUser: {
              ...sessionFor(tenantA),
              permissions: ["tenant.manage"],
            },
          }}
        >
          <TenantMutationProbe />
        </SessionProvider>
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "archive-tenant" }));

    expect(await screen.findByTestId("active-tenant-name")).toHaveTextContent(
      "none",
    );
    expect(queryClient.getQueryData(tenantQueryKeys.list)).toBeUndefined();
    expect(
      queryClient.getQueryData(tenantQueryKeys.detail(tenantA.id)),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(["classes", tenantA.id, "list"]),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(["media", tenantA.id, "room"]),
    ).toBeUndefined();
    expect(
      queryClient.getQueryData(["audit", tenantA.id, "list"]),
    ).toBeUndefined();
    expect(queryClient.getQueryData(["profile", "detail"])).toEqual(user);
  });
});
