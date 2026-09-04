import { ArrowLeft, Settings2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { MachineConnectionBadge } from "@/components/machine-connection-badge";
import { ProvisioningPhaseBadge } from "@/components/provisioning-phase-badge";
import {
  PageLoading,
  PermissionNotice,
  SettingsPage,
} from "@/components/settings-page";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { machineServiceClient } from "@/connect";
import { usePolling } from "@/hooks/use-polling";
import { useResourceQuery } from "@/hooks/use-resource-query";
import { provisioningActive } from "@/lib/provisioning-status";
import { formatTimestamp } from "@/lib/time-format";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";
import { ProvisioningPhase } from "@/types/proto-es/v1/machine_pb";
import type { Provisioner } from "@/types/proto-es/v1/provisioner_pb";
import { ProvisionerAccessCard } from "./settings-provisioner-access";

// SettingsProvisionerDetailPage is the detail view reachable from the
// provisioners settings table. It shows the provisioner's basic information
// and the list of machines it created (and manages the workload of).
export function SettingsProvisionerDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canGet = useHasPermission("laelia.provisioners.get");
  const canManageIam = useHasPermission("laelia.provisioners.delete");
  const { provisionerId } = useParams<{ provisionerId: string }>();
  const name = `provisioners/${provisionerId ?? ""}`;

  const [provisioner, setProvisioner] = useState<Provisioner | undefined>();
  const [loading, setLoading] = useState(true);

  // Re-fetch the full provisioner on every open/refresh: status (connected,
  // auto-upgrade) and machineCount are per-moment and must not go stale.
  useEffect(() => {
    if (!canGet) return;
    let cancelled = false;
    setLoading(true);
    void (async () => {
      try {
        const p = await useAppStore.getState().getProvisioner(name);
        if (!cancelled) setProvisioner(p);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [canGet, name]);

  const machinesQuery = useResourceQuery<MachineSummary>({
    enabled: canGet && Boolean(provisionerId),
    queryKey: ["settings", "provisioners", name, "machines"],
    queryFn: async (signal) =>
      (
        await machineServiceClient.listMachines(
          {
            pageSize: 100,
            pageToken: "",
            provisioner: name,
          },
          { signal }
        )
      ).machines ?? [],
    failureTitle: t("settings.provisioner-detail.machines-load-failed"),
  });

  // Keep polling while any machine's provisioning job is still moving so the
  // list flips to ONLINE / terminal phases without a manual refresh.
  const anyProvisioning = machinesQuery.items.some(
    (m) =>
      m.provisioning?.phase != null && provisioningActive(m.provisioning.phase)
  );
  usePolling(() => machinesQuery.reload(), 10000, { enabled: anyProvisioning });

  if (!canGet) {
    return (
      <PermissionNotice message={t("settings.provisioners.not-allowed")} />
    );
  }

  return (
    <SettingsPage
      title={
        <span className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            className="-ml-2"
            aria-label={t("settings.provisioner-detail.back")}
            title={t("settings.provisioner-detail.back")}
            onClick={() => navigate("/settings/provisioners")}
          >
            <ArrowLeft className="size-4" />
          </Button>
          {t("settings.provisioner-detail.title")}
        </span>
      }
      description={t("settings.provisioner-detail.description")}
      contentWidth="mx-auto w-full max-w-3xl"
    >
      {loading ? (
        <PageLoading />
      ) : !provisioner ? (
        <Alert
          variant="error"
          description={t("settings.provisioner-detail.not-found")}
        />
      ) : (
        <div className="flex flex-col gap-5">
          <div className="rounded-lg border border-control-border bg-background p-5 shadow-xs">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold text-main">
                {provisioner.title}
              </h2>
              <Badge variant="secondary">{provisioner.backend}</Badge>
              {provisioner.status?.connected ? (
                <Badge variant="success">
                  {t("settings.provisioners.status-connected")}
                </Badge>
              ) : (
                <Badge variant="default">
                  {t("settings.provisioners.status-offline")}
                </Badge>
              )}
            </div>
            {provisioner.description && (
              <p className="mt-1 text-sm text-control-light">
                {provisioner.description}
              </p>
            )}
            <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-4">
              <Detail
                label={t("settings.provisioners.header-version")}
                value={provisioner.status?.version ?? "-"}
              />
              <Detail
                label={t("settings.provisioners.header-machines")}
                value={String(provisioner.machineCount)}
              />
              <Detail
                label={t("settings.provisioners.header-created")}
                value={
                  provisioner.createdAt
                    ? formatTimestamp(provisioner.createdAt)
                    : "-"
                }
              />
              <Detail
                label={t("settings.provisioner-detail.auto-upgrade")}
                value={
                  provisioner.status?.autoUpgrade
                    ? t("common.yes")
                    : t("common.no")
                }
              />
            </dl>
          </div>

          <ProvisionerAccessCard
            name={name}
            title={provisioner.title}
            canManage={canManageIam}
          />

          <div className="rounded-lg border border-control-border bg-background shadow-xs">
            <div className="flex items-center justify-between border-b border-control-border px-5 py-4">
              <h3 className="text-sm font-semibold text-main">
                {t("settings.provisioner-detail.machines-title")}
              </h3>
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  navigate(
                    `/settings/provisioners/${provisionerId ?? ""}/cleanup`
                  )
                }
              >
                <Settings2 className="size-4" />
                {t("settings.provisioner-detail.cleanup-guide")}
              </Button>
            </div>
            {machinesQuery.initialLoading ? (
              <div className="p-6">
                <PageLoading />
              </div>
            ) : machinesQuery.items.length === 0 ? (
              <p className="px-5 py-8 text-center text-sm text-control-light">
                {t("settings.provisioner-detail.no-machines")}
              </p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      {t("settings.provisioner-detail.header-machine")}
                    </TableHead>
                    <TableHead>
                      {t("settings.provisioners.header-status")}
                    </TableHead>
                    <TableHead>
                      {t("settings.provisioner-detail.header-provisioning")}
                    </TableHead>
                    <TableHead>
                      {t("settings.provisioners.header-created")}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {machinesQuery.items.map((m) => {
                    const resourceId = m.name.replace(/^machines\//, "");
                    return (
                      <TableRow
                        key={m.name}
                        className="cursor-pointer"
                        onClick={() => navigate(`/machines/${resourceId}`)}
                      >
                        <TableCell className="font-medium text-main">
                          {m.title}
                        </TableCell>
                        <TableCell>
                          <MachineConnectionBadge state={m.status?.state} />
                        </TableCell>
                        <TableCell>
                          {m.provisioning?.phase != null &&
                          m.provisioning.phase !==
                            ProvisioningPhase.UNSPECIFIED ? (
                            <ProvisioningPhaseBadge
                              phase={m.provisioning.phase}
                            />
                          ) : (
                            "-"
                          )}
                        </TableCell>
                        <TableCell>
                          {m.createdAt ? formatTimestamp(m.createdAt) : "-"}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )}
          </div>
        </div>
      )}
    </SettingsPage>
  );
}

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-control-light">{label}</dt>
      <dd className="truncate font-medium text-main">{value}</dd>
    </div>
  );
}
