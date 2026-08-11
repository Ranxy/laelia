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

import { act } from "react";
import { createRoot } from "react-dom/client";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { MessageRow } from "@/components/chat/message-row";
import type { ChatMessageUI } from "@/stores/types";

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
  it("renders user content as pre-wrapped text", () => {
    const onViewDetails = (_id: string, _agentId: string) => {};
    act(() => {
      root!.render(
        <MessageRow
          msg={baseMsg({ role: "user", content: "hi there" })}
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
    expect(container?.textContent).toContain("hi there");
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
