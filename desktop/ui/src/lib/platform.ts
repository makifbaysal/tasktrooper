// The desktop bridge's info() also knows the platform, but it is async and
// absent in a browser; the user agent answers synchronously in both.
export function isMacPlatform(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.userAgent);
}

export function shortcutModifier(): string {
  return isMacPlatform() ? "⌘" : "Ctrl+";
}
