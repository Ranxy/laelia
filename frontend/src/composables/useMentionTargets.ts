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

export function targetToMention(target: MentionTarget): Mention {
  return create(MentionSchema, {
    type: target.type,
    id: target.id,
    name: target.handle,
  });
}
