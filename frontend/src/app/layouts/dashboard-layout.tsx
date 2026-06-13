import { Outlet } from "react-router-dom";
import { useAppStore } from "@/react/stores";

export function DashboardLayout() {
  const currentUser = useAppStore((s) => s.currentUser);

  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <header className="flex h-12 shrink-0 items-center gap-4 border-b border-control-border px-6">
        <span className="text-sm font-semibold text-main">Laelia AI</span>
        <div className="flex-1" />
        {currentUser ? (
          <span className="text-sm text-control-light">
            {currentUser.title || currentUser.email}
          </span>
        ) : null}
      </header>
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
