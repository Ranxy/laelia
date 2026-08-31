import { Combobox } from "@base-ui/react/combobox";
import {
  Bot,
  ChevronDown,
  Hash,
  Loader2,
  SlidersHorizontal,
  Users,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "@/components/ui/layer";
import { cn } from "@/lib/utils";
import type { Conversation } from "@/types/proto-es/v1/command_pb";

// Conversation type values mirror Conversation.type from the API:
// 1 = user↔agent DM, 2 = channel, 3 = agent↔agent DM, 4 = user↔user DM.
const CONVERSATION_TYPE_DM = 1;
const CONVERSATION_TYPE_CHANNEL = 2;
const CONVERSATION_TYPE_AGENT_DM = 3;
const CONVERSATION_TYPE_USER_DM = 4;

function conversationDisplayName(conv: Conversation): string {
  return conv.title || conv.address || conv.name;
}

function conversationSubLabel(conv: Conversation): string {
  return conv.address || conv.peer || conv.name;
}

function conversationSearchText(conv: Conversation): string {
  return [conv.title, conv.address, conv.peer, conv.name]
    .filter(Boolean)
    .join(" ");
}

function ConversationTypeIcon({
  type,
  className,
}: {
  type: number;
  className?: string;
}) {
  switch (type) {
    case CONVERSATION_TYPE_CHANNEL:
      return <Hash className={className} />;
    case CONVERSATION_TYPE_USER_DM:
      return <Users className={className} />;
    case CONVERSATION_TYPE_DM:
    case CONVERSATION_TYPE_AGENT_DM:
    default:
      return <Bot className={className} />;
  }
}

// Option rows render as Base UI Combobox items, which supply press, keyboard
// and aria wiring for the list.
const OPTION_ROW_CLASS = cn(
  "flex w-full cursor-pointer items-center gap-2 px-2 py-1.5 text-left text-sm",
  "outline-none hover:bg-control-bg data-highlighted:bg-control-bg"
);

// ConversationPicker is a searchable single-select for the "All conversations"
// filter. It lists every conversation the current user participates in
// (channels, user DMs, agent DMs), lets the user type to filter by title,
// address, peer or id, and commits the full conversation resource name. Built
// on the Base UI Combobox; the popup portals through the shared overlay layer.
export function ConversationPicker({
  value,
  onChange,
  conversations,
  loading = false,
  placeholder,
  fullWidth = false,
}: {
  value: string;
  onChange: (value: string) => void;
  conversations: Conversation[];
  loading?: boolean;
  placeholder: string;
  fullWidth?: boolean;
}) {
  const current = conversations.find((c) => c.name === value);
  const [query, setQuery] = useState(() =>
    current ? conversationDisplayName(current) : ""
  );
  const [open, setOpen] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  // Keep the visible label in sync when the selected value changes from
  // outside (e.g. another control clears the filter) while the popup is
  // closed.
  useEffect(() => {
    if (open) return;
    const selected = conversations.find((c) => c.name === value);
    setQuery(selected ? conversationDisplayName(selected) : "");
  }, [conversations, open, value]);

  const q = query.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      conversations.filter(
        (c) => q === "" || conversationSearchText(c).toLowerCase().includes(q)
      ),
    [conversations, q]
  );

  const currentLabel = current ? conversationDisplayName(current) : "";

  function select(conv: Conversation) {
    onChange(conv.name);
    setQuery(conversationDisplayName(conv));
    setOpen(false);
  }

  function clear() {
    onChange("");
    setQuery("");
    setOpen(false);
  }

  return (
    <Combobox.Root
      value={value}
      onValueChange={(next) => {
        if (next === null) {
          clear();
          return;
        }
        const conv = conversations.find((c) => c.name === next);
        if (conv) select(conv);
      }}
      inputValue={query}
      onInputValueChange={(next, eventDetails) => {
        // Committing an item echoes its label back through the input; the
        // selection itself arrives via onValueChange.
        if (eventDetails.reason !== "input-change") {
          return;
        }
        setQuery(next);
        setOpen(true);
        // Typing away from the selected conversation starts a fresh lookup
        // over all conversations.
        if (value && currentLabel !== next) {
          onChange("");
        }
      }}
      open={open}
      onOpenChange={setOpen}
      itemToStringLabel={(name) => {
        const conv = conversations.find((c) => c.name === name);
        return conv ? conversationDisplayName(conv) : name;
      }}
    >
      <div ref={wrapperRef} className={cn("relative", fullWidth && "w-full")}>
        <div className="flex items-center gap-1.5 rounded-md border border-control-border px-2 py-1">
          {current ? (
            <ConversationTypeIcon
              type={current.type}
              className="size-3.5 shrink-0 text-control-light"
            />
          ) : (
            <SlidersHorizontal className="size-3.5 shrink-0 text-control-light" />
          )}
          <Combobox.Input
            placeholder={placeholder}
            aria-label={placeholder}
            onFocus={() => setOpen(true)}
            autoComplete="off"
            spellCheck={false}
            className={cn(
              "h-6 min-w-0 border-0 bg-transparent px-0 text-xs text-main outline-none",
              "placeholder:text-control-placeholder focus-visible:ring-0",
              fullWidth ? "w-full" : "w-48"
            )}
          />
          {(value || query) && (
            <button
              type="button"
              onClick={clear}
              className="shrink-0 rounded p-0.5 text-control-light transition-colors hover:bg-control-bg hover:text-main"
              aria-label={placeholder}
            >
              <X className="size-3" />
            </button>
          )}
          <ChevronDown className="size-3.5 shrink-0 text-control-light" />
        </div>
        <Combobox.Portal container={getLayerRoot("overlay")}>
          <Combobox.Positioner
            anchor={wrapperRef}
            align="start"
            sideOffset={4}
            className={LAYER_SURFACE_CLASS}
          >
            <Combobox.Popup className="max-h-60 min-w-(--anchor-width) overflow-auto rounded-sm border border-control-border bg-background py-1 shadow-md">
              <Combobox.List>
                {loading && conversations.length === 0 ? (
                  <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-control-placeholder">
                    <Loader2 className="size-3.5 animate-spin" />
                  </div>
                ) : (
                  <>
                    <button
                      type="button"
                      onMouseDown={(e) => {
                        e.preventDefault();
                        clear();
                      }}
                      className="flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm hover:bg-control-bg"
                    >
                      <SlidersHorizontal className="size-3.5 shrink-0 text-control-light" />
                      <span className="truncate text-main">{placeholder}</span>
                    </button>
                    {filtered.length === 0 ? (
                      <div className="px-2 py-1.5 text-xs text-control-placeholder">
                        {placeholder}
                      </div>
                    ) : (
                      filtered.map((conv) => (
                        <Combobox.Item
                          key={conv.name}
                          value={conv.name}
                          className={OPTION_ROW_CLASS}
                        >
                          <ConversationTypeIcon
                            type={conv.type}
                            className="size-3.5 shrink-0 text-control-light"
                          />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-main">
                              {conversationDisplayName(conv)}
                            </span>
                            {conversationSubLabel(conv) && (
                              <span className="block truncate text-xs text-control-placeholder">
                                {conversationSubLabel(conv)}
                              </span>
                            )}
                          </span>
                        </Combobox.Item>
                      ))
                    )}
                  </>
                )}
              </Combobox.List>
            </Combobox.Popup>
          </Combobox.Positioner>
        </Combobox.Portal>
      </div>
    </Combobox.Root>
  );
}
