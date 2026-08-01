import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// MemberPicker uses react-i18next via useTranslation; stub it so the test does
// not depend on the i18n provider. The mock renders the {{count}} interpolation
// so the trigger's selected count is assertable.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, params?: { count?: number }) =>
      params?.count != null ? `${key}:${params.count}` : key,
  }),
}));

import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { MemberPicker } from "./member-picker";

// MemberPicker reads users/agents/fetchUsers from the app store via useAppStore.
// A tiny selector-based stand-in keeps the test focused on the picker itself.
const mockState = vi.hoisted(() => ({
  users: [
    { name: "users/1", title: "Alice", email: "alice@example.com" },
    { name: "users/2", title: "Bob", email: "bob@example.com" },
  ] as unknown as User[],
  agents: [
    { name: "agents/alpha", title: "Alpha" },
    { name: "agents/beta", title: "Beta" },
  ] as unknown as AgentSummary[],
  usersLoading: false,
  fetchUsers: vi.fn(),
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: typeof mockState) => unknown) =>
    selector(mockState),
}));

describe("MemberPicker multi-select", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("toggles members on click and keeps the popover open for multiple picks", () => {
    const onToggle = vi.fn();
    render(
      <MemberPicker
        memberType={1}
        existingMemberIds={new Set()}
        value={[]}
        onToggle={onToggle}
        placeholder="Pick"
      />
    );
    fireEvent.click(screen.getByRole("button", { name: "Pick" }));
    fireEvent.click(screen.getByRole("button", { name: /Alice/ }));
    expect(onToggle).toHaveBeenCalledWith("1");
    // The popover stays open: Bob is still pickable after the first pick.
    fireEvent.click(screen.getByRole("button", { name: /Bob/ }));
    expect(onToggle).toHaveBeenCalledWith("2");
  });

  it("shows the selected count on the trigger", () => {
    render(
      <MemberPicker
        memberType={1}
        existingMemberIds={new Set()}
        value={["1", "2"]}
        onToggle={vi.fn()}
        placeholder="Pick"
      />
    );
    expect(
      screen.getByRole("button", { name: "channel.selected-count:2" })
    ).toBeInTheDocument();
  });

  it("disables rows for members already in the channel", () => {
    const onToggle = vi.fn();
    render(
      <MemberPicker
        memberType={1}
        existingMemberIds={new Set(["2"])}
        value={[]}
        onToggle={onToggle}
        placeholder="Pick"
      />
    );
    fireEvent.click(screen.getByRole("button", { name: "Pick" }));
    const bobRow = screen.getByRole("button", { name: /Bob/ });
    expect(bobRow).toBeDisabled();
    fireEvent.click(bobRow);
    expect(onToggle).not.toHaveBeenCalled();
  });
});
