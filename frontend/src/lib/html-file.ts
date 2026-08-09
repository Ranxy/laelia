import type { CommentAnchor } from "@/lib/markdown-file";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// 10 MiB: same ceiling as markdown preview. The threshold is checked against
// attachment.sizeBytes before fetching, so an oversized file never triggers a
// download just to be refused.
export const MAX_HTML_PREVIEW_BYTES = 10n * 1024n * 1024n;

const HTML_NAME_RE = /\.(html?|xhtml)$/i;
const HTML_MIME = new Set(["text/html", "application/xhtml+xml"]);

// isHtmlAttachment reports whether the attachment is plausibly HTML, judged
// by name extension first (uploads often carry no mime type) and then by the
// declared mime type.
export function isHtmlAttachment(att: Attachment): boolean {
  if (att.mimeType && HTML_MIME.has(att.mimeType)) return true;
  return HTML_NAME_RE.test(att.name ?? "");
}

// isHtmlPreviewable is true only when the file is HTML AND within the
// in-browser preview size limit. Oversized HTML still offers download.
export function isHtmlPreviewable(att: Attachment): boolean {
  return (
    isHtmlAttachment(att) && (att.sizeBytes ?? 0n) <= MAX_HTML_PREVIEW_BYTES
  );
}

export const MAX_HTML_QUOTE_CHARS = 500;
export const MAX_HTML_ANCHOR_LABEL_CHARS = 60;

// randomId returns a per-open session secret (nonce / document epoch) for the
// preview bridge. crypto.randomUUID needs a secure context; fall back to a
// time+random string so previews still work on http hosts.
export function randomId(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

// HTML files have no heading structure to anchor comments to, so a comment's
// section_id carries a content-space locate hint: "html:y:{y}" where y is the
// content y of the selection at comment time. Re-opening the preview re-locates
// the anchor by matching the stored quoted text, preferring the occurrence
// nearest to y. The "html:" prefix keeps html anchors distinguishable from
// markdown heading ids (which are plain DOM ids).
export const HTML_ANCHOR_PREFIX = "html:y:";

export function htmlAnchorForSelection(
  quotedText: string,
  y: number
): CommentAnchor | null {
  const text = quotedText
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, MAX_HTML_QUOTE_CHARS);
  if (!text || !Number.isFinite(y)) return null;
  return {
    sectionId: `${HTML_ANCHOR_PREFIX}${Math.round(y)}`,
    sectionAnchor: text.slice(0, MAX_HTML_ANCHOR_LABEL_CHARS),
    quotedText: text,
  };
}

// parseHtmlAnchor extracts the stored content-y from a comment's section_id.
// Returns null when the id is not an html anchor spec.
export function parseHtmlAnchor(sectionId: string): { y: number } | null {
  if (!sectionId.startsWith(HTML_ANCHOR_PREFIX)) return null;
  const y = Number(sectionId.slice(HTML_ANCHOR_PREFIX.length));
  return Number.isFinite(y) ? { y } : null;
}

// Bridge script injected into the previewed document. It is the only channel
// between the sandboxed iframe (opaque origin, sandbox="allow-scripts") and
// the parent overlay. Every outgoing message carries the per-open nonce and
// the parent-issued document epoch; the bridge sends nothing until the parent
// activates the document with a valid epoch, so a document that navigates
// itself away can never speak for the original. All payloads are capped
// because the document content is untrusted.
const BRIDGE_NONCE_PLACEHOLDER = "__AC_BRIDGE_NONCE__";

const htmlBridgeSource = `(function () {
  "use strict";
  try {
    if (window.parent === window) return;
    var nonce = "${BRIDGE_NONCE_PLACEHOLDER}";
    if (!nonce) return;
    var documentEpoch = null;
    var MAX_QUOTE = 500;
    var MAX_HREF = 4097;

    function normalize(value) {
      return (typeof value === "string" ? value : "").replace(/\\s+/g, " ").trim();
    }

    function post(msg) {
      try {
        if (!documentEpoch) return;
        msg.slockAcBridge = 1;
        msg.nonce = nonce;
        msg.documentEpoch = documentEpoch;
        window.parent.postMessage(msg, "*");
      } catch (err) {}
    }

    // --- state: scroll + viewport, rAF-throttled ---
    var pendingState = false;
    function sendState() {
      if (pendingState) return;
      pendingState = true;
      requestAnimationFrame(function () {
        pendingState = false;
        var de = document.documentElement;
        var b = document.body;
        post({
          type: "state",
          scrollX: window.scrollX,
          scrollY: window.scrollY,
          docWidth: Math.max(de ? de.scrollWidth : 0, b ? b.scrollWidth : 0),
          docHeight: Math.max(de ? de.scrollHeight : 0, b ? b.scrollHeight : 0),
          viewportWidth: window.innerWidth,
          viewportHeight: window.innerHeight
        });
      });
    }
    window.addEventListener("scroll", sendState, { passive: true });
    window.addEventListener("resize", sendState, { passive: true });
    if (typeof MutationObserver === "function") {
      new MutationObserver(sendState).observe(document.documentElement, {
        attributes: true,
        childList: true,
        characterData: true,
        subtree: true
      });
    }
    if (typeof ResizeObserver === "function") {
      new ResizeObserver(sendState).observe(document.documentElement);
    }
    document.addEventListener("DOMContentLoaded", sendState);
    window.addEventListener("load", sendState);

    // --- selection reporting (native text selection inside the iframe) ---
    var reportedSelection = false;
    function reportSelection() {
      var payload = null;
      try {
        var sel = window.getSelection();
        if (sel && !sel.isCollapsed && sel.rangeCount > 0) {
          var range = sel.getRangeAt(0);
          var text = normalize(sel.toString());
          var rect = range.getBoundingClientRect();
          if (text && rect && rect.width > 0 && rect.height > 0) {
            payload = {
              type: "selection",
              text: text.slice(0, MAX_QUOTE),
              x: Math.round(rect.left + window.scrollX),
              y: Math.round(rect.top + window.scrollY),
              w: Math.round(rect.width),
              h: Math.round(rect.height)
            };
          }
        }
      } catch (err) { payload = null; }
      if (payload) {
        reportedSelection = true;
        post(payload);
      } else if (reportedSelection) {
        reportedSelection = false;
        post({ type: "selection-cleared" });
      }
    }
    document.addEventListener("mouseup", reportSelection);
    // Keyboard selection (shift+arrows) fires no mouseup; debounce it.
    var selectionTimer = null;
    document.addEventListener("selectionchange", function () {
      if (selectionTimer) clearTimeout(selectionTimer);
      selectionTimer = setTimeout(reportSelection, 200);
    });

    // --- anchor relocation: stored quote text -> content rect ---
    function locateQuote(quote, nearY) {
      var needle = normalize(quote);
      if (!needle) return null;
      try {
        var root = document.body || document.documentElement;
        if (!root) return null;
        var walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT, null);
        var el, candidates = [], seen = 0;
        while ((el = walker.nextNode()) && seen < 5000) {
          seen++;
          var text = normalize(el.textContent || "");
          if (!text || text.indexOf(needle) === -1) continue;
          candidates.push(el);
        }
        // Minimal containers only: every ancestor of a match also matches, so
        // drop any candidate that contains another candidate. What is left is
        // one element per distinct occurrence of the quote.
        var minimal = [];
        for (var i = 0; i < candidates.length; i++) {
          var isMinimal = true;
          for (var j = 0; j < candidates.length; j++) {
            if (i !== j && candidates[i].contains(candidates[j])) {
              isMinimal = false;
              break;
            }
          }
          if (isMinimal) minimal.push(candidates[i]);
        }
        // Duplicate quotes: prefer the occurrence nearest to the stored y.
        var best = null;
        var bestScore = Infinity;
        var hasNearY = typeof nearY === "number" && isFinite(nearY);
        for (var k = 0; k < minimal.length; k++) {
          var r = minimal[k].getBoundingClientRect();
          if (!r || (r.width <= 0 && r.height <= 0)) continue;
          var centerY = r.top + window.scrollY + r.height / 2;
          var score = hasNearY
            ? Math.abs(centerY - nearY)
            : normalize(minimal[k].textContent || "").length;
          if (score < bestScore) { bestScore = score; best = minimal[k]; }
        }
        if (best) {
          var br = best.getBoundingClientRect();
          return {
            x: Math.round(br.left + window.scrollX),
            y: Math.round(br.top + window.scrollY),
            w: Math.round(br.width),
            h: Math.round(br.height)
          };
        }
      } catch (err) {}
      return null;
    }

    // --- inbound messages ---
    window.addEventListener("message", function (ev) {
      var d = ev && ev.data;
      if (!d || d.slockAcBridge !== 1 || d.nonce !== nonce) return;
      if (d.type === "activate-document") {
        var next = typeof d.documentEpoch === "string" ? d.documentEpoch : "";
        if (!next || next.length > 200) return;
        documentEpoch = next;
        sendState();
        return;
      }
      if (!documentEpoch || d.documentEpoch !== documentEpoch) return;
      if (d.type === "scroll-to") {
        var x = Number(d.x);
        var y = Number(d.y);
        if (!isFinite(x) || !isFinite(y)) return;
        try { window.scrollTo({ left: x, top: y, behavior: "smooth" }); }
        catch (err) { window.scrollTo(x, y); }
        return;
      }
      if (d.type === "locate") {
        var nearY = Number(d.nearY);
        var hit = locateQuote(
          typeof d.quote === "string" ? d.quote : "",
          isFinite(nearY) ? nearY : null
        );
        post({
          type: "located",
          requestId: String(d.requestId || ""),
          x: hit ? hit.x : -1,
          y: hit ? hit.y : -1,
          w: hit ? hit.w : 0,
          h: hit ? hit.h : 0
        });
      }
    });

    // --- link interception: the preview must never navigate away ---
    document.addEventListener("click", function (e) {
      try {
        var target = e.target;
        var anchor = target && target.closest ? target.closest("a[href]") : null;
        if (!anchor) return;
        var href = anchor.getAttribute("href");
        if (typeof href !== "string" || !href.trim()) return;
        e.preventDefault();
        e.stopPropagation();
        post({
          type: "link-clicked",
          href: href.slice(0, MAX_HREF),
          text: normalize(anchor.textContent || "").slice(0, 200)
        });
      } catch (err) {}
    }, true);

    // Forms are blocked by the sandbox anyway; never let one navigate.
    document.addEventListener("submit", function (e) {
      e.preventDefault();
    }, true);

    // Esc inside the preview closes the parent overlay.
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") post({ type: "esc" });
    }, true);
  } catch (err) {}
})();`;

// buildHtmlPreviewDoc wraps downloaded HTML text into a document safe to set
// as iframe srcDoc, injecting the bridge script (with the per-open nonce):
//   - full documents (<html>…</html>): bridge goes right before </head>, or
//     right after the <head>/<html> open tag when head/body tags are absent;
//   - fragments: wrapped in a minimal document;
//   - a utf-8 charset meta is added when the document does not declare one so
//     the sandboxed render matches the UTF-8 text we decoded.
export function buildHtmlPreviewDoc(content: string, nonce: string): string {
  const script = `<script data-ac-bridge="1">${htmlBridgeSource.replace(BRIDGE_NONCE_PLACEHOLDER, nonce)}</script>`;
  const charset = `<meta charset="utf-8">`;
  if (/<html[\s>]/i.test(content)) {
    let doc = content;
    if (!/<meta\s+charset/i.test(doc)) {
      // Keep the original tag casing when injecting after <head>.
      doc = doc.replace(/(<head[^>]*>)/i, (_m, tag) => `${tag}${charset}`);
    }
    if (/<\/head>/i.test(doc)) {
      return doc.replace(/(<\/head>)/i, (m) => `${script}${m}`);
    }
    if (/<body[\s>]/i.test(doc)) {
      return doc.replace(/(<body[^>]*>)/i, (_m, tag) => `${script}${tag}`);
    }
    return doc.replace(/(<html[^>]*>)/i, (_m, tag) => `${tag}${script}`);
  }
  return `<!doctype html><html><head>${charset}${script}</head><body>${content}</body></html>`;
}
