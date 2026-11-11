import type { AudienceStudy, ScriptSection, StudyAnswer, StudyKind, StudyScript } from "@/api";

export type WizardStep = "context" | "personas" | "script" | "interviews" | "report";

export const WIZARD_STEPS: readonly WizardStep[] = ["context", "personas", "script", "interviews", "report"];

export const MAX_SCRIPT_QUESTIONS = 12;
export const MAX_SCRIPT_SECTIONS = 8;
export const MIN_VARIANTS = 2;
export const MAX_VARIANTS = 5;

export const INTENT_PRESETS = ["evaluate", "buy", "compare", "signup"] as const;

/** The step a study is at: a draft waits at its script, the interviews run, then the report. */
export function stepOf(study: Pick<AudienceStudy, "status" | "started_at">): WizardStep {
  switch (study.status) {
    case "draft":
      return "script";
    case "running":
    case "interviewed":
      return "interviews";
    case "synthesizing":
    case "done":
      return "report";
    default:
      return study.started_at ? "interviews" : "script";
  }
}

export function stepIndex(step: WizardStep): number {
  return WIZARD_STEPS.indexOf(step);
}

export function scriptQuestionCount(script?: StudyScript | null): number {
  return script ? script.sections.reduce((n, s) => n + s.questions.length, 0) : 0;
}

const HEADING = /^#{1,6}\s+(.*)$/;
const LABEL = /^([^?]{1,100}):$/;
const BULLET = /^(?:[-*+•]|\d{1,3}[.)])\s+/;

/**
 * A script from plain text or markdown. A `#` heading or a line ending in `:`
 * opens a section; every other non-empty line is a question, with its bullet
 * or number removed. Questions before any heading go to an untitled section.
 */
export function parseScriptText(text: string): StudyScript {
  const sections: ScriptSection[] = [];
  let current: ScriptSection | null = null;
  const open = (title: string) => {
    current = { title, questions: [] };
    sections.push(current);
  };
  for (const raw of text.replace(/\r\n?/g, "\n").split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const heading = HEADING.exec(line);
    if (heading) {
      open(heading[1].replace(/[*_`]/g, "").trim());
      continue;
    }
    const stripped = line.replace(BULLET, "").trim();
    const label = !BULLET.test(line) ? LABEL.exec(line) : null;
    if (label) {
      open(label[1].replace(/[*_`]/g, "").trim());
      continue;
    }
    if (!stripped) continue;
    if (!current) open("");
    (current as ScriptSection | null)?.questions.push(stripped);
  }
  return { sections: sections.filter((s) => s.questions.length > 0) };
}

/** The script with empty questions and empty sections removed, as the server will keep it. */
export function cleanScript(script: StudyScript): StudyScript {
  return {
    sections: script.sections
      .map((s) => ({ title: s.title.trim(), questions: s.questions.map((q) => q.trim()).filter(Boolean) }))
      .filter((s) => s.questions.length > 0),
  };
}

export interface TranscriptTurn {
  question: string;
  answer: string;
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

const list = (items?: string[]) => (items && items.length > 0 ? items.join("; ") : "");

/**
 * What the interviewer asked and what the person answered, in the order of the
 * interview: the kind's built-in questions first, then the script's. Built from
 * the stored answers, so a failed interview has none.
 */
export function buildTranscript(answer: StudyAnswer, kind: StudyKind, variants: string[] | undefined, t: Translate): TranscriptTurn[] {
  if (answer.status !== "ok") return [];
  const x = answer.answers;
  const q = (key: string) => t(`audienceStudy.transcript.${kind}.${key}`);
  const turns: TranscriptTurn[] = [];
  const add = (key: string, text: string | undefined) => {
    if (text && text.trim()) turns.push({ question: q(key), answer: text.trim() });
  };
  const verdict = `${t(`audienceStudy.transcript.verdict.${x.verdict}`)} (${x.intent_score}/10)`;
  switch (kind) {
    case "landing_page_test":
      add("impression", x.landing?.first_impression);
      add("clarity", x.landing ? t("audienceStudy.transcript.score", { score: x.landing.clarity_score }) : undefined);
      add("act", [verdict, x.reasoning].filter(Boolean).join(". "));
      add("confused", list(x.objections));
      add("missing", list(x.missing));
      add("trust", list(x.landing?.trust_signals));
      add("change", x.landing?.change);
      break;
    case "message_testing": {
      const lines = (x.ratings ?? []).map((r) => {
        const text = variants?.[r.variant - 1] ?? `#${r.variant}`;
        return t("audienceStudy.transcript.rating", { line: text, clarity: r.clarity, relevance: r.relevance, credibility: r.credibility, appeal: r.appeal });
      });
      add("ratings", lines.join("\n"));
      const best = x.best_variant ? (variants?.[x.best_variant - 1] ?? `#${x.best_variant}`) : "";
      add("best", [best, x.reasoning].filter(Boolean).join(" - "));
      add("stronger", x.stronger);
      break;
    }
    case "ab_test": {
      const order: ("a" | "b")[] = x.shown_first === "b" ? ["b", "a"] : ["a", "b"];
      for (const v of order) {
        const card = v === "a" ? x.page_a : x.page_b;
        if (!card) continue;
        const key = (name: string) => `${name}${v.toUpperCase()}`;
        add(key("score"), t("audienceStudy.transcript.pageScore", { act: card.act_score, clarity: card.clarity_score, verdict: t(`audienceStudy.transcript.verdict.${card.verdict}`) }));
        add(key("impression"), card.first_impression);
        add(key("confused"), list(card.objections));
        add(key("missing"), list(card.missing));
        add(key("trust"), list(card.trust_signals));
        add(key("change"), card.change);
      }
      add("preferred", x.preferred ? [t("audienceStudy.transcript.preferred", { page: x.preferred.toUpperCase() }), x.reasoning].filter(Boolean).join(". ") : undefined);
      break;
    }
    default:
      add("buy", [verdict, x.reasoning].filter(Boolean).join(". "));
      add("price", x.max_price === undefined ? undefined : x.max_price > 0 ? t("audienceStudy.transcript.maxPrice", { price: x.max_price }) : t("audienceStudy.transcript.wouldNotPay"));
      add("missing", list(x.missing));
      add("objections", list(x.objections));
      add("alternatives", list(x.alternatives));
  }
  for (const qa of x.answers ?? []) turns.push({ question: qa.question, answer: qa.answer });
  if (x.quote) turns.push({ question: q("quote"), answer: x.quote });
  return turns;
}

/** How many questions an interview of this kind asks before the script. */
export const CORE_QUESTION_COUNT: Record<StudyKind, number> = {
  user_interviews: 6,
  landing_page_test: 8,
  message_testing: 4,
  ab_test: 13,
};

export function interviewTotal(kind: StudyKind, script?: StudyScript | null): number {
  return CORE_QUESTION_COUNT[kind] + scriptQuestionCount(script);
}

export function isHttpUrl(value: string): boolean {
  try {
    const u = new URL(value.trim());
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}
