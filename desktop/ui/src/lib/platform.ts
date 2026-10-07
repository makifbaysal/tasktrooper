// The desktop bridge's info() also knows the platform, but it is async and
// absent in a browser; the user agent answers synchronously in both.
export function isMacPlatform(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.userAgent);
}

export function isWindowsPlatform(): boolean {
  return typeof navigator !== "undefined" && /Windows/.test(navigator.userAgent);
}

/** An absolute folder path in this OS's own shape, for input placeholders. */
export function exampleFolderPath(): string {
  if (isWindowsPlatform()) return "C:\\Users\\you\\projects\\app";
  if (isMacPlatform()) return "/Users/you/projects/app";
  return "/home/you/projects/app";
}

export function shortcutModifier(): string {
  return isMacPlatform() ? "⌘" : "Ctrl+";
}
