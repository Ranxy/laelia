import {
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { Fragment, useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { WorkspaceEntry } from "@/types/proto-es/v1/agent_pb";

// TreeRow mirrors one WorkspaceEntry plus its lazily loaded children.
// children === null means the directory has not been expanded yet.
interface TreeRow {
  entry: WorkspaceEntry;
  children: TreeRow[] | null;
  expanded: boolean;
  loading: boolean;
}

interface WorkspaceTreeProps {
  agentName: string;
  onPreview: (entry: WorkspaceEntry) => void;
}

// WorkspaceTree renders a lazily loaded file tree of an agent's workspace.
// Directories load one level at a time when expanded; the "show hidden files"
// toggle re-fetches the tree from the root. Server-side filtering (node_modules,
// never-visible paths, secret handling) is applied on the machine.
export function WorkspaceTree({ agentName, onPreview }: WorkspaceTreeProps) {
  const { t } = useTranslation();
  const listAgentWorkspaceDir = useAppStore((s) => s.listAgentWorkspaceDir);
  const [includeHidden, setIncludeHidden] = useState(false);
  const [rows, setRows] = useState<TreeRow[]>([]);
  const [rootLoading, setRootLoading] = useState(true);
  const [rootError, setRootError] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);

  const loadDir = useCallback(
    async (dirPath: string): Promise<TreeRow[]> => {
      const entries = await listAgentWorkspaceDir(
        agentName,
        dirPath,
        includeHidden
      );
      return entries.map((entry) => ({
        entry,
        children: null,
        expanded: false,
        loading: false,
      }));
    },
    [agentName, includeHidden, listAgentWorkspaceDir]
  );

  useEffect(() => {
    let cancelled = false;
    setRootLoading(true);
    setRootError(false);
    loadDir("")
      .then((loaded) => {
        if (!cancelled) setRows(loaded);
      })
      .catch(() => {
        if (!cancelled) setRootError(true);
      })
      .finally(() => {
        if (!cancelled) setRootLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [loadDir, reloadKey]);

  async function toggleDir(row: TreeRow) {
    if (!row.entry.isDirectory) {
      onPreview(row.entry);
      return;
    }
    if (row.expanded) {
      setRows((prev) => patchRow(prev, row.entry.path, { expanded: false }));
      return;
    }
    setRows((prev) => patchRow(prev, row.entry.path, { expanded: true }));
    if (row.children !== null) return;
    setRows((prev) => patchRow(prev, row.entry.path, { loading: true }));
    try {
      const children = await loadDir(row.entry.path);
      setRows((prev) =>
        patchRow(prev, row.entry.path, { children, loading: false })
      );
    } catch {
      setRows((prev) =>
        patchRow(prev, row.entry.path, { children: [], loading: false })
      );
    }
  }

  function reload() {
    setReloadKey((k) => k + 1);
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex shrink-0 items-center gap-3 border-b border-control-border px-4 py-2">
        <label className="flex cursor-pointer items-center gap-2 text-sm text-control">
          <Checkbox
            checked={includeHidden}
            onCheckedChange={(checked) => setIncludeHidden(checked === true)}
            size="sm"
          />
          {t("workspace.show-hidden")}
        </label>
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto size-7 p-0"
          onClick={reload}
          aria-label={t("workspace.refresh")}
        >
          <RefreshCw className="size-4" />
        </Button>
      </div>
      <div className="flex-1 overflow-auto p-2">
        {rootLoading ? (
          <div className="flex items-center gap-2 p-2 text-sm text-control-light">
            <Loader2 className="size-4 animate-spin" />
            {t("workspace.loading")}
          </div>
        ) : rootError ? (
          <div className="flex flex-col items-start gap-2 p-2 text-sm text-control-light">
            <span>{t("workspace.load-error")}</span>
            <Button variant="outline" size="sm" onClick={reload}>
              {t("workspace.refresh")}
            </Button>
          </div>
        ) : rows.length === 0 ? (
          <p className="p-2 text-sm text-control-light">
            {t("workspace.empty")}
          </p>
        ) : (
          <TreeRows
            rows={rows}
            depth={0}
            onToggle={toggleDir}
            onPreview={onPreview}
          />
        )}
      </div>
    </div>
  );
}

// patchRow updates the row whose entry.path matches path, immutably.
function patchRow(
  rows: TreeRow[],
  path: string,
  patch: Partial<Pick<TreeRow, "children" | "expanded" | "loading">>
): TreeRow[] {
  return rows.map((row) => {
    if (row.entry.path === path) return { ...row, ...patch };
    if (row.children) {
      const children = patchRow(row.children, path, patch);
      if (children !== row.children) return { ...row, children };
    }
    return row;
  });
}

function TreeRows({
  rows,
  depth,
  onToggle,
  onPreview,
}: {
  rows: TreeRow[];
  depth: number;
  onToggle: (row: TreeRow) => void;
  onPreview: (entry: WorkspaceEntry) => void;
}) {
  const { t } = useTranslation();
  return (
    <>
      {rows.map((row) => {
        const { entry } = row;
        return (
          <Fragment key={entry.path}>
            <button
              type="button"
              onClick={() => onToggle(row)}
              className={cn(
                "flex w-full cursor-pointer items-center gap-1.5 rounded-sm px-2 py-1 text-left font-mono text-sm hover:bg-control-bg",
                entry.isHidden && "opacity-60"
              )}
              style={{ paddingLeft: `${depth * 16 + 8}px` }}
            >
              {entry.isDirectory ? (
                row.expanded ? (
                  <FolderOpen className="size-4 shrink-0 text-accent" />
                ) : (
                  <Folder className="size-4 shrink-0 text-accent" />
                )
              ) : (
                <FileText className="size-4 shrink-0 text-control-light" />
              )}
              <span className="truncate">{entry.name}</span>
              {row.loading && (
                <Loader2 className="size-3.5 shrink-0 animate-spin" />
              )}
              {entry.isDirectory && (
                <ChevronRight
                  className={cn(
                    "ml-auto size-3.5 shrink-0 text-control-light transition-transform",
                    row.expanded && "rotate-90"
                  )}
                />
              )}
            </button>
            {row.expanded &&
              (row.children === null || row.loading ? (
                <div
                  className="flex items-center gap-2 py-1 font-mono text-xs text-control-light"
                  style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}
                >
                  <Loader2 className="size-3 animate-spin" />
                  {t("workspace.loading")}
                </div>
              ) : row.children.length === 0 ? (
                <p
                  className="py-1 font-mono text-xs text-control-light"
                  style={{ paddingLeft: `${(depth + 1) * 16 + 8}px` }}
                >
                  {t("workspace.empty")}
                </p>
              ) : (
                <TreeRows
                  rows={row.children}
                  depth={depth + 1}
                  onToggle={onToggle}
                  onPreview={onPreview}
                />
              ))}
          </Fragment>
        );
      })}
    </>
  );
}
