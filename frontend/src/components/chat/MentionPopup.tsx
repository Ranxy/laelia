import { useEffect, useRef } from "react";
import type { MentionTarget } from "@/composables/useMentionTargets";

interface MentionPopupProps {
  targets: MentionTarget[];
  query: string;
  position: { top: number; left: number; height: number };
  selectedIndex: number;
  onSelect: (target: MentionTarget) => void;
  onClose: () => void;
}

export function MentionPopup({
  targets,
  query,
  position,
  selectedIndex,
  onSelect,
  onClose,
}: MentionPopupProps) {
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = listRef.current;
    if (el) {
      const item = el.children[selectedIndex] as HTMLElement | undefined;
      item?.scrollIntoView({ block: "nearest" });
    }
  }, [selectedIndex]);

  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const handler = (e: MouseEvent) => {
      if (!el.contains(e.target as Node)) onClose();
    };
    window.addEventListener("mousedown", handler, true);
    return () => window.removeEventListener("mousedown", handler, true);
  }, [onClose]);

  const users = targets.filter((t) => t.type === "user");
  const agents = targets.filter((t) => t.type === "agent");

  return (
    <div
      ref={listRef}
      className="fixed z-[2600] w-56 max-h-48 overflow-y-auto rounded-lg border border-control-border bg-background shadow-lg"
      style={{
        bottom: window.innerHeight - position.top + 4,
        left: position.left,
      }}
    >
      {targets.length === 0 && (
        <div className="px-3 py-2 text-xs text-control-placeholder">
          No matches for &#39;@{query}&#39;
        </div>
      )}
      {users.length > 0 && (
        <>
          <div className="px-3 py-1 text-xs text-control-placeholder font-medium">
            Users
          </div>
          {users.map((t, i) => (
            <MentionItem
              key={t.id}
              target={t}
              query={query}
              active={i === selectedIndex}
              onClick={() => onSelect(t)}
            />
          ))}
        </>
      )}
      {agents.length > 0 && (
        <>
          <div className="px-3 py-1 text-xs text-control-placeholder font-medium">
            Agents
          </div>
          {agents.map((t, i) => (
            <MentionItem
              key={t.id}
              target={t}
              query={query}
              active={i + users.length === selectedIndex}
              onClick={() => onSelect(t)}
            />
          ))}
        </>
      )}
    </div>
  );
}

function MentionItem({
  target,
  query,
  active,
  onClick,
}: {
  target: MentionTarget;
  query: string;
  active: boolean;
  onClick: () => void;
}) {
  const idx = target.name.toLowerCase().indexOf(query.toLowerCase());
  const before = target.name.slice(0, idx);
  const match = target.name.slice(idx, idx + query.length);
  const after = target.name.slice(idx + query.length);

  return (
    <button
      type="button"
      className={`flex w-full items-center gap-2 px-3 py-1.5 text-sm text-left ${
        active ? "bg-accent text-background" : "text-main hover:bg-control-bg"
      }`}
      onMouseDown={(e) => {
        e.preventDefault();
        onClick();
      }}
    >
      <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-control-bg text-xs font-medium text-control">
        {target.name.charAt(0).toUpperCase()}
      </span>
      <span>
        @{before}
        <span className="font-semibold">{match}</span>
        {after}
      </span>
    </button>
  );
}
