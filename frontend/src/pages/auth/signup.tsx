import {
  CircleAlert,
  CircleCheck,
  CircleHelp,
  Eye,
  EyeOff,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { settingServiceClient } from "@/connect";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";

type PasswordCheck = {
  key: string;
  label: string;
  test: (pwd: string) => boolean;
};

type PasswordCheckResult = PasswordCheck & { matched: boolean };

const PASSWORD_CHECKS: PasswordCheck[] = [
  {
    key: "min-length",
    label: "min-length",
    test: (p) => p.length >= 8,
  },
  {
    key: "require-letter",
    label: "require-letter",
    test: (p) => /[a-zA-Z]/.test(p),
  },
  {
    key: "require-number",
    label: "require-number",
    test: (p) => /[0-9]/.test(p),
  },
];

function passwordChecks(password: string): PasswordCheckResult[] {
  return PASSWORD_CHECKS.map((c) => ({ ...c, matched: c.test(password) }));
}

function isValidEmail(value: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim());
}

export function SignUpPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const register = useAppStore((s) => s.register);

  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [nameManuallyEdited, setNameManuallyEdited] = useState(false);
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [touched, setTouched] = useState(false);
  const [signupDisallowed, setSignupDisallowed] = useState(false);
  const [allowedDomains, setAllowedDomains] = useState<string[]>([]);

  // The signup policy is public (GetWorkspaceInfo needs no auth): when the
  // workspace disallows self-service registration, show a notice instead of
  // the form. The backend enforces the policy regardless.
  useEffect(() => {
    let cancelled = false;
    settingServiceClient
      .getWorkspaceInfo({})
      .then((res) => {
        if (cancelled) return;
        setSignupDisallowed(res.disallowSignup);
        if (res.enforceIdentityDomain) setAllowedDomains(res.domains ?? []);
      })
      .catch(() => {
        // Keep the form on failure; the backend still enforces the policy.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const checks = passwordChecks(password);
  const hasHint =
    touched && password.length > 0 && checks.some((c) => !c.matched);
  const mismatch =
    touched && password.length > 0 && password !== passwordConfirm;
  const emailValid = isValidEmail(email);

  const allowSubmit =
    emailValid &&
    name.trim().length > 0 &&
    password.length > 0 &&
    !hasHint &&
    !mismatch &&
    !loading;

  // Auto-fill name from email
  useEffect(() => {
    if (nameManuallyEdited || !email.includes("@")) return;
    const parts = email.split("@")[0].replaceAll("_", ".").split(".");
    if (parts.length >= 2) {
      setName(
        `${parts[0].charAt(0).toUpperCase()}${parts[0].slice(1)} ${parts[1].charAt(0).toUpperCase()}${parts[1].slice(1)}`
      );
    } else if (parts[0].length > 0) {
      setName(parts[0].charAt(0).toUpperCase() + parts[0].slice(1));
    }
  }, [email, nameManuallyEdited]);

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setTouched(true);
    if (!allowSubmit) return;
    setLoading(true);
    try {
      await register(email, name.trim(), password);
      navigate("/", { replace: true });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("auth.sign-up.failed"),
        description:
          err instanceof Error
            ? err.message
            : t("auth.sign-up.failed-description"),
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
          {t("auth.sign-up.title")}
        </p>
      </div>

      {signupDisallowed ? (
        <div className="flex flex-col items-center gap-y-4 px-1 py-8 text-center">
          <p className="text-sm text-control-light">
            {t("auth.sign-up.disallowed")}
          </p>
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate("/auth/signin")}
          >
            {t("common.sign-in")}
          </Button>
        </div>
      ) : (
        <>
          <form onSubmit={handleSubmit} className="flex flex-col gap-y-6 px-1">
            {/* Email */}
            <div>
              <label
                htmlFor="signup-email"
                className="block text-sm font-medium leading-5 text-control"
              >
                {t("common.email")}
                <span className="ml-0.5 text-error">*</span>
              </label>
              <div className="mt-1 rounded-md shadow-xs">
                <Input
                  id="signup-email"
                  type="email"
                  autoComplete="email"
                  placeholder={t("auth.sign-up.email-placeholder")}
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className={touched && !emailValid ? "border-error" : ""}
                />
              </div>
              {touched && !emailValid && (
                <p className="mt-1 pl-1 text-sm text-error">
                  {t("auth.sign-up.email-invalid")}
                </p>
              )}
            </div>

            {/* Name */}
            <div>
              <label
                htmlFor="signup-name"
                className="block text-sm font-medium leading-5 text-control"
              >
                {t("common.name")}
                <span className="ml-0.5 text-error">*</span>
              </label>
              <div className="mt-1 rounded-md shadow-xs">
                <Input
                  id="signup-name"
                  type="text"
                  autoComplete="name"
                  placeholder={t("auth.sign-up.name-placeholder")}
                  required
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value);
                    setNameManuallyEdited(e.target.value.trim().length > 0);
                  }}
                />
              </div>
            </div>

            {/* Password */}
            <div>
              <label
                htmlFor="signup-password"
                className="block text-sm font-medium leading-5 text-control"
              >
                {t("common.password")}
                <span className="ml-0.5 text-error">*</span>
              </label>
              <div className="relative mt-1 flex flex-row items-center rounded-md shadow-xs">
                <Input
                  id="signup-password"
                  type={showPassword ? "text" : "password"}
                  autoComplete="new-password"
                  required
                  value={password}
                  onFocus={() => setTouched(true)}
                  onChange={(e) => setPassword(e.target.value)}
                  className={hasHint ? "border-error" : ""}
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
              {touched && password.length > 0 && (
                <ul className="mt-1 space-y-0.5 pl-1">
                  {checks.map((check) => (
                    <li
                      key={check.key}
                      className="flex items-center gap-x-1 text-sm"
                    >
                      {check.matched ? (
                        <CircleCheck className="size-4 shrink-0 text-success" />
                      ) : (
                        <CircleAlert className="size-4 shrink-0 text-error" />
                      )}
                      <span
                        className={
                          check.matched ? "text-control-light" : "text-error"
                        }
                      >
                        {t(`auth.sign-up.password-${check.label}`)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              {!touched && (
                <p className="mt-1 flex items-center gap-x-1 pl-1 text-sm text-control-light">
                  <CircleHelp className="size-4" />
                  {t("auth.sign-up.password-hint")}
                </p>
              )}
            </div>

            {/* Confirm password */}
            <div>
              <label
                htmlFor="signup-password-confirm"
                className="block text-sm font-medium leading-5 text-control"
              >
                {t("auth.sign-up.password-confirm")}
                <span className="ml-0.5 text-error">*</span>
              </label>
              <div className="relative mt-1 flex flex-row items-center rounded-md shadow-xs">
                <Input
                  id="signup-password-confirm"
                  type={showPassword ? "text" : "password"}
                  autoComplete="new-password"
                  required
                  value={passwordConfirm}
                  onFocus={() => setTouched(true)}
                  onChange={(e) => setPasswordConfirm(e.target.value)}
                  className={mismatch ? "border-error" : ""}
                />
              </div>
              {mismatch && (
                <p className="mt-1 pl-1 text-sm text-error">
                  {t("auth.sign-up.password-mismatch")}
                </p>
              )}
            </div>

            <div className="w-full">
              <Button
                type="submit"
                size="lg"
                className="w-full"
                disabled={!allowSubmit}
              >
                {loading ? "…" : t("common.sign-up")}
              </Button>
            </div>
          </form>

          <p className="text-center text-sm text-control-light">
            {t("auth.sign-up.existing-user")}{" "}
            <button
              type="button"
              className="text-accent hover:underline"
              onClick={() => navigate("/auth/signin")}
            >
              {t("common.sign-in")}
            </button>
          </p>
          {allowedDomains.length > 0 && (
            <p className="text-center text-xs text-control-light">
              {t("auth.sign-up.allowed-domains", {
                domains: allowedDomains.join(", "),
              })}
            </p>
          )}
        </>
      )}
    </div>
  );
}
