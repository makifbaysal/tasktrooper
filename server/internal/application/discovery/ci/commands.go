package ci

import (
	"path"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// cleanPath cleans a repo-relative path the same way inventory.Tree does;
// inventory.clean is unexported, so Join against "." stands in for it.
func cleanPath(p string) string { return inventory.Join(".", p) }

// jobCommands is one job's run-line extraction: the runnable LocalCommands in
// step order, plus every directory or workspace name a run line pointed at —
// the mapper needs the pointer even when the command itself (e.g. "npm ci")
// was filtered out as a dependency install.
type jobCommands struct {
	Commands  []domain.LocalCommand
	DirHints  []string
	NameHints []string
}

func extractJobCommands(j job, workflowWorkingDir string) jobCommands {
	var out jobCommands
	for _, s := range j.Steps {
		base := s.WorkingDir
		if base == "" {
			base = j.WorkingDir
		}
		if base == "" {
			base = workflowWorkingDir
		}
		if base == "" {
			base = "."
		}
		extractStepRun(s.Run, cleanPath(base), &out)
	}
	return out
}

func extractStepRun(run string, baseDir string, out *jobCommands) {
	dir := baseDir
	for _, logical := range joinContinuations(run) {
		trimmed := strings.TrimSpace(logical)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if isSetLine(trimmed) {
			continue
		}
		for _, rawSeg := range splitTopLevel(trimmed, "&&") {
			seg := strings.TrimSpace(rawSeg)
			if seg == "" {
				continue
			}
			if target, ok := cdTarget(seg); ok {
				if !strings.ContainsAny(target, "$`") {
					dir = joinDir(dir, unquote(target))
					out.DirHints = append(out.DirHints, dir)
				}
				continue
			}
			processCommandSegment(seg, dir, out)
		}
	}
}

func processCommandSegment(core, dir string, out *jobCommands) {
	tokens, ok := tokenize(core)
	if !ok || len(tokens) == 0 {
		return
	}

	dirHint, nameHint, rewriteDir, rewriteArgv := scanPrefixFlags(tokens, dir)
	if dirHint != "" {
		out.DirHints = append(out.DirHints, dirHint)
	}
	if nameHint != "" {
		out.NameHints = append(out.NameHints, nameHint)
	}

	if disqualifiedText(core) || disqualifiedLeadingWord(tokens) {
		return
	}

	argv := tokens
	effectiveDir := dir
	if rewriteDir != "" {
		effectiveDir = rewriteDir
		argv = rewriteArgv
	}
	if isDependencyInstall(argv) {
		return
	}
	out.Commands = append(out.Commands, domain.LocalCommand{Dir: cleanPath(effectiveDir), Argv: argv})
}

func joinDir(base, rel string) string {
	if strings.HasPrefix(rel, "/") {
		return cleanPath(rel)
	}
	return inventory.Join(base, rel)
}

// joinContinuations folds backslash line continuations into logical lines.
func joinContinuations(run string) []string {
	raw := strings.Split(run, "\n")
	var out []string
	var pending strings.Builder
	has := false
	for _, line := range raw {
		trimmedRight := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmedRight, "\\") {
			pending.WriteString(strings.TrimSuffix(trimmedRight, "\\"))
			pending.WriteString(" ")
			has = true
			continue
		}
		if has {
			pending.WriteString(line)
			out = append(out, pending.String())
			pending.Reset()
			has = false
		} else {
			out = append(out, line)
		}
	}
	if has {
		out = append(out, pending.String())
	}
	return out
}

func isSetLine(trimmed string) bool {
	fields := strings.Fields(trimmed)
	return len(fields) > 0 && fields[0] == "set" && (len(fields) == 1 || strings.HasPrefix(fields[1], "-"))
}

var cdRe = regexp.MustCompile(`^cd\s+(\S.*)$`)

// cdTarget recognizes "cd X" and "cd X;" (with no other statement in the
// segment); any other embedded ';' disqualifies the whole segment instead,
// which is handled by disqualifiedText.
func cdTarget(seg string) (string, bool) {
	core := seg
	if strings.HasSuffix(core, ";") {
		core = strings.TrimSpace(strings.TrimSuffix(core, ";"))
	}
	if strings.Contains(core, ";") {
		return "", false
	}
	m := cdRe.FindStringSubmatch(core)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

var varPattern = regexp.MustCompile(`\$\{|\$[A-Za-z_][A-Za-z0-9_]*`)
var assignPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=\S`)

// disqualifiedText rejects shell syntax a plain argv exec cannot reproduce:
// pipes, redirects, stray semicolons, subshells, and variable expansion.
func disqualifiedText(core string) bool {
	if strings.ContainsAny(core, "|><;") {
		return true
	}
	if strings.Contains(core, "$(") || strings.Contains(core, "`") {
		return true
	}
	if varPattern.MatchString(core) {
		return true
	}
	if assignPattern.MatchString(core) {
		return true
	}
	return false
}

var leadingBlacklist = map[string]bool{
	"sudo": true, "apt": true, "apt-get": true, "brew": true, "choco": true,
	"echo": true, "export": true, "mkdir": true, "cp": true, "mv": true,
	"rm": true, "chmod": true, "git": true, "curl": true, "wget": true,
	"cat": true, "ls": true, "source": true, ".": true,
}

func disqualifiedLeadingWord(tokens []string) bool {
	return len(tokens) == 0 || leadingBlacklist[tokens[0]]
}

// isDependencyInstall recognizes the package-manager invocations the verify
// runner already performs itself, so they never become a redundant LocalCommand.
func isDependencyInstall(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	get := func(i int) string {
		if i < len(argv) {
			return argv[i]
		}
		return ""
	}
	switch argv[0] {
	case "npm":
		return get(1) == "ci" || get(1) == "install"
	case "pnpm":
		return get(1) == "install" || get(1) == "i"
	case "yarn":
		return len(argv) == 1 || get(1) == "install"
	case "bun":
		return get(1) == "install"
	case "go":
		return get(1) == "mod" && (get(2) == "download" || get(2) == "tidy")
	case "pip", "pip3":
		return get(1) == "install"
	case "poetry":
		return get(1) == "install"
	case "uv":
		return get(1) == "sync"
	case "bundle":
		return get(1) == "install"
	case "composer":
		return get(1) == "install"
	case "flutter":
		return get(1) == "pub" && get(2) == "get"
	case "pod":
		return get(1) == "install"
	}
	return false
}

// scanPrefixFlags extracts a mapping candidate (directory or workspace name)
// from a tool-prefix flag, and, for the tools whose flag is a genuine
// directory switch, the Dir/argv rewrite the spec lists (npm --prefix,
// pnpm -C/--dir, yarn --cwd, go -C, make -C). Filter-style flags (pnpm
// --filter, yarn workspace, npm -w, turbo --filter) only ever contribute a
// candidate: they run from wherever the job already is.
func scanPrefixFlags(tokens []string, baseDir string) (dirHint, nameHint, rewriteDir string, rewriteArgv []string) {
	if len(tokens) == 0 {
		return
	}
	switch tokens[0] {
	case "npm":
		if idx, span, v, ok := findFlag(tokens, "--prefix", ""); ok {
			d := joinDir(baseDir, v)
			return d, "", d, removeFlag(tokens, idx, span)
		}
		if _, _, v, ok := findFlag(tokens, "--workspace", "-w"); ok {
			return "", v, "", nil
		}
	case "pnpm":
		if idx, span, v, ok := findFlag(tokens, "--dir", "-C"); ok {
			d := joinDir(baseDir, v)
			return d, "", d, removeFlag(tokens, idx, span)
		}
		if _, _, v, ok := findFlag(tokens, "--filter", "-F"); ok {
			d, n := filterCandidate(v)
			if d != "" {
				return joinDir(baseDir, d), "", "", nil
			}
			return "", n, "", nil
		}
	case "yarn":
		if idx, span, v, ok := findFlag(tokens, "--cwd", ""); ok {
			d := joinDir(baseDir, v)
			return d, "", d, removeFlag(tokens, idx, span)
		}
		if len(tokens) >= 3 && tokens[1] == "workspace" {
			return "", tokens[2], "", nil
		}
	case "go":
		if idx, span, v, ok := findFlag(tokens, "-C", ""); ok {
			d := joinDir(baseDir, v)
			return d, "", d, removeFlag(tokens, idx, span)
		}
	case "make":
		if idx, span, v, ok := findFlag(tokens, "-C", ""); ok {
			d := joinDir(baseDir, v)
			return d, "", d, removeFlag(tokens, idx, span)
		}
	case "cargo":
		if _, _, v, ok := findFlag(tokens, "--manifest-path", ""); ok {
			d := path.Dir(cleanPath(v))
			return joinDir(baseDir, d), "", "", nil
		}
	case "turbo":
		if _, _, v, ok := findFlag(tokens, "--filter", ""); ok {
			return "", v, "", nil
		}
	}
	return
}

func filterCandidate(value string) (dir, name string) {
	v := strings.TrimSpace(value)
	v = strings.TrimPrefix(v, "{")
	v = strings.TrimSuffix(v, "}")
	if strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") {
		return v, ""
	}
	return "", v
}

// findFlag looks for `long` or `short` among tokens in either "--flag value"
// or "--flag=value" form, and reports how many tokens the match spans.
func findFlag(tokens []string, long, short string) (idx, span int, value string, ok bool) {
	for i, t := range tokens {
		if long != "" {
			if t == long && i+1 < len(tokens) {
				return i, 2, tokens[i+1], true
			}
			if v, found := strings.CutPrefix(t, long+"="); found {
				return i, 1, v, true
			}
		}
		if short != "" && t == short && i+1 < len(tokens) {
			return i, 2, tokens[i+1], true
		}
	}
	return 0, 0, "", false
}

func removeFlag(tokens []string, idx, span int) []string {
	out := make([]string, 0, len(tokens)-span)
	out = append(out, tokens[:idx]...)
	out = append(out, tokens[idx+span:]...)
	return out
}
