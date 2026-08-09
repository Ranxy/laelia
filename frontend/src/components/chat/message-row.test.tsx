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

import { act } from "react";
import { createRoot } from "react-dom/client";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { MessageRow } from "@/components/chat/message-row";
import type { ChatMessageUI } from "@/stores/types";

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  translationHookCalls = 0;
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
