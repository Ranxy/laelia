import { Inbox } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Outlet, useParams } from "react-router-dom";
import { ActivityList } from "@/components/activity/activity-list";
import { EmptyState } from "@/components/chat/states";
import { TwoPaneShell } from "@/components/ui/two-pane-shell";

// ActivityLayout is the two-pane shell for the per-user Activity feed: a fixed
// left rail (the filterable, polling list of activities) and a right pane that
// embeds the selected item's full view or the empty hint. Mirrors ChatLayout's
// responsive split via the shared TwoPaneShell: on mobile only one pane is
// visible at a time, driven by whether a messageId is selected.
export function ActivityLayout() {
  const { t } = useTranslation();
  const { messageId } = useParams<{ messageId: string }>();

  return (
    <TwoPaneShell
      detailOpen={Boolean(messageId)}
      width="w-80"
      railClassName="bg-background"
      rail={
        // The activity list with filters + polling; mobile shows it
        // full-width only while no activity is open.
        <ActivityList />
      }
    >
      {/* The selected activity's embedded view (or empty hint on mobile). */}
      {messageId ? (
        <Outlet />
      ) : (
        <EmptyState
          icon={Inbox}
          message={t("activity.empty-hint")}
          className="h-full"
        />
      )}
    </TwoPaneShell>
  );
}
