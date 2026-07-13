import { QueryClient } from "@tanstack/react-query";

export function createTutorHubQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        retry: 1,
        retryDelay: 350,
        refetchOnWindowFocus: false,
      },
    },
  });
}
