import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { identityProviderServiceClient } from "@/connect";
import { startOAuthLogin } from "@/lib/oauth";
import { IdentityProviderType } from "@/types/proto-es/v1/idp_service_pb";

// OAuthLoginPage is a public deep-link entry point for SSO. A user (or an
// external link) can go straight to /oauth/login/{providerId} to start the
// OAuth flow for a specific provider without clicking the button on the
// sign-in page.
export function OAuthLoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { providerId } = useParams<{ providerId: string }>();
  const [searchParams] = useSearchParams();
  const [error, setError] = useState("");
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;

    if (!providerId) {
      setError(t("auth.oauth-login.missing-provider"));
      return;
    }

    const redirect = searchParams.get("redirect") ?? "/";

    (async () => {
      try {
        const res = await identityProviderServiceClient.listIdentityProviders(
          {}
        );
        const provider = (res.identityProviders ?? []).find(
          (p) =>
            p.name === `idps/${providerId}` || p.name.endsWith(`/${providerId}`)
        );
        if (!provider) {
          setError(
            t("auth.oauth-login.provider-not-found", { provider: providerId })
          );
          return;
        }
        if (provider.type !== IdentityProviderType.OAUTH2) {
          setError(
            t("auth.oauth-login.unsupported-type", { provider: provider.title })
          );
          return;
        }
        if (!startOAuthLogin(provider, redirect)) {
          setError(
            t("auth.oauth-login.invalid-config", { provider: provider.title })
          );
        }
        // startOAuthLogin navigates away on success; nothing else to do.
      } catch (err) {
        console.error("[oauth-login] failed to load providers", err);
        setError(t("auth.oauth-login.load-failed"));
      }
    })();
  }, [providerId, searchParams, t]);

  if (error) {
    return (
      <div className="flex w-full max-w-sm flex-col items-center gap-4">
        <h1 className="text-lg font-semibold text-main">
          {t("auth.oauth-login.failed")}
        </h1>
        <p className="text-center text-sm text-control-light">{error}</p>
        <Button onClick={() => navigate("/auth/signin")}>
          {t("auth.oauth-login.back-to-signin")}
        </Button>
      </div>
    );
  }

  return (
    <div className="flex w-full max-w-sm flex-col items-center gap-3">
      <p className="text-sm text-control-light">
        {t("auth.oauth-login.redirecting")}
      </p>
    </div>
  );
}
