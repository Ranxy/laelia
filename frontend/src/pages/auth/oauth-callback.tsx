import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { clearOAuthState, retrieveOAuthState } from "@/lib/oauth";
import { useAppStore } from "@/stores";

export function OAuthCallbackPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const login = useAppStore((s) => s.login);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const processed = useRef(false);

  useEffect(() => {
    if (processed.current) return;
    processed.current = true;

    const code = searchParams.get("code") ?? "";
    const stateToken = searchParams.get("state") ?? "";
    const stored = stateToken ? retrieveOAuthState(stateToken) : null;
    if (stored) clearOAuthState(stateToken);

    if (!code || !stored) {
      setError(t("auth.oauth-callback.invalid-state"));
      setDone(true);
      return;
    }

    (async () => {
      try {
        await login("", "", {
          idpName: stored.idpName,
          code,
        });
        const target =
          stored.redirect && stored.redirect !== "/" ? stored.redirect : "/";
        navigate(target, { replace: true });
      } catch (err) {
        setError(
          err instanceof Error
            ? err.message
            : t("auth.oauth-callback.login-failed")
        );
        setDone(true);
      }
    })();
  }, [login, navigate, searchParams, t]);

  if (done) {
    return (
      <div className="flex w-full max-w-sm flex-col items-center gap-4">
        <h1 className="text-lg font-semibold text-main">
          {t("auth.oauth-callback.failed")}
        </h1>
        <p className="text-center text-sm text-control-light">{error}</p>
        <Button onClick={() => navigate("/auth/signin")}>
          {t("auth.oauth-callback.back-to-signin")}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex w-full max-w-sm flex-col items-center gap-3">
      <p className="text-sm text-control-light">
        {t("auth.oauth-callback.signing-in")}
      </p>
    </div>
  );
}
