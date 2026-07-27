import { Hash, Loader2, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useAvatar } from "@/lib/avatar-cache";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { Conversation } from "@/types/proto-es/v1/command_pb";

// Conversation type values mirror Conversation.type: 1 = user↔agent DM,
// 2 = channel, 4 = user↔user DM.
const CONVERSATION_TYPE_DM = 1;
const CONVERSATION_TYPE_USER_DM = 4;

export function ConversationList() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { conversationId } = useParams<{ conversationId: string }>();

  const channels = useAppStore((s) => s.channels);
  const channelsLoading = useAppStore((s) => s.channelsLoading);
  const unreadByConv = useAppStore((s) => s.unreadByConv);
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const createChannel = useAppStore((s) => s.createChannel);

  const [createOpen, setCreateOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [creating, setCreating] = useState(false);
  const [query, setQuery] = useState("");

  useEffect(() => {
    fetchChannels();
  }, [fetchChannels]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return channels;
    return channels.filter((c) => (c.title ?? "").toLowerCase().includes(q));
  }, [channels, query]);

  const handleCreate = useCallback(async () => {
    const title = newTitle.trim();
    if (!title) return;
    setCreating(true);
    try {
      const ch = await createChannel(title);
      setCreateOpen(false);
      setNewTitle("");
      navigate(`/${ch.name.split("/").pop()}`);
    } catch {
      // create failed
    } finally {
      setCreating(false);
    }
  }, [newTitle, createChannel, navigate]);

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      {/* Header */}
      <div className="flex shrink-0 items-center justify-between gap-2 border-b border-control-border px-4 py-3">
        <h2 className="text-sm font-semibold text-main">{t("chat.title")}</h2>
        <Button
          onClick={() => setCreateOpen(true)}
          size="sm"
          className="size-7 p-0"
          aria-label={t("channel.create")}
        >
          <Plus className="size-4" />
        </Button>
      </div>

      {/* Search */}
      <div className="shrink-0 px-3 py-2">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("chat.search-placeholder")}
          className="h-8 text-sm"
        />
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto">
        {channelsLoading && channels.length === 0 && (
          <div className="flex items-center justify-center gap-2 py-12 text-control-light text-sm">
            <Loader2 className="size-4 animate-spin" />
            {t("common.loading")}
          </div>
        )}

        {!channelsLoading && filtered.length === 0 && (
          <div className="flex flex-col items-center justify-center gap-3 px-6 py-16 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-control-bg text-control-light">
              <Hash className="size-5" />
            </div>
            <p className="text-control-light text-xs max-w-[200px]">
              {query ? t("chat.select-conversation") : t("channel.empty")}
            </p>
          </div>
        )}

        {filtered.map((conv) => {
          const id = conv.name.split("/").pop() ?? "";
          const isDm = conv.type === CONVERSATION_TYPE_DM;
          const isUserDm = conv.type === CONVERSATION_TYPE_USER_DM;
          const active = id === conversationId;
          const unread = unreadByConv[conv.name] ?? 0;
          return (
            <ConversationRow
              key={conv.name}
              conv={conv}
              isDm={isDm}
              isUserDm={isUserDm}
              active={active}
              unread={unread}
              onClick={() => navigate(`/${id}`)}
            />
          );
        })}
      </div>

      {/* Create Channel Dialog */}
      <Dialog
        open={createOpen}
        onOpenChange={(open) => !open && setCreateOpen(false)}
      >
        <DialogContent className="max-w-md">
          <DialogTitle>{t("channel.create-title")}</DialogTitle>
          <DialogDescription>
            {t("channel.create-description")}
          </DialogDescription>
          <div className="mt-2 space-y-4">
            <Input
              placeholder={t("channel.name-placeholder")}
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreate();
              }}
              autoFocus
            />
            <div className="flex justify-end gap-3">
              <Button variant="outline" onClick={() => setCreateOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button
                onClick={handleCreate}
                disabled={!newTitle.trim() || creating}
              >
                {creating ? t("common.creating") : t("common.create")}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ConversationRow({
  conv,
  isDm,
  isUserDm,
  active,
  unread,
  onClick,
}: {
  conv: Conversation;
  isDm: boolean;
  isUserDm: boolean;
  active: boolean;
  unread: number;
  onClick: () => void;
}) {
  const isDirect = isDm || isUserDm;
  // conv.peer is the DM peer's resource name ("users/<id>" or "agents/<id>");
  // appending "/avatar" yields the avatar resource name the cache dispatches by
  // prefix. Undefined for channels (no peer), which keep the Hash icon below.
  const avatarName = conv.peer ? `${conv.peer}/avatar` : undefined;
  const avatarSrc = useAvatar(avatarName);
  const peerId = conv.peer ? (conv.peer.split("/").pop() ?? "") : "";
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-3 px-3 py-2.5 text-left transition-colors",
        active ? "bg-accent/10" : "hover:bg-control-bg/40"
      )}
    >
      {isDirect ? (
        <Avatar src={avatarSrc} seed={peerId || conv.title || conv.name} />
      ) : (
        <div
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-lg",
            active ? "bg-accent/15 text-accent" : "bg-control-bg text-control"
          )}
        >
          <Hash className="size-4" />
        </div>
      )}
      <div className="min-w-0 flex-1">
        <p
          className={cn(
            "text-sm truncate",
            unread > 0 ? "font-semibold text-main" : "font-medium text-main"
          )}
        >
          {conv.title || conv.name}
        </p>
        {!isDirect && (
          <p className="text-xs text-control-placeholder mt-0.5">
            {conv.memberCount} {conv.memberCount === 1 ? "member" : "members"}
          </p>
        )}
      </div>
      {unread > 0 && (
        <span
          className={cn(
            "shrink-0 inline-flex items-center justify-center rounded-full",
            "min-w-5 h-5 px-1.5 text-xs font-semibold",
            isDirect
              ? "bg-accent text-accent-foreground"
              : "bg-accent/15 text-accent"
          )}
        >
          {unread > 99 ? "99+" : unread}
        </span>
      )}
    </button>
  );
}
