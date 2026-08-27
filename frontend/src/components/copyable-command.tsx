import { Check, Copy } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

// CopyableCommand renders a shell command with a copy action. On narrow
// screens the command box and the button stack: the button spans the full
// width below the box so it is an easy touch target (a small side-by-side
// button is easy to miss). From sm up they sit on one compact row.
export function CopyableCommand({
  command,
  copied,
  onCopy,
}: {
  command: string;
  copied: boolean;
  onCopy: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
      <code className="min-w-0 flex-1 rounded bg-white border border-control-border px-3 py-2 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
        {command}
      </code>
      <Button
        variant="outline"
        size="sm"
        className="h-9 w-full px-3 text-sm leading-5 sm:h-7 sm:w-auto sm:px-2 sm:text-xs sm:leading-4"
        onClick={onCopy}
      >
        {copied ? (
          <Check className="size-4 text-success" />
        ) : (
          <Copy className="size-4" />
        )}
        {copied ? t("common.copied") : t("common.copy")}
      </Button>
    </div>
  );
}
