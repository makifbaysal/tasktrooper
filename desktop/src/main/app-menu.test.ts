import { describe, expect, it } from "vitest";
import { applicationMenuTemplate } from "./app-menu.js";

describe("applicationMenuTemplate", () => {
  it("binds Ctrl+Q to the draining quit on Windows and Linux", () => {
    for (const platform of ["win32", "linux"] as const) {
      const file = applicationMenuTemplate(platform, false, { openKeys: () => undefined })[0];
      const quit = (file?.submenu as { role?: string; accelerator?: string }[]).find((item) => item.role === "quit");
      expect(quit).toMatchObject({ role: "quit", accelerator: "Ctrl+Q" });
    }
  });

  it("opens the API-key window from File, when it is given one", () => {
    let opened = 0;
    const file = applicationMenuTemplate("linux", false, { openKeys: () => (opened += 1) })[0];
    const item = (file?.submenu as { label?: string; click?: () => void }[]).find((i) => i.label === "API &keys…");
    item?.click?.();
    expect(opened).toBe(1);
    const bare = applicationMenuTemplate("linux", false)[0];
    expect(JSON.stringify(bare)).not.toContain("API");
  });

  it("puts API keys… in the macOS app menu and keeps the default roles", () => {
    let opened = 0;
    const menu = applicationMenuTemplate("darwin", false, { openKeys: () => (opened += 1) });
    const app = menu[0]?.submenu as { label?: string; role?: string; click?: () => void }[];
    const at = app.findIndex((i) => i.label === "API keys…");
    expect(at).toBeGreaterThan(app.findIndex((i) => i.role === "about"));
    expect(at).toBeLessThan(app.findIndex((i) => i.role === "quit"));
    app[at]?.click?.();
    expect(opened).toBe(1);
    const roles = menu.map((item) => item.role);
    expect(roles).toEqual(expect.arrayContaining(["editMenu", "windowMenu"]));
    expect(JSON.stringify(applicationMenuTemplate("darwin", false))).not.toContain("API");
  });

  it("keeps the Edit roles that bind copy and paste in text fields", () => {
    expect(applicationMenuTemplate("linux", false).some((item) => item.role === "editMenu")).toBe(true);
  });

  it("offers DevTools only to a development build", () => {
    const has = (devTools: boolean): boolean =>
      JSON.stringify(applicationMenuTemplate("win32", devTools)).includes("toggleDevTools");
    expect(has(false)).toBe(false);
    expect(has(true)).toBe(true);
  });
});
