import { ClipboardCopy, CornerDownRight, ListTodo } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { useHasPermission } from "@/stores/permissions";
import type { ChatMessageUI } from "@/stores/types";

export interface MessageContextMenuProps {
  // The message the menu acts on. Actions are conditionally available based on
  // its shape: copy is shown for any final message, open-thread only for root
  // messages (no threadRoot), convert-to-task only for root, non-task messages
  // when the caller has the laelia.conversations.send permission.
  msg: ChatMessageUI;
  // content is the raw markdown to copy. The row passes msg.content (the final
  // body) rather than the streaming slice.
  content: string;
  // onCopy is called when "Copy markdown" is chosen. The menu keeps its own
  // open/closed state; the caller handles the clipboard write + toast.
  onCopy: (content: string) => void;
  // onOpenThread, when provided, shows "Open thread" for root messages. Reuses
  // the existing thread panel open flow (the same action as the hover entry).
  onOpenThread?: (msg: ChatMessageUI) => void;
  // onConvertToTask, when provided, shows "Convert to task" for root, non-task
  // messages. Gated behind the laelia.conversations.send permission.
  onConvertToTask?: (msg: ChatMessageUI) => void;
  // canOpenThread can force-hide the entry (e.g. the thread panel itself, where
  // opening a thread from a reply is meaningless). Defaults to true.
  canOpenThread?: boolean;
  // canConvertToTask can force-hide the entry (e.g. non-channel contexts). It
  // only applies to messages that are otherwise convertible (root + non-task);
  // the permission check is applied on top.
  canConvertToTask?: boolean;
  children: React.ReactNode;
}

// MessageContextMenu is the desktop right-click menu attached to a chat row.
// It exposes Copy Markdown, Open thread (root messages), and Convert to task
// (root, non-task messages with laelia.conversations.send). Mobile renders the
// children directly (long-press is used elsewhere in the app and would fight
// the row's swipe actions).
export function MessageContextMenu({
  msg,
  content,
  onCopy,
  onOpenThread,
  onConvertToTask,
  canOpenThread = true,
  canConvertToTask = true,
  children,
}: MessageContextMenuProps) {
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  const canSend = useHasPermission("laelia.conversations.send");

  const isRoot = !msg.threadRoot;
  const isTask = !!msg.task;
  const showOpenThread = !!onOpenThread && isRoot && canOpenThread;
  const showConvertToTask =
    !!onConvertToTask && isRoot && !isTask && canConvertToTask && canSend;

  // Desktop shows the right-click menu; mobile renders the row bare (the
  // trigger would otherwise also answer long-presses, which fights the row's
  // own swipe/tap handling).
  if (!isDesktop) {
    return <>{children}</>;
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger className="block">{children}</ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={() => onCopy(content)}>
          <ClipboardCopy className="size-4" />
          {t("chat.copy-markdown")}
        </ContextMenuItem>
        {showOpenThread && (
          <>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={() => onOpenThread(msg)}>
              <CornerDownRight className="size-4" />
              {t("chat.open-thread")}
            </ContextMenuItem>
          </>
        )}
        {showConvertToTask && (
          <>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={() => onConvertToTask(msg)}>
              <ListTodo className="size-4" />
              {t("chat.convert-to-task")}
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
}
