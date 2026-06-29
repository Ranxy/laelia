import { useEffect, useRef } from "react";
import { Outlet, useParams } from "react-router-dom";
import { ConversationList } from "@/components/chat/conversation-list";
import { useAppStore } from "@/stores";

// Left-rail list refresh cadence. The right pane polls the open conversation's
// messages every 2s via startWatchingChannel; the list poll is lighter and only
// refreshes the roster + unread badges so new messages in other conversations
// surface. 5s is enough for badge updates without doubling the message-poll load.
const LIST_POLL_INTERVAL_MS = 5000;

export function ChatLayout() {
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const { conversationId } = useParams<{ conversationId: string }>();

  useEffect(() => {
    fetchChannels();
    timerRef.current = setInterval(() => {
      fetchChannels();
    }, LIST_POLL_INTERVAL_MS);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [fetchChannels]);

  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* Left rail: merged channel + DM list with unread badges.
          Desktop: always visible as a fixed column.
          Mobile: shown full-width only when no conversation is open. */}
      <aside
        className={
          conversationId
            ? "hidden w-72 shrink-0 border-r border-control-border bg-background lg:flex lg:flex-col"
            : "flex w-full shrink-0 border-r border-control-border bg-background lg:w-72 lg:flex-col"
        }
      >
        <ConversationList />
      </aside>
      {/* Right pane: the selected conversation (or empty state).
          Mobile: hidden until a conversation is opened. */}
      <main
        className={
          conversationId ? "min-w-0 flex-1" : "hidden min-w-0 flex-1 lg:block"
        }
      >
        <Outlet />
      </main>
    </div>
  );
}
