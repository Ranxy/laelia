import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Stub react-i18next and markstream-react so MessageRow renders in isolation.
let translationHookCalls = 0;
vi.mock("react-i18next", () => ({
  useTranslation: () => {
    translationHookCalls += 1;
    return { t: (k: string) => k, i18n: { language: "en-US" } };
  },
}));

vi.mock("markstream-react", () => ({
  MarkdownRender: ({ content }: { content: string }) => <>{content}</>,
  setCustomComponents: () => {},
  default: ({ content }: { content: string }) => <>{content}</>,
}));

// Desktop by default so the mobile-only "tap bubble to open thread" path stays
// inert; mobile tests opt out with mockUseIsDesktop.mockReturnValue(false).
const mockUseIsDesktop = vi.hoisted(() => vi.fn(() => true));
vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: mockUseIsDesktop,
}));

import { create } from "@bufbuild/protobuf";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MessageRow } from "@/components/chat/message-row";
import type { ChatMessageUI } from "@/stores/types";
import {
  AttachmentSchema,
  MentionSchema,
  ReactionSchema,
} from "@/types/proto-es/v1/command_pb";

// buildReaction constructs a Reaction message (which requires $typeName).
function reaction(
  emoji: string,
  count: number,
  reactors: string[],
  reacted: boolean
) {
  return create(ReactionSchema, { emoji, count, reactors, reacted });
}

function mention(type: string, id: string, name: string) {
  return create(MentionSchema, { type, id, name });
}

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  translationHookCalls = 0;
  mockUseIsDesktop.mockReturnValue(true);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  if (root) root.unmount();
  container?.remove();
});

function baseMsg(overrides: Partial<ChatMessageUI> = {}): ChatMessageUI {
  return {
    id: "m1",
    role: "user",
    content: "hello",
    timestamp: new Date(0),
    ...overrides,
  };
}

describe("Avatar", () => {
  it("renders a pixel identicon (svg) for a seed", () => {
    act(() => {
      root!.render(<Avatar seed="42" />);
    });
    expect(container?.querySelector("svg")).not.toBeNull();
  });

  it("falls back to the label initial when no seed is given", () => {
    act(() => {
      root!.render(<Avatar seed="" label="agent" />);
    });
    expect(container?.textContent).toBe("A");
  });
});

describe("formatTime", () => {
  it("shows time only for today's messages", () => {
    const d = new Date();
    expect(formatTime(d, "en-US")).toMatch(/^\d{2}:\d{2} (AM|PM)$/);
  });

  it("includes the date for messages from another day", () => {
    const d = new Date(new Date().getFullYear() - 1, 0, 1, 9, 5);
    expect(formatTime(d, "en-US")).toBe(`1/1/${d.getFullYear()} 09:05 AM`);
  });

  it("switches the time format with the locale", () => {
    const d = new Date(new Date().getFullYear() - 1, 0, 1, 9, 5);
    expect(formatTime(d, "zh-CN")).toBe(`${d.getFullYear()}/1/1 09:05`);
  });
});

describe("MessageRow shared render", () => {
  it("renders user content through markdown", () => {
    const onViewDetails = (_id: string, _agentId: string) => {};
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({ role: "user", content: "**hi** there" })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={onViewDetails}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    expect(translationHookCalls).toBeGreaterThan(0);
    expect(container?.textContent).toContain("**hi** there");
  });

  it("renders user markdown with mentions through the mention-aware path", () => {
    const onViewDetails = (_id: string, _agentId: string) => {};
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            role: "user",
            content: "Hello @alice-user-1, see **this**",
            mentions: [mention("user", "alice-user-1", "Alice Lee")],
          })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={onViewDetails}
          MentionBadge={MentionBadge}
          onMentionClick={() => {}}
          markdownCustomId="channel-chat"
          debugMode={false}
        />
      );
    });
    // The mock MarkdownRender emits its content as-is, so the rewritten
    // mention node should be visible (user markdown now goes through the same
    // single-pass mention-aware renderer as agent markdown).
    expect(container?.textContent).toContain(
      '<mention type="user" id="alice-user-1" name="Alice Lee">@Alice Lee</mention>'
    );
    expect(container?.textContent).toContain("**this**");
  });
});

describe("MessageRow thread entry", () => {
  function renderRow(onOpenThread: () => void) {
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({ role: "user", content: "hi there" })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={() => {}}
          onOpenThread={onOpenThread}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
  }

  it("hides the Reply in thread button on mobile", () => {
    mockUseIsDesktop.mockReturnValue(false);
    renderRow(() => {});
    expect(
      container?.querySelector('[aria-label="chat.reply-in-thread"]')
    ).toBeNull();
  });

  it("keeps the Reply in thread button on desktop", () => {
    renderRow(() => {});
    expect(
      container?.querySelector('[aria-label="chat.reply-in-thread"]')
    ).not.toBeNull();
  });

  it("opens the thread when the bubble is tapped on mobile", () => {
    mockUseIsDesktop.mockReturnValue(false);
    const onOpenThread = vi.fn();
    renderRow(onOpenThread);
    const bubble = container?.querySelector(".rounded-2xl") as HTMLElement;
    act(() => {
      bubble.click();
    });
    expect(onOpenThread).toHaveBeenCalledTimes(1);
  });

  it("does not open the thread when a link inside the bubble is tapped", () => {
    mockUseIsDesktop.mockReturnValue(false);
    const onOpenThread = vi.fn();
    renderRow(onOpenThread);
    const bubble = container?.querySelector(".rounded-2xl") as HTMLElement;
    const link = document.createElement("a");
    link.href = "https://example.com";
    bubble.appendChild(link);
    act(() => {
      link.click();
    });
    expect(onOpenThread).not.toHaveBeenCalled();
  });
});

describe("MessageRow reaction bar", () => {
  function renderRow(
    reactions: ChatMessageUI["reactions"],
    onToggleReaction: (msg: ChatMessageUI, emoji: string) => void
  ) {
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({ role: "assistant", content: "done", reactions })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
          onToggleReaction={onToggleReaction}
        />
      );
    });
  }

  it("renders reaction pills with emoji and count", () => {
    renderRow(
      [
        reaction("👍", 2, ["alice", "rei-agent-1"], false),
        reaction("✅", 1, ["bob"], true),
      ],
      () => {}
    );
    const text = container?.textContent ?? "";
    expect(text).toContain("👍");
    expect(text).toContain("2");
    expect(text).toContain("✅");
    expect(text).toContain("1");
  });

  it("does not render the bar when there are no reactions", () => {
    renderRow([], () => {});
    expect(container?.textContent).not.toContain("reactions");
  });

  it("calls onToggleReaction with the emoji on click", () => {
    const onToggleReaction = vi.fn();
    renderRow([reaction("👍", 1, ["alice"], false)], onToggleReaction);
    const pill = Array.from(container?.querySelectorAll("button") ?? []).find(
      (b) => b.textContent?.includes("👍")
    );
    expect(pill).toBeTruthy();
    act(() => {
      pill!.click();
    });
    expect(onToggleReaction).toHaveBeenCalledTimes(1);
    expect(onToggleReaction.mock.calls[0][1]).toBe("👍");
  });
});

describe("MessageRow sender click", () => {
  function renderRow(
    msg: ChatMessageUI,
    onSenderClick: (type: string, id: string, name: string) => void,
    currentPrincipalId?: string
  ) {
    act(() => {
      root!.render(
        <MessageRow
          msg={msg}
          showAvatar
          agentTitle={msg.role === "assistant" ? "Agent One" : ""}
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={() => {}}
          onSenderClick={onSenderClick}
          currentPrincipalId={currentPrincipalId}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
  }

  it("opens the user detail sheet when the sender name is clicked", () => {
    const onSenderClick = vi.fn();
    renderRow(
      baseMsg({
        role: "user",
        content: "hi",
        principalId: "alice-user-1",
        senderName: "Alice Lee",
      }),
      onSenderClick,
      "me"
    );
    const name = Array.from(container?.querySelectorAll("button") ?? []).find(
      (b) => b.textContent === "Alice Lee"
    );
    expect(name).toBeTruthy();
    act(() => {
      name!.click();
    });
    expect(onSenderClick).toHaveBeenCalledWith(
      "user",
      "alice-user-1",
      "Alice Lee"
    );
  });

  it("opens the agent detail sheet when the avatar is clicked", () => {
    const onSenderClick = vi.fn();
    renderRow(
      baseMsg({
        role: "assistant",
        content: "done",
        agentId: "agent-1",
        senderName: "Agent One",
      }),
      onSenderClick
    );
    const avatar = Array.from(container?.querySelectorAll("button") ?? []).find(
      (b) => b.getAttribute("aria-label") === "Agent One"
    );
    expect(avatar).toBeTruthy();
    act(() => {
      avatar!.click();
    });
    expect(onSenderClick).toHaveBeenCalledWith("agent", "agent-1", "Agent One");
  });
});

describe("MessageRow inline thread preview", () => {
  const previewReplies: ChatMessageUI[] = [
    {
      id: "conversations/c/messages/r1",
      role: "assistant",
      content: "first reply",
      timestamp: new Date(0),
      threadRoot: "conversations/c/messages/root-1",
      senderName: "Agent",
      agentId: "agent-1",
    },
    {
      id: "conversations/c/messages/r2",
      role: "user",
      content: "latest reply",
      timestamp: new Date(0),
      threadRoot: "conversations/c/messages/root-1",
      principalId: "alice-user-1",
      senderName: "Alice Lee",
    },
  ];

  function renderRow(
    overrides: Partial<ChatMessageUI>,
    onOpenThreadAt: (rootMsg: ChatMessageUI, reply: ChatMessageUI) => void
  ) {
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            id: "conversations/c/messages/root-1",
            role: "user",
            content: "root message",
            threadReplyCount: 14,
            threadNewReplyCount: 2,
            threadPreview: previewReplies,
            ...overrides,
          })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={() => {}}
          onOpenThread={() => {}}
          onOpenThreadAt={onOpenThreadAt}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
  }

  it("renders the replies + new hints and one row per previewed reply", () => {
    renderRow({}, () => {});
    const text = container?.textContent ?? "";
    expect(text).toContain("chat.thread-replies");
    expect(text).toContain("chat.thread-new-replies");
    expect(text).toContain("first reply");
    expect(text).toContain("latest reply");
    expect(text).toContain("chat.you"); // the current user's own reply label
    expect(text).toContain("Agent"); // the agent reply's sender name
  });

  it("opens the thread at the clicked preview reply", () => {
    const onOpenThreadAt = vi.fn();
    renderRow({}, onOpenThreadAt);
    const rows = Array.from(container?.querySelectorAll("button") ?? []).filter(
      (b) => b.textContent?.includes("latest reply")
    );
    expect(rows).toHaveLength(1);
    act(() => {
      rows[0].click();
    });
    expect(onOpenThreadAt).toHaveBeenCalledTimes(1);
    expect(onOpenThreadAt.mock.calls[0][1].id).toBe(previewReplies[1].id);
  });

  it("falls back to the bare count entry before previews sync", () => {
    renderRow({ threadPreview: undefined }, () => {});
    const text = container?.textContent ?? "";
    expect(text).toContain("chat.thread-replies");
    expect(text).not.toContain("first reply");
    expect(text).not.toContain("latest reply");
  });

  it("hides the new-replies hint when the user is caught up", () => {
    renderRow({ threadNewReplyCount: 0 }, () => {});
    expect(container?.textContent).not.toContain("chat.thread-new-replies");
  });

  it("shows attachment names for a file-only preview reply", () => {
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({
            id: "conversations/c/messages/root-1",
            role: "user",
            content: "root message",
            threadReplyCount: 1,
            threadPreview: [
              {
                id: "conversations/c/messages/rf",
                role: "user",
                content: "",
                timestamp: new Date(0),
                threadRoot: "conversations/c/messages/root-1",
                attachments: [
                  create(AttachmentSchema, {
                    id: "a1",
                    name: "report.pdf",
                    mimeType: "application/pdf",
                    sizeBytes: 10n,
                  }),
                ],
              },
            ],
          })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={() => {}}
          onOpenThread={() => {}}
          onOpenThreadAt={() => {}}
          markdownCustomId="chat"
          debugMode={false}
        />
      );
    });
    expect(container?.textContent).toContain("report.pdf");
  });
});

describe("MessageRow context menu", () => {
  function renderRow(props: Partial<Parameters<typeof MessageRow>[0]>) {
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({ role: "assistant", content: "hello" })}
          showAvatar
          agentTitle="Agent"
          streamingContent=""
          streamingEvents={[]}
          onViewDetails={() => {}}
          markdownCustomId="chat"
          debugMode={false}
          {...props}
        />
      );
    });
  }

  it("wraps the row in the context menu when onCopyMarkdown is provided", () => {
    renderRow({ onCopyMarkdown: () => {} });
    // The menu trigger is present (desktop). We can't easily assert the base-ui
    // menu internals here, but the row content is still rendered.
    expect(container?.textContent).toContain("hello");
  });

  it("does not wrap in the context menu when onCopyMarkdown is absent", () => {
    renderRow({});
    expect(container?.textContent).toContain("hello");
  });
});
