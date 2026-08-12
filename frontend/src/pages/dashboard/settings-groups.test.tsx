import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { State } from "@/types/proto-es/v1/common_pb";
import {
  type Group,
  GroupMemberRole,
  type GroupReference,
} from "@/types/proto-es/v1/group_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { SettingsGroupsPage } from "./settings-groups";

const mock = vi.hoisted(() => ({
  listGroups: vi.fn(),
  listUsers: vi.fn(),
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
  deleteGroup: vi.fn(),
  getGroupReferences: vi.fn(),
}));

vi.mock("@/connect", () => ({
  groupServiceClient: {
    listGroups: mock.listGroups,
    createGroup: mock.createGroup,
    updateGroup: mock.updateGroup,
    deleteGroup: mock.deleteGroup,
    getGroupReferences: mock.getGroupReferences,
  },
  userServiceClient: {
    listUsers: mock.listUsers,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/lib/connect-errors", () => ({
  describeError: (err: unknown) =>
    err instanceof Error ? err.message : String(err),
}));

function group(overrides?: Partial<Group>): Group {
  return {
    name: "groups/eng",
    email: "eng@example.com",
    title: "Engineering",
    description: "Eng team",
    members: [
      { member: "users/1", role: GroupMemberRole.OWNER },
      { member: "users/2", role: GroupMemberRole.MEMBER },
    ],
    source: false,
    canManage: true,
    ...overrides,
  } as unknown as Group;
}

function user(overrides?: Partial<User>): User {
  return {
    name: "users/1",
    title: "Alice",
    email: "alice@example.com",
    state: State.ACTIVE,
    ...overrides,
  } as unknown as User;
}

function renderPage() {
  return render(<SettingsGroupsPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.groups.list",
        "laelia.groups.create",
        "laelia.groups.update",
        "laelia.groups.delete",
      ],
    } as never,
  });
  mock.listGroups.mockReset();
  mock.listUsers.mockReset();
  mock.createGroup.mockReset();
  mock.updateGroup.mockReset();
  mock.deleteGroup.mockReset();
  mock.getGroupReferences.mockReset();
  mock.listGroups.mockResolvedValue({ groups: [] });
  mock.listUsers.mockResolvedValue({ users: [] });
  mock.createGroup.mockResolvedValue({});
  mock.updateGroup.mockResolvedValue({});
  mock.deleteGroup.mockResolvedValue({});
  mock.getGroupReferences.mockResolvedValue({ references: [] });
  toastMock.add.mockReset();
});

describe("settings-groups", () => {
  it("shows the permission notice without the groups.list permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.groups.not-allowed")
    ).toBeInTheDocument();
    expect(mock.listGroups).not.toHaveBeenCalled();
  });

  it("renders the group table", async () => {
    mock.listGroups.mockResolvedValue({ groups: [group()] });
    mock.listUsers.mockResolvedValue({ users: [user()] });

    renderPage();

    expect(await screen.findByText("Engineering")).toBeInTheDocument();
    expect(screen.getByText("eng@example.com")).toBeInTheDocument();
    expect(
      screen.getByText("settings.groups.source-manual")
    ).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    renderPage();

    expect(
      await screen.findByText("settings.groups.no-groups")
    ).toBeInTheDocument();
  });

  it("creates a group with an owner member", async () => {
    mock.listUsers.mockResolvedValue({ users: [user()] });
    renderPage();

    fireEvent.click(await screen.findByText("settings.groups.create"));

    const title = await screen.findByPlaceholderText(
      "settings.groups.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "Design" } });

    // Add the only available user as a member.
    fireEvent.click(screen.getByText("settings.groups.member-add"));

    // The member defaults to MEMBER; switch the role to OWNER.
    const roleSelect = screen.getAllByRole("combobox")[1];
    fireEvent.click(roleSelect);
    const ownerItem = await screen.findByText(
      "settings.groups.member-role-owner"
    );
    fireEvent.pointerDown(ownerItem);
    fireEvent.pointerUp(ownerItem);
    fireEvent.click(ownerItem);

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.createGroup).toHaveBeenCalledWith(
        expect.objectContaining({
          group: expect.objectContaining({
            title: "Design",
            members: [{ member: "users/1", role: GroupMemberRole.OWNER }],
          }),
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("requires a title when creating", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("settings.groups.create"));

    fireEvent.click(await screen.findByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "settings.groups.title-required",
        })
      );
    });
    expect(mock.createGroup).not.toHaveBeenCalled();
  });

  it("requires at least one owner when creating", async () => {
    mock.listUsers.mockResolvedValue({ users: [user()] });
    renderPage();
    fireEvent.click(await screen.findByText("settings.groups.create"));

    const title = await screen.findByPlaceholderText(
      "settings.groups.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "Design" } });

    fireEvent.click(screen.getByText("settings.groups.member-add"));
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "settings.groups.at-least-one-owner",
        })
      );
    });
    expect(mock.createGroup).not.toHaveBeenCalled();
  });

  it("edits a group", async () => {
    mock.listGroups.mockResolvedValue({ groups: [group()] });
    mock.listUsers.mockResolvedValue({ users: [user()] });
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.edit"));

    const title = await screen.findByDisplayValue("Engineering");
    fireEvent.change(title, { target: { value: "Engineering Team" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateGroup).toHaveBeenCalledWith(
        expect.objectContaining({
          group: expect.objectContaining({
            name: "groups/eng",
            title: "Engineering Team",
          }),
          updateMask: { paths: ["title", "description"] },
        })
      );
    });
  });

  it("deletes a group after confirmation", async () => {
    mock.listGroups.mockResolvedValue({ groups: [group()] });
    mock.listUsers.mockResolvedValue({ users: [user()] });
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.delete"));

    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => {
      expect(mock.deleteGroup).toHaveBeenCalledWith({ name: "groups/eng" });
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("loads and expands group references", async () => {
    const refs: GroupReference[] = [
      { resourceType: "policy", resource: "policies/1" } as GroupReference,
    ];
    mock.listGroups.mockResolvedValue({ groups: [group()] });
    mock.listUsers.mockResolvedValue({ users: [user()] });
    mock.getGroupReferences.mockResolvedValue({ references: refs });
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", {
        name: "settings.groups.header-references",
      })
    );

    expect(await screen.findByText("policies/1 (policy)")).toBeInTheDocument();
    expect(mock.getGroupReferences).toHaveBeenCalledWith({
      name: "groups/eng",
    });
  });
});
