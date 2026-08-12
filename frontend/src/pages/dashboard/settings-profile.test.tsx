import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { SettingsProfilePage } from "./settings-profile";

const mock = vi.hoisted(() => ({
  fetchCurrentUser: vi.fn(),
  updateUser: vi.fn(),
  uploadAvatar: vi.fn(),
  deleteAvatar: vi.fn(),
  getPushConfig: vi.fn(),
  listPushSubscriptions: vi.fn(),
  webPushSupported: vi.fn(),
  isDeviceSubscribed: vi.fn(),
  enableDesktopNotifications: vi.fn(),
  disableDesktopNotifications: vi.fn(),
  resizeImageFile: vi.fn(),
  avatarChange: vi.fn(),
  avatarRemove: vi.fn(),
}));

vi.mock("@/connect", () => ({
  userServiceClient: {
    uploadAvatar: mock.uploadAvatar,
    deleteAvatar: mock.deleteAvatar,
  },
  notificationServiceClient: {
    getPushConfig: mock.getPushConfig,
    listPushSubscriptions: mock.listPushSubscriptions,
  },
}));

vi.mock("@/lib/web-push", () => ({
  webPushSupported: mock.webPushSupported,
  isDeviceSubscribed: mock.isDeviceSubscribed,
  enableDesktopNotifications: mock.enableDesktopNotifications,
  disableDesktopNotifications: mock.disableDesktopNotifications,
}));

vi.mock("@/lib/avatar-cache", () => ({
  useAvatar: () => "avatar-url",
}));

vi.mock("@/composables/useAvatarEditor", () => ({
  useAvatarEditor: () => ({
    busy: false,
    onChange: mock.avatarChange,
    onRemove: mock.avatarRemove,
  }),
}));

vi.mock("@/lib/image-resize", () => ({
  resizeImageFile: mock.resizeImageFile,
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/components/chat/avatar", () => ({
  Avatar: () => <div data-testid="avatar" />,
}));

vi.mock("@/components/profile-common", () => ({
  Card: ({
    title,
    children,
    footer,
  }: {
    title: string;
    children: React.ReactNode;
    footer?: React.ReactNode;
  }) => (
    <section>
      <h2>{title}</h2>
      {children}
      {footer}
    </section>
  ),
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function user(overrides?: Partial<User>): User {
  return {
    name: "users/1",
    title: "Alice",
    email: "alice@example.com",
    phone: "123",
    description: "hello",
    avatar: "",
    chatPreferences: { enterToSend: true, preferredLanguage: 0 },
    ...overrides,
  } as unknown as User;
}

function renderPage() {
  return render(<SettingsProfilePage />);
}

// Locate the switch inside a row whose heading text matches labelKey.
function rowSwitch(labelKey: string) {
  const heading = screen.getByText(labelKey);
  const row = heading.closest(
    ".flex.items-center.justify-between"
  ) as HTMLElement;
  return within(row).getByRole("switch");
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: user(),
    fetchCurrentUser: mock.fetchCurrentUser,
    updateUser: mock.updateUser,
  } as never);
  mock.fetchCurrentUser.mockReset();
  mock.updateUser.mockReset();
  mock.uploadAvatar.mockReset();
  mock.deleteAvatar.mockReset();
  mock.getPushConfig.mockReset();
  mock.listPushSubscriptions.mockReset();
  mock.webPushSupported.mockReset();
  mock.isDeviceSubscribed.mockReset();
  mock.enableDesktopNotifications.mockReset();
  mock.disableDesktopNotifications.mockReset();
  mock.resizeImageFile.mockReset();
  mock.avatarChange.mockReset();
  mock.avatarRemove.mockReset();
  mock.fetchCurrentUser.mockResolvedValue(undefined);
  mock.updateUser.mockResolvedValue(undefined);
  mock.uploadAvatar.mockResolvedValue(undefined);
  mock.deleteAvatar.mockResolvedValue(undefined);
  mock.getPushConfig.mockResolvedValue({ enabled: false });
  mock.listPushSubscriptions.mockResolvedValue({ pushSubscriptions: [] });
  mock.webPushSupported.mockReturnValue(false);
  mock.isDeviceSubscribed.mockResolvedValue(false);
  mock.enableDesktopNotifications.mockResolvedValue(undefined);
  mock.disableDesktopNotifications.mockResolvedValue(undefined);
  mock.resizeImageFile.mockResolvedValue({
    data: new Uint8Array(),
    mimeType: "image/png",
  });
  toastMock.add.mockReset();
});

describe("settings-profile", () => {
  it("renders the current user's fields", async () => {
    renderPage();

    expect(await screen.findByDisplayValue("Alice")).toBeInTheDocument();
    expect(screen.getByDisplayValue("alice@example.com")).toBeInTheDocument();
    expect(screen.getByDisplayValue("123")).toBeInTheDocument();
    expect(screen.getByDisplayValue("hello")).toBeInTheDocument();
  });

  it("saves changed fields with an update mask", async () => {
    renderPage();
    const title = await screen.findByDisplayValue("Alice");
    fireEvent.change(title, { target: { value: "Alice B" } });
    fireEvent.change(screen.getByDisplayValue("alice@example.com"), {
      target: { value: "alice@new.com" },
    });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateUser).toHaveBeenCalledWith(
        "users/1",
        { title: "Alice B", email: "alice@new.com" },
        ["title", "email"]
      );
    });
    expect(mock.fetchCurrentUser).toHaveBeenCalled();
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("does not call update when nothing changed", async () => {
    renderPage();
    await screen.findByDisplayValue("Alice");

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    expect(mock.updateUser).not.toHaveBeenCalled();
  });

  it("shows an error toast when saving fails", async () => {
    mock.updateUser.mockRejectedValue(new Error("boom"));
    renderPage();
    const title = await screen.findByDisplayValue("Alice");
    fireEvent.change(title, { target: { value: "Alice B" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({ type: "error" })
      );
    });
  });

  it("saves chat preferences when the enter-to-send switch toggles", async () => {
    renderPage();
    await screen.findByDisplayValue("Alice");

    const switchEl = rowSwitch("settings.profile.chat.enter-to-send");
    fireEvent.click(switchEl);

    await waitFor(() => {
      expect(mock.updateUser).toHaveBeenCalledWith(
        "users/1",
        expect.objectContaining({
          chatPreferences: expect.objectContaining({ enterToSend: false }),
        }),
        ["chat_preferences"]
      );
    });
  });

  it("saves chat preferences when the language changes", async () => {
    renderPage();
    await screen.findByDisplayValue("Alice");

    fireEvent.click(screen.getByRole("combobox"));
    const item = await screen.findByText(
      "settings.profile.chat.language.zh-CN"
    );
    fireEvent.pointerDown(item);
    fireEvent.pointerUp(item);
    fireEvent.click(item);

    await waitFor(() => {
      expect(mock.updateUser).toHaveBeenCalledWith(
        "users/1",
        expect.objectContaining({
          chatPreferences: expect.objectContaining({ preferredLanguage: 1 }),
        }),
        ["chat_preferences"]
      );
    });
  });

  it("shows the unsupported notice when web push is unavailable", async () => {
    renderPage();

    expect(
      await screen.findByText("settings.profile.notifications.unsupported")
    ).toBeInTheDocument();
  });

  it("shows the not-configured notice when push config is disabled", async () => {
    mock.webPushSupported.mockReturnValue(true);
    mock.getPushConfig.mockResolvedValue({ enabled: false });
    renderPage();

    expect(
      await screen.findByText("settings.profile.notifications.not-configured")
    ).toBeInTheDocument();
  });

  it("shows the denied notice when permission is denied", async () => {
    mock.webPushSupported.mockReturnValue(true);
    mock.getPushConfig.mockResolvedValue({ enabled: true });
    Object.defineProperty(window, "Notification", {
      value: { permission: "denied" },
      configurable: true,
    });
    renderPage();

    expect(
      await screen.findByText(
        "settings.profile.notifications.permission-denied"
      )
    ).toBeInTheDocument();
  });

  it("renders the ready toggle and enables notifications", async () => {
    mock.webPushSupported.mockReturnValue(true);
    mock.getPushConfig.mockResolvedValue({ enabled: true });
    Object.defineProperty(window, "Notification", {
      value: { permission: "granted" },
      configurable: true,
    });
    mock.isDeviceSubscribed.mockResolvedValue(false);
    renderPage();

    const toggle = await waitFor(() =>
      rowSwitch("settings.profile.notifications.enable")
    );
    expect(toggle).not.toBeChecked();

    fireEvent.click(toggle);
    await waitFor(() => {
      expect(mock.enableDesktopNotifications).toHaveBeenCalled();
    });
  });

  it("disables notifications when the toggle is turned off", async () => {
    mock.webPushSupported.mockReturnValue(true);
    mock.getPushConfig.mockResolvedValue({ enabled: true });
    Object.defineProperty(window, "Notification", {
      value: { permission: "granted" },
      configurable: true,
    });
    mock.isDeviceSubscribed.mockResolvedValue(true);
    renderPage();

    const toggle = await waitFor(() =>
      rowSwitch("settings.profile.notifications.enable")
    );
    expect(toggle).toBeChecked();

    fireEvent.click(toggle);
    await waitFor(() => {
      expect(mock.disableDesktopNotifications).toHaveBeenCalled();
    });
  });

  it("shows the avatar remove button only when an avatar exists", async () => {
    useAppStore.setState({
      currentUser: user({ avatar: "users/1/avatar" }),
    } as never);
    renderPage();

    expect(
      await screen.findByText("settings.profile.avatar-remove")
    ).toBeInTheDocument();
  });
});
