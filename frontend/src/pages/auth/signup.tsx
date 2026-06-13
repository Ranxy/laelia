import {
  CircleAlert,
  CircleCheck,
  CircleHelp,
  Eye,
  EyeOff,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "@/react/components/ui/button";
import { Input } from "@/react/components/ui/input";
import { toastManager } from "@/react/lib/toast";
import { useAppStore } from "@/react/stores";

type PasswordCheck = {
  key: string;
  label: string;
  test: (pwd: string) => boolean;
};

type PasswordCheckResult = PasswordCheck & { matched: boolean };

const PASSWORD_CHECKS: PasswordCheck[] = [
  {
    key: "min-length",
    label: "At least 8 characters",
    test: (p) => p.length >= 8,
  },
  {
    key: "require-letter",
    label: "At least 1 letter",
    test: (p) => /[a-zA-Z]/.test(p),
  },
  {
    key: "require-number",
    label: "At least 1 number",
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
        title: "Registration failed",
        description: err instanceof Error ? err.message : "Please try again.",
      });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="flex w-full max-w-sm flex-col gap-y-6">
      <div className="text-center">
        <h1 className="text-2xl font-semibold text-main">Laelia</h1>
        <p className="mt-1 text-sm text-control-light">Create your account</p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-y-6 px-1">
        {/* Email */}
        <div>
          <label
            htmlFor="signup-email"
            className="block text-sm font-medium leading-5 text-control"
          >
            Email
            <span className="ml-0.5 text-error">*</span>
          </label>
          <div className="mt-1 rounded-md shadow-xs">
            <Input
              id="signup-email"
              type="email"
              autoComplete="email"
              placeholder="you@example.com"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className={touched && !emailValid ? "border-error" : ""}
            />
          </div>
          {touched && !emailValid && (
            <p className="mt-1 pl-1 text-sm text-error">
              Please enter a valid email address.
            </p>
          )}
        </div>

        {/* Name */}
        <div>
          <label
            htmlFor="signup-name"
            className="block text-sm font-medium leading-5 text-control"
          >
            Name
            <span className="ml-0.5 text-error">*</span>
          </label>
          <div className="mt-1 rounded-md shadow-xs">
            <Input
              id="signup-name"
              type="text"
              autoComplete="name"
              placeholder="example: John Doe"
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
            Password
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
              aria-label="Toggle password visibility"
            >
              {showPassword ? (
                <Eye className="size-4" />
              ) : (
                <EyeOff className="size-4" />
              )}
            </button>
          </div>
          {/* Password requirement checks */}
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
                    {check.label}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {/* Password hint icon when collapsed */}
          {!touched && (
            <p className="mt-1 flex items-center gap-x-1 pl-1 text-sm text-control-light">
              <CircleHelp className="size-4" />
              Must be at least 8 characters with letters and numbers.
            </p>
          )}
        </div>

        {/* Confirm password */}
        <div>
          <label
            htmlFor="signup-password-confirm"
            className="block text-sm font-medium leading-5 text-control"
          >
            Confirm password
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
              Passwords do not match.
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
            {loading ? "Creating account…" : "Create account"}
          </Button>
        </div>
      </form>

      <p className="text-center text-sm text-control-light">
        Already have an account?{" "}
        <button
          type="button"
          className="text-accent hover:underline"
          onClick={() => navigate("/auth/signin")}
        >
          Sign in
        </button>
      </p>
    </div>
  );
}
