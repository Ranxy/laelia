import { redirect } from "react-router-dom";
import { useAppStore } from "@/stores";
import { resolveAuthRedirect } from "./auth-redirect";

export function rootGuard({ url }: { url: URL }): Response | null {
  const { isLoggedIn, sessionLoaded } = useAppStore.getState();
  const target = resolveAuthRedirect({
    sessionLoaded,
    isLoggedIn,
    pathname: url.pathname,
    search: url.search,
  });
  return target ? redirect(target) : null;
}
