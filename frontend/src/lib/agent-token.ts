// formatToken masks the middle of a bootstrap token for display, keeping the
// first and last few characters visible so an admin can confirm they copied the
// right token without exposing the full secret in screenshots.
export function formatToken(token: string): string {
  if (token.length <= 20) {
    return token.slice(0, 6) + "*".repeat(token.length - 6);
  }
  return `${token.slice(0, 10)}${"*".repeat(20)}${token.slice(-6)}`;
}

// getManagerURL returns the base URL agents/machines should connect back to,
// derived from the Vite API base or the current origin. Trailing slashes are
// stripped so the assembled `laelia-machine run --manager <url> --token <token>`
// command is valid.
export function getManagerURL(): string {
  return (import.meta.env.VITE_API_BASE_URL || window.location.origin).replace(
    /\/+$/,
    ""
  );
}
