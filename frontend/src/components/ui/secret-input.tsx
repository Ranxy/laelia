import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Input, type InputProps } from "@/components/ui/input";
import { cn } from "@/lib/utils";

// SecretInput renders a masked field for API keys and other secrets without
// ever using type="password": browsers treat password-typed inputs as login
// credentials (offering to save/autofill them and turning nearby text fields
// into "username" fields) even when autoComplete="off". Masking is done with
// CSS -webkit-text-security (Chrome/Edge/Safari, Firefox 121+), so the browser
// classifies the field as a plain text input. The eye toggle reveals it.
function SecretInput({ className, ...props }: InputProps) {
  const { t } = useTranslation();
  const [revealed, setRevealed] = useState(false);
  return (
    <div className="relative">
      <Input
        {...props}
        type="text"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        data-1p-ignore
        data-lpignore="true"
        data-form-type="other"
        className={cn(
          "pr-9",
          !revealed && "[-webkit-text-security:disc]",
          className
        )}
      />
      <button
        type="button"
        aria-label={t("common.toggle-secret-visibility")}
        className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 text-control-placeholder hover:text-control hover:cursor-pointer"
        onClick={() => setRevealed((v) => !v)}
      >
        {revealed ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  );
}

export { SecretInput };
