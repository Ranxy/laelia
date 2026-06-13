import { useNavigate } from "react-router-dom";
import { Button } from "@/react/components/ui/button";
import { useAppStore } from "@/react/stores";

export function LandingPage() {
  const navigate = useNavigate();
  const currentUser = useAppStore((s) => s.currentUser);
  const logout = useAppStore((s) => s.logout);

  async function handleLogout() {
    await logout();
    navigate("/auth/signin", { replace: true });
  }

  return (
    <div className="flex min-h-full flex-col items-center justify-center gap-6 px-4 py-16">
      <h1 className="text-3xl font-bold text-main">
        Welcome
        {currentUser ? `, ${currentUser.title || currentUser.email}` : ""}
      </h1>
      <p className="max-w-md text-center text-control-light">
        You are now signed in to Laelia AI. More features coming soon.
      </p>
      <Button variant="outline" onClick={handleLogout}>
        Sign out
      </Button>
    </div>
  );
}
