import { Loader2, Monitor, ShieldCheck } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { Button } from "@/components/ui/button";
import { deviceServiceClient } from "@/connect";
import { useAvatar } from "@/lib/avatar-cache";
import { describeError } from "@/lib/connect-errors";
import { useAppStore } from "@/stores";
import { DeviceLoginStatus } from "@/types/proto-es/v1/device_pb";

// Poll cadence with failure backoff: a successful tick schedules the next one
// at POLL_BASE_MS; each consecutive failure doubles the delay, capped at
// POLL_MAX_MS, and any success resets the streak back to the base interval.
const POLL_BASE_MS = 3000;
const POLL_MAX_MS = 15000;
// Consecutive failures before the page treats the server as unreachable.
const UNREACHABLE_AFTER_FAILURES = 3;

// isTerminalStatus reports whether a device login session has reached an end
// state: the APPROVED success view, the EXPIRED card, or the DENIED card. None
// of them can change afterwards, so polling stops.
function isTerminalStatus(s: DeviceLoginStatus): boolean {
  return (
    s === DeviceLoginStatus.APPROVED ||
    s === DeviceLoginStatus.EXPIRED ||
    s === DeviceLoginStatus.DENIED
  );
}

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
  // Reachability bookkeeping. `consecutiveFailures` counts back-to-back poll
  // errors (any success resets it); `everSucceeded` records whether at least
  // one poll returned data. The full-screen unreachable card is reserved for
  // "nothing ever loaded" (no data to lose); once device info has been shown,
  // a flaky connection keeps the page as-is — see `stale` below.
  const [consecutiveFailures, setConsecutiveFailures] = useState(0);
  const [everSucceeded, setEverSucceeded] = useState(false);
  const [closeBlocked, setCloseBlocked] = useState(false);

  // Returns whether the poll succeeded so the loop can apply backoff. Status
  // and device fields are only written on success: until the first successful
  // poll, `status` stays UNSPECIFIED.
  const poll = useCallback(async (): Promise<boolean> => {
    if (!userCode) return false;
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
      setConsecutiveFailures(0);
      setEverSucceeded(true);
      return true;
    } catch {
      return false;
    }
  }, [userCode]);

  // Poll loop. A self-scheduling setTimeout (instead of a fixed interval) so
  // each round can pick its own delay:
  //   - base 3s, doubled per consecutive failure, capped at 15s, reset by any
  //     success — a down server backs off instead of hammering getDeviceLoginStatus;
  //   - a hidden tab pauses polling (visibilitychange): the pending timeout is
  //     cancelled and an in-flight tick doesn't schedule a follow-up; coming
  //     back to the foreground polls immediately.
  // The effect keys off `running`, not the raw status: intermediate
  // transitions (UNSPECIFIED→PENDING) keep the loop running unchanged, while
  // terminal states and approval tear it down. The in-loop `failures` counter
  // survives those non-terminal re-renders, so the backoff streak is only
  // reset by an actual success.
  const running = !approved && !isTerminalStatus(status);
  useEffect(() => {
    if (!userCode || !running) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let disposed = false;
    let failures = 0;

    const schedule = (delay: number) => {
      timer = setTimeout(() => void tick(), delay);
    };

    const tick = async () => {
      timer = null;
      if (disposed || document.hidden) return;
      const ok = await poll();
      if (disposed) return;
      if (ok) {
        failures = 0;
        setEverSucceeded(true);
      } else {
        failures += 1;
      }
      setConsecutiveFailures(failures);
      // A tick that ended with the tab hidden doesn't schedule a follow-up;
      // the visibilitychange listener restarts the loop on return.
      if (document.hidden) return;
      schedule(
        failures === 0
          ? POLL_BASE_MS
          : Math.min(POLL_BASE_MS * 2 ** failures, POLL_MAX_MS)
      );
    };

    const onVisible = () => {
      if (document.hidden) {
        // Pause: cancel the pending scheduled poll.
        if (timer !== null) {
          clearTimeout(timer);
          timer = null;
        }
        return;
      }
      // Resume: poll immediately, then let the loop self-schedule again.
      void tick();
    };

    void tick();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      disposed = true;
      if (timer !== null) clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [userCode, running, poll]);

  // Never-succeeded: after UNREACHABLE_AFTER_FAILURES consecutive failures
  // with no data ever loaded, swap in the full-screen unreachable card (same
  // semantics as before). Once data HAS been shown, don't yank the page out
  // from under the user — keep the loaded content and overlay a light
  // stale-data notice instead. (A-2 follow-up: replace the notice with a
  // dedicated stale-state surface + retry affordance.)
  const unreachable =
    !everSucceeded && consecutiveFailures >= UNREACHABLE_AFTER_FAILURES;
  const stale =
    everSucceeded && consecutiveFailures >= UNREACHABLE_AFTER_FAILURES;

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
    // Logout is best-effort here: a failed call (e.g. the page is unmounting
    // mid-flight) must not surface as an unhandled rejection or block the
    // redirect to the sign-in page.
    await logout().catch(() => {});
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
  //
  // The 500ms fallback timer is tracked in a ref and cleared on unmount so it
  // can't fire state updates on an unmounted page.
  const closeBlockedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null
  );
  useEffect(
    () => () => {
      if (closeBlockedTimerRef.current !== null) {
        clearTimeout(closeBlockedTimerRef.current);
      }
    },
    []
  );

  function handleClosePage() {
    setCloseBlocked(false);
    window.close();
    if (closeBlockedTimerRef.current !== null) {
      clearTimeout(closeBlockedTimerRef.current);
    }
    closeBlockedTimerRef.current = setTimeout(() => setCloseBlocked(true), 500);
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
      ) : unreachable ? (
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
                  <p className="text-center text-xs text-error">
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
          {stale && (
            <p className="border-t border-control-border bg-control-bg/40 px-6 py-2 text-center text-xs text-warning">
              {t("auth.device-login.stale-data")}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
