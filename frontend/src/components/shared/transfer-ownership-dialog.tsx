import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { describeError } from "@/lib/connect-errors";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// All display strings are resolved by the caller so every i18n key is
// referenced statically at the call sites (see scripts/check-react-i18n.mjs).
interface TransferOwnershipLabels {
  pickerTitle: string;
  pickerDescription: string;
  targetLabel: string;
  targetPlaceholder: string;
  reasonLabel: string;
  reasonPlaceholder: string;
  confirmTitle: string;
  // Second dialog's description, parameterized with the chosen target's
  // resolved display title.
  confirmDescription: (targetTitle: string) => string;
  confirmAction: string;
}

interface TransferOwnershipDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  users: User[];
  // Users already owning the entity are excluded from the target picker.
  excludeUserName?: string;
  labels: TransferOwnershipLabels;
  // The store-backed transfer action; throws on failure so the error renders
  // in-dialog. Page-specific side effects (toast, entity refetch) stay here.
  onTransfer: (target: string, reason: string) => Promise<void>;
}

// Shared two-step ownership transfer flow (agent-profile / machine-profile):
// a first dialog picks the target user (and optional audit reason), then a
// second AlertDialog confirms the risky, unilateral, immediately-effective
// transfer before it is sent.
export function TransferOwnershipDialog({
  open,
  onOpenChange,
  users,
  excludeUserName,
  labels,
  onTransfer,
}: TransferOwnershipDialogProps) {
  const { t } = useTranslation();
  const [target, setTarget] = useState("");
  const [reason, setReason] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Reset the draft target/reason for a fresh run every time the picker
  // opens. Adjusting state during render (the guarded form below, not an
  // effect) so the very first mount of the picker never shows a stale value
  // from the previous flow — the same feel as the callers' old
  // "reset everything, then open" openTransferPicker.
  const [prevOpen, setPrevOpen] = useState(open);
  if (open !== prevOpen) {
    setPrevOpen(open);
    if (open) {
      setTarget("");
      setReason("");
      setError("");
    }
  }

  // resolveTitle maps a users/{id} resource name to the roster's display
  // title, falling back to the raw name so a stale/deleted user never renders
  // empty.
  function resolveTitle(name: string): string {
    return users.find((u) => u.name === name)?.title || name;
  }

  async function handleConfirm() {
    setBusy(true);
    setError("");
    try {
      await onTransfer(target, reason);
      setConfirmOpen(false);
      onOpenChange(false);
    } catch (err) {
      setError(describeError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={(next) => !next && onOpenChange(false)}>
        <DialogContent>
          <DialogTitle>{labels.pickerTitle}</DialogTitle>
          <DialogDescription>{labels.pickerDescription}</DialogDescription>
          <div className="mt-4 flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {labels.targetLabel}
              </label>
              <Select value={target} onValueChange={(v) => v && setTarget(v)}>
                <SelectTrigger>
                  <SelectValue placeholder={labels.targetPlaceholder}>
                    {(v: string | null) => (v ? resolveTitle(v) : "")}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {users
                    .filter((u) => u.name !== excludeUserName)
                    .map((u) => (
                      <SelectItem key={u.name} value={u.name}>
                        {u.title}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium">
                {labels.reasonLabel}
              </label>
              <Input
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder={labels.reasonPlaceholder}
              />
            </div>
            {error && <Alert variant="error" description={error} />}
          </div>
          <div className="mt-6 flex justify-end gap-2">
            <DialogClose>
              <Button variant="outline">{t("common.cancel")}</Button>
            </DialogClose>
            <Button
              disabled={!target}
              onClick={() => {
                setError("");
                onOpenChange(false);
                setConfirmOpen(true);
              }}
            >
              {t("common.next")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(next) => !next && setConfirmOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>{labels.confirmTitle}</AlertDialogTitle>
          <AlertDialogDescription>
            {labels.confirmDescription(resolveTitle(target))}
          </AlertDialogDescription>
          {error && <Alert variant="error" description={error} />}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={busy}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => void handleConfirm()}
            >
              {busy ? t("common.saving") : labels.confirmAction}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
