import { ArrowLeft, FolderTree, UserCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  MACHINE_ROUTE_PROFILE,
  MACHINE_ROUTE_WORKSPACE,
} from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";
import type { Machine } from "@/types/proto-es/v1/machine_pb";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";

type TabKey = "profile" | "workspace";

export function MachineDetailLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { machineId } = useParams<{ machineId: string }>();
  const machines = useAppStore((s) => s.machines);
  const fetchMachines = useAppStore((s) => s.fetchMachines);
  const getMachine = useAppStore((s) => s.getMachine);
  const [machine, setMachine] = useState<Machine | undefined>(undefined);

  const machineName = `machines/${machineId ?? ""}`;

  // Ensure the roster is loaded on a deep link / hard refresh; the header reads
  // the MachineSummary list (title/status) and falls back to the raw id until it
  // arrives.
  useEffect(() => {
    if (machines.length === 0) {
      void fetchMachines({ pageSize: 100 });
    }
  }, [machines.length, fetchMachines]);

  // Full GetMachine is fetched fresh for the workspace tab gate: canManage is
  // per-caller (machine creator or workspace admin) and must not come from a
  // cached roster.
  useEffect(() => {
    let cancelled = false;
    if (!machineId) return;
    getMachine(machineName).then((m) => {
      if (!cancelled) setMachine(m);
    });
    return () => {
      cancelled = true;
    };
  }, [machineId, machineName, getMachine]);

  const canManage = machine?.canManage === true;

  const displayMachine = machines.find((m) => m.name === machineName);
  const title = displayMachine?.title ?? machineId ?? "";

  const activeTab = useMemo<TabKey>(() => {
    const segments = location.pathname.split("/").filter(Boolean);
    // /machines/:machineId/<tab?>
    const afterId = segments[segments.indexOf(machineId ?? "") + 1];
    if (afterId === "workspace") return "workspace";
    return "profile";
  }, [location.pathname, machineId]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex shrink-0 items-center gap-3 border-b border-control-border px-4 py-2 lg:px-6">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/machines")}
          aria-label={t("machine.back")}
          className="size-8 p-0 lg:hidden"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="truncate text-base font-semibold text-main">{title}</h1>
        {displayMachine?.status?.state ===
          MachineStatus_ConnectionState.ONLINE && (
          <Badge variant="success">{t("machine.status-online")}</Badge>
        )}
      </div>
      <Tabs value={activeTab} className="flex h-full flex-col overflow-hidden">
        <div className="shrink-0 border-b border-control-border">
          <div className="flex items-end gap-2 px-4 pt-2 lg:px-6">
            <TabsList className="gap-x-6 border-b-0!">
              <TabsTrigger
                value="profile"
                className="px-1"
                onClick={() =>
                  navigate(resolvePath(MACHINE_ROUTE_PROFILE, { machineId }))
                }
              >
                <UserCircle className="size-4" />
                {t("machine.tab-profile")}
              </TabsTrigger>
              {canManage && (
                <TabsTrigger
                  value="workspace"
                  className="px-1"
                  onClick={() =>
                    navigate(
                      resolvePath(MACHINE_ROUTE_WORKSPACE, { machineId })
                    )
                  }
                >
                  <FolderTree className="size-4" />
                  {t("machine.tab-workspace")}
                </TabsTrigger>
              )}
            </TabsList>
          </div>
        </div>
        <div className="flex-1 overflow-hidden">
          <Outlet />
        </div>
      </Tabs>
    </div>
  );
}
