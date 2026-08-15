import { getManagerURL } from "./agent-token";

export type MachineInstallOS = "linux" | "macos" | "windows";

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
