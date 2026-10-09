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
 * macOS keeps the platform's own menu, rebuilt only so the app menu can carry
 * API keys… after About; Edit, View and Window stay Electron's roles.
 */
export function applicationMenuTemplate(
  platform: NodeJS.Platform,
  devTools: boolean,
  actions: { openKeys?: () => void } = {},
): MenuItemConstructorOptions[] {
  const { openKeys } = actions;
  if (platform === "darwin") {
    return [
      {
        label: "TaskTrooper",
        submenu: [
          { role: "about" },
          { type: "separator" },
          ...(openKeys ? [{ label: "API keys…", click: () => openKeys() }, { type: "separator" as const }] : []),
          { role: "services" },
          { type: "separator" },
          { role: "hide" },
          { role: "hideOthers" },
          { role: "unhide" },
          { type: "separator" },
          { role: "quit" },
        ],
      },
      { role: "fileMenu" },
      { role: "editMenu" },
      { role: "viewMenu" },
      { role: "windowMenu" },
    ];
  }
  return [
    {
      label: "&File",
      submenu: [
        ...(openKeys ? [{ label: "API &keys…", click: () => openKeys() }, { type: "separator" as const }] : []),
        // The role's quit is `app.quit()`, which `before-quit` turns into the
        // draining quit — the same path as the tray's Quit.
        { role: "quit", label: platform === "win32" ? "E&xit" : "&Quit", accelerator: "Ctrl+Q" },
      ],
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
