package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// launcher is how a configured CLI binary is actually exec'd: the program, and
// the arguments that go in front of the caller's.
//
// On macOS and Linux it is always the binary itself. On Windows an npm global
// install hands detect.ts a `claude.cmd` (or `opencode.cmd`, `cursor-agent.cmd`)
// rather than a program, and CreateProcess on a .cmd runs cmd.exe on it
// implicitly. That is a shell this package never runs, reached without anybody
// naming one: cmd.exe re-parses the command line with its own rules, so an
// argument the Go side quoted correctly for a program — cursor.run's prompt is
// argv — can end its quoting and run a command (BatBadBut), and the whole line
// is cut at cmd.exe's 8191 characters. So a shim is never exec'd: it is read,
// and what it would have run — node and a script — is run instead.
//
// Claude Code's native Windows installer ships a real claude.exe, and that
// path, like every non-batch path, is used exactly as configured.
type launcher struct {
	path   string
	prefix []string
}

// maxShimBytes bounds the read of a shim. npm's are under a kilobyte; a
// "shim" much larger than this is not one.
const maxShimBytes = 64 * 1024

// launcherFor resolves bin into what is exec'd. The error is a sentence for a
// not_ready answer.
func launcherFor(bin string) (launcher, error) {
	if runtime.GOOS != "windows" || !isBatchFile(bin) {
		return launcher{path: bin}, nil
	}
	l, err := resolveNPMShim(bin, exec.LookPath)
	if err != nil {
		return launcher{}, fmt.Errorf("%s is a Windows batch file, which this runner never runs (cmd.exe would re-parse every argument), "+
			"and it could not be read as an npm shim to run its node script directly: %v. "+
			"Reinstall the CLI with npm, or use an installer that ships an .exe", bin, err)
	}
	return l, nil
}

func (l launcher) args(args []string) []string {
	return append(append(make([]string, 0, len(l.prefix)+len(args)), l.prefix...), args...)
}

func (l launcher) command(args ...string) *exec.Cmd {
	return exec.Command(l.path, l.args(args)...)
}

func (l launcher) commandContext(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, l.path, l.args(args)...)
}

// commandFor is exec.Command for a configured binary.
func commandFor(bin string, args ...string) (*exec.Cmd, error) {
	l, err := launcherFor(bin)
	if err != nil {
		return nil, err
	}
	return l.command(args...), nil
}

func isBatchFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cmd" || ext == ".bat"
}

// resolveNPMShim reads the shim at path and returns node plus the script it
// runs, or the target itself when the shim runs an .exe with no interpreter.
// node is the one beside the shim when npm put one there — which is what the
// shim itself prefers — and otherwise whatever lookPath finds.
func resolveNPMShim(path string, lookPath func(string) (string, error)) (launcher, error) {
	f, err := os.Open(path)
	if err != nil {
		return launcher{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxShimBytes+1))
	_ = f.Close()
	if err != nil {
		return launcher{}, err
	}
	if len(raw) > maxShimBytes {
		return launcher{}, errors.New("it is too large to be an npm shim")
	}
	shim, err := parseNPMShim(string(raw))
	if err != nil {
		return launcher{}, err
	}

	dir := filepath.Dir(path)
	target := filepath.Join(dir, filepath.FromSlash(shim.script))
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		return launcher{}, fmt.Errorf("it runs %s, which is not a file", target)
	}
	if !shim.node {
		if !strings.EqualFold(filepath.Ext(target), ".exe") {
			return launcher{}, fmt.Errorf("it runs %s directly, which is not an .exe", target)
		}
		return launcher{path: target}, nil
	}

	node := filepath.Join(dir, "node.exe")
	if info, err := os.Stat(node); err != nil || !info.Mode().IsRegular() {
		if node, err = lookPath("node"); err != nil {
			return launcher{}, fmt.Errorf("it runs %s with node, and node is not on PATH: %v", target, err)
		}
	}
	return launcher{path: node, prefix: []string{target}}, nil
}

// npmShim is what a cmd-shim runs: a script relative to the shim's own
// directory (slash-separated), and whether node is what runs it.
type npmShim struct {
	script string
	node   bool
}

// parseNPMShim reads the text of an npm cmd-shim — the .cmd npm, pnpm and yarn
// write beside every global bin — and returns the script it runs.
//
// The shim's last command line ends in `%*` and names the target relative to
// the shim's own directory, `%dp0%` in today's npm and `%~dp0` in older ones
// and in pnpm's:
//
//	"%_prog%"  "%dp0%\node_modules\@anthropic-ai\claude-code\cli.js" %*
//	"%~dp0\node.exe"  "%~dp0\..\@anthropic-ai\claude-code\cli.js" %*
//	node  "%~dp0\node_modules\opencode-ai\bin\opencode" %*
//
// The interpreter must be node: the bare word, a node.exe beside the shim, or
// `%_prog%` when every `_prog` the shim assigns is one of those. A shim that
// names no interpreter runs its target directly. Anything else — a shim for
// bash or python, or a batch file that is not a shim at all — is refused.
func parseNPMShim(text string) (npmShim, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		before, _, found := strings.Cut(line, "%*")
		if !found {
			continue
		}
		quoted := quotedTokens(before)
		var script string
		for i := len(quoted) - 1; i >= 0; i-- {
			if rel, ok := dp0Relative(quoted[i]); ok && !isNodeExe(rel) {
				script = rel
				break
			}
		}
		if script == "" {
			continue
		}
		node, err := shimInterpreter(text, before, quoted)
		if err != nil {
			return npmShim{}, err
		}
		return npmShim{script: script, node: node}, nil
	}
	return npmShim{}, errors.New("it does not look like an npm shim (no line runs a script next to it with %*)")
}

// shimInterpreter decides what runs the script on a shim's command line.
func shimInterpreter(text, before string, quoted []string) (bool, error) {
	for _, token := range quoted {
		if strings.EqualFold(token, "%_prog%") {
			progs := progAssignments(text)
			if len(progs) == 0 {
				return false, errors.New("it runs %_prog% and never says what that is")
			}
			for _, prog := range progs {
				if !isNodeProgram(prog) {
					return false, fmt.Errorf("it runs its script with %q, not node", prog)
				}
			}
			return true, nil
		}
		if rel, ok := dp0Relative(token); ok && isNodeExe(rel) {
			return true, nil
		}
	}
	first := strings.Fields(strings.TrimLeft(strings.TrimSpace(before), "@"))
	if len(first) > 0 && strings.EqualFold(strings.Trim(first[0], `"`), "node") {
		return true, nil
	}
	if len(quoted) > 0 {
		if _, ok := dp0Relative(quoted[0]); ok && len(quoted) == 1 {
			return false, nil
		}
	}
	return false, errors.New("it does not run its script with node")
}

// progAssignments returns every value the shim gives `_prog`, as in
// `SET "_prog=%dp0%\node.exe"` and `SET "_prog=node"`.
func progAssignments(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(line)
		at := strings.Index(lower, "_prog=")
		if at < 0 || !strings.Contains(lower[:at], "set") {
			continue
		}
		value := line[at+len("_prog="):]
		out = append(out, strings.TrimSpace(strings.TrimRight(strings.TrimSpace(value), `"`)))
	}
	return out
}

func isNodeProgram(prog string) bool {
	if strings.EqualFold(prog, "node") || strings.EqualFold(prog, "node.exe") {
		return true
	}
	rel, ok := dp0Relative(prog)
	return ok && isNodeExe(rel)
}

func isNodeExe(rel string) bool {
	return strings.EqualFold(rel, "node.exe")
}

// dp0Relative strips a shim's own-directory prefix — `%dp0%` or `%~dp0` — and
// returns the rest slash-separated, or false when the token does not start
// with one.
func dp0Relative(token string) (string, bool) {
	lower := strings.ToLower(token)
	var rest string
	switch {
	case strings.HasPrefix(lower, "%dp0%"):
		rest = token[len("%dp0%"):]
	case strings.HasPrefix(lower, "%~dp0"):
		rest = token[len("%~dp0"):]
	default:
		return "", false
	}
	rest = strings.TrimLeft(strings.ReplaceAll(rest, `\`, "/"), "/")
	if rest == "" {
		return "", false
	}
	return rest, true
}

// quotedTokens returns the double-quoted strings in s, in order.
func quotedTokens(s string) []string {
	var out []string
	for {
		open := strings.IndexByte(s, '"')
		if open < 0 {
			return out
		}
		length := strings.IndexByte(s[open+1:], '"')
		if length < 0 {
			return out
		}
		out = append(out, s[open+1:open+1+length])
		s = s[open+1+length+1:]
	}
}
