/**
 * What failed and what fixes it when `safeStorage` cannot get a key, in the
 * terms of the OS that holds it. No Electron import, so the supervisor can
 * word its own refusal without loading `safeStorage`.
 */
export interface KeyStoreHelp {
  failure: string;
  remedy: string;
}

export function keyStoreHelp(platform: string = process.platform): KeyStoreHelp {
  switch (platform) {
    case "darwin":
      return {
        failure: "macOS could not provide an encryption key from the login keychain",
        remedy: "Unlock the login keychain and try again",
      };
    case "win32":
      return {
        failure: "Windows could not provide an encryption key for this user account",
        remedy: "Sign out of Windows and back in, then try again",
      };
    default:
      return {
        failure: "The system keyring (GNOME Keyring or KWallet) could not provide an encryption key",
        remedy: "Unlock the keyring and try again",
      };
  }
}
