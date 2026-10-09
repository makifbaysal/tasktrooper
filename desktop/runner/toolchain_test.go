package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolchain.detect, against real checkouts on disk.
//
// The three things worth asserting are the three rules the method exists for,
// and each of them fails silently if it is got wrong:
//
//  1. **A constraint is not a pin.** ">=3.11" names a set of runtimes. Handing
//     it to `claude.run` as `PYTHON_VERSION` would look like a pin and select
//     nothing, so a range is reported and never becomes an environment value.
//  2. **Absence is absence.** A checkout that declares nothing answers with an
//     empty list, not with "system" or "latest" — a default invented here would
//     be passed on as though the repository had asked for it.
//  3. **`env` is passable as it stands.** Every name in it has to be one
//     `checkEnv` accepts, or the cloud builds a request `claude.run` refuses.

func checkoutWith(t *testing.T, files map[string]string) (config, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "repos", "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the checkout: %v", err)
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return config{workspaceDir: root}, "repos/acme"
}

func detect(t *testing.T, cfg config, rel string) toolchainResult {
	t.Helper()
	c := &call{ctx: t.Context(), id: "tc", cfg: cfg, params: json.RawMessage(`{"workspace":` + jsonString(rel) + `}`)}
	raw, err := detectToolchain(c)
	if err != nil {
		t.Fatalf("toolchain.detect: %s", err.Message)
	}
	return raw.(toolchainResult)
}

func jsonString(s string) string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// pinFor finds the first entry for a language, which is also the one `env`
// would have taken.
func pinFor(got toolchainResult, language string) (toolchainPin, bool) {
	for _, p := range got.Pins {
		if p.Language == language {
			return p, true
		}
	}
	return toolchainPin{}, false
}

func TestToolchainReadsWhatEachKindOfPinFileSays(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		".nvmrc":              "v20.11.0\n",
		".python-version":     "3.12.1\n",
		".ruby-version":       "3.2.2\n",
		".java-version":       "17.0.9\n",
		"go.mod":              "module example.com/x\n\ngo 1.24.0\n",
		"rust-toolchain.toml": "[toolchain]\nchannel = \"1.79.0\"\ncomponents = [\"clippy\"]\n",
		".fvmrc":              `{"flutter":"3.19.0"}`,
	})

	got := detect(t, cfg, rel)
	want := map[string]struct{ version, envName, envValue string }{
		"node":    {"20.11.0", "NODE_VERSION", "20.11.0"},
		"python":  {"3.12.1", "PYTHON_VERSION", "3.12.1"},
		"ruby":    {"3.2.2", "RUBY_VERSION", "3.2.2"},
		"java":    {"17.0.9", "JAVA_VERSION", "17.0.9"},
		"go":      {"1.24.0", "GOTOOLCHAIN", "go1.24.0"},
		"rust":    {"1.79.0", "RUSTUP_TOOLCHAIN", "1.79.0"},
		"flutter": {"3.19.0", "FLUTTER_VERSION", "3.19.0"},
	}
	for language, expected := range want {
		pin, found := pinFor(got, language)
		if !found {
			t.Fatalf("%s was not detected; pins were %+v", language, got.Pins)
		}
		if pin.Version != expected.version {
			t.Fatalf("%s version = %q, want %q", language, pin.Version, expected.version)
		}
		if !pin.Exact {
			t.Fatalf("%s %q was reported as a range", language, pin.Version)
		}
		if pin.EnvName != expected.envName || pin.EnvValue != expected.envValue {
			t.Fatalf("%s env = %s=%s, want %s=%s", language, pin.EnvName, pin.EnvValue, expected.envName, expected.envValue)
		}
		if got.Env[expected.envName] != expected.envValue {
			t.Fatalf("env[%s] = %q, want %q", expected.envName, got.Env[expected.envName], expected.envValue)
		}
	}
	// The `v` on an .nvmrc is nvm's prefix, and no other tool takes it.
	if pin, _ := pinFor(got, "node"); strings.HasPrefix(pin.Version, "v") {
		t.Fatalf("node version = %q, want nvm's `v` prefix removed", pin.Version)
	}
}

// GOTOOLCHAIN takes a toolchain NAME and go.mod states a language VERSION. The
// two are different things spelled almost the same, and getting it wrong
// produces `GOTOOLCHAIN=1.24.0`, which selects nothing.
func TestGoIsTranslatedIntoAToolchainNameAndTheToolchainLineWins(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		"go.mod": "module example.com/x\n\ngo 1.24\n\ntoolchain go1.24.3\n",
	})
	got := detect(t, cfg, rel)
	pin, found := pinFor(got, "go")
	if !found {
		t.Fatalf("go was not detected; pins were %+v", got.Pins)
	}
	if pin.Version != "go1.24.3" || pin.EnvValue != "go1.24.3" {
		t.Fatalf("go pin = %+v, want the `toolchain` line, which is already a toolchain name and is more precise than the `go` line", pin)
	}

	// Without a toolchain line, the two-component language version is padded to
	// the three a toolchain name has.
	cfg, rel = checkoutWith(t, map[string]string{"go.mod": "module example.com/x\n\ngo 1.24\n"})
	got = detect(t, cfg, rel)
	pin, _ = pinFor(got, "go")
	if pin.Version != "1.24" {
		t.Fatalf("version = %q, want what go.mod actually says", pin.Version)
	}
	if pin.EnvValue != "go1.24.0" {
		t.Fatalf("env_value = %q, want go1.24.0 — GOTOOLCHAIN has no two-component form", pin.EnvValue)
	}
}

// A range is reported and never resolved. Resolving one here would be this
// program deciding which Python a repository meant, minutes before a session
// silently built against it.
func TestAConstraintIsReportedAsARangeAndNeverBecomesAnEnvironmentValue(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		"package.json":   `{"name":"x","engines":{"node":">=20 <21"}}`,
		"pyproject.toml": "[project]\nname = \"x\"\nrequires-python = \">=3.11\"\n",
		"pubspec.yaml":   "name: x\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n  flutter: \">=3.19.0\"\n",
	})
	got := detect(t, cfg, rel)
	if len(got.Pins) == 0 {
		t.Fatal("nothing was detected from three manifests that all state a version")
	}
	for _, pin := range got.Pins {
		if pin.Exact {
			t.Fatalf("%+v was reported as an exact pin; it is a constraint", pin)
		}
		if pin.EnvValue != "" || pin.EnvName != "" {
			t.Fatalf("%+v became an environment value; a range passed as a version pin selects nothing", pin)
		}
	}
	if len(got.Env) != 0 {
		t.Fatalf("env = %v, want empty: not one of these files names a single runtime", got.Env)
	}
}

// A checkout that declares nothing says so. A default invented here would reach
// `claude.run` as though the repository had asked for it.
func TestACheckoutThatPinsNothingReportsNothing(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		"README.md": "# acme\n",
		"main.py":   "print('hello')\n",
		"index.js":  "console.log(1)\n",
	})
	got := detect(t, cfg, rel)
	if len(got.Pins) != 0 {
		t.Fatalf("pins = %+v, want none — file extensions say somebody wrote Python, not which Python", got.Pins)
	}
	if len(got.Env) != 0 {
		t.Fatalf("env = %v, want empty", got.Env)
	}
	if len(got.Read) != 0 {
		t.Fatalf("read = %v, want none — no pin file exists in this checkout", got.Read)
	}
	// Non-nil, so the JSON says "asked and found none" rather than "not asked".
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	for _, field := range []string{`"pins":[]`, `"env":{}`, `"read":[]`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("the answer does not carry %s: %s", field, encoded)
		}
	}
}

// "There is no pin file" and "the files are there and pin nothing this side
// recognises" are different problems with different answers, and `read` is what
// separates them.
func TestReadSeparatesNoPinFileFromAPinFileThatSaysNothing(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		"package.json": `{"name":"x","version":"1.0.0"}`,
	})
	got := detect(t, cfg, rel)
	if len(got.Pins) != 0 {
		t.Fatalf("pins = %+v, want none: this package.json has no engines", got.Pins)
	}
	if len(got.Read) != 1 || got.Read[0] != "package.json" {
		t.Fatalf("read = %v, want the file that was there and said nothing", got.Read)
	}
}

// Two files pinning one language both appear, in the order a resolver should
// believe them. Dropping one would hide a disagreement the repository actually
// contains.
func TestTwoFilesPinningOneLanguageBothAppearInPrecedenceOrder(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		".tool-versions": "nodejs 20.11.0\n",
		".nvmrc":         "18.19.0\n",
		"package.json":   `{"engines":{"node":">=18"}}`,
	})
	got := detect(t, cfg, rel)

	var sources []string
	for _, pin := range got.Pins {
		if pin.Language == "node" {
			sources = append(sources, pin.Source)
		}
	}
	want := []string{".tool-versions", ".nvmrc", "package.json"}
	if len(sources) != len(want) {
		t.Fatalf("node sources = %v, want all three: %v", sources, want)
	}
	for i := range want {
		if sources[i] != want[i] {
			t.Fatalf("node source %d = %q, want %q — the version manager's own file resolves first", i, sources[i], want[i])
		}
	}
	// `env` takes the first, which is what the version manager would have used.
	if got.Env["NODE_VERSION"] != "20.11.0" {
		t.Fatalf("env[NODE_VERSION] = %q, want the .tool-versions answer", got.Env["NODE_VERSION"])
	}
}

func TestToolVersionsAndMiseAreReadWithTheirOwnConventions(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		// A comment, a fallback version, and a name only asdf uses.
		".tool-versions": "# managed by asdf\nnodejs 20.11.0 18.19.0\ngolang 1.24.0\nterraform 1.7.5\n",
	})
	got := detect(t, cfg, rel)

	if pin, _ := pinFor(got, "node"); pin.Version != "20.11.0" {
		t.Fatalf("node = %q, want the first version on the line (the rest are fallbacks)", pin.Version)
	}
	if pin, found := pinFor(got, "go"); !found || pin.EnvValue != "go1.24.0" {
		t.Fatalf("golang = %+v, want it normalised to `go` with a toolchain name", pin)
	}
	// A tool with no allowlisted name is REPORTED and cannot be passed through.
	// Dropping it would hide something the repository declared; inventing a name
	// for it would produce an `env` that `claude.run` refuses.
	pin, found := pinFor(got, "terraform")
	if !found {
		t.Fatalf("terraform was dropped; the file declares it: %+v", got.Pins)
	}
	if pin.EnvName != "" {
		t.Fatalf("terraform got env name %q; there is none on claude.run's allowlist", pin.EnvName)
	}

	cfg, rel = checkoutWith(t, map[string]string{
		"mise.toml": "[env]\nnode = \"999\"\n\n[tools]\nnode = [\"20.11.0\", \"18\"]\npython = \"3.12.1\"\n",
	})
	got = detect(t, cfg, rel)
	if pin, _ := pinFor(got, "node"); pin.Version != "20.11.0" {
		// A line scan that was not table-aware would read the `[env]` node
		// first and pin the wrong thing.
		t.Fatalf("node = %q, want the [tools] entry and not the [env] one", pin.Version)
	}
	if pin, _ := pinFor(got, "python"); pin.Version != "3.12.1" {
		t.Fatalf("python = %q, want 3.12.1", pin.Version)
	}
}

// rustup's channels are pins in rustup's own terms even though they are not
// numbers, and RUSTUP_TOOLCHAIN is the name rustup reads.
func TestARustChannelIsAnExactPin(t *testing.T) {
	for _, channel := range []string{"stable", "nightly-2024-03-01"} {
		cfg, rel := checkoutWith(t, map[string]string{"rust-toolchain": channel + "\n"})
		got := detect(t, cfg, rel)
		pin, found := pinFor(got, "rust")
		if !found || !pin.Exact {
			t.Fatalf("%q = %+v, want an exact pin", channel, pin)
		}
		if got.Env["RUSTUP_TOOLCHAIN"] != channel {
			t.Fatalf("env = %v, want RUSTUP_TOOLCHAIN=%s", got.Env, channel)
		}
	}
}

// Every name this method puts in `env` has to be one `claude.run` accepts. The
// two live in different files, and a name added to one and not the other is a
// request the cloud builds and this runner then refuses.
func TestEveryNameToolchainDetectEmitsIsOneClaudeRunAccepts(t *testing.T) {
	for language, name := range toolchainEnvName {
		if !envNameAllowed(name) {
			t.Fatalf("toolchain.detect maps %s onto %s, which claude.run's env allowlist refuses; "+
				"the cloud would build a request this runner rejects", language, name)
		}
	}
	// And the whole answer is passable as it stands, checked through the same
	// function `claude.run` uses rather than by restating the rule.
	cfg, rel := checkoutWith(t, map[string]string{
		".tool-versions": "nodejs 20.11.0\npython 3.12.1\nruby 3.2.2\njava 17.0.9\nrust 1.79.0\nflutter 3.19.0\n",
		"go.mod":         "module x\n\ngo 1.24.0\n",
	})
	got := detect(t, cfg, rel)
	if len(got.Env) != 7 {
		t.Fatalf("env = %v, want one entry per language", got.Env)
	}
	if _, err := checkEnv(got.Env); err != nil {
		t.Fatalf("claude.run refuses the env this method produced: %s", err.Message)
	}
}

// The only caller-supplied value is the path, and it goes through the same gate
// every other path does.
func TestToolchainDetectRefusesAWorkspaceOutsideTheRoot(t *testing.T) {
	cfg, _ := checkoutWith(t, map[string]string{".nvmrc": "20\n"})
	for _, workspace := range []string{"../..", "/etc", "repos/../../secrets", "-rf", ""} {
		c := &call{ctx: t.Context(), id: "tc", cfg: cfg, params: json.RawMessage(`{"workspace":` + jsonString(workspace) + `}`)}
		if _, err := detectToolchain(c); err == nil {
			t.Fatalf("toolchain.detect accepted %q", workspace)
		} else if err.Code != codeBadRequest {
			t.Fatalf("code = %q for %q, want %q", err.Code, workspace, codeBadRequest)
		}
	}
	// A checkout that is not there is refused rather than answered "pins
	// nothing", which would be a lie with the right shape.
	c := &call{ctx: t.Context(), id: "tc", cfg: cfg, params: json.RawMessage(`{"workspace":"repos/never-cloned"}`)}
	if _, err := detectToolchain(c); err == nil {
		t.Fatal("toolchain.detect answered for a checkout that does not exist")
	}
}

// A pin file that is a symlink is skipped. The names are this program's own, so
// a link at one of them was put there by whatever is in the checkout, and
// following it reads a file outside the workspace through a name that looks
// like it is inside.
func TestASymlinkedPinFileIsNotFollowed(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{"README.md": "x\n"})
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(outside, []byte("20.11.0\n"), 0o644); err != nil {
		t.Fatalf("writing the outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(cfg.workspaceDir, rel, ".nvmrc")); err != nil {
		t.Fatalf("linking: %v", err)
	}

	got := detect(t, cfg, rel)
	if len(got.Pins) != 0 {
		t.Fatalf("pins = %+v, want none: the .nvmrc is a link to a file outside the workspace", got.Pins)
	}
	if len(got.Read) != 0 {
		t.Fatalf("read = %v, want none", got.Read)
	}
}

// Nothing below the named directory is read. A monorepo pins per package, and
// walking to find those would mean choosing which of several answers is the
// repository's — so a caller that wants a package's pins names that package.
func TestOnlyTheNamedDirectoryIsRead(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		"web/.nvmrc":     "20.11.0\n",
		"api/go.mod":     "module x\n\ngo 1.24.0\n",
		".tool-versions": "python 3.12.1\n",
	})
	got := detect(t, cfg, rel)
	if len(got.Pins) != 1 || got.Pins[0].Language != "python" {
		t.Fatalf("pins = %+v, want only the root's own", got.Pins)
	}

	// Naming the package gets the package's answer, with no new mechanism.
	deeper := detect(t, cfg, rel+"/web")
	if len(deeper.Pins) != 1 || deeper.Pins[0].Language != "node" {
		t.Fatalf("pins for web = %+v, want the node pin", deeper.Pins)
	}
}

func TestAGemfileAndAnSDKMANFileAreRead(t *testing.T) {
	cfg, rel := checkoutWith(t, map[string]string{
		"Gemfile":   "source 'https://rubygems.org'\nruby \"3.2.2\"\n\ngem 'rails'\n",
		".sdkmanrc": "# sdkman\njava=17.0.9-tem\nmaven=3.9.6\n",
	})
	got := detect(t, cfg, rel)
	if pin, found := pinFor(got, "ruby"); !found || pin.Version != "3.2.2" {
		t.Fatalf("ruby = %+v, want the Gemfile's declaration", pin)
	}
	if pin, found := pinFor(got, "java"); !found || pin.Version != "17.0.9-tem" || pin.EnvValue != "17.0.9-tem" {
		t.Fatalf("java = %+v, want the vendor suffix kept — it is part of the version sdkman resolves", pin)
	}
}
