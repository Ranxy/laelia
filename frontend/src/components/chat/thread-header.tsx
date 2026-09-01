import { ArrowLeft, ExternalLink, Maximize2, Minimize2, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ThreadTaskControls } from "@/components/chat/thread-task-controls";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/ui-models";

// ---------------------------------------------------------------------------
// ThreadHeader — the thread panel's title bar: back-to-tasks drill-in, title,
// "view in channel" jump, task controls (delegated to ThreadTaskControls),
// task creator, expand/collapse, and close. Extracted from the former
// monolithic thread-panel.
// ---------------------------------------------------------------------------

export function ThreadHeader({
  title,
  channelName,
  channelId,
  rootMsg,
  onClose,
  onViewInChannel,
  readOnly,
  expanded,
  onToggleExpand,
}: {
  title: string;
  channelName: string;
  channelId: string;
  rootMsg: ChatMessageUI | null;
  onClose: () => void;
  // onViewInChannel renders the header "View in channel" jump. Omitted (and the
  // button hidden) when the thread is already shown inside its channel — the
  // jump is only meaningful from a standalone/embedded context (activity detail,
  // reminder detail) that is not the channel itself.
  onViewInChannel?: () => void;
  // readOnly hides the close-task action (agent-to-agent DMs are admin
  // view-only, same as the composer).
  readOnly?: boolean;
  // onToggleExpand renders the expand/collapse toggle; expanded selects the
  // icon shown. Both omitted outside the channel page.
  expanded?: boolean;
  onToggleExpand?: () => void;
}) {
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  const closeThread = useAppStore((s) => s.closeThread);
  const toggleTasksPanel = useAppStore((s) => s.toggleTasksPanel);
  const isTask = !!rootMsg?.task;
  const canManageTask = isTask && !readOnly;

  const handleBackToTasks = () => {
    // Drill back from a task's thread to the channel's task board.
    closeThread();
    toggleTasksPanel(channelId);
  };
  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2.5">
      {isTask && (
        <button
          type="button"
          onClick={handleBackToTasks}
          className="flex items-center gap-1 text-xs text-control-placeholder hover:text-main transition-colors"
          aria-label={t("channelTask.back-to-tasks")}
        >
          <ArrowLeft className="size-3.5" />
          <span className="hidden sm:inline">
            {t("channelTask.back-to-tasks")}
          </span>
        </button>
      )}
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold text-main truncate">
          {title} — #{channelName}
        </p>
      </div>
      {onViewInChannel && (
        <button
          type="button"
          onClick={onViewInChannel}
          className="flex items-center gap-1 text-xs text-control-placeholder hover:text-accent transition-colors"
        >
          <ExternalLink className="size-3.5" />
          <span className="hidden sm:inline">
            {t("chat.thread-view-in-channel")}
          </span>
        </button>
      )}
      {canManageTask && rootMsg && (
        <>
          <ThreadTaskControls channelId={channelId} rootMsg={rootMsg} />
        </>
      )}
      {/* Creator display: the task's author (display name, no avatar). */}
      {isTask && rootMsg?.senderName && (
        <span className="hidden shrink-0 text-xs text-control-placeholder sm:inline">
          {t("channelTask.creator", { name: rootMsg.senderName })}
        </span>
      )}
      {/* The expand/collapse toggle is a desktop affordance: on mobile the
          thread panel already fills the screen, so the toggle is meaningless. */}
      {isDesktop && onToggleExpand && (
        <button
          type="button"
          onClick={onToggleExpand}
          className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
          aria-label={t(
            expanded ? "chat.thread-collapse" : "chat.thread-expand"
          )}
          title={t(expanded ? "chat.thread-collapse" : "chat.thread-expand")}
        >
          {expanded ? (
            <Minimize2 className="size-4" />
          ) : (
            <Maximize2 className="size-4" />
          )}
        </button>
      )}
      <button
        type="button"
        onClick={onClose}
        className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
        aria-label={t("chat.thread-close")}
      >
        {isDesktop ? (
          <X className="size-4" />
        ) : (
          <ArrowLeft className="size-4" />
        )}
      </button>
    </div>
  );
}
