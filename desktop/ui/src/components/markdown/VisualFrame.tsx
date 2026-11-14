import { useEffect, useMemo, useRef, useState } from "react";
import { createNonce, FRAME_SANDBOX } from "@/components/board/analysis/srcdoc";
import { useI18n } from "@/hooks/useI18n";

/**
 * A drawing an agent put in its reply (a ```visual fenced block: one
 * self-contained HTML or SVG document). Rendered the way analysis frames are:
 * a sandboxed iframe with an opaque origin, and a CSP under which the agent's
 * own scripts and every network load are blocked. The one script that runs is
 * ours (nonce'd) and only reports the document's height.
 */

const MIN_HEIGHT = 80;
const MAX_HEIGHT = 900;

function visualCsp(nonce: string): string {
  return [
    "default-src 'none'",
    "style-src 'unsafe-inline'",
    "img-src data:",
    "font-src data:",
    `script-src 'nonce-${nonce}'`,
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ");
}

export function visualSrcdoc(source: string, nonce: string, frameId: string): string {
  const body = /^\s*<svg[\s>]/i.test(source) ? `<div class="tt-svg">${source}</div>` : source;
  return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${visualCsp(nonce)}">
<style>
:root{color-scheme:light dark}
html,body{margin:0;padding:0;background:transparent;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif}
body{padding:12px;box-sizing:border-box;overflow-x:auto}
.tt-svg svg{max-width:100%;height:auto;display:block;margin:0 auto}
</style></head><body>${body}
<script nonce="${nonce}">
(function(){var last=0;function post(){var h=Math.ceil(document.documentElement.scrollHeight);if(h!==last){last=h;parent.postMessage({ttVisual:${JSON.stringify(frameId)},height:h},"*");}}
new ResizeObserver(post).observe(document.documentElement);window.addEventListener("load",post);post();})();
</script></body></html>`;
}

/** While a reply streams the block grows token by token; the frame reloads only once it holds still. */
const SETTLE_MS = 400;

export function VisualFrame({ source }: { source: string }) {
  const { t } = useI18n();
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(240);
  const [settled, setSettled] = useState(source);
  const frameId = useMemo(() => createNonce(), []);
  const srcdoc = useMemo(() => visualSrcdoc(settled, createNonce(), frameId), [settled, frameId]);

  useEffect(() => {
    if (source === settled) return;
    const timer = setTimeout(() => setSettled(source), SETTLE_MS);
    return () => clearTimeout(timer);
  }, [source, settled]);

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.source !== frameRef.current?.contentWindow) return;
      const data = e.data as { ttVisual?: string; height?: number } | null;
      if (!data || data.ttVisual !== frameId || typeof data.height !== "number") return;
      setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, data.height)));
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [frameId]);

  return (
    <figure className="not-prose my-3 overflow-hidden rounded-lg border border-border bg-background">
      <iframe
        ref={frameRef}
        title={t("chatArea.markdown.visual.title")}
        sandbox={FRAME_SANDBOX}
        srcDoc={srcdoc}
        style={{ height }}
        className="block w-full border-0"
      />
    </figure>
  );
}
