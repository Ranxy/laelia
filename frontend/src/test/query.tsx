import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";

export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // Page tests assert deterministic failure paths and toasts: no
        // retries, and keep entries alive for the whole test regardless of
        // unmount timing.
        retry: false,
        gcTime: Infinity,
      },
    },
  });
}

// Renders a component tree inside a QueryClientProvider backed by a fresh
// per-test client. Settings-page tests migrate to this as their pages move
// onto useResourceQuery (ADR-1).
export function renderWithQueryClient(ui: ReactElement) {
  const client = createTestQueryClient();
  const view = render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>
  );
  return { client, ...view };
}
