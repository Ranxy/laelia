import { Loader2, Monitor, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { Button } from "@/components/ui/button";
import { deviceServiceClient } from "@/connect";
import { useAvatar } from "@/lib/avatar-cache";
import { describeError } from "@/lib/connect-errors";
import { useAppStore } from "@/stores";
import { DeviceLoginStatus } from "@/types/proto-es/v1/device_pb";

// DeviceLoginPage is the public approval page for the OAuth2-style device
// code flow. The machine CLI prints
//   https://<manager>/login/device?user_code=XXXX-XXXX
// and the user opens it here: the page shows the device's hostname and the
// user code (so the user can verify they match the device screen), the
// signed-in account (so the user can verify it is the right one), and an
// Approve action. Logged-out users are offered a sign-in link that returns
// here after login; signed-in users can switch accounts.
export function DeviceLoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const userCode = (searchParams.get("user_code") ?? "").toUpperCase();
  const currentUser = useAppStore((s) => s.currentUser);
  const logout = useAppStore((s) => s.logout);
  const avatarUrl = useAvatar(currentUser?.name);

  const [status, setStatus] = useState<DeviceLoginStatus>(
    DeviceLoginStatus.UNSPECIFIED
  );
  const [hostname, setHostname] = useState("");
  const [os, setOs] = useState("");
  const [arch, setArch] = useState("");
  const [ip, setIp] = useState("");
  const [reauthExisting, setReauthExisting] = useState(false);
  const [machineTitle, setMachineTitle] = useState("");
  const [machineOwner, setMachineOwner] = useState("");
  const [denialReason, setDenialReason] = useState("");
  const [approving, setApproving] = useState(false);
  const [approved, setApproved] = useState(false);
  const [approveError, setApproveError] = useState("");
  const [pollFailed, setPollFailed] = useState(false);
  const [closeBlocked, setCloseBlocked] = useState(false);

  const poll = useCallback(async () => {
    if (!userCode) return;
    try {
      const res = await deviceServiceClient.getDeviceLoginStatus({ userCode });
      setStatus(res.status);
      setHostname(res.hostname);
      setOs(res.os);
      setArch(res.arch);
      setIp(res.ip);
      setReauthExisting(res.reauthExisting);
      setMachineTitle(res.machineTitle);
      setMachineOwner(res.machineOwner);
      setDenialReason(res.denialReason);
      setPollFailed(false);
    } catch {
      setPollFailed(true);
    }
  }, [userCode]);

  // Stop polling once the login is approved: the success view replaces the
  // whole pending UI, so there is nothing left to refresh.
  useEffect(() => {
    if (!userCode || approved) return;
    void poll();
    const id = setInterval(() => void poll(), 3000);
    return () => clearInterval(id);
  }, [poll, userCode, approved]);

  async function handleApprove() {
    if (approving) return;
    setApproving(true);
    setApproveError("");
    try {
      await deviceServiceClient.approveDeviceLogin({ userCode });
      setApproved(true);
    } catch (err) {
      // A policy denial marks the session DENIED server-side; the next poll
      // surfaces the reason. Show the raw error meanwhile and allow retry.
      setApproveError(describeError(err));
      setApproving(false);
    }
  }

  async function handleUseAnotherAccount() {
    const redirect = encodeURIComponent(
      window.location.pathname + window.location.search
    );
    await logout();
    navigate(`/auth/signin?redirect=${redirect}`, { replace: true });
  }

  const signInHref = `/auth/signin?redirect=${encodeURIComponent(
    window.location.pathname + window.location.search
  )}`;

  // Browsers only let scripts close windows they opened themselves; this tab
  // was opened by the user (from the URL the CLI printed), so window.close()
  // is silently blocked. Try it anyway (it works when the page was opened via
  // window.open or has a single history entry); if the tab is still alive a
  // moment later, fall back to a manual-close hint.
  function handleClosePage() {
    setCloseBlocked(false);
    window.close();
    window.setTimeout(() => setCloseBlocked(true), 500);
  }

  if (approved || status === DeviceLoginStatus.APPROVED) {
    return (
      <div className="mx-auto mt-20 w-full max-w-md">
        <div className="rounded-2xl border border-control-border bg-background p-8 text-center shadow-sm">
          <div className="mx-auto flex size-16 items-center justify-center rounded-full bg-success/10">
            <ShieldCheck className="size-9 text-success" />
          </div>
          <h2 className="mt-5 text-xl font-semibold text-main">
            {t("auth.device-login.approved-title")}
          </h2>
          <p className="mt-2 text-sm leading-relaxed text-control-light">
            {t("auth.device-login.approved-complete")}
          </p>
          <Button size="lg" className="mt-7 w-full" onClick={handleClosePage}>
            {t("auth.device-login.close-page")}
          </Button>
          {closeBlocked && (
            <p className="mt-3 text-xs text-control-light">
              {t("auth.device-login.close-blocked")}
            </p>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto mt-20 w-full max-w-md">
      <div className="text-center">
        <h1 className="text-2xl font-semibold text-main">Laelia</h1>
        <p className="mt-1 text-sm text-control-light">
          {t("auth.device-login.title")}
        </p>
      </div>

      {!userCode ? (
        <div className="mt-6 rounded-2xl border border-control-border bg-background p-6 text-center shadow-sm">
          <p className="text-sm text-control-light">
            {t("auth.device-login.missing-code")}
          </p>
        </div>
      ) : status === DeviceLoginStatus.EXPIRED ? (
        <div className="mt-6 rounded-2xl border border-control-border bg-background p-6 text-center shadow-sm">
          <p className="text-sm font-medium text-main">
            {t("auth.device-login.expired")}
          </p>
          <p className="mt-1 text-sm text-control-light">
            {t("auth.device-login.expired-hint")}
          </p>
        </div>
      ) : status === DeviceLoginStatus.DENIED ? (
        <div className="mt-6 rounded-2xl border border-control-border bg-background p-6 text-center shadow-sm">
          <p className="text-sm font-medium text-main">
            {t("auth.device-login.denied")}
          </p>
          <p className="mt-1 text-sm text-control-light">
            {denialReason || t("auth.device-login.denied-hint")}
          </p>
        </div>
      ) : pollFailed && status === DeviceLoginStatus.UNSPECIFIED ? (
        <div className="mt-6 rounded-2xl border border-control-border bg-background p-6 text-center shadow-sm">
          <p className="text-sm text-control-light">
            {t("auth.device-login.unreachable")}
          </p>
        </div>
      ) : (
        <div className="mt-6 overflow-hidden rounded-2xl border border-control-border bg-background shadow-sm">
          {/* Device info */}
          <div className="flex items-center gap-3 border-b border-control-border px-6 py-4">
            <div className="flex size-10 shrink-0 items-center justify-center rounded-full bg-control-bg">
              <Monitor className="size-5 text-control" />
            </div>
            <div className="min-w-0">
              <p className="truncate text-sm font-medium text-main">
                {hostname || t("auth.device-login.unknown-device")}
              </p>
              <p className="text-xs text-control-light">
                {os}
                {arch ? ` · ${arch}` : ""}
                {ip ? ` · ${ip}` : ""}
              </p>
            </div>
          </div>
          {reauthExisting && machineTitle && (
            <p className="border-b border-control-border bg-control-bg/40 px-6 py-2 text-center text-xs text-control-light">
              {t("auth.device-login.reauth-existing", { title: machineTitle })}
              {machineOwner
                ? ` · ${t("auth.device-login.reauth-owner", { owner: machineOwner })}`
                : ""}
            </p>
          )}

          {/* Device code */}
          <div className="px-6 py-5">
            <p className="text-center text-xs font-medium uppercase tracking-widest text-control-light">
              {t("auth.device-login.enter-code")}
            </p>
            <div className="mt-3 rounded-xl border border-dashed border-control-border bg-control-bg/50 py-4 text-center">
              <p className="font-mono text-3xl font-bold tracking-[0.35em] text-main">
                {userCode}
              </p>
            </div>
          </div>

          {/* Account + approve */}
          <div className="border-t border-control-border px-6 py-5">
            {currentUser ? (
              <div className="flex flex-col gap-4">
                <div>
                  <p className="text-xs font-medium uppercase tracking-wider text-control-light">
                    {t("auth.device-login.signed-in-as")}
                  </p>
                  <div className="mt-2 flex items-center gap-3">
                    <Avatar src={avatarUrl} seed={currentUser.name} />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium text-main">
                        {currentUser.title}
                      </p>
                      <p className="truncate text-xs text-control-light">
                        {currentUser.email}
                      </p>
                    </div>
                    <button
                      type="button"
                      className="shrink-0 text-xs font-medium text-accent hover:underline"
                      onClick={() => void handleUseAnotherAccount()}
                    >
                      {t("auth.device-login.use-another-account")}
                    </button>
                  </div>
                </div>
                <Button
                  size="lg"
                  className="w-full"
                  disabled={approving}
                  onClick={() => void handleApprove()}
                >
                  {approving ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <ShieldCheck className="size-4" />
                  )}
                  {approving
                    ? t("auth.device-login.approving")
                    : t("auth.device-login.approve")}
                </Button>
                {approveError && (
                  <p className="text-center text-xs text-danger">
                    {approveError}
                  </p>
                )}
                <p className="text-center text-xs text-control-light">
                  {t("auth.device-login.approve-hint")}
                </p>
              </div>
            ) : (
              <div className="flex flex-col gap-3">
                <Link to={signInHref} className="w-full">
                  <Button size="lg" className="w-full">
                    {t("auth.device-login.sign-in")}
                  </Button>
                </Link>
                <p className="text-center text-xs text-control-light">
                  {t("auth.device-login.sign-in-hint")}
                </p>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
