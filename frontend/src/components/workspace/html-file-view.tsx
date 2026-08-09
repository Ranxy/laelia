import { useEffect, useMemo, useRef } from "react";
import { buildHtmlPreviewDoc, randomId } from "@/lib/html-file";

// HtmlFileView renders a workspace html file inline in a sandboxed iframe.
// The bridge script inside intercepts link clicks (opened in a new tab) and
// keeps the preview from navigating away; no comment plumbing here — the
// workspace pane is read-only.
export function HtmlFileView({
  name,
  content,
}: {
  name: string;
  content: string;
}) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  // Per-open session secrets, stable for the life of this view.
  const nonce = useMemo(() => randomId(), []);
  const epoch = useMemo(() => randomId(), []);
  const srcDoc = useMemo(
    () => buildHtmlPreviewDoc(content, nonce),
    [content, nonce]
  );

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      const d = e.data;
      if (!d || typeof d !== "object") return;
      if (e.source !== iframeRef.current?.contentWindow) return;
      if (
        d.slockAcBridge !== 1 ||
        d.nonce !== nonce ||
        d.documentEpoch !== epoch
      )
        return;
      if (d.type === "link-clicked") {
        const href = String(d.href ?? "");
        if (href) window.open(href, "_blank", "noopener,noreferrer");
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [nonce, epoch]);

  return (
    <iframe
      ref={iframeRef}
      title={name}
      sandbox="allow-scripts"
      srcDoc={srcDoc}
      referrerPolicy="no-referrer"
      onLoad={() =>
        iframeRef.current?.contentWindow?.postMessage(
          {
            slockAcBridge: 1,
            nonce,
            documentEpoch: epoch,
            type: "activate-document",
          },
          "*"
        )
      }
      className="h-full w-full border-0 bg-white"
    />
  );
}
