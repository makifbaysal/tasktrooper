import { describe, expect, it } from "vitest";
import { keyStoreHelp, undecryptableHelp } from "./keystore.js";

describe("keyStoreHelp", () => {
  it("names the login keychain only on macOS", () => {
    expect(keyStoreHelp("darwin").remedy).toContain("login keychain");
    for (const platform of ["win32", "linux"]) {
      const help = keyStoreHelp(platform);
      expect(`${help.failure} ${help.remedy}`).not.toMatch(/keychain|macOS/);
    }
  });

  it("names the OS's own key store", () => {
    expect(keyStoreHelp("win32").failure).toContain("Windows");
    expect(keyStoreHelp("linux").failure).toMatch(/GNOME Keyring|KWallet/);
  });

  /**
   * Linux's basic_text backend is Electron's built-in key: it obscures, it
   * does not protect. A message promising "nothing is written unencrypted"
   * there would be false.
   */
  it("does not claim on Linux that nothing is ever written without real encryption", () => {
    expect(keyStoreHelp("linux").fallback).not.toMatch(/not offered/);
    expect(keyStoreHelp("linux").fallback).toMatch(/built-in key/);
    expect(keyStoreHelp("darwin").fallback).toMatch(/not offered/);
  });
});

describe("undecryptableHelp", () => {
  it("says how to recover, and what the irreversible way out costs", () => {
    const text = undecryptableHelp("/home/me/.config/TaskTrooper/local.bin");
    expect(text).toMatch(/Unlock or start it/);
    expect(text).toContain("/home/me/.config/TaskTrooper/local.bin");
    expect(text).toMatch(/cannot be recovered/);
  });
});
