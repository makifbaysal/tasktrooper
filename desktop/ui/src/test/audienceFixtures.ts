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
    kind: "user_interviews",
    inputs: {},
    script_state: "none",
    script_calls: 0,
    script_version: 0,
    script_approved_version: 0,
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
    pending: [],
    follow_ups: [{ group_id: "g1", question: "One thing missing?", asked_at: "2026-11-11T10:30:00Z", asked: 2, replies: [{ persona_id: "p1", persona: "Ayse Demir", text: "Invoicing." }] }],
    running: false,
    ...overrides,
  };
}

export function landingReport(overrides: Partial<AudienceReport> = {}): AudienceReport {
  return report({
    kind: "landing_page_test",
    summary: "The page works but hides its price.",
    verdict: { label: "promising", score: 62, rationale: "Clear offer, no price." },
    objections: [{ label: "No pricing", count: 3, pct: 75, personas: ["Ayse Demir", "Can"] }],
    needs: [{ label: "A visible price", count: 2, pct: 50 }],
    alternatives: [],
    strengths: [{ label: "4,000 freelancers", count: 2, pct: 50 }],
    landing: {
      intent: "buy",
      source: "https://acme.test/",
      clarity_mean: 7.5,
      clarity_by_segment: [
        { name: "Freelancers", value: 8 },
        { name: "Agency leads", value: 6 },
      ],
    },
    recommendations: [{ title: "Show the price", detail: "Above the fold.", priority: "high" }],
    methodology: "4 synthetic personas read the page as first-time visitors.",
    ...overrides,
  });
}

export function messagesReport(overrides: Partial<AudienceReport> = {}): AudienceReport {
  return report({
    kind: "message_testing",
    summary: "The plain line wins.",
    verdict: { label: "strong", score: 90, rationale: "Clarity." },
    alternatives: [],
    strengths: [{ label: "Says what it does", count: 3, pct: 75 }],
    messages: {
      winner: 2,
      variants: [
        { variant: 1, text: "Boards for agents", rank: 2, clarity: 3, relevance: 3, credibility: 3, appeal: 3, overall: 3, best_pct: 0 },
        {
          variant: 2,
          text: "Invoices that chase themselves",
          rank: 1,
          clarity: 9,
          relevance: 9,
          credibility: 9,
          appeal: 9,
          overall: 9,
          best_pct: 100,
          by_segment: [
            { name: "Freelancers", value: 9 },
            { name: "Agency leads", value: 8.5 },
          ],
        },
      ],
      rewrites: [{ persona: "Ayse Demir", segment: "Freelancers", text: "Get paid faster" }],
    },
    recommendations: [{ title: "Get paid faster", detail: "Shorter.", priority: "high" }],
    methodology: "4 synthetic personas read the lines in a rotated order.",
    ...overrides,
  });
}

export function abReport(overrides: Partial<AudienceReport> = {}): AudienceReport {
  return report({
    kind: "ab_test",
    summary: "Version A is clearer.",
    verdict: { label: "promising", score: 80 },
    alternatives: [],
    ab: {
      winner: "A",
      confidence: "lean",
      first_shown_win_pct: 80,
      act: { a: { mean: 7.5, median: 8 }, b: { mean: 4, median: 3.5 }, diff: -3.5, a_higher: 4, b_higher: 0, same: 0, effect_size: 1.4, effect_label: "large" },
      clarity: { a: { mean: 8, median: 8 }, b: { mean: 5, median: 5 }, diff: -3, a_higher: 3, b_higher: 1, same: 0, effect_size: 0.9, effect_label: "medium" },
      sides: [
        {
          label: "A",
          source: "https://a.test/",
          wins: 3,
          win_pct: 75,
          mean_score: 7.5,
          act_pct: 75,
          win_pct_by_segment: [
            { name: "Freelancers", value: 100 },
            { name: "Agency leads", value: 0 },
          ],
          issues: [{ label: "vague", count: 2, pct: 50 }],
          reasons: [{ label: "Clear offer", count: 3, pct: 75 }],
          quotes: [{ persona: "Ayse Demir", segment: "Freelancers", text: "I got it at once.", version: "A" }],
        },
        {
          label: "B",
          wins: 1,
          win_pct: 25,
          mean_score: 4,
          act_pct: 25,
          issues: [{ label: "jargon", count: 3, pct: 75 }],
          quotes: [{ persona: "Can", text: "Too much jargon.", version: "B" }],
        },
      ],
    },
    recommendations: [{ title: "Ship version A", priority: "high" }],
    methodology: "4 synthetic personas saw both versions.",
    ...overrides,
  });
}
