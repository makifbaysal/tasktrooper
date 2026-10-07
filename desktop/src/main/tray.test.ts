import { describe, expect, it, vi } from "vitest";
import type { SupervisorSnapshot } from "../ipc/types.js";

const native = vi.hoisted(() => ({ menus: 0, images: 0 }));

vi.mock("electron", () => ({
  Tray: class {
    setImage(): void {
      native.images += 1;
    }
    setToolTip(): void {}
    setContextMenu(): void {}
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
