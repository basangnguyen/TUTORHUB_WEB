/* eslint-disable react-refresh/only-export-components -- The exported route configuration is intentionally colocated with its guard. */

import {
  Navigate,
  Outlet,
  type RouteObject,
  useLocation,
} from "react-router-dom";
import { AppShell } from "../components/AppShell";
import { DashboardPage, ModulePage } from "../pages/AppPages";
import {
  ClassroomDetailPage,
  ClassroomListPage,
} from "../pages/ClassroomPages";
import {
  WorkspaceOnboardingPage,
  WorkspaceSelectionPage,
} from "../pages/WorkspacePages";
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

export interface NavigationItem {
  to: string;
  labelKey: TranslationKey;
}

export const navigationItems: readonly NavigationItem[] = [
  { to: "/app/home", labelKey: "nav.home" },
  { to: "/app/classrooms", labelKey: "nav.classrooms" },
  { to: "/app/calendar", labelKey: "nav.calendar" },
  { to: "/app/messages", labelKey: "nav.messages" },
  { to: "/app/tasks", labelKey: "nav.tasks" },
  { to: "/app/resources", labelKey: "nav.drive" },
  { to: "/app/settings", labelKey: "nav.settings" },
];

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
                  path: "calendar",
                  element: <ModulePage moduleKey="nav.calendar" />,
                },
                {
                  path: "messages",
                  element: <ModulePage moduleKey="nav.messages" />,
                },
                {
                  path: "tasks",
                  element: <ModulePage moduleKey="nav.tasks" />,
                },
                {
                  path: "resources",
                  element: <ModulePage moduleKey="nav.drive" />,
                },
                {
                  path: "settings",
                  element: <ModulePage moduleKey="nav.settings" />,
                },
                {
                  path: "system-error",
                  element: <div aria-hidden="true" />,
                  loader: throwSystemError,
                },
              ],
            },
          ],
        },
      ],
    },
    { path: "/forbidden", element: <ForbiddenPage /> },
    { path: "/sign-in", element: <SignInPage /> },
    { path: "/signed-out", element: <SignedOutPage /> },
    { path: "/offline", element: <OfflinePage /> },
    { path: "*", element: <NotFoundPage /> },
  ];
}
