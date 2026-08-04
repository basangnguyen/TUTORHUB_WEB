import { describe, expect, it, vi } from "vitest";
import {
  APIRequestError,
  createDirectConversation,
  deleteConversationMessage,
  editConversationMessage,
  ensureClassConversation,
  getConversation,
  listConversationMessages,
  listConversations,
  markConversationRead,
  sendConversationMessage,
} from "./index";
import type {
  ActiveMessage,
  Conversation,
  ConversationPage,
  CreateDirectConversationRequest,
  DeletedMessage,
  DeleteMessageRequest,
  EditMessageRequest,
  MarkConversationReadRequest,
  Message,
  MessagePage,
  MessageReadState,
  SendMessageRequest,
} from "./index";

const tenantID = "7f44c093-1cb2-46ae-8285-779b78728524";
const conversationID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const classConversationID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";
const classID = "0ce0994b-1d0c-4125-9ad0-dfba33f70322";
const actorID = "5391c8b2-1224-4105-a44e-452eb69d9884";
const targetID = "ad831635-bca7-4d3e-b770-3e5d09452256";
const messageID = "b9170d5e-c354-44a8-9241-4571a5e26e57";
const clientMessageID = "8e0131b2-9b49-487c-92ca-a240eeef0a66";

const directConversation: Conversation = {
  created_at: "2030-08-03T00:00:00Z",
  id: conversationID,
  kind: "direct",
  unread_count: 0,
  unread_count_capped: false,
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
  unread_count: 0,
  unread_count_capped: false,
  participants: [{ display_name: "An", user_id: actorID }],
  title: "Lớp Toán",
  updated_at: "2030-08-03T00:00:00Z",
  viewer_access: { can_post_messages: true },
};

const directInput = {
  target_member_email: "binh@example.test",
} satisfies CreateDirectConversationRequest;

const message: ActiveMessage = {
  author: { display_name: "An", user_id: actorID },
  client_message_id: clientMessageID,
  content: "ChÃ o Bá»‹nh",
  conversation_id: conversationID,
  created_at: "2030-08-03T01:00:00Z",
  id: messageID,
  sequence: 7,
  state: "active",
  updated_at: "2030-08-03T01:00:00Z",
  version: 1,
};

const messageReadState: MessageReadState = {
  last_read_message_id: messageID,
  last_read_sequence: 7,
  updated_at: "2030-08-03T01:01:00Z",
};

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

  it("binds message history, lifecycle, and read state to the tenant with exact bodies", async () => {
    const messagePage: MessagePage = {
      items: [message],
      next_cursor: "message_cursor_v1+/",
      read_state: null,
      unread_count: 1,
      unread_count_capped: false,
    };
    const editedMessage: ActiveMessage = {
      ...message,
      content: "ChÃ o Bá»‹nh!",
      edited_at: "2030-08-03T01:02:00Z",
      updated_at: "2030-08-03T01:02:00Z",
      version: 2,
    };
    const deletedMessage: DeletedMessage = {
      author: editedMessage.author,
      client_message_id: editedMessage.client_message_id,
      conversation_id: editedMessage.conversation_id,
      created_at: editedMessage.created_at,
      deleted_at: "2030-08-03T01:03:00Z",
      edited_at: editedMessage.edited_at,
      id: editedMessage.id,
      sequence: editedMessage.sequence,
      state: "deleted",
      updated_at: "2030-08-03T01:03:00Z",
      version: 3,
    };
    const sendInput = {
      client_message_id: clientMessageID,
      content: "ChÃ o Bá»‹nh",
    } satisfies SendMessageRequest;
    const editInput = {
      content: "ChÃ o Bá»‹nh!",
      expected_version: 1,
    } satisfies EditMessageRequest;
    const deleteInput = {
      expected_version: 2,
    } satisfies DeleteMessageRequest;
    const readInput = {
      message_id: messageID,
    } satisfies MarkConversationReadRequest;
    const fetchMock = vi.fn().mockImplementation((request: Request) => {
      const path = new URL(request.url).pathname;
      if (request.method === "GET") {
        return Promise.resolve(jsonResponse(messagePage));
      }
      if (path.endsWith("/read")) {
        return Promise.resolve(jsonResponse(messageReadState));
      }
      if (request.method === "PATCH") {
        return Promise.resolve(jsonResponse(editedMessage));
      }
      if (request.method === "DELETE") {
        return Promise.resolve(jsonResponse(deletedMessage));
      }
      return Promise.resolve(jsonResponse(message, 201));
    });
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await expect(
      listConversationMessages(
        tenantID,
        conversationID,
        { cursor: "message_cursor_v0+/", limit: 37 },
        options,
      ),
    ).resolves.toEqual(messagePage);
    await expect(
      sendConversationMessage(
        tenantID,
        conversationID,
        sendInput,
        "send-csrf",
        options,
      ),
    ).resolves.toEqual(message);
    await expect(
      editConversationMessage(
        tenantID,
        conversationID,
        messageID,
        editInput,
        "edit-csrf",
        options,
      ),
    ).resolves.toEqual(editedMessage);
    await expect(
      deleteConversationMessage(
        tenantID,
        conversationID,
        messageID,
        deleteInput,
        "delete-csrf",
        options,
      ),
    ).resolves.toEqual(deletedMessage);
    await expect(
      markConversationRead(
        tenantID,
        conversationID,
        readInput,
        "read-csrf",
        options,
      ),
    ).resolves.toEqual(messageReadState);

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests).toHaveLength(5);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
    }
    expect(requests.map((request) => request.method)).toEqual([
      "GET",
      "POST",
      "PATCH",
      "DELETE",
      "POST",
    ]);
    expect(
      requests.map((request) => request.headers.get("X-CSRF-Token")),
    ).toEqual([null, "send-csrf", "edit-csrf", "delete-csrf", "read-csrf"]);

    const listURL = new URL(requests[0]!.url);
    expect(listURL.pathname).toBe(
      `/api/v1/conversations/${conversationID}/messages`,
    );
    expect(listURL.searchParams.get("cursor")).toBe("message_cursor_v0+/");
    expect(listURL.searchParams.get("limit")).toBe("37");
    expect(new URL(requests[1]!.url).pathname).toBe(listURL.pathname);
    expect(new URL(requests[2]!.url).pathname).toBe(
      `${listURL.pathname}/${messageID}`,
    );
    expect(new URL(requests[3]!.url).pathname).toBe(
      `${listURL.pathname}/${messageID}`,
    );
    expect(new URL(requests[4]!.url).pathname).toBe(
      `/api/v1/conversations/${conversationID}/read`,
    );
    await expect(requests[1]!.clone().json()).resolves.toEqual(sendInput);
    await expect(requests[2]!.clone().json()).resolves.toEqual(editInput);
    await expect(requests[3]!.clone().json()).resolves.toEqual(deleteInput);
    await expect(requests[4]!.clone().json()).resolves.toEqual(readInput);
  });

  it("preserves only validated Retry-After seconds for a rate-limited send", async () => {
    const problem = {
      type: "urn:tutorhub:problem:quota-exceeded",
      title: "Quota exceeded",
      status: 429,
      detail: "Wait before sending another message.",
    };
    const responses = [
      new Response(JSON.stringify(problem), {
        status: 429,
        headers: {
          "Content-Type": "application/problem+json",
          "Retry-After": "17",
          "X-Internal-Rate-Bucket": "must-not-escape",
        },
      }),
      new Response(JSON.stringify(problem), {
        status: 429,
        headers: {
          "Content-Type": "application/problem+json",
          "Retry-After": "1.5",
        },
      }),
    ];
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(responses[0]!)
      .mockResolvedValueOnce(responses[1]!);
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    let rateLimitError: unknown;
    try {
      await sendConversationMessage(
        tenantID,
        conversationID,
        {
          client_message_id: clientMessageID,
          content: "rate limited",
        },
        "send-csrf",
        options,
      );
    } catch (error) {
      rateLimitError = error;
    }

    expect(rateLimitError).toBeInstanceOf(APIRequestError);
    expect(rateLimitError).toMatchObject({
      problem,
      retryAfterSeconds: 17,
      status: 429,
    });
    expect(rateLimitError).not.toHaveProperty("headers");
    expect(rateLimitError).not.toHaveProperty("response");
    expect(rateLimitError).not.toHaveProperty("X-Internal-Rate-Bucket");

    let invalidRateLimitError: unknown;
    try {
      await sendConversationMessage(
        tenantID,
        conversationID,
        {
          client_message_id: clientMessageID,
          content: "invalid retry header",
        },
        "send-csrf",
        options,
      );
    } catch (error) {
      invalidRateLimitError = error;
    }

    expect(invalidRateLimitError).toBeInstanceOf(APIRequestError);
    expect(invalidRateLimitError).toMatchObject({
      retryAfterSeconds: undefined,
      status: 429,
    });
  });
});

function projectMessageText(item: Message): string {
  return item.state === "active" ? item.content : item.deleted_at;
}

void projectMessageText(message);

const deletedMessageTypeFixture: DeletedMessage = {
  author: message.author,
  client_message_id: message.client_message_id,
  conversation_id: message.conversation_id,
  created_at: message.created_at,
  deleted_at: "2030-08-03T01:03:00Z",
  id: message.id,
  sequence: message.sequence,
  state: "deleted",
  updated_at: "2030-08-03T01:03:00Z",
  version: 2,
};

const invalidDeletedMessage: DeletedMessage = {
  ...deletedMessageTypeFixture,
  // @ts-expect-error Deleted tombstones cannot expose message content.
  content: "must not compile",
};
void invalidDeletedMessage;

const invalidActiveMessage: ActiveMessage = {
  ...message,
  // @ts-expect-error Active messages cannot carry a deletion timestamp.
  deleted_at: "2030-08-03T01:03:00Z",
};
void invalidActiveMessage;
