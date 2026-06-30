import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  AttachmentSchema,
  ChatMessageSchema,
  MentionSchema,
} from "@/types/proto-es/v1/command_pb";
import { appendNewMessages, toUiMessage } from "./chat";

// A fixed timestamp shared across fixtures so unchanged round-trips produce
// equal Date values (timestampDate(ts) is deterministic for a given input).
const fixedTimestamp = create(TimestampSchema, {
  seconds: 1_700_000_000n,
  nanos: 0,
});

function buildMessage(
  overrides: Partial<{
    name: string;
    content: string;
    role: number;
    commandId: string;
    mentions: ReturnType<typeof create<typeof MentionSchema>>[];
    attachments: ReturnType<typeof create<typeof AttachmentSchema>>[];
  }> = {}
) {
  return create(ChatMessageSchema, {
    name: overrides.name ?? "conversations/c/messages/1",
    conversation: "conversations/c",
    principalName: "users/1",
    role: overrides.role ?? 1,
    content: overrides.content ?? "hello",
    commandId: overrides.commandId ?? "",
    createdAt: fixedTimestamp,
    senderName: "users/1",
    senderType: 1,
    roomVersion: 1n,
    mentions: overrides.mentions ?? [],
    attachments: overrides.attachments ?? [],
    isOwn: false,
  });
}

describe("appendNewMessages", () => {
  it("appends delta messages whose id is not already present, in order", () => {
    const prev = [toUiMessage(buildMessage({ name: "m1" }))];
    const delta = [
      toUiMessage(buildMessage({ name: "m2", content: "second" })),
      toUiMessage(buildMessage({ name: "m3", content: "third" })),
    ];

    const merged = appendNewMessages(prev, delta);

    expect(merged).not.toBe(prev);
    expect(merged.map((m) => m.id)).toEqual(["m1", "m2", "m3"]);
  });

  it("skips delta messages already in prev (optimistic-send echo dedup)", () => {
    // The optimistic send already appended the user message with its real
    // server id; the watcher's after-version delta echoes it back. The dedup
    // must keep the optimistic copy and drop the echo, then append only the
    // genuinely new agent reply.
    const optimistic = toUiMessage(buildMessage({ name: "m1", content: "hi" }));
    const prev = [optimistic];
    const delta = [
      toUiMessage(buildMessage({ name: "m1", content: "hi" })),
      toUiMessage(buildMessage({ name: "m2", role: 2, content: "reply" })),
    ];

    const merged = appendNewMessages(prev, delta);

    expect(merged).not.toBe(prev);
    expect(merged).toHaveLength(2);
    // The existing message keeps its reference (no duplicate, no replacement).
    expect(merged[0]).toBe(optimistic);
    expect(merged[1].id).toBe("m2");
  });

  it("returns the same reference when nothing was added", () => {
    const prev = [toUiMessage(buildMessage({ name: "m1" }))];
    // Empty delta — a poll that found no new messages.
    expect(appendNewMessages(prev, [])).toBe(prev);
    // Delta whose ids are all already present.
    expect(
      appendNewMessages(prev, [toUiMessage(buildMessage({ name: "m1" }))])
    ).toBe(prev);
  });
});

describe("toUiMessage", () => {
  it("always populates mentions and attachments", () => {
    // A message that carries both fields.
    const withBoth = buildMessage({
      mentions: [
        create(MentionSchema, { type: "user", id: "users/2", name: "Alice" }),
      ],
      attachments: [
        create(AttachmentSchema, {
          id: "att-1",
          name: "file.txt",
          mimeType: "text/plain",
          sizeBytes: 4n,
        }),
      ],
    });
    const uiWithBoth = toUiMessage(withBoth);
    expect(uiWithBoth.mentions).toHaveLength(1);
    expect(uiWithBoth.attachments).toHaveLength(1);

    // A message with neither: the fields are still populated (as empty arrays),
    // never undefined, so downstream renderers can safely map over them.
    const empty = buildMessage();
    const uiEmpty = toUiMessage(empty);
    expect(uiEmpty.mentions).toEqual([]);
    expect(uiEmpty.attachments).toEqual([]);
  });
});
