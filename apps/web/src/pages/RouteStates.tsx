import { isRouteErrorResponse, useRouteError } from "react-router-dom";
import { Link } from "react-router-dom";
import { useI18n } from "../app/i18n";

interface StatusPageProps {
  description: string;
  title: string;
  retry?: boolean;
}

export function LoadingScreen() {
  const { t } = useI18n();

  return (
    <main className="app-loading" aria-live="polite">
      <div className="app-loading__mark" aria-hidden="true">
        TH
      </div>
      <p>{t("app.loading")}</p>
    </main>
  );
}

function StatusPage({ description, retry = false, title }: StatusPageProps) {
  const { t } = useI18n();

  return (
    <main className="route-state">
      <section aria-labelledby="route-state-title">
        <p>{t("brand.product")}</p>
        <h1 id="route-state-title">{title}</h1>
        <span>{description}</span>
        <div className="route-state__actions">
          <Link to="/app/home">{t("state.goHome")}</Link>
          {retry && (
            <button onClick={() => window.location.reload()} type="button">
              {t("state.retry")}
            </button>
          )}
        </div>
      </section>
    </main>
  );
}

export function ForbiddenPage() {
  const { t } = useI18n();
  return (
    <StatusPage
      description={t("state.forbiddenDescription")}
      title={t("state.forbiddenTitle")}
    />
  );
}

export function NotFoundPage() {
  const { t } = useI18n();
  return (
    <StatusPage
      description={t("state.notFoundDescription")}
      title={t("state.notFoundTitle")}
    />
  );
}

export function OfflinePage() {
  const { t } = useI18n();
  return (
    <StatusPage
      description={t("state.offlineDescription")}
      retry
      title={t("state.offlineTitle")}
    />
  );
}

export function RouteErrorBoundary() {
  const { t } = useI18n();
  const error = useRouteError();
  const description = isRouteErrorResponse(error)
    ? `${t("state.errorDescription")} (${error.status})`
    : error instanceof Error
      ? error.message
      : t("state.errorDescription");

  return (
    <StatusPage description={description} retry title={t("state.errorTitle")} />
  );
}
