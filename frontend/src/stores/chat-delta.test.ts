import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";
import { ChatMessageSchema } from "@/types/proto-es/v1/command_pb";
import { fetchConversationDelta } from "./chat";

// fetchConversationDelta is driven by scripted listConversationMessages pages:
// each step yields messages (by name) plus a nextPageToken that names the next
// step, so pagination/cursor behavior is asserted directly.
const mock = vi.hoisted(() => ({
  calls: 0,
  // Global per-message counter so roomVersions are distinct across pages.
  msgIndex: 1,
  script: [] as { names: string[]; nextToken: string }[],
  currentVersion: 0n,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    async listConversationMessages() {
      const step = mock.script[mock.calls++];
      if (!step) {
        return {
          messages: [],
          nextPageToken: "",
          currentVersion: mock.currentVersion,
        };
      }
      return {
        messages: step.names.map((name) =>
          create(ChatMessageSchema, {
            name,
            content: "x",
            role: 1,
            roomVersion: BigInt(mock.msgIndex++),
          })
        ),
        nextPageToken: step.nextToken,
        currentVersion: mock.currentVersion,
      };
    },
  },
}));

describe("fetchConversationDelta", () => {
  it("returns a single page and the server cursor when there is no next page", async () => {
    mock.calls = 0;
    mock.msgIndex = 1;
    mock.currentVersion = 10n;
    mock.script = [
      {
        names: ["conversations/c/messages/1", "conversations/c/messages/2"],
        nextToken: "",
      },
    ];

    const { uiMsgs, currentVersion } = await fetchConversationDelta(
      "conversations/c",
      0n
    );

    expect(uiMsgs.map((m) => m.id)).toEqual([
      "conversations/c/messages/1",
      "conversations/c/messages/2",
    ]);
    expect(currentVersion).toBe(10n);
    expect(mock.calls).toBe(1);
  });

  it("follows nextPageToken across pages in order", async () => {
    mock.calls = 0;
    mock.msgIndex = 1;
    mock.currentVersion = 50n;
    mock.script = [
      {
        names: ["conversations/c/messages/1", "conversations/c/messages/2"],
        nextToken: "p2",
      },
      { names: ["conversations/c/messages/3"], nextToken: "" },
    ];

    const { uiMsgs, currentVersion } = await fetchConversationDelta(
      "conversations/c",
      0n
    );

    expect(uiMsgs.map((m) => m.id)).toEqual([
      "conversations/c/messages/1",
      "conversations/c/messages/2",
      "conversations/c/messages/3",
    ]);
    expect(currentVersion).toBe(50n);
    expect(mock.calls).toBe(2);
  });

  it("keeps the cursor at the last fetched message when the delta exceeds the page cap", async () => {
    mock.calls = 0;
    mock.msgIndex = 1;
    mock.currentVersion = 999n;
    // MAX_DELTA_PAGES is 10: every page still points to a next token, so the
    // fetch must NOT advance the cursor to the server's currentVersion (which
    // would permanently skip the un-fetched remainder on the next poll).
    mock.script = Array.from({ length: 10 }, (_, i) => ({
      names: [`conversations/c/messages/${i + 1}`],
      nextToken: `p${i + 1}`,
    }));

    const { uiMsgs, currentVersion } = await fetchConversationDelta(
      "conversations/c",
      0n
    );

    expect(uiMsgs).toHaveLength(10);
    expect(mock.calls).toBe(10);
    // Falls back to the last fetched message's room_version (1..10), not 999n.
    expect(currentVersion).toBe(10n);
  });
});
