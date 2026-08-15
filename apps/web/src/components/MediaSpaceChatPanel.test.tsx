// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import { MediaSpaceChatPanel } from "./MediaSpaceChatPanel";

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const mediaSpaceID = "3b96de90-4d8b-460f-aafd-a1e814b0a6bf";
const conversationID = "c82ef7ee-0a1b-4e99-b9d5-3ae20858a82e";
const actorID = "be85eb92-0f18-4163-85ba-50e4d343d632";

vi.mock("./ConversationMessages", () => ({
  ConversationMessages: (props: {
    actorID: string;
    canPostMessages: boolean;
    conversationID: string;
    tenantID: string;
  }) => (
    <div
      data-actor-id={props.actorID}
      data-can-post={String(props.canPostMessages)}
      data-conversation-id={props.conversationID}
      data-tenant-id={props.tenantID}
      data-testid="persistent-room-messages"
    />
  ),
}));

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type":
        status >= 400 ? "application/problem+json" : "application/json",
    },
  });
}

function roomConversation(canPostMessages: boolean) {
  return {
    id: conversationID,
    kind: "room",
    media_space_id: mediaSpaceID,
    media_space_status: canPostMessages ? "open" : "ended",
    title: "Weekly tutoring room",
    participants: [],
    viewer_access: { can_post_messages: canPostMessages },
    unread_count: 0,
    unread_count_capped: false,
    created_at: "2026-08-14T08:00:00Z",
    updated_at: "2026-08-14T08:00:00Z",
  };
}

function renderPanel(enabled = true) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <MediaSpaceChatPanel
          actorID={actorID}
          enabled={enabled}
          mediaSpaceID={mediaSpaceID}
          tenantID={tenantID}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("MediaSpaceChatPanel", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("ensures one canonical room conversation and mounts the persistent message aggregate", async () => {
    const requests: Request[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((request: Request) => {
        requests.push(request);
        if (new URL(request.url).pathname.endsWith("/api/v1/auth/csrf")) {
          return Promise.resolve(jsonResponse({ csrf_token: "room-csrf" }));
        }
        return Promise.resolve(jsonResponse(roomConversation(true), 201));
      }),
    );

    renderPanel();

    const messages = await screen.findByTestId("persistent-room-messages");
    expect(messages).toHaveAttribute("data-conversation-id", conversationID);
    expect(messages).toHaveAttribute("data-tenant-id", tenantID);
    expect(messages).toHaveAttribute("data-actor-id", actorID);
    expect(messages).toHaveAttribute("data-can-post", "true");
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(new URL(requests[1]?.url ?? "http://localhost").pathname).toBe(
      `/api/v1/media/spaces/${mediaSpaceID}/conversation`,
    );
  });

  it("keeps committed room history mounted but disables writes after the room ends", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((request: Request) => {
        if (new URL(request.url).pathname.endsWith("/api/v1/auth/csrf")) {
          return Promise.resolve(jsonResponse({ csrf_token: "room-csrf" }));
        }
        return Promise.resolve(jsonResponse(roomConversation(false)));
      }),
    );

    renderPanel();

    expect(
      await screen.findByTestId("persistent-room-messages"),
    ).toHaveAttribute("data-can-post", "false");
  });

  it("does not create a chat aggregate before the signed room scope is ready", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    renderPanel(false);

    expect(screen.getByText("Chat is unavailable")).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
