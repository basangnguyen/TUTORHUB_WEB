import { describe, expect, it, vi } from "vitest";
import {
  createDirectConversation,
  ensureClassConversation,
  getConversation,
  listConversations,
} from "./index";
import type {
  Conversation,
  ConversationPage,
  CreateDirectConversationRequest,
} from "./index";

const tenantID = "7f44c093-1cb2-46ae-8285-779b78728524";
const conversationID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const classConversationID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";
const classID = "0ce0994b-1d0c-4125-9ad0-dfba33f70322";
const actorID = "5391c8b2-1224-4105-a44e-452eb69d9884";
const targetID = "ad831635-bca7-4d3e-b770-3e5d09452256";

const directConversation: Conversation = {
  created_at: "2030-08-03T00:00:00Z",
  id: conversationID,
  kind: "direct",
  participants: [
    { display_name: "An", user_id: actorID },
    { display_name: "Bình", user_id: targetID },
  ],
  title: "Bình",
  updated_at: "2030-08-03T00:00:00Z",
  viewer_access: { can_post_messages: true },
};

const classConversation: Conversation = {
  class_id: classID,
  class_status: "active",
  created_at: "2030-08-03T00:00:00Z",
  id: classConversationID,
  kind: "class",
  participants: [{ display_name: "An", user_id: actorID }],
  title: "Lớp Toán",
  updated_at: "2030-08-03T00:00:00Z",
  viewer_access: { can_post_messages: true },
};

const directInput = {
  target_member_email: "binh@example.test",
} satisfies CreateDirectConversationRequest;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("conversation API", () => {
  it("binds every request to the expected tenant and never accepts a participant array", async () => {
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path.endsWith("/conversations")) {
        return Promise.resolve(
          jsonResponse({
            items: [directConversation],
            next_cursor: "conversation_cursor_v1",
          } satisfies ConversationPage),
        );
      }
      if (path.endsWith("/conversations/direct")) {
        return Promise.resolve(jsonResponse(directConversation, 201));
      }
      if (path.endsWith(`/classes/${classID}/conversation`)) {
        return Promise.resolve(jsonResponse(classConversation));
      }
      return Promise.resolve(jsonResponse(directConversation));
    });
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await listConversations(
      tenantID,
      { cursor: "conversation_cursor_v0", kind: "direct", limit: 17 },
      options,
    );
    await getConversation(tenantID, conversationID, options);
    await createDirectConversation(
      tenantID,
      directInput,
      "direct-csrf",
      options,
    );
    await ensureClassConversation(tenantID, classID, "class-csrf", options);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests).toHaveLength(4);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
    }
    expect(requests.slice(0, 2).map((request) => request.method)).toEqual([
      "GET",
      "GET",
    ]);
    expect(
      requests
        .slice(0, 2)
        .map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual([null, null]);
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("direct-csrf");
    expect(requests[3]?.headers.get("X-CSRF-Token")).toBe("class-csrf");

    const listURL = new URL(requests[0]!.url);
    expect(listURL.searchParams.get("cursor")).toBe("conversation_cursor_v0");
    expect(listURL.searchParams.get("kind")).toBe("direct");
    expect(listURL.searchParams.get("limit")).toBe("17");
    expect(new URL(requests[1]!.url).pathname).toBe(
      `/api/v1/conversations/${conversationID}`,
    );

    const directBody = JSON.parse(await requests[2]!.clone().text()) as Record<
      string,
      unknown
    >;
    expect(directBody).toEqual({
      target_member_email: directInput.target_member_email,
    });
    expect(directBody).not.toHaveProperty("participants");
    expect(new URL(requests[3]!.url).pathname).toBe(
      `/api/v1/classes/${classID}/conversation`,
    );
    expect(await requests[3]!.clone().text()).toBe("");
  });

  it("rejects an empty expected tenant before sending", async () => {
    const fetchMock = vi.fn();

    await expect(
      listConversations(
        "   ",
        {},
        {
          baseUrl: "https://web.example.test/api",
          fetch: fetchMock,
        },
      ),
    ).rejects.toThrow(TypeError);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
