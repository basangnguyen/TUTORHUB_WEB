/* eslint-disable react-refresh/only-export-components -- The exported route configuration is intentionally colocated with its guard. */

import {
  Navigate,
  Outlet,
  type RouteObject,
  useLocation,
} from "react-router-dom";
import type { CurrentUser } from "@tutorhub/api-client";
import { Button, ErrorState } from "@tutorhub/ui";
import { RefreshCw } from "lucide-react";
import { lazy } from "react";
import { AppShell } from "../components/AppShell";
import { DashboardPage } from "../pages/AppPages";
import {
  ClassroomDetailPage,
  ClassroomListPage,
} from "../pages/ClassroomPages";
import {
  WorkspaceOnboardingPage,
  WorkspaceSelectionPage,
} from "../pages/WorkspacePages";
import { ProfileSettingsPage } from "../pages/ProfileSettingsPage";
import { WorkspaceManagementPage } from "../pages/WorkspaceManagementPage";
import { AuditLogPage } from "../pages/AuditLogPage";
import { NotificationCenterPage } from "../pages/NotificationCenterPage";
import { NotificationPreferencesPage } from "../pages/NotificationPreferencesPage";
import {
  MembershipInvitationAcceptedPage,
  MembershipInvitationPage,
} from "../pages/MembershipInvitationPage";
import { ClassInvitationPage } from "../pages/ClassInvitationPage";
import { ExternalCalendarRSVPPage } from "../pages/ExternalCalendarRSVPPage";
import { PublicAvailabilityPollPage } from "../pages/PublicAvailabilityPollPage";
import {
  ForbiddenPage,
  AuthenticationErrorPage,
  LoadingScreen,
  NotFoundPage,
  OfflinePage,
  RouteErrorBoundary,
  SignInPage,
  SignedOutPage,
} from "../pages/RouteStates";
import { useSession } from "./session";
import { useI18n, type TranslationKey } from "./i18n";
import { useTenantCapabilities } from "./tenantCapabilities";

const ClassroomPreJoinPage = lazy(() =>
  import("../pages/LiveKitPages").then((module) => ({
    default: module.ClassroomPreJoinPage,
  })),
);
const CalendarPage = lazy(() =>
  import("../pages/CalendarPage").then((module) => ({
    default: module.CalendarPage,
  })),
);
const ConversationsPage = lazy(() =>
  import("../pages/ConversationsPage").then((module) => ({
    default: module.ConversationsPage,
  })),
);
const AvailabilityPollManagementPage = lazy(() =>
  import("../pages/AvailabilityPollManagementPage").then((module) => ({
    default: module.AvailabilityPollManagementPage,
  })),
);
const ClassroomRoomPage = lazy(() =>
  import("../pages/LiveKitPages").then((module) => ({
    default: module.ClassroomRoomPage,
  })),
);

export interface NavigationItem {
  to: string;
  labelKey: TranslationKey;
  requiredPermission?: CurrentUser["permissions"][number];
  showInSidebar?: boolean;
}

export const navigationItems: readonly NavigationItem[] = [
  { to: "/app/home", labelKey: "nav.home" },
  {
    to: "/app/notifications",
    labelKey: "nav.notifications",
    showInSidebar: false,
  },
  { to: "/app/classrooms", labelKey: "nav.classrooms" },
  { to: "/app/messages", labelKey: "nav.messages" },
  { to: "/app/calendar", labelKey: "nav.calendar" },
  {
    to: "/app/workspace",
    labelKey: "nav.workspace",
    requiredPermission: "tenant.view",
  },
  { to: "/app/settings", labelKey: "nav.settings" },
];

export function getVisibleNavigationItems(
  permissions: CurrentUser["permissions"],
) {
  return navigationItems.filter(
    (item) =>
      item.showInSidebar !== false &&
      (!item.requiredPermission ||
        permissions.includes(item.requiredPermission)),
  );
}

function ProtectedRoute() {
  const session = useSession();
  const location = useLocation();

  if (!navigator.onLine) {
    return <OfflinePage />;
  }

  if (session.status === "loading") {
    return <LoadingScreen />;
  }

  if (session.status === "error") {
    return <AuthenticationErrorPage />;
  }

  if (session.status === "unauthenticated") {
    return <Navigate replace state={{ from: location }} to="/sign-in" />;
  }

  return <Outlet />;
}

function WorkspaceRoute() {
  const session = useSession();
  const currentUser = session.currentUser;

  if (!currentUser) {
    return <AuthenticationErrorPage />;
  }
  if (!currentUser.active_tenant && currentUser.memberships.length === 0) {
    return <WorkspaceOnboardingPage />;
  }
  if (!currentUser.active_tenant) {
    return <WorkspaceSelectionPage />;
  }

  return <Outlet />;
}

function PermissionRoute({
  permission,
}: {
  permission: CurrentUser["permissions"][number];
}) {
  const session = useSession();

  if (!session.currentUser?.permissions.includes(permission)) {
    return <Navigate replace to="/forbidden" />;
  }

  return <Outlet />;
}

function NotificationFeatureRoute() {
  const { t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const capabilities = useTenantCapabilities(tenantID, Boolean(tenantID));

  if (capabilities.isPending) {
    return <LoadingScreen />;
  }
  if (capabilities.isError) {
    return (
      <div className="page-content notification-center">
        <ErrorState
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void capabilities.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={t("notifications.capabilitiesErrorDescription")}
          title={t("notifications.capabilitiesErrorTitle")}
        />
      </div>
    );
  }
  if (capabilities.data?.features.in_app_notifications?.enabled !== true) {
    return <Navigate replace to="/forbidden" />;
  }

  return <Outlet />;
}

function AvailabilityPollFeatureRoute() {
  const { t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const capabilities = useTenantCapabilities(tenantID, Boolean(tenantID));

  if (capabilities.isPending) {
    return <LoadingScreen />;
  }
  if (capabilities.isError) {
    return (
      <div className="page-content availability-poll-route-state">
        <ErrorState
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void capabilities.refetch()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={t("availabilityPolls.capabilitiesErrorDescription")}
          title={t("availabilityPolls.capabilitiesErrorTitle")}
        />
      </div>
    );
  }
  if (capabilities.data?.features.availability_polls?.enabled !== true) {
    return <Navigate replace to="/forbidden" />;
  }

  return <Outlet />;
}

function throwSystemError(): never {
  throw new Response("Temporary route error", {
    status: 503,
    statusText: "Service unavailable",
  });
}

export function createAppRoutes(): RouteObject[] {
  return [
    {
      path: "/",
      element: <Navigate replace to="/app/home" />,
    },
    {
      path: "/app",
      element: <ProtectedRoute />,
      hydrateFallbackElement: <LoadingScreen />,
      children: [
        {
          element: <WorkspaceRoute />,
          children: [
            {
              element: <AppShell />,
              errorElement: <RouteErrorBoundary />,
              children: [
                { index: true, element: <Navigate replace to="home" /> },
                { path: "home", element: <DashboardPage /> },
                {
                  path: "classrooms",
                  element: <ClassroomListPage />,
                },
                {
                  path: "classrooms/:classId",
                  element: <ClassroomDetailPage />,
                },
                {
                  path: "classrooms/:classId/prejoin",
                  element: <ClassroomPreJoinPage />,
                },
                {
                  path: "messages",
                  element: <ConversationsPage />,
                },
                {
                  path: "messages/:conversationId",
                  element: <ConversationsPage />,
                },
                {
                  path: "calendar",
                  element: <CalendarPage />,
                },
                {
                  element: <AvailabilityPollFeatureRoute />,
                  children: [
                    {
                      path: "calendar/availability-polls",
                      element: <AvailabilityPollManagementPage />,
                    },
                  ],
                },
                {
                  path: "settings",
                  element: <ProfileSettingsPage />,
                },
                {
                  element: <NotificationFeatureRoute />,
                  children: [
                    {
                      path: "notifications",
                      element: <NotificationCenterPage />,
                    },
                    {
                      path: "notifications/preferences",
                      element: <NotificationPreferencesPage />,
                    },
                  ],
                },
                {
                  path: "workspace",
                  element: <WorkspaceManagementPage />,
                },
                {
                  element: <PermissionRoute permission="audit.view" />,
                  children: [
                    {
                      path: "workspace/audit",
                      element: <AuditLogPage />,
                    },
                  ],
                },
                {
                  path: "system-error",
                  element: <div aria-hidden="true" />,
                  loader: throwSystemError,
                },
              ],
            },
            {
              path: "classrooms/:classId/room",
              element: <ClassroomRoomPage />,
              errorElement: <RouteErrorBoundary />,
            },
          ],
        },
      ],
    },
    { path: "/forbidden", element: <ForbiddenPage /> },
    { path: "/class-invite", element: <ClassInvitationPage /> },
    { path: "/calendar/respond", element: <ExternalCalendarRSVPPage /> },
    {
      path: "/availability/:publicId",
      element: <PublicAvailabilityPollPage />,
    },
    { path: "/invite", element: <MembershipInvitationPage /> },
    {
      path: "/invite/accepted",
      element: <MembershipInvitationAcceptedPage />,
    },
    { path: "/sign-in", element: <SignInPage /> },
    { path: "/signed-out", element: <SignedOutPage /> },
    { path: "/offline", element: <OfflinePage /> },
    { path: "*", element: <NotFoundPage /> },
  ];
}
