import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

const packaged = vi.hoisted(() => ({ value: false }));

vi.mock("electron", () => ({
  app: {
    getAppPath: () => "/nonexistent/app.asar",
    get isPackaged() {
      return packaged.value;
    },
  },
  dialog: {},
  shell: {},
}));

const { checkWorkspace, cloudFolderName, isInside, mountWarning } = await import("./workspace.js");

describe("cloudFolderName", () => {
  it("names the sync client on Windows paths, backslashes and all", () => {
    expect(cloudFolderName("C:\\Users\\me\\OneDrive\\code")).toBe("OneDrive");
    expect(cloudFolderName("C:\\Users\\me\\OneDrive - Contoso Ltd\\code")).toBe("OneDrive");
    expect(cloudFolderName("C:\\Users\\me\\Dropbox\\code")).toBe("Dropbox");
    expect(cloudFolderName("C:\\Users\\me\\Dropbox (Personal)\\code")).toBe("Dropbox");
    expect(cloudFolderName("G:\\My Drive\\code")).toBe("Google Drive");
    expect(cloudFolderName("C:\\Users\\me\\Google Drive\\code")).toBe("Google Drive");
    expect(cloudFolderName("C:\\Users\\me\\iCloudDrive\\code")).toBe("iCloud Drive");
  });

  it("still names them on macOS and Linux", () => {
    expect(cloudFolderName("/Users/me/Library/Mobile Documents/com~apple~CloudDocs/x")).toBe("iCloud Drive");
    expect(cloudFolderName("/Users/me/Library/CloudStorage/OneDrive-Personal/x")).toBe("OneDrive");
    expect(cloudFolderName("/Users/me/Library/CloudStorage/GoogleDrive-me@example.com/My Drive/x")).toBe("Google Drive");
    expect(cloudFolderName("/Users/me/Library/CloudStorage/Box-Box/x")).toBe("a cloud storage folder");
    expect(cloudFolderName("/home/me/Dropbox/x")).toBe("Dropbox");
  });

  it("does not flag a name that merely contains one", () => {
    expect(cloudFolderName("/home/me/code/onedrive-sdk")).toBeNull();
    expect(cloudFolderName("C:\\Users\\me\\TaskTrooper")).toBeNull();
    expect(cloudFolderName("/home/me/TaskTrooper")).toBeNull();
  });
});

describe("mountWarning", () => {
  it("calls out a UNC share as a network share", () => {
    expect(mountWarning("\\\\fileserver\\team\\TaskTrooper")).toMatch(/network share/);
    expect(mountWarning("//fileserver/team/TaskTrooper")).toMatch(/network share/);
  });

  it("calls out removable and network mount points on macOS and Linux", () => {
    for (const p of ["/Volumes/USB/x", "/media/me/USB/x", "/run/media/me/USB/x", "/mnt/nas/x"]) {
      expect(mountWarning(p), p).toMatch(/mounted volume/);
    }
  });

  it("says nothing about a home directory or a local drive", () => {
    expect(mountWarning("/home/me/TaskTrooper")).toBeNull();
    expect(mountWarning("C:\\Users\\me\\TaskTrooper")).toBeNull();
    expect(mountWarning("D:\\work")).toBeNull();
  });
});

describe("isInside", () => {
  it("compares Windows paths without case and POSIX paths with it", () => {
    const install = "C:\\Users\\me\\AppData\\Local\\Programs\\TaskTrooper";
    expect(isInside("c:\\users\\me\\appdata\\local\\programs\\tasktrooper\\ws", install, "win32")).toBe(true);
    expect(isInside(install, install, "win32")).toBe(true);
    expect(isInside("C:\\Users\\me\\AppData\\Local\\Programs\\TaskTrooper2", install, "win32")).toBe(false);
    expect(isInside("/opt/TaskTrooper/ws", "/opt/TaskTrooper", "linux")).toBe(true);
    expect(isInside("/opt/tasktrooper/ws", "/opt/TaskTrooper", "linux")).toBe(false);
    expect(isInside("/opt/..x", "/opt/TaskTrooper", "linux")).toBe(false);
  });
});

describe("checkWorkspace", () => {
  const realPlatform = Object.getOwnPropertyDescriptor(process, "platform");
  let dir: string | undefined;
  afterEach(() => {
    packaged.value = false;
    if (realPlatform) Object.defineProperty(process, "platform", realPlatform);
    if (dir) rmSync(dir, { recursive: true, force: true });
    dir = undefined;
  });

  it("refuses a folder inside a packaged Windows or Linux install", () => {
    packaged.value = true;
    Object.defineProperty(process, "platform", { value: "linux", configurable: true });
    const got = checkWorkspace(path.join(path.dirname(process.execPath), "workspace"));
    expect(got.ok).toBe(false);
    expect(got.error).toMatch(/install folder/);
  });

  it("passes an ordinary writable folder", () => {
    dir = mkdtempSync(path.join(tmpdir(), "tt-ws-"));
    const got = checkWorkspace(dir);
    expect(got.ok).toBe(true);
    expect(got.warnings.filter((w) => /sync client|mounted|network/.test(w))).toEqual([]);
  });
});
