import { Outlet } from "react-router-dom";
import { LocaleSwitch } from "@/components/locale-switch";

export function SplashLayout() {
  return (
    <div className="relative flex min-h-screen items-center justify-center bg-control-bg px-4 py-12 sm:px-6 lg:px-8">
      <Outlet />
      <div className="absolute bottom-0 left-0 mb-8 w-full text-center">
        <LocaleSwitch />
        <p className="mt-2 text-sm text-control-light">
          &copy; {new Date().getFullYear()} Laelia AI
        </p>
      </div>
    </div>
  );
}
