// @vitest-environment jsdom
import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/react/stores";
import { RootLayout } from "./root-layout";

// Mock the connect clients so the real auth store's loadSession never hits the
// network; getCurrentUser is controlled per test to set logged-in vs logged-out.
const mocks = vi.hoisted(() => ({
  getCurrentUser: vi.fn(),
}));

vi.mock("@/connect", () => ({
  agentServiceClient: {},
  authServiceClient: {},
  commandServiceClient: {},
  settingServiceClient: {},
  userServiceClient: { getCurrentUser: mocks.getCurrentUser },
}));

const PROTECTED_MARKER = "protected-chat";
const SIGNIN_MARKER = "signin-page";

function renderAt(path: string) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: <RootLayout />,
        children: [
          { path: "auth/signin", element: <div>{SIGNIN_MARKER}</div> },
          { path: "agents/:id/chat", element: <div>{PROTECTED_MARKER}</div> },
        ],
      },
    ],
    { initialEntries: [path] }
  );
  const root = createRoot(container);
  act(() => {
    root.render(createElement(RouterProvider, { router }));
  });
  return {
    container,
    unmount: () => {
      act(() => {
        root.unmount();
        container.remove();
      });
    },
  };
}

describe("RootLayout auth guard", () => {
  beforeEach(() => {
    useAppStore.setState({
      currentUser: null,
      token: null,
      isLoggedIn: false,
      sessionLoaded: false,
    });
    mocks.getCurrentUser.mockReset();
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("redirects a logged-out user away from a protected route without flashing it", async () => {
    mocks.getCurrentUser.mockRejectedValue(new Error("unauthenticated"));

    const { container, unmount } = renderAt("/agents/x/chat");

    // Let the session load + reactive redirect effect settle.
    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).not.toContain(PROTECTED_MARKER);
    expect(container.textContent).toContain(SIGNIN_MARKER);
    unmount();
  });

  it("allows a logged-out user to reach an auth route", async () => {
    mocks.getCurrentUser.mockRejectedValue(new Error("unauthenticated"));

    const { container, unmount } = renderAt("/auth/signin");

    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain(SIGNIN_MARKER);
    unmount();
  });

  it("renders a protected route for a logged-in user", async () => {
    mocks.getCurrentUser.mockResolvedValue({ name: "users/1", email: "a@b.c" });

    const { container, unmount } = renderAt("/agents/x/chat");

    await act(async () => {
      await Promise.resolve();
    });

    expect(container.textContent).toContain(PROTECTED_MARKER);
    unmount();
  });
});
