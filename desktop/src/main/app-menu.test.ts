import { describe, expect, it } from "vitest";
import { applicationMenuTemplate } from "./app-menu.js";

describe("applicationMenuTemplate", () => {
  it("binds Ctrl+Q to the draining quit on Windows and Linux", () => {
    for (const platform of ["win32", "linux"] as const) {
      const file = applicationMenuTemplate(platform, false)[0];
      const quit = (file?.submenu as { role?: string; accelerator?: string }[])[0];
      expect(quit).toMatchObject({ role: "quit", accelerator: "Ctrl+Q" });
    }
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
