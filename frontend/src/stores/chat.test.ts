import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  AttachmentSchema,
  ChatMessageSchema,
  MentionSchema,
} from "@/types/proto-es/v1/command_pb";
import { mergeMessages, toUiMessage } from "./chat";

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

describe("mergeMessages", () => {
  it("detects an attachment added to an existing message", () => {
    const prev = [toUiMessage(buildMessage())];
    const next = [
      toUiMessage(
        buildMessage({
          attachments: [
            create(AttachmentSchema, {
              id: "att-1",
              name: "file.txt",
              mimeType: "text/plain",
              sizeBytes: 4n,
            }),
          ],
        })
      ),
    ];

    const merged = mergeMessages(prev, next);

    // A change was detected: the array reference changed and the new object
    // (carrying the attachment) replaced the stale one.
    expect(merged).not.toBe(prev);
    expect(merged[0]).toBe(next[0]);
    expect(merged[0].attachments).toHaveLength(1);
    expect(merged[0].attachments?.[0].id).toBe("att-1");
  });

  it("detects a mention added to an existing message", () => {
    const prev = [toUiMessage(buildMessage())];
    const next = [
      toUiMessage(
        buildMessage({
          mentions: [
            create(MentionSchema, {
              type: "user",
              id: "users/2",
              name: "Alice",
            }),
          ],
        })
      ),
    ];

    const merged = mergeMessages(prev, next);

    expect(merged).not.toBe(prev);
    expect(merged[0]).toBe(next[0]);
    expect(merged[0].mentions).toHaveLength(1);
    expect(merged[0].mentions?.[0].id).toBe("users/2");
  });

  it("preserves object identity for unchanged messages", () => {
    // Two independent backend round-trips with identical fields.
    const prev = [toUiMessage(buildMessage())];
    const next = [toUiMessage(buildMessage())];

    const merged = mergeMessages(prev, next);

    // No field changed: the whole array reference is preserved...
    expect(merged).toBe(prev);
    // ...and so is the per-message reference (React.memo skips re-render).
    expect(merged[0]).toBe(prev[0]);
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
