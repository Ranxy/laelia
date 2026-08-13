import { useEffect } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  isAuthPath,
  isPublicPath,
  resolveAuthRedirect,
} from "@/router/auth-redirect";
import { useAppStore } from "@/stores";

export function RootLayout() {
  const location = useLocation();
  const navigate = useNavigate();

  const sessionLoaded = useAppStore((s) => s.sessionLoaded);
  const isLoggedIn = useAppStore((s) => s.isLoggedIn);
  const loadSession = useAppStore((s) => s.loadSession);

  // Kick off session loading on mount
  useEffect(() => {
    loadSession();
  }, [loadSession]);

  // Reactive auth guard. The root loader only runs on navigation, so it cannot
  // react to the auth store flipping sessionLoaded/isLoggedIn after the initial
  // load. Re-evaluate on every state change so a logged-out user landing
  // directly on a protected route is redirected without the protected content
  // flashing first. Also covers a logged-in user hitting an /auth/* page.
  useEffect(() => {
    const target = resolveAuthRedirect({
      sessionLoaded,
      isLoggedIn,
      pathname: location.pathname,
      search: location.search,
    });
    if (target !== null) {
      navigate(target, { replace: true });
    }
  }, [sessionLoaded, isLoggedIn, location, navigate]);

  // Show spinner until session state is known
  if (!sessionLoaded) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="size-8 animate-spin rounded-full border-2 border-accent border-t-transparent" />
      </div>
    );
  }

  // While a redirect is pending for a logged-out user on a protected route,
  // render nothing instead of <Outlet/> so the protected page never flashes.
  // Public routes (e.g. the device-login approval page) stay renderable.
  if (
    !isLoggedIn &&
    !isAuthPath(location.pathname) &&
    !isPublicPath(location.pathname)
  ) {
    return null;
  }

  return <Outlet />;
}
