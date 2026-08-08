import { useTranslation } from "react-i18next";

import type { TokenUsagePayload } from "@/types/proto-es/v1/command_pb";

// TokenUsageCard renders the per-command token consumption (input/output/
// cache/total) recorded in the TOKEN_USAGE event, shown on the command detail
// summary tab once the command completes.
export function TokenUsageCard({ usage }: { usage: TokenUsagePayload }) {
  const { t } = useTranslation();
  const rows = [
    { key: "command.token-input", value: Number(usage.inputTokens) },
    { key: "command.token-output", value: Number(usage.outputTokens) },
    { key: "command.token-cache-read", value: Number(usage.cacheReadTokens) },
    { key: "command.token-cache-write", value: Number(usage.cacheWriteTokens) },
  ];
  return (
    <div className="rounded border border-control-border p-3">
      <div className="flex items-center gap-2">
        <span className="text-xs font-medium text-info">
          {t("command.token-usage")}
        </span>
        <span className="text-xs text-control-light ml-auto">
          {t("command.token-total")}:{" "}
          {Number(usage.totalTokens).toLocaleString()}
        </span>
      </div>
      <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-control-light">
        {rows.map((row) => (
          <span key={row.key}>
            {t(row.key)}: {row.value.toLocaleString()}
          </span>
        ))}
      </div>
    </div>
  );
}
