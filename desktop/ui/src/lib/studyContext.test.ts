import { describe, expect, it } from "vitest";
import { contextProblem, contextToInput, emptyContext } from "./studyContext";

describe("contextProblem", () => {
  it("needs nothing for user interviews", () => {
    expect(contextProblem("user_interviews", emptyContext())).toBeNull();
  });

  it("needs a readable page for a landing page test", () => {
    const c = emptyContext();
    expect(contextProblem("landing_page_test", c)).toBe("needUrl");
    c.pages[0].url = "acme.test";
    expect(contextProblem("landing_page_test", c)).toBe("badUrl");
    c.pages[0].url = "https://acme.test";
    expect(contextProblem("landing_page_test", c)).toBeNull();
    c.pages[0] = { mode: "paste", url: "", copy: "  " };
    expect(contextProblem("landing_page_test", c)).toBe("needCopy");
  });

  it("names which page of an A/B test is missing", () => {
    const c = emptyContext();
    c.pages[0].url = "https://a.test";
    expect(contextProblem("ab_test", c)).toBe("needUrlB");
    c.pages[1] = { mode: "paste", url: "", copy: "page b" };
    expect(contextProblem("ab_test", c)).toBeNull();
  });

  it("needs two different lines and at most five", () => {
    const c = emptyContext();
    c.variants = ["One", " one ", ""];
    expect(contextProblem("message_testing", c)).toBe("needVariants");
    c.variants = ["One", "Two"];
    expect(contextProblem("message_testing", c)).toBeNull();
    c.variants = ["1", "2", "3", "4", "5", "6"];
    expect(contextProblem("message_testing", c)).toBe("tooManyVariants");
  });
});

describe("contextToInput", () => {
  it("sends prices only with user interviews", () => {
    const c = emptyContext();
    c.goal = " Would they pay? ";
    c.prices = "19, 9";
    expect(contextToInput("user_interviews", c)).toEqual({ kind: "user_interviews", goal: "Would they pay?", price_points: [9, 19], currency: "USD" });
    expect(contextToInput("landing_page_test", { ...c, pages: [{ mode: "paste", url: "", copy: "Copy" }, c.pages[1]] })).toEqual({
      kind: "landing_page_test",
      goal: "Would they pay?",
      inputs: { intent: "evaluate", pages: [{ copy: "Copy" }] },
    });
  });

  it("sends the lines and the two pages", () => {
    const c = emptyContext();
    c.variants = [" A ", "", "B"];
    expect(contextToInput("message_testing", c).inputs).toEqual({ variants: ["A", "B"] });
    c.pages = [{ mode: "url", url: " https://a.test ", copy: "" }, { mode: "paste", url: "", copy: "B copy" }];
    c.intent = " buy ";
    expect(contextToInput("ab_test", c).inputs).toEqual({ intent: "buy", pages: [{ url: "https://a.test" }, { copy: "B copy" }] });
  });
});
