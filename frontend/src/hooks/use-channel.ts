import { useQuery, useQueryClient } from "@tanstack/react-query";
import { commandServiceClient } from "@/connect";
import type { Conversation } from "@/types/proto-es/v1/command_pb";

// ---------------------------------------------------------------------------
// useChannel — the single-conversation GetChannel read (09 章 §2.2 收敛).
//
// One shared Query entry replaces the three hand-rolled mount effects that
// each fired the same RPC (chat conversation metadata fallback, channel
// detail deep-link source of truth, activity detail title fallback).
//
// Semantics:
//   - `enabled` gates the fetch AND the exposed value: a disabled gate
//     reports null so the caller's roster entry takes over — mirroring the
//     old "skip the fetch when the conversation is already in the left rail"
//     effects, where the fetched fallback was reset to null.
//   - `loading` is the first-load skeleton case (no cache entry for this key
//     yet); a refetch or cache hit keeps the previous data painted.
//   - `setChannel` writes through the cache (the archive toggle's optimistic
//     flip) — the Query equivalent of the old local useState setter.
// ---------------------------------------------------------------------------

export interface UseChannelResult {
  channel: Conversation | null;
  loading: boolean;
  setChannel: (next: Conversation | null) => void;
}

export function useChannel(
  conversationName: string | null,
  opts: { enabled?: boolean } = {}
): UseChannelResult {
  const { enabled = true } = opts;
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["channel", conversationName],
    queryFn: () => commandServiceClient.getChannel({ name: conversationName! }),
    enabled: enabled && !!conversationName,
    // Metadata read at human pace; the app-default stale window dedupes the
    // chat ↔ activity deep-link hops on the same conversation.
  });

  return {
    channel: enabled ? (query.data ?? null) : null,
    loading: enabled ? query.isPending : false,
    setChannel: (next) =>
      queryClient.setQueryData(["channel", conversationName], next),
  };
}
