import type { ReactNode } from "react";
import type { AgentProviderInfo } from "@/types/proto-es/v1/agent_pb";

// Presentational helpers shared by the agent and machine profile pages. They
// were previously copy-pasted between the two (with trivial drift); the ACP
// config form itself is still page-local because the two pages wire it to very
// different state.

// Field renders a labeled value row in the identity grid. The label is muted
// and right-aligned on a fixed column so values line up vertically.
export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <>
      <dt className="text-xs text-control-light whitespace-nowrap pt-0.5">
        {label}
      </dt>
      <dd className="text-sm text-main min-w-0 break-words">{children}</dd>
    </>
  );
}

export function Card({
  title,
  children,
  footer,
  actions,
}: {
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <section className="flex flex-col rounded-lg border border-control-border bg-background shadow-xs">
      <header className="flex items-center justify-between border-b border-control-border px-5 py-3">
        <h2 className="text-sm font-semibold text-control">{title}</h2>
        {actions}
      </header>
      <div className="flex flex-col gap-4 p-5">{children}</div>
      {footer && (
        <footer className="border-t border-control-border px-5 py-3">
          {footer}
        </footer>
      )}
    </section>
  );
}

export function providerDisplayName(p: AgentProviderInfo): string {
  if (p.displayName) {
    return p.version ? `${p.displayName} (${p.version})` : p.displayName;
  }
  return p.providerId;
}

export function providerLabel(
  id: string,
  providers: AgentProviderInfo[]
): string {
  if (id === "custom") return "";
  const p = providers.find((it) => it.providerId === id);
  return p ? providerDisplayName(p) : id;
}

export function modelLabel(
  value: string,
  models: { value: string; name: string }[]
) {
  const m = models.find((it) => it.value === value);
  return m ? m.name || m.value : value;
}

// Phase-1 API providers for the built-in pi runtime. The model list for each
// is fetched dynamically from the provider's model API (see usePiModels), never
// hardcoded.
export const piAPIProviderIds = ["deepseek", "openrouter"];
