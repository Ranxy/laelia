import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceEntry } from "@/types/proto-es/v1/agent_pb";
import { WorkspaceTree } from "./workspace-tree";

const mock = vi.hoisted(() => ({
  listAgentWorkspaceDir: vi.fn(),
}));

vi.mock("@/stores", () => {
  const state = {
    listAgentWorkspaceDir: mock.listAgentWorkspaceDir,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

// The tree only reads identity fields for rendering.
function entry(
  name: string,
  path: string,
  isDirectory: boolean
): WorkspaceEntry {
  return {
    name,
    path,
    isDirectory,
    isHidden: false,
    size: 0n,
  } as unknown as WorkspaceEntry;
}

const TREE: Record<string, WorkspaceEntry[]> = {
  "": [
    entry("src", "src", true),
    entry("README.md", "README.md", false),
    entry("docs", "docs", true),
  ],
  docs: [entry("a.md", "docs/a.md", false), entry("b.md", "docs/b.md", false)],
  src: [
    entry("index.ts", "src/index.ts", false),
    entry("lib", "src/lib", true),
  ],
  "src/lib": [entry("util.ts", "src/lib/util.ts", false)],
};

// virtual-core measures the scroll element via offsetWidth/offsetHeight
// (jsdom has no layout engine, so both read 0 on a stubbed DOM). Give the
// tree a deterministic 280x400 viewport while a test is active.
function mockViewport(height = 400) {
  const width = vi
    .spyOn(HTMLElement.prototype, "offsetWidth", "get")
    .mockReturnValue(280);
  const offsetHeight = vi
    .spyOn(HTMLElement.prototype, "offsetHeight", "get")
    .mockReturnValue(height);
  return () => {
    width.mockRestore();
    offsetHeight.mockRestore();
  };
}

let restoreViewport = () => {};
afterEach(() => restoreViewport());

function renderTree(onPreview: (entry: WorkspaceEntry) => void = vi.fn()) {
  restoreViewport = mockViewport();
  const utils = render(
    <WorkspaceTree agentName="agents/a1" onPreview={onPreview} />
  );
  return { ...utils, onPreview };
}

function item(name: string) {
  return screen.getByRole("treeitem", { name });
}

function searchBox() {
  return screen.getByRole("textbox");
}

beforeEach(() => {
  mock.listAgentWorkspaceDir.mockReset();
  mock.listAgentWorkspaceDir.mockImplementation(
    async (_name: string, dirPath: string) => TREE[dirPath] ?? []
  );
});

describe("WorkspaceTree", () => {
  it("renders root rows and lazy-loads directory children on expand/collapse", async () => {
    renderTree();

    expect(
      await screen.findByRole("treeitem", { name: "src" })
    ).toBeInTheDocument();
    expect(item("README.md")).toBeInTheDocument();
    expect(
      screen.queryByRole("treeitem", { name: "index.ts" })
    ).not.toBeInTheDocument();

    fireEvent.click(item("src"));
    expect(mock.listAgentWorkspaceDir).toHaveBeenCalledWith(
      "agents/a1",
      "src",
      false
    );
    expect(
      await screen.findByRole("treeitem", { name: "index.ts" })
    ).toBeInTheDocument();

    // Collapse hides the children, re-expand reuses the cached lazy load.
    fireEvent.click(item("src"));
    await waitFor(() =>
      expect(
        screen.queryByRole("treeitem", { name: "index.ts" })
      ).not.toBeInTheDocument()
    );
    fireEvent.click(item("src"));
    expect(
      await screen.findByRole("treeitem", { name: "index.ts" })
    ).toBeInTheDocument();
    expect(mock.listAgentWorkspaceDir).toHaveBeenCalledTimes(2); // root + src
  });

  it("exposes tree semantics: roles, levels, expansion, and selection", async () => {
    renderTree();

    const src = await screen.findByRole("treeitem", { name: "src" });
    expect(src).toHaveAttribute("aria-level", "1");
    expect(src).toHaveAttribute("aria-expanded", "false");
    const readme = item("README.md");
    expect(readme).toHaveAttribute("aria-level", "1");
    expect(readme).not.toHaveAttribute("aria-expanded");

    fireEvent.click(src);
    const inner = await screen.findByRole("treeitem", { name: "index.ts" });
    expect(inner).toHaveAttribute("aria-level", "2");
    expect(item("src")).toHaveAttribute("aria-expanded", "true");

    // The tree moves a single visual highlight; aria-selected tracks it and
    // aria-activedescendant exposes it to assistive tech. After expanding
    // src the next flat row is its first child.
    expect(item("src")).toHaveAttribute("aria-selected", "true");
    const tree = screen.getByRole("tree");
    fireEvent.keyDown(tree, { key: "ArrowDown" });
    expect(item("index.ts")).toHaveAttribute("aria-selected", "true");
    expect(item("src")).toHaveAttribute("aria-selected", "false");
    expect(tree).toHaveAttribute(
      "aria-activedescendant",
      "workspace-tree-item-src/index.ts"
    );
  });

  it("opens a file with Enter, matching click semantics", async () => {
    const onPreview = vi.fn();
    const first = renderTree(onPreview);

    const readme = await screen.findByRole("treeitem", { name: "README.md" });
    fireEvent.click(readme);
    expect(onPreview).toHaveBeenCalledWith(
      expect.objectContaining({ path: "README.md" })
    );
    first.unmount();

    // Fresh mount: the highlight starts on the first root row; navigate to
    // README.md (second row) and open it with Enter only.
    renderTree(onPreview);
    await screen.findByRole("treeitem", { name: "README.md" });
    const tree = screen.getByRole("tree");
    fireEvent.keyDown(tree, { key: "ArrowDown" });
    fireEvent.keyDown(tree, { key: "Enter" });
    expect(onPreview).toHaveBeenCalledTimes(2); // click + Enter
    expect(onPreview).toHaveBeenLastCalledWith(
      expect.objectContaining({ path: "README.md" })
    );
  });

  it("expands and collapses directories with ArrowRight/ArrowLeft", async () => {
    renderTree();

    const tree = screen.getByRole("tree");
    await screen.findByRole("treeitem", { name: "src" });
    fireEvent.keyDown(tree, { key: "ArrowRight" });
    expect(
      await screen.findByRole("treeitem", { name: "index.ts" })
    ).toBeInTheDocument();
    fireEvent.keyDown(tree, { key: "ArrowLeft" });
    await waitFor(() =>
      expect(
        screen.queryByRole("treeitem", { name: "index.ts" })
      ).not.toBeInTheDocument()
    );
  });

  it("filters to matching subtrees with the parent chain kept and restores on clear", async () => {
    renderTree();

    await screen.findByRole("treeitem", { name: "src" });
    // Load src and its lib child so the filter has data to walk.
    fireEvent.click(item("src"));
    await screen.findByRole("treeitem", { name: "index.ts" });
    fireEvent.click(item("lib"));
    expect(
      await screen.findByRole("treeitem", { name: "util.ts" })
    ).toBeInTheDocument();

    fireEvent.change(searchBox(), { target: { value: "util" } });

    // Parent chain (src > lib) stays visible and auto-expanded; branches
    // without hits are dropped from the flattened view.
    expect(item("src")).toBeInTheDocument();
    expect(item("lib")).toBeInTheDocument();
    expect(item("util.ts")).toBeInTheDocument();
    expect(
      screen.queryByRole("treeitem", { name: "index.ts" })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("treeitem", { name: "README.md" })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("treeitem", { name: "docs" })
    ).not.toBeInTheDocument();
    expect(item("src")).toHaveAttribute("aria-expanded", "true");
    expect(item("lib")).toHaveAttribute("aria-expanded", "true");

    fireEvent.change(searchBox(), { target: { value: "" } });
    expect(item("README.md")).toBeInTheDocument();
    expect(item("docs")).toBeInTheDocument();
    // Previous expansion states survive the filter round trip.
    expect(item("index.ts")).toBeInTheDocument();
    expect(item("util.ts")).toBeInTheDocument();
  });

  it("shows the no-data hint when no loaded entries match the query", async () => {
    renderTree();
    await screen.findByRole("treeitem", { name: "src" });

    fireEvent.change(searchBox(), { target: { value: "zzz" } });

    expect(screen.getByText("common.no-data")).toBeInTheDocument();
    expect(screen.queryByRole("treeitem")).not.toBeInTheDocument();
  });

  it("virtualizes long lists: only a window of rows mounts and follows scroll", async () => {
    const files = Array.from({ length: 60 }, (_, i) =>
      entry(
        `f${String(i).padStart(2, "0")}.txt`,
        `f${String(i).padStart(2, "0")}.txt`,
        false
      )
    );
    mock.listAgentWorkspaceDir.mockImplementation(async (_name, dirPath) =>
      dirPath ? [] : files
    );
    renderTree();

    expect(
      await screen.findByRole("treeitem", { name: "f00.txt" })
    ).toBeInTheDocument();
    // A 400px viewport at 28px rows plus overscan stays far below 60 rows.
    const visible = screen.getAllByRole("treeitem");
    expect(visible.length).toBeLessThan(40);
    expect(visible.length).toBeGreaterThan(5);
    expect(
      screen.queryByRole("treeitem", { name: "f59.txt" })
    ).not.toBeInTheDocument();

    // Scroll toward the end: the flattened window follows the offset.
    const tree = screen.getByRole("tree");
    tree.scrollTop = 28 * 45;
    fireEvent.scroll(tree);
    expect(
      await screen.findByRole("treeitem", { name: "f45.txt" })
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("treeitem", { name: "f00.txt" })
    ).not.toBeInTheDocument();
  });
});
