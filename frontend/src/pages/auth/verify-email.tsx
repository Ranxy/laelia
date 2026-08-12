import { CircleAlert, CircleCheck, Loader2, Mail } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";

type VerifyState = "verifying" | "success" | "error";

export function VerifyEmailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const verifyEmail = useAppStore((s) => s.verifyEmail);
  const resendVerificationEmail = useAppStore((s) => s.resendVerificationEmail);

  const [state, setState] = useState<VerifyState>("verifying");
  const [resendEmail, setResendEmail] = useState("");
  const [resending, setResending] = useState(false);

  const token = searchParams.get("token") ?? "";

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!token) {
        if (!cancelled) setState("error");
        return;
      }
      try {
        await verifyEmail(token);
        if (!cancelled) setState("success");
      } catch {
        if (!cancelled) setState("error");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token, verifyEmail]);

  async function handleResend() {
    if (!resendEmail.trim()) return;
    setResending(true);
    try {
      await resendVerificationEmail(resendEmail.trim());
      toastManager.add({
        type: "success",
        title: t("auth.verify-email.resend-sent"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("auth.verify-email.resend-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setResending(false);
    }
  }

  return (
    <div className="flex w-full max-w-sm flex-col gap-y-6">
      <div className="text-center">
        <h1 className="text-2xl font-semibold text-main">Laelia</h1>
        <p className="mt-1 text-sm text-control-light">
          {t("auth.verify-email.title")}
        </p>
      </div>

      <div className="flex flex-col items-center gap-y-4 px-1 py-8 text-center">
        {state === "verifying" && (
          <>
            <Loader2 className="size-10 animate-spin text-control" />
            <p className="text-sm text-control">
              {t("auth.verify-email.verifying")}
            </p>
          </>
        )}

        {state === "success" && (
          <>
            <CircleCheck className="size-10 text-success" />
            <p className="text-sm text-control">
              {t("auth.verify-email.success")}
            </p>
            <Button type="button" onClick={() => navigate("/auth/signin")}>
              {t("common.sign-in")}
            </Button>
          </>
        )}

        {state === "error" && (
          <>
            <CircleAlert className="size-10 text-error" />
            <p className="text-sm text-control">
              {t("auth.verify-email.failed")}
            </p>
            <p className="text-xs text-control-light">
              {t("auth.verify-email.failed-hint")}
            </p>
            <div className="flex w-full flex-col gap-y-2">
              <Input
                type="email"
                placeholder={t("common.email")}
                value={resendEmail}
                onChange={(e) => setResendEmail(e.target.value)}
              />
              <Button
                type="button"
                variant="outline"
                onClick={handleResend}
                disabled={resending || !resendEmail.trim()}
              >
                {resending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Mail className="size-4" />
                )}
                {t("auth.verify-email.resend")}
              </Button>
            </div>
            <button
              type="button"
              className="text-accent hover:underline"
              onClick={() => navigate("/auth/signin")}
            >
              {t("common.sign-in")}
            </button>
          </>
        )}
      </div>
    </div>
  );
}
