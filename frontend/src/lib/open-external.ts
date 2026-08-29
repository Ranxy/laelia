// The preview iframe bridge forwards links clicked inside untrusted documents
// (user uploads + agent output) to the parent window via postMessage. Those
// hrefs are attacker-controlled, so opening them must go through a strict
// scheme allow-list: javascript:/data:/vbscript: etc. would otherwise widen
// the script-execution surface of the app itself.
export function safeOpenExternal(href: string): boolean {
  if (!href.trim()) return false;
  let url: URL;
  try {
    url = new URL(href, window.location.href);
  } catch {
    return false;
  }
  if (
    url.protocol !== "http:" &&
    url.protocol !== "https:" &&
    url.protocol !== "mailto:"
  ) {
    return false;
  }
  window.open(url.href, "_blank", "noopener,noreferrer");
  return true;
}
