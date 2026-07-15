import { QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@tutorhub/ui";
import { RouterProvider, createBrowserRouter } from "react-router-dom";
import { Suspense, useState } from "react";
import { I18nProvider } from "./app/i18n";
import { createTutorHubQueryClient } from "./app/queryClient";
import { createAppRoutes } from "./app/routes";
import { SessionProvider } from "./app/session";
import { LoadingScreen } from "./pages/RouteStates";

export function App() {
  const [queryClient] = useState(createTutorHubQueryClient);
  const [router] = useState(() => createBrowserRouter(createAppRoutes()));

  return (
    <TooltipProvider delayDuration={350}>
      <QueryClientProvider client={queryClient}>
        <I18nProvider>
          <SessionProvider>
            <Suspense fallback={<LoadingScreen />}>
              <RouterProvider router={router} />
            </Suspense>
          </SessionProvider>
        </I18nProvider>
      </QueryClientProvider>
    </TooltipProvider>
  );
}
