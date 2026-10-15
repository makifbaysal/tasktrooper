import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ nativeImage: {} }));

const { drawGlyph, trayTone } = await import("./tray-icons.js");

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

describe("drawGlyph", () => {
  const size = 36;
  const alphaAt = (buffer: Buffer, x: number, y: number) => buffer[(y * size + x) * 4 + 3]!;
  const column = (buffer: Buffer, x: number) => Array.from({ length: size }, (_, y) => alphaAt(buffer, x, y));
  /** The solid runs down the middle of the mark: one per chevron when the gaps stay open. */
  const runs = (alphas: number[]) => alphas.filter((a, i) => a > 128 && !(alphas[i - 1]! > 128)).length;

  it("draws the logo's three chevrons with clear gaps between them", () => {
    expect(runs(column(drawGlyph("running", size, "template"), size / 2))).toBe(3);
  });

  it("is solid when running and faint all over when stopped", () => {
    expect(Math.max(...column(drawGlyph("running", size, "template"), size / 2))).toBe(255);
    const stopped = Math.max(...column(drawGlyph("stopped", size, "template"), size / 2));
    expect(stopped).toBeGreaterThan(0);
    expect(stopped).toBeLessThan(128);
  });

  it("fades only the top chevron when degraded", () => {
    const middle = column(drawGlyph("degraded", size, "template"), size / 2);
    expect(Math.max(...middle.slice(0, size / 3))).toBeLessThan(128);
    expect(Math.max(...middle.slice((size * 2) / 3))).toBe(255);
  });

  it("is black for a template image and white for a dark panel", () => {
    const at = (tone: "template" | "light") => drawGlyph("running", size, tone).subarray(((size - 4) * size + size / 2) * 4, ((size - 4) * size + size / 2) * 4 + 4);
    expect([...at("template")]).toEqual([0, 0, 0, 255]);
    expect([...at("light")]).toEqual([255, 255, 255, 255]);
  });
});
