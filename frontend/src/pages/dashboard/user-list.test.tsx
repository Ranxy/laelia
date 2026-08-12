import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { State } from "@/types/proto-es/v1/common_pb";
import { type User, UserType } from "@/types/proto-es/v1/user_service_pb";
import { UserListPage } from "./user-list";

const mock = vi.hoisted(() => ({
  fetchUsers: vi.fn(),
  createUser: vi.fn(),
  updateUser: vi.fn(),
  resetPassword: vi.fn(),
  deleteUser: vi.fn(),
  undeleteUser: vi.fn(),
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

function user(overrides?: Partial<User>): User {
  return {
    name: "users/101",
    title: "Alice",
    email: "alice@example.com",
    phone: "123",
    description: "",
    userType: UserType.USER,
    state: State.ACTIVE,
    profile: { lastLoginTime: { seconds: 0n, nanos: 0 } },
    ...overrides,
  } as unknown as User;
}

function renderPage() {
  return render(<UserListPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.users.create",
        "laelia.users.update",
        "laelia.users.delete",
      ],
    } as never,
    users: [],
    usersLoading: false,
    deletedUsers: [],
    deletedUsersLoading: false,
    fetchUsers: mock.fetchUsers,
    createUser: mock.createUser,
    updateUser: mock.updateUser,
    resetPassword: mock.resetPassword,
    deleteUser: mock.deleteUser,
    undeleteUser: mock.undeleteUser,
  } as never);
  mock.fetchUsers.mockReset();
  mock.createUser.mockReset();
  mock.updateUser.mockReset();
  mock.resetPassword.mockReset();
  mock.deleteUser.mockReset();
  mock.undeleteUser.mockReset();
  mock.fetchUsers.mockResolvedValue(undefined);
  mock.createUser.mockResolvedValue(undefined);
  mock.updateUser.mockResolvedValue(undefined);
  mock.resetPassword.mockResolvedValue(undefined);
  mock.deleteUser.mockResolvedValue(undefined);
  mock.undeleteUser.mockResolvedValue(undefined);
  toastMock.add.mockReset();
});

describe("user-list", () => {
  it("loads the active roster on mount", async () => {
    renderPage();

    await waitFor(() => {
      expect(mock.fetchUsers).toHaveBeenCalledWith(
        expect.objectContaining({ showDeleted: false, includeSystemBot: true }),
        expect.anything()
      );
    });
  });

  it("renders active users", async () => {
    useAppStore.setState({ users: [user()] } as never);
    renderPage();

    expect(await screen.findByText("alice@example.com")).toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it("debounces the search query into a filter", async () => {
    renderPage();
    await waitFor(() => expect(mock.fetchUsers).toHaveBeenCalled());

    const search = screen.getByPlaceholderText("user.search-placeholder");
    fireEvent.change(search, { target: { value: "bob" } });

    await waitFor(
      () => {
        expect(mock.fetchUsers).toHaveBeenLastCalledWith(
          expect.objectContaining({
            showDeleted: false,
            filter: 'name.matches("bob") || email.matches("bob")',
          }),
          expect.anything()
        );
      },
      { timeout: 3000 }
    );
  });

  it("switches to the trash tab and loads deleted users", async () => {
    useAppStore.setState({
      deletedUsers: [
        user({
          name: "users/102",
          title: "Bob",
          email: "bob@example.com",
          state: State.DELETED,
        }),
      ],
    } as never);
    renderPage();

    fireEvent.click(await screen.findByText("user.tab-trash"));

    expect(await screen.findByText("bob@example.com")).toBeInTheDocument();
    await waitFor(() => {
      expect(mock.fetchUsers).toHaveBeenLastCalledWith(
        expect.objectContaining({ showDeleted: true }),
        expect.anything()
      );
    });
  });

  it("creates a user from the create sheet", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("user.create"));

    const email = await screen.findByPlaceholderText(
      "user.field-email-placeholder"
    );
    fireEvent.change(email, { target: { value: "bob@example.com" } });
    const title = screen.getByPlaceholderText("user.field-title-placeholder");
    fireEvent.change(title, { target: { value: "Bob" } });
    const password = screen.getByPlaceholderText(
      "user.field-password-placeholder"
    );
    fireEvent.change(password, { target: { value: "secret" } });

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    await waitFor(() => {
      expect(mock.createUser).toHaveBeenCalledWith({
        email: "bob@example.com",
        title: "Bob",
        phone: "",
        password: "secret",
      });
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("rejects an invalid email when creating", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("user.create"));

    const email = await screen.findByPlaceholderText(
      "user.field-email-placeholder"
    );
    fireEvent.change(email, { target: { value: "not-an-email" } });
    const title = screen.getByPlaceholderText("user.field-title-placeholder");
    fireEvent.change(title, { target: { value: "Bob" } });
    const password = screen.getByPlaceholderText(
      "user.field-password-placeholder"
    );
    fireEvent.change(password, { target: { value: "secret" } });

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    expect(
      await screen.findByText("user.create-email-invalid")
    ).toBeInTheDocument();
    expect(mock.createUser).not.toHaveBeenCalled();
  });

  it("edits a user", async () => {
    useAppStore.setState({ users: [user()] } as never);
    renderPage();

    fireEvent.click(await screen.findByText("user.edit"));

    const title = await screen.findByDisplayValue("Alice");
    fireEvent.change(title, { target: { value: "Alice B" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateUser).toHaveBeenCalledWith(
        "users/101",
        { title: "Alice B" },
        ["title"]
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("resets a user password", async () => {
    useAppStore.setState({ users: [user()] } as never);
    renderPage();

    fireEvent.click(await screen.findByText("user.reset-password"));

    const inputs = await screen.findAllByPlaceholderText(
      "user.field-password-new"
    );
    fireEvent.change(inputs[0], { target: { value: "newpass" } });
    const confirm = screen.getByPlaceholderText("user.field-password-confirm");
    fireEvent.change(confirm, { target: { value: "newpass" } });

    fireEvent.click(
      screen.getByRole("button", { name: "user.reset-password" })
    );

    await waitFor(() => {
      expect(mock.resetPassword).toHaveBeenCalledWith("users/101", "newpass");
    });
  });

  it("rejects a mismatched password reset", async () => {
    useAppStore.setState({ users: [user()] } as never);
    renderPage();

    fireEvent.click(await screen.findByText("user.reset-password"));

    const inputs = await screen.findAllByPlaceholderText(
      "user.field-password-new"
    );
    fireEvent.change(inputs[0], { target: { value: "newpass" } });
    const confirm = screen.getByPlaceholderText("user.field-password-confirm");
    fireEvent.change(confirm, { target: { value: "other" } });

    fireEvent.click(
      screen.getByRole("button", { name: "user.reset-password" })
    );

    expect(
      await screen.findByText("user.reset-password-mismatch")
    ).toBeInTheDocument();
    expect(mock.resetPassword).not.toHaveBeenCalled();
  });

  it("deletes a user after confirmation", async () => {
    useAppStore.setState({ users: [user()] } as never);
    renderPage();

    fireEvent.click(await screen.findByText("common.delete"));

    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => {
      expect(mock.deleteUser).toHaveBeenCalledWith("users/101");
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("restores a deleted user from the trash tab", async () => {
    useAppStore.setState({
      deletedUsers: [
        user({
          name: "users/102",
          title: "Bob",
          email: "bob@example.com",
          state: State.DELETED,
        }),
      ],
    } as never);
    renderPage();

    fireEvent.click(await screen.findByText("user.tab-trash"));
    fireEvent.click(await screen.findByText("user.restore"));

    await waitFor(() => {
      expect(mock.undeleteUser).toHaveBeenCalledWith("users/102");
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });
});
