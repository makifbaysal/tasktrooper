package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The shims below are what npm, older npm and pnpm actually write, CRLF line
// endings included. The parser is a pure function, so it is tested here on
// every OS even though only Windows ever calls it on a real install.

const npmShimCurrent = "@ECHO off\r\n" +
	"GOTO start\r\n" +
	":find_dp0\r\n" +
	"SET dp0=%~dp0\r\n" +
	"EXIT /b\r\n" +
	":start\r\n" +
	"SETLOCAL\r\n" +
	"CALL :find_dp0\r\n" +
	"\r\n" +
	"IF EXIST \"%dp0%\\node.exe\" (\r\n" +
	"  SET \"_prog=%dp0%\\node.exe\"\r\n" +
	") ELSE (\r\n" +
	"  SET \"_prog=node\"\r\n" +
	"  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n" +
	")\r\n" +
	"\r\n" +
	"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\"  \"%dp0%\\node_modules\\@anthropic-ai\\claude-code\\cli.js\" %*\r\n"

const npmShimLegacy = "@IF EXIST \"%~dp0\\node.exe\" (\r\n" +
	"  \"%~dp0\\node.exe\"  \"%~dp0\\node_modules\\opencode-ai\\bin\\opencode\" %*\r\n" +
	") ELSE (\r\n" +
	"  @SETLOCAL\r\n" +
	"  @SET PATHEXT=%PATHEXT:;.JS;=;%\r\n" +
	"  node  \"%~dp0\\node_modules\\opencode-ai\\bin\\opencode\" %*\r\n" +
	")\r\n"

const pnpmShim = "@SETLOCAL\r\n" +
	"@IF NOT DEFINED NODE_PATH (\r\n" +
	"  @SET \"NODE_PATH=C:\\pnpm\\global\\5\\node_modules\"\r\n" +
	")\r\n" +
	"@IF EXIST \"%~dp0\\node.exe\" (\r\n" +
	"  \"%~dp0\\node.exe\"  \"%~dp0\\..\\@anthropic-ai\\claude-code\\cli.js\" %*\r\n" +
	") ELSE (\r\n" +
	"  @SET PATHEXT=%PATHEXT:;.JS;=;%\r\n" +
	"  node  \"%~dp0\\..\\@anthropic-ai\\claude-code\\cli.js\" %*\r\n" +
	")\r\n"

const exeShim = "@ECHO off\r\n" +
	"GOTO start\r\n" +
	":find_dp0\r\n" +
	"SET dp0=%~dp0\r\n" +
	"EXIT /b\r\n" +
	":start\r\n" +
	"SETLOCAL\r\n" +
	"CALL :find_dp0\r\n" +
	"\"%dp0%\\node_modules\\some-cli\\bin\\some-cli.exe\"   %*\r\n"

func TestParseNPMShimFindsTheScriptAndItsInterpreter(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantScript string
		wantNode   bool
	}{
		{"current npm", npmShimCurrent, "node_modules/@anthropic-ai/claude-code/cli.js", true},
		{"older npm", npmShimLegacy, "node_modules/opencode-ai/bin/opencode", true},
		{"pnpm", pnpmShim, "../@anthropic-ai/claude-code/cli.js", true},
		{"an exe target with no interpreter", exeShim, "node_modules/some-cli/bin/some-cli.exe", false},
		{"LF line endings", strings.ReplaceAll(npmShimCurrent, "\r\n", "\n"), "node_modules/@anthropic-ai/claude-code/cli.js", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNPMShim(tc.text)
			if err != nil {
				t.Fatalf("parseNPMShim: %v", err)
			}
			if got.script != tc.wantScript || got.node != tc.wantNode {
				t.Fatalf("parseNPMShim = %+v, want script %q node %v", got, tc.wantScript, tc.wantNode)
			}
		})
	}
}

// Anything that is not a node shim is refused rather than guessed at: the
// alternative to resolving a shim is running cmd.exe, which is the thing this
// exists to avoid.
func TestParseNPMShimRefusesWhatIsNotANodeShim(t *testing.T) {
	cases := map[string]string{
		"a bash shim": strings.ReplaceAll(strings.ReplaceAll(npmShimCurrent,
			`SET "_prog=%dp0%\node.exe"`, `SET "_prog=%dp0%\bash.exe"`), `SET "_prog=node"`, `SET "_prog=bash"`),
		"a hand-written batch file": "@echo off\r\nset \"DIR=%~dp0\"\r\n\"%DIR%node.exe\" \"%DIR%index.js\" %*\r\n",
		"no command line at all":    "@echo off\r\necho hello\r\n",
		"_prog never assigned":      "@ECHO off\r\n\"%_prog%\" \"%dp0%\\cli.js\" %*\r\n",
		"python runs the script":    "@ECHO off\r\npython \"%~dp0\\tool.py\" \"%~dp0\\other.py\" %*\r\n",
		"empty":                     "",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := parseNPMShim(text); err == nil {
				t.Fatalf("parseNPMShim accepted it: %+v", got)
			}
		})
	}
}

// writeShim lays out a global npm prefix the way npm does: the shim beside a
// node_modules holding the package, and optionally a node.exe beside both.
func writeShim(t *testing.T, withNode bool) (shim, script, node string) {
	t.Helper()
	dir := t.TempDir()
	shim = filepath.Join(dir, "claude.cmd")
	if err := os.WriteFile(shim, []byte(npmShimCurrent), 0o644); err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(dir, "node_modules", "@anthropic-ai", "claude-code", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("console.log('hi')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withNode {
		node = filepath.Join(dir, "node.exe")
		if err := os.WriteFile(node, []byte("not really node"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return shim, script, node
}

func TestResolveNPMShimPrefersTheNodeBesideIt(t *testing.T) {
	shim, script, node := writeShim(t, true)
	got, err := resolveNPMShim(shim, func(string) (string, error) {
		t.Fatal("PATH was searched although the shim's own node.exe is there")
		return "", nil
	})
	if err != nil {
		t.Fatalf("resolveNPMShim: %v", err)
	}
	if got.path != node || len(got.prefix) != 1 || got.prefix[0] != script {
		t.Fatalf("launcher = %+v, want %s %s", got, node, script)
	}
	argv := got.command("--print").Args
	if want := []string{node, script, "--print"}; strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
}

func TestResolveNPMShimFallsBackToNodeOnPath(t *testing.T) {
	shim, script, _ := writeShim(t, false)
	got, err := resolveNPMShim(shim, func(name string) (string, error) {
		if name != "node" {
			t.Fatalf("looked up %q, want node", name)
		}
		return `C:\Program Files\nodejs\node.exe`, nil
	})
	if err != nil {
		t.Fatalf("resolveNPMShim: %v", err)
	}
	if got.path != `C:\Program Files\nodejs\node.exe` || got.prefix[0] != script {
		t.Fatalf("launcher = %+v", got)
	}
}

func TestResolveNPMShimRefusesWhatItCannotRun(t *testing.T) {
	t.Run("no node anywhere", func(t *testing.T) {
		shim, _, _ := writeShim(t, false)
		_, err := resolveNPMShim(shim, func(string) (string, error) { return "", errors.New("not found") })
		if err == nil || !strings.Contains(err.Error(), "node is not on PATH") {
			t.Fatalf("err = %v, want it to say node is missing", err)
		}
	})
	t.Run("the script it names is gone", func(t *testing.T) {
		shim, script, _ := writeShim(t, true)
		if err := os.Remove(script); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveNPMShim(shim, nil); err == nil || !strings.Contains(err.Error(), "not a file") {
			t.Fatalf("err = %v, want it to name the missing script", err)
		}
	})
	t.Run("a shim larger than any shim", func(t *testing.T) {
		shim := filepath.Join(t.TempDir(), "big.cmd")
		if err := os.WriteFile(shim, []byte(strings.Repeat("x", maxShimBytes+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveNPMShim(shim, nil); err == nil {
			t.Fatal("a file over the bound was parsed")
		}
	})
}

// Off Windows nothing is resolved: a .cmd there is just a file name, and every
// binary is exec'd exactly as the desktop app detected it. On Windows only a
// batch file is touched — claude.exe from the native installer is not.
func TestLauncherForLeavesEverythingButAWindowsBatchFileAlone(t *testing.T) {
	for _, bin := range []string{"/opt/homebrew/bin/claude", `C:\Users\me\.local\bin\claude.exe`} {
		got, err := launcherFor(bin)
		if err != nil || got.path != bin || len(got.prefix) != 0 {
			t.Fatalf("launcherFor(%q) = %+v, %v; want it unchanged", bin, got, err)
		}
	}
	if runtime.GOOS != "windows" {
		got, err := launcherFor(`C:\Users\me\AppData\Roaming\npm\claude.cmd`)
		if err != nil || got.path != `C:\Users\me\AppData\Roaming\npm\claude.cmd` {
			t.Fatalf("launcherFor resolved a .cmd off Windows: %+v, %v", got, err)
		}
		return
	}
	if _, err := launcherFor(filepath.Join(t.TempDir(), "missing.cmd")); err == nil {
		t.Fatal("an unreadable .cmd was accepted; it would have been run through cmd.exe")
	}
}

func TestIsBatchFile(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\npm\claude.cmd`: true,
		`C:\npm\CLAUDE.CMD`: true,
		`C:\tools\run.bat`:  true,
		`C:\tools\run.exe`:  false,
		"/usr/local/bin/x":  false,
		"/tmp/cmd":          false,
	} {
		if got := isBatchFile(path); got != want {
			t.Errorf("isBatchFile(%q) = %v, want %v", path, got, want)
		}
	}
}
