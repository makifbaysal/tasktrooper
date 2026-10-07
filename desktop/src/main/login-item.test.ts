import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ app: {} }));

const { autostartEntry, autostartFile, desktopExecArg, launchedHidden } = await import("./login-item.js");

describe("autostartFile", () => {
  it("lives in the XDG config home, falling back to ~/.config", () => {
    expect(autostartFile({ XDG_CONFIG_HOME: "/cfg" }, "/home/me")).toBe("/cfg/autostart/tasktrooper.desktop");
    expect(autostartFile({}, "/home/me")).toBe("/home/me/.config/autostart/tasktrooper.desktop");
    // A relative XDG_CONFIG_HOME is invalid per the spec and ignored.
    expect(autostartFile({ XDG_CONFIG_HOME: "cfg" }, "/home/me")).toBe("/home/me/.config/autostart/tasktrooper.desktop");
  });
});

describe("autostartEntry", () => {
  it("starts the AppImage itself, hidden", () => {
    const entry = autostartEntry("/home/me/Applications/TaskTrooper-0.1.11-x86_64.AppImage");
    expect(entry).toContain('Exec="/home/me/Applications/TaskTrooper-0.1.11-x86_64.AppImage" --hidden\n');
    expect(entry.startsWith("[Desktop Entry]\nType=Application\n")).toBe(true);
  });

  it("quotes a path with spaces and escapes what the spec reserves", () => {
    expect(desktopExecArg("/home/me/My Apps/Task Trooper")).toBe('"/home/me/My Apps/Task Trooper"');
    expect(desktopExecArg('/x/a"b')).toBe('"/x/a\\\\"b"');
    expect(desktopExecArg("/x/$HOME")).toBe('"/x/\\\\$HOME"');
    expect(desktopExecArg("/x/100%")).toBe('"/x/100%%"');
    expect(desktopExecArg("/x/a\\b")).toBe('"/x/a\\\\\\\\b"');
  });
});

describe("launchedHidden", () => {
  it("is the login item's --hidden on Windows and Linux, and never on macOS", () => {
    expect(launchedHidden(["/opt/TaskTrooper/tasktrooper", "--hidden"], "linux")).toBe(true);
    expect(launchedHidden(["C:\\TaskTrooper.exe", "--hidden"], "win32")).toBe(true);
    expect(launchedHidden(["C:\\TaskTrooper.exe"], "win32")).toBe(false);
    expect(launchedHidden(["/Applications/TaskTrooper.app/Contents/MacOS/TaskTrooper", "--hidden"], "darwin")).toBe(false);
  });
});
