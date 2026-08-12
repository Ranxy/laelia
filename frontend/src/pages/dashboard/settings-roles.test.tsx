import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import { SettingsRolesPage } from "./settings-roles";

const mock = vi.hoisted(() => ({
  listRoles: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
  deleteRole: vi.fn(),
}));

vi.mock("@/connect", () => ({
  roleServiceClient: {
    listRoles: mock.listRoles,
    createRole: mock.createRole,
    updateRole: mock.updateRole,
    deleteRole: mock.deleteRole,
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

function role(overrides?: Partial<Role>): Role {
  return {
    name: "roles/admin",
    title: "Admin",
    description: "Full access",
    permissions: ["laelia.roles.list", "laelia.roles.create"],
    predefined: true,
    ...overrides,
  } as unknown as Role;
}

function renderPage() {
  return render(<SettingsRolesPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.roles.list",
        "laelia.roles.create",
        "laelia.roles.update",
        "laelia.roles.delete",
      ],
    } as never,
  });
  mock.listRoles.mockReset();
  mock.createRole.mockReset();
  mock.updateRole.mockReset();
  mock.deleteRole.mockReset();
  mock.listRoles.mockResolvedValue({ roles: [] });
  mock.createRole.mockResolvedValue({});
  mock.updateRole.mockResolvedValue({});
  mock.deleteRole.mockResolvedValue({});
  toastMock.add.mockReset();
});

describe("settings-roles", () => {
  it("shows the permission notice without the roles.list permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.roles.not-allowed")
    ).toBeInTheDocument();
    expect(mock.listRoles).not.toHaveBeenCalled();
  });

  it("renders the role table", async () => {
    mock.listRoles.mockResolvedValue({
      roles: [
        role(),
        role({
          name: "roles/custom",
          title: "Custom",
          description: "",
          permissions: ["laelia.agents.list"],
          predefined: false,
        }),
      ],
    });

    renderPage();

    expect(await screen.findByText("Admin")).toBeInTheDocument();
    expect(screen.getByText("Custom")).toBeInTheDocument();
    expect(
      screen.getByText("settings.roles.type-predefined")
    ).toBeInTheDocument();
    expect(screen.getByText("settings.roles.type-custom")).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    renderPage();

    expect(await screen.findByText("common.no-data")).toBeInTheDocument();
  });

  it("shows an error toast when loading fails", async () => {
    mock.listRoles.mockRejectedValue(new Error("boom"));
    renderPage();

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({ type: "error" })
      );
    });
  });

  it("opens the view sheet when a row is clicked", async () => {
    mock.listRoles.mockResolvedValue({ roles: [role()] });
    renderPage();

    fireEvent.click(await screen.findByText("Admin"));

    // The sheet title duplicates the row title; the mono id is sheet-only.
    expect(await screen.findByText("admin")).toBeInTheDocument();
    expect(screen.getAllByText("Admin").length).toBeGreaterThanOrEqual(2);
  });

  it("creates a role from the create sheet", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("common.create"));

    const title = await screen.findByPlaceholderText(
      "settings.roles.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "Reviewer" } });
    const id = screen.getByPlaceholderText(
      "settings.roles.field-id-placeholder"
    );
    expect((id as HTMLInputElement).value).toBe("reviewer");

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    await waitFor(() => {
      expect(mock.createRole).toHaveBeenCalledWith(
        expect.objectContaining({
          role: expect.objectContaining({
            name: "roles/reviewer",
            title: "Reviewer",
          }),
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("requires an id and title when creating", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("common.create"));

    const title = await screen.findByPlaceholderText(
      "settings.roles.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "Reviewer" } });
    const id = screen.getByPlaceholderText(
      "settings.roles.field-id-placeholder"
    );
    fireEvent.change(id, { target: { value: "" } });

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    expect(
      await screen.findByText("settings.roles.id-required")
    ).toBeInTheDocument();
    expect(mock.createRole).not.toHaveBeenCalled();
  });

  it("edits a role from the edit sheet", async () => {
    mock.listRoles.mockResolvedValue({
      roles: [
        role({
          name: "roles/custom",
          title: "Custom",
          description: "",
          permissions: ["laelia.agents.list"],
          predefined: false,
        }),
      ],
    });
    renderPage();

    fireEvent.click(await screen.findByText("Custom"));
    fireEvent.click(await screen.findByText("common.edit"));

    const title = await screen.findByDisplayValue("Custom");
    fireEvent.change(title, { target: { value: "Super Custom" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateRole).toHaveBeenCalledWith(
        expect.objectContaining({
          role: expect.objectContaining({
            name: "roles/custom",
            title: "Super Custom",
          }),
          updateMask: { paths: ["title"] },
        })
      );
    });
  });

  it("deletes a role after confirmation", async () => {
    mock.listRoles.mockResolvedValue({
      roles: [
        role({
          name: "roles/custom",
          title: "Custom",
          description: "",
          permissions: [],
          predefined: false,
        }),
      ],
    });
    renderPage();

    fireEvent.click(await screen.findByText("Custom"));
    fireEvent.click(await screen.findByText("common.delete"));

    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => {
      expect(mock.deleteRole).toHaveBeenCalledWith({ name: "roles/custom" });
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });
});
