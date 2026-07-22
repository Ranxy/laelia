import { AlertTriangle } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { settingServiceClient } from "@/connect";
import { SETTINGS_ROUTE_STORAGE } from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useHasPermission } from "@/stores/auth";

// Frontend registry: setup item id -> presentation. Mirrors the backend
// SettingService.setupChecks. Add an entry here when a new required-config
// item is added backend-side; the dialog then surfaces it automatically.
const SETUP_ITEMS: Record<
  string,
  { titleKey: string; descriptionKey: string; route: string }
> = {
  s3: {
    titleKey: "setup.item.s3.title",
    descriptionKey: "setup.item.s3.description",
    route: SETTINGS_ROUTE_STORAGE,
  },
};

interface PendingItem {
  id: string;
  titleKey: string;
  descriptionKey: string;
  route: string;
}

export function SetupChecklistDialog() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const isAdmin = useHasPermission("laelia.settings.get");

  const [pending, setPending] = useState<PendingItem[]>([]);
  // Per-session dismiss: survives in-session navigation, resets on the next
  // login (fresh DashboardLayout mount). See dashboard-layout.tsx.
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    if (!isAdmin) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await settingServiceClient.getSetupStatus({});
        if (cancelled) return;
        const items: PendingItem[] = (res.items ?? [])
          .filter((it) => !it.configured && SETUP_ITEMS[it.id])
          .map((it) => ({ id: it.id, ...SETUP_ITEMS[it.id] }));
        if (!cancelled) setPending(items);
      } catch (err) {
        // Onboarding must never block the dashboard on a transient failure.
        console.warn("failed to load setup status", err);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [isAdmin]);

  const open = isAdmin && !dismissed && pending.length > 0;

  function configure(item: PendingItem) {
    setDismissed(true);
    navigate(resolvePath(item.route));
  }

  function remindLater() {
    setDismissed(true);
  }

  if (!open) return null;

  return (
    <Dialog open onOpenChange={(next) => !next && remindLater()}>
      <DialogContent className="max-w-lg">
        <div className="flex items-start gap-3">
          <AlertTriangle className="mt-0.5 size-5 shrink-0 text-warning" />
          <div className="flex-1">
            <DialogTitle>{t("setup.dialog.title")}</DialogTitle>
            <DialogDescription>
              {t("setup.dialog.description")}
            </DialogDescription>
          </div>
        </div>
        <ul className="mt-4 space-y-3">
          {pending.map((item) => (
            <li
              key={item.id}
              className="flex flex-col gap-2 rounded-md border border-control-border p-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <div>
                <p className="text-sm font-medium text-main">
                  {t(item.titleKey)}
                </p>
                <p className="mt-0.5 text-xs text-control-light">
                  {t(item.descriptionKey)}
                </p>
              </div>
              <Button size="sm" onClick={() => configure(item)}>
                {t("setup.action.configure")}
              </Button>
            </li>
          ))}
        </ul>
        <div className="mt-4 flex justify-end">
          <Button variant="ghost" size="sm" onClick={remindLater}>
            {t("setup.action.remind-later")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
