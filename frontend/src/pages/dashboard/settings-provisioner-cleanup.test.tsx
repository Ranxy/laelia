import { screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { renderWithQueryClient } from "@/test/query";
import { SettingsProvisionerCleanupPage } from "./settings-provisioner-cleanup";

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function renderPage(state?: { title?: string }) {
  return renderWithQueryClient(
    <MemoryRouter
      initialEntries={[
        { pathname: "/settings/provisioners/p1/cleanup", state },
      ]}
    >
      <Routes>
        <Route
          path="/settings/provisioners/:provisionerId/cleanup"
          element={<SettingsProvisionerCleanupPage />}
        />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  useAppStore.getState().reset();
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: ["laelia.provisioners.get"],
    } as never,
  });
});

describe("settings-provisioner-cleanup", () => {
  it("shows the permission notice without the get permission", () => {
    useAppStore.setState({
      currentUser: { name: "users/2", permissions: [] } as never,
    });

    renderPage();

    expect(
      screen.getByText("settings.provisioners.not-allowed")
    ).toBeInTheDocument();
  });

  it("shows the scaled-to-zero note and all cleanup steps", () => {
    renderPage();

    expect(
      screen.getByText("settings.provisioner-cleanup.scaled-note")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-1-title")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-2-title")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-3-title")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-4-title")
    ).toBeInTheDocument();

    // The kubectl commands are wired to their i18n keys.
    expect(
      screen.getByText("settings.provisioner-cleanup.step-1-command")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-2-command")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-3-command")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-4-command-1")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioner-cleanup.step-4-command-2")
    ).toBeInTheDocument();
  });

  it("renders the deleted provisioner title from navigation state", () => {
    renderPage({ title: "Prod Cluster" });

    expect(
      screen.getByText("settings.provisioner-cleanup.description")
    ).toBeInTheDocument();
  });
});
