import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { SettingsIndex, SettingsMenuPage } from "./settings-menu";

const mock = vi.hoisted(() => ({
  debugToggle: vi.fn(),
  signOut: vi.fn(),
  setLocale: vi.fn(),
  isDesktop: false,
}));

vi.mock("@/components/user-menu", () => ({
  useDebugConfig: () => ({
    isAdmin: true,
    enabled: false,
    loaded: true,
    toggle: mock.debugToggle,
  }),
  useLogout: () => mock.signOut,
}));

vi.mock("@/lib/i18n", () => ({
  LOCALES: [
    { value: "en-US", label: "English" },
    { value: "zh-CN", label: "中文" },
  ],
  setLocale: mock.setLocale,
}));

vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: () => mock.isDesktop,
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn, i18n: { language: "en-US" } }),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/settings"]}>
      <Routes>
        <Route path="/settings" element={<SettingsMenuPage />} />
        <Route
          path="/settings/storage"
          element={<div data-testid="storage" />}
        />
        <Route path="/settings/users" element={<div data-testid="users" />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      email: "admin@example.com",
      permissions: [
        "laelia.settings.get",
        "laelia.settings.update",
        "laelia.users.list",
        "laelia.machines.get",
        "laelia.roles.list",
        "laelia.iam.getPolicy",
        "laelia.groups.list",
        "laelia.apiProviders.list",
        "laelia.auditLogs.search",
        "laelia.pushConfig.update",
      ],
    } as never,
  });
  mock.debugToggle.mockReset();
  mock.signOut.mockReset();
  mock.setLocale.mockReset();
  mock.isDesktop = false;
});

describe("settings-menu", () => {
  it("renders every settings entry for a full-permission admin", () => {
    renderPage();

    expect(screen.getByText("sidebar.settings-profile")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-storage")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-general")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-smtp")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-agents")).toBeInTheDocument();
    expect(
      screen.getByText("sidebar.settings-notifications")
    ).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-users")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-roles")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-iam")).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-groups")).toBeInTheDocument();
    expect(
      screen.getByText("sidebar.settings-api-providers")
    ).toBeInTheDocument();
    expect(
      screen.getByText("sidebar.settings-mcp-servers")
    ).toBeInTheDocument();
    expect(screen.getByText("sidebar.settings-audit")).toBeInTheDocument();
    expect(screen.getByText("sidebar.machines")).toBeInTheDocument();
  });

  it("hides permission-gated entries for a caller without those permissions", () => {
    useAppStore.setState({
      currentUser: {
        name: "users/2",
        title: "User",
        email: "user@example.com",
        permissions: [],
      } as never,
    });

    renderPage();

    expect(screen.getByText("sidebar.settings-profile")).toBeInTheDocument();
    expect(
      screen.getByText("sidebar.settings-mcp-servers")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("sidebar.settings-storage")
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("sidebar.settings-users")
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("sidebar.settings-audit")
    ).not.toBeInTheDocument();
    expect(screen.queryByText("sidebar.machines")).not.toBeInTheDocument();
  });

  it("navigates when a menu entry is clicked", () => {
    renderPage();

    fireEvent.click(screen.getByText("sidebar.settings-storage"));
    expect(screen.getByTestId("storage")).toBeInTheDocument();
  });

  it("shows the current user identity", () => {
    renderPage();

    expect(screen.getByText("Admin")).toBeInTheDocument();
    expect(screen.getByText("admin@example.com")).toBeInTheDocument();
  });

  it("switches the locale from the language select", () => {
    renderPage();

    fireEvent.click(screen.getByRole("combobox"));
    const item = screen.getByText("中文");
    // Base UI select items commit on pointer events; fire the full sequence.
    fireEvent.pointerDown(item);
    fireEvent.pointerUp(item);
    fireEvent.click(item);

    expect(mock.setLocale).toHaveBeenCalledWith("zh-CN");
  });

  it("confirms sign-out before logging out", async () => {
    renderPage();

    fireEvent.click(screen.getByText("common.sign-out"));
    expect(
      screen.getByText("common.sign-out-confirm-title")
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "common.sign-out" }));

    await waitFor(() => expect(mock.signOut).toHaveBeenCalledTimes(1));
  });
});

describe("settings-index", () => {
  it("redirects to the profile page on desktop", () => {
    mock.isDesktop = true;

    render(
      <MemoryRouter initialEntries={["/settings"]}>
        <Routes>
          <Route path="/settings" element={<SettingsIndex />} />
          <Route
            path="/settings/profile"
            element={<div data-testid="profile" />}
          />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getByTestId("profile")).toBeInTheDocument();
  });
});
