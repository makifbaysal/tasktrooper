/**
 * What failed and what fixes it when `safeStorage` cannot get a key, in the
 * terms of the OS that holds it. No Electron import, so the supervisor can
 * word its own refusal without loading `safeStorage`.
 */
export interface KeyStoreHelp {
  failure: string;
  remedy: string;
  /**
   * What this app does when there is no OS key store at all, said as it is.
   * Linux's `basic_text` backend encrypts with a key compiled into Electron,
   * which hides the file from a casual look and from nobody else.
   */
  fallback: string;
}

export function keyStoreHelp(platform: string = process.platform): KeyStoreHelp {
  switch (platform) {
    case "darwin":
      return {
        failure: "macOS could not provide an encryption key from the login keychain",
        remedy: "Unlock the login keychain and try again",
        fallback: "Writing them unencrypted is not offered.",
      };
    case "win32":
      return {
        failure: "Windows could not provide an encryption key for this user account",
        remedy: "Sign out of Windows and back in, then try again",
        fallback: "Writing them unencrypted is not offered.",
      };
    default:
      return {
        failure: "The system keyring (GNOME Keyring or KWallet) could not provide an encryption key",
        remedy: "Unlock the keyring and try again",
        fallback:
          "Only on a session with no keyring at all does this app fall back to Electron's built-in key, which " +
          "obscures the file rather than protecting it.",
      };
  }
}

/**
 * The refusal for a `local.bin` that exists and will not decrypt on Linux.
 *
 * There the backend `safeStorage` uses is chosen per session — a keyring when
 * one is running and unlocked, Electron's built-in key when none is — so the
 * same file can decrypt on Monday and not on Tuesday. Generating new secrets
 * then would replace `mcp_secrets_key` and permanently orphan every provider
 * credential the backend stored under the old one, so the app stops and says
 * so instead. The way out of a genuinely lost keyring is spelled out, because
 * it is the user's call to make and not this app's.
 */
export function undecryptableHelp(file: string): string {
  return (
    `TaskTrooper's stored credentials (${file}) were encrypted with a key from the system keyring, and this ` +
    "session cannot provide that key: the keyring (GNOME Keyring or KWallet) is locked, not running, or not the " +
    "one that was used before. Unlock or start it, then press Connect again. If the keyring was reset, the " +
    `stored credentials cannot be recovered: delete ${file} and restart TaskTrooper to create new ones, then ` +
    "enter the provider keys you saved in TaskTrooper again."
  );
}
