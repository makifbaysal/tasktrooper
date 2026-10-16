import path from "node:path";
import { TOOLCHAIN_IDS, type ToolchainId } from "../../ipc/types.js";
import { MIN_CLAUDE_VERSION, probeAndroidSdk, probeChrome } from "../services/detect.js";
import {
  anyOf,
  appiumDriver,
  binary,
  byPlatform,
  file,
  fromPreflight,
  jdk,
  matching,
  pythonInstallDirs,
  wingetLinks,
  xcode,
  xcodeCommandLineTools,
} from "./detectors.js";
import { cargoBinDir, dockerBinDirs, dotnetDirs, duckdbDirs, goBinDirs, programFiles, userBinDir } from "./locations.js";
import { BREW_CANDIDATES } from "./recipes/brew.js";
import type { PinnedAsset, ProbeKit, ToolDefinition, ToolchainPlatform } from "./types.js";

/**
 * Every tool this app installs, and how, per operating system.
 *
 * The page sends ids; everything that runs — a formula, a winget id, a package
 * list, a URL — is read from here and nowhere else. An OS missing from
 * `recipes` is an OS the tool does not apply to (no Xcode on Windows); a
 * `manual` recipe is one this app will not run and links to instead.
 *
 * Package ids and URLs were checked against their sources when they were
 * written (winget-pkgs manifests, formulae.brew.sh, the Android repository
 * XML, the release's own digests); a pinned download carries the digest its
 * publisher lists, and a file that does not match is refused.
 */

const HOMEBREW_INSTALLER = "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh";
/** Where https://dot.net/v1/dotnet-install.sh redirects. */
const DOTNET_INSTALL_SH = "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh";
const RUSTUP_INIT_SH = "https://sh.rustup.rs";
const XCODE_APP_STORE = "https://apps.apple.com/app/xcode/id497799835";

/** Command-line tools 23.0 (build 16111833), the stable `cmdline-tools;latest` in Google's repository2-3.xml. */
const ANDROID_TOOLS = (name: string, sha1: string): PinnedAsset => ({
  url: `https://dl.google.com/android/repository/commandlinetools-${name}-16111833_latest.zip`,
  sha1,
});
const ANDROID_PACKAGES = ["platform-tools", "emulator", "platforms;android-35", "build-tools;35.0.0"];

/** uv 0.13.1 and DuckDB 1.5.6, with the sha256 digests GitHub lists for each asset. */
const UV = (asset: string, sha256: string): PinnedAsset => ({
  url: `https://github.com/astral-sh/uv/releases/download/0.13.1/${asset}`,
  sha256,
});
const DUCKDB = (asset: string, sha256: string): PinnedAsset => ({
  url: `https://github.com/duckdb/duckdb/releases/download/v1.5.6/${asset}`,
  sha256,
});

/** OSV-Scanner v2.7.0, with the sha256 digests GitHub lists for each asset. */
const OSV = (asset: string, sha256: string): PinnedAsset => ({
  url: `https://github.com/google/osv-scanner/releases/download/v2.7.0/${asset}`,
  sha256,
});

const home = (kit: ProbeKit, ...parts: string[]): string => path.join(kit.home, ...parts);

const DEFINITIONS: { readonly [K in ToolchainId]: ToolDefinition & { readonly id: K } } = {
  homebrew: {
    id: "homebrew",
    label: "Homebrew",
    docsUrl: "https://brew.sh",
    affectsRuntime: false,
    recipes: { darwin: { install: { kind: "interactive", via: "terminal", scriptUrl: HOMEBREW_INSTALLER } } },
    detect: binary({ names: BREW_CANDIDATES, what: "brew", versionArgs: ["--version"] }),
  },
  git: {
    id: "git",
    runtime: "bin-dir",
    label: "Git",
    docsUrl: "https://git-scm.com/downloads",
    affectsRuntime: true,
    recipes: {
      // Apple ships git in the command line tools; Homebrew (and a Terminal
      // password) only for git would be a second git nobody asked for.
      darwin: { install: { kind: "bundled", with: "xcode-clt" } },
      win32: { install: { kind: "winget", id: "Git.Git" } },
      linux: { install: { kind: "linux-pkg", apt: ["git"], dnf: ["git"] } },
    },
    detect: binary({ names: ["git"], versionArgs: ["--version"], appleShim: true }),
  },
  "claude-code": {
    id: "claude-code",
    runtime: "bin-dir",
    label: "Claude Code",
    docsUrl: "https://code.claude.com/docs/en/setup",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "script", url: "https://claude.ai/install.sh", interpreter: "bash" } },
      linux: { install: { kind: "script", url: "https://claude.ai/install.sh", interpreter: "bash" } },
      win32: { install: { kind: "script", url: "https://claude.ai/install.ps1", interpreter: "powershell" } },
    },
    detect: binary({ names: ["claude"], versionArgs: ["--version"], minVersion: MIN_CLAUDE_VERSION }),
  },
  node: {
    id: "node",
    runtime: "bin-dir",
    label: "Node.js",
    docsUrl: "https://nodejs.org/en/download",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew", formula: "node" } },
      win32: { install: { kind: "winget", id: "OpenJS.NodeJS.LTS" } },
      linux: { install: { kind: "linux-pkg", apt: ["nodejs", "npm"], dnf: ["nodejs", "npm"] } },
    },
    detect: binary({ names: ["node"], versionArgs: ["--version"] }),
  },
  python: {
    id: "python",
    label: "Python",
    docsUrl: "https://www.python.org/downloads/",
    affectsRuntime: false,
    recipes: {
      darwin: { install: { kind: "brew", formula: "python" } },
      win32: { install: { kind: "winget", id: "Python.Python.3.12" } },
      linux: {
        install: { kind: "linux-pkg", apt: ["python3", "python3-pip", "python3-venv"], dnf: ["python3", "python3-pip"] },
      },
    },
    detect: byPlatform({
      darwin: binary({ names: ["python3"], versionArgs: ["--version"], appleShim: true }),
      linux: binary({ names: ["python3"], versionArgs: ["--version"] }),
      // `python.exe` under WindowsApps is the Store's "install me" alias, not Python.
      win32: binary({
        names: ["python", "py"],
        what: "python",
        versionArgs: ["--version"],
        rejectPath: /\\WindowsApps\\/i,
        extraDirs: pythonInstallDirs,
      }),
    }),
  },
  uv: {
    id: "uv",
    runtime: "bin-dir",
    label: "uv",
    docsUrl: "https://docs.astral.sh/uv/getting-started/installation/",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew", formula: "uv" } },
      win32: { install: { kind: "winget", id: "astral-sh.uv" } },
      linux: {
        install: {
          kind: "binary-download",
          name: "uv",
          archive: "tar.gz",
          files: ["uv", "uvx"],
          assets: {
            x64: UV("uv-x86_64-unknown-linux-gnu.tar.gz", "eadd9d27c134ab5ac1ffd4473b97a741407f3ea238288f03121fc4deddf90220"),
            arm64: UV("uv-aarch64-unknown-linux-gnu.tar.gz", "9041c73c99fdc5892cf128140beb354eca9231d502068cc36e6cb53a8e59ad14"),
          },
        },
      },
    },
    detect: binary({
      names: ["uv"],
      versionArgs: ["--version"],
      extraDirs: (kit) => [...wingetLinks(kit), userBinDir(kit, "uv"), home(kit, ".cargo", "bin")],
    }),
  },
  chrome: {
    id: "chrome",
    label: "Chrome / Chromium",
    docsUrl: "https://www.google.com/chrome/",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew-cask", cask: "google-chrome" } },
      win32: { install: { kind: "winget", id: "Google.Chrome" } },
      // Chromium from the snap store, which the preflight finds at
      // /snap/bin/chromium and the backend's browser tools drive like Chrome.
      linux: { install: { kind: "snap", name: "chromium", classic: false } },
    },
    detect: () => Promise.resolve(fromPreflight(probeChrome())),
  },
  xcode: {
    id: "xcode",
    label: "Xcode",
    docsUrl: XCODE_APP_STORE,
    affectsRuntime: false,
    recipes: { darwin: { install: { kind: "manual", url: XCODE_APP_STORE, via: "App Store" } } },
    detect: xcode,
  },
  "xcode-clt": {
    id: "xcode-clt",
    label: "Xcode Command Line Tools",
    docsUrl: "https://developer.apple.com/xcode/resources/",
    affectsRuntime: false,
    recipes: { darwin: { install: { kind: "interactive", via: "xcode-select" } } },
    detect: xcodeCommandLineTools,
  },
  cocoapods: {
    id: "cocoapods",
    label: "CocoaPods",
    docsUrl: "https://guides.cocoapods.org/using/getting-started.html",
    affectsRuntime: false,
    recipes: { darwin: { install: { kind: "brew", formula: "cocoapods" } } },
    detect: binary({ names: ["pod"], what: "pod", versionArgs: ["--version"] }),
  },
  jdk: {
    id: "jdk",
    runtime: "java-home",
    label: "JDK 17",
    docsUrl: "https://adoptium.net/temurin/releases/?version=17",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew", formula: "openjdk@17" } },
      win32: { install: { kind: "winget", id: "EclipseAdoptium.Temurin.17.JDK" } },
      linux: { install: { kind: "linux-pkg", apt: ["openjdk-17-jdk"], dnf: ["java-17-openjdk-devel"] } },
    },
    detect: jdk,
  },
  "android-sdk": {
    id: "android-sdk",
    label: "Android SDK",
    docsUrl: "https://developer.android.com/tools/sdkmanager",
    affectsRuntime: true,
    recipes: {
      darwin: {
        install: {
          kind: "android-sdk",
          archives: {
            arm64: ANDROID_TOOLS("mac_arm64", "ad03dc49bfacfd52c110b14104ea548b8a07e830"),
            x64: ANDROID_TOOLS("mac_x86_64", "112cf9618794a997ff273537d55bee02c22abffe"),
          },
          packages: ANDROID_PACKAGES,
        },
      },
      // Google publishes no emulator for Linux or Windows on arm64, so only x64 is automatic there.
      win32: {
        install: {
          kind: "android-sdk",
          archives: { x64: ANDROID_TOOLS("win", "57d04f2d75eb8e8fffc5000a987e5de4b5a63e9d") },
          packages: ANDROID_PACKAGES,
        },
      },
      linux: {
        install: {
          kind: "android-sdk",
          archives: { x64: ANDROID_TOOLS("linux", "e025545c62a8e64c7559119566a569fb1dec5f60") },
          packages: ANDROID_PACKAGES,
        },
      },
    },
    detect: () => Promise.resolve(fromPreflight(probeAndroidSdk())),
  },
  appium: {
    id: "appium",
    runtime: "bin-dir",
    label: "Appium",
    docsUrl: "https://appium.io/docs/en/latest/quickstart/install/",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "npm-global", pkg: "appium" } },
      win32: { install: { kind: "npm-global", pkg: "appium" } },
      linux: { install: { kind: "npm-global", pkg: "appium" } },
    },
    detect: binary({ names: ["appium"], versionArgs: ["--version"] }),
  },
  "appium-xcuitest": {
    id: "appium-xcuitest",
    label: "Appium XCUITest driver",
    docsUrl: "https://appium.github.io/appium-xcuitest-driver/latest/",
    affectsRuntime: true,
    recipes: { darwin: { install: { kind: "appium-driver", driver: "xcuitest" } } },
    detect: appiumDriver("xcuitest"),
  },
  "appium-uiautomator2": {
    id: "appium-uiautomator2",
    label: "Appium UiAutomator2 driver",
    docsUrl: "https://github.com/appium/appium-uiautomator2-driver",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "appium-driver", driver: "uiautomator2" } },
      win32: { install: { kind: "appium-driver", driver: "uiautomator2" } },
      linux: { install: { kind: "appium-driver", driver: "uiautomator2" } },
    },
    detect: appiumDriver("uiautomator2"),
  },
  flutter: {
    id: "flutter",
    label: "Flutter",
    docsUrl: "https://docs.flutter.dev/install",
    affectsRuntime: false,
    recipes: {
      darwin: { install: { kind: "brew-cask", cask: "flutter" } },
      // No winget package for the Flutter SDK could be verified; Google documents the zip.
      win32: { install: { kind: "manual", url: "https://docs.flutter.dev/install", via: "Download" } },
      linux: { install: { kind: "snap", name: "flutter", classic: true } },
    },
    // Presence only: a first `flutter --version` downloads the Dart SDK.
    detect: binary({
      names: ["flutter"],
      extraDirs: (kit) =>
        kit.platform === "win32"
          ? ["C:\\src\\flutter\\bin", home(kit, "flutter", "bin"), home(kit, "develop", "flutter", "bin")]
          : [home(kit, "flutter", "bin"), home(kit, "develop", "flutter", "bin"), home(kit, "development", "flutter", "bin")],
    }),
  },
  docker: {
    id: "docker",
    runtime: "bin-dir",
    label: "Docker",
    docsUrl: "https://docs.docker.com/get-started/get-docker/",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew-cask", cask: "docker-desktop" } },
      win32: { install: { kind: "winget", id: "Docker.DockerDesktop" } },
      linux: {
        install: { kind: "linux-pkg", apt: ["docker.io"], dnf: ["moby-engine"] },
        docsUrl: "https://docs.docker.com/engine/install/",
      },
    },
    detect: anyOf(
      binary({
        names: ["docker"],
        versionArgs: ["--version"],
        extraDirs: dockerBinDirs,
      }),
      byPlatform({ darwin: file(() => ["/Applications/Docker.app"], "Docker.app") }),
    ),
  },
  go: {
    id: "go",
    runtime: "bin-dir",
    label: "Go",
    docsUrl: "https://go.dev/doc/install",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew", formula: "go" } },
      win32: { install: { kind: "winget", id: "GoLang.Go" } },
      linux: { install: { kind: "linux-pkg", apt: ["golang-go"], dnf: ["golang"] } },
    },
    detect: binary({
      names: ["go"],
      versionArgs: ["version"],
      extraDirs: goBinDirs,
    }),
  },
  dotnet: {
    id: "dotnet",
    runtime: "bin-dir",
    label: ".NET SDK",
    docsUrl: "https://dotnet.microsoft.com/en-us/download",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew-cask", cask: "dotnet-sdk" } },
      win32: { install: { kind: "winget", id: "Microsoft.DotNet.SDK.10" } },
      // Each distribution packages a different set of SDK versions, or none
      // (Debian needs Microsoft's own feed); Microsoft's script installs the
      // current LTS into ~/.dotnet on any of them, without root.
      linux: {
        install: { kind: "script", url: DOTNET_INSTALL_SH, interpreter: "bash", args: ["--channel", "LTS"] },
        docsUrl: "https://learn.microsoft.com/en-us/dotnet/core/install/linux-scripted-manual",
      },
    },
    // A host with no SDK answers `--version` with "No .NET SDKs were found" and a non-zero exit: unusable, not installed.
    detect: binary({ names: ["dotnet"], versionArgs: ["--version"], extraDirs: dotnetDirs }),
  },
  rust: {
    id: "rust",
    runtime: "bin-dir",
    label: "Rust",
    docsUrl: "https://rust-lang.org/tools/install",
    affectsRuntime: true,
    recipes: {
      // rustup, as rust-lang.org documents it; `-y` because it otherwise asks on a terminal there is none of.
      // Linking needs a C toolchain's `cc`, which on a Mac is the command line tools'.
      darwin: { install: { kind: "script", url: RUSTUP_INIT_SH, interpreter: "sh", args: ["-y"] }, dependsOn: ["xcode-clt"] },
      linux: { install: { kind: "script", url: RUSTUP_INIT_SH, interpreter: "sh", args: ["-y"] } },
      win32: {
        install: { kind: "winget", id: "Rustlang.Rustup" },
        docsUrl: "https://rust-lang.github.io/rustup/installation/windows-msvc.html",
      },
    },
    // `cargo` is rustup's proxy: with no default toolchain it fails, which is what makes it unusable rather than installed.
    detect: binary({ names: ["cargo"], versionArgs: ["--version"], extraDirs: (kit) => [cargoBinDir(kit)] }),
  },
  "osv-scanner": {
    id: "osv-scanner",
    runtime: "bin-dir",
    label: "OSV-Scanner",
    docsUrl: "https://google.github.io/osv-scanner/installation/",
    affectsRuntime: true,
    recipes: {
      darwin: { install: { kind: "brew", formula: "osv-scanner" } },
      win32: {
        install: {
          kind: "binary-download",
          name: "osv-scanner",
          assets: {
            x64: OSV("osv-scanner_windows_amd64.exe", "9964ed64ec9a387391eb4cc006ad5c531154821aef29b793e02c448de4686438"),
            arm64: OSV("osv-scanner_windows_arm64.exe", "3e630381c1f1fd3c9c4d447e6ff2ef6c737083371722773db8337eead0f97962"),
          },
        },
      },
      linux: {
        install: {
          kind: "binary-download",
          name: "osv-scanner",
          assets: {
            x64: OSV("osv-scanner_linux_amd64", "160c6253e2922e9e0a6a9771c5c4c40a4b3c6090b34a3cdd5e80c96cc2e4218b"),
            arm64: OSV("osv-scanner_linux_arm64", "4061ef4c021ed3c47f203b818f337e351336cc5cd67c0274fc36bd1dfc728dd7"),
          },
        },
      },
    },
    detect: binary({
      names: ["osv-scanner"],
      versionArgs: ["--version"],
      extraDirs: (kit) => [userBinDir(kit, "osv-scanner")],
    }),
  },
  duckdb: {
    id: "duckdb",
    label: "DuckDB",
    docsUrl: "https://duckdb.org/docs/installation",
    affectsRuntime: false,
    recipes: {
      darwin: { install: { kind: "brew", formula: "duckdb" } },
      win32: { install: { kind: "winget", id: "DuckDB.cli" } },
      linux: {
        install: {
          kind: "binary-download",
          name: "duckdb",
          archive: "gz",
          assets: {
            x64: DUCKDB("duckdb_cli-linux-amd64.gz", "cce2687a38681db644c248198b99ecd86170ff47e324c5f33e50c0042f142839"),
            arm64: DUCKDB("duckdb_cli-linux-arm64.gz", "1af0541e649a3ae34eb20eec2ca564308bf0e8993e5d6dba6be77bfb289a2718"),
          },
        },
      },
    },
    detect: binary({
      names: ["duckdb"],
      versionArgs: ["--version"],
      extraDirs: (kit) => [...duckdbDirs(kit), userBinDir(kit, "duckdb")],
    }),
  },
  godot: {
    id: "godot",
    label: "Godot",
    docsUrl: "https://godotengine.org/download/",
    affectsRuntime: false,
    recipes: {
      darwin: { install: { kind: "brew-cask", cask: "godot" } },
      win32: { install: { kind: "winget", id: "GodotEngine.GodotEngine" } },
      linux: { install: { kind: "manual", url: "https://godotengine.org/download/linux/", via: "Download" } },
    },
    // Presence only: asking the editor its version starts it.
    detect: anyOf(
      byPlatform({ darwin: file((kit) => matching(kit, "/Applications", /^Godot.*\.app$/), "Godot.app") }),
      binary({ names: ["godot", "godot4"], extraDirs: wingetLinks }),
    ),
  },
  "unity-hub": {
    id: "unity-hub",
    label: "Unity Hub",
    docsUrl: "https://docs.unity.com/en-us/hub",
    affectsRuntime: false,
    recipes: {
      darwin: { install: { kind: "brew-cask", cask: "unity-hub" } },
      win32: { install: { kind: "winget", id: "Unity.UnityHub" } },
      linux: { install: { kind: "manual", url: "https://docs.unity.com/en-us/hub", via: "Unity's apt repository" } },
    },
    detect: byPlatform({
      darwin: file(() => ["/Applications/Unity Hub.app"], "Unity Hub.app"),
      win32: file((kit) => [path.join(programFiles(kit), "Unity Hub", "Unity Hub.exe")], "Unity Hub"),
      linux: anyOf(binary({ names: ["unityhub"] }), file(() => ["/opt/unityhub/unityhub"], "Unity Hub")),
    }),
  },
};

export const TOOLCHAIN_DEFINITIONS: Readonly<Record<ToolchainId, ToolDefinition>> = DEFINITIONS;

export function definitionOf(id: ToolchainId): ToolDefinition {
  return DEFINITIONS[id];
}

export function appliesOn(id: ToolchainId, platform: ToolchainPlatform): boolean {
  return DEFINITIONS[id].recipes[platform] !== undefined;
}

/** In `TOOLCHAIN_IDS` order, which is the order every list of them is shown in. */
export function applicableIds(platform: ToolchainPlatform): ToolchainId[] {
  return TOOLCHAIN_IDS.filter((id) => appliesOn(id, platform));
}
