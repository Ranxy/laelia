import { ArrowLeft } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ROUTE_INFO } from "@/router/route-info";
import { useCurrentRoute } from "@/router/use-current-route";

interface MobileHeaderProps {
  // While the swipe-back gesture previews the destination page underneath,
  // the header shows that page's title (and hides its own back button).
  previewTitleKey?: string;
}

export function MobileHeader({ previewTitleKey }: MobileHeaderProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const currentRoute = useCurrentRoute();

  const { title, backPath } = useMemo(() => {
    if (previewTitleKey) {
      return { title: t(previewTitleKey), backPath: undefined };
    }
    const info = currentRoute.name ? ROUTE_INFO[currentRoute.name] : undefined;
    if (info) {
      return { title: t(info.titleKey), backPath: info.backTo };
    }
    return { title: t("sidebar.home"), backPath: undefined };
  }, [previewTitleKey, currentRoute.name, t]);

  return (
    <header className="flex h-[var(--mobile-header-height)] shrink-0 items-center gap-2 border-b border-control-border bg-background px-4 lg:hidden">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => backPath && navigate(backPath)}
        disabled={!backPath}
        aria-label={t("common.back")}
        className={cn(
          "size-8 shrink-0 p-0",
          !backPath && "invisible pointer-events-none"
        )}
      >
        <ArrowLeft className="size-5" />
      </Button>
      <h1 className="min-w-0 flex-1 truncate text-center text-base font-semibold text-main">
        {title}
      </h1>
    </header>
  );
}
