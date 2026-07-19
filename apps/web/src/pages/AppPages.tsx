import { useQuery } from "@tanstack/react-query";
import { getHealth } from "@tutorhub/api-client";
import { StatusBadge } from "@tutorhub/ui";
import { Link } from "react-router-dom";
import { getVisibleNavigationItems } from "../app/routes";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export function DashboardPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const visibleNavigationItems = getVisibleNavigationItems(
    session.currentUser?.permissions ?? [],
  );
  const activeTenant = session.currentUser?.active_tenant;
  const roleLabel =
    activeTenant?.role === "org_admin"
      ? t("shell.role.admin")
      : activeTenant?.role === "teacher"
        ? t("shell.role.teacher")
        : activeTenant?.role === "student"
          ? t("shell.role.student")
          : t("shell.role.guest");
  const healthQuery = useQuery({
    queryKey: ["core-api", "health"],
    queryFn: ({ signal }) => getHealth({ baseUrl: getApiBaseUrl(), signal }),
  });

  return (
    <div className="page-content">
      <header className="page-heading">
        <p>{t("home.kicker")}</p>
        <h1>{t("home.title")}</h1>
        <span>{t("home.description")}</span>
      </header>

      <div className="overview-layout">
        <section
          aria-labelledby="workspace-heading"
          className="workspace-section"
        >
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
              <dd>{language === "vi" ? "Tiếng Việt" : "English"}</dd>
            </div>
          </dl>
        </section>

        <section aria-labelledby="core-api-heading" className="service-section">
          <div className="service-section__heading">
            <div>
              <h2 id="core-api-heading">{t("home.serviceTitle")}</h2>
              <p>{t("home.serviceDescription")}</p>
            </div>
            <HealthStatus />
          </div>

          {healthQuery.isError && (
            <div className="service-error" role="alert">
              <span>
                {healthQuery.error instanceof Error
                  ? healthQuery.error.message
                  : t("home.serviceError")}
              </span>
              <button onClick={() => void healthQuery.refetch()} type="button">
                {t("home.retry")}
              </button>
            </div>
          )}
        </section>
      </div>

      <section aria-labelledby="prepared-heading" className="prepared-section">
        <div className="prepared-section__heading">
          <h2 id="prepared-heading">{t("home.nextTitle")}</h2>
          <p>{t("home.nextDescription")}</p>
        </div>
        <ul className="module-list">
          {visibleNavigationItems
            .filter((item) => item.to !== "/app/home")
            .map((item) => (
              <li key={item.to}>
                <Link to={item.to}>
                  <span>{t(item.labelKey)}</span>
                  <small>
                    {t("home.openModule", { module: t(item.labelKey) })}
                  </small>
                </Link>
              </li>
            ))}
        </ul>
      </section>
    </div>
  );
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
