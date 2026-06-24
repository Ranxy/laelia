import { useMemo } from "react";
import { useAppStore } from "@/stores";
import type { Mention } from "@/types/proto-es/v1/command_pb";

export interface MentionTarget {
  type: "user" | "agent";
  id: string;
  name: string;
}

export function useMentionTargets(channelId?: string): MentionTarget[] {
  const membersByConv = useAppStore((s) => s.channelMembersByConv);

  return useMemo(() => {
    if (!channelId) return [];

    const conversationName = `conversations/${channelId}`;
    const members = membersByConv[conversationName];
    if (!members) return [];

    return members.map((m) => ({
      type: m.memberType === 2 ? "agent" : "user",
      id: m.memberId,
      name: m.displayName,
    }));
  }, [channelId, membersByConv]);
}

export function targetToMention(target: MentionTarget): Mention {
  return {
    $typeName: "laelia.v1.Mention",
    type: target.type,
    id: target.id,
    name: target.name,
  } as Mention;
}
