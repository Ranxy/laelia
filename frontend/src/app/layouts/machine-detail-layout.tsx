import { ArrowLeft } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/stores";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";

export function MachineDetailLayout() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { machineId } = useParams<{ machineId: string }>();
  const machines = useAppStore((s) => s.machines);
  const fetchMachines = useAppStore((s) => s.fetchMachines);

  const machineName = `machines/${machineId ?? ""}`;

  // Ensure the roster is loaded on a deep link / hard refresh; the header reads
  // the MachineSummary list (title/status) and falls back to the raw id until it
  // arrives.
  useEffect(() => {
    if (machines.length === 0) {
      void fetchMachines({ pageSize: 100 });
    }
  }, [machines.length, fetchMachines]);

  const displayMachine = machines.find((m) => m.name === machineName);
  const title = displayMachine?.title ?? machineId ?? "";

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex items-center gap-3 border-b border-control-border px-4 py-3 shrink-0 lg:px-6">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/machines")}
          aria-label={t("machine.back")}
          className="size-8 p-0 lg:hidden"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="text-base font-semibold text-main truncate">{title}</h1>
        {displayMachine?.status?.state ===
          MachineStatus_ConnectionState.ONLINE && (
          <Badge variant="success">{t("machine.status-online")}</Badge>
        )}
      </div>
      <div className="flex-1 overflow-hidden">
        <Outlet />
      </div>
    </div>
  );
}
