import { create } from "@bufbuild/protobuf";
import { ChevronRight } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSubmenu,
  DropdownMenuSubmenuContent,
  DropdownMenuSubmenuTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import { settingServiceClient } from "@/connect";
import { LOCALES, setLocale } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import {
  GetDebugConfigRequestSchema,
  UpdateDebugConfigRequestSchema,
} from "@/types/proto-es/v1/setting_pb";

// Debug-config state + toggle, shared by the sidebar user menu (desktop) and
// the settings account section (mobile).
export function useDebugConfig() {
  const isAdmin = useHasPermission("laelia.settings.get");
  const [enabled, setEnabled] = useState(false);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!isAdmin) return;
    settingServiceClient
      .getDebugConfig(create(GetDebugConfigRequestSchema))
      .then((res) => {
        setEnabled(res.enabled);
        setLoaded(true);
      })
      .catch(() => {
        setLoaded(true);
      });
  }, [isAdmin]);

  const toggle = useCallback(async (checked: boolean) => {
    setEnabled(checked);
    try {
      await settingServiceClient.updateDebugConfig(
        create(UpdateDebugConfigRequestSchema, { enabled: checked })
      );
    } catch {
      setEnabled(!checked);
    }
  }, []);

  return { isAdmin, enabled, loaded, toggle };
}

// Sign-out + redirect to the sign-in page, shared by the sidebar user menu and
// the settings account section.
export function useLogout() {
  const logout = useAppStore((s) => s.logout);
  const navigate = useNavigate();
  return useCallback(async () => {
    await logout();
    navigate("/auth/signin", { replace: true });
  }, [logout, navigate]);
}

export function UserMenu({ collapsed = false }: { collapsed?: boolean }) {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const currentUser = useAppStore((s) => s.currentUser);
  const [open, setOpen] = useState(false);
  const {
    isAdmin,
    enabled: debugEnabled,
    loaded: debugLoaded,
    toggle: handleDebugToggle,
  } = useDebugConfig();
  const signOut = useLogout();

  async function handleLogout() {
    setOpen(false);
    await signOut();
  }

  if (!currentUser) return null;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        aria-label={currentUser.title || currentUser.email}
        className={cn(
          "flex items-center text-sm text-control-light hover:bg-link-hover hover:text-control",
          collapsed
            ? "mx-auto size-8 justify-center rounded-full bg-control-bg font-semibold"
            : "w-full gap-1.5 rounded-md px-2 py-2"
        )}
      >
        {collapsed ? (
          <span>
            {(currentUser.title || currentUser.email).charAt(0).toUpperCase()}
          </span>
        ) : (
          <span className="min-w-0 flex-1 truncate text-left">
            {currentUser.title || currentUser.email}
          </span>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-48" side={collapsed ? "right" : "top"}>
        {/* User info */}
        <div className="px-3 py-2">
          <div className="truncate text-sm font-medium text-control">
            {currentUser.title || currentUser.email}
          </div>
          <div className="truncate text-xs text-control-light">
            {currentUser.email}
          </div>
        </div>
        <DropdownMenuSeparator />

        <DropdownMenuItem
          onClick={() => {
            setOpen(false);
            navigate("/settings/profile");
          }}
        >
          {t("common.profile")}
        </DropdownMenuItem>

        {/* Language submenu */}
        <DropdownMenuSubmenu>
          <DropdownMenuSubmenuTrigger>
            <span className="flex-1">{t("common.language")}</span>
            <ChevronRight className="size-3.5 text-control-light" />
          </DropdownMenuSubmenuTrigger>
          <DropdownMenuSubmenuContent>
            {LOCALES.map((locale) => (
              <DropdownMenuItem
                key={locale.value}
                className={
                  locale.value === i18n.language ? "bg-control-bg" : ""
                }
                onClick={() => {
                  setLocale(locale.value);
                  setOpen(false);
                }}
              >
                <span className="w-4 text-center text-xs">
                  {locale.value === i18n.language ? "✓" : ""}
                </span>
                {locale.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuSubmenuContent>
        </DropdownMenuSubmenu>

        {isAdmin && (
          <>
            <DropdownMenuSeparator />
            <div className="flex items-center justify-between px-3 py-2 text-sm">
              <span className="text-control">{t("common.debug-mode")}</span>
              <Switch
                checked={debugEnabled}
                onCheckedChange={handleDebugToggle}
                disabled={!debugLoaded}
                size="sm"
              />
            </div>
          </>
        )}

        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleLogout}>
          {t("common.sign-out")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
