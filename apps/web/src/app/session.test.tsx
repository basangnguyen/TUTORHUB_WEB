import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
} from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { APIRequestError, getCurrentCSRFToken } from "@tutorhub/api-client";
import { useState, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createTutorHubQueryClient } from "./queryClient";
import {
  clearPrivateSessionCache,
  SessionProvider,
  useSession,
} from "./session";

function SessionProbe() {
  const session = useSession();
  return (
    <>
      <output>
        {session.status}:{session.currentUser?.user.email ?? "anonymous"}
      </output>
      <button type="button" onClick={() => void session.signOut()}>
        sign out
      </button>
    </>
  );
}

function UnauthorizedMutationProbe() {
  const mutation = useMutation({
    mutationFn: async () => {
      throw new APIRequestError(401);
    },
  });
  return (
    <button type="button" onClick={() => mutation.mutate()}>
      expire through mutation
    </button>
  );
}

function UnauthorizedQueryProbe({ onAttempt }: { onAttempt: () => void }) {
  const [enabled, setEnabled] = useState(false);
  useQuery({
    queryKey: ["classes", "expired-session"],
    queryFn: async () => {
      onAttempt();
      throw new APIRequestError(401);
    },
    enabled,
  });
  return (
    <button type="button" onClick={() => setEnabled(true)}>
      expire through query
    </button>
  );
}

function AuthenticatedOnly({ children }: { children: ReactNode }) {
  const session = useSession();
  return session.status === "authenticated" ? children : null;
}

function ActivePrivateQueryProbe({ onAttempt }: { onAttempt: () => void }) {
  const privateQuery = useQuery({
    queryKey: ["classes", "active-private"],
    queryFn: async () => {
      onAttempt();
      return { title: "Active private class" };
    },
    staleTime: Infinity,
  });
  return <span>{privateQuery.data?.title}</span>;
}

function NativeSignInProbe() {
  const session = useSession();
  const [handOff, setHandOff] = useState("idle");
  return (
    <>
      <output>
        {session.status}:{session.currentUser?.user.email ?? "anonymous"}
      </output>
      <button
        type="button"
        onClick={() => {
          session.completeNativeSignIn().then(
            () => setHandOff("hand-off done"),
            () => setHandOff("hand-off failed"),
          );
        }}
      >
        complete native sign-in
      </button>
      <span>{handOff}</span>
    </>
  );
}

function requestPath(input: unknown) {
  const url = input instanceof Request ? input.url : String(input);
  return new URL(url, "http://localhost").pathname;
}

function csrfResponse(token: string) {
  return new Response(JSON.stringify({ csrf_token: token }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function renderRemoteSession() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SessionProvider>
        <SessionProbe />
      </SessionProvider>
    </QueryClientProvider>,
  );
}

function renderBoundarySession(children: ReactNode) {
  const queryClient = createTutorHubQueryClient();
  const view = render(
    <QueryClientProvider client={queryClient}>
      <SessionProvider>
        <SessionProbe />
        {children}
      </SessionProvider>
    </QueryClientProvider>,
  );
  return { queryClient, view };
}

function problemResponse(status: number) {
  return new Response(
    JSON.stringify({
      type: `urn:tutorhub:problem:http-${status}`,
      title: "Authentication failed",
      status,
    }),
    {
      status,
      headers: { "Content-Type": "application/problem+json" },
    },
  );
}

function authenticatedResponse() {
  return new Response(
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
  );
}

describe("remote session provider", () => {
  afterEach(() => {
    cleanup();
    sessionStorage.clear();
    vi.unstubAllGlobals();
  });

  it("hydrates the authenticated principal from /me", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(authenticatedResponse()));

    renderRemoteSession();

    expect(
      await screen.findByText("authenticated:student@example.com"),
    ).toBeInTheDocument();
  });

  it("maps HTTP 401 to an unauthenticated browser state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(problemResponse(401)));

    renderRemoteSession();

    expect(
      await screen.findByText("unauthenticated:anonymous"),
    ).toBeInTheDocument();
  });

  it("keeps provider or network failures separate from signed-out state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(problemResponse(503)));

    renderRemoteSession();

    expect(await screen.findByText("error:anonymous")).toBeInTheDocument();
  });

  it("surfaces sign-out failures instead of leaving an unhandled rejection", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(authenticatedResponse())
        .mockResolvedValueOnce(problemResponse(503)),
    );

    const view = renderRemoteSession();

    expect(
      await screen.findByText("authenticated:student@example.com"),
    ).toBeInTheDocument();
    fireEvent.click(view.getByRole("button", { name: "sign out" }));

    expect(
      await screen.findByText("error:student@example.com"),
    ).toBeInTheDocument();
  });

  it("clears cached authenticated data after a successful sign-out boundary", async () => {
    const queryClient = new QueryClient();
    sessionStorage.setItem("tutorhub:draft:v1:test", "private-draft");
    queryClient.setQueryData(["auth", "me"], { user: "student" });
    queryClient.setQueryData(["profile", "detail"], { name: "Student" });
    queryClient.setQueryData(["classes", "tenant-a", "list"], ["class-a"]);

    await clearPrivateSessionCache(queryClient);

    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    expect(sessionStorage.getItem("tutorhub:draft:v1:test")).toBeNull();
  });

  it("purges private caches and rechecks /me after a mutation-only 401", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(authenticatedResponse())
      .mockResolvedValueOnce(problemResponse(401));
    vi.stubGlobal("fetch", fetchMock);
    const { queryClient, view } = renderBoundarySession(
      <UnauthorizedMutationProbe />,
    );

    expect(
      await screen.findByText("authenticated:student@example.com"),
    ).toBeInTheDocument();
    queryClient.setQueryData(["classes", "private"], {
      title: "Private class",
    });
    queryClient.setQueryData(["tenants", "private"], {
      name: "Private workspace",
    });

    fireEvent.click(
      view.getByRole("button", { name: "expire through mutation" }),
    );

    expect(
      await screen.findByText("unauthenticated:anonymous"),
    ).toBeInTheDocument();
    expect(queryClient.getQueryData(["classes", "private"])).toBeUndefined();
    expect(queryClient.getQueryData(["tenants", "private"])).toBeUndefined();
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not retry a tenant query 401 and avoids recursing on /me 401", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(authenticatedResponse())
      .mockResolvedValueOnce(problemResponse(401));
    vi.stubGlobal("fetch", fetchMock);
    const queryAttempts = vi.fn();
    const { queryClient, view } = renderBoundarySession(
      <UnauthorizedQueryProbe onAttempt={queryAttempts} />,
    );

    expect(
      await screen.findByText("authenticated:student@example.com"),
    ).toBeInTheDocument();
    queryClient.setQueryData(["audit", "private"], {
      actor: "Private user",
    });

    fireEvent.click(view.getByRole("button", { name: "expire through query" }));

    expect(
      await screen.findByText("unauthenticated:anonymous"),
    ).toBeInTheDocument();
    expect(queryAttempts).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(["audit", "private"])).toBeUndefined();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("completes a native sign-in by clearing private caches, rotating CSRF and reloading /me", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(problemResponse(401))
      .mockResolvedValueOnce(csrfResponse("csrf-native-session"))
      .mockResolvedValueOnce(authenticatedResponse());
    vi.stubGlobal("fetch", fetchMock);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <SessionProvider>
          <NativeSignInProbe />
        </SessionProvider>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByText("unauthenticated:anonymous"),
    ).toBeInTheDocument();
    sessionStorage.setItem("tutorhub:draft:v1:test", "previous-draft");
    queryClient.setQueryData(["classes", "previous-principal"], ["class-a"]);

    fireEvent.click(
      screen.getByRole("button", { name: "complete native sign-in" }),
    );

    expect(await screen.findByText("hand-off done")).toBeInTheDocument();
    expect(
      screen.getByText("authenticated:student@example.com"),
    ).toBeInTheDocument();
    const paths = fetchMock.mock.calls.map(([input]) => requestPath(input));
    expect(paths).toHaveLength(3);
    expect(paths[1]).toMatch(/\/v1\/auth\/csrf$/);
    expect(paths[2]).toMatch(/\/v1\/me$/);
    expect(getCurrentCSRFToken()).toBe("csrf-native-session");
    expect(
      queryClient.getQueryData(["classes", "previous-principal"]),
    ).toBeUndefined();
    expect(sessionStorage.getItem("tutorhub:draft:v1:test")).toBeNull();
  });

  it("rejects the native hand-off when the CSRF token cannot be rotated", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(problemResponse(401))
      .mockResolvedValueOnce(problemResponse(503));
    vi.stubGlobal("fetch", fetchMock);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <SessionProvider>
          <NativeSignInProbe />
        </SessionProvider>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByText("unauthenticated:anonymous"),
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "complete native sign-in" }),
    );

    expect(await screen.findByText("hand-off failed")).toBeInTheDocument();
    // /me is not re-read with a stale CSRF token.
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("removes an active private query after the session gate unmounts it", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(authenticatedResponse())
      .mockResolvedValueOnce(problemResponse(401));
    vi.stubGlobal("fetch", fetchMock);
    const privateQueryAttempts = vi.fn();
    const { queryClient, view } = renderBoundarySession(
      <AuthenticatedOnly>
        <ActivePrivateQueryProbe onAttempt={privateQueryAttempts} />
        <UnauthorizedMutationProbe />
      </AuthenticatedOnly>,
    );

    expect(
      await screen.findByText("authenticated:student@example.com"),
    ).toBeInTheDocument();
    expect(await screen.findByText("Active private class")).toBeInTheDocument();
    expect(
      queryClient
        .getQueryCache()
        .find({ queryKey: ["classes", "active-private"], exact: true })
        ?.isActive(),
    ).toBe(true);

    fireEvent.click(
      view.getByRole("button", { name: "expire through mutation" }),
    );

    expect(
      await screen.findByText("unauthenticated:anonymous"),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(
        queryClient.getQueryData(["classes", "active-private"]),
      ).toBeUndefined();
    });
    expect(screen.queryByText("Active private class")).not.toBeInTheDocument();
    expect(privateQueryAttempts).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });
});
