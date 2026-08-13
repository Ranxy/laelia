import { Check, Copy, Loader2, Monitor, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Card, Field } from "@/components/profile-common";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { buildMachineSetupCommand } from "@/lib/machine-token";
import { useAppStore } from "@/stores";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";

// MachineNewPage is the create-machine waiting page. It no longer creates a
// machine directly: it shows the `laelia-machine setup` command, then watches
// ListMachines for a machine that was created by the current user after this
// page opened (i.e. the machine the user just approved on the device-login
// page). When one appears, the user confirms/renames it and is taken to its
// profile.
export function MachineNewPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const fetchMachines = useAppStore((s) => s.fetchMachines);
  const machines = useAppStore((s) => s.machines);
  const currentUser = useAppStore((s) => s.currentUser);
  const getMachine = useAppStore((s) => s.getMachine);
  const updateMachine = useAppStore((s) => s.updateMachine);

  // pageOpenTime anchors "new": only machines created after this page opened
  // and by the current user count as the machine being set up right now.
  const pageOpenTime = useRef(Date.now());
  const [copied, setCopied] = useState(false);
  const [candidate, setCandidate] = useState<MachineSummary | undefined>(
    undefined
  );
  const [candidateInfo, setCandidateInfo] = useState<{
    hostname: string;
    os: string;
    arch: string;
    ip: string;
  } | null>(null);
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const command = buildMachineSetupCommand();

  const poll = useCallback(async () => {
    if (candidate) return;
    await fetchMachines({ pageSize: 100 }, { silent: true });
  }, [candidate, fetchMachines]);

  useEffect(() => {
    void poll();
    const id = setInterval(() => void poll(), 5000);
    return () => clearInterval(id);
  }, [poll]);

  // Detect the freshly approved machine. The full Machine (with host info) is
  // fetched on demand since MachineSummary only carries identity/status.
  useEffect(() => {
    if (candidate || !currentUser) return;
    const fresh = machines.find(
      (m) =>
        m.createdBy === currentUser.name &&
        m.createdAt &&
        Number(m.createdAt.seconds) * 1000 > pageOpenTime.current
    );
    if (!fresh) return;
    setCandidate(fresh);
    setName(fresh.title);
    void getMachine(fresh.name).then((machine) => {
      const info = machine?.info;
      if (info) {
        setCandidateInfo({
          hostname: info.hostname,
          os: info.os,
          arch: info.arch,
          ip: info.ip,
        });
      }
    });
  }, [candidate, currentUser, machines, getMachine]);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard unavailable; the command is visible for manual copy.
    }
  }

  async function handleConfirm() {
    if (!candidate) return;
    setSaving(true);
    setError("");
    try {
      const title = name.trim() || candidate.title;
      await updateMachine(candidate.name, title);
      navigate(`/machines/${candidate.name.replace(/^machines\//, "")}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto flex w-full max-w-2xl flex-col gap-6">
        <div>
          <h1 className="text-lg font-semibold text-main">
            {t("machine.new.title")}
          </h1>
          <p className="mt-1 text-sm text-control-light">
            {t("machine.new.description")}
          </p>
        </div>

        <Card title={t("machine.new.step-command")}>
          <div className="flex flex-col gap-3">
            <p className="text-sm text-control-light">
              {t("machine.new.command-hint")}
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 rounded bg-white border border-control-border px-3 py-2 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
                {command}
              </code>
              <Button
                variant="outline"
                size="sm"
                onClick={() => void handleCopy()}
              >
                {copied ? (
                  <Check className="size-4 text-success" />
                ) : (
                  <Copy className="size-4" />
                )}
                {copied ? t("common.copied") : t("common.copy")}
              </Button>
            </div>
          </div>
        </Card>

        <Card title={t("machine.new.step-wait")}>
          {candidate ? (
            <div className="flex flex-col gap-4">
              <div className="flex items-center gap-2">
                <Monitor className="size-5 text-control" />
                <span className="text-sm font-medium text-main">
                  {candidateInfo?.hostname || candidate.title}
                </span>
              </div>
              {candidateInfo && (
                <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
                  <Field label={t("machine.detail-hostname")}>
                    {candidateInfo.hostname}
                  </Field>
                  <Field label={t("machine.detail-os")}>
                    {candidateInfo.os}
                    {candidateInfo.arch ? ` · ${candidateInfo.arch}` : ""}
                  </Field>
                  <Field label={t("machine.detail-ip")}>
                    {candidateInfo.ip || "-"}
                  </Field>
                </dl>
              )}
              <div className="flex flex-col gap-1">
                <label
                  htmlFor="machine-new-name"
                  className="text-sm font-medium text-control"
                >
                  {t("machine.new.name-label")}
                </label>
                <Input
                  id="machine-new-name"
                  value={name}
                  placeholder={t("machine.new.name-placeholder")}
                  onChange={(e) => {
                    setName(e.target.value);
                    setError("");
                  }}
                />
              </div>
              {error && <p className="text-xs text-danger">{error}</p>}
              <div className="flex items-center justify-end gap-2">
                <Button
                  variant="outline"
                  onClick={() => {
                    setCandidate(undefined);
                    setCandidateInfo(null);
                  }}
                >
                  <X className="size-4" />
                  {t("machine.new.not-mine")}
                </Button>
                <Button disabled={saving} onClick={() => void handleConfirm()}>
                  {saving && <Loader2 className="size-4 animate-spin" />}
                  {t("machine.new.confirm")}
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex flex-col items-center gap-3 py-6 text-center">
              <Loader2 className="size-6 animate-spin text-control-light" />
              <p className="text-sm text-control-light">
                {t("machine.new.waiting")}
              </p>
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}
