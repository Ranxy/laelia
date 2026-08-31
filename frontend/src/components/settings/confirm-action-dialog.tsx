import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";

// ---------------------------------------------------------------------------
// ConfirmActionDialog — the shared destructive-confirm dialog of the settings
// CRUD pages (and reusable wherever an AlertDialog confirm fits). Replaces the
// per-page 20-line AlertDialog block (01-R6b): title/description in, busy
// state on the confirm button, Cancel wired through AlertDialogClose.
// Failure presentation stays with the page (toasts); this dialog is dumb.
// ---------------------------------------------------------------------------

interface ConfirmActionDialogProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  busy: boolean;
  onConfirm: () => void;
  // Defaults keep the delete-confirm wording; override for other confirms.
  confirmLabel?: string;
  busyLabel?: string;
  cancelLabel?: string;
}

export function ConfirmActionDialog(props: ConfirmActionDialogProps) {
  const { open, onClose, title, description, busy, onConfirm } = props;
  const { t } = useTranslation();
  const confirmLabel = props.confirmLabel ?? t("common.delete");
  const busyLabel = props.busyLabel ?? t("common.deleting");
  const cancelLabel = props.cancelLabel ?? t("common.cancel");

  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && onClose()}>
      <AlertDialogContent>
        <AlertDialogTitle>{title}</AlertDialogTitle>
        {description ? (
          <AlertDialogDescription>{description}</AlertDialogDescription>
        ) : undefined}
        <AlertDialogFooter>
          <AlertDialogClose>
            <Button variant="outline" disabled={busy}>
              {cancelLabel}
            </Button>
          </AlertDialogClose>
          <Button variant="destructive" disabled={busy} onClick={onConfirm}>
            {busy ? busyLabel : confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
