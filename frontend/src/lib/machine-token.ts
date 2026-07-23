import { formatToken, getManagerURL } from "./agent-token";

// buildMachineRunCommand assembles the full machine bootstrap command for
// copy-to-clipboard. A machine authenticates once with its registration token;
// the machine app then hosts every agent bound to it (no per-agent token).
export function buildMachineRunCommand(token: string, masked = true): string {
  return `laelia-machine run --manager ${getManagerURL()} --token ${
    masked ? formatToken(token) : token
  }`;
}
