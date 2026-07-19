/* eslint-disable react-refresh/only-export-components -- The exported route configuration is intentionally colocated with its guard. */

import {
  Navigate,
  Outlet,
  type RouteObject,
  useLocation,
} from "react-router-dom";
import type { CurrentUser } from "@tutorhub/api-client";
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
import {
  MembershipInvitationAcceptedPage,
  MembershipInvitationPage,
} from "../pages/MembershipInvitationPage";
import { ClassInvitationPage } from "../pages/ClassInvitationPage";
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
import type { TranslationKey } from "./i18n";

const ClassroomPreJoinPage = lazy(() =>
  import("../pages/LiveKitPages").then((module) => ({
    default: module.ClassroomPreJoinPage,
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
}

export const navigationItems: readonly NavigationItem[] = [
  { to: "/app/home", labelKey: "nav.home" },
  { to: "/app/classrooms", labelKey: "nav.classrooms" },
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
      !item.requiredPermission || permissions.includes(item.requiredPermission),
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
                  path: "settings",
                  element: <ProfileSettingsPage />,
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
