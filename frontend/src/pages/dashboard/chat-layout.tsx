import { useEffect } from "react";
import { Outlet, useParams } from "react-router-dom";
import { ConversationList } from "@/components/chat/conversation-list";
import { TwoPaneShell } from "@/components/ui/two-pane-shell";
import { usePolling } from "@/hooks/use-polling";
import { useAppStore } from "@/stores";

// Left-rail list refresh cadence. The right pane long-polls the open
// conversation's messages via startWatchingChannel (one held request, woken on
// new messages); the list poll is lighter and only refreshes the roster +
// unread badges so new messages in other conversations surface. 5s is enough
// for badge updates without doubling the message-poll load.
const LIST_POLL_INTERVAL_MS = 5000;

export function ChatLayout() {
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const { conversationId } = useParams<{ conversationId: string }>();

  useEffect(() => {
    fetchChannels();
  }, [fetchChannels]);
  usePolling(fetchChannels, LIST_POLL_INTERVAL_MS);

  return (
    <TwoPaneShell
      detailOpen={Boolean(conversationId)}
      width="w-72"
      // ChatLayout's mobile rail keeps its historical row direction —
      // ConversationList manages its own column internally (see
      // TwoPaneShell.railMobileDirection). Preserved from its old copy.
      railMobileDirection="row"
      railClassName="bg-background"
      rail={
        // Merged channel + DM list with unread badges. Desktop: always a fixed
        // column; mobile: full-width only while no conversation is open.
        <ConversationList />
      }
    >
      {/* The open conversation (or empty state); mobile: hidden until one is
          opened. */}
      <Outlet />
    </TwoPaneShell>
  );
}
