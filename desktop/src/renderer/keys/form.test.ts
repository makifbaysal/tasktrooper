import { describe, expect, it } from "vitest";
import { emptyForm, formForPrefill, toRequest } from "./form";

const UUID = "8f0e7c0a-1b2c-4d5e-9f00-0123456789ab";

describe("formForPrefill", () => {
  it("shows the provider the page asked for, fixed, with nothing in the key field", () => {
    const form = formForPrefill({
      id: UUID,
      type: "openai_compatible",
      base_url: "https://llm.example/v1",
      models: ["a", "b"],
      name: "Team gateway",
    });
    expect(form).toEqual({
      id: UUID,
      type: "openai_compatible",
      baseUrl: "https://llm.example/v1",
      models: "a, b",
      apiKey: "",
      locked: true,
      name: "Team gateway",
    });
  });

  it("sends the key only once it has been typed here, with the prefilled identity", () => {
    const form = formForPrefill({ id: "openai", type: "openai" });
    expect(toRequest(form)).toEqual({ id: "openai", type: "openai" });
    expect(toRequest({ ...form, apiKey: " sk-typed " })).toEqual({ id: "openai", type: "openai", api_key: "sk-typed" });
  });

  it("is unlocked for a window opened plain", () => {
    expect(emptyForm().locked).toBeUndefined();
  });
});
