import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Stub react-i18next and markstream-react so MessageRow renders in isolation.
let translationHookCalls = 0;
vi.mock("react-i18next", () => ({
  useTranslation: () => {
    translationHookCalls += 1;
    return { t: (k: string) => k };
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
  it("renders the first character uppercase", () => {
    act(() => {
      root!.render(<Avatar label="agent" />);
    });
    expect(container?.textContent).toBe("A");
  });

  it("renders U for the current user", () => {
    act(() => {
      root!.render(<Avatar label="U" />);
    });
    expect(container?.textContent).toBe("U");
  });
});

describe("formatTime", () => {
  it("formats HH:MM with zero padding", () => {
    const d = new Date(2026, 0, 1, 9, 5);
    expect(formatTime(d)).toBe("09:05");
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
