import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => vi.fn(),
}));

vi.mock("@/lib/markdown", () => ({
  MarkdownRenderer: ({ content }: { content: string }) => <>{content}</>,
}));

const mockConnect = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  listChannelMembers: vi.fn(),
  listTasks: vi.fn(),
  listTaskCounts: vi.fn(),
}));

const mockAgentTeamClient = vi.hoisted(() => ({
  listAgentTeams: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    sendMessage: mockConnect.sendMessage,
    listChannelMembers: mockConnect.listChannelMembers,
    listTasks: mockConnect.listTasks,
    listTaskCounts: mockConnect.listTaskCounts,
  },
  agentTeamServiceClient: mockAgentTeamClient,
}));

vi.mock("@/components/chat/message-row", () => ({
  MessageRow: () => <div data-testid="message-row" />,
  rowStreamingProps: () => ({ streamingContent: "", streamingEvents: [] }),
  EMPTY_EVENTS: [],
}));

vi.mock("@/components/chat/mention-popup", () => ({
  MentionPopup: () => <div />,
}));

vi.mock("@/components/chat/remote-image", () => ({
  RemoteImage: () => <div />,
}));

// Desktop by default (the composer chrome is identical in both modes here);
// mobile tests opt out via mockUseIsDesktop.mockReturnValue(false).
const mockUseIsDesktop = vi.hoisted(() => vi.fn(() => true));
vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: mockUseIsDesktop,
}));

const mockUpload = vi.hoisted(() => vi.fn());
vi.mock("@/lib/file-upload", () => ({
  MAX_UPLOAD_BYTES: 512 * 1024 * 1024,
  uploadFileToConversation: mockUpload,
}));

import { create } from "@bufbuild/protobuf";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/ui-models";
import { ChannelMemberSchema } from "@/types/proto-es/v1/command_pb";
import { ThreadPanel } from "./thread-panel";

const ROOT_A = "conversations/c1/messages/m1";
const ROOT_B = "conversations/c1/messages/m2";

function rootMsg(id: string): ChatMessageUI {
  return { id, role: "user", content: "root", timestamp: new Date(0) };
}

function renderThread(rootMessageId: string) {
  return render(
    <ThreadPanel
      channelId="c1"
      channelTitle="C1"
      rootMessageId={rootMessageId}
      onClose={() => {}}
    />
  );
}

function composerTextarea() {
  return screen.getByPlaceholderText(
    "chat.thread-placeholder"
  ) as HTMLTextAreaElement;
}

beforeEach(() => {
  useAppStore.getState().reset();
  mockUseIsDesktop.mockReturnValue(true);
  mockConnect.sendMessage.mockReset();
  mockConnect.sendMessage.mockResolvedValue({ name: "m-new" });
  mockUpload.mockReset();
  mockAgentTeamClient.listAgentTeams.mockResolvedValue({ agentTeams: [] });
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("ThreadPanel composer regressions (shared ChatComposer)", () => {
  it("restores the composer text after a failed send", async () => {
    useAppStore.setState({
      threadByRoot: {
        [ROOT_A]: {
          messages: [rootMsg(ROOT_A)],
          currentVersion: 1n,
          loading: false,
        },
      },
    });
    mockConnect.sendMessage.mockRejectedValueOnce(new Error("network down"));
    renderThread(ROOT_A);

    const textarea = composerTextarea();
    fireEvent.change(textarea, { target: { value: "hello world" } });
    fireEvent.keyDown(textarea, { key: "Enter" });

    // The optimistic row is removed and the half-typed text returns so the
    // user can retry (previously the input stayed cleared on failure).
    await waitFor(() => {
      expect(textarea.value).toBe("hello world");
    });
    const messages =
      useAppStore.getState().threadByRoot[ROOT_A]?.messages ?? [];
    expect(messages).toHaveLength(1);
  });

  it("drops a pending mention when its @token is deleted", async () => {
    useAppStore.setState({
      // The mention roster resolves from the channel members map; a resolved
      // member lets the derived mention map pick up the typed @handle.
      channelMembersByConv: {
        "conversations/c1": [
          create(ChannelMemberSchema, {
            memberType: 1,
            memberId: "u1",
            handle: "ran",
            displayName: "Ran",
          }),
        ],
      },
      threadByRoot: {
        [ROOT_A]: {
          messages: [rootMsg(ROOT_A)],
          currentVersion: 1n,
          loading: false,
        },
      },
    });
    mockConnect.sendMessage.mockResolvedValue({ name: "m-new" });
    renderThread(ROOT_A);

    const textarea = composerTextarea();
    // Type the token with the caret right after it (as a real keystroke
    // would leave it): the popup matches the member, the first Enter selects
    // it, and the inserted "@ran " becomes part of the draft.
    fireEvent.change(textarea, { target: { value: "hi @ran" } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    fireEvent.keyDown(textarea, { key: "Enter" });
    await waitFor(() => {
      expect(mockConnect.sendMessage).toHaveBeenCalledTimes(1);
    });
    // The send carries the typed mention.
    expect(mockConnect.sendMessage.mock.calls[0][0].mentions).toMatchObject([
      { type: "user", id: "u1", name: "ran" },
    ]);

    // Now delete the token (select-all + retype plain prose): the map
    // re-derives from the text, so the stale mention must not be sent again
    // (previously mentionMap kept the residual entry).
    fireEvent.change(textarea, { target: { value: "hi" } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    await waitFor(() => {
      expect(mockConnect.sendMessage).toHaveBeenCalledTimes(2);
    });
    expect(mockConnect.sendMessage.mock.calls[1][0].mentions).toEqual([]);
  });

  it("keeps a thread's in-flight upload out of the next thread's composer", async () => {
    useAppStore.setState({
      threadByRoot: {
        [ROOT_A]: {
          messages: [rootMsg(ROOT_A)],
          currentVersion: 1n,
          loading: false,
        },
        [ROOT_B]: {
          messages: [rootMsg(ROOT_B)],
          currentVersion: 1n,
          loading: false,
        },
      },
    });
    // A's upload never resolves on its own; the test releases it later.
    let resolveUpload: (value: unknown) => void = () => {};
    mockUpload.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveUpload = resolve;
        })
    );

    const { rerender } = renderThread(ROOT_A);
    pasteImage();

    // Switch to thread B (same mounted panel, fresh composer instance keyed
    // by the draft key).
    rerender(
      <ThreadPanel
        channelId="c1"
        channelTitle="C1"
        rootMessageId={ROOT_B}
        onClose={() => {}}
      />
    );

    // A's upload completes while thread B is open: it must not attach to B's
    // composer (previously it leaked into the open conversation).
    resolveUpload({
      id: "f1",
      name: "thread-image.png",
      mimeType: "image/png",
      sizeBytes: 3n,
    });

    // Send from thread B: no attachment from thread A's upload, no chip.
    const textarea = composerTextarea();
    fireEvent.change(textarea, { target: { value: "from B" } });
    fireEvent.keyDown(textarea, { key: "Enter" });
    await waitFor(() => {
      expect(mockConnect.sendMessage).toHaveBeenCalledTimes(1);
    });
    const req = mockConnect.sendMessage.mock.calls[0][0] as {
      content: string;
      attachments: unknown[];
    };
    expect(req.content).toBe("from B");
    expect(req.attachments).toEqual([]);
    expect(screen.queryByText("thread-image.png")).toBeNull();
  });
});

// A pasted clipboard image uploads like a picked file; preventDefault fires
// because real files were found.
function pasteImage() {
  const file = new File(["png"], "thread-image.png", { type: "image/png" });
  const cancelled = fireEvent.paste(composerTextarea(), {
    clipboardData: {
      items: [
        { kind: "string", type: "text/plain", getAsFile: () => null },
        { kind: "file", type: "image/png", getAsFile: () => file },
      ],
    },
  });
  expect(cancelled).toBe(false);
}
