import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

// KeyValueEnvEditor edits a set of NAME=value environment overrides. Entries
// with empty keys are dropped on save (empty values are kept so a user can set
// FOO=""), which the caller folds into a Record<string,string>.
export function KeyValueEnvEditor({
  label,
  entries,
  onChange,
}: {
  label: string;
  entries: { key: string; value: string }[];
  onChange: (next: { key: string; value: string }[]) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium">{label}</label>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onChange([...entries, { key: "", value: "" }])}
        >
          {t("common.add")}
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        {entries.map((entry, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              placeholder={t("agent.acp-config-custom-env-key-placeholder")}
              value={entry.key}
              onChange={(e) => {
                const next = [...entries];
                next[index] = { ...next[index], key: e.target.value };
                onChange(next);
              }}
            />
            <Input
              placeholder={t("agent.acp-config-custom-env-value-placeholder")}
              value={entry.value}
              onChange={(e) => {
                const next = [...entries];
                next[index] = { ...next[index], value: e.target.value };
                onChange(next);
              }}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => onChange(entries.filter((_, i) => i !== index))}
            >
              {t("common.remove")}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}
