import type { StudyKind, StudyPlanInput, StudyPageInput } from "@/api";
import { parsePrices } from "@/lib/audienceHub";
import { isHttpUrl, MAX_VARIANTS, MIN_VARIANTS } from "@/lib/studyWizard";

export interface PageDraft {
  mode: "url" | "paste";
  url: string;
  copy: string;
}

/** What the first step of the wizard collects, for every kind at once so switching kind loses nothing. */
export interface ContextDraft {
  goal: string;
  prices: string;
  currency: string;
  intent: string;
  pages: [PageDraft, PageDraft];
  variants: string[];
}

export function emptyContext(): ContextDraft {
  return {
    goal: "",
    prices: "",
    currency: "USD",
    intent: "evaluate",
    pages: [
      { mode: "url", url: "", copy: "" },
      { mode: "url", url: "", copy: "" },
    ],
    variants: ["", ""],
  };
}

export function pagesNeeded(kind: StudyKind): number {
  return kind === "ab_test" ? 2 : kind === "landing_page_test" ? 1 : 0;
}

function pageProblem(p: PageDraft): "needUrl" | "badUrl" | "needCopy" | null {
  if (p.mode === "paste") return p.copy.trim() ? null : "needCopy";
  if (!p.url.trim()) return "needUrl";
  return isHttpUrl(p.url) ? null : "badUrl";
}

/** The first thing wrong with the context for this kind, as a locale key under audienceStudy.context.errors, or null. */
export function contextProblem(kind: StudyKind, c: ContextDraft): string | null {
  for (let i = 0; i < pagesNeeded(kind); i++) {
    const problem = pageProblem(c.pages[i]);
    if (problem) return kind === "ab_test" ? `${problem}${i === 0 ? "A" : "B"}` : problem;
  }
  if (kind === "message_testing") {
    const filled = new Set(c.variants.map((v) => v.trim().toLowerCase()).filter(Boolean));
    if (filled.size < MIN_VARIANTS) return "needVariants";
    if (c.variants.filter((v) => v.trim()).length > MAX_VARIANTS) return "tooManyVariants";
  }
  return null;
}

function pageInput(p: PageDraft): StudyPageInput {
  return p.mode === "paste" ? { copy: p.copy.trim() } : { url: p.url.trim() };
}

/** The plan request for the study the context describes. */
export function contextToInput(kind: StudyKind, c: ContextDraft): StudyPlanInput {
  const input: StudyPlanInput = { kind, goal: c.goal.trim() };
  if (kind === "user_interviews") {
    const prices = parsePrices(c.prices);
    if (prices.length > 0) {
      input.price_points = prices;
      input.currency = c.currency.trim().toUpperCase() || "USD";
    }
  } else if (kind === "message_testing") {
    input.inputs = { variants: c.variants.map((v) => v.trim()).filter(Boolean) };
  } else {
    input.inputs = { intent: c.intent.trim() || "evaluate", pages: c.pages.slice(0, pagesNeeded(kind)).map(pageInput) };
  }
  return input;
}
