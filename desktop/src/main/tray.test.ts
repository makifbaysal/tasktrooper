import { describe, expect, it, vi } from "vitest";
import type { SupervisorSnapshot } from "../ipc/types.js";

const native = vi.hoisted(() => ({ menus: 0, images: 0, menu: [] as { label?: string; enabled?: boolean; click?: () => void }[], tooltip: "" }));

vi.mock("electron", () => ({
  Tray: class {
    setImage(): void {
      native.images += 1;
    }
    setToolTip(tip: string): void {
      native.tooltip = tip;
    }
    setContextMenu(menu: typeof native.menu): void {
      native.menu = menu;
    }
    on(): void {}
    destroy(): void {}
  },
  Menu: {
    buildFromTemplate: (template: unknown) => {
      native.menus += 1;
      return template;
    },
  },
  nativeTheme: { on: () => undefined, off: () => undefined, shouldUseDarkColors: false, shouldUseDarkColorsForSystemIntegratedUI: false },
  nativeImage: { createFromBuffer: () => ({ setTemplateImage: () => undefined }), createFromDataURL: () => ({}) },
}));

vi.mock("./tray-icons.js", () => ({ trayIcon: () => ({}), trayTone: () => "light" }));

const { AppTray } = await import("./tray.js");

const deps = {
  showWindow: () => undefined,
  start: () => undefined,
  stop: () => undefined,
  quit: () => undefined,
  checkForUpdate: () => undefined,
  restartToUpdate: () => undefined,
};

function snapshot(patch: Partial<SupervisorSnapshot>): SupervisorSnapshot {
  return { state: "running", since: 1, children: [], ...patch };
}

describe("AppTray", () => {
  it("rebuilds only when something it draws has changed", () => {
    const tray = new AppTray(deps);
    tray.create();
    const afterCreate = native.menus;

    // Narration and timestamps change on every supervisor event; the tray draws neither.
    tray.update(snapshot({ detail: "Waiting for the local server…", since: 2 }));
    const first = native.menus;
    tray.update(snapshot({ detail: "The local server is ready.", since: 3 }));
    tray.update(snapshot({ detail: "something else", since: 4 }));
    expect(native.menus).toBe(first);
    expect(first).toBe(afterCreate + 1);

    tray.update(snapshot({ state: "stopped" }));
    expect(native.menus).toBe(first + 1);

    tray.updateStatus({ phase: "available", percent: 10 });
    tray.updateStatus({ phase: "available", percent: 10 });
    expect(native.menus).toBe(first + 2);
    tray.updateStatus({ phase: "available", percent: 11 });
    expect(native.menus).toBe(first + 3);
  });
});

describe("AppTray: running locally for now", () => {
  it("has no API keys item: the account's LLM Connection page opens the key window", () => {
    const tray = new AppTray(deps);
    tray.create();
    expect(native.menu.some((item) => item.label?.includes("API keys"))).toBe(false);
  });

  it("says so at the top while running locally for now, with the way back, and drops it after", () => {
    let banner: { label: string; action: string; run: () => void } | null = null;
    let back = 0;
    const tray = new AppTray({ ...deps, banner: () => banner });
    tray.create();
    expect(native.menu[0]?.label).not.toMatch(/for now/);

    banner = { label: "Using TaskTrooper without your account for now", action: "Back to app.tasktrooper.ai", run: () => (back += 1) };
    tray.update(snapshot({ state: "running" }));
    expect(native.menu[0]).toMatchObject({ label: banner.label, enabled: false });
    expect(native.tooltip).toContain("for now");
    native.menu[1]?.click?.();
    expect(back).toBe(1);

    banner = null;
    tray.update(snapshot({ state: "running" }));
    expect(native.menu.some((item) => item.label?.includes("for now"))).toBe(false);
  });
});
