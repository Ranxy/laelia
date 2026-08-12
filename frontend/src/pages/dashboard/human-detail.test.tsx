import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { HumanDetailPage } from "./human-detail";

const mock = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getWorkspaceIamPolicy: vi.fn(),
  listRoles: vi.fn(),
  uploadAvatar: vi.fn(),
  deleteAvatar: vi.fn(),
  fetchUsers: vi.fn(),
  fetchCurrentUser: vi.fn(),
  updateUser: vi.fn(),
  getOrCreateUserUserDM: vi.fn(),
  fetchChannels: vi.fn(),
  avatarChange: vi.fn(),
  avatarRemove: vi.fn(),
}));

vi.mock("@/connect", () => ({
  groupServiceClient: { listGroups: mock.listGroups },
  iamServiceClient: { getWorkspaceIamPolicy: mock.getWorkspaceIamPolicy },
  roleServiceClient: { listRoles: mock.listRoles },
  userServiceClient: {
    uploadAvatar: mock.uploadAvatar,
    deleteAvatar: mock.deleteAvatar,
  },
}));

vi.mock("@/lib/avatar-cache", () => ({
  avatarNameForAgentId: (id: string) => `agents/${id}/avatar`,
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
  resizeImageFile: vi.fn(),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/components/chat/avatar", () => ({
  Avatar: () => <div data-testid="avatar" />,
}));

vi.mock("@/components/connection-badge", () => ({
  ConnectionBadge: () => <span data-testid="conn" />,
}));

const tFn = (key: string, params?: Record<string, string | number>) => {
  if (!params) return key;
  const values = Object.values(params);
  return values.length > 0 ? `${key}:${values.join(":")}` : key;
};
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function user(overrides?: Partial<User>): User {
  return {
    name: "users/1",
    title: "Alice",
    email: "alice@example.com",
    description: "hello",
    avatar: "",
    groups: [],
    ...overrides,
  } as unknown as User;
}

function agent(overrides?: Partial<AgentSummary>): AgentSummary {
  return {
    name: "agents/a1",
    title: "Alpha",
    owner: "users/1",
    provider: "openai",
    ...overrides,
  } as unknown as AgentSummary;
}

function seedStore(overrides?: {
  users?: User[];
  agents?: AgentSummary[];
  currentUser?: Partial<User> | null;
}) {
  useAppStore.setState({
    users: overrides?.users ?? [user()],
    agents: overrides?.agents ?? [agent()],
    currentUser: (overrides?.currentUser ?? {
      name: "users/2",
      title: "Bob",
      permissions: [],
    }) as never,
    fetchUsers: mock.fetchUsers,
    fetchCurrentUser: mock.fetchCurrentUser,
    updateUser: mock.updateUser,
    getOrCreateUserUserDM: mock.getOrCreateUserUserDM,
    fetchChannels: mock.fetchChannels,
  } as never);
}

function renderPage(userId = "1") {
  const router = createMemoryRouter(
    [
      {
        path: "/members",
        element: <Outlet />,
        children: [
          { path: "users/:userId", element: <HumanDetailPage /> },
          { path: "agents/:agentId", element: <div>agent-route</div> },
        ],
      },
      { path: "/:conversationId", element: <div>dm-route</div> },
    ],
    { initialEntries: [`/members/users/${userId}`] }
  );
  return render(<RouterProvider router={router} />);
}

beforeEach(() => {
  seedStore();
  mock.listGroups.mockReset();
  mock.getWorkspaceIamPolicy.mockReset();
  mock.listRoles.mockReset();
  mock.uploadAvatar.mockReset();
  mock.deleteAvatar.mockReset();
  mock.fetchUsers.mockReset();
  mock.fetchCurrentUser.mockReset();
  mock.updateUser.mockReset();
  mock.getOrCreateUserUserDM.mockReset();
  mock.fetchChannels.mockReset();
  mock.avatarChange.mockReset();
  mock.avatarRemove.mockReset();
  mock.listGroups.mockResolvedValue({ groups: [] });
  mock.getWorkspaceIamPolicy.mockResolvedValue({ policy: { bindings: [] } });
  mock.listRoles.mockResolvedValue({ roles: [] });
  mock.fetchUsers.mockResolvedValue(undefined);
  mock.fetchCurrentUser.mockResolvedValue(undefined);
  mock.updateUser.mockResolvedValue(undefined);
  mock.getOrCreateUserUserDM.mockResolvedValue("conversations/c1");
  mock.fetchChannels.mockResolvedValue(undefined);
  toastMock.add.mockReset();
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("HumanDetailPage", () => {
  it("shows the loading state and fetches the roster on a deep link", async () => {
    seedStore({ users: [] });
    renderPage();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
    await waitFor(() => {
      expect(mock.fetchUsers).toHaveBeenCalledWith({ pageSize: 100 });
    });
  });

  it("renders the member's identity, description, groups and owned agents", async () => {
    seedStore({
      users: [
        user({
          groups: ["groups/design", "groups/eng"],
          description: "Design lead",
        }),
      ],
    });
    mock.listGroups.mockResolvedValue({
      groups: [
        { name: "groups/design", title: "Designers" },
        { name: "groups/eng", title: "Engineers" },
      ],
    });
    renderPage();

    expect(await screen.findByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("alice@example.com")).toBeInTheDocument();
    expect(screen.getByText("Design lead")).toBeInTheDocument();
    expect(screen.getByText("Designers")).toBeInTheDocument();
    expect(screen.getByText("Engineers")).toBeInTheDocument();
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("openai")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("shows the no-description placeholder when the member has none", async () => {
    seedStore({ users: [user({ description: "" })] });
    renderPage();

    expect(
      await screen.findByText("members.human.no-description")
    ).toBeInTheDocument();
  });

  it("saves an edited description with the description update mask", async () => {
    seedStore({
      currentUser: {
        name: "users/2",
        title: "Bob",
        permissions: ["laelia.users.update"],
      },
    });
    renderPage();

    fireEvent.click(
      await screen.findByLabelText("members.human.edit-description")
    );
    const textarea = screen.getByPlaceholderText(
      "user.field-description-placeholder"
    );
    fireEvent.change(textarea, { target: { value: "New bio" } });
    fireEvent.click(screen.getByRole("button", { name: "members.human.save" }));

    await waitFor(() => {
      expect(mock.updateUser).toHaveBeenCalledWith(
        "users/1",
        { description: "New bio" },
        ["description"]
      );
    });
    expect(mock.fetchUsers).toHaveBeenCalledWith(
      { pageSize: 100 },
      { silent: true }
    );
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
    expect(
      screen.queryByPlaceholderText("user.field-description-placeholder")
    ).not.toBeInTheDocument();
  });

  it("shows an error toast when saving the description fails", async () => {
    seedStore({
      currentUser: {
        name: "users/2",
        title: "Bob",
        permissions: ["laelia.users.update"],
      },
    });
    mock.updateUser.mockRejectedValue(new Error("boom"));
    renderPage();

    fireEvent.click(
      await screen.findByLabelText("members.human.edit-description")
    );
    fireEvent.change(
      screen.getByPlaceholderText("user.field-description-placeholder"),
      { target: { value: "New bio" } }
    );
    fireEvent.click(screen.getByRole("button", { name: "members.human.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          description: "boom",
        })
      );
    });
  });

  it("cancels the description edit without saving", async () => {
    seedStore({
      currentUser: {
        name: "users/2",
        title: "Bob",
        permissions: ["laelia.users.update"],
      },
    });
    renderPage();

    fireEvent.click(
      await screen.findByLabelText("members.human.edit-description")
    );
    fireEvent.change(
      screen.getByPlaceholderText("user.field-description-placeholder"),
      { target: { value: "New bio" } }
    );
    fireEvent.click(
      screen.getByRole("button", { name: "members.human.cancel" })
    );

    expect(mock.updateUser).not.toHaveBeenCalled();
    expect(
      screen.queryByPlaceholderText("user.field-description-placeholder")
    ).not.toBeInTheDocument();
  });

  it("starts a 1:1 DM and navigates to the conversation", async () => {
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "members.message-human" })
    );

    await waitFor(() => {
      expect(mock.getOrCreateUserUserDM).toHaveBeenCalledWith("users/1");
    });
    expect(mock.fetchChannels).toHaveBeenCalled();
    expect(await screen.findByText("dm-route")).toBeInTheDocument();
  });

  it("renders direct and group role bindings from the IAM policy", async () => {
    seedStore({
      users: [user({ groups: ["groups/design"] })],
      currentUser: {
        name: "users/2",
        title: "Bob",
        permissions: ["laelia.iam.getPolicy"],
      },
    });
    mock.getWorkspaceIamPolicy.mockResolvedValue({
      policy: {
        bindings: [
          { role: "roles/admin", members: ["users/1"] },
          { role: "roles/designer", members: ["groups/design"] },
        ],
      },
    });
    mock.listRoles.mockResolvedValue({
      roles: [
        { name: "roles/admin", title: "Admin" },
        { name: "roles/designer", title: "Designer" },
      ],
    });
    mock.listGroups.mockResolvedValue({
      groups: [{ name: "groups/design", title: "Designers" }],
    });
    renderPage();

    expect(await screen.findByText("Admin")).toBeInTheDocument();
    expect(
      screen.getByText("members.human.role-source-direct")
    ).toBeInTheDocument();
    expect(screen.getByText("Designer")).toBeInTheDocument();
    expect(
      screen.getByText("members.human.role-source-group:Designers")
    ).toBeInTheDocument();
  });

  it("hides the role row when the caller cannot read the policy", async () => {
    seedStore({
      currentUser: { name: "users/2", title: "Bob", permissions: [] },
    });
    renderPage();

    await screen.findByText("Alice");
    expect(screen.queryByText("members.human.role")).not.toBeInTheDocument();
    expect(mock.getWorkspaceIamPolicy).not.toHaveBeenCalled();
  });

  it("shows a dash when the member has no role bindings", async () => {
    seedStore({
      currentUser: {
        name: "users/2",
        title: "Bob",
        permissions: ["laelia.iam.getPolicy"],
      },
    });
    renderPage();

    await waitFor(() => {
      expect(screen.getAllByText("—").length).toBeGreaterThan(0);
    });
  });

  it("marks the self view with a you badge and avatar controls", async () => {
    seedStore({
      currentUser: {
        name: "users/1",
        title: "Alice",
        permissions: [],
        avatar: "users/1/avatar",
      },
      users: [user({ avatar: "users/1/avatar" })],
    });
    renderPage();

    expect(await screen.findByText("members.human.you")).toBeInTheDocument();
    expect(
      screen.getByLabelText("members.human.avatar-upload")
    ).toBeInTheDocument();
    expect(
      screen.getByLabelText("members.human.avatar-remove")
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "members.message-human" })
    ).not.toBeInTheDocument();
    expect(screen.queryByText("members.send-message")).not.toBeInTheDocument();
  });

  it("triggers the avatar upload flow from the file input", async () => {
    seedStore({
      currentUser: { name: "users/1", title: "Alice", permissions: [] },
    });
    renderPage();

    const input = (await screen
      .findByLabelText("members.human.avatar-upload")
      .then(() =>
        document.querySelector('input[type="file"]')
      )) as HTMLInputElement;
    const file = new File(["x"], "a.png", { type: "image/png" });
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => {
      expect(mock.avatarChange).toHaveBeenCalledWith(file);
    });
  });

  it("removes the avatar from the self view", async () => {
    seedStore({
      currentUser: {
        name: "users/1",
        title: "Alice",
        permissions: [],
        avatar: "users/1/avatar",
      },
      users: [user({ avatar: "users/1/avatar" })],
    });
    renderPage();

    fireEvent.click(
      await screen.findByLabelText("members.human.avatar-remove")
    );

    await waitFor(() => {
      expect(mock.avatarRemove).toHaveBeenCalled();
    });
  });

  it("navigates to an owned agent's profile", async () => {
    renderPage();

    fireEvent.click(await screen.findByText("Alpha"));

    expect(await screen.findByText("agent-route")).toBeInTheDocument();
  });

  it("shows the mobile send-message FAB for other members", async () => {
    renderPage();

    expect(await screen.findByText("members.send-message")).toBeInTheDocument();
  });
});
