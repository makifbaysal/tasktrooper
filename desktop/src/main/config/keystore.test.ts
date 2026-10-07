import { describe, expect, it } from "vitest";
import { keyStoreHelp } from "./keystore.js";

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
});
