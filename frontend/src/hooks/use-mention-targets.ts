import { create } from "@bufbuild/protobuf";
import { useMemo } from "react";
import { useAppStore } from "@/stores";
import {
  type ChannelMember,
  type Mention,
  MentionSchema,
} from "@/types/proto-es/v1/command_pb";

export interface MentionTarget {
  type: "user" | "agent";
  id: string;
  /** Mention handle (e.g. "ran-user-1"); the only token the server parses. */
  handle: string;
  /** Display name shown in the mention popup; never used for parsing. */
  name: string;
}

// Stable empty fallback so the per-conversation selector below returns a
// consistent reference while the conversation's roster is unloaded.
const EMPTY_MEMBERS: ChannelMember[] = [];

export function useMentionTargets(channelId?: string): MentionTarget[] {
  const conversationName = channelId ? `conversations/${channelId}` : "";
  // Select only this conversation's member list (with a stable empty fallback)
  // rather than the whole channelMembersByConv map, so a member change in any
  // *other* conversation no longer re-renders consumers of this hook (the chat
  // page and the thread panel) or recomputes the targets array.
  const members =
    useAppStore((s) =>
      conversationName ? s.channelMembersByConv[conversationName] : undefined
    ) ?? EMPTY_MEMBERS;

  return useMemo(() => {
    if (members.length === 0) return [];
    return members.map((m) => {
      const handle = m.handle || m.memberId;
      return {
        type: m.memberType === 2 ? "agent" : "user",
        id: m.memberId,
        handle,
        name: m.displayName,
      };
    });
  }, [members]);
}

// Builds a stable resolver mapping a mention handle to its display label:
// the member's display name, or "name(handle)" when another member of the
// same channel shares the display name (so the badge stays unambiguous).
// Returns undefined for handles not in the roster (e.g. a member who left),
// letting callers fall back to the raw handle.
export function mentionLabelResolver(
  targets: MentionTarget[]
): (handle: string) => string | undefined {
  const byName = new Map<string, number>();
  for (const t of targets) {
    byName.set(t.name, (byName.get(t.name) ?? 0) + 1);
  }
  const labels = new Map<string, string>();
  for (const t of targets) {
    labels.set(
      t.handle,
      (byName.get(t.name) ?? 0) > 1 ? `${t.name}(${t.handle})` : t.name
    );
  }
  return (handle: string) => labels.get(handle);
}

// Stable per-conversation label resolver; recreated only when the roster
// changes, so memoized message lists keep bailing out.
export function useMentionLabelResolver(
  channelId?: string
): (handle: string) => string | undefined {
  const targets = useMentionTargets(channelId);
  return useMemo(() => mentionLabelResolver(targets), [targets]);
}

export function targetToMention(target: MentionTarget): Mention {
  return create(MentionSchema, {
    type: target.type,
    id: target.id,
    name: target.handle,
  });
}
