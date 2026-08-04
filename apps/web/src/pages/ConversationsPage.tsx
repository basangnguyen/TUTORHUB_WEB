import { APIRequestError, type Conversation } from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  EmptyState,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import { ChevronDown, MessageCircle, Plus, RefreshCw } from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
  type RefObject,
} from "react";
import { NavLink, useNavigate, useParams } from "react-router";
import {
  conversationCreationAvailability,
  useConversation,
  useConversations,
  useCreateDirectConversation,
} from "../app/conversations";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";
import { useTenantCapabilities } from "../app/tenantCapabilities";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { ConversationMessages } from "../components/ConversationMessages";
import { TenantOperationNotice } from "../components/TenantOperationNotice";
import "./ConversationsPage.css";

export function ConversationsPage() {
  const { conversationId } = useParams();
  const { language, t } = useI18n();
  const navigate = useNavigate();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const conversations = useConversations(tenantID);
  const conversation = useConversation(tenantID, conversationId);
  const capabilities = useTenantCapabilities(tenantID, Boolean(tenantID));
  const createAvailability = conversationCreationAvailability(capabilities);
  const createDirect = useCreateDirectConversation(tenantID);
  const [createOpen, setCreateOpen] = useState(false);
  const [targetEmail, setTargetEmail] = useState("");
  const [emailError, setEmailError] = useState<string | null>(null);
  const [createdTitle, setCreatedTitle] = useState<string | null>(null);
  const createTrigger = useRef<HTMLButtonElement>(null);
  const detailHeading = useRef<HTMLHeadingElement>(null);
  const items = useMemo(() => {
    const byID = new Map<string, Conversation>();
    for (const page of conversations.data?.pages ?? []) {
      for (const item of page.items) {
        byID.set(item.id, item);
      }
    }
    return [...byID.values()];
  }, [conversations.data?.pages]);
  const formatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  useEffect(() => {
    if (conversationId && conversation.isSuccess) {
      const frame = window.requestAnimationFrame(() => {
        detailHeading.current?.focus();
      });
      return () => window.cancelAnimationFrame(frame);
    }
    return undefined;
  }, [conversation.isSuccess, conversationId]);

  const listConcealed = shouldConcealTenantScopedData(conversations.error);
  const listForbidden =
    conversations.error instanceof APIRequestError &&
    conversations.error.status === 403;
  const initialListError =
    conversations.isError && (listConcealed || items.length === 0);
  const refreshingError =
    conversations.isError &&
    !listConcealed &&
    items.length > 0 &&
    !conversations.isFetchNextPageError;

  const openCreate = (event: MouseEvent<HTMLButtonElement>) => {
    createTrigger.current = event.currentTarget;
    setCreatedTitle(null);
    setCreateOpen(true);
  };

  const submitDirect = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalizedEmail = targetEmail.trim().toLowerCase();
    if (!normalizedEmail) {
      setEmailError(t("conversations.targetEmailRequired"));
      return;
    }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)) {
      setEmailError(t("conversations.targetEmailInvalid"));
      return;
    }
    setEmailError(null);
    createDirect.mutate(
      { target_member_email: normalizedEmail },
      {
        onSuccess: (created) => {
          setCreateOpen(false);
          setTargetEmail("");
          setCreatedTitle(created.title);
          navigate(`/app/messages/${created.id}`);
        },
      },
    );
  };

  if (initialListError) {
    const State = listForbidden ? ForbiddenState : ErrorState;
    return (
      <div className="page-content conversations-page">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void conversations.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={
            listForbidden
              ? t("conversations.forbiddenDescription")
              : t("conversations.errorDescription")
          }
          title={
            listForbidden
              ? t("conversations.forbiddenTitle")
              : t("conversations.errorTitle")
          }
        />
      </div>
    );
  }

  return (
    <div className="page-content conversations-page">
      <header className="page-heading conversations-page__header">
        <div>
          <p>{t("conversations.kicker")}</p>
          <h1>{t("conversations.title")}</h1>
          <span>{t("conversations.description")}</span>
        </div>
        <Button
          disabled={!createAvailability.available}
          leadingIcon={<Plus />}
          onClick={openCreate}
        >
          {t("conversations.createAction")}
        </Button>
      </header>

      {createdTitle && (
        <p className="conversations-page__feedback" role="status">
          {t("conversations.created", { title: createdTitle })}
        </p>
      )}

      <TenantOperationNotice
        availability={createAvailability}
        label={t("capabilities.operationCreateConversation")}
        onRetry={() => void capabilities.refetch()}
      />

      {refreshingError && (
        <div className="conversations-page__inline-error" role="alert">
          <span>{t("conversations.refreshError")}</span>
          <Button
            onClick={() => void conversations.refetch()}
            size="sm"
            variant="secondary"
          >
            {t("state.retry")}
          </Button>
        </div>
      )}

      <div className="conversations-layout">
        <section
          aria-labelledby="conversation-list-title"
          className="conversation-list-panel"
        >
          <div className="conversation-list-panel__heading">
            <h2 id="conversation-list-title">{t("conversations.listTitle")}</h2>
            <Button
              leadingIcon={<RefreshCw />}
              loading={
                conversations.isRefetching && !conversations.isFetchingNextPage
              }
              loadingLabel={t("conversations.refreshing")}
              onClick={() => void conversations.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("conversations.refresh")}
            </Button>
          </div>

          {conversations.isPending && (
            <SkeletonGroup label={t("conversations.loading")}>
              <Skeleton height={82} />
              <Skeleton height={82} />
              <Skeleton height={82} />
            </SkeletonGroup>
          )}

          {!listConcealed && conversations.data && items.length === 0 && (
            <EmptyState
              actions={
                createAvailability.available ? (
                  <Button onClick={openCreate} variant="secondary">
                    {t("conversations.createAction")}
                  </Button>
                ) : undefined
              }
              description={t("conversations.emptyDescription")}
              title={t("conversations.emptyTitle")}
            />
          )}

          {!listConcealed && items.length > 0 && (
            <>
              <p className="visually-hidden" role="status">
                {t("conversations.loadedCount", { count: items.length })}
              </p>
              <ul className="conversation-list">
                {items.map((item) => (
                  <li key={item.id}>
                    <NavLink
                      className={({ isActive }) =>
                        `conversation-list__link${
                          isActive ? " conversation-list__link--active" : ""
                        }`
                      }
                      to={`/app/messages/${item.id}`}
                    >
                      <span className="conversation-list__identity">
                        <strong>{item.title}</strong>
                        <StatusBadge tone="neutral">
                          {t(
                            item.kind === "class"
                              ? "conversations.classKind"
                              : "conversations.directKind",
                          )}
                        </StatusBadge>
                      </span>
                      <span className="conversation-list__summary">
                        <time dateTime={item.updated_at}>
                          {formatter.format(new Date(item.updated_at))}
                        </time>
                        {item.unread_count > 0 && (
                          <StatusBadge tone="warning">
                            {t("conversations.unreadBadge", {
                              count: item.unread_count_capped
                                ? "100+"
                                : item.unread_count,
                            })}
                          </StatusBadge>
                        )}
                      </span>
                    </NavLink>
                  </li>
                ))}
              </ul>
            </>
          )}

          {conversations.isFetchNextPageError && (
            <div className="conversations-page__inline-error" role="alert">
              <span>{t("conversations.loadMoreError")}</span>
              <Button
                onClick={() => void conversations.fetchNextPage()}
                size="sm"
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            </div>
          )}

          {conversations.hasNextPage && (
            <div className="conversation-list-panel__pagination">
              <Button
                leadingIcon={<ChevronDown />}
                loading={conversations.isFetchingNextPage}
                loadingLabel={t("conversations.loadingMore")}
                onClick={() => void conversations.fetchNextPage()}
                variant="secondary"
              >
                {t("conversations.loadMore")}
              </Button>
            </div>
          )}
        </section>

        <ConversationDetail
          actorID={session.currentUser?.user.id ?? ""}
          conversation={conversation}
          detailHeading={detailHeading}
          formatter={formatter}
          selected={Boolean(conversationId)}
          tenantID={tenantID ?? ""}
        />
      </div>

      <Dialog
        onOpenChange={(open) => {
          if (createDirect.isPending) {
            return;
          }
          setCreateOpen(open);
          if (!open) {
            setEmailError(null);
            createDirect.reset();
          }
        }}
        open={createOpen}
      >
        <DialogContent
          closeLabel={t("conversations.closeDialog")}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            createTrigger.current?.focus();
          }}
        >
          <form onSubmit={submitDirect}>
            <DialogTitle>{t("conversations.createDialogTitle")}</DialogTitle>
            <DialogDescription>
              {t("conversations.createDialogDescription")}
            </DialogDescription>
            <TextField
              autoComplete="off"
              autoFocus
              disabled={createDirect.isPending}
              error={emailError ?? undefined}
              hint={t("conversations.targetEmailHint")}
              label={t("conversations.targetEmailLabel")}
              onChange={(event) => {
                setTargetEmail(event.target.value);
                if (emailError) {
                  setEmailError(null);
                }
              }}
              type="email"
              value={targetEmail}
            />
            {createDirect.isError && (
              <p className="conversations-page__form-error" role="alert">
                {t("conversations.createError")}
              </p>
            )}
            <DialogFooter>
              <DialogClose asChild>
                <Button disabled={createDirect.isPending} variant="secondary">
                  {t("conversations.cancel")}
                </Button>
              </DialogClose>
              <Button
                loading={createDirect.isPending}
                loadingLabel={t("conversations.creating")}
                type="submit"
              >
                {t("conversations.create")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ConversationDetail({
  actorID,
  conversation,
  detailHeading,
  formatter,
  selected,
  tenantID,
}: {
  actorID: string;
  conversation: ReturnType<typeof useConversation>;
  detailHeading: RefObject<HTMLHeadingElement | null>;
  formatter: Intl.DateTimeFormat;
  selected: boolean;
  tenantID: string;
}) {
  const { t } = useI18n();

  if (!selected) {
    return (
      <section className="conversation-detail-panel">
        <EmptyState
          description={t("conversations.detailPromptDescription")}
          title={t("conversations.detailPromptTitle")}
        />
      </section>
    );
  }

  if (conversation.isPending) {
    return (
      <section className="conversation-detail-panel">
        <SkeletonGroup label={t("conversations.detailLoading")}>
          <Skeleton height={36} width="55%" />
          <Skeleton height={90} />
          <Skeleton height={150} />
        </SkeletonGroup>
      </section>
    );
  }

  if (conversation.isError) {
    const concealed = shouldConcealTenantScopedData(conversation.error);
    const State =
      conversation.error instanceof APIRequestError &&
      conversation.error.status === 403
        ? ForbiddenState
        : concealed
          ? EmptyState
          : ErrorState;
    return (
      <section className="conversation-detail-panel">
        <State
          actions={
            concealed ? undefined : (
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void conversation.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            )
          }
          description={
            concealed
              ? t("conversations.notFoundDescription")
              : t("conversations.detailErrorDescription")
          }
          title={
            concealed
              ? t("conversations.notFoundTitle")
              : t("conversations.detailErrorTitle")
          }
        />
      </section>
    );
  }

  const item = conversation.data;
  const readOnly =
    item.class_status === "archived" ||
    item.viewer_access.can_post_messages !== true;

  return (
    <article className="conversation-detail-panel">
      <header className="conversation-detail-panel__header">
        <div>
          <p>
            {t(
              item.kind === "class"
                ? "conversations.classKind"
                : "conversations.directKind",
            )}
          </p>
          <h2 ref={detailHeading} tabIndex={-1}>
            {item.title}
          </h2>
        </div>
        <StatusBadge tone={readOnly ? "warning" : "success"}>
          {t(readOnly ? "conversations.readOnly" : "conversations.ready")}
        </StatusBadge>
      </header>

      {readOnly && (
        <p className="conversation-detail-panel__notice" role="status">
          {t("conversations.readOnlyDescription")}
        </p>
      )}

      <section aria-labelledby="conversation-participants-title">
        <h3 id="conversation-participants-title">
          {t("conversations.participantsTitle")}
        </h3>
        {item.participants.length > 0 ? (
          <ul className="conversation-participants">
            {item.participants.map((participant) => (
              <li key={participant.user_id}>
                <MessageCircle aria-hidden="true" />
                <span>{participant.display_name}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p>{t("conversations.participantsEmpty")}</p>
        )}
      </section>

      <dl className="conversation-detail-panel__facts">
        <div>
          <dt>{t("conversations.updatedLabel")}</dt>
          <dd>
            <time dateTime={item.updated_at}>
              {formatter.format(new Date(item.updated_at))}
            </time>
          </dd>
        </div>
      </dl>

      <ConversationMessages
        actorID={actorID}
        canPostMessages={!readOnly}
        conversationID={item.id}
        formatter={formatter}
        key={item.id}
        tenantID={tenantID}
      />
    </article>
  );
}
