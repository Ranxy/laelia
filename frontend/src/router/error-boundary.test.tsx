import { act } from "react";
import { createRoot } from "react-dom/client";
import {
  createMemoryRouter,
  type RouteObject,
  RouterProvider,
} from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

// RouterErrorBoundary uses react-i18next; stub it so the test asserts the
// boundary renders the error rather than depending on the i18n provider.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

import { RouterErrorBoundary } from "./error-boundary";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

describe("RouterErrorBoundary", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("TestRouter_ErrorElementRendersOnError: renders the error UI when a route loader throws", async () => {
    const boom = new Error("kaboom");
    const routes: RouteObject[] = [
      {
        errorElement: <RouterErrorBoundary />,
        children: [
          {
            path: "/",
            loader: () => {
              throw boom;
            },
            element: <div>should-not-render</div>,
          },
        ],
      },
    ];
    const router = createMemoryRouter(routes, { initialEntries: ["/"] });

    const container = document.createElement("div");
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () => {
      root.render(<RouterProvider router={router} />);
    });

    // The loader throws synchronously, but react-router's data router settles
    // the error boundary on a follow-up render; flush microtasks so the
    // errorElement is mounted before we assert on it.
    await act(async () => {
      await Promise.resolve();
    });

    // The boundary renders an alert region with the translated title and the
    // thrown error message.
    const alert = container.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(container.textContent).toContain("router.error-title");
    expect(container.textContent).toContain("kaboom");
    expect(container.textContent).not.toContain("should-not-render");

    await act(async () => {
      root.unmount();
      container.remove();
    });
  });
});
