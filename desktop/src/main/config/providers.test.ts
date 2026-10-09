import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ safeStorage: {}, app: { getPath: () => "/userData" } }));

const { asProviders } = await import("./providers.js");

/**
 * The member's API keys go from this file to the runner's stdin and on to the
 * executor's. The runner refuses a list that breaks its rules at startup,
 * which would be a runner that never attaches over a typo; so the same rules
 * are held here, before anything is written and again when it is read.
 */
describe("asProviders", () => {
  const openai = { id: "openai", type: "openai", api_key: "sk-1", models: ["gpt-4.1"] };

  it("accepts the executor contract's shape, keys and timeouts included", () => {
    const list = [
      openai,
      { id: "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab", type: "openai_compatible", base_url: "https://openrouter.ai/api/v1", api_key: "k", timeout_seconds: 120 },
      { id: "local", type: "local", base_url: "http://127.0.0.1:1234/v1" },
    ];
    expect(asProviders(list)).toEqual(list);
    expect(asProviders([])).toEqual([]);
  });

  it("refuses a key that would travel over plain http to another machine", () => {
    expect(asProviders([{ ...openai, base_url: "http://llm.example.com/v1" }])).toBeNull();
    expect(asProviders([{ ...openai, base_url: "https://user:pw@llm.example.com" }])).toBeNull();
  });

  it("refuses what the runner would refuse: a flag for an id, a duplicate, a header in a key", () => {
    for (const list of [
      [{ ...openai, id: "-x" }],
      [openai, openai],
      [{ ...openai, api_key: "sk\nX-Evil: 1" }],
      [{ ...openai, models: [""] }],
      [{ ...openai, timeout_seconds: -1 }],
      [{ id: "openai" }],
      "not a list",
    ]) {
      expect(asProviders(list), JSON.stringify(list)).toBeNull();
    }
  });
});
