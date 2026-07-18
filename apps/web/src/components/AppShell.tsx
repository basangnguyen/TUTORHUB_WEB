import { Button, IconButton } from "@tutorhub/ui";
import { LogOut, Menu, RefreshCw, X } from "lucide-react";
import { useEffect, useState } from "react";
import {
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
  useNavigation,
} from "react-router-dom";
import { navigationItems } from "../app/routes";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";
import { useWorkspaceActions } from "../app/workspaces";

function useOnlineStatus() {
  const [isOnline, setIsOnline] = useState(() => navigator.onLine);

  useEffect(() => {
    const markOnline = () => setIsOnline(true);
    const markOffline = () => setIsOnline(false);

    window.addEventListener("online", markOnline);
    window.addEventListener("offline", markOffline);

    return () => {
      window.removeEventListener("online", markOnline);
      window.removeEventListener("offline", markOffline);
    };
  }, []);

  return isOnline;
}

export function AppShell() {
  const { language, setLanguage, t } = useI18n();
  const session = useSession();
  const navigate = useNavigate();
  const { switchWorkspace } = useWorkspaceActions({
    onSwitchSuccess: () => navigate("/app/home", { replace: true }),
  });
  const location = useLocation();
  const navigation = useNavigation();
  const isOnline = useOnlineStatus();
  const [isNavigationOpen, setIsNavigationOpen] = useState(false);

  const activeTenant = session.currentUser?.active_tenant;
  const activeMemberships =
    session.currentUser?.memberships.filter(
      (membership) => membership.status === "active",
    ) ?? [];
  const tenantOptions = activeMemberships.length
    ? activeMemberships
    : activeTenant?.status === "active"
      ? [activeTenant]
      : [];

  const currentItem = navigationItems.find((item) =>
    location.pathname.startsWith(item.to),
  );
  const role = session.currentUser?.active_tenant?.role ?? "guest";
  const roleLabel =
    role === "org_admin"
      ? t("shell.role.admin")
      : role === "teacher"
        ? t("shell.role.teacher")
        : role === "student"
          ? t("shell.role.student")
          : t("shell.role.guest");

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">
        {t("shell.skip")}
      </a>

      <aside
        aria-label={t("shell.navigation")}
        className={`app-sidebar${isNavigationOpen ? " app-sidebar--open" : ""}`}
      >
        <div className="app-brand">
          <span className="app-brand__mark" aria-hidden="true">
            TH
          </span>
          <span>
            <strong>{t("brand.product")}</strong>
            <small>{t("brand.version")}</small>
          </span>
        </div>

        <nav className="app-navigation" aria-label={t("shell.navigation")}>
          {navigationItems.map((item) => (
            <NavLink
              className={({ isActive }) =>
                `app-navigation__link${isActive ? " app-navigation__link--active" : ""}`
              }
              key={item.to}
              onClick={() => setIsNavigationOpen(false)}
              to={item.to}
            >
              {t(item.labelKey)}
            </NavLink>
          ))}
        </nav>

        <div className="app-sidebar__footer">
          <span className="app-sidebar__role">{roleLabel}</span>
          <strong>
            {session.currentUser?.user.display_name ?? t("shell.profile")}
          </strong>
        </div>
      </aside>

      <div className="app-workspace">
        <header className="app-topbar">
          <div className="app-topbar__context">
            <IconButton
              aria-expanded={isNavigationOpen}
              className="menu-toggle"
              label={
                isNavigationOpen
                  ? t("shell.closeNavigation")
                  : t("shell.openNavigation")
              }
              onClick={() => setIsNavigationOpen((open) => !open)}
              size="sm"
              variant="secondary"
            >
              {isNavigationOpen ? <X /> : <Menu />}
            </IconButton>
            <span className="app-topbar__eyebrow">{t("brand.product")}</span>
            <strong>
              {currentItem ? t(currentItem.labelKey) : t("nav.home")}
            </strong>
          </div>

          <div className="app-topbar__actions">
            <label className="workspace-select">
              <span className="visually-hidden">
                {t("workspace.activeLabel")}
              </span>
              {tenantOptions.length > 1 ? (
                <select
                  aria-label={t("workspace.activeLabel")}
                  aria-busy={switchWorkspace.isPending || undefined}
                  disabled={switchWorkspace.isPending}
                  onChange={(event) => {
                    if (
                      !switchWorkspace.isPending &&
                      event.target.value !== activeTenant?.id
                    ) {
                      switchWorkspace.mutate(event.target.value);
                    }
                  }}
                  value={activeTenant?.id ?? ""}
                >
                  {tenantOptions.map((tenant) => (
                    <option key={tenant.id} value={tenant.id}>
                      {tenant.name}
                    </option>
                  ))}
                </select>
              ) : (
                <span>{activeTenant?.name}</span>
              )}
            </label>
            <span
              className={`connection-status${isOnline ? " connection-status--online" : ""}`}
              role="status"
            >
              {isOnline ? t("shell.online") : t("shell.offline")}
            </span>
            <label className="language-select">
              <span className="visually-hidden">{t("shell.language")}</span>
              <select
                aria-label={t("shell.language")}
                onChange={(event) =>
                  setLanguage(event.target.value as typeof language)
                }
                value={language}
              >
                <option value="vi">Tiếng Việt</option>
                <option value="en">English</option>
              </select>
            </label>
            <Button
              className="app-topbar__logout"
              leadingIcon={<LogOut />}
              onClick={() => void session.signOut()}
              size="sm"
              variant="secondary"
            >
              {t("auth.signOut")}
            </Button>
          </div>
        </header>

        {switchWorkspace.isPending && (
          <p className="visually-hidden" role="status">
            {t("workspace.switching")}
          </p>
        )}

        {switchWorkspace.isError && (
          <section className="workspace-switch-error" role="alert">
            <span>{t("workspace.selectError")}</span>
            <Button
              disabled={!switchWorkspace.variables}
              onClick={() => {
                if (switchWorkspace.variables) {
                  switchWorkspace.mutate(switchWorkspace.variables);
                }
              }}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          </section>
        )}

        {!isOnline && (
          <section className="connectivity-notice" role="status">
            <div>
              <strong>{t("shell.offline")}</strong>
              <p>{t("shell.offlineMessage")}</p>
            </div>
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => window.location.reload()}
              size="sm"
              variant="secondary"
            >
              {t("shell.retryConnection")}
            </Button>
          </section>
        )}

        <main id="main-content" tabIndex={-1}>
          {navigation.state !== "idle" && (
            <div className="route-progress" aria-live="polite" />
          )}
          <Outlet />
        </main>
      </div>
    </div>
  );
}
