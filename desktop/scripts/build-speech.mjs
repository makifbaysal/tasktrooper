/* global process, console */
import { execFileSync } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import path from "node:path";

/**
 * Builds `tt-speech`, the macOS speech-recognition helper (`speech/`), into
 * `bin/`, where `main/index.ts` looks for it next to the other bundled
 * binaries. Universal (arm64 + x86_64) like the rest of the mac app; the
 * signature comes from electron-builder (`mac.binaries`), this only ad-hoc
 * signs it so a dev build runs.
 *
 * `Info.plist` is linked into the binary itself (`__TEXT,__info_plist`): the
 * helper disclaims its parent before it asks for speech recognition, so that
 * embedded plist is where macOS reads the usage string.
 *
 * Elsewhere this does nothing: Windows and Linux have no voice input yet.
 */
if (process.platform !== "darwin") {
  console.log("build-speech: not macOS, skipped");
  process.exit(0);
}

const root = path.resolve(import.meta.dirname, "..");
const src = path.join(root, "speech");
const outDir = path.join(root, "bin");
const out = path.join(outDir, "tt-speech");
mkdirSync(outDir, { recursive: true });

const sources = ["main.swift", "disclaim.swift"].map((f) => path.join(src, f));
const plistFlags = ["-Xlinker", "-sectcreate", "-Xlinker", "__TEXT", "-Xlinker", "__info_plist", "-Xlinker", path.join(src, "Info.plist")];
const slices = [];
for (const arch of ["arm64", "x86_64"]) {
  const slice = `${out}-${arch}`;
  execFileSync(
    "xcrun",
    ["swiftc", "-O", "-target", `${arch}-apple-macos13.0`, ...sources, "-o", slice, ...plistFlags],
    { stdio: "inherit" },
  );
  slices.push(slice);
}
execFileSync("lipo", ["-create", "-output", out, ...slices], { stdio: "inherit" });
for (const slice of slices) rmSync(slice);
execFileSync("codesign", ["--force", "--sign", "-", out], { stdio: "inherit" });
console.log(`build-speech: wrote ${out}`);
