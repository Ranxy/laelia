import { useEffect } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { useAppStore } from "@/react/stores";

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

  // Once session is loaded and user is logged in, redirect away from auth pages
  useEffect(() => {
    if (!sessionLoaded || !isLoggedIn) return;
    if (!location.pathname.startsWith("/auth/")) return;

    const params = new URLSearchParams(location.search);
    const redirectTo = params.get("redirect") ?? "/";
    navigate(redirectTo, { replace: true });
  }, [sessionLoaded, isLoggedIn, location, navigate]);

  // Show spinner until session state is known
  if (!sessionLoaded) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="size-8 animate-spin rounded-full border-2 border-accent border-t-transparent" />
      </div>
    );
  }

  return <Outlet />;
}
