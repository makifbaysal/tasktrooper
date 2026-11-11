import { describe, expect, it } from "vitest";
import type { StudyAnswer } from "@/api";
import { buildTranscript, cleanScript, interviewTotal, isHttpUrl, parseScriptText, scriptQuestionCount, stepOf } from "./studyWizard";

describe("stepOf", () => {
  it("puts a draft at its script, running and interviewed studies at the interviews and finished ones at the report", () => {
    expect(stepOf({ status: "draft" })).toBe("script");
    expect(stepOf({ status: "running" })).toBe("interviews");
    expect(stepOf({ status: "interviewed" })).toBe("interviews");
    expect(stepOf({ status: "synthesizing" })).toBe("report");
    expect(stepOf({ status: "done" })).toBe("report");
    expect(stepOf({ status: "failed", started_at: "2026-11-11T10:00:00Z" })).toBe("interviews");
    expect(stepOf({ status: "cancelled" })).toBe("script");
  });
});

describe("parseScriptText", () => {
  it("reads markdown headings, labels, bullets and numbers into sections of questions", () => {
    const script = parseScriptText(
      "# Today\n- How do you invoice today?\n2. What went wrong last time?\n\nDecision:\nWhat would make you switch?\n* Would you pay 9?\n",
    );
    expect(script.sections).toEqual([
      { title: "Today", questions: ["How do you invoice today?", "What went wrong last time?"] },
      { title: "Decision", questions: ["What would make you switch?", "Would you pay 9?"] },
    ]);
    expect(scriptQuestionCount(script)).toBe(4);
  });

  it("puts questions before any heading in an untitled section, keeps unicode and drops empty sections", () => {
    const script = parseScriptText("Şimdi nasıl fatura kesiyorsun?\r\nİş yerinde kim karar veriyor?\r\n## Boş\n");
    expect(script.sections).toEqual([{ title: "", questions: ["Şimdi nasıl fatura kesiyorsun?", "İş yerinde kim karar veriyor?"] }]);
  });

  it("does not take a question that ends with a colon-free question mark for a label", () => {
    expect(parseScriptText("Why: now?").sections[0].questions).toEqual(["Why: now?"]);
    expect(parseScriptText("").sections).toEqual([]);
  });
});

describe("cleanScript", () => {
  it("trims and drops empties", () => {
    expect(cleanScript({ sections: [{ title: " A ", questions: [" q ", ""] }, { title: "B", questions: [" "] }] })).toEqual({
      sections: [{ title: "A", questions: ["q"] }],
    });
  });
});

describe("isHttpUrl", () => {
  it("accepts http and https only", () => {
    expect(isHttpUrl("https://acme.test/a")).toBe(true);
    expect(isHttpUrl("ftp://x")).toBe(false);
    expect(isHttpUrl("acme.test")).toBe(false);
  });
});

const t = (key: string, params?: Record<string, string | number>) => (params ? `${key}:${JSON.stringify(params)}` : key);

describe("buildTranscript", () => {
  const base: StudyAnswer = {
    id: "x",
    persona: { name: "Şule İpek" },
    status: "ok",
    answers: { intent_score: 6, verdict: "maybe", would_pay_now: false, reasoning: "unclear", answers: [{ question: "Why?", answer: "Because." }], quote: "hmm" },
  };

  it("turns an interview into the kind's questions, then the script's, then the quote", () => {
    const turns = buildTranscript({ ...base, answers: { ...base.answers, max_price: 12, missing: ["Invoices"] } }, "user_interviews", undefined, t);
    expect(turns.map((x) => x.question)).toEqual([
      "audienceStudy.transcript.user_interviews.buy",
      "audienceStudy.transcript.user_interviews.price",
      "audienceStudy.transcript.user_interviews.missing",
      "Why?",
      "audienceStudy.transcript.user_interviews.quote",
    ]);
    expect(turns[0].answer).toContain("unclear");
  });

  it("has nothing for a failed interview", () => {
    expect(buildTranscript({ ...base, status: "failed" }, "user_interviews", undefined, t)).toEqual([]);
  });

  it("names the lines a reader rated and the page a visitor chose", () => {
    const messages = buildTranscript(
      { ...base, answers: { ...base.answers, ratings: [{ variant: 2, clarity: 9, relevance: 8, credibility: 7, appeal: 9 }], best_variant: 2, stronger: "Get paid" } },
      "message_testing",
      ["One", "İki"],
      t,
    );
    expect(messages[0].answer).toContain("İki");
    expect(messages.some((x) => x.answer === "Get paid")).toBe(true);
    const card = (act: number, objection: string) => ({ clarity_score: 5, act_score: act, verdict: "maybe" as const, first_impression: `impression ${act}`, objections: [objection] });
    const ab = buildTranscript(
      { ...base, answers: { ...base.answers, shown_first: "b", preferred: "b", page_a: card(3, "vague"), page_b: card(8, "pricey") } },
      "ab_test",
      undefined,
      t,
    );
    expect(ab.map((x) => x.question).slice(0, 2)).toEqual(["audienceStudy.transcript.ab_test.scoreB", "audienceStudy.transcript.ab_test.impressionB"]);
    expect(ab.some((x) => x.question === "audienceStudy.transcript.ab_test.confusedA" && x.answer === "vague")).toBe(true);
    expect(ab.find((x) => x.question === "audienceStudy.transcript.ab_test.preferred")?.answer).toContain('"page":"B"');
  });
});

describe("interviewTotal", () => {
  it("adds the script to the kind's own questions", () => {
    expect(interviewTotal("ab_test", { sections: [{ title: "", questions: ["a", "b"] }] })).toBe(15);
    expect(interviewTotal("user_interviews")).toBe(6);
  });
});
