import type { AudienceReport, ReportTheme, StudyKind } from "@/api";
import { abRows, positionBiased, signed } from "@/lib/abComparison";
import { formatMoney, formatPct } from "@/lib/audienceHub";

type Translate = (key: string, params?: Record<string, string | number>) => string;

export interface StudyReportHtmlInput {
  report: AudienceReport;
  title: string;
  audienceName?: string;
  kind?: StudyKind;
  lang: string;
  t: Translate;
  generatedAt?: Date;
}

const ESCAPES: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

export function escapeHtml(value: unknown): string {
  return String(value ?? "").replace(/[&<>"']/g, (c) => ESCAPES[c]);
}

const CSS = `
:root{color-scheme:light dark;--bg:#f6f7f9;--card:#fff;--fg:#14171f;--muted:#5d6577;--line:#dfe3ea;--track:#e8ebf1;--primary:#3f5bd8;--good:#1f9d55;--warn:#c98a0b;--bad:#d64545;--info:#2b7bd6}
@media (prefers-color-scheme:dark){:root{--bg:#0f1218;--card:#181c25;--fg:#e8ebf2;--muted:#9aa3b6;--line:#2a3040;--track:#262c3a;--primary:#7e93f2;--good:#46c07c;--warn:#e2ad3b;--bad:#ea6b6b;--info:#5aa2ee}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:960px;margin:0 auto;padding:32px 20px 48px}
h1{font-size:28px;line-height:1.2;margin:4px 0 6px}
h2{font-size:18px;margin:0 0 4px}
h3{font-size:15px;margin:16px 0 6px}
p{margin:6px 0}
.eyebrow{font-size:12px;letter-spacing:.06em;text-transform:uppercase;color:var(--muted);margin:0}
.meta{color:var(--muted);font-size:13px}
.card{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:20px;margin:16px 0;break-inside:avoid}
.help{color:var(--muted);font-size:13px;margin:0 0 12px}
.hero{display:grid;grid-template-columns:minmax(0,240px) 1fr;gap:24px}
@media (max-width:640px){.hero{grid-template-columns:1fr}}
.score{font-size:48px;font-weight:600;line-height:1;font-variant-numeric:tabular-nums}
.score small{font-size:16px;color:var(--muted);font-weight:400}
.badge{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:1px 10px;font-size:12px;font-weight:600;margin:4px 6px 4px 0}
.badge.good{color:var(--good);border-color:var(--good)}.badge.warn{color:var(--warn);border-color:var(--warn)}.badge.bad{color:var(--bad);border-color:var(--bad)}.badge.info{color:var(--info);border-color:var(--info)}
.tiles{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin-top:16px}
.tile{border:1px solid var(--line);border-radius:10px;padding:12px}
.tile .l{color:var(--muted);font-size:12px}.tile .v{font-size:22px;font-weight:600;margin-top:2px}.tile .f{color:var(--muted);font-size:12px;margin-top:4px}
.bar{height:8px;background:var(--track);border-radius:999px;overflow:hidden;margin:4px 0 2px}
.bar>span{display:block;height:100%;border-radius:999px;background:var(--primary)}
.bar.good>span{background:var(--good)}.bar.warn>span{background:var(--warn)}.bar.bad>span{background:var(--bad)}
.row{display:flex;justify-content:space-between;gap:12px;font-size:13px}
.row .n{font-weight:600;min-width:0;overflow-wrap:anywhere}.row .v{color:var(--muted);white-space:nowrap;font-variant-numeric:tabular-nums}
ul.plain{list-style:none;margin:0;padding:0}ul.plain>li{margin:0 0 12px}
.sub{color:var(--muted);font-size:12px;margin:0}
table{width:100%;border-collapse:collapse;font-size:14px}
th,td{text-align:left;padding:8px 10px 8px 0;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--muted);font-size:12px;font-weight:600}
td.num{font-variant-numeric:tabular-nums}
.grid2{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:16px}
.inner{border:1px solid var(--line);border-radius:10px;padding:14px}
figure{margin:0 0 12px;border:1px solid var(--line);border-left:4px solid var(--line);border-radius:8px;padding:12px 14px}
figure.positive{border-left-color:var(--good)}figure.negative{border-left-color:var(--bad)}
blockquote{margin:0}figcaption{color:var(--muted);font-size:13px;margin-top:6px}figcaption b{color:var(--fg)}
.disclaimer{border:2px solid var(--warn);border-radius:12px;padding:16px 20px;margin:16px 0;background:var(--card)}
.disclaimer strong{display:block;margin-bottom:4px}
svg text{fill:var(--muted);font-size:11px}
footer{color:var(--muted);font-size:12px;margin-top:24px}
code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:13px;overflow-wrap:anywhere}
@media print{:root{--bg:#fff;--card:#fff;--fg:#000;--muted:#444;--line:#bbb;--track:#ddd}body{background:#fff}main{padding:0}.card{break-inside:avoid;border-color:#bbb}}
`;

const toneOf = (label: string): string => ({ strong: "good", promising: "info", weak: "warn", poor: "bad" })[label] ?? "info";

const pct = (n: number): number => Math.min(100, Math.max(0, Number.isFinite(n) ? n : 0));

function bar(percent: number, tone = ""): string {
  return `<div class="bar ${tone}"><span style="width:${pct(percent).toFixed(1)}%"></span></div>`;
}

function barRow(name: string, valueText: string, percent: number, tone = ""): string {
  return `<li><div class="row"><span class="n">${escapeHtml(name)}</span><span class="v">${escapeHtml(valueText)}</span></div>${bar(percent, tone)}</li>`;
}

function columnsSvg(label: string, items: { caption: string; value: number }[]): string {
  const max = Math.max(1, ...items.map((i) => i.value));
  const w = 36;
  const gap = 8;
  const height = 130;
  const width = items.length * (w + gap) + gap;
  const cols = items
    .map((item, i) => {
      const h = Math.round((item.value / max) * (height - 38));
      const x = gap + i * (w + gap);
      const y = height - 22 - h;
      return `<rect x="${x}" y="${y}" width="${w}" height="${Math.max(h, 1)}" rx="3" fill="var(--primary)" opacity="${item.value === 0 ? 0.25 : 0.8}"/><text x="${x + w / 2}" y="${height - 6}" text-anchor="middle">${escapeHtml(item.caption)}</text>${item.value > 0 ? `<text x="${x + w / 2}" y="${y - 4}" text-anchor="middle">${item.value}</text>` : ""}`;
    })
    .join("");
  return `<svg role="img" aria-label="${escapeHtml(label)}" viewBox="0 0 ${width} ${height}" width="100%" style="max-width:${width}px">${cols}</svg>`;
}

function themeList(themes: ReportTheme[], total: number, t: Translate, tone = ""): string {
  return `<ul class="plain">${themes
    .map((theme) => {
      const people = t("audienceHub.report.peopleOf", { count: theme.count, total });
      return `<li><div class="row"><span class="n">${escapeHtml(theme.label)}</span><span class="v">${escapeHtml(people)} · ${escapeHtml(formatPct(theme.pct))}</span></div>${bar(theme.pct, tone)}${
        theme.personas && theme.personas.length > 0 ? `<p class="sub">${escapeHtml(theme.personas.join(", "))}</p>` : ""
      }</li>`;
    })
    .join("")}</ul>`;
}

function section(title: string, body: string, help?: string): string {
  return `<section class="card"><h2>${escapeHtml(title)}</h2>${help ? `<p class="help">${escapeHtml(help)}</p>` : ""}${body}</section>`;
}

function scoreBars(items: { name: string; value: number }[], max: number, format: (v: number) => string, tone = ""): string {
  return `<ul class="plain">${items.map((i) => barRow(i.name, format(i.value), (i.value / max) * 100, tone)).join("")}</ul>`;
}

const score10 = (v: number) => `${v.toFixed(1)} / 10`;

function tile(label: string, value: string, foot?: string): string {
  return `<div class="tile"><div class="l">${escapeHtml(label)}</div><div class="v">${escapeHtml(value)}</div>${foot ? `<div class="f">${escapeHtml(foot)}</div>` : ""}</div>`;
}

export function buildStudyReportHtml(input: StudyReportHtmlInput): string {
  const { report, title, audienceName, lang, t } = input;
  const kind: StudyKind = input.kind ?? report.kind ?? "user_interviews";
  const interviews = kind === "user_interviews";
  const r = (key: string, params?: Record<string, string | number>) => t(`audienceHub.report.${key}`, params);
  const rk = (key: string, params?: Record<string, string | number>) => t(`audienceReportKinds.${key}`, params);
  const kt = (group: string, params?: Record<string, string | number>) => rk(`${group}.${kind}`, params);
  const m = report.metrics;
  const price = m.price;
  const generated = new Date(report.generated_at);
  const dateText = Number.isNaN(generated.getTime())
    ? ""
    : generated.toLocaleDateString(lang, { year: "numeric", month: "long", day: "numeric" });
  const stamp = (input.generatedAt ?? new Date()).toLocaleDateString(lang, { year: "numeric", month: "long", day: "numeric" });
  const money = (v: number) => formatMoney(v, price.currency, lang);
  const people = (n: number) => r("people", { count: n });
  const total = m.respondents;
  const tone = toneOf(report.verdict.label);

  const tiles = [
    tile(r("respondents"), String(m.respondents), m.failed > 0 ? r("couldNotAnswer", { count: m.failed }) : undefined),
    tile(interviews ? r("meanIntent") : kt("meanScore"), `${m.intent_mean.toFixed(1)} / 10`),
    tile(r("yesMaybeNo"), `${m.yes} · ${m.maybe} · ${m.no}`, r("yesMaybeNoFoot", { yes: m.yes, maybe: m.maybe, no: m.no })),
  ];
  if (interviews) tiles.push(tile(r("wouldPayNow"), formatPct(m.would_pay_now_pct)));
  if (report.landing) tiles.push(tile(rk("landing.clarityMean"), score10(report.landing.clarity_mean), rk("landing.clarityFoot")));
  if (report.messages) tiles.push(tile(rk("messages.winner"), rk("messages.winnerLine", { n: report.messages.winner })));
  if (report.ab) {
    tiles.push(
      tile(
        rk("ab.tile"),
        report.ab.winner === "tie" ? rk("ab.tie") : rk("ab.version", { label: report.ab.winner }),
        rk(`ab.confidence.${report.ab.confidence}`),
      ),
    );
  }

  const parts: string[] = [];

  parts.push(`<header class="card">
<p class="eyebrow">${escapeHtml(interviews ? r("eyebrow") : kt("eyebrow"))} · ${escapeHtml(rk(`kind.${kind}`))}</p>
<h1>${escapeHtml(title)}</h1>
<p class="meta">${audienceName ? `${escapeHtml(rk("html.audience"))}: ${escapeHtml(audienceName)} · ` : ""}${escapeHtml(dateText)}</p>
<div class="hero">
<div><p class="meta">${escapeHtml(interviews ? r("intentIndex") : kt("score"))}</p>
<div class="score" aria-label="${escapeHtml(rk("html.scoreOutOf", { score: Math.round(report.verdict.score) }))}">${Math.round(report.verdict.score)} <small>/ 100</small></div>
<span class="badge ${tone}">${escapeHtml(r(`verdict.${report.verdict.label}`))}</span>
${bar(report.verdict.score, tone)}
<p class="sub">${escapeHtml(r("indexScale"))}</p></div>
<div><h2>${escapeHtml(r("executiveSummary"))}</h2><p>${escapeHtml(report.summary)}</p>${report.verdict.rationale ? `<p class="meta">${escapeHtml(report.verdict.rationale)}</p>` : ""}</div>
</div>
<div class="tiles">${tiles.join("")}</div>
</header>`);

  parts.push(`<aside class="disclaimer" role="note"><strong>${escapeHtml(r("disclaimerTitle"))}</strong><p>${escapeHtml(rk("html.syntheticLine"))}</p>${
    report.disclaimer ? `<p>${escapeHtml(report.disclaimer)}</p>` : ""
  }</aside>`);

  if (report.synthesis_failed) {
    parts.push(section(r("synthesisFailedTitle"), `<p>${escapeHtml(r("synthesisFailedBody"))}</p>`));
  }

  parts.push(
    section(
      interviews ? r("intentTitle") : kt("intentTitle"),
      columnsSvg(
        interviews ? r("intentTitle") : kt("intentTitle"),
        m.intent_distribution.map((count, i) => ({ caption: String(i), value: count })),
      ) + `<p class="sub">${escapeHtml(people(m.respondents))}</p>`,
      interviews ? r("intentHelp") : kt("intentHelp"),
    ),
  );

  if (report.landing) {
    const l = report.landing;
    const meta = `<p class="sub">${escapeHtml(rk("landing.visitorIntent"))}: ${escapeHtml(l.intent ?? "")} · ${escapeHtml(rk("landing.source"))}: <code>${escapeHtml(l.source || rk("landing.pasted"))}</code></p>`;
    const by = l.clarity_by_segment && l.clarity_by_segment.length > 0 ? scoreBars(l.clarity_by_segment, 10, score10) : "";
    parts.push(section(rk("landing.clarityTitle"), meta + by, rk("landing.clarityHelp")));
    if (report.strengths && report.strengths.length > 0) {
      parts.push(section(rk("landing.strengthsTitle"), themeList(report.strengths, total, t, "good"), rk("landing.strengthsHelp")));
    }
  }

  if (report.messages) {
    const winnerVariant = report.messages.winner;
    const ranked = [...report.messages.variants].sort((a, b) => a.rank - b.rank);
    const items = ranked
      .map((v) => {
        const winner = v.variant === winnerVariant;
        const dims = scoreBars(
          [
            { name: rk("messages.clarity"), value: v.clarity },
            { name: rk("messages.relevance"), value: v.relevance },
            { name: rk("messages.credibility"), value: v.credibility },
            { name: rk("messages.appeal"), value: v.appeal },
          ],
          10,
          score10,
        );
        const seg = v.by_segment && v.by_segment.length > 1 ? `<h3>${escapeHtml(rk("messages.bySegment"))}</h3>${scoreBars(v.by_segment, 10, score10, "warn")}` : "";
        return `<li class="inner"><span class="badge ${winner ? "good" : ""}">${escapeHtml(winner ? rk("messages.winner") : rk("messages.rank", { rank: v.rank }))}</span> <span class="meta">${escapeHtml(rk("messages.line", { n: v.variant }))} · ${escapeHtml(rk("messages.overall"))} ${escapeHtml(score10(v.overall))}</span><p><b>${escapeHtml(v.text)}</b></p>${dims}<p class="sub">${escapeHtml(rk("messages.bestPct", { pct: Math.round(v.best_pct) }))}</p>${seg}</li>`;
      })
      .join("");
    parts.push(section(rk("messages.rankingTitle"), `<ol class="plain">${items}</ol>`, rk("messages.rankingHelp")));
    if (report.strengths && report.strengths.length > 0) {
      parts.push(section(rk("messages.strengthsTitle"), themeList(report.strengths, total, t, "good"), rk("messages.strengthsHelp")));
    }
    if (report.messages.rewrites && report.messages.rewrites.length > 0) {
      const figs = report.messages.rewrites
        .map(
          (q) =>
            `<figure><blockquote>${escapeHtml(q.text)}</blockquote><figcaption><b>${escapeHtml(q.persona)}</b>${
              [q.role, q.segment].filter(Boolean).length > 0 ? ` · ${escapeHtml([q.role, q.segment].filter(Boolean).join(" · "))}` : ""
            } · ${escapeHtml(rk("html.syntheticPersona"))}</figcaption></figure>`,
        )
        .join("");
      parts.push(section(rk("messages.rewritesTitle"), figs, rk("messages.rewritesHelp")));
    }
  }

  if (report.ab) {
    const ab = report.ab;
    const version = (label: string) => rk("ab.version", { label });
    const fmt = (unit: "score" | "pct", v: number | undefined) => (v === undefined ? "-" : unit === "pct" ? formatPct(v) : v.toFixed(1));
    const rows = abRows(ab)
      .map(
        (row) =>
          `<tr><th scope="row">${escapeHtml(rk(`ab.rows.${row.key}`))}</th><td class="num">${escapeHtml(fmt(row.unit, row.aMean))}</td><td class="num">${escapeHtml(fmt(row.unit, row.aMedian))}</td><td class="num">${escapeHtml(fmt(row.unit, row.bMean))}</td><td class="num">${escapeHtml(fmt(row.unit, row.bMedian))}</td><td class="num">${escapeHtml(signed(row.diff, row.unit === "pct" ? 0 : 1))}</td><td class="num">${
            row.paired ? escapeHtml(rk("ab.pairedValue", { a: row.paired.a, b: row.paired.b, same: row.paired.same })) : "-"
          }</td><td>${row.effect ? escapeHtml(rk("ab.effectValue", { label: rk(`ab.effectLabel.${row.effect.label}`), size: row.effect.size.toFixed(2) })) : "-"}</td></tr>`,
      )
      .join("");
    const table = `<div style="overflow-x:auto"><table><thead><tr><th rowspan="2">${escapeHtml(rk("ab.question"))}</th><th colspan="2">${escapeHtml(version("A"))}</th><th colspan="2">${escapeHtml(version("B"))}</th><th rowspan="2">${escapeHtml(rk("ab.diff"))}</th><th rowspan="2">${escapeHtml(rk("ab.paired"))}</th><th rowspan="2">${escapeHtml(rk("ab.effect"))}</th></tr><tr><th>${escapeHtml(rk("ab.colMean"))}</th><th>${escapeHtml(rk("ab.colMedian"))}</th><th>${escapeHtml(rk("ab.colMean"))}</th><th>${escapeHtml(rk("ab.colMedian"))}</th></tr></thead><tbody>${rows}</tbody></table></div>`;
    const bias = positionBiased(ab) ? `<p class="disclaimer">${escapeHtml(rk("ab.positionBias", { pct: Math.round(ab.first_shown_win_pct) }))}</p>` : "";
    parts.push(
      section(
        ab.winner === "tie" ? rk("ab.tie") : rk("ab.winner", { label: ab.winner }),
        `<p><span class="meta">${escapeHtml(rk("ab.confidenceTitle"))}:</span> <span class="badge info">${escapeHtml(rk(`ab.confidence.${ab.confidence}`))}</span></p><p>${escapeHtml(rk(`ab.caveat.${ab.confidence}`))}</p>${bias}<h3>${escapeHtml(rk("ab.compareTitle"))}</h3>${table}<p class="disclaimer"><strong>${escapeHtml(rk("ab.honestyTitle"))}</strong>${escapeHtml(rk("ab.honesty"))}</p>`,
        rk("ab.compareHelp"),
      ),
    );
    const sides = ab.sides
      .map((side) => {
        const win = ab.winner === side.label ? ` <span class="badge good">${escapeHtml(rk("ab.winner", { label: side.label }))}</span>` : "";
        const shares = scoreBars(
          [
            { name: rk("ab.rows.preferred"), value: side.win_pct },
            { name: rk("ab.rows.actPct"), value: side.act_pct },
          ],
          100,
          formatPct,
        );
        const seg =
          side.win_pct_by_segment && side.win_pct_by_segment.length > 1
            ? `<h3>${escapeHtml(rk("ab.preferenceTitle"))}</h3>${scoreBars(side.win_pct_by_segment, 100, formatPct, "warn")}`
            : "";
        const reasons = side.reasons && side.reasons.length > 0 ? `<h3>${escapeHtml(rk("ab.reasons"))}</h3>${themeList(side.reasons, total, t, "good")}` : "";
        const issues = side.issues && side.issues.length > 0 ? `<h3>${escapeHtml(rk("ab.issues"))}</h3>${themeList(side.issues, total, t, "bad")}` : "";
        const quotes =
          side.quotes && side.quotes.length > 0
            ? `<h3>${escapeHtml(rk("ab.quotes"))}</h3>${side.quotes
                .map(
                  (q) =>
                    `<figure><blockquote>${escapeHtml(q.text)}</blockquote><figcaption><b>${escapeHtml(q.persona)}</b>${
                      [q.role, q.segment].filter(Boolean).length > 0 ? ` · ${escapeHtml([q.role, q.segment].filter(Boolean).join(" · "))}` : ""
                    } · ${escapeHtml(version(q.version ?? side.label))} · ${escapeHtml(rk("html.syntheticPersona"))}</figcaption></figure>`,
                )
                .join("")}`
            : "";
        return `<div class="inner"><h3>${escapeHtml(version(side.label))}${win}</h3><p class="sub">${escapeHtml(rk("ab.source"))}: <code>${escapeHtml(side.source || rk("ab.pasted"))}</code></p>${shares}<p class="sub">${escapeHtml(rk("ab.preferredValue", { pct: formatPct(side.win_pct), count: side.wins, total }))} · ${escapeHtml(rk("ab.meanScore"))}: ${escapeHtml(score10(side.mean_score))}</p>${seg}${reasons}${issues}${quotes}</div>`;
      })
      .join("");
    parts.push(section(rk("ab.versionsTitle"), `<div class="grid2">${sides}</div>`, rk("ab.versionsHelp")));
  }

  if (interviews) {
    let pay: string;
    if (price.answered === 0) {
      pay = `<p>${escapeHtml(price.not_willing > 0 ? r("payNobody") : r("payNoData"))}</p>`;
    } else {
      pay = `<div class="tiles">${tile(r("median"), money(price.median))}${tile(r("mean"), money(price.mean))}${tile(r("range"), `${money(price.min)} - ${money(price.max)}`, r("quartiles", { low: money(price.p25), high: money(price.p75) }))}${tile(r("payNothing"), String(price.not_willing), r("payNothingFoot"))}</div><h3>${escapeHtml(r("histogram"))}</h3>${columnsSvg(
        r("histogram"),
        price.histogram.map((b) => ({ caption: money(b.from), value: b.count })),
      )}`;
    }
    if (price.tests && price.tests.length > 0) {
      pay += `<h3>${escapeHtml(r("priceTests"))}</h3><ul class="plain">${price.tests
        .map((test) =>
          barRow(money(test.price), test.asked === 0 ? r("notAsked") : r("buyAt", { pct: Math.round(test.buy_pct), asked: test.asked }), test.asked === 0 ? 0 : test.buy_pct),
        )
        .join("")}</ul>`;
    }
    parts.push(section(r("payTitle"), pay, r("payHelp", { currency: price.currency })));
  }

  if (report.segments.length > 0) {
    const head = `<tr><th>${escapeHtml(r("segment"))}</th><th>${escapeHtml(r("respondents"))}</th><th>${escapeHtml(interviews ? r("meanIntent") : kt("meanScore"))}</th><th>${escapeHtml(r("yesPct"))}</th>${
      interviews ? `<th>${escapeHtml(r("medianPrice"))}</th>` : ""
    }</tr>`;
    const rows = report.segments
      .map(
        (s) =>
          `<tr><td><b>${escapeHtml(s.name)}</b>${s.insight ? `<p class="sub">${escapeHtml(s.insight)}</p>` : ""}</td><td class="num">${s.respondents}</td><td class="num">${s.intent_mean.toFixed(1)}</td><td class="num">${escapeHtml(formatPct(s.yes_pct))}</td>${
            interviews ? `<td class="num">${s.median_price > 0 ? escapeHtml(money(s.median_price)) : "-"}</td>` : ""
          }</tr>`,
      )
      .join("");
    parts.push(section(r("segmentsTitle"), `<div style="overflow-x:auto"><table><thead>${head}</thead><tbody>${rows}</tbody></table></div>`));
  }

  if (report.objections.length > 0) {
    parts.push(
      section(interviews ? r("objectionsTitle") : kt("objectionsTitle"), themeList(report.objections, total, t, "bad"), interviews ? r("objectionsHelp") : kt("objectionsHelp")),
    );
  }
  if (report.needs.length > 0) {
    parts.push(section(interviews ? r("needsTitle") : kt("needsTitle"), themeList(report.needs, total, t, "warn"), interviews ? r("needsHelp") : kt("needsHelp")));
  }
  if (report.alternatives.length > 0) {
    parts.push(section(r("alternativesTitle"), themeList(report.alternatives, total, t), r("alternativesHelp")));
  }

  const quotesPerVersion = Boolean(report.ab?.sides.some((side) => side.quotes && side.quotes.length > 0));
  if (report.quotes.length > 0 && !quotesPerVersion) {
    const figs = report.quotes
      .map((q) => {
        const sentiment = q.sentiment === "positive" || q.sentiment === "negative" ? q.sentiment : "";
        const who = [q.role, q.segment].filter(Boolean).join(" · ");
        return `<figure class="${sentiment}"><blockquote>${escapeHtml(q.text)}</blockquote><figcaption><b>${escapeHtml(q.persona)}</b>${who ? ` · ${escapeHtml(who)}` : ""}${
          q.version ? ` · ${escapeHtml(rk("ab.version", { label: q.version }))}` : ""
        }${q.topic ? ` · ${escapeHtml(q.topic)}` : ""} · ${escapeHtml(rk("html.syntheticPersona"))}</figcaption></figure>`;
      })
      .join("");
    parts.push(section(r("quotesTitle"), figs, r("quotesHelp")));
  }

  if (report.recommendations.length > 0) {
    const items = report.recommendations
      .map(
        (rec) =>
          `<li><b>${escapeHtml(rec.title)}</b>${rec.priority ? ` <span class="badge ${rec.priority === "high" ? "bad" : rec.priority === "medium" ? "warn" : ""}">${escapeHtml(r(`priority.${rec.priority}`))}</span>` : ""}${
            rec.detail ? `<p class="meta">${escapeHtml(rec.detail)}</p>` : ""
          }</li>`,
      )
      .join("");
    parts.push(section(interviews ? r("recommendationsTitle") : kt("recommendationsTitle"), `<ol>${items}</ol>`));
  }

  if (report.open_questions && report.open_questions.length > 0) {
    const items = report.open_questions.map((q) => `<dt><b>${escapeHtml(q.question)}</b></dt><dd>${escapeHtml(q.digest)}</dd>`).join("");
    parts.push(section(r("openQuestionsTitle"), `<dl>${items}</dl>`));
  }

  parts.push(section(r("methodologyTitle"), `<p>${escapeHtml(report.methodology)}</p>`));
  parts.push(`<footer>${escapeHtml(rk("html.generatedOn", { date: stamp }))} · ${escapeHtml(rk("html.footer"))}</footer>`);

  return `<!doctype html>
<html lang="${escapeHtml(lang)}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">
<meta name="color-scheme" content="light dark">
<title>${escapeHtml(title)}</title>
<style>${CSS}</style>
</head>
<body>
<main>
${parts.join("\n")}
</main>
</body>
</html>
`;
}

export function reportFilename(title: string, kindOrDate: string): string {
  const slug = (value: string) => value.normalize("NFC").replace(/[^\p{L}\p{N}]+/gu, "-").replace(/^-+|-+$/g, "");
  const name = Array.from(slug(title)).slice(0, 80).join("").replace(/-+$/g, "") || "study-report";
  const suffix = slug(kindOrDate);
  return `${name}${suffix ? `-${suffix}` : ""}.html`;
}

export function downloadHtml(filename: string, html: string): void {
  const url = URL.createObjectURL(new Blob([html], { type: "text/html;charset=utf-8" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function openHtml(filename: string, html: string): void {
  const url = URL.createObjectURL(new Blob([html], { type: "text/html;charset=utf-8" }));
  const opened = window.open(url, "_blank");
  if (!opened) {
    URL.revokeObjectURL(url);
    downloadHtml(filename, html);
    return;
  }
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
