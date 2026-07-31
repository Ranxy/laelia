import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";

// ContextUsageBar renders the latest CONTEXT_USAGE_UPDATE as a live
// used/size progress bar in the command events panel, replacing the noisy
// per-update event rows.
export function ContextUsageBar({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  if (event.payload.case !== "contextUsage") return null;

  const payload = event.payload.value;
  const size = Number(payload.size);
  const used = Number(payload.used);
  if (!(size > 0)) return null;

  const ratio = payload.usageRatio > 0 ? payload.usageRatio : used / size;
  const pct = Math.min(100, Math.max(0, Math.round(ratio * 100)));

  return (
    <div className="flex flex-col gap-1 py-1.5 px-3 rounded border border-control-border">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium text-info">
          {t("command.event-context-usage")}
        </span>
        <span className="text-xs text-control-light ml-auto">
          {pct}% ({used.toLocaleString()}/{size.toLocaleString()} tokens)
        </span>
      </div>
      <div className="h-1.5 rounded-full bg-dark-bg overflow-hidden">
        <div
          className={cn(
            "h-full rounded-full",
            pct >= 90 ? "bg-error" : pct >= 75 ? "bg-warning" : "bg-info"
          )}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}
