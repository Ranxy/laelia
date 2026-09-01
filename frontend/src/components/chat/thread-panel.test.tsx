import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

const mockClient = vi.hoisted(() => ({
  updateTaskStatus: vi.fn(),
  assignTask: vi.fn(),
  listChannelMembers: vi.fn(),
  listTasks: vi.fn(),
  listTaskCounts: vi.fn(),
}));

const mockAgentTeamClient = vi.hoisted(() => ({
  listAgentTeams: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: mockClient,
  agentTeamServiceClient: mockAgentTeamClient,
}));

vi.mock("@/components/chat/message-row", () => ({
  // Renders the resolver output for a fixed handle so tests can assert that
  // mentionLabel is forwarded to (and resolves on) each rendered row.
  MessageRow: ({
    mentionLabel,
  }: {
    mentionLabel?: (handle: string) => string | undefined;
  }) => (
    <div data-testid="message-row">{mentionLabel?.("@alice") ?? "@alice"}</div>
  ),
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

vi.mock("@/hooks/use-mention-detect", () => ({
  detectMention: () => null,
}));

// Configurable mention-label resolver so tests can inject one (defaults to
// "always undefined", matching the pre-fix fallback-to-handle rendering).
const mockMentionLabelResolver = vi.hoisted(() => ({
  fn: undefined as ((handle: string) => string | undefined) | undefined,
}));

vi.mock("@/hooks/use-mention-targets", () => ({
  useMentionTargets: () => [],
  useMentionLabelResolver: () =>
    mockMentionLabelResolver.fn ?? (() => undefined),
  targetToMention: (t: unknown) => t,
}));

// Desktop by default; mobile tests opt out with mockUseIsDesktop.mockReturnValue(false).
const mockUseIsDesktop = vi.hoisted(() => vi.fn(() => true));
vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: mockUseIsDesktop,
}));

const mockUpload = vi.hoisted(() => vi.fn());
vi.mock("@/lib/file-upload", () => ({
  MAX_UPLOAD_BYTES: 512 * 1024 * 1024,
  uploadFileToConversation: mockUpload,
}));

import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/ui-models";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema, TaskStatus } from "@/types/proto-es/v1/command_pb";
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

// Base UI Select commits a selection on a real pointer click. jsdom's
// fireEvent.click is a virtual click (detail === 0) that only activates an
// already-highlighted option, so drive the option with pointer events first.
function selectOption(optionText: string) {
  const option = screen.getByText(optionText).closest('[role="option"]');
  if (!option) throw new Error(`option not found: ${optionText}`);
  fireEvent.pointerEnter(option, { pointerType: "mouse" });
  fireEvent.pointerDown(option, { pointerType: "mouse" });
  fireEvent.click(option);
}

describe("ThreadPanel task controls", () => {
  beforeEach(() => {
    useAppStore.getState().reset();
    mockUseIsDesktop.mockReturnValue(true);
    mockClient.updateTaskStatus.mockResolvedValue({});
    mockClient.assignTask.mockResolvedValue({});
    mockClient.listChannelMembers.mockResolvedValue({ members: [] });
    mockClient.listTasks.mockResolvedValue({ tasks: [], nextPageToken: "" });
    mockClient.listTaskCounts.mockResolvedValue({
      todoCount: 0,
      inProgressCount: 0,
      inReviewCount: 0,
      doneCount: 1,
    });
    mockAgentTeamClient.listAgentTeams.mockResolvedValue({ agentTeams: [] });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("shows the status dropdown for an open task thread, before the expand toggle", () => {
    useAppStore.setState({
      threadByRoot: {
        [ROOT_NAME]: {
          messages: [taskRoot(TaskStatus.IN_PROGRESS)],
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
    const status = screen.getByLabelText("channelTask.status-change-aria");
    const expand = screen.getByLabelText("chat.thread-expand");
    expect(status).not.toBeNull();
    expect(
      status.compareDocumentPosition(expand) & Node.DOCUMENT_POSITION_FOLLOWING
    ).not.toBe(0);
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

  it("keeps the status dropdown for a DONE task (any status transition is allowed)", () => {
    renderPanel(taskRoot(TaskStatus.DONE));
    expect(
      screen.getByLabelText("channelTask.status-change-aria")
    ).not.toBeNull();
  });

  it("hides the status dropdown on non-task threads", () => {
    renderPanel(plainRoot());
    expect(
      screen.queryByLabelText("channelTask.status-change-aria")
    ).toBeNull();
  });

  it("hides the status dropdown in readOnly threads", () => {
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS), true);
    expect(
      screen.queryByLabelText("channelTask.status-change-aria")
    ).toBeNull();
  });

  it("changes the task status via the dropdown and calls updateTaskStatus", async () => {
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS));
    const status = screen.getByLabelText("channelTask.status-change-aria");
    fireEvent.click(status);
    await screen.findByText("channelTask.status-done");
    selectOption("channelTask.status-done");
    expect(mockClient.updateTaskStatus).toHaveBeenCalledTimes(1);
    const req = mockClient.updateTaskStatus.mock.calls[0][0];
    expect(req.message).toBe(ROOT_NAME);
    expect(req.status).toBe(TaskStatus.DONE);
  });

  it("shows the assignee dropdown for an open task thread", () => {
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS));
    expect(screen.getByLabelText("channelTask.assignee-aria")).not.toBeNull();
  });

  it("assigns a member via the dropdown and calls assignTask", async () => {
    mockClient.listChannelMembers.mockResolvedValue({
      members: [{ memberType: 1, memberId: "ran-user-1", displayName: "Ran" }],
    });
    renderPanel(taskRoot(TaskStatus.IN_PROGRESS));
    const assignee = screen.getByLabelText("channelTask.assignee-aria");
    fireEvent.click(assignee);
    await screen.findByText("Ran");
    selectOption("Ran");
    expect(mockClient.assignTask).toHaveBeenCalledTimes(1);
    const req = mockClient.assignTask.mock.calls[0][0];
    expect(req.message).toBe(ROOT_NAME);
    expect(req.memberType).toBe(1);
    expect(req.memberId).toBe("ran-user-1");
  });
});

describe("ThreadPanel composer paste", () => {
  beforeEach(() => {
    useAppStore.getState().reset();
    mockUseIsDesktop.mockReturnValue(true);
    mockUpload.mockClear();
    mockClient.listChannelMembers.mockResolvedValue({ members: [] });
    mockClient.listTasks.mockResolvedValue({ tasks: [], nextPageToken: "" });
    mockClient.listTaskCounts.mockResolvedValue({
      todoCount: 0,
      inProgressCount: 0,
      inReviewCount: 0,
      doneCount: 0,
    });
    mockAgentTeamClient.listAgentTeams.mockResolvedValue({ agentTeams: [] });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  // jsdom has no DataTransfer; a paste event only needs the items list the
  // composers read (kind/type/getAsFile).
  function paste(clipboardData: unknown): boolean {
    const textarea = screen.getByPlaceholderText("chat.thread-placeholder");
    return fireEvent.paste(textarea, { clipboardData });
  }

  it("uploads a pasted clipboard image like a picked file", async () => {
    const file = new File(["png"], "image.png", { type: "image/png" });
    const att: Attachment = create(AttachmentSchema, {
      id: "f1",
      name: "image.png",
      mimeType: "image/png",
      sizeBytes: 3n,
    });
    mockUpload.mockResolvedValue(att);
    renderPanel(plainRoot());

    const notCancelled = paste({
      items: [
        {
          kind: "string",
          type: "text/plain",
          getAsFile: () => null,
        },
        {
          kind: "file",
          type: "image/png",
          getAsFile: () => file,
        },
      ],
    });

    // preventDefault stops the browser from inserting the image URL as text.
    expect(notCancelled).toBe(false);
    await waitFor(() => expect(mockUpload).toHaveBeenCalledTimes(1));
    const options = mockUpload.mock.calls[0][0];
    expect(options.conversation).toBe("conversations/c1");
    expect(options.originalName).toBe("image.png");
    expect(options.mimeType).toBe("image/png");
    expect(options.file).toBe(file);
  });

  it("keeps text-only paste untouched", () => {
    renderPanel(plainRoot());
    const notCancelled = paste({
      items: [{ kind: "string", type: "text/plain", getAsFile: () => null }],
    });
    expect(notCancelled).toBe(true);
    expect(mockUpload).not.toHaveBeenCalled();
    expect(
      (
        screen.getByPlaceholderText(
          "chat.thread-placeholder"
        ) as HTMLTextAreaElement
      ).value
    ).toBe("");
  });
});

describe("ThreadPanel mention labels", () => {
  beforeEach(() => {
    useAppStore.getState().reset();
    mockUseIsDesktop.mockReturnValue(true);
    mockClient.listChannelMembers.mockResolvedValue({ members: [] });
    mockClient.listTasks.mockResolvedValue({ tasks: [], nextPageToken: "" });
    mockClient.listTaskCounts.mockResolvedValue({
      todoCount: 0,
      inProgressCount: 0,
      inReviewCount: 0,
      doneCount: 0,
    });
    mockAgentTeamClient.listAgentTeams.mockResolvedValue({ agentTeams: [] });
  });

  afterEach(() => {
    mockMentionLabelResolver.fn = undefined;
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("resolves mention labels on the root row and reply rows", () => {
    mockMentionLabelResolver.fn = (handle) =>
      handle === "@alice" ? "Alice" : undefined;
    const reply: ChatMessageUI = {
      id: "conversations/c1/messages/m2",
      role: "user",
      content: "reply mentioning @alice",
      timestamp: new Date(0),
    };
    useAppStore.setState({
      threadByRoot: {
        [ROOT_NAME]: {
          messages: [plainRoot(), reply],
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
      />
    );
    const rows = screen.getAllByTestId("message-row");
    expect(rows).toHaveLength(2);
    for (const row of rows) {
      expect(row).toHaveTextContent("Alice");
      expect(row).not.toHaveTextContent("@alice");
    }
  });
});
