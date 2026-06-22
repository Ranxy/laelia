import { Hash, Loader2, Plus, Users } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/react/components/ui/dialog";
import { cn } from "@/react/lib/utils";
import { useAppStore } from "@/react/stores";

export function ChannelListPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const channels = useAppStore((s) => s.channels);
  const channelsLoading = useAppStore((s) => s.channelsLoading);
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const createChannel = useAppStore((s) => s.createChannel);

  const [createOpen, setCreateOpen] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    fetchChannels();
  }, [fetchChannels]);

  const handleCreate = useCallback(async () => {
    const title = newTitle.trim();
    if (!title) return;
    setCreating(true);
    try {
      const ch = await createChannel(title);
      setCreateOpen(false);
      setNewTitle("");
      navigate(`/channels/${ch.name.split("/").pop()}`);
    } catch {
      // create failed
    } finally {
      setCreating(false);
    }
  }, [newTitle, createChannel, navigate]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {/* Header */}
      <div className="flex shrink-0 items-center justify-between border-b border-control-border px-6 py-4">
        <h1 className="text-lg font-semibold text-main">
          {t("channel.title")}
        </h1>
        <button
          type="button"
          onClick={() => setCreateOpen(true)}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground hover:bg-accent-hover transition-colors"
        >
          <Plus className="size-4" />
          {t("channel.create")}
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto">
        {channelsLoading && (
          <div className="flex items-center justify-center gap-2 py-16 text-control-light text-sm">
            <Loader2 className="size-4 animate-spin" />
            {t("common.loading")}
          </div>
        )}

        {!channelsLoading && channels.length === 0 && (
          <div className="flex flex-col items-center justify-center gap-4 py-24 text-center">
            <div className="flex size-14 items-center justify-center rounded-full bg-control-bg text-control-light">
              <Hash className="size-6" />
            </div>
            <p className="text-control-light text-sm max-w-xs">
              {t("channel.empty")}
            </p>
            <button
              type="button"
              onClick={() => setCreateOpen(true)}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-accent-foreground hover:bg-accent-hover transition-colors"
            >
              <Plus className="size-4" />
              {t("channel.create")}
            </button>
          </div>
        )}

        {!channelsLoading && channels.length > 0 && (
          <div className="divide-y divide-control-border">
            {channels.map((ch) => {
              const channelId = ch.name.split("/").pop() ?? "";
              return (
                <button
                  key={ch.name}
                  type="button"
                  onClick={() => navigate(`/channels/${channelId}`)}
                  className="flex w-full items-center gap-4 px-6 py-4 hover:bg-control-bg/40 transition-colors text-left"
                >
                  <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-control-bg text-control">
                    <Hash className="size-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-main truncate">
                      {ch.title}
                    </p>
                    <p className="text-xs text-control-placeholder flex items-center gap-1 mt-0.5">
                      <Users className="size-3" />
                      {t("channel.members", { count: ch.memberCount })}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>
        )}
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
            <input
              type="text"
              className={cn(
                "w-full rounded-lg border border-control-border bg-background px-3 py-2 text-sm text-main",
                "placeholder:text-control-placeholder focus:outline-none focus:border-accent"
              )}
              placeholder={t("channel.name-placeholder")}
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreate();
              }}
              autoFocus
            />
            <div className="flex justify-end gap-3">
              <button
                type="button"
                onClick={() => setCreateOpen(false)}
                className="rounded-lg border border-control-border px-3 py-1.5 text-sm text-control hover:bg-control-bg transition-colors"
              >
                {t("common.cancel")}
              </button>
              <button
                type="button"
                onClick={handleCreate}
                disabled={!newTitle.trim() || creating}
                className={cn(
                  "rounded-lg px-3 py-1.5 text-sm font-medium transition-colors",
                  newTitle.trim() && !creating
                    ? "bg-accent text-accent-foreground hover:bg-accent-hover"
                    : "bg-control-bg text-control-placeholder cursor-not-allowed"
                )}
              >
                {creating ? t("common.creating") : t("common.create")}
              </button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
