import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useRouteError } from "react-router-dom";
import { Button } from "@/components/ui/button";

// Top-level error boundary for the router. Renders whenever a route loader
// or element throws, replacing react-router's default blank error screen
// with a translated, recoverable message.
export function RouterErrorBoundary() {
  const { t } = useTranslation();
  const error = useRouteError();

  // Surface the error in the console for debugging even when the UI shows a
  // friendly message.
  useEffect(() => {
    console.error("Route error:", error);
  }, [error]);

  const message =
    error instanceof Error ? error.message : t("router.error-unknown");

  return (
    <div role="alert" className="flex h-full flex-col items-center gap-4 p-8">
      <h1 className="text-lg font-semibold text-main">
        {t("router.error-title")}
      </h1>
      <p className="max-w-md text-sm text-control-light break-words">
        {message}
      </p>
      <Button onClick={() => window.location.reload()}>
        {t("router.error-retry")}
      </Button>
    </div>
  );
}
