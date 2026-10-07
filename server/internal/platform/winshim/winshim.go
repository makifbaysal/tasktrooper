// Package winshim runs what a Windows npm/pnpm `.cmd` shim runs, without the
// shim. Most agent CLIs (claude, opencode, cursor-agent, npx) install on
// Windows as `name.cmd`, and CreateProcess runs a .cmd through cmd.exe, which
// re-parses argv with its own rules: a prompt longer than 8191 characters is
// refused, the first newline truncates it, and `& | < > ^ %` in task text
// become live shell syntax. Resolving the shim to `node.exe cli.js` keeps the
// argv intact and makes the CLI a direct child, so killing it kills it.
// Stdlib-only, like childenv, so any layer can use it.
package winshim

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Command is exec.CommandContext, except that on Windows a .cmd/.bat npm shim
// is replaced by the program it launches. Anything it cannot resolve runs as
// before.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if prefix, ok := resolveName(name); ok {
		return exec.CommandContext(ctx, prefix[0], append(prefix[1:], args...)...)
	}
	return exec.CommandContext(ctx, name, args...)
}

// Cmd is Command for callers that manage the process lifetime themselves.
func Cmd(name string, args ...string) *exec.Cmd {
	if prefix, ok := resolveName(name); ok {
		return exec.Command(prefix[0], append(prefix[1:], args...)...)
	}
	return exec.Command(name, args...)
}

func resolveName(name string) ([]string, bool) {
	if runtime.GOOS != "windows" {
		return nil, false
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, false
	}
	return Resolve(path)
}

// Resolve returns the argv prefix — [node.exe, cli.js] or [target.exe] — that
// runs what the shim at path runs. ok is false when path is not a .cmd/.bat
// file or its target cannot be found.
func Resolve(path string) ([]string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".cmd" && ext != ".bat" {
		return nil, false
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return parse(path, string(text), fileExists, func() (string, error) { return exec.LookPath("node") })
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

var (
	setLine     = regexp.MustCompile(`(?i)^@?\s*set\s+"?([A-Za-z_][A-Za-z0-9_]*)=([^"]*)"?\s*$`)
	quotedToken = regexp.MustCompile(`"([^"]*)"`)
	varRef      = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%`)
	dp0Ref      = regexp.MustCompile(`(?i)%~dp0|%dp0%`)
)

// parse understands the two shim shapes in the wild: npm's cmd-shim
// (`"%_prog%"  "%dp0%\node_modules\pkg\cli.js" %*`, with _prog set to
// node.exe or node) and npm's own npm.cmd/npx.cmd
// (`"%NODE_EXE%" "%NPX_CLI_JS%" %*`, with both SET earlier), plus pnpm's
// (`node  "%~dp0\..\pkg\bin\cli.cjs" %*`).
func parse(path, text string, exists func(string) bool, lookNode func() (string, error)) ([]string, bool) {
	dir := winDir(path)
	vars := map[string]string{}
	expand := func(s string) string {
		s = dp0Ref.ReplaceAllLiteralString(s, dir+`\`)
		return varRef.ReplaceAllStringFunc(s, func(ref string) string {
			if v, ok := vars[strings.ToUpper(strings.Trim(ref, "%"))]; ok {
				return v
			}
			return ref
		})
	}

	launch := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if m := setLine.FindStringSubmatch(line); m != nil {
			// A value still holding % came from a FOR loop or a variable this
			// parser does not track (the npm-prefix lookup in npx.cmd); keeping
			// the earlier value is right, since that branch only overrides it
			// when the file it names exists.
			if v := expand(m[2]); !strings.Contains(v, "%") {
				vars[strings.ToUpper(m[1])] = v
			}
			continue
		}
		if strings.Contains(line, "%*") {
			launch = line
		}
	}
	if launch == "" {
		return nil, false
	}
	launch = launch[:strings.LastIndex(launch, "%*")]

	var tokens []string
	for _, m := range quotedToken.FindAllStringSubmatch(launch, -1) {
		tokens = append(tokens, cleanWindows(expand(m[1])))
	}
	if len(tokens) == 0 {
		return nil, false
	}

	node := func() (string, bool) {
		if local := dir + `\node.exe`; exists(local) {
			return local, true
		}
		p, err := lookNode()
		return p, err == nil
	}
	isScript := func(p string) bool {
		lower := strings.ToLower(p)
		return strings.HasSuffix(lower, ".js") || strings.HasSuffix(lower, ".cjs") || strings.HasSuffix(lower, ".mjs")
	}

	first := tokens[0]
	switch base := strings.ToLower(winBase(first)); {
	case base == "node" || base == "node.exe" || strings.EqualFold(first, "node"):
		if len(tokens) < 2 || !exists(tokens[1]) {
			return nil, false
		}
		n, ok := node()
		if !ok {
			return nil, false
		}
		return append([]string{n}, tokens[1:]...), true
	case isScript(first):
		if !exists(first) {
			return nil, false
		}
		n, ok := node()
		if !ok {
			return nil, false
		}
		return append([]string{n}, tokens...), true
	case strings.HasSuffix(base, ".exe") && exists(first):
		return tokens, true
	}
	return nil, false
}

func winDir(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[:i]
	}
	return "."
}

func winBase(p string) string {
	return p[strings.LastIndexAny(p, `\/`)+1:]
}

// cleanWindows normalises a backslash path whatever the host OS, so the parser
// behaves the same under test on macOS/Linux as it does on Windows.
func cleanWindows(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	for strings.Contains(p, `\\`) {
		p = strings.ReplaceAll(p, `\\`, `\`)
	}
	parts := strings.Split(p, `\`)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case ".":
		case "..":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, part)
		}
	}
	return strings.Join(out, `\`)
}
