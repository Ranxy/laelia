import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { SettingsNotificationsPage } from "./settings-notifications";

const mock = vi.hoisted(() => ({
  getPushConfig: vi.fn(),
  updatePushConfig: vi.fn(),
}));

vi.mock("@/connect", () => ({
  notificationServiceClient: {
    getPushConfig: mock.getPushConfig,
    updatePushConfig: mock.updatePushConfig,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

function renderPage() {
  return render(<SettingsNotificationsPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: ["laelia.pushConfig.update"],
    } as never,
  });
  mock.getPushConfig.mockReset();
  mock.updatePushConfig.mockReset();
  toastMock.add.mockReset();
});

describe("settings-notifications", () => {
  it("shows the permission notice for callers without pushConfig.update", async () => {
    useAppStore.setState({
      currentUser: {
        name: "users/2",
        title: "User",
        permissions: [],
      } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.notifications.not-allowed")
    ).toBeInTheDocument();
    expect(mock.getPushConfig).not.toHaveBeenCalled();
  });

  it("renders the proxy toggle off when no proxy is configured", async () => {
    mock.getPushConfig.mockResolvedValue({ httpProxy: "" });

    renderPage();

    const toggle = await screen.findByRole("switch");
    expect(toggle).not.toBeChecked();
    expect(
      screen.getByText("settings.notifications.disabled")
    ).toBeInTheDocument();
  });

  it("renders the proxy toggle on with the stored proxy when configured", async () => {
    mock.getPushConfig.mockResolvedValue({ httpProxy: "http://proxy:8080" });

    renderPage();

    const toggle = await screen.findByRole("switch");
    expect(toggle).toBeChecked();
    expect(
      screen.getByText("settings.notifications.enabled")
    ).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText("settings.notifications.proxy-placeholder")
    ).toHaveValue("http://proxy:8080");
  });

  it("saves the edited proxy on save", async () => {
    mock.getPushConfig.mockResolvedValue({ httpProxy: "" });
    mock.updatePushConfig.mockResolvedValue({});

    renderPage();
    const toggle = await screen.findByRole("switch");
    fireEvent.click(toggle);

    const input = screen.getByPlaceholderText(
      "settings.notifications.proxy-placeholder"
    );
    fireEvent.change(input, { target: { value: "http://proxy:3128" } });
    fireEvent.click(
      screen.getByRole("button", { name: "settings.notifications.proxy-save" })
    );

    await waitFor(() => expect(mock.updatePushConfig).toHaveBeenCalledTimes(1));
    const req = mock.updatePushConfig.mock.calls[0][0] as {
      httpProxy: string;
    };
    expect(req.httpProxy).toBe("http://proxy:3128");
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "info",
        title: "settings.notifications.proxy-saved",
      })
    );
  });

  it("clears the stored proxy immediately when the toggle is turned off", async () => {
    mock.getPushConfig.mockResolvedValue({ httpProxy: "http://proxy:8080" });
    mock.updatePushConfig.mockResolvedValue({});

    renderPage();
    const toggle = await screen.findByRole("switch");
    fireEvent.click(toggle);

    await waitFor(() => expect(mock.updatePushConfig).toHaveBeenCalledTimes(1));
    const req = mock.updatePushConfig.mock.calls[0][0] as {
      httpProxy: string;
    };
    expect(req.httpProxy).toBe("");
    expect(toggle).not.toBeChecked();
  });

  it("reverts the toggle and toasts when clearing the proxy fails", async () => {
    mock.getPushConfig.mockResolvedValue({ httpProxy: "http://proxy:8080" });
    mock.updatePushConfig.mockRejectedValue(new Error("push down"));

    renderPage();
    const toggle = await screen.findByRole("switch");
    fireEvent.click(toggle);

    await waitFor(() => expect(toggle).toBeChecked());
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "error",
        title: "settings.notifications.proxy-save-failed",
        description: "push down",
      })
    );
  });
});
