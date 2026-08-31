import { FolderTree, UserCircle } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import {
  MACHINE_ROUTE_PROFILE,
  MACHINE_ROUTE_WORKSPACE,
} from "@/router/handles";
import { useAppStore } from "@/stores";
import type { Machine } from "@/types/proto-es/v1/machine_pb";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";
import { type DetailTab, DetailTabsLayout } from "./detail-tabs-layout";

export function MachineDetailLayout() {
  const { t } = useTranslation();
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

  const tabs: DetailTab[] = [
    {
      key: "profile",
      icon: UserCircle,
      labelKey: "machine.tab-profile",
      route: MACHINE_ROUTE_PROFILE,
    },
    {
      key: "workspace",
      icon: FolderTree,
      labelKey: "machine.tab-workspace",
      route: MACHINE_ROUTE_WORKSPACE,
      gate: canManage,
    },
  ];

  return (
    <DetailTabsLayout
      idParam="machineId"
      tabs={tabs}
      header={
        <div className="hidden shrink-0 items-center gap-3 border-b border-control-border px-4 py-2 lg:flex lg:px-6">
          <h1 className="truncate text-base font-semibold text-main">
            {title}
          </h1>
          {displayMachine?.status?.state ===
            MachineStatus_ConnectionState.ONLINE && (
            <Badge variant="success">{t("machine.status-online")}</Badge>
          )}
        </div>
      }
    />
  );
}
