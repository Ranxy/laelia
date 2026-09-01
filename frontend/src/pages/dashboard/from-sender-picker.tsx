import { Combobox } from "@base-ui/react/combobox";
import { Loader2, User as UserIcon, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/chat/avatar";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "@/components/ui/layer";
import { useUserSearch } from "@/hooks/use-user-search";
import { useAvatar } from "@/lib/avatar-cache";
import { avatarNameForAgentId, avatarNameForUserId } from "@/lib/resource";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// FromSender is a selected "From" filter value: either a human user or an
// agent. The backend accepts both handles in SearchChatHistoryRequest.from.
export type FromSender =
  | { kind: "user"; user: User }
  | { kind: "agent"; agent: AgentSummary };

function senderTitle(sender: FromSender): string {
  return sender.kind === "user"
    ? sender.user.title || sender.user.handle || ""
    : sender.agent.title || sender.agent.handle || "";
}

// senderKey identifies a sender across re-renders: roster fetches return new
// user/agent objects each time, so item equality is keyed on the handle.
function senderKey(sender: FromSender): string {
  return sender.kind === "user"
    ? `user:${sender.user.handle || sender.user.name}`
    : `agent:${sender.agent.handle || sender.agent.name}`;
}

// Option rows share one layout: avatar + display name + a Human/Agent badge so
// the two sender kinds are easy to tell apart at a glance. The row wrapper is
// the Base UI Combobox item, which supplies press, keyboard and aria wiring.
const OPTION_ROW_CLASS = cn(
  "flex w-full cursor-pointer items-center gap-2 px-2 py-1.5 text-left text-sm",
  "outline-none hover:bg-control-bg data-highlighted:bg-control-bg"
);

function SenderOption({
  seed,
  avatarSrc,
  title,
  subtitle,
  badge,
}: {
  seed: string;
  avatarSrc: string | null;
  title: string;
  subtitle?: string;
  badge: string;
}) {
  return (
    <>
      <Avatar seed={seed} src={avatarSrc} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-main">{title}</span>
        {subtitle && (
          <span className="block truncate text-xs text-control-placeholder">
            {subtitle}
          </span>
        )}
      </span>
      <span className="shrink-0 rounded bg-control-bg px-1.5 py-0.5 text-[10px] font-medium text-control">
        {badge}
      </span>
    </>
  );
}

// UserPickerOption is one human row in the From autocomplete.
function UserPickerOption({ user }: { user: User }) {
  const { t } = useTranslation();
  const avatarSrc = useAvatar(avatarNameForUserId(user.handle || ""));
  const label = user.title || user.email || user.handle;
  const sublabel = user.handle || user.email;

  return (
    <Combobox.Item value={{ kind: "user", user }} className={OPTION_ROW_CLASS}>
      <SenderOption
        seed={user.handle || user.name}
        avatarSrc={avatarSrc}
        title={label}
        subtitle={sublabel !== label ? sublabel : undefined}
        badge={t("members.kind-user")}
      />
    </Combobox.Item>
  );
}

// AgentPickerOption is one agent row in the From autocomplete.
function AgentPickerOption({ agent }: { agent: AgentSummary }) {
  const { t } = useTranslation();
  const avatarSrc = useAvatar(avatarNameForAgentId(agent.handle || ""));
  const label = agent.title || agent.handle;
  const sublabel = agent.description || agent.handle;

  return (
    <Combobox.Item
      value={{ kind: "agent", agent }}
      className={OPTION_ROW_CLASS}
    >
      <SenderOption
        seed={agent.handle || agent.name}
        avatarSrc={avatarSrc}
        title={label}
        subtitle={sublabel !== label ? sublabel : undefined}
        badge={t("chat.agent")}
      />
    </Combobox.Item>
  );
}

// FromSenderPicker is a single-select sender autocomplete used by the global
// search "From" filter. It matches both human users (server-side search) and
// agents (client-side filter over the shared roster), and shows a Human/Agent
// badge so the two kinds are easy to tell apart. Built on the Base UI
// Combobox; the popup portals through the shared overlay layer.
export function FromSenderPicker({
  value,
  onChange,
  placeholder,
  fullWidth = false,
}: {
  value: FromSender | null;
  onChange: (value: FromSender | null) => void;
  placeholder: string;
  fullWidth?: boolean;
}) {
  const agents = useAppStore((s) => s.agents);
  const agentsLoading = useAppStore((s) => s.agentsLoading);
  const fetchAgents = useAppStore((s) => s.fetchAgents);

  const [query, setQuery] = useState(() => (value ? senderTitle(value) : ""));
  const [open, setOpen] = useState(false);
  const wrapperRef = useRef<HTMLDivElement>(null);

  // Debounced server-side user search, shared with MemberPicker
  // (hooks/use-user-search). Gated on typed input: the empty query clears the
  // results instead of listing a browse page. `searching` flips on with the
  // query change so the dropdown shows the loading state instead of stale rows.
  const { results: userResults, searching: loading } = useUserSearch(query, {
    enabled: query.trim() !== "",
  });

  // Preload the agent roster as soon as the picker mounts so agents are ready
  // before the user starts typing (avoids agents popping in later and making
  // the dropdown look like it "refreshes" with different content).
  useEffect(() => {
    if (useAppStore.getState().agents.length === 0) {
      void fetchAgents({ pageSize: 100 });
    }
  }, [fetchAgents]);

  // Make sure the agent roster is loaded when the picker opens so agents can
  // appear in the dropdown alongside humans.
  useEffect(() => {
    if (!open) return;
    if (useAppStore.getState().agents.length === 0) {
      void fetchAgents({ pageSize: 100 });
    }
  }, [open, fetchAgents]);

  const q = query.trim().toLowerCase();
  const filteredAgents = useMemo(
    () =>
      agents.filter(
        (a) =>
          q === "" ||
          a.title.toLowerCase().includes(q) ||
          a.handle.toLowerCase().includes(q) ||
          a.description.toLowerCase().includes(q)
      ),
    [agents, q]
  );

  const hasResults = userResults.length > 0 || filteredAgents.length > 0;
  // Wait for the first agent-roster fetch too, so the combined list doesn't
  // first appear with only users and then "refresh" when agents arrive.
  const waitingForAgents = agentsLoading && agents.length === 0;

  // The hand-rolled dropdown stayed closed while the query was empty; the
  // Base UI popup is gated the same way on top of the open intent.
  const showDropdown = open && query.trim().length > 0;

  function clear() {
    onChange(null);
    setQuery("");
    setOpen(false);
  }

  return (
    <Combobox.Root
      value={value}
      onValueChange={(next) => {
        if (next === null) {
          onChange(null);
          return;
        }
        onChange(next);
        setQuery(senderTitle(next));
        setOpen(false);
      }}
      inputValue={query}
      onInputValueChange={(next, eventDetails) => {
        // Committing an item echoes its label back through the input; the
        // selection itself arrives via onValueChange.
        if (eventDetails.reason === "item-press") {
          setQuery(next);
          return;
        }
        // Edits while closed would resync the typed text to the selection
        // after the popup unmounts; keep the typed text as the old picker did.
        if (eventDetails.reason !== "input-change") {
          return;
        }
        setQuery(next);
        setOpen(true);
        // The shared hook drops stale results and flips `searching` on this
        // query change, so the dropdown never flashes old content.
        // Typing away from the selected sender starts a fresh lookup.
        if (value && senderTitle(value) !== next) {
          onChange(null);
        }
      }}
      open={showDropdown}
      onOpenChange={setOpen}
      isItemEqualToValue={(a, b) => senderKey(a) === senderKey(b)}
      itemToStringLabel={senderTitle}
    >
      <div ref={wrapperRef} className={cn("relative", fullWidth && "w-full")}>
        <div className="flex items-center gap-1.5 rounded-md border border-control-border px-2 py-1">
          <UserIcon className="size-3.5 shrink-0 text-control-light" />
          <Combobox.Input
            placeholder={placeholder}
            onFocus={() => setOpen(true)}
            autoComplete="off"
            spellCheck={false}
            className={cn(
              "h-6 min-w-0 border-0 bg-transparent px-0 text-xs text-main outline-none",
              "placeholder:text-control-placeholder focus-visible:ring-0",
              fullWidth ? "w-full" : "w-32"
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
                {loading || waitingForAgents ? (
                  <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-control-placeholder">
                    <Loader2 className="size-3.5 animate-spin" />
                  </div>
                ) : !hasResults ? (
                  <div className="px-2 py-1.5 text-xs text-control-placeholder">
                    {placeholder}
                  </div>
                ) : (
                  <>
                    {userResults.map((user) => (
                      <UserPickerOption key={user.name} user={user} />
                    ))}
                    {filteredAgents.map((agent) => (
                      <AgentPickerOption key={agent.name} agent={agent} />
                    ))}
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
