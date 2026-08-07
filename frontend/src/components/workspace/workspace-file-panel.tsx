import { create } from "@bufbuild/protobuf";
import { FileText, Loader2, X } from "lucide-react";
import MarkdownRender from "markstream-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { formatBytes } from "@/components/chat/file-card";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/stores";
import {
  type WorkspaceEntry,
  type WorkspaceReadResponse,
  WorkspaceReadResponseSchema,
} from "@/types/proto-es/v1/agent_pb";

interface WorkspaceFilePanelProps {
  agentName: string;
  // entry is the file shown in the right pane; null shows the empty state.
  entry: WorkspaceEntry | null;
  onClose: () => void;
}

const MARKDOWN_EXTENSIONS = new Set([".md", ".markdown"]);

function isMarkdownFile(name: string): boolean {
  const dot = name.lastIndexOf(".");
  if (dot < 0) return false;
  return MARKDOWN_EXTENSIONS.has(name.slice(dot).toLowerCase());
}

// WorkspaceFilePanel is the right pane of the agent workspace browser: it
// shows one file's content next to the tree. Text files render in a monospace
// pane, markdown files through markstream-react, images inline, and other
// binary files as metadata only. The machine returns a non-empty `error` for
// files it refuses to read (sensitive, too large, missing), rendered instead
// of content.
export function WorkspaceFilePanel({
  agentName,
  entry,
  onClose,
}: WorkspaceFilePanelProps) {
  const { t } = useTranslation();
  const readAgentWorkspaceFile = useAppStore((s) => s.readAgentWorkspaceFile);
  const [file, setFile] = useState<WorkspaceReadResponse | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!entry) {
      setFile(null);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setFile(null);
    readAgentWorkspaceFile(agentName, entry.path)
      .then((res) => {
        if (!cancelled) setFile(res);
      })
      .catch(() => {
        if (!cancelled)
          setFile(
            create(WorkspaceReadResponseSchema, {
              error: t("workspace.load-error"),
            })
          );
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [agentName, entry, readAgentWorkspaceFile, t]);

  if (!entry) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-control-light">
        <FileText className="size-8" />
        <p className="text-sm">{t("workspace.select-file")}</p>
      </div>
    );
  }

  const markdown = !file?.binary && isMarkdownFile(entry.name);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-4 py-2">
        <FileText className="size-4 shrink-0 text-control-light" />
        <span className="truncate font-mono text-sm text-main">
          {entry.name}
        </span>
        <span className="ml-auto shrink-0 font-mono text-xs text-control-light">
          {formatBytes(file?.size ?? entry.size)}
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="size-7 p-0"
          onClick={onClose}
          aria-label={t("workspace.close")}
        >
          <X className="size-4" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {loading ? (
          <div className="flex items-center gap-2 px-4 py-4 text-sm text-control-light">
            <Loader2 className="size-4 animate-spin" />
            {t("workspace.loading")}
          </div>
        ) : !file ? null : file.error ? (
          <p className="px-4 py-2 text-sm text-danger">{file.error}</p>
        ) : file.binary && file.mimeType && file.content ? (
          <div className="flex h-full items-start justify-center p-4">
            <img
              src={`data:${file.mimeType};base64,${file.content}`}
              alt={entry.name}
              className="max-h-full max-w-full rounded-sm"
            />
          </div>
        ) : file.binary ? (
          <p className="px-4 py-2 text-sm text-control-light">
            {t("workspace.binary-file")} — {formatBytes(file.size)}
          </p>
        ) : markdown ? (
          <div className="markstream-chat mx-auto w-full max-w-4xl px-6 py-6">
            <MarkdownRender
              customId="workspace-md-preview"
              content={file.content}
              final
              fade
              batchRendering
              deferNodesUntilVisible={false}
            />
          </div>
        ) : (
          <pre className="whitespace-pre-wrap break-words px-4 py-2 font-mono text-sm">
            {file.content}
          </pre>
        )}
      </div>
    </div>
  );
}
