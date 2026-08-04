import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { Conversation } from "@tutorhub/api-client";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  conversationQueryKeys,
  useConversations,
  useCreateDirectConversation,
  useEnsureClassConversation,
} from "./conversations";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const conversationID = "c82ef7ee-0a1b-4e99-b9d5-3ae20858a82e";
const classID = "a912f628-f3d2-4c18-84c6-42a9e858dc8d";

const conversation: Conversation = {
  id: conversationID,
  kind: "direct",
  title: "TutorHub Student",
  participants: [
    {
      user_id: "be85eb92-0f18-4163-85ba-50e4d343d632",
      display_name: "TutorHub Teacher",
    },
    {
      user_id: "53f0dac5-6c10-46ff-bcb8-da03d07bc142",
      display_name: "TutorHub Student",
    },
  ],
  viewer_access: { can_post_messages: true },
  unread_count: 0,
  unread_count_capped: false,
  created_at: "2026-08-03T09:00:00Z",
  updated_at: "2026-08-03T09:00:00Z",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function ListProbe() {
  const query = useConversations(tenantID);
  if (!query.data) {
    return <span>loading</span>;
  }
  return (
    <>
      <span>{query.data.pages.flatMap((page) => page.items).length}</span>
      {query.hasNextPage && (
        <button onClick={() => void query.fetchNextPage()} type="button">
          next
        </button>
      )}
    </>
  );
}

function MutationProbe() {
  const direct = useCreateDirectConversation(tenantID);
  const classConversation = useEnsureClassConversation(tenantID);
  return (
    <>
      <button
        onClick={() =>
          direct.mutate({ target_member_email: "student@example.com" })
        }
        type="button"
      >
        direct
      </button>
      <button onClick={() => classConversation.mutate(classID)} type="button">
        class
      </button>
      <output>{direct.data?.id ?? classConversation.data?.id ?? "none"}</output>
    </>
  );
}

function renderProbe(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
  return queryClient;
}

describe("conversation queries", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("uses tenant-scoped keyset pagination without losing the first page", async () => {
    const requests: Request[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((request: Request) => {
        requests.push(request);
        const cursor = new URL(request.url).searchParams.get("cursor");
        return Promise.resolve(
          jsonResponse({
            items: cursor
              ? [{ ...conversation, id: crypto.randomUUID() }]
              : [conversation],
            next_cursor: cursor ? null : "next+/cursor",
          }),
        );
      }),
    );

    renderProbe(<ListProbe />);

    expect(await screen.findByText("1")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "next" }));
    expect(await screen.findByText("2")).toBeInTheDocument();

    expect(requests).toHaveLength(2);
    expect(requests[0]?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    expect(
      new URL(requests[0]?.url ?? "http://localhost").searchParams.get("limit"),
    ).toBe("25");
    expect(
      new URL(requests[1]?.url ?? "http://localhost").searchParams.get(
        "cursor",
      ),
    ).toBe("next+/cursor");
  });

  it("rotates CSRF, sends only the direct target email, and seeds detail cache", async () => {
    const requests: Request[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((request: Request) => {
        requests.push(request);
        if (new URL(request.url).pathname.endsWith("/api/v1/auth/csrf")) {
          return Promise.resolve(
            jsonResponse({ csrf_token: "conversation-csrf" }),
          );
        }
        return Promise.resolve(jsonResponse(conversation, 201));
      }),
    );
    const queryClient = renderProbe(<MutationProbe />);

    fireEvent.click(screen.getByRole("button", { name: "direct" }));
    expect(await screen.findByText(conversationID)).toBeInTheDocument();

    const createRequest = requests[1];
    expect(createRequest?.headers.get("X-CSRF-Token")).toBe(
      "conversation-csrf",
    );
    expect(createRequest?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    await expect(createRequest?.clone().json()).resolves.toEqual({
      target_member_email: "student@example.com",
    });
    expect(
      queryClient.getQueryData(
        conversationQueryKeys.detail(tenantID, conversationID),
      ),
    ).toEqual(conversation);
  });

  it("ensures a class conversation with class scope and no participant body", async () => {
    const requests: Request[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((request: Request) => {
        requests.push(request);
        if (new URL(request.url).pathname.endsWith("/api/v1/auth/csrf")) {
          return Promise.resolve(jsonResponse({ csrf_token: "class-csrf" }));
        }
        return Promise.resolve(
          jsonResponse({
            ...conversation,
            kind: "class",
            class_id: classID,
            class_status: "active",
            title: "Cơ sở An toàn thông tin",
          }),
        );
      }),
    );
    renderProbe(<MutationProbe />);

    fireEvent.click(screen.getByRole("button", { name: "class" }));
    await waitFor(() => expect(requests).toHaveLength(2));

    const ensureRequest = requests[1];
    expect(new URL(ensureRequest?.url ?? "http://localhost").pathname).toBe(
      `/api/v1/classes/${classID}/conversation`,
    );
    expect(ensureRequest?.headers.get("X-CSRF-Token")).toBe("class-csrf");
    expect(await ensureRequest?.clone().text()).toBe("");
  });
});
