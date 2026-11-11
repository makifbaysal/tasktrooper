import type { Audience, AudienceDetail, AudiencePersona, AudienceReport, AudienceStudy, AudienceStudyDetail } from "@/api";

export function persona(overrides: Partial<AudiencePersona> = {}): AudiencePersona {
  return {
    id: "p1",
    audience_id: "a1",
    ordinal: 1,
    generation: 1,
    name: "Ayse Demir",
    segment: "Freelancers",
    age: 31,
    role: "UX designer",
    location: "Istanbul",
    bio: "Works alone for startups.",
    goals: ["more clients"],
    pains: ["tool sprawl"],
    quote: "I hate juggling five tools.",
    ...overrides,
  };
}

export function audience(overrides: Partial<Audience> = {}): Audience {
  return {
    id: "a1",
    name: "Freelance designers",
    status: "ready",
    brief: { product: "A kanban board where AI agents do the work.", segments: [{ name: "Freelancers", count: 3 }], pricing: "19 USD a month" },
    persona_target: 12,
    generation: 1,
    created_at: "2026-11-11T10:00:00Z",
    updated_at: "2026-11-11T10:00:00Z",
    persona_count: 2,
    study_count: 1,
    ...overrides,
  };
}

export function study(overrides: Partial<AudienceStudy> = {}): AudienceStudy {
  return {
    id: "s1",
    audience_id: "a1",
    audience_name: "Freelance designers",
    title: "Would they pay?",
    questions: [],
    price_points: [9, 19],
    currency: "USD",
    status: "done",
    persona_total: 2,
    persona_done: 2,
    persona_failed: 0,
    calls_estimate: 3,
    created_at: "2026-11-11T10:05:00Z",
    ...overrides,
  };
}

export function report(overrides: Partial<AudienceReport> = {}): AudienceReport {
  return {
    generated_at: "2026-11-11T10:20:00Z",
    summary: "Interest is lukewarm and price-bound.",
    verdict: { label: "promising", score: 57.5, rationale: "Mean intent is middling." },
    metrics: {
      respondents: 4,
      failed: 1,
      intent_distribution: [0, 0, 1, 0, 0, 1, 0, 1, 0, 1, 0],
      intent_mean: 5.75,
      yes: 2,
      maybe: 1,
      no: 1,
      would_pay_now_pct: 50,
      price: {
        currency: "USD",
        answered: 3,
        not_willing: 1,
        min: 5,
        p25: 7,
        median: 12,
        mean: 13.33,
        p75: 17.5,
        max: 20,
        histogram: [
          { from: 4, to: 8, count: 1 },
          { from: 8, to: 12, count: 0 },
          { from: 12, to: 16, count: 1 },
          { from: 16, to: 20, count: 1 },
        ],
        tests: [
          { price: 9, buy_pct: 75, asked: 4 },
          { price: 19, buy_pct: 25, asked: 4 },
        ],
      },
    },
    segments: [
      { name: "Freelancers", respondents: 3, intent_mean: 5, yes_pct: 33.3, median_price: 9, insight: "Freelancers balk at the price." },
      { name: "Agency leads", respondents: 1, intent_mean: 9, yes_pct: 100, median_price: 20 },
    ],
    objections: [{ label: "Too expensive", count: 3, pct: 75, personas: ["Ayse Demir", "Can", "Deniz"] }],
    needs: [{ label: "Invoicing", count: 2, pct: 50 }],
    alternatives: [{ label: "Spreadsheets", count: 2, pct: 50 }],
    quotes: [{ persona: "Ayse Demir", segment: "Freelancers", role: "UX designer", text: "too pricey for me", topic: "price", sentiment: "negative" }],
    recommendations: [{ title: "Add a free tier", detail: "Lower the barrier to a first project.", priority: "high" }],
    open_questions: [{ question: "What would convince you?", digest: "A free tier." }],
    methodology: "4 synthetic personas were each asked the same questions.",
    disclaimer: "These findings come from synthetic personas, not real customers.",
    ...overrides,
  };
}

export function audienceDetail(overrides: Partial<AudienceDetail> = {}): AudienceDetail {
  return {
    ...audience(),
    personas: [persona(), persona({ id: "p2", name: "Can Yilmaz", role: "Agency lead", segment: "Agency leads", quote: undefined })],
    studies: [study()],
    build_calls: 2,
    ...overrides,
  };
}

export function studyDetail(overrides: Partial<AudienceStudyDetail> = {}): AudienceStudyDetail {
  return {
    ...study({ report: report() }),
    answers: [
      {
        id: "x1",
        persona_id: "p1",
        persona: { name: "Ayse Demir", segment: "Freelancers", role: "UX designer" },
        status: "ok",
        answers: {
          intent_score: 2,
          verdict: "no",
          would_pay_now: false,
          reasoning: "too pricey for me",
          max_price: 5,
          price_reactions: [{ price: 9, would_buy: false }],
          objections: ["Too expensive"],
        },
      },
      { id: "x2", persona_id: "p2", persona: { name: "Can Yilmaz", segment: "Agency leads" }, status: "failed", error: "no answer", answers: { intent_score: 0, verdict: "no", would_pay_now: false } },
    ],
    follow_ups: [{ group_id: "g1", question: "One thing missing?", asked_at: "2026-11-11T10:30:00Z", asked: 2, replies: [{ persona_id: "p1", persona: "Ayse Demir", text: "Invoicing." }] }],
    running: false,
    ...overrides,
  };
}
