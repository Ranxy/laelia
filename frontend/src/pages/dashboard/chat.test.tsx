import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MessageRow, rowStreamingProps } from "@/components/chat/message-row";
import type { ChatMessageUI } from "@/stores/types";

// MessageRow pulls react-i18next (no provider in the test environment) and
// markstream-react (full markdown renderer). Stub both so the test isolates
// memo behaviour: react-i18next's useTranslation is the per-render sentinel we
// count, and markstream's MarkdownRender becomes a trivial passthrough.
let translationHookCalls = 0;
vi.mock("react-i18next", () => ({
  useTranslation: () => {
    translationHookCalls += 1;
    return { t: (k: string) => k };
  },
}));

vi.mock("markstream-react", () => ({
  MarkdownRender: ({ content }: { content: string }) => <>{content}</>,
  MarkdownCodeBlockNode: () => null,
  setCustomComponents: () => {},
  default: ({ content }: { content: string }) => <>{content}</>,
}));

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

function baseMsg(overrides: Partial<ChatMessageUI> = {}): ChatMessageUI {
  return {
    id: "m1",
    role: "user",
    content: "hello",
    timestamp: new Date(0),
    ...overrides,
  };
}

// A single stable container reused across renders so re-rendering with the
// same prop references exercises React.memo's shallow compare (memo skips the
// re-render only when every prop is referentially unchanged).
let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  translationHookCalls = 0;
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  if (root) {
    root.unmount();
  }
  container?.remove();
});

describe("MessageRow memo", () => {
  it("skips re-render when props are referentially unchanged", () => {
    const onViewDetails = (commandId: string) => {
      void commandId;
    };
    const msg = baseMsg();
    const props = {
      msg,
      showAvatar: true,
      agentTitle: "Agent",
      streamingContent: "",
      streamingEvents: [] as never[],
      onViewDetails,
      markdownCustomId: "chat",
    };

    act(() => {
      root!.render(<MessageRow {...props} />);
    });
    const initialCalls = translationHookCalls;
    expect(initialCalls).toBeGreaterThan(0);

    // Re-render the parent with the exact same prop references. React.memo
    // must bail out, so useTranslation (called inside MessageRow) is NOT
    // invoked again.
    act(() => {
      root!.render(<MessageRow {...props} />);
    });
    expect(translationHookCalls).toBe(initialCalls);

    // Re-render with a changed prop reference (new msg object). memo can no
    // longer bail out, so MessageRow re-renders and useTranslation fires again.
    act(() => {
      root!.render(
        <MessageRow {...props} msg={baseMsg({ content: "changed" })} />
      );
    });
    expect(translationHookCalls).toBe(initialCalls + 1);
  });
});

describe("rowStreamingProps", () => {
  it("only the streaming row receives the live streaming slices", () => {
    const liveContent = "streaming token";
    const liveEvents = [{ seqNo: 1 }] as never[];
    const ownEvents = [{ seqNo: 9 }] as never[];

    // A row that is actively streaming gets the live slices.
    const streaming = rowStreamingProps(
      baseMsg({ streaming: true, commandName: "c/cmd", events: ownEvents }),
      true,
      liveContent,
      liveEvents
    );
    expect(streaming.streamingContent).toBe(liveContent);
    expect(streaming.streamingEvents).toBe(liveEvents);

    // A finished row never sees the live slices: it gets an empty content and
    // its own stable events so memo skips it while the streaming row updates.
    const finished = rowStreamingProps(
      baseMsg({ events: ownEvents }),
      false,
      liveContent,
      liveEvents
    );
    expect(finished.streamingContent).toBe("");
    expect(finished.streamingEvents).toBe(ownEvents);

    // A row with no events falls back to the stable module-level empty array
    // (not a per-render literal) so Object.is holds across re-renders.
    const noEvents = rowStreamingProps(
      baseMsg(),
      false,
      liveContent,
      liveEvents
    );
    expect(noEvents.streamingContent).toBe("");
    expect(noEvents.streamingEvents).toEqual([]);
  });
});
