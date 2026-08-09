import { Inbox } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Outlet, useParams } from "react-router-dom";
import { ActivityList } from "@/components/activity/activity-list";
import { EmptyState } from "@/components/chat/states";

// ActivityLayout is the two-pane shell for the per-user Activity feed: a fixed
// left rail (the filterable, polling list of activities) and a right pane that
// embeds the selected item's full view (thread or channel) or an empty state.
// Mirrors ChatLayout's responsive split: on mobile only one pane is visible at
// a time, driven by whether a messageId is selected.
export function ActivityLayout() {
  const { t } = useTranslation();
  const { messageId } = useParams<{ messageId: string }>();

  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* Left rail: the activity list with filters + polling.
          Desktop: always visible as a fixed column.
          Mobile: shown full-width only when no activity is open. */}
      <aside
        className={
          messageId
            ? "hidden w-80 shrink-0 border-r border-control-border bg-background lg:flex lg:flex-col"
            : "flex w-full flex-col shrink-0 border-r border-control-border bg-background lg:w-80"
        }
      >
        <ActivityList />
      </aside>
      {/* Right pane: the selected activity's embedded view (or empty state).
          Mobile: hidden until an activity is opened. */}
      <main
        className={
          messageId ? "min-w-0 flex-1" : "hidden min-w-0 flex-1 lg:block"
        }
      >
        {messageId ? (
          <Outlet />
        ) : (
          <EmptyState
            icon={Inbox}
            message={t("activity.empty-hint")}
            className="h-full"
          />
        )}
      </main>
    </div>
  );
}
