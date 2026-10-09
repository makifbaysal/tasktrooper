package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The rules in CLAUDE.md that are the security posture rather than a matter of
// taste, stated in Go so they hold when nobody has read the markdown.
//
// One of them changed with this rework and the change is worth being explicit
// about. This process used to be forbidden from spawning anything at all,
// because it was a byte-mover: an exec here would have turned a network proxy
// into a remote-code-execution surface for anything that could reach the
// control plane. Running Claude Code sessions IS this program's job now, so
// that rule cannot survive as written — but what it was protecting can:
//
//   - **It still binds nothing.** Every connection to the outside world is one
//     this process dials. A Mac behind a home router, with no port forwarded
//     and no inbound rule, is the whole point. The one loopback listener that
//     used to exist — the database socket, which replaced Google's
//     cloud-sql-proxy — is gone with the database.
//
//     It DOES run an http.Server, and that is not a hole: its listener is the
//     yamux session, which yields streams the control plane opened on a
//     connection this process dialled outward. The thing that would be a hole
//     is `net.Listen`, and that is what this test forbids — not the server type.
//     `ListenAndServe` is forbidden for the same reason: it binds.
//   - **It never runs a shell.** Children are spawned with an argv array and
//     no shell, which is what makes a backtick in a prompt a backtick. Nothing
//     a caller supplies reaches argv as free text at all: the prompt goes on
//     stdin and every other field is matched against a grammar first.
//   - **Cancellation kills the process GROUP.** Enforced behaviourally in
//     session_test.go, because it is a property of a running process tree
//     rather than of the source text.
//
// It works on the AST rather than on the text, so a comment that mentions
// net.Listen — like this one, and like the ones in main.go explaining why it is
// absent — is not a violation, and an aliased or renamed import is not a way
// around it. Test files are skipped: the fakes these tests dial are real TCP
// listeners.

// forbiddenSymbols are qualified identifiers, keyed by "<import path>.<name>",
// that must never appear in this package.
var forbiddenSymbols = map[string]string{
	"net.Listen":       "outbound-only: this process dials, it never accepts",
	"net.ListenTCP":    "outbound-only: this process dials, it never accepts",
	"net.ListenUnix":   "outbound-only: this process dials, it never accepts",
	"net.ListenPacket": "outbound-only: this process dials, it never accepts",
	"net.FileListener": "outbound-only: this process dials, it never accepts",
	// http.Server is allowed — see the header. These two are not, because they
	// bind: they are the only members of net/http that create a socket.
	"net/http.ListenAndServe":    "binds a port; the tunnel's listener is the yamux session and nothing else",
	"net/http.ListenAndServeTLS": "binds a port; the tunnel's listener is the yamux session and nothing else",
	// Spawning is now this program's job; spawning through a SHELL is not, and
	// never becomes it. exec.Command with an argv array is safe in a way that
	// `sh -c` composed from a caller's fields can never be made to be.
	"syscall.Exec": "replaces this process rather than supervising a child, so nothing could ever be cancelled",
}

// forbiddenImports cannot appear at all: importing one binds a port by
// definition, whichever symbol is then used.
var forbiddenImports = map[string]string{
	"net/http/httptest":        "a test server has no business in the shipped binary",
	"net/http/pprof":           "pprof exists to be served on a port, and this process binds none",
	"net/http/fcgi":            "binds a port, in another protocol",
	"net/rpc":                  "binds a port, in another protocol",
	"golang.org/x/net/netutil": "listener plumbing for a listener that is a tunnel session",
}

// shellBinaries are the interpreters a child must never be, as an argv element.
// Checked as string literals rather than through the AST, because the hazard is
// a value and not a call: `exec.Command("/bin/sh", "-c", …)` is the one spawn
// that would put a caller's text back in front of a parser.
var shellBinaries = []string{`"/bin/sh"`, `"/bin/bash"`, `"/bin/zsh"`, `"sh"`, `"bash"`, `"zsh"`, `"-c"`}

// shellText is what a shell PROGRAM looks like in a literal, as opposed to an
// interpreter in an argv. This half of the rule is the one that had gone
// vacuous: the exact-match list above reads like "there is no shell here", and
// there is one — this package generates a bash program and executes it, and that
// program sources two more. `#!` catches any shebang whatever it names, so a
// second generated script cannot arrive by spelling its interpreter differently.
var shellText = []string{"#!", "/bin/sh", "/bin/bash", "/bin/zsh", "/usr/bin/env"}

// releaseWrapperConst is the one literal allowed to match shellText, and
// TestTheOnlyShellIsTheGeneratedWrapper is the rule that keeps it the only one.
const releaseWrapperConst = "releaseWrapper"

// TestPackageNeverRunsAShell scans for a shell interpreter appearing as an argv
// literal anywhere in the shipped source. Deliberately blunt: there is no
// legitimate reason for one to be here, so a false positive is a conversation
// worth having and a false negative is a hole.
func TestPackageNeverRunsAShell(t *testing.T) {
	forEachSourceFile(t, func(fset *token.FileSet, name string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			for _, shell := range shellBinaries {
				if lit.Value == shell {
					t.Errorf("%s has the literal %s — children are spawned with an argv array and no shell",
						fset.Position(lit.Pos()), lit.Value)
				}
			}
			return true
		})
	})
}

// TestTheOnlyShellIsTheGeneratedWrapper states the rule as it actually stands
// now that this package runs a shell program on purpose.
//
// The rule used to be "no shell", enforced as seven exact string literals, and
// it kept passing after `releaseWrapper` arrived — a bash program, written to
// disk, executed, and sourcing two further files — because none of those seven
// spellings appears in it. A rule that reads as "there is no shell here" while
// there is one is worse than no rule: the next person adding a second generated
// script has nothing telling them it is a decision rather than a detail.
//
// So it is stated structurally instead. There may be exactly ONE shell program
// in this package, it is `releaseWrapper`, and the two tests below pin what it
// is allowed to be.
func TestTheOnlyShellIsTheGeneratedWrapper(t *testing.T) {
	found := 0
	forEachSourceFile(t, func(fset *token.FileSet, name string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if ok && len(spec.Names) == 1 && spec.Names[0].Name == releaseWrapperConst {
				found++
				// Its own literal is allowed to be a shell program; do not
				// descend into it.
				return false
			}
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, marker := range shellText {
				if strings.Contains(value, marker) {
					t.Errorf("%s has a literal containing %q — the only shell program in this package is %s, and a second one is a decision that has to be argued here first",
						fset.Position(lit.Pos()), marker, releaseWrapperConst)
				}
			}
			return true
		})
	})
	if found != 1 {
		t.Fatalf("found %d declarations of %s, want exactly 1; this rule is enforcing nothing", found, releaseWrapperConst)
	}
}

// releaseWrapperText is what that program is allowed to be, byte for byte.
//
// Pinned rather than described, because every line of it is load-bearing and
// three of them are security properties that read as tidying: the self-delete
// (a SIGKILL between exec and it leaves an executable in a git working copy),
// the unlink of the signing material the instant it has been sourced, and the
// ABSOLUTE shebang — `#!/usr/bin/env bash` resolved the interpreter through an
// inherited PATH, which is the same hole releaseEnviron closes from the other
// side.
const releaseWrapperText = `#!/bin/bash
# GENERATED per release run by the TaskTrooper runner, and deleted by its own
# first line. If you are reading this in a checkout, a release was SIGKILLed
# between exec and that line; it is safe to delete.
set -eu
tt_script="$1"
tt_channel="$2"
tt_secrets="$3"
rm -f "$0"
. "$tt_secrets"
rm -f "$tt_secrets"
set -- "$tt_channel"
. "$tt_script"
`

// TestReleaseWrapperIsFixedAndTakesItsInputsAsArguments is the other half of
// the rule above: the one shell program is a CONSTANT, and nothing a caller
// sends is ever spliced into it.
//
// Both halves are checked structurally rather than by reading the string,
// because "no interpolation" is a property of how the value is built. A
// fmt.Sprintf here — a channel, a path, a secret name — would put caller text
// in front of a shell parser, which is the single thing this package's oldest
// rule exists to prevent. The wrapper instead takes all three of its inputs as
// positional parameters, where a value is a value whatever is in it.
func TestReleaseWrapperIsFixedAndTakesItsInputsAsArguments(t *testing.T) {
	if releaseWrapper != releaseWrapperText {
		t.Fatalf("the generated wrapper changed. It is pinned here because every line of it is a property argued in mobile_release.go — the self-delete, the unlink of the signing material, the absolute shebang. If the change is deliberate, restate the property here.\ngot:\n%s\nwant:\n%s", releaseWrapper, releaseWrapperText)
	}
	if !strings.HasPrefix(releaseWrapper, "#!/bin/bash\n") {
		t.Error("the wrapper's shebang is not an absolute path; `env` resolves the interpreter through PATH, which is the hole the curated environment closes from the other side")
	}
	for _, arg := range []string{`"$1"`, `"$2"`, `"$3"`, `"$0"`} {
		if !strings.Contains(releaseWrapper, arg) {
			t.Errorf("the wrapper no longer uses %s; its inputs are positional parameters, never text spliced into the program", arg)
		}
	}

	forEachSourceFile(t, func(fset *token.FileSet, name string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			// The declaration: one plain literal, not a concatenation and not
			// the result of a call.
			if spec, ok := n.(*ast.ValueSpec); ok && len(spec.Names) == 1 && spec.Names[0].Name == releaseWrapperConst {
				if len(spec.Values) != 1 {
					t.Errorf("%s: %s has %d values, want one literal", fset.Position(spec.Pos()), releaseWrapperConst, len(spec.Values))
					return false
				}
				if lit, ok := spec.Values[0].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
					t.Errorf("%s: %s is built rather than written down; a shell program assembled at run time is one whose content nothing pins",
						fset.Position(spec.Pos()), releaseWrapperConst)
				}
				return false
			}
			// Every use: never an operand of a `+`, never an argument to a
			// formatting call.
			if bin, ok := n.(*ast.BinaryExpr); ok && (namesWrapper(bin.X) || namesWrapper(bin.Y)) {
				t.Errorf("%s: %s is combined with another expression; the wrapper is written verbatim or not at all",
					fset.Position(bin.Pos()), releaseWrapperConst)
			}
			if callExpr, ok := n.(*ast.CallExpr); ok {
				for _, arg := range callExpr.Args {
					if !namesWrapper(arg) {
						continue
					}
					if sel, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
						if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" {
							t.Errorf("%s: %s is passed to fmt.%s; caller text spliced into a shell program is the one thing this package has never allowed",
								fset.Position(callExpr.Pos()), releaseWrapperConst, sel.Sel.Name)
						}
					}
				}
			}
			return true
		})
	})
}

func namesWrapper(e ast.Expr) bool {
	ident, ok := e.(*ast.Ident)
	return ok && ident.Name == releaseWrapperConst
}

// forEachSourceFile parses this package's shipped source. Test files are
// skipped: the fakes these tests dial are real TCP listeners, and the wrapper's
// pinned copy above is a shell program on purpose.
func forEachSourceFile(t *testing.T, fn func(*token.FileSet, string, *ast.File)) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		fn(fset, name, file)
	}
	if scanned == 0 {
		t.Fatal("no non-test Go files were scanned; this rule is now enforcing nothing")
	}
}

func TestPackageNeverBindsAPort(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		checkFile(t, name)
	}
	// A rule enforced over nothing is not enforced. If the package is ever
	// laid out differently, this test must be made to find it again.
	if scanned == 0 {
		t.Fatal("no non-test Go files were scanned; this rule is now enforcing nothing")
	}
}

// The supervisor's SIGTERM grace must not be shorter than this program's own
// worst-case drain.
//
// These two numbers live in two languages and were previously chosen
// independently: 8s in `child.ts` against 25s here. Quitting the app while a
// task was running therefore SIGKILLed the runner before it had finished
// reaping its `claude` process groups — which orphaned the tree the whole
// ordered teardown exists to take down, and left the run's MCP bearer token on
// disk until the next startup sweep. Neither side was wrong on its own; they
// were wrong together, which is the kind of bug only a test spanning both can
// hold shut.
//
// A TypeScript file cannot import a Go constant, so the numbers are duplicated
// there and this reads them back. That makes the duplication a derivation.
func TestSupervisorGraceCoversThisProgramsDrain(t *testing.T) {
	const childTS = runnerSupervisorDir + "/child.ts"
	src, err := os.ReadFile(childTS)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("%s does not exist yet: the desktop app's runner supervisor is not in this checkout", childTS)
	}
	if err != nil {
		t.Fatalf("reading %s: %v", childTS, err)
	}
	text := string(src)

	msFor := func(constName string) time.Duration {
		t.Helper()
		re := regexp.MustCompile(`(?m)^const ` + regexp.QuoteMeta(constName) + ` = ([0-9_]+);`)
		m := re.FindStringSubmatch(text)
		if m == nil {
			t.Fatalf("%s no longer declares %s as a numeric literal; this rule is enforcing nothing", childTS, constName)
		}
		n, convErr := strconv.Atoi(strings.ReplaceAll(m[1], "_", ""))
		if convErr != nil {
			t.Fatalf("%s = %q, which is not a number: %v", constName, m[1], convErr)
		}
		return time.Duration(n) * time.Millisecond
	}

	// The copies of this package's constants, which must still be the same
	// numbers. Checked separately from the comparison below so a drift reports
	// as drift rather than as an off-by-one in a budget.
	if got := msFor("RUNNER_CLAUDE_GRACE_MS"); got != claudeGrace {
		t.Errorf("%s says claudeGrace is %s; this package says %s", childTS, got, claudeGrace)
	}
	if got := msFor("RUNNER_REAP_TIMEOUT_MS"); got != claudeReapTimeout {
		t.Errorf("%s says claudeReapTimeout is %s; this package says %s", childTS, got, claudeReapTimeout)
	}

	// And the property that actually matters: the supervisor waits at least as
	// long as this program needs.
	if !strings.Contains(text, "const TERM_GRACE_MS = RUNNER_DRAIN_BUDGET_MS +") {
		t.Fatalf("%s no longer derives TERM_GRACE_MS from the runner's drain budget; "+
			"a grace period chosen independently of this program's is how a quit orphans a claude session", childTS)
	}
	if drainBudget != claudeGrace+claudeReapTimeout {
		t.Fatalf("drainBudget is %s, which is not claudeGrace + claudeReapTimeout (%s)", drainBudget, claudeGrace+claudeReapTimeout)
	}
}

func checkFile(t *testing.T, name string) {
	t.Helper()

	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	// Local name -> import path, so an aliased import is checked as what it is.
	imported := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s: unquote import %s: %v", name, spec.Path.Value, err)
		}
		if why, bad := forbiddenImports[path]; bad {
			t.Errorf("%s imports %q — %s", fset.Position(spec.Pos()), path, why)
			continue
		}
		local := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			// A dot import would put Listen into this file's scope unqualified
			// and make the check below blind to it.
			if spec.Name.Name == "." {
				t.Errorf("%s dot-imports %q; qualified imports only, so this rule stays checkable",
					fset.Position(spec.Pos()), path)
				continue
			}
			local = spec.Name.Name
		}
		imported[local] = path
	}

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		path, ok := imported[pkg.Name]
		if !ok {
			return true
		}
		if why, bad := forbiddenSymbols[path+"."+sel.Sel.Name]; bad {
			t.Errorf("%s uses %s.%s — %s", fset.Position(sel.Pos()), pkg.Name, sel.Sel.Name, why)
		}
		return true
	})
}
