import { Outlet } from "react-router-dom";

export function SplashLayout() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-control-bg px-4 py-12 sm:px-6 lg:px-8">
      <Outlet />
    </div>
  );
}
