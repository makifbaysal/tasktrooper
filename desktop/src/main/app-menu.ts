import type { MenuItemConstructorOptions } from "electron";

/**
 * The application menu on Windows and Linux.
 *
 * Electron's default there is a File/Edit/View/Window/Help bar across the top
 * of the window that belongs to Electron rather than to this app. It is
 * replaced rather than removed: with no menu at all, nothing binds Ctrl+Q, and
 * the Edit roles are what keep cut/copy/paste/undo bound in every text field
 * regardless of how a renderer handles its keys. The window hides the bar
 * (`autoHideMenuBar`), so Alt shows it and nothing else does.
 *
 * macOS keeps Electron's default menu, which already is the platform's own.
 */
export function applicationMenuTemplate(platform: NodeJS.Platform, devTools: boolean): MenuItemConstructorOptions[] {
  return [
    {
      label: "&File",
      // The role's quit is `app.quit()`, which `before-quit` turns into the
      // draining quit — the same path as the tray's Quit.
      submenu: [{ role: "quit", label: platform === "win32" ? "E&xit" : "&Quit", accelerator: "Ctrl+Q" }],
    },
    { role: "editMenu" },
    {
      label: "&View",
      submenu: [
        { role: "resetZoom" },
        { role: "zoomIn" },
        { role: "zoomOut" },
        { type: "separator" },
        { role: "togglefullscreen" },
        ...(devTools ? [{ type: "separator" as const }, { role: "toggleDevTools" as const }] : []),
      ],
    },
  ];
}
