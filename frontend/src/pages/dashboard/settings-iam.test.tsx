import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  type Binding,
  BindingSchema,
  type IamPolicy,
  IamPolicySchema,
} from "@/types/proto-es/store/policy_pb";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import { SettingsIamPage } from "./settings-iam";

const mock = vi.hoisted(() => ({
  getWorkspaceIamPolicy: vi.fn(),
  setWorkspaceIamPolicy: vi.fn(),
  listRoles: vi.fn(),
  listGroups: vi.fn(),
  fetchUsers: vi.fn(),
}));

vi.mock("@/connect", () => ({
  iamServiceClient: {
    getWorkspaceIamPolicy: mock.getWorkspaceIamPolicy,
    setWorkspaceIamPolicy: mock.setWorkspaceIamPolicy,
  },
  roleServiceClient: { listRoles: mock.listRoles },
  groupServiceClient: { listGroups: mock.listGroups },
}));

const tFn = (key: string, params?: Record<string, string | number>) => {
  if (!params) return key;
  const values = Object.values(params);
  return values.length > 0 ? `${key}:${values.join(":")}` : key;
};
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/lib/command-status", () => ({
  roleIDFromName: (name: string) => name.split("/")[1] ?? name,
}));

function policy(bindings: Binding[]): IamPolicy {
  return create(IamPolicySchema, { bindings });
}

function binding(role: string, members: string[]): Binding {
  return create(BindingSchema, { role, members });
}

function role(overrides?: Partial<Role>): Role {
  return {
    name: "roles/admin",
    title: "Admin",
    description: "Full access",
    permissions: [],
    predefined: true,
    ...overrides,
  } as unknown as Role;
}

function renderPage() {
  return render(<SettingsIamPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: ["laelia.iam.getPolicy", "laelia.iam.setPolicy"],
    } as never,
    users: [
      {
        name: "users/1",
        title: "Alice",
        email: "alice@example.com",
      },
    ],
    fetchUsers: mock.fetchUsers,
  } as never);
  mock.getWorkspaceIamPolicy.mockReset();
  mock.setWorkspaceIamPolicy.mockReset();
  mock.listRoles.mockReset();
  mock.listGroups.mockReset();
  mock.fetchUsers.mockReset();
  mock.getWorkspaceIamPolicy.mockResolvedValue({
    policy: policy([]),
    etag: "etag-1",
  });
  mock.setWorkspaceIamPolicy.mockResolvedValue({
    policy: policy([]),
    etag: "etag-2",
  });
  mock.listRoles.mockResolvedValue({ roles: [] });
  mock.listGroups.mockResolvedValue({ groups: [] });
  mock.fetchUsers.mockResolvedValue(undefined);
  toastMock.add.mockReset();
});

describe("settings-iam", () => {
  it("shows the permission notice without the getPolicy permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.iam.not-allowed")
    ).toBeInTheDocument();
    expect(mock.getWorkspaceIamPolicy).not.toHaveBeenCalled();
  });

  it("renders the policy bindings", async () => {
    mock.getWorkspaceIamPolicy.mockResolvedValue({
      policy: policy([
        binding("roles/admin", ["users/1", "allUsers"]),
        binding("roles/custom", ["groups/eng"]),
      ]),
      etag: "etag-1",
    });
    mock.listRoles.mockResolvedValue({
      roles: [
        role(),
        role({ name: "roles/custom", title: "Custom", predefined: false }),
      ],
    });
    mock.listGroups.mockResolvedValue({
      groups: [
        {
          name: "groups/eng",
          email: "eng@example.com",
          title: "Engineering",
          description: "",
          members: [],
        },
      ],
    });

    renderPage();

    expect(await screen.findByText("Admin")).toBeInTheDocument();
    expect(screen.getByText("Custom")).toBeInTheDocument();
    expect(screen.getByText("alice@example.com")).toBeInTheDocument();
    expect(
      screen.getByText("settings.iam.member-all-users")
    ).toBeInTheDocument();
    expect(screen.getByText("Engineering")).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    renderPage();

    expect(await screen.findByText("common.no-data")).toBeInTheDocument();
  });

  it("assigns roles to a member", async () => {
    mock.getWorkspaceIamPolicy.mockResolvedValue({
      policy: policy([]),
      etag: "etag-1",
    });
    mock.listRoles.mockResolvedValue({ roles: [role()] });
    renderPage();

    fireEvent.click(await screen.findByText("settings.iam.assign"));

    // Pick the user from the member picker.
    const search = await screen.findByPlaceholderText(
      "settings.iam.member-picker-search"
    );
    fireEvent.change(search, { target: { value: "alice" } });
    fireEvent.click(await screen.findByText("alice@example.com"));

    // Check the admin role.
    const checkbox = await screen.findByRole("checkbox", { name: /Admin/ });
    fireEvent.click(checkbox);

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.setWorkspaceIamPolicy).toHaveBeenCalledWith(
        expect.objectContaining({
          etag: "etag-1",
          policy: expect.objectContaining({
            bindings: [
              expect.objectContaining({
                role: "roles/admin",
                members: ["users/1"],
              }),
            ],
          }),
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("opens the role sheet and saves member changes", async () => {
    mock.getWorkspaceIamPolicy.mockResolvedValue({
      policy: policy([binding("roles/admin", ["users/1"])]),
      etag: "etag-1",
    });
    mock.listRoles.mockResolvedValue({ roles: [role()] });
    renderPage();

    fireEvent.click(await screen.findByText("Admin"));

    // Remove the existing member.
    fireEvent.click(
      await screen.findByLabelText(
        "settings.iam.role-sheet-remove-member:alice@example.com"
      )
    );

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.setWorkspaceIamPolicy).toHaveBeenCalledWith(
        expect.objectContaining({
          policy: expect.objectContaining({ bindings: [] }),
        })
      );
    });
  });

  it("confirms discarding unsaved role sheet changes", async () => {
    useAppStore.setState({
      users: [
        {
          name: "users/1",
          title: "Alice",
          email: "alice@example.com",
        },
        {
          name: "users/2",
          title: "Bob",
          email: "bob@example.com",
        },
      ],
    } as never);
    mock.getWorkspaceIamPolicy.mockResolvedValue({
      policy: policy([binding("roles/admin", ["users/1"])]),
      etag: "etag-1",
    });
    mock.listRoles.mockResolvedValue({ roles: [role()] });
    renderPage();

    fireEvent.click(await screen.findByText("Admin"));

    // Add a member via the picker.
    const search = await screen.findByPlaceholderText(
      "settings.iam.member-picker-search"
    );
    fireEvent.change(search, { target: { value: "bob" } });
    fireEvent.click(await screen.findByText("bob@example.com"));

    // Try to close with unsaved changes.
    fireEvent.click(screen.getByRole("button", { name: "common.cancel" }));

    expect(
      await screen.findByText("settings.iam.role-sheet-discard-title")
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "common.discard" }));

    await waitFor(() => {
      expect(
        screen.queryByText("settings.iam.role-sheet-discard-title")
      ).not.toBeInTheDocument();
    });
  });
});
