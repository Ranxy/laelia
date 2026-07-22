import type { ReactNode } from "react";

interface FieldRowProps {
  label: string;
  hint?: string;
  htmlFor?: string;
  children: ReactNode;
}

// FieldRow renders a labeled form field with an optional hint.
// Used inside sheets and dialogs for a consistent vertical rhythm.
export function FieldRow({ label, hint, htmlFor, children }: FieldRowProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <label
        htmlFor={htmlFor}
        className="text-xs font-semibold uppercase tracking-wide text-control"
      >
        {label}
      </label>
      {children}
      {hint && <span className="text-xs text-control-placeholder">{hint}</span>}
    </div>
  );
}
