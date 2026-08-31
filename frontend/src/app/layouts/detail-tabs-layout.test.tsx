import { render, screen } from "@testing-library/react";
import { CircleUser, FolderTree } from "lucide-react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { type DetailTab, DetailTabsLayout } from "./detail-tabs-layout";

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function renderLayout(
  path: string,
  { workspaceGate = true }: { workspaceGate?: boolean } = {}
) {
  const tabs: DetailTab[] = [
    {
      key: "profile",
      icon: CircleUser,
      labelKey: "detail.tab-profile",
      route: "detail.profile",
    },
    {
      key: "workspace",
      icon: FolderTree,
      labelKey: "detail.tab-workspace",
      route: "detail.workspace",
      gate: workspaceGate,
    },
  ];
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/things/:thingId/*"
          element={<DetailTabsLayout idParam="thingId" tabs={tabs} />}
        >
          <Route
            path="workspace"
            element={<div data-testid="workspace-content" />}
          />
          <Route path="*" element={<div data-testid="fallback" />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe("detail-tabs-layout", () => {
  it("resolves the active tab from the URL segment after the id param", () => {
    renderLayout("/things/t1/workspace");

    expect(
      screen.getByRole("tab", { name: "detail.tab-workspace" })
    ).toHaveAttribute("aria-selected", "true");
    expect(
      screen.getByRole("tab", { name: "detail.tab-profile" })
    ).toHaveAttribute("aria-selected", "false");
    expect(screen.getByTestId("workspace-content")).toBeInTheDocument();
  });

  it("falls back to the first tab when no tab segment is present", () => {
    renderLayout("/things/t1");

    expect(
      screen.getByRole("tab", { name: "detail.tab-profile" })
    ).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("fallback")).toBeInTheDocument();
  });

  it("renders gated tabs only while the gate holds", () => {
    renderLayout("/things/t1", { workspaceGate: false });

    expect(
      screen.queryByRole("tab", { name: "detail.tab-workspace" })
    ).toBeNull();
    expect(
      screen.getByRole("tab", { name: "detail.tab-profile" })
    ).toBeInTheDocument();
  });
});
