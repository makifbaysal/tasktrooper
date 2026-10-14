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

const { confirmInstall, consentDialog } = await import("./consent.js");

const PLAN = [
  { id: "homebrew" as const, label: "Homebrew", method: "Terminal" },
  { id: "node" as const, label: "Node.js", method: "Homebrew" },
];

describe("the install consent dialog", () => {
  it("lists every planned tool with how it is installed, with Cancel as both the default and the escape", () => {
    const options = consentDialog(PLAN);
    expect(options.message).toBe("Install developer tools?");
    expect(options.detail).toContain("• Homebrew — Terminal");
    expect(options.detail).toContain("• Node.js — Homebrew");
    expect(options.buttons).toEqual(["Install", "Cancel"]);
    expect(options.cancelId).toBe(1);
    expect(options.defaultId).toBe(1);
  });

  it("is a yes only for Install, and is shown on the given window", async () => {
    const window = { isDestroyed: () => false };
    shown.response = 0;
    await expect(confirmInstall(window as never, PLAN)).resolves.toBe(true);
    expect(shown.calls.at(-1)?.parent).toBe(window);
    shown.response = 1;
    await expect(confirmInstall(window as never, PLAN)).resolves.toBe(false);
    await expect(confirmInstall(null, PLAN)).resolves.toBe(false);
    expect(shown.calls.at(-1)?.parent).toBeNull();
  });
});
