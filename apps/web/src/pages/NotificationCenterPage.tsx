import { APIRequestError, type Notification } from "@tutorhub/api-client";
import {
  Button,
  EmptyState,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
} from "@tutorhub/ui";
import { CheckCheck, RefreshCw, Settings } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import { notificationTarget } from "../app/notificationTarget";
import {
  useMarkAllNotificationsRead,
  useMarkNotificationRead,
  useNotificationUnreadCount,
  useNotifications,
} from "../app/notifications";
import { useSession } from "../app/session";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";

type NotificationFilter = "all" | "unread";

interface NotificationCopy {
  body: string;
  title: string;
}

export function NotificationCenterPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const [filter, setFilter] = useState<NotificationFilter>("all");
  const notifications = useNotifications(tenantID, filter === "unread", true);
  const unreadCount = useNotificationUnreadCount(tenantID, true);
  const markRead = useMarkNotificationRead(tenantID);
  const markAllRead = useMarkAllNotificationsRead(tenantID);
  const items = useMemo(() => {
    const byID = new Map<string, Notification>();
    for (const page of notifications.data?.pages ?? []) {
      for (const item of page.items) {
        byID.set(item.id, item);
      }
    }
    return [...byID.values()];
  }, [notifications.data?.pages]);
  const formatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  const concealed = shouldConcealTenantScopedData(notifications.error);
  const forbidden =
    notifications.error instanceof APIRequestError &&
    notifications.error.status === 403;
  const initialError = notifications.isError && items.length === 0;
  const refreshingError =
    notifications.isError &&
    items.length > 0 &&
    !notifications.isFetchNextPageError;
  const mutationError = markRead.isError || markAllRead.isError;
  const hasUnread = unreadCount.data
    ? unreadCount.data.count > 0
    : items.some((item) => !item.read_at);

  if (initialError) {
    const State = forbidden ? ForbiddenState : ErrorState;
    return (
      <div className="page-content notification-center">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void notifications.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={
            forbidden
              ? t("notifications.forbiddenDescription")
              : t("notifications.errorDescription")
          }
          title={
            forbidden
              ? t("notifications.forbiddenTitle")
              : t("notifications.errorTitle")
          }
        />
      </div>
    );
  }

  return (
    <div className="page-content notification-center">
      <header className="page-heading notification-center__header">
        <div>
          <p>{t("notifications.kicker")}</p>
          <h1>{t("notifications.title")}</h1>
          <span>{t("notifications.description")}</span>
        </div>
        <div className="notification-center__header-actions">
          <Link
            className="notification-center__preferences-link"
            to="/app/notifications/preferences"
          >
            <Settings aria-hidden="true" />
            {t("notifications.preferencesAction")}
          </Link>
          <Button
            disabled={!hasUnread || markAllRead.isPending}
            leadingIcon={<CheckCheck />}
            loading={markAllRead.isPending}
            loadingLabel={t("notifications.markingAllRead")}
            onClick={() => markAllRead.mutate()}
            variant="secondary"
          >
            {t("notifications.markAllRead")}
          </Button>
        </div>
      </header>

      <section
        aria-labelledby="notification-list-title"
        className="notification-center__panel"
      >
        <div className="notification-center__toolbar">
          <h2 className="visually-hidden" id="notification-list-title">
            {t("notifications.title")}
          </h2>
          <div
            aria-label={t("notifications.filterLabel")}
            className="notification-center__filters"
            role="group"
          >
            {(["all", "unread"] as const).map((value) => (
              <Button
                aria-pressed={filter === value}
                key={value}
                onClick={() => setFilter(value)}
                size="sm"
                variant={filter === value ? "primary" : "secondary"}
              >
                {t(
                  value === "all"
                    ? "notifications.filterAll"
                    : "notifications.filterUnread",
                )}
              </Button>
            ))}
          </div>
          <Button
            leadingIcon={<RefreshCw />}
            loading={
              notifications.isRefetching && !notifications.isFetchingNextPage
            }
            loadingLabel={t("notifications.refreshing")}
            onClick={() => void notifications.refetch()}
            size="sm"
            variant="secondary"
          >
            {t("notifications.refresh")}
          </Button>
        </div>

        {refreshingError && (
          <div className="notification-center__inline-error" role="alert">
            <span>{t("notifications.refreshError")}</span>
            <Button
              onClick={() => void notifications.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          </div>
        )}

        {mutationError && (
          <p className="notification-center__inline-error" role="alert">
            {t("notifications.markError")}
          </p>
        )}

        {notifications.isPending && (
          <SkeletonGroup label={t("notifications.loading")}>
            <Skeleton height={116} />
            <Skeleton height={116} />
            <Skeleton height={116} />
          </SkeletonGroup>
        )}

        {!concealed && notifications.isSuccess && items.length === 0 && (
          <EmptyState
            description={t(
              filter === "unread"
                ? "notifications.unreadEmptyDescription"
                : "notifications.emptyDescription",
            )}
            title={t(
              filter === "unread"
                ? "notifications.unreadEmptyTitle"
                : "notifications.emptyTitle",
            )}
          />
        )}

        {!concealed && items.length > 0 && (
          <>
            <p className="visually-hidden" role="status">
              {t("notifications.loadedCount", { count: items.length })}
            </p>
            <ul className="notification-center__list">
              {items.map((item) => {
                const copy = notificationCopy(item, t);
                const target = notificationTarget(item);
                const isUnread = !item.read_at;
                const markingThis =
                  markRead.isPending && markRead.variables === item.id;
                return (
                  <li
                    className={
                      isUnread ? "notification-card--unread" : undefined
                    }
                    key={item.id}
                  >
                    <div className="notification-card__content">
                      <div className="notification-card__heading">
                        <strong>{copy.title}</strong>
                        <StatusBadge tone={isUnread ? "info" : "neutral"}>
                          {t(
                            isUnread
                              ? "notifications.unread"
                              : "notifications.read",
                          )}
                        </StatusBadge>
                      </div>
                      <p>{copy.body}</p>
                      <time dateTime={item.occurred_at}>
                        {formatter.format(new Date(item.occurred_at))}
                      </time>
                    </div>
                    <div className="notification-card__actions">
                      {target ? (
                        <Link to={target}>{t("notifications.openTarget")}</Link>
                      ) : (
                        <span>{t("notifications.noTarget")}</span>
                      )}
                      {isUnread && (
                        <Button
                          loading={markingThis}
                          loadingLabel={t("notifications.markingRead")}
                          onClick={() => markRead.mutate(item.id)}
                          size="sm"
                          variant="secondary"
                        >
                          {t("notifications.markRead")}
                        </Button>
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          </>
        )}

        {notifications.isFetchNextPageError && (
          <div className="notification-center__inline-error" role="alert">
            <span>{t("notifications.loadMoreError")}</span>
            <Button
              onClick={() => void notifications.fetchNextPage()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          </div>
        )}

        {notifications.hasNextPage && (
          <div className="notification-center__pagination">
            <Button
              loading={notifications.isFetchingNextPage}
              loadingLabel={t("notifications.loadingMore")}
              onClick={() => void notifications.fetchNextPage()}
              variant="secondary"
            >
              {t("notifications.loadMore")}
            </Button>
          </div>
        )}
      </section>
    </div>
  );
}

function notificationCopy(
  item: Notification,
  t: (key: TranslationKey, values?: Record<string, string | number>) => string,
): NotificationCopy {
  const values = {
    className:
      item.context.class_title || t("notifications.template.classFallback"),
    startsAt: item.context.starts_at || "",
  };
  const normalized = item.template_key.endsWith(".v1")
    ? item.template_key.slice(0, -3)
    : item.template_key;

  switch (normalized) {
    case "class_session.scheduled":
      return {
        body: t("notifications.template.sessionScheduledBody", values),
        title: t("notifications.template.sessionScheduledTitle"),
      };
    case "class_session.updated":
      return {
        body: t("notifications.template.sessionUpdatedBody", values),
        title: t("notifications.template.sessionUpdatedTitle"),
      };
    case "class_session.cancelled":
      return {
        body: t("notifications.template.sessionCancelledBody", values),
        title: t("notifications.template.sessionCancelledTitle"),
      };
    case "class_session.reminder":
      return {
        body: t("notifications.template.sessionReminderBody", values),
        title: t("notifications.template.sessionReminderTitle"),
      };
    default:
      return {
        body: t("notifications.template.unknownBody"),
        title: t("notifications.template.unknownTitle"),
      };
  }
}
