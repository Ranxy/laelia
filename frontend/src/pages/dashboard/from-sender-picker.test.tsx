import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { AgentSummarySchema } from "@/types/proto-es/v1/agent_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { FromSenderPicker } from "./from-sender-picker";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mock = vi.hoisted(() => ({
  listUsers: vi.fn(),
}));

vi.mock("@/connect", () => ({
  userServiceClient: { listUsers: mock.listUsers },
}));

function typeInto(input: HTMLInputElement, value: string) {
  const setter = Object.getOwnPropertyDescriptor(
    HTMLInputElement.prototype,
    "value"
  )?.set;
  setter?.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

const user = create(UserSchema, {
  name: "users/1",
  title: "Alice",
  handle: "alice",
  email: "alice@example.com",
});

const agent = create(AgentSummarySchema, {
  name: "agents/alpha",
  title: "Alpha",
  handle: "alpha",
});

function renderPicker(props?: Partial<Parameters<typeof FromSenderPicker>[0]>) {
  const onChange = vi.fn();
  render(
    <FromSenderPicker
      value={null}
      onChange={onChange}
      placeholder="From"
      {...props}
    />
  );
  return { onChange };
}

function pickerInput(): HTMLInputElement {
  return screen.getByPlaceholderText("From") as HTMLInputElement;
}

beforeEach(() => {
  mock.listUsers.mockReset();
  mock.listUsers.mockResolvedValue({ users: [], nextPageToken: "" });
});

afterEach(() => {
  document.body.innerHTML = "";
  useAppStore.setState({ agents: [], agentsLoading: false });
});

describe("FromSenderPicker", () => {
  it("searches users server-side once the debounce settles", async () => {
    mock.listUsers.mockResolvedValue({ users: [user], nextPageToken: "" });

    renderPicker();
    fireEvent.focus(pickerInput());
    typeInto(pickerInput(), "ali");

    // The human row arrives through the debounced search; the badge labels it.
    const row = await screen.findByRole("option", undefined, {
      timeout: 3000,
    });
    expect(row.textContent).toContain("Alice");
    expect(row.textContent).toContain("members.kind-user");
    expect(mock.listUsers).toHaveBeenCalledWith({
      pageSize: 50,
      filter: 'name.matches("ali") || email.matches("ali")',
    });
  });

  it("never searches while the query is empty", async () => {
    useAppStore.setState({ agents: [agent] });

    renderPicker();
    fireEvent.focus(pickerInput());

    // The dropdown stays gated on typed input (old hand-rolled behavior), and
    // no browse-style listUsers fires.
    await waitFor(() => {
      expect(screen.queryByRole("option")).toBeNull();
    });
    expect(mock.listUsers).not.toHaveBeenCalled();
  });

  it("commits an agent sender from the client-side roster filter", async () => {
    useAppStore.setState({ agents: [agent] });

    const { onChange } = renderPicker();
    fireEvent.focus(pickerInput());
    typeInto(pickerInput(), "alp");

    const row = await screen.findByRole("option", undefined, {
      timeout: 3000,
    });
    expect(row.textContent).toContain("Alpha");
    fireEvent.click(row);

    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    const sender = onChange.mock.calls[0][0];
    expect(sender.kind).toBe("agent");
    expect(sender.agent.name).toBe("agents/alpha");
  });

  it("clears the selection from the reset button", () => {
    const { onChange } = renderPicker({ value: { kind: "user", user } });

    fireEvent.click(screen.getByRole("button", { name: "From" }));

    expect(onChange).toHaveBeenCalledWith(null);
  });
});
