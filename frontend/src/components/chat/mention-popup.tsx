import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "@/components/ui/layer";
import type { MentionTarget } from "@/composables/useMentionTargets";

interface MentionPopupProps {
  id: string;
  targets: MentionTarget[];
  query: string;
  position: { top: number; left: number; height: number };
  selectedIndex: number;
  onSelect: (target: MentionTarget) => void;
  onClose: () => void;
}

function optionId(popupId: string, index: number): string {
  return `${popupId}-opt-${index}`;
}

export function MentionPopup({
  id,
  targets,
  query,
  position,
  selectedIndex,
  onSelect,
  onClose,
}: MentionPopupProps) {
  const { t } = useTranslation();
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

  const users = targets.filter((target) => target.type === "user");
  const agents = targets.filter((target) => target.type === "agent");

  let optionIndex = -1;
  const renderOption = (target: MentionTarget, active: boolean) => {
    optionIndex += 1;
    const current = optionIndex;
    return (
      <MentionItem
        key={target.id}
        id={optionId(id, current)}
        target={target}
        query={query}
        active={active}
        onClick={() => onSelect(target)}
      />
    );
  };

  return createPortal(
    <div
      id={id}
      ref={listRef}
      role="listbox"
      aria-label={t("chat.mention-list-aria-label")}
      className={`fixed ${LAYER_SURFACE_CLASS} w-56 max-h-48 overflow-y-auto rounded-lg border border-control-border bg-background shadow-lg`}
      style={{
        bottom: window.innerHeight - position.top + 4,
        left: position.left,
      }}
    >
      {targets.length === 0 && (
        <div className="px-3 py-2 text-xs text-control-placeholder">
          {t("chat.mention-no-matches", { query })}
        </div>
      )}
      {users.length > 0 && (
        <>
          <div className="px-3 py-1 text-xs text-control-placeholder font-medium">
            {t("chat.mention-group-users")}
          </div>
          {users.map((target, i) => renderOption(target, i === selectedIndex))}
        </>
      )}
      {agents.length > 0 && (
        <>
          <div className="px-3 py-1 text-xs text-control-placeholder font-medium">
            {t("chat.mention-group-agents")}
          </div>
          {agents.map((target, i) =>
            renderOption(target, i + users.length === selectedIndex)
          )}
        </>
      )}
    </div>,
    getLayerRoot("overlay")
  );
}

function MentionItem({
  id,
  target,
  query,
  active,
  onClick,
}: {
  id: string;
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
      role="option"
      id={id}
      aria-selected={active}
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
