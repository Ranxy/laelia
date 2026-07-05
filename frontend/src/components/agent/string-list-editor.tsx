import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

// StringListEditor edits a list of free-text strings (e.g. command args or
// allow-env var names). Empty rows are kept so a user can stage additions before
// saving; trimming/filtering is the caller's responsibility.
export function StringListEditor({
  label,
  placeholder,
  values,
  onChange,
}: {
  label: string;
  placeholder: string;
  values: string[];
  onChange: (next: string[]) => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <label className="text-sm font-medium">{label}</label>
        <Button
          variant="outline"
          size="sm"
          onClick={() => onChange([...values, ""])}
        >
          {t("common.add")}
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        {values.map((value, index) => (
          <div key={index} className="flex items-center gap-2">
            <Input
              placeholder={placeholder}
              value={value}
              onChange={(e) => {
                const next = [...values];
                next[index] = e.target.value;
                onChange(next);
              }}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => onChange(values.filter((_, i) => i !== index))}
            >
              {t("common.remove")}
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}
