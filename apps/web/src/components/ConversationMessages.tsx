import { APIRequestError, type Message } from "@tutorhub/api-client";
import {
  Button,
  EmptyState,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
} from "@tutorhub/ui";
import { ChevronUp, RefreshCw, Send } from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent,
} from "react";
import {
  useConversationMessages,
  useMarkConversationRead,
  useSendConversationMessage,
} from "../app/conversations";
import { useI18n } from "../app/i18n";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";

const maximumMessageCharacters = 4000;
const maximumMessageBytes = 16 * 1024;

interface PendingMessage {
  clientMessageID: string;
  content: string;
}

export function ConversationMessages({
  actorID,
  canPostMessages,
  conversationID,
  formatter,
  tenantID,
}: {
  actorID: string;
  canPostMessages: boolean;
  conversationID: string;
  formatter: Intl.DateTimeFormat;
  tenantID: string;
}) {
  const { t } = useI18n();
  const messages = useConversationMessages(tenantID, conversationID);
  const markRead = useMarkConversationRead(tenantID, conversationID);
  const sendMessage = useSendConversationMessage(tenantID, conversationID);
  const [draft, setDraft] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [sentFeedback, setSentFeedback] = useState(false);
  const pendingMessage = useRef<PendingMessage | null>(null);
  const lastReadAttempt = useRef<string | null>(null);
  const composer = useRef<HTMLTextAreaElement>(null);
  const oldestMessage = useRef<HTMLLIElement>(null);

  const items = useMemo(() => {
    const byID = new Map<string, Message>();
    for (const page of messages.data?.pages ?? []) {
      for (const item of page.items) {
        byID.set(item.id, item);
      }
    }
    return [...byID.values()].sort(
      (left, right) => left.sequence - right.sequence,
    );
  }, [messages.data?.pages]);

  const newestPage = messages.data?.pages[0];
  const newestMessage = newestPage?.items.reduce<Message | undefined>(
    (newest, item) =>
      !newest || item.sequence > newest.sequence ? item : newest,
    undefined,
  );
  const unreadCount = newestPage?.unread_count ?? 0;
  const unreadCountCapped = newestPage?.unread_count_capped ?? false;

  const markNewestRead = useCallback(() => {
    if (
      document.visibilityState !== "visible" ||
      !messages.isSuccess ||
      unreadCount === 0 ||
      !newestMessage ||
      markRead.isPending ||
      lastReadAttempt.current === newestMessage.id
    ) {
      return;
    }
    lastReadAttempt.current = newestMessage.id;
    markRead.mutate(newestMessage.id);
  }, [markRead, messages.isSuccess, newestMessage, unreadCount]);

  useEffect(() => {
    markNewestRead();
    const handleVisibilityChange = () => markNewestRead();
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () =>
      document.removeEventListener("visibilitychange", handleVisibilityChange);
  }, [markNewestRead]);

  const submitMessage = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (sendMessage.isPending) {
      return;
    }
    const content = normalizeDraft(draft);
    if (!content) {
      setValidationError(t("conversations.messageRequired"));
      return;
    }
    if (
      [...content].length > maximumMessageCharacters ||
      new TextEncoder().encode(content).length > maximumMessageBytes
    ) {
      setValidationError(t("conversations.messageTooLong"));
      return;
    }

    setValidationError(null);
    setSentFeedback(false);
    const existing = pendingMessage.current;
    const request =
      existing?.content === content
        ? existing
        : { clientMessageID: crypto.randomUUID(), content };
    pendingMessage.current = request;
    sendMessage.mutate(
      {
        client_message_id: request.clientMessageID,
        content: request.content,
      },
      {
        onSuccess: () => {
          pendingMessage.current = null;
          setDraft("");
          setSentFeedback(true);
          window.requestAnimationFrame(() => composer.current?.focus());
        },
      },
    );
  };

  const handleComposerKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      event.currentTarget.form?.requestSubmit();
    }
  };

  const loadOlderMessages = async () => {
    const result = await messages.fetchNextPage();
    if (!result.isError && !result.hasNextPage) {
      window.requestAnimationFrame(() => oldestMessage.current?.focus());
    }
  };

  const concealed = shouldConcealTenantScopedData(messages.error);
  const forbidden =
    messages.error instanceof APIRequestError && messages.error.status === 403;
  const initialError = messages.isError && (items.length === 0 || concealed);
  const refreshError =
    messages.isRefetchError &&
    !messages.isFetchNextPageError &&
    items.length > 0 &&
    !concealed;

  return (
    <section
      aria-labelledby={`conversation-messages-title-${conversationID}`}
      className="conversation-messages"
    >
      <div className="conversation-messages__heading">
        <div>
          <h3 id={`conversation-messages-title-${conversationID}`}>
            {t("conversations.messagesTitle")}
          </h3>
          {!initialError && unreadCount > 0 && (
            <StatusBadge tone="warning">
              {t("conversations.unreadBadge", {
                count: unreadCountCapped ? "100+" : unreadCount,
              })}
            </StatusBadge>
          )}
        </div>
        <Button
          leadingIcon={<RefreshCw />}
          loading={messages.isRefetching && !messages.isFetchingNextPage}
          loadingLabel={t("conversations.messagesRefreshing")}
          onClick={() => void messages.refetch()}
          size="sm"
          variant="secondary"
        >
          {t("conversations.messagesRefresh")}
        </Button>
      </div>

      {messages.isPending && (
        <SkeletonGroup label={t("conversations.messagesLoading")}>
          <Skeleton height={72} />
          <Skeleton height={72} />
          <Skeleton height={72} />
        </SkeletonGroup>
      )}

      {initialError &&
        (() => {
          const State = forbidden
            ? ForbiddenState
            : concealed
              ? EmptyState
              : ErrorState;
          return (
            <State
              actions={
                concealed && !forbidden ? undefined : (
                  <Button
                    leadingIcon={<RefreshCw />}
                    onClick={() => void messages.refetch()}
                    variant="secondary"
                  >
                    {t("state.retry")}
                  </Button>
                )
              }
              description={
                forbidden
                  ? t("conversations.messagesForbiddenDescription")
                  : concealed
                    ? t("conversations.messagesUnavailableDescription")
                    : t("conversations.messagesErrorDescription")
              }
              title={
                forbidden
                  ? t("conversations.messagesForbiddenTitle")
                  : concealed
                    ? t("conversations.messagesUnavailableTitle")
                    : t("conversations.messagesErrorTitle")
              }
            />
          );
        })()}

      {refreshError && (
        <div className="conversations-page__inline-error" role="alert">
          <span>{t("conversations.messagesRefreshError")}</span>
          <Button
            onClick={() => void messages.refetch()}
            size="sm"
            variant="secondary"
          >
            {t("state.retry")}
          </Button>
        </div>
      )}

      {messages.isSuccess && items.length === 0 && (
        <EmptyState
          description={t("conversations.messagesEmptyDescription")}
          title={t("conversations.messagesEmptyTitle")}
        />
      )}

      {!initialError && messages.hasNextPage && (
        <div className="conversation-messages__history-action">
          <Button
            leadingIcon={<ChevronUp />}
            loading={messages.isFetchingNextPage}
            loadingLabel={t("conversations.messagesLoadingOlder")}
            onClick={() => void loadOlderMessages()}
            variant="secondary"
          >
            {messages.isFetchNextPageError
              ? t("state.retry")
              : t("conversations.messagesLoadOlder")}
          </Button>
        </div>
      )}

      {!initialError && messages.isFetchNextPageError && (
        <div className="conversations-page__inline-error" role="alert">
          <span>{t("conversations.messagesOlderError")}</span>
        </div>
      )}

      {!initialError && items.length > 0 && (
        <ol
          aria-label={t("conversations.messagesHistoryLabel")}
          className="conversation-message-list"
        >
          {items.map((message, index) => (
            <li
              className={
                message.author.user_id === actorID
                  ? "conversation-message conversation-message--own"
                  : "conversation-message"
              }
              key={message.id}
              ref={index === 0 ? oldestMessage : undefined}
              tabIndex={index === 0 ? -1 : undefined}
            >
              <div className="conversation-message__meta">
                <strong>{message.author.display_name}</strong>
                <time dateTime={message.created_at}>
                  {formatter.format(new Date(message.created_at))}
                </time>
              </div>
              {message.state === "deleted" ? (
                <p className="conversation-message__deleted">
                  {t("conversations.messageDeleted")}
                </p>
              ) : (
                <p className="conversation-message__content">
                  {message.content}
                </p>
              )}
              {message.state === "active" && message.edited_at && (
                <span className="conversation-message__edited">
                  {t("conversations.messageEdited")}
                </span>
              )}
            </li>
          ))}
        </ol>
      )}

      {!initialError && markRead.isError && (
        <div className="conversations-page__inline-error" role="alert">
          <span>{t("conversations.markReadError")}</span>
          <Button
            onClick={() => {
              markRead.reset();
              lastReadAttempt.current = null;
              markNewestRead();
            }}
            size="sm"
            variant="secondary"
          >
            {t("state.retry")}
          </Button>
        </div>
      )}

      {canPostMessages && !messages.isPending && !initialError && (
        <form className="conversation-composer" onSubmit={submitMessage}>
          <label htmlFor={`conversation-composer-${conversationID}`}>
            {t("conversations.composerLabel")}
          </label>
          <textarea
            aria-describedby={`conversation-composer-hint-${conversationID}`}
            disabled={sendMessage.isPending}
            id={`conversation-composer-${conversationID}`}
            onChange={(event) => {
              setDraft(event.target.value);
              setValidationError(null);
              setSentFeedback(false);
              if (sendMessage.isError) {
                sendMessage.reset();
              }
            }}
            onKeyDown={handleComposerKeyDown}
            placeholder={t("conversations.composerPlaceholder")}
            ref={composer}
            rows={3}
            value={draft}
          />
          <div className="conversation-composer__footer">
            <span id={`conversation-composer-hint-${conversationID}`}>
              {t("conversations.composerHint")}
            </span>
            <span aria-live="polite">
              {t("conversations.composerCount", {
                count: [...draft].length,
                limit: maximumMessageCharacters,
              })}
            </span>
          </div>
          {validationError && (
            <p className="conversations-page__form-error" role="alert">
              {validationError}
            </p>
          )}
          {sendMessage.isError && (
            <p className="conversations-page__form-error" role="alert">
              {messageMutationError(sendMessage.error, t)}
            </p>
          )}
          {sentFeedback && (
            <p className="conversation-composer__success" role="status">
              {t("conversations.messageSent")}
            </p>
          )}
          <div className="conversation-composer__actions">
            <Button
              leadingIcon={<Send />}
              loading={sendMessage.isPending}
              loadingLabel={t("conversations.messageSending")}
              type="submit"
            >
              {sendMessage.isError
                ? t("conversations.messageRetry")
                : t("conversations.messageSend")}
            </Button>
          </div>
        </form>
      )}
    </section>
  );
}

function normalizeDraft(value: string) {
  return value.replaceAll("\r\n", "\n").replaceAll("\r", "\n").trim();
}

function messageMutationError(
  error: Error,
  t: ReturnType<typeof useI18n>["t"],
) {
  if (error instanceof APIRequestError) {
    if (error.status === 403) {
      return t("conversations.messageSendForbidden");
    }
    if (error.status === 404) {
      return t("conversations.messageSendUnavailable");
    }
    if (error.status === 409) {
      return t("conversations.messageSendConflict");
    }
    if (error.status === 429) {
      return t("conversations.messageSendRateLimited");
    }
  }
  return t("conversations.messageSendError");
}
