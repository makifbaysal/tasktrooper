import { describe, expect, it } from "vitest";
import { binaryDirs, mergePath, pathKeyOf, prependDirs, withoutAppImage } from "./process-env.js";

describe("mergePath", () => {
  it("keeps the first list's order and appends only what is new", () => {
    const gui = ["/usr/bin", "/bin", "/usr/sbin", "/sbin"];
    const login = ["/home/me/.nvm/versions/node/v22/bin", "/usr/local/bin", "/usr/bin", "/bin"];
    expect(mergePath([gui, ["/opt/homebrew/bin"], login], "darwin")).toEqual([
      "/usr/bin",
      "/bin",
      "/usr/sbin",
      "/sbin",
      "/opt/homebrew/bin",
      "/home/me/.nvm/versions/node/v22/bin",
      "/usr/local/bin",
    ]);
  });

  it("compares case-insensitively on Windows only", () => {
    expect(mergePath([["C:\\Tools"], ["c:\\tools", "C:\\Other"]], "win32")).toEqual(["C:\\Tools", "C:\\Other"]);
    expect(mergePath([["/Tools"], ["/tools"]], "linux")).toEqual(["/Tools", "/tools"]);
  });
});

describe("prependDirs", () => {
  it("moves a CLI's directory to the front so its shebang finds the node beside it", () => {
    const parts = ["/usr/local/bin", "/usr/bin", "/home/me/.nvm/versions/node/v22/bin"];
    expect(prependDirs(parts, ["/home/me/.nvm/versions/node/v22/bin"], "linux")).toEqual([
      "/home/me/.nvm/versions/node/v22/bin",
      "/usr/local/bin",
      "/usr/bin",
    ]);
  });

  it("never reorders a system directory, which every PATH already has", () => {
    const parts = ["/usr/local/bin", "/usr/bin", "/bin"];
    expect(prependDirs(parts, ["/usr/bin", "/bin"], "linux")).toEqual(parts);
  });

  it("ignores relative and duplicate entries", () => {
    expect(prependDirs(["/a"], ["rel", "/b", "/b"], "linux")).toEqual(["/b", "/a"]);
  });
});

describe("binaryDirs", () => {
  it("is the directory of every absolute path it is given", () => {
    expect(binaryDirs(["/x/bin/claude", undefined, "relative", "/usr/bin/git"], "linux")).toEqual(["/x/bin", "/usr/bin"]);
    expect(binaryDirs(["C:\\Users\\me\\AppData\\Roaming\\npm\\claude.cmd"], "win32")).toEqual([
      "C:\\Users\\me\\AppData\\Roaming\\npm",
    ]);
  });
});

describe("pathKeyOf", () => {
  it("finds Windows' Path spelling", () => {
    expect(pathKeyOf({ Path: "x" })).toBe("Path");
    expect(pathKeyOf({})).toBe("PATH");
  });
});

describe("withoutAppImage", () => {
  const appdir = "/tmp/.mount_TaskTrXYZ";
  const appRun: NodeJS.ProcessEnv = {
    APPIMAGE: "/home/me/Apps/TaskTrooper-0.1.11-x86_64.AppImage",
    APPDIR: appdir,
    ARGV0: "TaskTrooper.AppImage",
    OWD: "/home/me",
    PATH: `${appdir}:${appdir}/usr/sbin:/usr/local/bin:/usr/bin`,
    LD_LIBRARY_PATH: `${appdir}/usr/lib`,
    XDG_DATA_DIRS: `${appdir}/usr/share/:/usr/share/ubuntu:/usr/local/share/:/usr/share/`,
    GSETTINGS_SCHEMA_DIR: `${appdir}/usr/share/glib-2.0/schemas`,
    HOME: "/home/me",
  };

  it("strips what AppRun added and keeps what the session had", () => {
    const env = withoutAppImage(appRun);
    expect(env.PATH).toBe("/usr/local/bin:/usr/bin");
    expect(env.LD_LIBRARY_PATH).toBeUndefined();
    expect(env.GSETTINGS_SCHEMA_DIR).toBeUndefined();
    expect(env.XDG_DATA_DIRS).toBe("/usr/share/ubuntu:/usr/local/share/:/usr/share/");
    for (const key of ["APPIMAGE", "APPDIR", "ARGV0", "OWD"]) expect(env[key]).toBeUndefined();
    expect(env.HOME).toBe("/home/me");
  });

  it("keeps the user's own LD_LIBRARY_PATH entries", () => {
    const env = withoutAppImage({ ...appRun, LD_LIBRARY_PATH: `${appdir}/usr/lib:/opt/cuda/lib64` });
    expect(env.LD_LIBRARY_PATH).toBe("/opt/cuda/lib64");
  });

  it("changes nothing outside an AppImage", () => {
    const plain = { PATH: "/usr/bin", LD_LIBRARY_PATH: "/opt/x", GSETTINGS_SCHEMA_DIR: "/y" };
    expect(withoutAppImage(plain)).toBe(plain);
  });

  it("does not mistake a sibling directory for the AppDir", () => {
    const env = withoutAppImage({ ...appRun, PATH: `${appdir}:${appdir}-other/bin` });
    expect(env.PATH).toBe(`${appdir}-other/bin`);
  });
});
