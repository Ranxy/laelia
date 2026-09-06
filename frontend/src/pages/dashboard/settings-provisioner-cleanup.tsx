import { ArrowLeft, Terminal } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router-dom";
import { CopyableCommand } from "@/components/copyable-command";
import { PermissionNotice, SettingsPage } from "@/components/settings-page";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useHasPermission } from "@/stores/permissions";

// SettingsProvisionerCleanupPage is the full-page cleanup guide shown after a
// provisioner is deleted. Deleting a provisioner only removes its registry
// entry on the manager; the operator it ran in the user's Kubernetes cluster
// is NOT removed automatically. The operator scales its own Deployment to 0
// (so it stops crash-looping), but the Deployment/CRD/RBAC/namespace remain
// and must be removed manually with the kubectl steps below.
export function SettingsProvisionerCleanupPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const canGet = useHasPermission("laelia.provisioners.get");
  const location = useLocation();
  const title = (location.state as { title?: string } | null)?.title;

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
            aria-label={t("settings.provisioner-cleanup.back")}
            title={t("settings.provisioner-cleanup.back")}
            onClick={() => navigate("/settings/provisioners")}
          >
            <ArrowLeft className="size-4" />
          </Button>
          {t("settings.provisioner-cleanup.title")}
        </span>
      }
      description={t("settings.provisioner-cleanup.description", {
        title: title ?? "",
      })}
      contentWidth="mx-auto w-full max-w-3xl"
    >
      <div className="flex flex-col gap-5">
        <Alert
          variant="warning"
          description={t("settings.provisioner-cleanup.scaled-note")}
        />

        <HelmNote
          title={t("settings.provisioner-cleanup.helm-title")}
          description={t("settings.provisioner-cleanup.helm-description")}
          commands={[
            t("settings.provisioner-cleanup.helm-command-1"),
            t("settings.provisioner-cleanup.helm-command-2"),
          ]}
        />

        <CleanupStep
          step={1}
          title={t("settings.provisioner-cleanup.step-1-title")}
          description={t("settings.provisioner-cleanup.step-1-description")}
          commands={[t("settings.provisioner-cleanup.step-1-command")]}
        />
        <CleanupStep
          step={2}
          title={t("settings.provisioner-cleanup.step-2-title")}
          description={t("settings.provisioner-cleanup.step-2-description")}
          commands={[t("settings.provisioner-cleanup.step-2-command")]}
        />
        <CleanupStep
          step={3}
          title={t("settings.provisioner-cleanup.step-3-title")}
          description={t("settings.provisioner-cleanup.step-3-description")}
          commands={[t("settings.provisioner-cleanup.step-3-command")]}
        />
        <CleanupStep
          step={4}
          title={t("settings.provisioner-cleanup.step-4-title")}
          description={t("settings.provisioner-cleanup.step-4-description")}
          commands={[
            t("settings.provisioner-cleanup.step-4-command-1"),
            t("settings.provisioner-cleanup.step-4-command-2"),
          ]}
        />

        <div className="flex items-start gap-2 rounded-lg border border-control-border bg-background p-4 text-sm text-control">
          <Terminal className="mt-0.5 size-4 shrink-0 text-control-light" />
          <p>{t("settings.provisioner-cleanup.note")}</p>
        </div>
      </div>
    </SettingsPage>
  );
}

// CleanupStep renders one numbered step: a title, an explanation, and one or
// more copyable kubectl commands.
function CleanupStep({
  step,
  title,
  description,
  commands,
}: {
  step: number;
  title: string;
  description: string;
  commands: string[];
}) {
  return (
    <div className="rounded-lg border border-control-border bg-background p-5 shadow-xs">
      <div className="flex items-center gap-3">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent text-xs font-semibold text-accent-foreground">
          {step}
        </span>
        <h3 className="text-sm font-semibold text-main">{title}</h3>
      </div>
      <p className="mt-2 text-sm text-control-light">{description}</p>
      <div className="mt-3 flex flex-col gap-2">
        {commands.map((command) => (
          <CopyCommand key={command} command={command} />
        ))}
      </div>
    </div>
  );
}

// HelmNote is the alternative cleanup path for the Helm chart: uninstall the
// release, then delete the CRD by hand (Helm does not remove crds/ objects).
function HelmNote({
  title,
  description,
  commands,
}: {
  title: string;
  description: string;
  commands: string[];
}) {
  return (
    <div className="rounded-lg border border-control-border bg-background p-5 shadow-xs">
      <div className="flex items-center gap-3">
        <span className="flex size-6 shrink-0 items-center justify-center rounded bg-accent text-xs font-semibold text-accent-foreground">
          <Terminal className="size-3.5" />
        </span>
        <h3 className="text-sm font-semibold text-main">{title}</h3>
      </div>
      <p className="mt-2 text-sm text-control-light">{description}</p>
      <div className="mt-3 flex flex-col gap-2">
        {commands.map((command) => (
          <CopyCommand key={command} command={command} />
        ))}
      </div>
    </div>
  );
}

// CopyCommand wraps CopyableCommand with per-command copy state.
function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard unavailable; the command stays selectable in the box.
    }
  }
  return (
    <CopyableCommand
      command={command}
      copied={copied}
      onCopy={() => void handleCopy()}
    />
  );
}
