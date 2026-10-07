// Package hostshell picks the shell a model-written command line is handed to,
// and names it so the prompt can tell the model which syntax to write.
//
// Hardcoding `sh -c` broke every run on Windows, where there is no sh: each
// run_terminal call failed, the agent fell back to paging files with read_file,
// and the loop guard stopped the run with nothing done. Stdlib-only, like
// childenv, so any layer can use it.
package hostshell

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"
)

type Kind string

const (
	POSIX      Kind = "posix"
	PowerShell Kind = "powershell"
	Cmd        Kind = "cmd"
)

// GitBashName is Shell.Name for Git for Windows' bash.
const GitBashName = "Git Bash"

type Shell struct {
	Path string
	// Name is what the prompt calls it, e.g. "Git Bash" or "Windows PowerShell".
	Name string
	Kind Kind
}

// Host is everything the prompt says about the machine commands run on.
type Host struct {
	OS    string
	Arch  string
	Shell Shell
}

var (
	once   sync.Once
	cached Shell
)

// Default is the shell for this machine, detected once per process.
func Default() Shell {
	once.Do(func() { cached = detect(runtime.GOOS, systemProbe()) })
	return cached
}

func Current() Host {
	return Host{OS: OSName(runtime.GOOS), Arch: runtime.GOARCH, Shell: Default()}
}

// OSName is the name a user would use for goos.
func OSName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	case "freebsd":
		return "FreeBSD"
	default:
		return goos
	}
}

// Command runs script in this shell. The caller sets Dir, Env and output.
func (s Shell) Command(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, s.Path, s.args(script)...)
	applyCommandLine(cmd, s, script)
	return cmd
}

func (s Shell) args(script string) []string {
	switch s.Kind {
	case PowerShell:
		// -EncodedCommand sidesteps PowerShell's own command-line parsing, which
		// re-splits a -Command argument and mangles embedded quotes.
		return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script)}
	case Cmd:
		return []string{"/d", "/s", "/c", script}
	default:
		return []string{"-c", script}
	}
}

func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

type probe struct {
	lookPath func(string) (string, error)
	isFile   func(string) bool
	getenv   func(string) string
}

func systemProbe() probe {
	return probe{
		lookPath: exec.LookPath,
		isFile: func(p string) bool {
			info, err := os.Stat(p)
			return err == nil && !info.IsDir()
		},
		getenv: os.Getenv,
	}
}

func detect(goos string, p probe) Shell {
	if goos != "windows" {
		if path, err := p.lookPath("sh"); err == nil {
			return Shell{Path: path, Name: "sh", Kind: POSIX}
		}
		return Shell{Path: "/bin/sh", Name: "sh", Kind: POSIX}
	}
	return detectWindows(p)
}

// detectWindows prefers Git Bash: agents write POSIX command lines, every repo
// script here is bash, and git is already required. `bash` is deliberately not
// looked up on PATH — System32\bash.exe is the WSL launcher, which runs the
// command in a Linux VM that cannot see C:\ paths the way the caller means them.
func detectWindows(p probe) Shell {
	for _, candidate := range gitBashCandidates(p) {
		if p.isFile(candidate) {
			return Shell{Path: candidate, Name: GitBashName, Kind: POSIX}
		}
	}
	if path, err := p.lookPath("sh"); err == nil {
		return Shell{Path: path, Name: "sh", Kind: POSIX}
	}
	if path, err := p.lookPath("pwsh"); err == nil {
		return Shell{Path: path, Name: "PowerShell", Kind: PowerShell}
	}
	if path, err := p.lookPath("powershell"); err == nil {
		return Shell{Path: path, Name: "Windows PowerShell", Kind: PowerShell}
	}
	comspec := p.getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	return Shell{Path: comspec, Name: "cmd.exe", Kind: Cmd}
}

// gitBashCandidates is bin\bash.exe under each likely Git for Windows root —
// the launcher, not usr\bin\bash.exe, because only the launcher puts Git's
// coreutils (sed, grep, find) on PATH.
func gitBashCandidates(p probe) []string {
	var roots []string
	if git, err := p.lookPath("git"); err == nil {
		dir := filepath.Dir(git)
		// cmd\git.exe, bin\git.exe and mingw64\bin\git.exe are all on PATH in
		// some install.
		roots = append(roots, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir)))
	}
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if base := p.getenv(env); base != "" {
			roots = append(roots, filepath.Join(base, "Git"))
		}
	}
	if local := p.getenv("LOCALAPPDATA"); local != "" {
		roots = append(roots, filepath.Join(local, "Programs", "Git"))
	}
	seen := make(map[string]bool, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		candidate := filepath.Join(root, "bin", "bash.exe")
		if !seen[candidate] {
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}

// ResolveArgv points an argv that names a POSIX script or shell at the shell
// this host actually has. On Windows `./scripts/ci.sh` is not a Win32 program
// ("%1 is not a valid Win32 application"), and a bare `bash` resolves to
// System32\bash.exe, the WSL launcher, which runs the script in a Linux VM
// that cannot see the checkout the way the caller means it. Elsewhere argv
// is returned unchanged.
func ResolveArgv(argv []string) []string {
	if runtime.GOOS != "windows" {
		return argv
	}
	return resolveArgv(Default(), argv)
}

func resolveArgv(s Shell, argv []string) []string {
	if len(argv) == 0 || s.Kind != POSIX {
		return argv
	}
	name := strings.ToLower(filepath.Base(strings.ReplaceAll(argv[0], `\`, "/")))
	switch {
	case name == "bash" || name == "bash.exe" || name == "sh" || name == "sh.exe":
		return append([]string{s.Path}, argv[1:]...)
	case strings.HasSuffix(name, ".sh"):
		return append([]string{s.Path}, argv...)
	}
	return argv
}
