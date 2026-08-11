import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => vi.fn(),
}));

vi.mock("markstream-react", () => ({
  MarkdownRender: ({ content }: { content: string }) => <>{content}</>,
  setCustomComponents: () => {},
  default: ({ content }: { content: string }) => <>{content}</>,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {},
}));

vi.mock("@/components/chat/message-row", () => ({
  MessageRow: () => <div data-testid="message-row" />,
  rowStreamingProps: () => ({ streamingContent: "", streamingEvents: [] }),
  EMPTY_EVENTS: [],
}));

vi.mock("@/components/chat/states", () => ({
  EmptyState: () => <div />,
  LoadingState: () => <div data-testid="loading" />,
}));

vi.mock("@/components/chat/mention-badge", () => ({
  MentionBadge: () => <div />,
}));

vi.mock("@/components/chat/mention-popup", () => ({
  MentionPopup: () => <div />,
}));

vi.mock("@/components/chat/remote-image", () => ({
  RemoteImage: () => <div />,
}));

vi.mock("@/composables/useMentionDetect", () => ({
  detectMention: () => null,
}));

vi.mock("@/composables/useMentionTargets", () => ({
  useMentionTargets: () => [],
  targetToMention: (t: unknown) => t,
}));

// Desktop by default; mobile tests opt out with mockUseIsDesktop.mockReturnValue(false).
const mockUseIsDesktop = vi.hoisted(() => vi.fn(() => true));
vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: mockUseIsDesktop,
}));

import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/types";
import { TaskStatus } from "@/types/proto-es/v1/command_pb";
import { ThreadPanel } from "./thread-panel";

const ROOT_NAME = "conversations/c1/messages/m1";

function taskRoot(status: number): ChatMessageUI {
  return {
    id: ROOT_NAME,
    role: "user",
    content: "root",
    timestamp: new Date(0),
    task: { taskNumber: 7, status },
  };
}

function plainRoot(): ChatMessageUI {
  return {
    id: ROOT_NAME,
    role: "user",
    content: "root",
    timestamp: new Date(0),
  };
}

function renderPanel(rootMsg: ChatMessageUI, readOnly?: boolean) {
  useAppStore.setState({
    threadByRoot: {
      [ROOT_NAME]: { messages: [rootMsg], currentVersion: 1n, loading: false },
    },
  });
  return render(
    <ThreadPanel
      channelId="c1"
      channelTitle="C1"
      rootMessageId={ROOT_NAME}
      onClose={() => {}}
      readOnly={readOnly}
    />
  );
}

describe("ThreadPanel close-task button", () => {
  beforeEach(() => {
    useAppStore.getState().reset();
    mockUseIsDesktop.mockReturnValue(true);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("shows the close button for an open task thread, to the left of the expand toggle", () => {
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS));
    // Re-render with the expand toggle present (channel-page layout) and
    // assert DOM order: close task, then expand.
    const close = screen.getByLabelText("channelTask.close");
    expect(close).not.toBeNull();
    expect(close.getAttribute("title")).toBe("channelTask.close");
  });

  it("hides the expand/collapse toggle on mobile", () => {
    mockUseIsDesktop.mockReturnValue(false);
    useAppStore.setState({
      threadByRoot: {
        [ROOT_NAME]: {
          messages: [taskRoot(TaskStatus.TODO)],
          currentVersion: 1n,
          loading: false,
        },
      },
    });
    render(
      <ThreadPanel
        channelId="c1"
        channelTitle="C1"
        rootMessageId={ROOT_NAME}
        onClose={() => {}}
        onToggleExpand={() => {}}
      />
    );
    expect(screen.queryByLabelText("chat.thread-expand")).toBeNull();
    expect(screen.queryByLabelText("chat.thread-collapse")).toBeNull();
  });

  it("places the close button before the expand toggle in the header", () => {
    useAppStore.setState({
      threadByRoot: {
        [ROOT_NAME]: {
          messages: [taskRoot(TaskStatus.TODO)],
          currentVersion: 1n,
          loading: false,
        },
      },
    });
    render(
      <ThreadPanel
        channelId="c1"
        channelTitle="C1"
        rootMessageId={ROOT_NAME}
        onClose={() => {}}
        onToggleExpand={() => {}}
      />
    );
    const close = screen.getByLabelText("channelTask.close");
    const expand = screen.getByLabelText("chat.thread-expand");
    expect(
      close.compareDocumentPosition(expand) & Node.DOCUMENT_POSITION_FOLLOWING
    ).not.toBe(0);
  });

  it("hides the close button once the task is DONE", () => {
    renderPanel(taskRoot(TaskStatus.DONE));
    expect(screen.queryByLabelText("channelTask.close")).toBeNull();
  });

  it("hides the close button on non-task threads", () => {
    renderPanel(plainRoot());
    expect(screen.queryByLabelText("channelTask.close")).toBeNull();
  });

  it("hides the close button in readOnly threads", () => {
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS), true);
    expect(screen.queryByLabelText("channelTask.close")).toBeNull();
  });

  it("confirms before closing and calls closeTask with the conversation + root ids", () => {
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS));
    const closeTask = vi
      .spyOn(useAppStore.getState(), "closeTask")
      .mockResolvedValue(undefined);

    fireEvent.click(screen.getByLabelText("channelTask.close"));
    expect(screen.getByText("channelTask.close-confirm-title")).not.toBeNull();

    fireEvent.click(screen.getByText("channelTask.close-confirm-action"));
    expect(closeTask).toHaveBeenCalledWith("c1", ROOT_NAME);
  });
});
