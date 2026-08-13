import { getManagerURL } from "./agent-token";

// buildMachineSetupCommand assembles the device-code setup command shown on
// the create-machine waiting page. The machine authenticates via the OAuth2
// device flow, so no token is embedded in the command.
export function buildMachineSetupCommand(): string {
  return `laelia-machine --manager ${getManagerURL()} setup`;
}
