import { beforeEach, describe, expect, it } from "vitest";
import { useAppStore } from "./index";
import type { ChatMessageUI } from "./types";

// Regressions for the optimistic composer store actions (batch 4): the
// composer's optimistic send pipeline now routes through these slice actions
// instead of inlining useAppStore.setState surgery, so the dedup/same-ref
// invariants are asserted here.

function uiMsg(id: string): ChatMessageUI {
  return { id, role: "user", content: id, timestamp: new Date(0) };
}

beforeEach(() => {
  useAppStore.setState({
    chatMessages: {},
    threadByRoot: {},
  });
});

describe("appendChatMessage", () => {
  it("appends the optimistic row and dedups against an echoed id", () => {
    const s = useAppStore.getState();
    s.appendChatMessage("conversations/c", uiMsg("m1"));

    // The watcher echo carries the same server id — it must not duplicate.
    s.appendChatMessage("conversations/c", uiMsg("m1"));

    const list = useAppStore.getState().chatMessages["conversations/c"];
    expect(list).toHaveLength(1);
  });

  it("creates the conversation list on demand", () => {
    useAppStore.getState().appendChatMessage("conversations/c", uiMsg("m1"));
    expect(useAppStore.getState().chatMessages["conversations/c"]).toHaveLength(
      1
    );
  });
});

describe("patchChatMessage", () => {
  it("patches one row and keeps siblings at their references", () => {
    const before = [uiMsg("m1"), uiMsg("m2")];
    useAppStore.setState({ chatMessages: { "conversations/c": before } });

    useAppStore.getState().patchChatMessage("conversations/c", "m1", {
      uploadProgress: { "pending-f1": 42 },
    });

    const list = useAppStore.getState().chatMessages["conversations/c"];
    expect(list[0].uploadProgress).toEqual({ "pending-f1": 42 });
    expect(list[0]).not.toBe(before[0]);
    expect(list[1]).toBe(before[1]);
  });

  it("is a same-reference no-op when no patched key differs", () => {
    const before = [uiMsg("m1")];
    useAppStore.setState({ chatMessages: { "conversations/c": before } });

    useAppStore.getState().patchChatMessage("conversations/c", "m1", {
      content: "m1", // unchanged
    });

    expect(useAppStore.getState().chatMessages["conversations/c"]).toBe(before);
  });

  it("is a no-op for unknown rows or conversations", () => {
    useAppStore.getState().patchChatMessage("conversations/c", "mX", {
      content: "nope",
    });
    expect(
      useAppStore.getState().chatMessages["conversations/c"]
    ).toBeUndefined();
  });
});

describe("removeChatMessage", () => {
  it("drops the optimistic row on a failed send", () => {
    const before = [uiMsg("m1"), uiMsg("m2")];
    useAppStore.setState({ chatMessages: { "conversations/c": before } });

    useAppStore.getState().removeChatMessage("conversations/c", "m1");

    expect(
      useAppStore.getState().chatMessages["conversations/c"].map((m) => m.id)
    ).toEqual(["m2"]);
  });
});

describe("thread optimistic actions", () => {
  it("appendThreadMessage creates the thread snapshot when missing", () => {
    // A send racing ahead of openThread's initial load still lands.
    useAppStore.getState().appendThreadMessage("root-1", uiMsg("m1"));

    const thread = useAppStore.getState().threadByRoot["root-1"];
    expect(thread?.messages).toHaveLength(1);
    expect(thread?.currentVersion).toBe(0n);
    expect(thread?.loading).toBe(false);
  });

  it("patchThreadMessage writes into the (possibly new) snapshot", () => {
    const s = useAppStore.getState();
    s.appendThreadMessage("root-1", uiMsg("m1"));
    s.patchThreadMessage("root-1", "m1", { uploadProgress: { p: 5 } });

    expect(
      useAppStore.getState().threadByRoot["root-1"]?.messages[0].uploadProgress
    ).toEqual({ p: 5 });
  });

  it("removeThreadMessage drops the row and skips unknown ids", () => {
    const s = useAppStore.getState();
    s.appendThreadMessage("root-1", uiMsg("m1"));
    s.removeThreadMessage("root-1", "m1");
    expect(
      useAppStore.getState().threadByRoot["root-1"]?.messages
    ).toHaveLength(0);
    // Unknown conversation: a silent no-op.
    s.removeThreadMessage("root-2", "m1");
    expect(useAppStore.getState().threadByRoot["root-2"]).toBeUndefined();
  });
});
