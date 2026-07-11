import { ChevronRight } from "lucide-react";
import { useState } from "react";
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
import { setLocale } from "@/lib/i18n";
import { useAppStore } from "@/stores";

type LocaleOption = {
  value: string;
  label: string;
};

const LOCALES: LocaleOption[] = [
  { value: "en-US", label: "English" },
  { value: "zh-CN", label: "中文" },
];

export function UserMenu() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const currentUser = useAppStore((s) => s.currentUser);
  const logout = useAppStore((s) => s.logout);
  const [open, setOpen] = useState(false);

  async function handleLogout() {
    setOpen(false);
    await logout();
    navigate("/auth/signin", { replace: true });
  }

  if (!currentUser) return null;

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger className="flex items-center gap-1.5 rounded-sm px-2 py-1 text-sm text-control-light hover:bg-control-bg hover:text-control">
        {currentUser.title || currentUser.email}
      </DropdownMenuTrigger>
      <DropdownMenuContent className="w-48">
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

        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={handleLogout}>
          {t("common.sign-out")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
