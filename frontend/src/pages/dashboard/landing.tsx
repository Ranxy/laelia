import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Button } from "@/react/components/ui/button";
import { useAppStore } from "@/react/stores";

export function LandingPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const currentUser = useAppStore((s) => s.currentUser);
  const logout = useAppStore((s) => s.logout);

  async function handleLogout() {
    await logout();
    navigate("/auth/signin", { replace: true });
  }

  return (
    <div className="flex h-full overflow-y-auto flex-col items-center justify-center gap-6 px-4 py-16">
      <h1 className="text-3xl font-bold text-main">
        {t("landing.welcome")}
        {currentUser ? `, ${currentUser.title || currentUser.email}` : ""}
      </h1>
      <p className="max-w-md text-center text-control-light">
        {t("landing.description")}
      </p>
      <Button variant="outline" onClick={handleLogout}>
        {t("common.sign-out")}
      </Button>
    </div>
  );
}
