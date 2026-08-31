import { useMemo } from "react";
import { useHtmlPreviewBridge } from "@/components/preview/html-preview-bridge";
import { buildHtmlPreviewDoc } from "@/lib/html-file";
import { safeOpenExternal } from "@/lib/open-external";

// HtmlFileView renders a workspace html file inline in a sandboxed iframe.
// The bridge script inside intercepts link clicks (opened in a new tab) and
// keeps the preview from navigating away; no comment plumbing here — the
// workspace pane is read-only. Parent-side message handling and the per-open
// session secrets come from the shared useHtmlPreviewBridge hook.
export function HtmlFileView({
  name,
  content,
}: {
  name: string;
  content: string;
}) {
  const bridge = useHtmlPreviewBridge({
    onLinkClick: (href) => {
      // Bridge payloads come from untrusted preview documents; only
      // allow-listed schemes may reach window.open.
      if (!safeOpenExternal(href)) {
        console.warn("[html-file-view] blocked link with rejected scheme");
      }
    },
  });
  const srcDoc = useMemo(
    () => buildHtmlPreviewDoc(content, bridge.nonce),
    [content, bridge.nonce]
  );

  return (
    <iframe
      ref={bridge.iframeRef}
      title={name}
      sandbox="allow-scripts"
      srcDoc={srcDoc}
      referrerPolicy="no-referrer"
      onLoad={bridge.activate}
      className="h-full w-full border-0 bg-white"
    />
  );
}
