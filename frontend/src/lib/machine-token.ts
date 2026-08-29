export type MachineInstallOS = "linux" | "macos" | "windows";

// getManagerURL returns the base URL agents/machines should connect back to,
// derived from the Vite API base or the current origin. Trailing slashes are
// stripped so the assembled install/setup commands are valid.
function getManagerURL(): string {
  return (import.meta.env.VITE_API_BASE_URL || window.location.origin).replace(
    /\/+$/,
    ""
  );
}

// machineInstallOSFromInfo maps the OS string reported by a machine (Go's
// runtime.GOOS: "linux", "darwin", "windows") to the install-command OS used
// by the new-machine page. Returns undefined when the OS is unknown.
export function machineInstallOSFromInfo(
  os: string | undefined
): MachineInstallOS | undefined {
  if (!os) return undefined;
  const normalized = os.toLowerCase();
  if (normalized.includes("win")) return "windows";
  if (normalized.includes("darwin") || normalized.includes("mac"))
    return "macos";
  if (normalized.includes("linux")) return "linux";
  return undefined;
}

// buildMachineSetupCommand assembles the device-code setup command shown on
// the create-machine waiting page. The machine authenticates via the OAuth2
// device flow, so no token is embedded in the command.
export function buildMachineSetupCommand(): string {
  return `laelia-machine --manager ${getManagerURL()} setup`;
}

// buildMachineInstallCommand assembles the one-line install command for the
// given OS. The manager injects its public URL into the served scripts, so the
// user does not need to pass any environment variables.
export function buildMachineInstallCommand(os: MachineInstallOS): string {
  const url = getManagerURL();
  if (os === "windows") {
    return `irm ${url}/machine/install.ps1 | iex`;
  }
  return `curl -fsSL ${url}/machine/install.sh | sh`;
}
