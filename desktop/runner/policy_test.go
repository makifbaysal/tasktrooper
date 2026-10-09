//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tool policy, the effort level, the extra environment, and the setting
// sources — tested from inside the child, because argv and an environment are
// things only the child can actually report.
//
// The one that is not a parameter is the one that matters most. Left to its
// default the CLI also loads the user's own `~/.claude`, so a `SessionStart`
// hook or a plugin somebody installed for themselves would run inside a board
// task on their Mac. `--setting-sources project,local` closes that, it is
// unconditional, and the test below is what stops a later change from making
// it optional again — a security property the caller has to remember to ask
// for is not a security property.

// reportingClaude writes a fake CLI that echoes its arguments and the parts of
// its environment a test wants to see.
func reportingClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"cat >/dev/null\n" +
		"echo \"argv:$*\"\n" +
		"echo \"env:GOTOOLCHAIN=$GOTOOLCHAIN\"\n" +
		"echo \"env:NODE_VERSION=$NODE_VERSION\"\n" +
		"echo \"env:HOME_INHERITED=${HOME:+yes}\"\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake claude: %v", err)
	}
	return path
}

// runReporting posts one claude.run and returns every line the fake CLI wrote,
// after the response has ended.
func runReporting(t *testing.T, body string) []string {
	t.Helper()
	h := newMCPHarness(t, reportingClaude(t), emptyWorkspace(t), t.TempDir())
	res := h.start(t, body)
	defer func() { _ = res.Body.Close() }()
	ch := frames(res)

	var lines []string
	deadline := time.After(30 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return lines
			}
			if f["event"] == "output" {
				if data, isString := f["data"].(string); isString {
					lines = append(lines, data)
				}
			}
			if f["event"] == "done" {
				if f["ok"] != true {
					t.Fatalf("the run failed: %v", f["error"])
				}
				return lines
			}
		case <-deadline:
			t.Fatalf("the run never finished; lines so far: %v", lines)
		}
	}
}

func lineWithPrefix(t *testing.T, lines []string, prefix string) string {
	t.Helper()
	for _, line := range lines {
		if rest, found := strings.CutPrefix(line, prefix); found {
			return rest
		}
	}
	t.Fatalf("the session never reported a %q line; it wrote %v", prefix, lines)
	return ""
}

// The unconditional one. A run that asks for nothing at all still gets it,
// which is the whole point: the cloud forgetting a field must never be the
// difference between "the operator's hooks ran" and "they did not".
func TestEverySessionIsPinnedToProjectAndLocalSettingsOnly(t *testing.T) {
	lines := runReporting(t, `{"id":"c-plain","workspace":"repo","prompt":"go"}`)
	argv := lineWithPrefix(t, lines, "argv:")
	if !strings.Contains(argv, "--setting-sources project,local") {
		t.Fatalf("argv %q has no --setting-sources project,local; the user's own ~/.claude hooks and plugins would run inside this task", argv)
	}
	// `user` is the source that would bring somebody's own settings in. Naming
	// it here rather than only checking the positive keeps a later widening of
	// the value — "project,local,user" — from passing the check above.
	//
	// Asserted on the FLAG'S OWN VALUE, not on the whole argv. A substring scan
	// of the command line matches any future flag or path containing "user" —
	// `/Users/...` in a --mcp-config path is the obvious one — so it would have
	// started failing for a reason unrelated to what it is checking, and the
	// fix for that kind of failure is usually to delete the assertion.
	sources := flagValue(t, argv, "--setting-sources")
	for _, source := range strings.Split(sources, ",") {
		if strings.TrimSpace(source) == "user" {
			t.Fatalf("--setting-sources is %q, which names the user's own settings", sources)
		}
	}
}

// Unconditional too: a run whose caller sent no tool policy has every built-in,
// the CLI's own question card included, and nobody in --print to answer it.
func TestEverySessionWithholdsTheCLIsOwnQuestionCard(t *testing.T) {
	lines := runReporting(t, `{"id":"c-plain","workspace":"repo","prompt":"go"}`)
	argv := lineWithPrefix(t, lines, "argv:")
	if got := flagValue(t, argv, "--disallowedTools"); got != "AskUserQuestion" {
		t.Fatalf("--disallowedTools is %q; the CLI's own AskUserQuestion card would lose every question asked through it", got)
	}
}

// flagValue pulls one flag's value out of the argv line the fake CLI echoed.
// Fails the test when the flag is absent, so a missing flag is never mistaken
// for a flag with an innocuous value.
func flagValue(t *testing.T, argv, flag string) string {
	t.Helper()
	fields := strings.Fields(argv)
	for i, f := range fields {
		if f == flag {
			if i+1 >= len(fields) {
				t.Fatalf("%s is the last argument, with no value after it: %q", flag, argv)
			}
			return fields[i+1]
		}
	}
	t.Fatalf("argv %q does not carry %s", argv, flag)
	return ""
}

// The three parameters, end to end: the flags the CLI is given and the
// environment it is started with.
func TestToolsEffortAndEnvReachTheChild(t *testing.T) {
	lines := runReporting(t, `{"id":"c-policy","workspace":"repo","prompt":"go",`+
		`"tools":["Read","Bash","mcp__tasktrooper__update_criterion"],`+
		`"effort":"high",`+
		`"env":{"GOTOOLCHAIN":"go1.24.0","NODE_VERSION":"22.11.0"}}`)

	argv := lineWithPrefix(t, lines, "argv:")
	// Split across the CLI's two flags — see
	// TestTheToolPolicyIsSplitAcrossTheCLIsTwoFlags for why. Each half is still
	// ONE comma-joined argument, which is what the name grammar buys.
	if got := flagValue(t, argv, "--tools"); got != "Read,Bash" {
		t.Fatalf("--tools = %q, want the built-in half of the policy as one comma-joined argument", got)
	}
	if got := flagValue(t, argv, "--allowedTools"); got != "mcp__tasktrooper__update_criterion" {
		t.Fatalf("--allowedTools = %q, want the MCP half of the policy", got)
	}
	if !strings.Contains(argv, "--effort high") {
		t.Fatalf("argv %q does not carry the effort level", argv)
	}

	if got := lineWithPrefix(t, lines, "env:GOTOOLCHAIN="); got != "go1.24.0" {
		t.Fatalf("GOTOOLCHAIN in the child = %q, want the value the caller sent", got)
	}
	if got := lineWithPrefix(t, lines, "env:NODE_VERSION="); got != "22.11.0" {
		t.Fatalf("NODE_VERSION in the child = %q, want the value the caller sent", got)
	}
	// Extra environment EXTENDS this process's rather than replacing it. A
	// child with nothing but the caller's two variables would have no HOME, no
	// PATH and no way to run git.
	if got := lineWithPrefix(t, lines, "env:HOME_INHERITED="); got != "yes" {
		t.Fatalf("the child did not inherit this process's environment (HOME was %q)", got)
	}
}

// A run that sends none of the three is unchanged: no --tools, no --effort,
// and an environment that is only what this process already had. That is what
// makes these parameters safe to add to a wire format other callers already
// use.
func TestARunThatSendsNoneOfThemIsUnchanged(t *testing.T) {
	lines := runReporting(t, `{"id":"c-none","workspace":"repo","prompt":"go"}`)
	argv := lineWithPrefix(t, lines, "argv:")
	for _, absent := range []string{"--tools", "--effort"} {
		if strings.Contains(argv, absent) {
			t.Fatalf("argv %q carries %s for a call that asked for none", argv, absent)
		}
	}
	// Unchanged means "whatever this process already had", NOT empty: a run
	// extends the environment rather than replacing it, which is what the
	// sibling test above pins. Asserting empty only held because a developer's
	// shell has no GOTOOLCHAIN — actions/setup-go exports GOTOOLCHAIN=local, so
	// the first CI run that ever reached this test failed on an environment the
	// code is supposed to pass through untouched.
	if got, want := lineWithPrefix(t, lines, "env:GOTOOLCHAIN="), os.Getenv("GOTOOLCHAIN"); got != want {
		t.Fatalf("GOTOOLCHAIN in the child = %q, want this process's own %q for a call that sent no env", got, want)
	}
}

// Every one of these is refused with a status, before the 200 — not in a
// `done` frame the caller has to read a stream to reach, and not by handing
// the CLI an argument built out of it.
func TestAMalformedPolicyEffortOrEnvIsRefusedBeforeTheStream(t *testing.T) {
	workspace := emptyWorkspace(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"a tool that is a flag", `"tools":["--dangerously-skip-permissions"]`, "is not a tool name"},
		{"a tool with a comma in it", `"tools":["Read,Bash"]`, "is not a tool name"},
		{"a tool with a space in it", `"tools":["Read Bash"]`, "is not a tool name"},
		{"an empty tool policy", `"tools":[]`, "empty"},
		{"an effort that is a flag", `"effort":"--debug"`, "is not an effort level"},
		{"an effort with a space in it", `"effort":"very high"`, "is not an effort level"},
		{"an env name that is not one", `"env":{"not-a-name":"x"}`, "is not an environment variable name"},
		{"an env name with an equals in it", `"env":{"A=B":"x"}`, "is not an environment variable name"},
		// The env cases below assert the NAME in the refusal rather than a
		// per-name explanation, because there is no longer a per-name
		// explanation to assert: the allowlist refuses everything it does not
		// know, with one message that says what the parameter is for. Naming the
		// variable is what makes the refusal actionable, and the shared check
		// after the table is what makes it explain itself.
		//
		// The list is kept — rather than collapsed to one case — because these
		// are the names the old denylist enumerated, and a regression to
		// "refuse the bad ones" must fail here as well as in
		// TestEnvRefusesEveryNameThatDecidesWhatRuns.
		{"PATH", `"env":{"PATH":"/tmp/evil"}`, "env may not set PATH"},
		{"HOME", `"env":{"HOME":"/tmp/elsewhere"}`, "env may not set HOME"},
		{"a dynamic linker knob", `"env":{"DYLD_INSERT_LIBRARIES":"/tmp/evil.dylib"}`, "env may not set DYLD_INSERT_LIBRARIES"},
		{"LD_PRELOAD", `"env":{"LD_PRELOAD":"/tmp/evil.so"}`, "env may not set LD_PRELOAD"},
		{"the CLI's own config directory", `"env":{"CLAUDE_CONFIG_DIR":"/tmp/theirs"}`, "env may not set CLAUDE_CONFIG_DIR"},
		{"the model endpoint", `"env":{"ANTHROPIC_BASE_URL":"https://elsewhere.example"}`, "env may not set ANTHROPIC_BASE_URL"},
		{"a command git would run", `"env":{"GIT_SSH_COMMAND":"sh -c whoami"}`, "env may not set GIT_SSH_COMMAND"},
		{"git config through the environment", `"env":{"GIT_CONFIG_GLOBAL":"/tmp/theirs"}`, "env may not set GIT_CONFIG_GLOBAL"},
		{"a file node would require", `"env":{"NODE_OPTIONS":"--require /tmp/evil.js"}`, "env may not set NODE_OPTIONS"},
		{"a shell startup file", `"env":{"BASH_ENV":"/tmp/evil.sh"}`, "env may not set BASH_ENV"},
		// The names that got through the denylist. Same refusal, same reason —
		// which is the point of an allowlist.
		{"git's helper directory", `"env":{"GIT_EXEC_PATH":"/tmp/evil"}`, "env may not set GIT_EXEC_PATH"},
		{"the XDG config root", `"env":{"XDG_CONFIG_HOME":"/tmp/theirs"}`, "env may not set XDG_CONFIG_HOME"},
		{"a proxy for all traffic", `"env":{"HTTPS_PROXY":"http://127.0.0.1:8080"}`, "env may not set HTTPS_PROXY"},
		{"a perl option that runs code", `"env":{"PERL5OPT":"-Mevil"}`, "env may not set PERL5OPT"},
		{"a JVM agent", `"env":{"JAVA_TOOL_OPTIONS":"-javaagent:/tmp/evil.jar"}`, "env may not set JAVA_TOOL_OPTIONS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newMCPHarness(t, reportingClaude(t), workspace, t.TempDir())
			res, err := h.client.Post(h.srv.URL+"/claude.run", "application/json",
				strings.NewReader(`{"workspace":"repo","prompt":"go",`+tc.body+`}`))
			if err != nil {
				t.Fatalf("POST /claude.run: %v", err)
			}
			defer func() { _ = res.Body.Close() }()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 — this is refused before the stream starts", res.StatusCode)
			}
			var body errorBody
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decoding the error: %v", err)
			}
			if body.Error.Code != codeBadRequest {
				t.Fatalf("code = %q, want %q", body.Error.Code, codeBadRequest)
			}
			if !strings.Contains(body.Error.Message, tc.want) {
				t.Fatalf("message = %q, want it to mention %q", body.Error.Message, tc.want)
			}
			// Every env refusal explains the rule it is applying and names a
			// value that WOULD be accepted. A refusal that only says no makes
			// the caller read this repository to find out what is allowed.
			if strings.Contains(tc.body, `"env"`) && strings.Contains(tc.want, "env may not set") {
				for _, explains := range []string{"version pins", "GOTOOLCHAIN"} {
					if !strings.Contains(body.Error.Message, explains) {
						t.Errorf("the refusal %q does not mention %q, so it does not say what IS allowed", body.Error.Message, explains)
					}
				}
			}
		})
	}
}

// The env parameter is an ALLOWLIST, and this is the case that made it one.
//
// Every name below passed the denylist that came before it, and every one of
// them decides what the session executes or where. They are not listed here to
// be fixed one by one — that is the trap the denylist was — but so that a
// future change back to "refuse the bad ones" fails immediately, with the
// evidence attached.
func TestEnvRefusesEveryNameThatDecidesWhatRuns(t *testing.T) {
	escaped := map[string]string{
		"GIT_EXEC_PATH":       "git runs its helpers from here and prepends it to PATH",
		"GIT_TEMPLATE_DIR":    "its hooks/ are copied into every repository git clones",
		"GIT_DIR":             "points git at a repository outside the workspace root",
		"GIT_WORK_TREE":       "the same, for the working copy",
		"GIT_INDEX_FILE":      "the same, for the index",
		"XDG_CONFIG_HOME":     "reinstates git/config — core.sshCommand, aliases, core.pager",
		"HTTPS_PROXY":         "repoints all traffic, including the model's",
		"HTTP_PROXY":          "the same",
		"ALL_PROXY":           "the same",
		"NODE_PATH":           "decides which module require() resolves",
		"NODE_EXTRA_CA_CERTS": "makes a proxy's certificate trusted",
		"PYTHONPATH":          "decides which module import executes",
		"PYTHONHOME":          "relocates the whole interpreter",
		"PERL5OPT":            "-Mevil runs code before the script",
		"PERL5LIB":            "decides which module use() resolves",
		"RUBYOPT":             "-rfoo requires a file",
		"RUBYLIB":             "the same, by path",
		"JAVA_TOOL_OPTIONS":   "-javaagent: loads a jar into every JVM",
		"_JAVA_OPTIONS":       "the same",
		"GOFLAGS":             "-toolexec= runs a program for every compile step",
		"CC":                  "cgo and every build system run what it names",
		"CXX":                 "the same",
		"LESSOPEN":            "|cmd %s runs a command through less",
		"BROWSER":             "names a command another tool will run",
		"MANPAGER":            "the same",
		"SSL_CERT_FILE":       "makes a proxy's certificate trusted",
		"TMPDIR":              "relocates where every tool writes its temporaries",
	}
	for name, why := range escaped {
		if _, err := checkEnv(map[string]string{name: "x"}); err == nil {
			t.Errorf("env accepted %s — %s", name, why)
		}
	}

	// And the ones that must still work, because they are what the parameter
	// was added for.
	for _, name := range []string{"GOTOOLCHAIN", "NODE_VERSION", "TT_TASK_ID"} {
		if _, err := checkEnv(map[string]string{name: "x"}); err != nil {
			t.Errorf("env refused %s, which is a version pin or this product's own: %s", name, err.Message)
		}
	}

	// The prefix is a prefix, not a name. `TT_` on its own carries no value.
	if _, err := checkEnv(map[string]string{"TT_": "x"}); err == nil {
		t.Error("env accepted the bare reserved prefix as a name")
	}

	// The refusal names the way out. A message that only says "no" sends
	// somebody to read the source to find out what is allowed.
	_, err := checkEnv(map[string]string{"LD_PRELOAD": "/tmp/evil.so"})
	if err == nil {
		t.Fatal("env accepted LD_PRELOAD")
	}
	for _, want := range []string{"GOTOOLCHAIN", "TT_", "version pins"} {
		if !strings.Contains(err.Message, want) {
			t.Errorf("the refusal %q does not mention %q", err.Message, want)
		}
	}
}

// The tool policy is split across the CLI's two flags, because the CLI's two
// flags mean two different things.
//
// Measured against claude 2.1.220, not read off `--help`: `--tools` filters the
// BUILT-IN surface and silently ignores an `mcp__` name, so sending MCP names
// to it was a no-op that read like a policy. The wire contract does not change
// — the cloud still sends one flat array, because which flag understands which
// name is a fact about the CLI.
func TestTheToolPolicyIsSplitAcrossTheCLIsTwoFlags(t *testing.T) {
	lines := runReporting(t, `{"id":"c-split","workspace":"repo","prompt":"go",`+
		`"tools":["Read","Bash","mcp__tasktrooper__update_criterion"]}`)
	argv := lineWithPrefix(t, lines, "argv:")

	if got := flagValue(t, argv, "--tools"); got != "Read,Bash" {
		t.Fatalf("--tools = %q, want only the built-in names", got)
	}
	if got := flagValue(t, argv, "--allowedTools"); got != "mcp__tasktrooper__update_criterion" {
		t.Fatalf("--allowedTools = %q, want the mcp__ names", got)
	}
}

// A policy naming only MCP tools must still say "no built-ins", and the way to
// say that is `--tools ""` — NOT omitting the flag.
//
// This is the accidental widening the parameter exists to prevent, arriving
// through the back door: a split that only emitted `--tools` when it had native
// names would hand an MCP-only policy the CLI's entire built-in surface, Bash
// included.
func TestAnMCPOnlyPolicyStillDisablesEveryBuiltInTool(t *testing.T) {
	lines := runReporting(t, `{"id":"c-mcponly","workspace":"repo","prompt":"go",`+
		`"tools":["mcp__tasktrooper__update_criterion"]}`)
	argv := lineWithPrefix(t, lines, "argv:")

	// The fake CLI echoes `$*`, where an empty argument collapses to nothing —
	// so the flag is asserted through claudeArgs, where the empty string is
	// visible as its own element.
	args, err := claudeArgs(claudeRunParams{Tools: []string{"mcp__tasktrooper__update_criterion"}})
	if err != nil {
		t.Fatalf("claudeArgs: %s", err.Message)
	}
	var sawEmptyTools bool
	for i, a := range args {
		if a == "--tools" && i+1 < len(args) && args[i+1] == "" {
			sawEmptyTools = true
		}
	}
	if !sawEmptyTools {
		t.Fatalf("argv %q does not carry an empty --tools; an MCP-only policy would get every built-in tool", args)
	}
	if !strings.Contains(argv, "--allowedTools mcp__tasktrooper__update_criterion") {
		t.Fatalf("argv %q does not carry the MCP half of the policy", argv)
	}
}

// checkEnv's own shape: sorted "NAME=value" pairs, so two identical requests
// produce an identical child and the order never depends on Go's map
// iteration.
func TestCheckEnvIsSortedAndInExecsOwnForm(t *testing.T) {
	got, err := checkEnv(map[string]string{"TT_ZULU": "z", "TT_ALPHA": "a", "TT_MIKE": "m"})
	if err != nil {
		t.Fatalf("checkEnv: %s", err.Message)
	}
	want := []string{"TT_ALPHA=a", "TT_MIKE=m", "TT_ZULU=z"}
	if len(got) != len(want) {
		t.Fatalf("checkEnv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("checkEnv = %v, want %v", got, want)
		}
	}

	// Absent is the normal case and is not an error.
	if got, err := checkEnv(nil); err != nil || got != nil {
		t.Fatalf("checkEnv(nil) = %v, %v; no env is a valid run", got, err)
	}
}
