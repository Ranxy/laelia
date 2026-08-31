import { useVirtualizer } from "@tanstack/react-virtual";
import {
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { SearchInput } from "@/components/ui/search-input";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { WorkspaceEntry } from "@/types/proto-es/v1/agent_pb";

// Fixed row height: the virtualizer assumes one constant size for every row.
const ROW_HEIGHT = 28;
const DEPTH_STEP = 16;
const BASE_INDENT = 8;
const OVERSCAN = 8;

// DirNode is one lazily loaded directory: children === null means the children
// have not been fetched yet. Nodes live in a flat map keyed by path, so a
// toggle updates a single map entry instead of remapping a nested tree.
interface DirNode {
  entry: WorkspaceEntry;
  children: WorkspaceEntry[] | null;
  loading: boolean;
}

// FlatRow is one row of the flattened, expanded-only view. Placeholder rows
// carry the loading / empty-directory hints that used to render inline.
type FlatRow =
  | {
      kind: "entry";
      key: string;
      entry: WorkspaceEntry;
      depth: number;
      // Whether the branch is open (forced on while name filtering).
      expanded: boolean;
      // Whether this directory is currently fetching its children.
      loading: boolean;
    }
  | { kind: "loading" | "empty"; key: string; depth: number };

interface WorkspaceTreeProps {
  agentName: string;
  onPreview: (entry: WorkspaceEntry) => void;
}

// WorkspaceTree renders a lazily loaded file tree of an agent's workspace.
// Expanded paths are flattened into a fixed-height row array and rendered
// through @tanstack/react-virtual, so large workspaces only mount the visible
// window. Directories load one level at a time when expanded; the "show hidden
// files" toggle re-fetches the tree from the root; the search input narrows the
// loaded tree to matching names (parent chain kept). Server-side filtering
// (node_modules, never-visible paths, secret handling) is applied on the
// machine.
export function WorkspaceTree({ agentName, onPreview }: WorkspaceTreeProps) {
  const { t } = useTranslation();
  const listAgentWorkspaceDir = useAppStore((s) => s.listAgentWorkspaceDir);
  const [includeHidden, setIncludeHidden] = useState(false);
  const [query, setQuery] = useState("");
  const [roots, setRoots] = useState<WorkspaceEntry[]>([]);
  const [dirNodes, setDirNodes] = useState<Map<string, DirNode>>(new Map());
  const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());
  const [rootLoading, setRootLoading] = useState(true);
  const [rootError, setRootError] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const [activeIndex, setActiveIndex] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);

  const loadDir = useCallback(
    async (dirPath: string): Promise<WorkspaceEntry[]> =>
      listAgentWorkspaceDir(agentName, dirPath, includeHidden),
    [agentName, includeHidden, listAgentWorkspaceDir]
  );

  useEffect(() => {
    let cancelled = false;
    setRootLoading(true);
    setRootError(false);
    setDirNodes(new Map());
    setExpandedPaths(new Set());
    loadDir("")
      .then((loaded) => {
        if (!cancelled) setRoots(loaded);
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

  // updateDirNode patches a single node map entry (copy-on-write): toggles no
  // longer remap every node object of the tree, and flattening re-derives the
  // visible rows from roots + expandedPaths + this map.
  const updateDirNode = useCallback(
    (
      entry: WorkspaceEntry,
      patch: Partial<Pick<DirNode, "children" | "loading">>
    ) => {
      setDirNodes((prev) => {
        const cur = prev.get(entry.path);
        const next = new Map(prev);
        next.set(entry.path, {
          entry,
          children:
            patch.children !== undefined
              ? patch.children
              : (cur?.children ?? null),
          loading: patch.loading ?? cur?.loading ?? false,
        });
        return next;
      });
    },
    []
  );

  const toggleDir = useCallback(
    (entry: WorkspaceEntry) => {
      if (!entry.isDirectory) {
        onPreview(entry);
        return;
      }
      if (expandedPaths.has(entry.path)) {
        setExpandedPaths((prev) => {
          if (!prev.has(entry.path)) return prev;
          const next = new Set(prev);
          next.delete(entry.path);
          return next;
        });
        return;
      }
      setExpandedPaths((prev) => {
        if (prev.has(entry.path)) return prev;
        const next = new Set(prev);
        next.add(entry.path);
        return next;
      });
      const node = dirNodes.get(entry.path);
      if (node?.loading) return;
      if (node?.children !== null && node?.children) return;
      updateDirNode(entry, { loading: true });
      loadDir(entry.path)
        .then((children) => updateDirNode(entry, { children, loading: false }))
        .catch(() => updateDirNode(entry, { children: [], loading: false }));
    },
    [expandedPaths, dirNodes, loadDir, onPreview, updateDirNode]
  );

  const q = query.trim().toLowerCase();
  const filtering = q.length > 0;

  // nameMatches reports whether the entry or any of its loaded descendants
  // matches the query; only loaded data can be filtered (the tree is lazy).
  const nameMatches = useCallback(
    (entry: WorkspaceEntry): boolean => {
      if (entry.name.toLowerCase().includes(q)) return true;
      if (!entry.isDirectory) return false;
      const node = dirNodes.get(entry.path);
      if (!node || node.children === null) return false;
      return node.children.some(nameMatches);
    },
    [q, dirNodes]
  );

  const flatRows = useMemo(() => {
    const rows: FlatRow[] = [];
    const walk = (entries: WorkspaceEntry[], depth: number) => {
      for (const entry of entries) {
        if (filtering && !nameMatches(entry)) continue;
        const node = entry.isDirectory ? dirNodes.get(entry.path) : undefined;
        const expanded = entry.isDirectory
          ? filtering || expandedPaths.has(entry.path)
          : false;
        rows.push({
          kind: "entry",
          key: entry.path,
          entry,
          depth,
          expanded,
          loading: node?.loading ?? false,
        });
        if (!entry.isDirectory || !expanded) continue;
        if (filtering) {
          // While filtering, kept directories auto-expand; unloaded branches
          // simply render without placeholder rows.
          const children = node?.children;
          if (children && children.length > 0) walk(children, depth + 1);
        } else if (!node || node.children === null || node.loading) {
          rows.push({
            kind: "loading",
            key: `${entry.path}:loading`,
            depth: depth + 1,
          });
        } else if (node.children.length === 0) {
          rows.push({
            kind: "empty",
            key: `${entry.path}:empty`,
            depth: depth + 1,
          });
        } else {
          walk(node.children, depth + 1);
        }
      }
    };
    walk(roots, 0);
    return rows;
  }, [roots, dirNodes, expandedPaths, filtering, nameMatches]);

  // The keyboard highlight lands only on interactive rows; placeholder rows
  // are skipped while stepping, and the stored index is clamped at read time.
  const clampedActive = flatRows.length
    ? Math.min(activeIndex, flatRows.length - 1)
    : 0;
  const candidateRow = flatRows[clampedActive];
  const activeRow =
    candidateRow && candidateRow.kind === "entry" ? candidateRow : undefined;

  // Keep the highlighted row visible while navigating with the keyboard.
  // jsdom polyfills scrollIntoView as a no-op in the test setup.
  useEffect(() => {
    if (!activeRow) return;
    document
      .getElementById(workspaceItemId(activeRow.entry.path))
      ?.scrollIntoView({ block: "nearest" });
  }, [activeRow]);

  function stepIndex(from: number, delta: 1 | -1): number {
    let i = from + delta;
    while (i >= 0 && i < flatRows.length && flatRows[i].kind !== "entry") {
      i += delta;
    }
    return i >= 0 && i < flatRows.length ? i : clampedActive;
  }

  function activateFlatRow(row: FlatRow) {
    if (row.kind !== "entry") return;
    setActiveIndex(flatRows.indexOf(row));
    if (!row.entry.isDirectory) {
      onPreview(row.entry);
      return;
    }
    toggleDir(row.entry);
  }

  function onKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (flatRows.length === 0) return;
    const row = flatRows[clampedActive];
    switch (e.key) {
      case "ArrowDown": {
        e.preventDefault();
        setActiveIndex(stepIndex(clampedActive, 1));
        break;
      }
      case "ArrowUp": {
        e.preventDefault();
        setActiveIndex(stepIndex(clampedActive, -1));
        break;
      }
      case "Enter": {
        if (row.kind !== "entry") return;
        e.preventDefault();
        activateFlatRow(row);
        break;
      }
      // Arrow keys walk the outline like a native tree: right expands a
      // collapsed branch, left collapses an expanded one.
      case "ArrowRight":
      case "ArrowLeft": {
        if (row.kind !== "entry" || !row.entry.isDirectory) return;
        if (e.key === "ArrowRight" ? row.expanded : !row.expanded) return;
        e.preventDefault();
        activateFlatRow(row);
        break;
      }
    }
  }

  function reload() {
    setReloadKey((k) => k + 1);
  }

  const virtualizer = useVirtualizer({
    count: flatRows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: OVERSCAN,
    getItemKey: (index) => flatRows[index].key,
  });

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
      <div className="shrink-0 border-b border-control-border px-3 py-2">
        <SearchInput
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label={t("common.type-to-search")}
        />
      </div>
      <div
        ref={scrollRef}
        role="tree"
        tabIndex={0}
        onKeyDown={onKeyDown}
        aria-activedescendant={
          activeRow ? workspaceItemId(activeRow.entry.path) : undefined
        }
        className="flex-1 overflow-auto p-2"
      >
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
        ) : flatRows.length === 0 ? (
          <p className="p-2 text-sm text-control-light">
            {filtering ? t("common.no-data") : t("workspace.empty")}
          </p>
        ) : (
          <div
            className="relative"
            style={{ height: virtualizer.getTotalSize() }}
          >
            {virtualizer.getVirtualItems().map((virtualItem) => {
              const row = flatRows[virtualItem.index];
              if (row.kind !== "entry") {
                return (
                  <div
                    key={virtualItem.key}
                    role="presentation"
                    className="absolute left-0 top-0 flex w-full items-center gap-2 font-mono text-xs text-control-light"
                    style={{
                      height: virtualItem.size,
                      transform: `translateY(${virtualItem.start}px)`,
                      paddingLeft: row.depth * DEPTH_STEP + BASE_INDENT,
                    }}
                  >
                    {row.kind === "loading" ? (
                      <>
                        <Loader2 className="size-3 animate-spin" />
                        {t("workspace.loading")}
                      </>
                    ) : (
                      t("workspace.empty")
                    )}
                  </div>
                );
              }
              const { entry } = row;
              const isActive = row === activeRow;
              return (
                <div
                  key={virtualItem.key}
                  id={workspaceItemId(entry.path)}
                  role="treeitem"
                  tabIndex={-1}
                  aria-level={row.depth + 1}
                  aria-selected={isActive}
                  aria-expanded={entry.isDirectory ? row.expanded : undefined}
                  onClick={() => activateFlatRow(row)}
                  className={cn(
                    "absolute left-0 top-0 flex w-full cursor-pointer items-center gap-1.5 rounded-sm px-2 text-left font-mono text-sm hover:bg-control-bg",
                    entry.isHidden && "opacity-60",
                    isActive && "bg-control-bg"
                  )}
                  style={{
                    height: virtualItem.size,
                    transform: `translateY(${virtualItem.start}px)`,
                    paddingLeft: row.depth * DEPTH_STEP + BASE_INDENT,
                  }}
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
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function workspaceItemId(path: string): string {
  return `workspace-tree-item-${path}`;
}
