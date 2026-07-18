import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
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
    queryClient.setQueryData(["auth", "me"], { user: "student" });
    queryClient.setQueryData(["profile", "detail"], { name: "Student" });
    queryClient.setQueryData(["classes", "tenant-a", "list"], ["class-a"]);

    await clearPrivateSessionCache(queryClient);

    expect(queryClient.getQueryCache().getAll()).toHaveLength(0);
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
  });
});
