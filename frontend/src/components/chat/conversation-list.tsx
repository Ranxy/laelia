import { timestampDate } from "@bufbuild/protobuf/wkt";
import { Hash, Loader2, Pin, PinOff, Plus, X } from "lucide-react";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { useAvatar } from "@/lib/avatar-cache";
import { formatConversationListTime } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";

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
  const createChannel = useAppStore((s) => s.createChannel);
  const setConversationPinned = useAppStore((s) => s.setConversationPinned);
  const setConversationClosed = useAppStore((s) => s.setConversationClosed);
  // currentUser's {user} segment is the decimal principal id used to tag the
  // viewer's own messages in the last-message preview ("You: ...").
  const myPrincipalId = useAppStore((s) =>
    s.currentUser?.name.split("/").pop()
  );

  const [createOpen, setCreateOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [creating, setCreating] = useState(false);
  const [query, setQuery] = useState("");
  // True once the list has been scrolled down; the mobile create-channel
  // FAB collapses to a bare icon while the list is not at the top.
  const [listScrolled, setListScrolled] = useState(false);

  // No mount fetch here: ChatLayout (the only host of this list) owns the
  // listChannels fetch + 5s poll, so fetching again here duplicated the request
  // on every /chat entry.

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

  // Stable per-row handlers (the id is threaded through the row's own
  // onClick) so ConversationRow's memo can bail out on unrelated re-renders —
  // e.g. an unread-badge change on another conversation must not re-render
  // every row in the rail.
  const handleOpen = useCallback(
    (id: string) => {
      navigate(`/${id}`);
    },
    [navigate]
  );

  const handleTogglePin = useCallback(
    (id: string, pinned: boolean) => {
      setConversationPinned(id, pinned);
    },
    [setConversationPinned]
  );

  // Close hides the row from the rail without touching the conversation or
  // its messages; the server clears the flag again on the next main-channel
  // message, so the chat reappears on its own. The toast leaves a short undo
  // window for accidental closes.
  const handleClose = useCallback(
    (id: string) => {
      void setConversationClosed(id, true);
      toastManager.add({
        type: "info",
        title: t("chat.closed-title"),
        timeout: 5000,
        actionProps: {
          children: t("chat.undo"),
          onClick: () => {
            void setConversationClosed(id, false);
          },
        },
      });
    },
    [setConversationClosed, t]
  );

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      {/* Header */}
      <div className="hidden shrink-0 items-center justify-between gap-2 border-b border-control-border px-3 py-2 lg:flex lg:px-4 lg:py-3">
        <h2 className="hidden text-sm font-semibold text-main lg:block">
          {t("chat.title")}
        </h2>
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
      <div className="shrink-0 px-2 py-2 lg:px-3">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("chat.search-placeholder")}
          className="h-8 rounded-full text-sm"
        />
      </div>

      {/* List */}
      {/* divide-y gives each row a hairline top border so the rail scans
          quickly; /50 keeps the divider light against the row whitespace. */}
      <div
        data-testid="conversation-list-scroll"
        className="flex-1 divide-y divide-control-border/50 overflow-y-auto"
        onScroll={(e) => setListScrolled(e.currentTarget.scrollTop > 8)}
      >
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
              id={id}
              title={conv.title || conv.name}
              peer={conv.peer}
              pinned={conv.pinned ?? false}
              memberCount={conv.memberCount}
              isDirect={isDm || isUserDm}
              active={active}
              unread={unread}
              lastMessage={conv.lastMessage}
              lastMessageSender={conv.lastMessageSender}
              lastMessageIsMine={
                conv.lastMessagePrincipalId !== "" &&
                conv.lastMessagePrincipalId === myPrincipalId
              }
              lastMessageAtMs={
                conv.lastMessageAt
                  ? timestampDate(conv.lastMessageAt).getTime()
                  : undefined
              }
              onOpen={handleOpen}
              onTogglePin={handleTogglePin}
              onClose={handleClose}
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
      {/* Mobile create-channel FAB: replaces the header + button on touch
          layouts. Pills up with an icon + label until the list is scrolled
          down, then collapses to a bare icon pinned above the tab bar in the
          bottom-right corner. */}
      <button
        type="button"
        onClick={() => setCreateOpen(true)}
        aria-label={t("channel.create")}
        data-testid="create-channel-fab"
        className={cn(
          "fixed right-4 z-chrome flex h-14 items-center justify-center gap-1.5 overflow-hidden",
          "bottom-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom)+0.75rem)]",
          "rounded-full bg-accent text-accent-text shadow-lg transition-all duration-200",
          "focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2",
          "lg:hidden",
          listScrolled ? "w-14" : "w-32"
        )}
      >
        <Plus className="size-6 shrink-0" strokeWidth={2.25} />
        {!listScrolled && (
          <span className="text-sm font-semibold whitespace-nowrap">
            {t("channel.fab-label")}
          </span>
        )}
      </button>
    </div>
  );
}

// ConversationRow is memoized and receives only primitive props (plus stable
// id-threaded handlers), so a re-render of the parent — an unread-badge change
// on one conversation, or a fetchChannels poll — does not re-render every row:
// rows whose title/unread/active/pinned are unchanged bail out.
// Two 72px swipe actions (pin + close) side by side.
const SWIPE_ACTION_WIDTH = 144;

const ConversationRow = memo(function ConversationRow({
  id,
  title,
  peer,
  pinned,
  memberCount,
  isDirect,
  active,
  unread,
  lastMessage,
  lastMessageSender,
  lastMessageIsMine,
  lastMessageAtMs,
  onOpen,
  onTogglePin,
  onClose,
}: {
  id: string;
  title: string;
  // peer is the DM peer's resource name ("users/<id>" or "agents/<id>");
  // appending "/avatar" yields the avatar resource name the cache dispatches by
  // prefix. Undefined for channels (no peer), which keep the Hash icon below.
  peer?: string;
  pinned: boolean;
  memberCount: number;
  isDirect: boolean;
  active: boolean;
  unread: number;
  // last-message preview from ListChannels: single-line truncated content,
  // the author's display name, whether the author is the viewer (from the
  // decimal principal id), and the send time in ms. The row stays memoizable
  // because every value here is a primitive.
  lastMessage: string;
  lastMessageSender: string;
  lastMessageIsMine: boolean;
  lastMessageAtMs?: number;
  onOpen: (id: string) => void;
  onTogglePin: (id: string, pinned: boolean) => void;
  onClose: (id: string) => void;
}) {
  const { t } = useTranslation();
  const avatarName = peer ? `${peer}/avatar` : undefined;
  const avatarSrc = useAvatar(avatarName);
  const peerId = peer ? (peer.split("/").pop() ?? "") : "";

  const [offset, setOffset] = useState(0);
  const startXRef = useRef(0);
  const startOffsetRef = useRef(0);

  // Close the swipe action when the row becomes active (user navigated into it)
  // or when pinned state changes so the UI doesn't feel stuck.
  useEffect(() => {
    setOffset(0);
  }, [active, pinned]);

  const clampOffset = useCallback((value: number) => {
    return Math.max(0, Math.min(SWIPE_ACTION_WIDTH, value));
  }, []);

  const handleTouchStart = useCallback(
    (e: React.TouchEvent) => {
      startXRef.current = e.touches[0].clientX;
      startOffsetRef.current = offset;
    },
    [offset]
  );

  const handleTouchMove = useCallback(
    (e: React.TouchEvent) => {
      const clientX = e.touches[0]?.clientX ?? startXRef.current;
      const delta = startXRef.current - clientX;
      // Only allow left-swipe (positive delta) from an already-open or closed
      // state; right-swipe closes the action.
      const next = clampOffset(startOffsetRef.current + delta);
      setOffset(next);
    },
    [clampOffset]
  );

  const handleTouchEnd = useCallback(() => {
    setOffset((current) => {
      // Snap open if dragged past half the action width, otherwise close.
      return current > SWIPE_ACTION_WIDTH / 2 ? SWIPE_ACTION_WIDTH : 0;
    });
  }, []);

  const handleOpen = useCallback(() => {
    if (offset > 8) {
      setOffset(0);
      return;
    }
    onOpen(id);
  }, [offset, onOpen, id]);

  const handlePinClick = useCallback(() => {
    onTogglePin(id, !pinned);
    setOffset(0);
  }, [onTogglePin, id, pinned]);

  const handleCloseClick = useCallback(() => {
    onClose(id);
    setOffset(0);
  }, [onClose, id]);

  const isDesktop = useIsDesktop();

  const row = (
    <>
      {/* Mobile swipe actions: revealed by left-swiping the row. The
          destructive close action hugs the screen edge (iOS convention) with
          the pin toggle to its left. */}
      <button
        type="button"
        onClick={handleCloseClick}
        aria-label={t("chat.close")}
        data-testid="swipe-close"
        className={cn(
          "absolute inset-y-0 right-0 z-0 flex w-[72px] shrink-0 items-center justify-center",
          "bg-error text-white transition-colors lg:hidden"
        )}
      >
        <X className="size-5" />
      </button>
      <button
        type="button"
        onClick={handlePinClick}
        aria-label={pinned ? t("channel.unpin") : t("channel.pin")}
        data-testid="swipe-pin"
        className={cn(
          "absolute inset-y-0 right-[72px] z-0 flex w-[72px] shrink-0 items-center justify-center",
          "transition-colors lg:hidden",
          pinned
            ? "bg-accent/15 text-accent"
            : "bg-control-bg text-control-light"
        )}
      >
        {pinned ? <PinOff className="size-5" /> : <Pin className="size-5" />}
      </button>

      <button
        type="button"
        onClick={handleOpen}
        onTouchStart={handleTouchStart}
        onTouchMove={handleTouchMove}
        onTouchEnd={handleTouchEnd}
        style={{
          transform: `translateX(${-offset}px)`,
          transition: offset === 0 ? "transform 200ms ease-out" : "none",
        }}
        className={cn(
          "relative z-10 flex w-full items-center gap-3 bg-background px-2 py-2.5 pr-10 text-left transition-colors lg:pl-3 lg:pr-10",
          active ? "bg-accent/10" : "hover:bg-control-bg/40"
        )}
      >
        {isDirect ? (
          <Avatar src={avatarSrc} seed={peerId || title} />
        ) : (
          <div
            className={cn(
              "flex size-8 shrink-0 items-center justify-center rounded-lg",
              active ? "bg-accent/15 text-accent" : "bg-control-bg text-control"
            )}
          >
            <Hash className="size-4" />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-baseline gap-1.5">
            <p
              className={cn(
                "text-sm truncate",
                unread > 0 ? "font-semibold text-main" : "font-medium text-main"
              )}
            >
              {title}
            </p>
            {!isDirect && (
              <span className="shrink-0 text-xs text-control-placeholder">
                {t("channel.members", { count: memberCount })}
              </span>
            )}
            {lastMessageAtMs !== undefined && (
              <span className="ml-auto shrink-0 pl-1 text-xs text-control-placeholder">
                {formatConversationListTime(lastMessageAtMs)}
              </span>
            )}
          </div>
          {/* Preview of the newest main-channel message. A non-breaking space
              keeps the line height uniform for conversations with no messages
              yet. */}
          <p className="mt-0.5 truncate text-xs text-control-light">
            {lastMessage
              ? lastMessageIsMine
                ? `${t("chat.you")}: ${lastMessage}`
                : lastMessageSender
                  ? `${lastMessageSender}: ${lastMessage}`
                  : lastMessage
              : "\u00A0"}
          </p>
        </div>
        {/* The pinned indicator is mobile-only: on desktop the always-visible
            pin/unpin button in the row's corner already conveys the state, so
            showing both would render a pin and an unpin icon side by side. */}
        {pinned && (
          <Pin
            className="size-3.5 shrink-0 text-accent lg:hidden"
            aria-hidden
          />
        )}
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
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onTogglePin(id, !pinned);
        }}
        aria-label={pinned ? t("channel.unpin") : t("channel.pin")}
        className={cn(
          // z-20 keeps the desktop pin button clickable above the row's z-10
          // surface (the row covers the full width and would otherwise swallow
          // the click and open the channel instead of toggling the pin).
          "absolute right-1 top-1/2 z-20 hidden size-6 -translate-y-1/2 items-center justify-center rounded transition-colors lg:inline-flex",
          pinned
            ? "text-accent opacity-100"
            : "text-control-light opacity-0 hover:bg-control-bg group-hover:opacity-100"
        )}
      >
        {pinned ? (
          <PinOff className="size-3.5" />
        ) : (
          <Pin className="size-3.5" />
        )}
      </button>
    </>
  );

  // Desktop right-click menu (Pin/Unpin + Close). Mobile gets the swipe
  // actions only: the trigger div would otherwise also answer long-presses,
  // which fights the swipe gesture.
  if (isDesktop) {
    return (
      <ContextMenu>
        <ContextMenuTrigger className="group relative flex w-full items-center overflow-hidden">
          {row}
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={handlePinClick}>
            {pinned ? (
              <PinOff className="size-4" />
            ) : (
              <Pin className="size-4" />
            )}
            {t(pinned ? "channel.unpin" : "channel.pin")}
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem onClick={handleCloseClick}>
            <X className="size-4" />
            {t("chat.close")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
    );
  }

  return (
    <div className="group relative flex w-full items-center overflow-hidden">
      {row}
    </div>
  );
});
