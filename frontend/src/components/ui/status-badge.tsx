import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Badge, type BadgeVariant } from "@/components/ui/badge";

// One mapping entry: the Badge color plus the i18n key rendered as the label.
export interface StatusBadgeEntry {
  variant: BadgeVariant;
  labelKey: string;
}

// mergeStatusMapping zips a lib status→variant table with its status→i18n
// table into the single `mapping` shape StatusBadge consumes, so the lib
// tables stay the source of truth.
export function mergeStatusMapping<T extends string | number>(
  variants: Record<T, BadgeVariant>,
  labelKeys: Record<T, string>
): Partial<Record<T, StatusBadgeEntry>> {
  return Object.fromEntries(
    Object.entries(variants).map(([status, variant]) => [
      status,
      { variant, labelKey: labelKeys[status as T] },
    ])
  ) as Partial<Record<T, StatusBadgeEntry>>;
}

interface StatusBadgeProps<T extends string | number> {
  mapping: Partial<Record<T, StatusBadgeEntry>>;
  status: T | undefined;
  // Rendered for a status that is undefined or absent from the mapping.
  fallback: StatusBadgeEntry;
  className?: string;
  // Override the default `t(entry.labelKey)` content (e.g. TaskStatusBadge's
  // "[#N · status · assignee]" label).
  children?: (entry: StatusBadgeEntry) => ReactNode;
}

// StatusBadge renders the shared enum→{variant,labelKey} pill: colors and
// label keys live in lookup tables, components stay thin glue over it.
function StatusBadge<T extends string | number>({
  mapping,
  status,
  fallback,
  className,
  children,
}: StatusBadgeProps<T>) {
  const { t } = useTranslation();
  const entry =
    (status === undefined ? undefined : mapping[status]) ?? fallback;
  return (
    <Badge variant={entry.variant} className={className}>
      {children ? children(entry) : t(entry.labelKey)}
    </Badge>
  );
}

export { StatusBadge };
