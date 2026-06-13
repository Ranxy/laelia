import { Outlet } from "react-router-dom";
import { UserMenu } from "@/react/components/user-menu";

export function DashboardLayout() {
  return (
    <div className="flex h-screen flex-col overflow-hidden">
      <header className="flex h-12 shrink-0 items-center gap-4 border-b border-control-border px-6">
        <span className="text-sm font-semibold text-main">Laelia AI</span>
        <div className="flex-1" />
        <UserMenu />
      </header>
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  );
}
