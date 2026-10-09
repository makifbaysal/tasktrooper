import { describe, expect, it } from "vitest";
import { emptyMcpForm, mcpFormFor, toMcpRequest } from "./mcp-form";

describe("toMcpRequest", () => {
  it("builds a stdio request with arguments one per line and the variables typed", () => {
    expect(
      toMcpRequest({
        ...emptyMcpForm(),
        name: " notes ",
        command: " /bin/notes ",
        args: "--stdio\n\n --verbose ",
        secrets: [
          { name: "NOTES_TOKEN", value: "t" },
          { name: " ", value: "ignored" },
        ],
      }),
    ).toEqual({ name: "notes", command: "/bin/notes", args: ["--stdio", "--verbose"], env: { NOTES_TOKEN: "t" } });
  });

  it("builds an http request with headers, and none of the stdio fields", () => {
    expect(
      toMcpRequest({ ...emptyMcpForm(), name: "wiki", transport: "http", command: "ignored", url: " https://wiki.example/mcp ", secrets: [{ name: "Authorization", value: "Bearer x" }] }),
    ).toEqual({ name: "wiki", url: "https://wiki.example/mcp", headers: { Authorization: "Bearer x" } });
  });
});

describe("mcpFormFor", () => {
  it("shows the stored server with its secret names and no value, so an empty value keeps it", () => {
    const form = mcpFormFor({ name: "notes", transport: "stdio", command: "/bin/notes", args: ["--stdio"], secretNames: ["NOTES_TOKEN"], hasSecret: true });
    expect(form.secrets).toEqual([{ name: "NOTES_TOKEN", value: "" }]);
    expect(toMcpRequest(form)).toEqual({ name: "notes", command: "/bin/notes", args: ["--stdio"], env: { NOTES_TOKEN: "" } });
  });
});
