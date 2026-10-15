import { describe, expect, it, vi } from "vitest";

const shown = vi.hoisted(() => ({ calls: [] as { parent: unknown; options: Record<string, unknown> }[], response: 0 }));

vi.mock("electron", () => ({
  dialog: {
    showMessageBox: (...args: unknown[]) => {
      const [parent, options] = args.length === 2 ? args : [null, args[0]];
      shown.calls.push({ parent, options: options as Record<string, unknown> });
      return Promise.resolve({ response: shown.response, checkboxChecked: false });
    },
  },
}));

const { confirmMcpServer, mcpConsentDialog, shownArg, visible } = await import("./consent.js");

const ADDED = {
  name: "blender",
  added: true,
  command: "uvx",
  args: ["mcp-for-blender"],
  values: [["BLENDER_MCP_SAFE_MODE", "1"]] as [string, string][],
  kept: [],
};

describe("the MCP server consent dialog", () => {
  it("names the server, the exact command and its values, with Cancel as both the default and the escape", () => {
    const options = mcpConsentDialog(ADDED);
    expect(options.message).toBe('Add the MCP server "blender" to this computer?');
    expect(options.detail).toContain("uvx mcp-for-blender");
    expect(options.detail).toContain("• BLENDER_MCP_SAFE_MODE=1");
    expect(options.buttons).toEqual(["Add", "Cancel"]);
    expect(options.defaultId).toBe(1);
    expect(options.cancelId).toBe(1);
    expect(mcpConsentDialog({ ...ADDED, added: false, values: [] }).buttons).toEqual(["Change", "Cancel"]);
  });

  it("writes out what would hide or reorder text, and quotes an argument that is not plain", () => {
    expect(visible("a\nb\u202Ec")).toBe("a\\u{a}b\\u{202e}c");
    expect(visible("a\\u{a}b")).toBe("a\\\\u{a}b");
    expect(visible("a\\u{a}b")).not.toBe(visible("a\nb"));
    expect(shownArg('C:\\dir\\"x"')).toBe('"C:\\\\dir\\\\\\"x\\""');
    expect(shownArg("--stdio")).toBe("--stdio");
    expect(shownArg("two words")).toBe('"two words"');
    expect(shownArg("")).toBe('""');
    const options = mcpConsentDialog({ ...ADDED, args: ["x\nrm -rf ~"], values: [["NODE_OPTIONS", "a\u202Eb"]] });
    expect(options.detail).toContain('uvx "x\\u{a}rm -rf ~"');
    expect(options.detail).toContain("• NODE_OPTIONS=a\\u{202e}b");
  });

  it("says which saved values go with a changed server", () => {
    const options = mcpConsentDialog({ ...ADDED, added: false, url: "https://evil.example/mcp", command: undefined, args: undefined, values: [], kept: ["Authorization"] });
    expect(options.detail).toContain("https://evil.example/mcp");
    expect(options.detail).toContain("Your saved values go with it: Authorization.");
  });

  it("is a yes only for its first button, shown on the given window", async () => {
    const window = { isDestroyed: () => false };
    shown.response = 0;
    await expect(confirmMcpServer(window as never, ADDED)).resolves.toBe(true);
    expect(shown.calls.at(-1)?.parent).toBe(window);
    shown.response = 1;
    await expect(confirmMcpServer(window as never, ADDED)).resolves.toBe(false);
    await expect(confirmMcpServer(null, ADDED)).resolves.toBe(false);
  });
});
