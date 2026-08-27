import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// Compact pill rendered right after a conversation title to mark the peer as
// an agent. Shared by the chat left-rail rows and the conversation header so
// both surfaces always render the identical marker.
export function AgentBadge({ className }: { className?: string }) {
  const { t } = useTranslation();
  return (
    <Badge
      variant="secondary"
      className={cn("shrink-0 px-1.5 py-0 text-[10px] leading-4", className)}
    >
      {t("chat.agent")}
    </Badge>
  );
}
