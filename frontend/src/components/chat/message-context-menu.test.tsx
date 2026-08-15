import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessageUI } from "@/stores/types";
import { MessageContextMenu } from "./message-context-menu";

// MessageContextMenu uses react-i18next, the context-menu UI (base-ui), and the
// useHasPermission permission hook (which reads the app store). Stub i18n so
// labels read as keys, and stub the permission hook so tests control whether
// the "Convert to task" entry appears.
const mockHasPermission = vi.hoisted(() => vi.fn(() => true));
const mockUseIsDesktop = vi.hoisted(() => vi.fn(() => true));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/stores/permissions", () => ({
  useHasPermission: mockHasPermission,
}));

vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: mockUseIsDesktop,
}));

import { act, fireEvent } from "@testing-library/react";

function baseMsg(overrides: Partial<ChatMessageUI> = {}): ChatMessageUI {
  return {
    id: "conversations/c1/messages/m1",
    role: "user",
    content: "hello **world**",
    timestamp: new Date(0),
    ...overrides,
  };
}

function renderMenu(
  msg: ChatMessageUI,
  props: Partial<{
    onCopy: (content: string) => void;
    onOpenThread: (msg: ChatMessageUI) => void;
    onConvertToTask: (msg: ChatMessageUI) => void;
    canOpenThread: boolean;
    canConvertToTask: boolean;
  }> = {}
) {
  return render(
    <MessageContextMenu
      msg={msg}
      content={msg.content}
      onCopy={props.onCopy ?? (() => {})}
      onOpenThread={props.onOpenThread}
      onConvertToTask={props.onConvertToTask}
      canOpenThread={props.canOpenThread}
      canConvertToTask={props.canConvertToTask}
    >
      <div data-testid="row">row content</div>
    </MessageContextMenu>
  );
}

// Right-click (contextmenu event) opens the base-ui menu.
function openMenu() {
  act(() => {
    fireEvent.contextMenu(screen.getByTestId("row"));
  });
}

afterEach(() => {
  vi.clearAllMocks();
  mockUseIsDesktop.mockReturnValue(true);
  mockHasPermission.mockReturnValue(true);
});

describe("MessageContextMenu", () => {
  it("is inert on mobile (renders children bare, no right-click)", () => {
    mockUseIsDesktop.mockReturnValue(false);
    const onCopy = vi.fn();
    renderMenu(baseMsg(), { onCopy });
    expect(screen.getByTestId("row")).toBeInTheDocument();
    // No context menu items are rendered until the menu opens on desktop.
    expect(screen.queryByText("chat.copy-markdown")).toBeNull();
  });

  it("always shows Copy Markdown", () => {
    renderMenu(baseMsg());
    openMenu();
    expect(screen.getByText("chat.copy-markdown")).toBeInTheDocument();
  });

  it("invokes onCopy with the message's final content", () => {
    const onCopy = vi.fn();
    renderMenu(baseMsg({ content: "final **markdown**" }), { onCopy });
    openMenu();
    act(() => {
      fireEvent.click(screen.getByText("chat.copy-markdown"));
    });
    expect(onCopy).toHaveBeenCalledWith("final **markdown**");
  });

  it("shows Open thread for a root message", () => {
    const onOpenThread = vi.fn();
    renderMenu(baseMsg(), { onOpenThread });
    openMenu();
    expect(screen.getByText("chat.open-thread")).toBeInTheDocument();
  });

  it("hides Open thread for a thread reply (threadRoot set)", () => {
    const onOpenThread = vi.fn();
    renderMenu(baseMsg({ threadRoot: "conversations/c1/messages/m0" }), {
      onOpenThread,
    });
    openMenu();
    expect(screen.queryByText("chat.open-thread")).toBeNull();
  });

  it("hides Open thread when canOpenThread is false", () => {
    const onOpenThread = vi.fn();
    renderMenu(baseMsg(), { onOpenThread, canOpenThread: false });
    openMenu();
    expect(screen.queryByText("chat.open-thread")).toBeNull();
  });

  it("shows Convert to task for a root, non-task message", () => {
    const onConvertToTask = vi.fn();
    renderMenu(baseMsg(), { onConvertToTask });
    openMenu();
    expect(screen.getByText("chat.convert-to-task")).toBeInTheDocument();
  });

  it("hides Convert to task for an already-task message", () => {
    const onConvertToTask = vi.fn();
    renderMenu(baseMsg({ task: { taskNumber: 3, status: 1 } }), {
      onConvertToTask,
    });
    openMenu();
    expect(screen.queryByText("chat.convert-to-task")).toBeNull();
  });

  it("hides Convert to task for a thread reply", () => {
    const onConvertToTask = vi.fn();
    renderMenu(baseMsg({ threadRoot: "conversations/c1/messages/m0" }), {
      onConvertToTask,
    });
    openMenu();
    expect(screen.queryByText("chat.convert-to-task")).toBeNull();
  });

  it("hides Convert to task when the caller lacks laelia.conversations.send", () => {
    mockHasPermission.mockReturnValue(false);
    const onConvertToTask = vi.fn();
    renderMenu(baseMsg(), { onConvertToTask });
    openMenu();
    expect(screen.queryByText("chat.convert-to-task")).toBeNull();
  });

  it("invokes onOpenThread with the message", () => {
    const onOpenThread = vi.fn();
    renderMenu(baseMsg(), { onOpenThread });
    openMenu();
    act(() => {
      fireEvent.click(screen.getByText("chat.open-thread"));
    });
    expect(onOpenThread).toHaveBeenCalledTimes(1);
  });

  it("invokes onConvertToTask with the message", () => {
    const onConvertToTask = vi.fn();
    renderMenu(baseMsg(), { onConvertToTask });
    openMenu();
    act(() => {
      fireEvent.click(screen.getByText("chat.convert-to-task"));
    });
    expect(onConvertToTask).toHaveBeenCalledTimes(1);
  });
});
