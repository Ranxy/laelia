import { useCallback, useRef, useState } from "react";
import { toastManager } from "@/lib/toast";

// ---------------------------------------------------------------------------
// useCrudDialog — the create/edit/delete state machine shared by the settings
// CRUD pages. Owns the three open flags, the (mutually exclusive) busy flag,
// the edit/delete targets, and the submit runner:
//
//   busy guard → run(RPC body) → success toast → close → onChanged(reload)
//
// Validation and payload building stay page-side: the page's form component
// hands its values to a handler that validates and calls runCreate/runSave/
// runDelete with the RPC body. Failure presentation is owned by the page via
// `outcome.onError` (title/description vary per action and page).
//
// Targets are retained after close on purpose: the closing drawer/confirm
// keeps its title for the ~200ms close animation instead of flickering to a
// blank name (01-B14). A new openEdit/openDelete replaces them; create
// renders nothing target-dependent (its form mounts fresh), so retention
// cannot leak edit data into create (01-B4).
// ---------------------------------------------------------------------------

type SubmitKind = "create" | "save" | "delete";

export interface SubmitOutcomeOptions {
  // Translated title toasted on success ("created"). Omit for silent success.
  successTitle?: string;
  // Failure presentation is owned by the page — a submit that fails silently
  // is exactly the bug class this hook exists to prevent, so pages must pass
  // this for user-initiated submits (usually showErrorToast/toastManager).
  onError?: (err: unknown) => void;
}

export interface CrudDialog<TTarget> {
  createOpen: boolean;
  editOpen: boolean;
  deleteOpen: boolean;
  editTarget: TTarget | null;
  deleteTarget: TTarget | null;
  creating: boolean;
  saving: boolean;
  deleting: boolean;
  openCreate(): void;
  openEdit(target: TTarget): void;
  openDelete(target: TTarget): void;
  closeCreate(): void;
  closeEdit(): void;
  closeDelete(): void;
  /** True when the body ran to completion; false on busy or failure. */
  runCreate(
    run: () => Promise<void>,
    outcome?: SubmitOutcomeOptions
  ): Promise<boolean>;
  runSave(
    run: () => Promise<void>,
    outcome?: SubmitOutcomeOptions
  ): Promise<boolean>;
  runDelete(
    run: () => Promise<void>,
    outcome?: SubmitOutcomeOptions
  ): Promise<boolean>;
}

export function useCrudDialog<TTarget>(options?: {
  // Fired after a successful create/save/delete (typically: reload the list,
  // invalidate affected caches). Kept in a ref — callers close over query
  // hooks whose identity changes every render.
  onChanged?: () => void;
}): CrudDialog<TTarget> {
  const onChangedRef = useRef(options?.onChanged);
  onChangedRef.current = options?.onChanged;

  const [createOpen, setCreateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<TTarget | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TTarget | null>(null);
  const [busy, setBusy] = useState<SubmitKind | null>(null);
  // Ref mirror of `busy`: the guard must be synchronous so two rapid clicks
  // can never both pass it before the first state update lands.
  const busyRef = useRef<SubmitKind | null>(null);

  const closeSheetFor = (kind: SubmitKind) => {
    if (kind === "create") setCreateOpen(false);
    else if (kind === "save") setEditOpen(false);
    else setDeleteOpen(false);
  };

  const runSubmit = useCallback(
    async (
      kind: SubmitKind,
      run: () => Promise<void>,
      outcome?: SubmitOutcomeOptions
    ): Promise<boolean> => {
      if (busyRef.current) return false;
      busyRef.current = kind;
      setBusy(kind);
      try {
        await run();
        if (outcome?.successTitle) {
          toastManager.add({
            type: "success",
            title: outcome.successTitle,
          });
        }
        closeSheetFor(kind);
        onChangedRef.current?.();
        return true;
      } catch (err) {
        outcome?.onError?.(err);
        return false;
      } finally {
        busyRef.current = null;
        setBusy(null);
      }
    },
    []
  );

  return {
    createOpen,
    editOpen,
    deleteOpen,
    editTarget,
    deleteTarget,
    creating: busy === "create",
    saving: busy === "save",
    deleting: busy === "delete",
    openCreate: () => setCreateOpen(true),
    openEdit: (target) => {
      setEditTarget(target);
      setEditOpen(true);
    },
    openDelete: (target) => {
      setDeleteTarget(target);
      setDeleteOpen(true);
    },
    closeCreate: () => setCreateOpen(false),
    closeEdit: () => setEditOpen(false),
    closeDelete: () => setDeleteOpen(false),
    runCreate: (run, outcome) => runSubmit("create", run, outcome),
    runSave: (run, outcome) => runSubmit("save", run, outcome),
    runDelete: (run, outcome) => runSubmit("delete", run, outcome),
  };
}
