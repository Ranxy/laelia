import { Eye, EyeOff } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { settingServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";

export function SignInPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const login = useAppStore((s) => s.login);

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [signupDisallowed, setSignupDisallowed] = useState(false);

  // The signup policy is public (GetWorkspaceInfo needs no auth): hide the
  // signup entry when the workspace disallows self-service registration.
  useEffect(() => {
    let cancelled = false;
    settingServiceClient
      .getWorkspaceInfo({})
      .then((res) => {
        if (!cancelled) setSignupDisallowed(res.disallowSignup);
      })
      .catch(() => {
        // Keep the signup link on failure; the backend still enforces the
        // policy on the signup attempt itself.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const redirectTo = searchParams.get("redirect") ?? "/";
  const allowSubmit = email.length > 0 && password.length > 0 && !loading;

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!allowSubmit) return;
    setLoading(true);
    try {
      await login(email, password);
      navigate(redirectTo, { replace: true });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("auth.sign-in.failed"),
        description:
          err instanceof Error
            ? err.message
            : t("auth.sign-in.failed-description"),
      });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex w-full max-w-sm flex-col gap-y-6">
      <div className="text-center">
        <h1 className="text-2xl font-semibold text-main">Laelia</h1>
        <p className="mt-1 text-sm text-control-light">
          {t("auth.sign-in.title")}
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-y-6 px-1">
        <div>
          <label
            htmlFor="signin-email"
            className="block text-sm font-medium leading-5 text-control"
          >
            {t("common.email")}
            <span className="ml-0.5 text-error">*</span>
          </label>
          <div className="mt-1 rounded-md shadow-xs">
            <Input
              id="signin-email"
              type="email"
              autoComplete="email"
              placeholder={t("auth.sign-in.email-placeholder")}
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </div>
        </div>

        <div>
          <label
            htmlFor="signin-password"
            className="block text-sm font-medium leading-5 text-control"
          >
            {t("common.password")}
            <span className="ml-0.5 text-error">*</span>
          </label>
          <div className="relative mt-1 flex flex-row items-center rounded-md shadow-xs">
            <Input
              id="signin-password"
              type={showPassword ? "text" : "password"}
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <button
              type="button"
              className="absolute right-3 hover:cursor-pointer"
              onClick={() => setShowPassword((v) => !v)}
              aria-label={t("common.toggle-password-visibility")}
            >
              {showPassword ? (
                <Eye className="size-4" />
              ) : (
                <EyeOff className="size-4" />
              )}
            </button>
          </div>
        </div>

        <div className="w-full">
          <Button
            type="submit"
            size="lg"
            className="w-full"
            disabled={!allowSubmit}
          >
            {loading ? "…" : t("common.sign-in")}
          </Button>
        </div>
      </form>

      {!signupDisallowed && (
        <p className="text-center text-sm text-control-light">
          {t("auth.sign-in.new-user")}{" "}
          <button
            type="button"
            className="text-accent hover:underline"
            onClick={() =>
              navigate(
                `/auth/signup${redirectTo !== "/" ? `?redirect=${encodeURIComponent(redirectTo)}` : ""}`
              )
            }
          >
            {t("common.sign-up")}
          </button>
        </p>
      )}
    </div>
  );
}
