import { describe, expect, it, vi } from "vitest";
import { tStatic } from "@/hooks/useI18n";
import { abReport, landingReport, messagesReport, report } from "@/test/audienceFixtures";
import { buildStudyReportHtml, downloadHtml, openHtml, reportFilename } from "./studyReportHtml";
import type { AudienceReport } from "@/api";

const build = (r: AudienceReport, extra: Partial<Parameters<typeof buildStudyReportHtml>[0]> = {}) =>
  buildStudyReportHtml({
    report: r,
    title: "Would they pay?",
    audienceName: "Freelance designers",
    lang: "en",
    t: tStatic,
    generatedAt: new Date("2026-11-12T09:00:00Z"),
    ...extra,
  });

const PAYLOADS = ["<script>alert(1)</script>", '"><img src=x onerror=1>'];

describe("buildStudyReportHtml", () => {
  it("is one self-contained document: no scripts, no links, no external requests, a locked-down policy", () => {
    for (const html of [build(report()), build(landingReport()), build(messagesReport()), build(abReport())]) {
      expect(html.startsWith("<!doctype html>")).toBe(true);
      expect(html).toContain('<meta charset="utf-8">');
      expect(html).not.toMatch(/<script/i);
      expect(html).not.toMatch(/<link\b/i);
      expect(html).not.toMatch(/https?:\/\/(?!acme\.test|a\.test)/);
      expect(html).not.toMatch(/@import|url\(/i);
      expect(html).not.toMatch(/<a\s/i);
      expect(html).not.toMatch(/<img/i);
      expect(html).toContain("default-src 'none'");
    }
  });

  it("follows the reader's colour scheme and prints cleanly", () => {
    const html = build(report());
    expect(html).toContain("@media (prefers-color-scheme:dark)");
    expect(html).toContain("@media print");
    expect(html).toContain('<html lang="en">');
  });

  it("escapes every piece of model-written or user-given text", () => {
    for (const payload of PAYLOADS) {
      const base = landingReport();
      const evil = landingReport({
        summary: payload,
        verdict: { label: "promising", score: 60, rationale: payload },
        segments: [{ name: payload, respondents: 1, intent_mean: 5, yes_pct: 0, median_price: 0, insight: payload }],
        objections: [{ label: payload, count: 1, pct: 25, personas: [payload] }],
        needs: [{ label: payload, count: 1, pct: 25 }],
        strengths: [{ label: payload, count: 1, pct: 25 }],
        quotes: [{ persona: payload, segment: payload, role: payload, text: payload, topic: payload, sentiment: "negative" }],
        recommendations: [{ title: payload, detail: payload, priority: "high" }],
        open_questions: [{ question: payload, digest: payload }],
        methodology: payload,
        disclaimer: payload,
        landing: { ...base.landing!, intent: payload, source: payload, clarity_by_segment: [{ name: payload, value: 5 }] },
      });
      const html = build(evil, { title: payload, audienceName: payload });
      expect(html).not.toContain(payload);
      expect(html).not.toMatch(/<script/i);
      expect(html).not.toMatch(/<img/i);
      expect(html).toContain("&lt;");
    }
  });

  it("escapes candidate lines, page sources and persona names in the message and A/B sections", () => {
    const payload = PAYLOADS[0];
    const m = messagesReport();
    const msg = messagesReport({
      messages: {
        ...m.messages!,
        variants: m.messages!.variants.map((v) => ({ ...v, text: payload, by_segment: [{ name: payload, value: 5 }, { name: "x", value: 4 }] })),
        rewrites: [{ persona: payload, text: payload, segment: payload }],
      },
    });
    expect(build(msg)).not.toContain(payload);
    const a = abReport();
    const evilQuote = (version: string) => ({ persona: payload, segment: payload, role: payload, text: payload, version: version as "A" | "B" });
    const ab = abReport({
      quotes: [{ persona: payload, text: payload, version: payload as "A" }],
      ab: {
        ...a.ab!,
        sides: [
          {
            ...a.ab!.sides[0],
            source: payload,
            issues: [{ label: payload, count: 1, pct: 25 }],
            reasons: [{ label: payload, count: 1, pct: 25, personas: [payload] }],
            win_pct_by_segment: [{ name: payload, value: 50 }, { name: "x", value: 50 }],
            quotes: [evilQuote(payload)],
          },
          { ...a.ab!.sides[1], source: '"><img src=x onerror=1>', quotes: [evilQuote('"><img src=x onerror=1>')] },
        ],
      },
    });
    const html = build(ab);
    expect(html).not.toContain(payload);
    expect(html).not.toContain('"><img');
    expect(html).not.toMatch(/<script/i);
    expect(html).not.toMatch(/<img/i);
    expect(html).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
    const generic = build(abReport({ ab: { ...a.ab!, sides: [{ ...a.ab!.sides[0], quotes: [] }, { ...a.ab!.sides[1], quotes: [] }] }, quotes: [{ persona: payload, text: payload, version: payload as "A" }] }));
    expect(generic).not.toContain(payload);
  });

  it("keeps names with Turkish letters byte for byte", () => {
    const names = ["Şule İpek", "Ayşe Yıldız", "ı İ ş ğ"];
    const r = report({
      quotes: names.map((persona) => ({ persona, text: `${persona} dedi`, sentiment: "positive" as const })),
      objections: [{ label: "ığ", count: 3, pct: 75, personas: names }],
    });
    const html = build(r, { title: "Şule'nin çayı", audienceName: "İstanbul" });
    for (const n of names) expect(html).toContain(n);
    expect(html).toContain("Şule&#39;nin çayı");
    expect(html).toContain("İstanbul");
  });

  it("labels quotes as synthetic personas and shows the disclaimer and the method", () => {
    const html = build(report());
    expect(html).toContain("synthetic persona");
    expect(html).toContain('role="note"');
    expect(html).toContain("The numbers are hypotheses to validate with real people");
    expect(html).toContain("These findings come from synthetic personas, not real customers.");
    expect(html).toContain("4 synthetic personas were each asked the same questions.");
    expect(html).toContain("too pricey for me");
  });

  it("shows the interview sections: purchase intent, willingness to pay, price tests and the price column", () => {
    const html = build(report());
    expect(html).toContain("Willingness to pay");
    expect(html).toContain("75% would buy (4 asked)");
    expect(html).toContain("Median price");
    expect(html).toContain("<svg");
    expect(html).toContain("User interviews");
    expect(html).toContain("58");
  });

  it("shows the landing page sections and drops the price ones", () => {
    const html = build(landingReport());
    expect(html).toContain("Landing page test");
    expect(html).toContain("Act-now score");
    expect(html).toContain("Clarity of the offer");
    expect(html).toContain("Freelancers");
    expect(html).toContain("Top frictions");
    expect(html).toContain("No pricing");
    expect(html).toContain("What worked");
    expect(html).toContain("Recommended edits");
    expect(html).toContain("Show the price");
    expect(html).not.toContain("Willingness to pay");
    expect(html).not.toContain("Median price");
  });

  it("shows the ranked lines of a message test with the winner and the rewrites", () => {
    const html = build(messagesReport());
    expect(html).toContain("Ranking of the lines");
    expect(html).toContain("Invoices that chase themselves");
    expect(html).toContain("Boards for agents");
    expect(html).toContain("100% picked it as the best");
    expect(html).toContain("Line 2");
    expect(html).toContain("Stronger lines people wrote");
    expect(html).toContain("Get paid faster");
    expect(html).toContain("Suggested rewrites");
  });

  it("shows the A/B comparison table, the bars per version, objections and quotes per version, and the honesty note", () => {
    const html = build(abReport());
    expect(html).toContain("Preferred by most: version A");
    expect(html).toContain("Side by side");
    expect(html).toContain("<table>");
    expect(html).toContain("Act-now score (0-10)");
    expect(html).toContain("Clarity (0-10)");
    expect(html).toContain("Share who would act");
    expect(html).toContain("Preferred by");
    expect(html).toContain("A 4 · B 0 · same 0");
    expect(html).toContain("Large (1.40)");
    expect(html).toContain("\u22123.5");
    expect(html).toContain("Lean");
    expect(html).toContain("Treat it as a lean, not a result.");
    expect(html).toContain("Version B");
    expect(html).toContain("jargon");
    expect(html).toContain("Clear offer");
    expect(html).toContain("I got it at once.");
    expect(html).toContain("Too much jargon.");
    expect(html).toContain("Version A · synthetic persona");
    expect(html).toContain("not statistically significant results, and no p-values are computed");
    expect(html).toContain("The page shown first won 80% of the time");
    expect(html).not.toContain("Win share");
    expect(build(abReport({ ab: { ...abReport().ab!, first_shown_win_pct: 50 } }))).not.toContain("shown first won");
  });

  it("says so when the written analysis failed", () => {
    expect(build(report({ synthesis_failed: true }))).toContain("The written analysis could not be produced");
  });
});

describe("reportFilename", () => {
  it("keeps letters and digits of any alphabet and turns the rest into dashes", () => {
    expect(reportFilename("Would they pay?", "landing_page_test")).toBe("Would-they-pay-landing-page-test.html");
    expect(reportFilename("Şule'nin / çayı: 50%", "2026-11-11")).toBe("Şule-nin-çayı-50-2026-11-11.html");
    expect(reportFilename("../../etc/passwd", "ab_test")).not.toMatch(/[/\\]/);
    expect(reportFilename("???", "ab_test")).toBe("study-report-ab-test.html");
    expect(reportFilename("x".repeat(300), "a").length).toBeLessThan(100);
  });
});

describe("downloadHtml and openHtml", () => {
  it("downloads through a blob link and releases the address", () => {
    vi.useFakeTimers();
    const create = vi.fn(() => "blob:report");
    const revoke = vi.fn();
    URL.createObjectURL = create;
    URL.revokeObjectURL = revoke;
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      expect(this.download).toBe("r.html");
      expect(this.href).toBe("blob:report");
    });
    downloadHtml("r.html", "<p>x</p>");
    expect(create).toHaveBeenCalledOnce();
    expect(click).toHaveBeenCalledOnce();
    vi.advanceTimersByTime(1500);
    expect(revoke).toHaveBeenCalledWith("blob:report");
    click.mockRestore();
    vi.useRealTimers();
  });

  it("opens a window, and falls back to a download when it is blocked", () => {
    URL.createObjectURL = vi.fn(() => "blob:report");
    URL.revokeObjectURL = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue({} as Window);
    openHtml("r.html", "<p>x</p>");
    expect(open).toHaveBeenCalledWith("blob:report", "_blank");
    open.mockReturnValue(null);
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    openHtml("r.html", "<p>x</p>");
    expect(click).toHaveBeenCalledOnce();
    open.mockRestore();
    click.mockRestore();
  });
});
