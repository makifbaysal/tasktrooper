import type { RunTraceCategory, RunTraceEvent, RunTraceSummary } from "@/api";

export const TRACE_CATEGORIES: RunTraceCategory[] = ["read", "change", "exec", "external", "board"];

type Detail = Record<string, unknown>;

const detailOf = (event: RunTraceEvent): Detail => event.detail ?? {};

export function detailString(event: RunTraceEvent, key: string): string {
  const v = detailOf(event)[key];
  return typeof v === "string" ? v : "";
}

export function detailNumber(event: RunTraceEvent, key: string): number | null {
  const v = detailOf(event)[key];
  return typeof v === "number" ? v : null;
}

export function detailLines(event: RunTraceEvent, key: string): string[] {
  const v = detailOf(event)[key];
  return Array.isArray(v) ? v.filter((l): l is string => typeof l === "string") : [];
}

export function detailEffect(event: RunTraceEvent): string {
  return detailString(event, "effect");
}

/** "a.go:10-49" for a read with a range, the bare target otherwise. */
export function readLabel(event: RunTraceEvent): string {
  const target = event.target ?? "";
  const offset = detailNumber(event, "offset");
  const limit = detailNumber(event, "limit");
  const start = detailNumber(event, "start_line");
  const end = detailNumber(event, "end_line");
  if (start && end) return `${target}:${start}-${end}`;
  if (offset != null && limit) return `${target}:${offset + 1}-${offset + limit}`;
  if (limit) return `${target}:1-${limit}`;
  return target;
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)} s`;
  return `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`;
}

export function formatSizeDelta(bytes: number): string {
  const sign = bytes > 0 ? "+" : bytes < 0 ? "-" : "";
  const abs = Math.abs(bytes);
  if (abs < 1024) return `${sign}${abs} B`;
  return `${sign}${(abs / 1024).toFixed(1)} KB`;
}

export function shortSha(sha?: string): string {
  return sha ? sha.slice(0, 10) : "";
}

export function categoryCount(summary: RunTraceSummary, category: RunTraceCategory): number {
  return summary.counts[category] ?? 0;
}

export const escapeHtml = (value: string): string =>
  value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");

export interface TraceHtmlLabels {
  lang: string;
  title: string;
  generated: string;
  counts: string;
  untouched: string;
  verifiedFiles: string;
  unrecordedTitle: string;
  unrecordedBody: string;
  nonUndoable: string;
  timeline: string;
  noEvents: string;
  commandLabel: string;
  exitLabel: string;
  failed: string;
  category: Record<RunTraceCategory, string>;
}

function eventLines(event: RunTraceEvent, labels: TraceHtmlLabels): string[] {
  const out: string[] = [];
  if (event.category === "exec") {
    const command = detailString(event, "command");
    if (command) out.push(`${labels.commandLabel}: ${command}`);
    const exit = detailNumber(event, "exit_code");
    if (exit != null) out.push(`${labels.exitLabel}: ${exit}`);
    for (const l of detailLines(event, "output_head")) out.push(`  ${l}`);
    const tail = detailLines(event, "output_tail");
    if (tail.length > 0) {
      out.push("  …");
      for (const l of tail) out.push(`  ${l}`);
    }
  } else if (event.category === "change") {
    const before = detailString(event, "hash_before");
    const after = detailString(event, "hash_after");
    if (before || after) out.push(`${before.slice(0, 12) || "-"} → ${after.slice(0, 12) || "-"}`);
    const delta = detailNumber(event, "size_delta");
    if (delta != null) out.push(formatSizeDelta(delta));
  }
  if (event.error) out.push(event.error);
  return out;
}

/**
 * One self-contained HTML page for a run's trace: no script, no external
 * resource, every dynamic string escaped. It follows the reader's light/dark
 * preference.
 */
export function buildTraceHtml(summary: RunTraceSummary, events: RunTraceEvent[], labels: TraceHtmlLabels): string {
  const e = escapeHtml;
  const v = summary.verification;
  const rows = events
    .map((ev) => {
      const target = ev.category === "read" ? readLabel(ev) : (ev.target ?? "");
      const lines = eventLines(ev, labels);
      const status = ev.status === "ok" ? "" : ` <span class="bad">${e(labels.failed)}</span>`;
      return `<tr><td class="n">${ev.seq}</td><td>${e(labels.category[ev.category] ?? ev.category)}</td><td><code>${e(ev.tool)}</code> ${e(target)}${status}${
        lines.length ? `<pre>${e(lines.join("\n"))}</pre>` : ""
      }</td><td class="n">${e(formatDuration(ev.duration_ms))}</td><td class="n">${e(new Date(ev.at).toISOString())}</td></tr>`;
    })
    .join("");
  const files = (v?.files ?? [])
    .map((f) => `<li><code>${e(f.status)}</code> ${e(f.path)}${f.from ? ` <small>(${e(f.from)})</small>` : ""}</li>`)
    .join("");
  const unrecorded = v && v.unrecorded.length > 0
    ? `<section class="warn"><h2>${e(labels.unrecordedTitle)}</h2><p>${e(labels.unrecordedBody)}</p><ul>${v.unrecorded
        .map((p) => `<li>${e(p)}</li>`)
        .join("")}</ul></section>`
    : "";
  const effects = summary.non_undoable.length
    ? `<section><h2>${e(labels.nonUndoable)}</h2><ul>${summary.non_undoable
        .map((x) => `<li><code>${e(x.tool)}</code> ${e(x.kind)}${x.target ? ` · ${e(x.target)}` : ""}</li>`)
        .join("")}</ul></section>`
    : "";
  return `<!doctype html>
<html lang="${e(labels.lang)}"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light dark">
<title>${e(labels.title)}</title>
<style>
:root{--bg:#fff;--fg:#1b1b1f;--muted:#666a73;--line:#dcdde2;--warn:#fff4dc;--warnfg:#6b4a00;--bad:#b3261e;--code:#f3f4f7}
@media (prefers-color-scheme:dark){:root{--bg:#16171a;--fg:#e8e8ec;--muted:#9a9ea8;--line:#33353b;--warn:#3b2f12;--warnfg:#f3d68a;--bad:#ff8a80;--code:#222428}}
body{background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,sans-serif;margin:0;padding:24px;max-width:1000px;margin-inline:auto}
h1{font-size:20px}h2{font-size:15px;margin:20px 0 6px}p,small{color:var(--muted)}
table{border-collapse:collapse;width:100%}td{border-top:1px solid var(--line);padding:6px 8px;vertical-align:top}
.n{white-space:nowrap;color:var(--muted)}code,pre{background:var(--code);border-radius:4px}code{padding:0 4px}
pre{padding:8px;overflow:auto;white-space:pre-wrap;word-break:break-word;margin:6px 0 0;font-size:12px}
.warn{background:var(--warn);color:var(--warnfg);border-radius:8px;padding:8px 14px}.warn p{color:inherit}.bad{color:var(--bad);font-weight:600}
</style></head><body>
<h1>${e(labels.title)}</h1>
<p>${e(labels.generated)}</p>
<p>${e(labels.counts)}</p>
${v && v.available ? `<p>${e(labels.untouched)}</p>` : ""}
${unrecorded}
${files ? `<section><h2>${e(labels.verifiedFiles)}</h2><ul>${files}</ul></section>` : ""}
${effects}
<section><h2>${e(labels.timeline)}</h2>${
    rows ? `<table>${rows}</table>` : `<p>${e(labels.noEvents)}</p>`
  }</section>
</body></html>`;
}

export function saveTextFile(name: string, mime: string, text: string): void {
  const url = URL.createObjectURL(new Blob([text], { type: mime }));
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}
