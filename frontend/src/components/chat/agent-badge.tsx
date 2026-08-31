import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// Compact pill rendered right after a conversation title to mark the peer as
// an agent. Shared by the chat left-rail rows and the conversation header so
// both surfaces always render the identical marker.
export function AgentBadge({ className }: { className?: string }) {
  const { t } = useTranslation();
  return (
    <Badge size="sm" variant="secondary" className={cn("shrink-0", className)}>
      {t("chat.agent")}
    </Badge>
  );
}
