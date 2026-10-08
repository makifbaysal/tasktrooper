import { ANNOTATION_CONTEXT_MAX, TASK_QUESTION_ANSWER_MAX } from "@/api";

/**
 * Agent-written HTML is rendered in an `<iframe sandbox="allow-scripts">` —
 * never with `allow-same-origin`. Without it the frame's origin is opaque, so
 * even a script that did run there could not read this page, its storage or
 * the API token the desktop shell hands it. The CSP below then makes sure the
 * only script that runs at all is our own nonce'd one, and that the document
 * cannot load anything from the network except images.
 */
export const FRAME_SANDBOX = "allow-scripts";

export function frameCsp(nonce: string): string {
  return [
    "default-src 'none'",
    "style-src 'unsafe-inline'",
    "img-src data: https:",
    "font-src data:",
    `script-src 'nonce-${nonce}'`,
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ");
}

export function createNonce(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

const HIGHLIGHT_CSS = `
mark[data-tt-id]{background:rgba(250,204,21,.45)!important;color:inherit!important;padding:0!important}
mark[data-tt-id]{border-radius:2px;cursor:pointer;box-decoration-break:clone;-webkit-box-decoration-break:clone}
mark[data-tt-id][data-tt-status="submitted"]{background:rgba(59,130,246,.3)!important}
mark[data-tt-id][data-tt-status="resolved"]{background:rgba(34,197,94,.2)!important;opacity:.75}
mark[data-tt-id][data-tt-active="true"]{outline:2px solid rgb(249,115,22)!important;outline-offset:1px}
`;

// Design documents only (see canvasRuntime.ts): the frame never scrolls, the
// body is moved by a transform from its top-left corner. The overflow and
// scroll-behavior overrides are there because a document that clips or
// scrolls itself would otherwise fight every drag.
const CANVAS_CSS = `
html[data-tt-canvas]{overflow:hidden!important;overscroll-behavior:none;scroll-behavior:auto!important}
html[data-tt-canvas]>body{overflow:visible!important;transform-origin:0 0!important}
html[data-tt-canvas].tt-grab,html[data-tt-canvas].tt-grab *{cursor:grab!important}
html[data-tt-canvas].tt-panning,html[data-tt-canvas].tt-panning *{cursor:grabbing!important;user-select:none!important;-webkit-user-select:none!important}
`;

/** The frame body element the Open questions section is mounted in, always present, empty when there are none. */
export const OPEN_QUESTIONS_ID = "tt-questions";

// Self-contained: the agent's document may carry no theming at all (or the
// wrong one), so this reads the OS/app color scheme on its own rather than
// trusting anything the host page sets on <html>.
const QUESTIONS_CSS = `
#${OPEN_QUESTIONS_ID}{--tt-q-bg:#f8fafc;--tt-q-border:#cbd5e1;--tt-q-fg:#0f172a;--tt-q-muted:#475569;--tt-q-accent:#b45309;--tt-q-accent-bg:#fef3c7}
#${OPEN_QUESTIONS_ID}{box-sizing:border-box;margin:0 0 1.5em;padding:1em 1.25em;border:1px solid var(--tt-q-border);border-radius:10px;background:var(--tt-q-bg);color:var(--tt-q-fg);font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif}
#${OPEN_QUESTIONS_ID}:empty,#${OPEN_QUESTIONS_ID}[hidden]{display:none}
#${OPEN_QUESTIONS_ID} *{box-sizing:border-box}
#${OPEN_QUESTIONS_ID} .tt-q-heading{margin:0 0 .75em;font-size:1em;font-weight:600}
#${OPEN_QUESTIONS_ID} .tt-q{margin:0 0 .85em;padding:.75em .9em;border:1px solid var(--tt-q-border);border-radius:8px;background:rgba(255,255,255,.6)}
#${OPEN_QUESTIONS_ID} .tt-q:last-child{margin-bottom:0}
#${OPEN_QUESTIONS_ID} .tt-q-head{display:flex;flex-wrap:wrap;align-items:center;gap:.4em;margin:0 0 .4em}
#${OPEN_QUESTIONS_ID} .tt-q-key{font-weight:700;font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
#${OPEN_QUESTIONS_ID} .tt-q-badge{display:inline-block;padding:.1em .55em;border-radius:999px;font-size:.75em;font-weight:600;background:rgba(15,23,42,.08);color:var(--tt-q-muted)}
#${OPEN_QUESTIONS_ID} .tt-q-badge--blocking{background:var(--tt-q-accent-bg);color:var(--tt-q-accent)}
#${OPEN_QUESTIONS_ID} .tt-q-prompt{margin:0 0 .5em;white-space:pre-wrap}
#${OPEN_QUESTIONS_ID} .tt-q-recommended{margin:0 0 .5em;color:var(--tt-q-muted)}
#${OPEN_QUESTIONS_ID} .tt-q-answer-wrap{margin-top:.25em}
#${OPEN_QUESTIONS_ID} .tt-q-answer-label{display:block;margin:0 0 .3em;font-size:.8em;font-weight:600;color:var(--tt-q-muted)}
#${OPEN_QUESTIONS_ID} textarea.tt-q-answer{display:block;width:100%;min-height:4.5em;resize:vertical;padding:.5em .6em;border:1px solid var(--tt-q-border);border-radius:6px;background:#fff;color:var(--tt-q-fg);font:inherit}
#${OPEN_QUESTIONS_ID} textarea.tt-q-answer:focus{outline:2px solid var(--tt-q-accent);outline-offset:1px}
#${OPEN_QUESTIONS_ID} .tt-q-answer-static{margin:0;white-space:pre-wrap}
#${OPEN_QUESTIONS_ID} .tt-q-answer-static--empty{color:var(--tt-q-muted);font-style:italic}
@media (prefers-color-scheme:dark){
#${OPEN_QUESTIONS_ID}{--tt-q-bg:#1e293b;--tt-q-border:#334155;--tt-q-fg:#e2e8f0;--tt-q-muted:#94a3b8;--tt-q-accent:#fbbf24;--tt-q-accent-bg:rgba(251,191,36,.18)}
#${OPEN_QUESTIONS_ID} .tt-q{background:rgba(15,23,42,.4)}
#${OPEN_QUESTIONS_ID} .tt-q-badge{background:rgba(226,232,240,.12)}
#${OPEN_QUESTIONS_ID} textarea.tt-q-answer{background:#0f172a;color:var(--tt-q-fg)}
}
`;

// Removed rather than trusted: a document's own CSP intersects with ours and
// could block the runtime, a `refresh` could navigate the frame away, a
// `<base>` would re-point links, `<link>` can preconnect outside CSP's reach,
// and `<noscript>` parses differently here (scripting off) than in the frame
// (scripting on) — the classic way to smuggle markup through a re-serialize.
const STRIPPED = "meta[http-equiv], base, link, noscript";

/**
 * Turns a document into the frame's srcdoc: parsed inertly (DOMParser runs no
 * script and fetches nothing), stripped of the elements above, then given the
 * CSP meta as the very first element of `<head>` — so it governs everything
 * after it — our highlight styles, and our runtime as the last element of
 * `<body>`. Scripts the document carries stay in place but, lacking the nonce,
 * are refused by the policy.
 */
export function buildAnalysisSrcdoc(
  html: string,
  options: { nonce: string; script: string; canvas?: boolean },
): string {
  const parsed = new DOMParser().parseFromString(html, "text/html");
  for (const element of Array.from(parsed.querySelectorAll(STRIPPED))) element.remove();
  for (const element of Array.from(parsed.querySelectorAll("[nonce]"))) element.removeAttribute("nonce");

  const head = parsed.head;
  const csp = parsed.createElement("meta");
  csp.setAttribute("http-equiv", "Content-Security-Policy");
  csp.setAttribute("content", frameCsp(options.nonce));
  head.insertBefore(csp, head.firstChild);

  const style = parsed.createElement("style");
  style.textContent = HIGHLIGHT_CSS + QUESTIONS_CSS + (options.canvas ? CANVAS_CSS : "");
  head.appendChild(style);
  if (options.canvas) parsed.documentElement.setAttribute("data-tt-canvas", "");

  // Always present so the runtime never has to create it later — just filled
  // (or hidden) once the page posts `tt:questions`. Excluded from text-quote
  // collection (textQuote.ts honours `data-tt-skip`) so it can never anchor an
  // annotation and a selection inside it is never reported.
  const questions = parsed.createElement("section");
  questions.id = OPEN_QUESTIONS_ID;
  questions.setAttribute("data-tt-skip", "");
  questions.setAttribute("aria-live", "polite");
  questions.hidden = true;
  parsed.body.insertBefore(questions, parsed.body.firstChild);

  const script = parsed.createElement("script");
  script.setAttribute("nonce", options.nonce);
  script.textContent = options.script;
  parsed.body.appendChild(script);

  return `<!DOCTYPE html>${parsed.documentElement.outerHTML}`;
}

const MARKDOWN_CSS = `
:root{color-scheme:light;--fg:#1f2328;--muted:#59636e;--border:#d1d9e0;--code:#f6f8fa;--link:#0969da;--bg:#ffffff}
:root.dark{color-scheme:dark;--fg:#e6edf3;--muted:#9198a1;--border:#3d444d;--code:#151b23;--link:#4493f8;--bg:#0d1117}
html,body{margin:0;background:var(--bg);color:var(--fg)}
body{font:15px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif}
main{max-width:52rem;margin:0 auto;padding:2rem 2.5rem 4rem}
h1,h2,h3,h4{line-height:1.3;margin:1.6em 0 .6em}
h1{font-size:1.8em;border-bottom:1px solid var(--border);padding-bottom:.3em}
h2{font-size:1.4em;border-bottom:1px solid var(--border);padding-bottom:.3em}
h3{font-size:1.15em}
p,ul,ol,blockquote,pre,table{margin:0 0 1em}
ul,ol{padding-left:1.6em}
li+li{margin-top:.25em}
a{color:var(--link)}
code{font:.9em ui-monospace,SFMono-Regular,Menlo,monospace;background:var(--code);padding:.15em .35em;border-radius:4px}
pre{background:var(--code);padding:1em;border-radius:6px;overflow:auto}
pre code{background:none;padding:0}
blockquote{border-left:4px solid var(--border);color:var(--muted);padding:0 1em;margin-left:0}
table{border-collapse:collapse;display:block;overflow:auto}
th,td{border:1px solid var(--border);padding:.4em .8em;text-align:left}
hr{border:0;border-top:1px solid var(--border);margin:2em 0}
img{max-width:100%}
`;

export function markdownFrameHtml(bodyHtml: string, theme: "light" | "dark"): string {
  const open = `<!DOCTYPE html><html class="${theme === "dark" ? "dark" : ""}">`;
  return `${open}<head><style>${MARKDOWN_CSS}</style></head><body><main>${bodyHtml}</main></body></html>`;
}

export interface FrameSelection {
  quote: string;
  prefix: string;
  suffix: string;
}

/** One open question, as drawn inside the frame's Open questions section. */
export interface FrameQuestion {
  id: string;
  key: string;
  kind: "product" | "technical";
  blocking: boolean;
  prompt: string;
  recommendedAnswer: string;
  answer: string;
  status: "open" | "answered" | "withdrawn";
  /** Whether this question's answer box is an editable `<textarea>` or static text. */
  editable: boolean;
}

/** Every piece of display text the section needs, translated by the host before it crosses into the frame. */
export interface QuestionsLabels {
  heading: string;
  kindProduct: string;
  kindTechnical: string;
  blocking: string;
  recommendedPrefix: string;
  answerLabel: string;
  answerPlaceholder: string;
  unanswered: string;
  recommendedStands: string;
}

export type FrameMessage =
  | { type: "tt:ready" }
  | ({ type: "tt:selection"; rect: { top: number; left: number; width: number; height: number } } & FrameSelection)
  | { type: "tt:anchored"; results: { id: string; found: boolean }[] }
  | { type: "tt:focus"; id: string }
  | { type: "tt:answer"; id: string; text: string }
  | { type: "tt:zoom"; zoom: number }
  | { type: "tt:outline"; pages: CanvasPage[] }
  | { type: "tt:page"; id: string | null; cause: "view" | "goto" };

/** One page of a design canvas: a `<h2>` and what it heads (see canvasRuntime.ts). */
export interface CanvasPage {
  id: string;
  title: string;
}

const QUOTE_LIMIT = 20000;
const ZOOM_MIN = 0.05;
const ZOOM_MAX = 10;
const PAGES_LIMIT = 100;
const PAGE_TITLE_LIMIT = 200;
const ID_LIMIT = 200;

function isId(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= ID_LIMIT;
}

function isContext(value: unknown): value is string {
  return typeof value === "string" && value.length <= ANNOTATION_CONTEXT_MAX;
}

function toRect(value: unknown): { top: number; left: number; width: number; height: number } {
  const v = (value && typeof value === "object" ? value : {}) as Record<string, unknown>;
  const num = (x: unknown) => (typeof x === "number" && Number.isFinite(x) ? x : 0);
  return { top: num(v.top), left: num(v.left), width: num(v.width), height: num(v.height) };
}

/**
 * The frame is untrusted even though its only live script is ours: anything
 * it posts is checked field by field and rebuilt, never passed through.
 */
export function parseFrameMessage(data: unknown): FrameMessage | null {
  if (!data || typeof data !== "object") return null;
  const d = data as Record<string, unknown>;
  switch (d.type) {
    case "tt:ready":
      return { type: "tt:ready" };
    case "tt:selection":
      if (typeof d.quote !== "string" || !d.quote.trim() || d.quote.length > QUOTE_LIMIT) return null;
      if (!isContext(d.prefix) || !isContext(d.suffix)) return null;
      return { type: "tt:selection", quote: d.quote, prefix: d.prefix, suffix: d.suffix, rect: toRect(d.rect) };
    case "tt:anchored":
      if (!Array.isArray(d.results)) return null;
      return {
        type: "tt:anchored",
        results: d.results
          .filter((r): r is { id: string; found: boolean } =>
            Boolean(r) && typeof r === "object" && isId(r.id) && typeof r.found === "boolean",
          )
          .map((r) => ({ id: r.id, found: r.found })),
      };
    case "tt:focus":
      return isId(d.id) ? { type: "tt:focus", id: d.id } : null;
    case "tt:answer":
      if (!isId(d.id) || typeof d.text !== "string" || d.text.length > TASK_QUESTION_ANSWER_MAX) return null;
      return { type: "tt:answer", id: d.id, text: d.text };
    case "tt:zoom":
      if (typeof d.zoom !== "number" || !Number.isFinite(d.zoom) || d.zoom < ZOOM_MIN || d.zoom > ZOOM_MAX) return null;
      return { type: "tt:zoom", zoom: d.zoom };
    case "tt:outline":
      if (!Array.isArray(d.pages)) return null;
      return {
        type: "tt:outline",
        pages: d.pages
          .filter(
            (p): p is CanvasPage =>
              Boolean(p) &&
              typeof p === "object" &&
              isId(p.id) &&
              typeof p.title === "string" &&
              p.title.trim().length > 0 &&
              p.title.length <= PAGE_TITLE_LIMIT,
          )
          .slice(0, PAGES_LIMIT)
          .map((p) => ({ id: p.id, title: p.title })),
      };
    case "tt:page":
      if (d.id !== null && !isId(d.id)) return null;
      if (d.cause !== "view" && d.cause !== "goto") return null;
      return { type: "tt:page", id: d.id, cause: d.cause };
    default:
      return null;
  }
}
