import { ShieldAlert } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/stores";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";

interface ChatPermissionRequestProps {
  event: CommandEvent;
  commandName: string;
}

export function ChatPermissionRequest({
  event,
  commandName,
}: ChatPermissionRequestProps) {
  const { t } = useTranslation();
  const respondPermission = useAppStore((s) => s.respondPermission);
  const [responded, setResponded] = useState(false);

  if (event.payload.case !== "permissionRequested") return null;

  const { kind, title, options } = event.payload.value;

  const handleRespond = async (optionId: string) => {
    setResponded(true);
    try {
      await respondPermission(commandName, optionId);
    } catch {
      setResponded(false);
    }
  };

  if (responded) {
    return (
      <div className="rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-xs text-control-light">
        <ShieldAlert className="size-3.5 inline mr-1.5" />
        {t("command.event-permission-decided")}
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-warning/30 bg-warning/5 px-3 py-2.5 flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <ShieldAlert className="size-3.5 shrink-0 text-warning" />
        <Badge variant="warning" className="text-[10px] px-1.5 py-0 shrink-0">
          {kind}
        </Badge>
        <span className="text-xs text-main truncate flex-1">
          {title || t("command.permission-required")}
        </span>
      </div>
      <div className="flex items-center gap-2 flex-wrap">
        {options.map((opt) => (
          <Button
            key={opt.optionId}
            variant={
              opt.kind === "allow_once" || opt.kind === "allow_always"
                ? "default"
                : "outline"
            }
            size="xs"
            onClick={() => handleRespond(opt.optionId)}
          >
            {opt.name}
          </Button>
        ))}
      </div>
    </div>
  );
}
