import {
  Bell,
  Blocks,
  Bot,
  Bug,
  ChevronRight,
  ClipboardList,
  Database,
  Languages,
  Lock,
  LogOut,
  Monitor,
  Server,
  Shield,
  UserCircle,
  UserCog,
  Users,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Navigate, useNavigate } from "react-router-dom";
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useDebugConfig, useLogout } from "@/components/user-menu";
import { LOCALES, setLocale } from "@/lib/i18n";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";

interface MenuItem {
  to: string;
  icon: typeof UserCircle;
  label: string;
}

function useSettingsMenuItems(): MenuItem[] {
  const { t } = useTranslation();
  const canViewStorage =
    useHasPermission("laelia.settings.get") ||
    useHasPermission("laelia.settings.update");
  const canViewUsers = useHasPermission("laelia.users.list");
  const canViewMachines = useHasPermission("laelia.machines.get");
  const canViewRoles = useHasPermission("laelia.roles.list");
  const canViewIam = useHasPermission("laelia.iam.getPolicy");
  const canViewGroups = useHasPermission("laelia.groups.list");
  const canViewApiProviders = useHasPermission("laelia.apiProviders.list");
  const canViewAudit = useHasPermission("laelia.auditLogs.search");
  const canViewPushConfig = useHasPermission("laelia.pushConfig.update");

  return useMemo(
    () =>
      [
        {
          to: "/settings/profile",
          icon: UserCircle,
          label: t("sidebar.settings-profile"),
        },
        canViewStorage && {
          to: "/settings/storage",
          icon: Database,
          label: t("sidebar.settings-storage"),
        },
        canViewStorage && {
          to: "/settings/agents",
          icon: Bot,
          label: t("sidebar.settings-agents"),
        },
        canViewPushConfig && {
          to: "/settings/notifications",
          icon: Bell,
          label: t("sidebar.settings-notifications"),
        },
        canViewUsers && {
          to: "/settings/users",
          icon: Users,
          label: t("sidebar.settings-users"),
        },
        canViewRoles && {
          to: "/settings/roles",
          icon: Shield,
          label: t("sidebar.settings-roles"),
        },
        canViewIam && {
          to: "/settings/iam",
          icon: Lock,
          label: t("sidebar.settings-iam"),
        },
        canViewGroups && {
          to: "/settings/groups",
          icon: UserCog,
          label: t("sidebar.settings-groups"),
        },
        canViewApiProviders && {
          to: "/settings/api-providers",
          icon: Blocks,
          label: t("sidebar.settings-api-providers"),
        },
        {
          to: "/settings/mcp-servers",
          icon: Server,
          label: t("sidebar.settings-mcp-servers"),
        },
        canViewAudit && {
          to: "/settings/audit",
          icon: ClipboardList,
          label: t("sidebar.settings-audit"),
        },
        canViewMachines && {
          to: "/machines",
          icon: Monitor,
          label: t("sidebar.machines"),
        },
      ].filter(Boolean) as MenuItem[],
    [
      t,
      canViewStorage,
      canViewUsers,
      canViewRoles,
      canViewIam,
      canViewGroups,
      canViewApiProviders,
      canViewAudit,
      canViewPushConfig,
      canViewMachines,
    ]
  );
}

export function SettingsMenuPage() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const items = useSettingsMenuItems();
  const currentUser = useAppStore((s) => s.currentUser);
  const {
    isAdmin,
    enabled: debugEnabled,
    loaded: debugLoaded,
    toggle: handleDebugToggle,
  } = useDebugConfig();
  const signOut = useLogout();
  const [signOutOpen, setSignOutOpen] = useState(false);

  async function handleConfirmSignOut() {
    setSignOutOpen(false);
    await signOut();
  }

  return (
    <div className="h-full overflow-y-auto px-4 pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom)+1rem)] pt-4 lg:p-6">
      <h1 className="mb-4 hidden text-xl font-semibold text-main lg:block">
        {t("settings.title")}
      </h1>
      <nav aria-label={t("settings.title")} className="flex flex-col gap-1">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <button
              key={item.to}
              type="button"
              onClick={() => navigate(item.to)}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-3 text-left",
                "text-sm font-medium text-main transition-colors hover:bg-control-bg"
              )}
            >
              <Icon className="size-5 shrink-0 text-control" />
              <span className="flex-1 truncate">{item.label}</span>
              <ChevronRight className="size-4 shrink-0 text-control-light" />
            </button>
          );
        })}
      </nav>
      {/* The user menu (identity + language/debug/sign-out) lives here on
          mobile instead of the page header; Profile is already a settings
          item above. */}
      <div className="mt-4 border-t border-control-border pt-3">
        <p className="px-3 pb-2 text-xs font-medium text-control-light">
          {t("settings.account")}
        </p>
        <div className="flex flex-col gap-1">
          <div className="px-3 py-2">
            <div className="truncate text-sm font-medium text-control">
              {currentUser?.title || currentUser?.email}
            </div>
            <div className="truncate text-xs text-control-light">
              {currentUser?.email}
            </div>
          </div>
          <div className="flex items-center gap-3 rounded-md px-3 py-3 text-sm font-medium text-main">
            <Languages className="size-5 shrink-0 text-control" />
            <span className="flex-1 truncate">{t("common.language")}</span>
            <Select
              value={i18n.language}
              onValueChange={(value) => {
                if (value) setLocale(value);
              }}
            >
              <SelectTrigger className="shrink-0">
                <SelectValue>
                  {(value) =>
                    LOCALES.find((l) => l.value === value)?.label ?? value
                  }
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {LOCALES.map((locale) => (
                  <SelectItem key={locale.value} value={locale.value}>
                    {locale.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {isAdmin && (
            <div className="flex items-center gap-3 rounded-md px-3 py-3 text-sm font-medium text-main">
              <Bug className="size-5 shrink-0 text-control" />
              <span className="flex-1 truncate">{t("common.debug-mode")}</span>
              <Switch
                checked={debugEnabled}
                onCheckedChange={handleDebugToggle}
                disabled={!debugLoaded}
                size="sm"
              />
            </div>
          )}
          <button
            type="button"
            onClick={() => setSignOutOpen(true)}
            className="flex w-full items-center gap-3 rounded-md border border-error/40 bg-error/10 px-3 py-3 text-left text-sm font-medium text-error transition-colors hover:bg-error/15"
          >
            <LogOut className="size-5 shrink-0" />
            <span className="flex-1 truncate">{t("common.sign-out")}</span>
          </button>
        </div>
      </div>
      <AlertDialog open={signOutOpen} onOpenChange={setSignOutOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("common.sign-out-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("common.sign-out-confirm-description")}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline">{t("common.cancel")}</Button>
            </AlertDialogClose>
            <Button variant="destructive" onClick={handleConfirmSignOut}>
              {t("common.sign-out")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

export function SettingsIndex() {
  const isDesktop = useIsDesktop();
  if (isDesktop) {
    return <Navigate to="profile" replace />;
  }
  return <SettingsMenuPage />;
}
