import { fireEvent, render, screen } from "@testing-library/react";
import {
  createMemoryRouter,
  type RouteObject,
  RouterProvider,
} from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
  permissions: [] as string[] | undefined,
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: unknown) => unknown) =>
    selector({ currentUser: { permissions: mock.permissions } }),
}));

import {
  deniedByPermissions,
  permissionGroupsFor,
  RoutePermissionGate,
} from "./route-permission-gate";

const routes: RouteObject[] = [
  {
    path: "/",
    element: <RoutePermissionGate />,
    children: [
      { index: true, element: <div>home</div> },
      {
        path: "settings/users",
        handle: { permission: "laelia.users.list" },
        element: <div>users page</div>,
      },
      {
        path: "settings/agents",
        handle: {
          permission: ["laelia.settings.get", "laelia.settings.update"],
        },
        element: <div>agents page</div>,
      },
    ],
  },
];

function renderGate(initial = "/settings/users") {
  const router = createMemoryRouter(routes, {
    initialEntries: [initial],
  });
  render(<RouterProvider router={router} />);
  return router;
}

describe("route permission helpers", () => {
  it("collects only the handles that declare permissions (array = any-of)", () => {
    expect(
      permissionGroupsFor([
        { name: "chat" },
        { permission: "laelia.users.list" },
        { permission: ["laelia.settings.get", "laelia.settings.update"] },
        "not-a-handle",
      ])
    ).toEqual([
      ["laelia.users.list"],
      ["laelia.settings.get", "laelia.settings.update"],
    ]);
  });

  it("denies only when a group is fully ungranted", () => {
    expect(deniedByPermissions([["a", "b"]], ["b"])).toBe(false);
    expect(deniedByPermissions([["a", "b"]], undefined)).toBe(true);
    expect(deniedByPermissions([["a"], ["b"]], ["a"])).toBe(true);
    expect(deniedByPermissions([], [])).toBe(false);
  });
});

describe("RoutePermissionGate", () => {
  beforeEach(() => {
    mock.permissions = [];
  });

  it("refuses the page and shows the forbidden surface without the permission", () => {
    renderGate();
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.queryByText("users page")).toBeNull();
  });

  it("renders the page when the permission is granted", () => {
    mock.permissions = ["laelia.users.list"];
    renderGate();
    expect(screen.getByText("users page")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("accepts any-of permission arrays", () => {
    mock.permissions = ["laelia.settings.update"];
    renderGate("/settings/agents");
    expect(screen.getByText("agents page")).toBeTruthy();
  });

  it("navigates home from the forbidden surface", () => {
    renderGate();
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByText("home")).toBeTruthy();
  });
});
