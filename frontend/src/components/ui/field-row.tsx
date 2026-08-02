import type { ReactNode } from "react";

interface FieldRowProps {
  label: string;
  required?: boolean;
  hint?: string;
  htmlFor?: string;
  children: ReactNode;
}

// FieldRow renders a labeled form field with an optional hint.
// Used inside sheets and dialogs for a consistent vertical rhythm.
export function FieldRow({
  label,
  required,
  hint,
  htmlFor,
  children,
}: FieldRowProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <label
        htmlFor={htmlFor}
        className="text-xs font-semibold uppercase tracking-wide text-control"
      >
        {label}
        {required && <span className="text-danger"> *</span>}
      </label>
      {children}
      {hint && <span className="text-xs text-control-placeholder">{hint}</span>}
    </div>
  );
}
