import { Button } from "@tutorhub/ui";
import { LogIn, RefreshCw } from "lucide-react";
import {
  isRouteErrorResponse,
  useLocation,
  useRouteError,
} from "react-router-dom";
import { Link } from "react-router-dom";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";

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
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => window.location.reload()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
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

export function SignInPage() {
  const { t } = useI18n();
  const { signIn } = useSession();
  const location = useLocation();
  const state = location.state as {
    from?: { pathname?: string; search?: string };
  } | null;
  const returnTo = `${state?.from?.pathname ?? "/app/home"}${state?.from?.search ?? ""}`;

  return (
    <main className="route-state route-state--auth">
      <section aria-labelledby="sign-in-title">
        <p>{t("brand.product")}</p>
        <h1 id="sign-in-title">{t("auth.signInTitle")}</h1>
        <span>{t("auth.signInDescription")}</span>
        <div className="route-state__actions">
          <Button leadingIcon={<LogIn />} onClick={() => signIn(returnTo)}>
            {t("auth.signInAction")}
          </Button>
        </div>
      </section>
    </main>
  );
}

export function SignedOutPage() {
  const { t } = useI18n();
  const { signIn } = useSession();

  return (
    <main className="route-state route-state--auth">
      <section aria-labelledby="signed-out-title">
        <p>{t("brand.product")}</p>
        <h1 id="signed-out-title">{t("auth.signedOutTitle")}</h1>
        <span>{t("auth.signedOutDescription")}</span>
        <div className="route-state__actions">
          <Button leadingIcon={<LogIn />} onClick={() => signIn()}>
            {t("auth.signInAgain")}
          </Button>
        </div>
      </section>
    </main>
  );
}

export function AuthenticationErrorPage() {
  const { t } = useI18n();
  return (
    <StatusPage
      description={t("auth.unavailableDescription")}
      retry
      title={t("auth.unavailableTitle")}
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
