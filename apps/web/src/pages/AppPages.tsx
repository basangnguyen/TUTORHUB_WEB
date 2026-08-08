import { useQuery } from "@tanstack/react-query";
import {
  getHealth,
  type AuthorizedSearchResult,
  type CalendarItem,
  type HomeRecentFile,
} from "@tutorhub/api-client";
import { StatusBadge } from "@tutorhub/ui";
import {
  Bell,
  CalendarDays,
  FileText,
  MessageCircle,
  Search,
} from "lucide-react";
import { useDeferredValue, useState, type ReactNode } from "react";
import { Link } from "react-router";
import {
  useHomeMessageUnread,
  useHomeNotificationUnread,
  useHomeRecentFiles,
  useHomeSearch,
  useHomeUpcomingSessions,
  isHomeSearchReady,
} from "../app/homeDashboard";
import { useI18n } from "../app/i18n";
import { getVisibleNavigationItems } from "../app/routes";
import { useSession } from "../app/session";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export function DashboardPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const [searchText, setSearchText] = useState("");
  const deferredSearch = useDeferredValue(searchText);
  const currentUser = session.currentUser;
  const activeTenant = currentUser?.active_tenant;
  const principal = {
    tenantID: activeTenant?.id,
    userID: currentUser?.user.id,
  };
  const timezone = currentUser?.user.timezone ?? "UTC";
  const upcomingQuery = useHomeUpcomingSessions({ ...principal, timezone });
  const notificationQuery = useHomeNotificationUnread(principal);
  const messageQuery = useHomeMessageUnread(principal);
  const recentFileQuery = useHomeRecentFiles(principal);
  const searchQuery = useHomeSearch({ ...principal, query: deferredSearch });
  const visibleNavigationItems = getVisibleNavigationItems(
    currentUser?.permissions ?? [],
  );
  const roleLabel =
    activeTenant?.role === "org_admin"
      ? t("shell.role.admin")
      : activeTenant?.role === "teacher"
        ? t("shell.role.teacher")
        : activeTenant?.role === "student"
          ? t("shell.role.student")
          : t("shell.role.guest");

  return (
    <div className="page-content home-dashboard">
      <header className="page-heading home-dashboard__heading">
        <div>
          <p>{t("home.kicker")}</p>
          <h1>{t("home.title")}</h1>
          <span>{t("home.description")}</span>
        </div>
        <HealthStatus />
      </header>

      <section aria-labelledby="home-search-title" className="home-search">
        <div className="home-search__heading">
          <div>
            <h2 id="home-search-title">{t("home.search.title")}</h2>
            <p>{t("home.search.description")}</p>
          </div>
          <Search aria-hidden="true" size={20} />
        </div>
        <label className="home-search__field">
          <span className="sr-only">{t("home.search.label")}</span>
          <Search aria-hidden="true" size={18} />
          <input
            autoComplete="off"
            maxLength={100}
            onChange={(event) => setSearchText(event.target.value)}
            placeholder={t("home.search.placeholder")}
            type="search"
            value={searchText}
          />
          {searchQuery.isFetching && isHomeSearchReady(deferredSearch) && (
            <span aria-live="polite" className="home-search__status">
              {t("home.search.loading")}
            </span>
          )}
        </label>
        <SearchResults
          error={searchQuery.isError}
          items={searchQuery.data?.items ?? []}
          language={language}
          onRetry={() => void searchQuery.refetch()}
          query={deferredSearch}
        />
      </section>

      <section aria-labelledby="dashboard-cards-title">
        <div className="home-section-heading">
          <div>
            <h2 id="dashboard-cards-title">{t("home.dashboard.title")}</h2>
            <p>{t("home.dashboard.description")}</p>
          </div>
          <time dateTime={new Date().toISOString()}>
            {new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
              dateStyle: "full",
            }).format(new Date())}
          </time>
        </div>

        <div className="home-card-grid">
          <HomeCard
            action={<Link to="/app/calendar">{t("home.viewCalendar")}</Link>}
            icon={<CalendarDays aria-hidden="true" size={19} />}
            title={t("home.upcoming.title")}
          >
            <UpcomingContent
              error={upcomingQuery.isError}
              items={upcomingQuery.data ?? []}
              language={language}
              loading={upcomingQuery.isPending}
              onRetry={() => void upcomingQuery.refetch()}
            />
          </HomeCard>

          <HomeCard
            action={<Link to="/app/messages">{t("home.openMessages")}</Link>}
            icon={<MessageCircle aria-hidden="true" size={19} />}
            title={t("home.unread.title")}
          >
            <div className="home-unread-list">
              <UnreadRow
                count={messageQuery.data?.count}
                error={messageQuery.isError}
                icon={<MessageCircle aria-hidden="true" size={17} />}
                label={t("home.unread.messages")}
                loading={messageQuery.isPending}
                onRetry={() => void messageQuery.refetch()}
                suffix={messageQuery.data?.capped ? "+" : ""}
                to="/app/messages"
              />
              <UnreadRow
                count={notificationQuery.data?.count}
                error={notificationQuery.isError}
                icon={<Bell aria-hidden="true" size={17} />}
                label={t("home.unread.notifications")}
                loading={notificationQuery.isPending}
                onRetry={() => void notificationQuery.refetch()}
                suffix={notificationQuery.data?.is_capped ? "+" : ""}
                to="/app/notifications"
              />
            </div>
          </HomeCard>

          <HomeCard
            icon={<FileText aria-hidden="true" size={19} />}
            title={t("home.recentFiles.title")}
          >
            <RecentFilesContent
              error={recentFileQuery.isError}
              items={recentFileQuery.data ?? []}
              language={language}
              loading={recentFileQuery.isPending}
              onRetry={() => void recentFileQuery.refetch()}
            />
          </HomeCard>
        </div>
      </section>

      <section aria-labelledby="workspace-heading" className="home-context">
        <div>
          <h2 id="workspace-heading">{t("home.workspace")}</h2>
          <dl className="workspace-facts">
            <div>
              <dt>{t("home.workspace")}</dt>
              <dd>{activeTenant?.name ?? t("home.workspaceValue")}</dd>
            </div>
            <div>
              <dt>{t("home.role")}</dt>
              <dd>{roleLabel}</dd>
            </div>
            <div>
              <dt>{t("home.language")}</dt>
              <dd>{language === "vi" ? t("home.language.vi") : "English"}</dd>
            </div>
          </dl>
        </div>
        <nav aria-label={t("home.quickLinks")} className="home-quick-links">
          {visibleNavigationItems
            .filter((item) => item.to !== "/app/home")
            .map((item) => (
              <Link
                aria-label={t("home.openModule", { module: t(item.labelKey) })}
                key={item.to}
                to={item.to}
              >
                {t(item.labelKey)}
              </Link>
            ))}
        </nav>
      </section>
    </div>
  );
}

function HomeCard({
  action,
  children,
  icon,
  title,
}: {
  action?: ReactNode;
  children: ReactNode;
  icon: ReactNode;
  title: string;
}) {
  return (
    <article className="home-card">
      <header className="home-card__heading">
        <span className="home-card__icon">{icon}</span>
        <h3>{title}</h3>
        {action && <span className="home-card__action">{action}</span>}
      </header>
      {children}
    </article>
  );
}

function UpcomingContent({
  error,
  items,
  language,
  loading,
  onRetry,
}: {
  error: boolean;
  items: readonly CalendarItem[];
  language: "en" | "vi";
  loading: boolean;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  if (loading) return <CardSkeleton />;
  if (error)
    return <CardError label={t("home.upcoming.title")} onRetry={onRetry} />;
  if (items.length === 0) {
    return <p className="home-card__empty">{t("home.upcoming.empty")}</p>;
  }
  return (
    <ol className="home-event-list">
      {items.map((item) => (
        <li key={item.id}>
          <time dateTime={item.starts_at}>
            {formatDateTime(item.starts_at, language)}
          </time>
          <Link to="/app/calendar">{item.title}</Link>
          <span>{item.class_title}</span>
        </li>
      ))}
    </ol>
  );
}

function RecentFilesContent({
  error,
  items,
  language,
  loading,
  onRetry,
}: {
  error: boolean;
  items: readonly HomeRecentFile[];
  language: "en" | "vi";
  loading: boolean;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  if (loading) return <CardSkeleton />;
  if (error)
    return <CardError label={t("home.recentFiles.title")} onRetry={onRetry} />;
  if (items.length === 0) {
    return <p className="home-card__empty">{t("home.recentFiles.empty")}</p>;
  }
  return (
    <ul className="home-file-list">
      {items.map((file) => (
        <li key={file.id}>
          <FileText aria-hidden="true" size={18} />
          <div>
            <Link to={`/app/classrooms/${file.class_id}#class-files-title`}>
              {file.display_name}
            </Link>
            <span>
              {file.class_title} · {formatBytes(file.size_bytes, language)}
            </span>
          </div>
        </li>
      ))}
    </ul>
  );
}

function UnreadRow({
  count,
  error,
  icon,
  label,
  loading,
  onRetry,
  suffix,
  to,
}: {
  count: number | undefined;
  error: boolean;
  icon: ReactNode;
  label: string;
  loading: boolean;
  onRetry: () => void;
  suffix: string;
  to: string;
}) {
  const { t } = useI18n();
  return (
    <div className="home-unread-row">
      <span className="home-unread-row__icon">{icon}</span>
      <Link aria-label={t("home.openModule", { module: label })} to={to}>
        {label}
      </Link>
      {loading ? (
        <span aria-label={t("home.loading")} className="home-count-skeleton" />
      ) : error ? (
        <button
          aria-label={t("home.retryArea", { area: label })}
          onClick={onRetry}
          type="button"
        >
          {t("home.retryShort")}
        </button>
      ) : (
        <strong aria-label={t("home.unread.count", { count: count ?? 0 })}>
          {count ?? 0}
          {suffix}
        </strong>
      )}
    </div>
  );
}

function CardSkeleton() {
  const { t } = useI18n();
  return (
    <div aria-label={t("home.loading")} className="home-card__skeleton">
      <span />
      <span />
      <span />
    </div>
  );
}

function CardError({ label, onRetry }: { label: string; onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <div className="home-card__error" role="alert">
      <p>{t("home.cardError")}</p>
      <button
        aria-label={t("home.retryArea", { area: label })}
        onClick={onRetry}
        type="button"
      >
        {t("home.retryShort")}
      </button>
    </div>
  );
}

function SearchResults({
  error,
  items,
  language,
  onRetry,
  query,
}: {
  error: boolean;
  items: readonly AuthorizedSearchResult[];
  language: "en" | "vi";
  onRetry: () => void;
  query: string;
}) {
  const { t } = useI18n();
  const normalized = query.trim();
  if (!isHomeSearchReady(normalized)) return null;
  if (error) {
    return (
      <div className="home-search__error" role="alert">
        <span>{t("home.search.error")}</span>
        <button
          aria-label={t("home.retryArea", { area: t("home.search.title") })}
          onClick={onRetry}
          type="button"
        >
          {t("home.retryShort")}
        </button>
      </div>
    );
  }
  if (items.length === 0) {
    return (
      <p aria-live="polite" className="home-search__empty">
        {t("home.search.empty", { query: normalized })}
      </p>
    );
  }
  return (
    <ul aria-label={t("home.search.results")} className="home-search__results">
      {items.map((item) => (
        <li key={`${item.kind}-${item.id}`}>
          <span data-kind={item.kind}>{searchKindLabel(item, t)}</span>
          <Link to={searchResultLink(item)}>{item.title}</Link>
          <small>
            {searchContext(item, t)} ·{" "}
            {formatDateTime(item.occurred_at, language)}
          </small>
        </li>
      ))}
    </ul>
  );
}

function searchResultLink(item: AuthorizedSearchResult) {
  if (item.kind === "conversation") return `/app/messages/${item.id}`;
  if (item.kind === "file" && item.class_id) {
    return `/app/classrooms/${item.class_id}#class-files-title`;
  }
  return "/app/calendar";
}

function searchKindLabel(
  item: AuthorizedSearchResult,
  t: ReturnType<typeof useI18n>["t"],
) {
  if (item.kind === "conversation") return t("home.search.kindConversation");
  if (item.kind === "file") return t("home.search.kindFile");
  return t("home.search.kindSession");
}

function searchContext(
  item: AuthorizedSearchResult,
  t: ReturnType<typeof useI18n>["t"],
) {
  if (item.kind !== "conversation") return item.context;
  return item.context === "direct"
    ? t("home.search.directConversation")
    : t("home.search.classConversation");
}

function formatDateTime(value: string, language: "en" | "vi") {
  return new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function formatBytes(value: number, language: "en" | "vi") {
  if (value < 1_000_000) return `${Math.max(1, Math.round(value / 1000))} KB`;
  return `${new Intl.NumberFormat(language === "vi" ? "vi-VN" : "en-US", {
    maximumFractionDigits: 1,
  }).format(value / 1_000_000)} MB`;
}

function HealthStatus() {
  const { t } = useI18n();
  const healthQuery = useQuery({
    queryKey: ["core-api", "health"],
    queryFn: ({ signal }) => getHealth({ baseUrl: getApiBaseUrl(), signal }),
  });

  if (healthQuery.isPending) {
    return <span className="health-skeleton">{t("home.serviceLoading")}</span>;
  }
  if (healthQuery.isSuccess) {
    return (
      <StatusBadge tone="success">
        {t("home.serviceReady", { environment: healthQuery.data.environment })}
      </StatusBadge>
    );
  }
  return <StatusBadge tone="danger">{t("home.serviceError")}</StatusBadge>;
}
