import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ nativeImage: {} }));

const { trayTone } = await import("./tray-icons.js");

describe("trayTone", () => {
  it("keeps macOS on a template image, which the menu bar recolours itself", () => {
    expect(trayTone("darwin", { systemDark: false, appDark: false })).toBe("template");
    expect(trayTone("darwin", { systemDark: true, appDark: true })).toBe("template");
  });

  /**
   * Windows 10's default is a dark taskbar with light apps. The taskbar's own
   * theme is the one that decides whether a glyph on it can be seen.
   */
  it("follows the Windows taskbar's theme, not the apps'", () => {
    expect(trayTone("win32", { systemDark: true, appDark: false })).toBe("light");
    expect(trayTone("win32", { systemDark: false, appDark: true })).toBe("dark");
  });

  it("draws white on GNOME's top bar, which is dark in light mode too", () => {
    expect(trayTone("linux", { systemDark: false, appDark: false }, "ubuntu:GNOME")).toBe("light");
    expect(trayTone("linux", { systemDark: false, appDark: false }, "")).toBe("light");
  });

  it("follows the colour scheme on KDE, whose panel does", () => {
    expect(trayTone("linux", { systemDark: false, appDark: false }, "KDE")).toBe("dark");
    expect(trayTone("linux", { systemDark: false, appDark: true }, "KDE")).toBe("light");
  });
});
