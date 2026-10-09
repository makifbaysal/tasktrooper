package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rs/zerolog/log"
)

// toolchain.detect — what a prepared checkout says it needs.
//
// The cloud cannot answer this. It resolves a repository's toolchain overlay
// against a path that exists on this Mac and not in a pod, so `Detect()` there
// reads nothing and the only variable that survives is `GOTOOLCHAIN` — which is
// a directive rather than a location, and therefore the only one that could
// survive a portability check. Real detection has to run where the checkout is,
// which is here.
//
// Three rules shape everything below, and each of them is a rule against a
// tempting shortcut:
//
//  1. **Read what the repository DECLARES, never infer from what it contains.**
//     A directory full of `.py` says somebody wrote Python; it does not say
//     which Python, and a version this side invented is worse than no version
//     at all — it would be passed to `claude.run` as a pin and silently build
//     against the wrong runtime. Only files whose PURPOSE is to state a version
//     are read.
//  2. **Absence is absence.** A language with no pin file does not appear in the
//     answer. It is not reported as "default", "system" or "latest", because
//     none of those is something the checkout said.
//  3. **Nothing is installed, and nothing can be.** This method reads files. It
//     runs no version manager, downloads nothing, and changes nothing on disk.
//
// It also spawns nothing at all, which is why there is no argv gate in this
// file: the only caller-supplied value is the workspace, and that goes through
// `resolveInWorkspace` like every other path a caller names. Every FILE name
// here is this program's own constant.

// pinFileLimit bounds one pin file. These are lines, not documents — the
// largest of them is a package.json — and a limit is what stops a file in the
// workspace from being an allocation primitive.
const pinFileLimit = 1024 * 1024

// pinValueLimit bounds one version string. A pin is characters, not kilobytes.
const pinValueLimit = 128

// maxToolchainPins bounds the answer. `.tool-versions` may name any tool, so
// the count is a repository's to choose and therefore a count this side bounds.
const maxToolchainPins = 128

// exactVersion is what may become an environment value.
//
// A pin like "20.11.0" or "17.0.9-tem" names one runtime. A constraint like
// ">=3.11", "^20" or "lts/hydrogen" names a set, and passing a set where a
// version is expected is a guess dressed as a fact — so those are reported with
// `exact:false` and no `env_value`, and the cloud decides what to do with a
// range rather than being handed a resolution nobody made.
var exactVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([.-][A-Za-z0-9][A-Za-z0-9._+-]*)?$`)

// rustChannel is the other shape of an exact answer. `stable`, `beta`,
// `nightly` and a dated nightly are what rustup takes, so they are pins in
// rustup's own terms even though they are not numbers.
var rustChannel = regexp.MustCompile(`^(stable|beta|nightly)(-[0-9]{4}-[0-9]{2}-[0-9]{2})?$`)

// toolchainEnvName maps a language onto the name `claude.run`'s `env`
// allowlist accepts for it, so the cloud never needs a mapping table of its
// own — and cannot invent one that disagrees with `checkEnv`.
//
// A language with no entry is reported without an `env_name`, which is the
// honest statement "this pin exists and cannot be passed through". Adding an
// entry here without adding the same name to `envAllowed` would produce an
// `env` object `claude.run` then refuses, so the two are checked against each
// other in the tests.
var toolchainEnvName = map[string]string{
	"go":     "GOTOOLCHAIN",
	"node":   "NODE_VERSION",
	"python": "PYTHON_VERSION",
	"ruby":   "RUBY_VERSION",
	"java":   "JAVA_VERSION",
	// RUSTUP_TOOLCHAIN rather than RUST_TOOLCHAIN: both are on the allowlist and
	// only this one is read by rustup, which is what a session actually runs.
	"rust":    "RUSTUP_TOOLCHAIN",
	"flutter": "FLUTTER_VERSION",
}

// toolAliases normalise the names version managers use for the same runtime.
// `.tool-versions` says `nodejs`, mise says `node`, and a caller that had to
// know which is which would be holding this table instead.
var toolAliases = map[string]string{
	"nodejs": "node",
	"golang": "go",
	"rustc":  "rust",
	"jdk":    "java",
}

type toolchainParams struct {
	// Workspace is a path relative to the workspace folder — the checkout a
	// `workspace.prepare` produced. It must already exist: there is nothing to
	// read in a directory that is not there, and reporting "pins nothing" for a
	// checkout that was never made would be a lie with the right shape.
	Workspace string `json:"workspace"`
}

// toolchainPin is one declaration, with the file that made it.
type toolchainPin struct {
	// Language is normalised and lowercase: go, node, python, ruby, java, rust,
	// flutter, dart — or whatever a `.tool-versions` line named, unchanged.
	Language string `json:"language"`
	// Version is what the file says, verbatim. A range stays a range.
	Version string `json:"version"`
	// Exact is false for a constraint. It is the field that says whether
	// `Version` names one runtime or a set of them.
	Exact bool `json:"exact"`
	// Source is the file this came from, relative to the workspace.
	Source string `json:"source"`
	// EnvName and EnvValue are the pin in the form `claude.run` takes, present
	// only for an exact pin whose language has an allowlisted name. EnvValue is
	// not always EnvVersion: GOTOOLCHAIN takes `go1.24.0`, not `1.24.0`.
	EnvName  string `json:"env_name,omitempty"`
	EnvValue string `json:"env_value,omitempty"`
}

type toolchainResult struct {
	V         int    `json:"v"`
	Workspace string `json:"workspace"`
	Path      string `json:"path"`
	// Pins is every declaration found, in precedence order per language — a
	// version manager's own file first, then the language's dotfile, then a
	// manifest's constraint. Two files pinning one language BOTH appear: this
	// side reports the disagreement rather than resolving it silently, and the
	// order says which one a resolver should believe.
	Pins []toolchainPin `json:"pins"`
	// Env is the exact pins, one per language, ready to pass to `claude.run` as
	// its `env`. Built from the first exact pin per language in `Pins`, so it is
	// a projection of the list above rather than a second opinion about it.
	Env map[string]string `json:"env"`
	// Read is the pin files that existed and were read. It is what makes an
	// empty answer readable: no files read means the checkout declares nothing,
	// and files read with no pins means they exist and say nothing this side
	// recognises. Those are different problems.
	Read []string `json:"read"`
}

// detectToolchain reads the checkout and reports what it pins.
func detectToolchain(c *call) (any, *rpcError) {
	var p toolchainParams
	if err := json.Unmarshal(c.params, &p); err != nil {
		return nil, failure(codeBadRequest, "toolchain.detect params are not the expected object: %v", err)
	}
	rel := strings.TrimSpace(p.Workspace)
	if rel == "" {
		return nil, failure(codeBadRequest, "workspace is required: it names the checkout to inspect, relative to the workspace folder")
	}
	dir, err := resolveInWorkspace(c.cfg.workspaceDir, rel)
	if err != nil {
		return nil, failure(codeBadRequest, "workspace: %v", err)
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return nil, failure(codeBadRequest, "workspace %q is not a directory on this machine; call workspace.prepare first", rel)
	}

	// Only the directory named, never below it. A monorepo pins per package,
	// and walking to find those would mean guessing which of several answers is
	// the repository's — so a caller that wants a package's pins names that
	// package as the workspace. `workspace.prepare`'s `rel` and this one are
	// the same kind of path for the same reason.
	// Both start empty rather than nil, so the answer says "this checkout was
	// read and declares nothing" instead of encoding a null, which reads as
	// "this side did not look".
	r := &pinReader{dir: dir, read: []string{}}
	pins := []toolchainPin{}
	for _, source := range pinSources {
		pins = append(pins, source(r)...)
		if len(pins) > maxToolchainPins {
			pins = pins[:maxToolchainPins]
			break
		}
	}

	env := map[string]string{}
	for _, pin := range pins {
		if pin.EnvName == "" || pin.EnvValue == "" {
			continue
		}
		// First wins: `pinSources` is in precedence order, so the version
		// manager's own answer beats a manifest's constraint rather than the
		// other way round because of map ordering.
		if _, taken := env[pin.EnvName]; !taken {
			env[pin.EnvName] = pin.EnvValue
		}
	}

	log.Info().Str("call", c.id).Str("dir", rel).Int("pins", len(pins)).Int("files", len(r.read)).
		Msg("toolchain detected")
	return toolchainResult{
		V:         protocolVersion,
		Workspace: filepath.ToSlash(rel),
		Path:      dir,
		Pins:      pins,
		Env:       env,
		Read:      r.read,
	}, nil
}

// pinReader reads the files and remembers which ones existed.
type pinReader struct {
	dir  string
	read []string
}

// text reads one pin file, or reports that it is not there.
//
// A symlink is skipped rather than followed. The names here are this program's
// own, so a link at one of them was put there by whatever is in the checkout,
// and following it would read a file outside the workspace through a name that
// looks like it is inside — which is `resolveInWorkspace`'s rule arriving by
// another route.
func (r *pinReader) text(name string) (string, bool) {
	path := filepath.Join(r.dir, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > pinFileLimit {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	r.read = append(r.read, filepath.ToSlash(name))
	return string(raw), true
}

// pin builds one entry, deciding for itself whether the version is exact and
// whether it can be passed through as an environment value.
func pin(language, version, source string) []toolchainPin {
	language = strings.ToLower(strings.TrimSpace(language))
	if alias, ok := toolAliases[language]; ok {
		language = alias
	}
	version = strings.TrimSpace(version)
	if language == "" || version == "" || len(version) > pinValueLimit {
		return nil
	}

	exact := exactVersion.MatchString(version) || (language == "rust" && rustChannel.MatchString(version))
	out := toolchainPin{Language: language, Version: version, Exact: exact, Source: source}
	if exact {
		if name, ok := toolchainEnvName[language]; ok {
			out.EnvName = name
			out.EnvValue = envValueFor(language, version)
		}
	}
	return []toolchainPin{out}
}

// envValueFor is the one place a pin is translated rather than copied, and Go
// is the only language that needs it.
//
// `GOTOOLCHAIN` takes a TOOLCHAIN NAME (`go1.24.0`), and go.mod states a
// LANGUAGE VERSION (`1.24.0`) — the two are different things spelled almost the
// same. A two-component version is padded because a toolchain name has three:
// `go 1.24` in a go.mod means "at least 1.24", and `go1.24.0` is the toolchain
// that satisfies it. The `toolchain` directive, when a go.mod has one, is
// already a toolchain name and is used verbatim — see goPins.
func envValueFor(language, version string) string {
	if language != "go" {
		return version
	}
	if strings.HasPrefix(version, "go") {
		return version
	}
	if strings.Count(version, ".") == 1 {
		version += ".0"
	}
	return "go" + version
}

// pinSources is every reader, IN PRECEDENCE ORDER. A version manager's own file
// comes before a language's dotfile, which comes before a manifest's
// constraint: that is the order the tools themselves resolve in, and it is the
// order `Env` takes its first-wins answer from.
var pinSources = []func(*pinReader) []toolchainPin{
	toolVersionsPins,
	misePins,
	goPins,
	nodePins,
	pythonPins,
	rubyPins,
	javaPins,
	rustPins,
	flutterPins,
	pubspecPins,
	packageJSONPins,
	pyprojectPins,
	gemfilePins,
}

// toolVersionsPins reads asdf's and mise's shared format: one `tool version`
// per line, extra versions on a line being fallbacks the first of which wins.
func toolVersionsPins(r *pinReader) []toolchainPin {
	text, ok := r.text(".tool-versions")
	if !ok {
		return nil
	}
	var out []toolchainPin
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, pin(fields[0], fields[1], ".tool-versions")...)
	}
	return out
}

// misePins reads the `[tools]` table of a mise configuration.
//
// Read with a table-aware line scan rather than a TOML parser. Three keys in
// three files do not justify a dependency inside a binary that ships to users,
// and the scan is table-aware precisely so a `node = "20"` under some OTHER
// table is not read as a tool pin — which is the failure a naive line match
// would have.
func misePins(r *pinReader) []toolchainPin {
	for _, name := range []string{"mise.toml", ".mise.toml"} {
		text, ok := r.text(name)
		if !ok {
			continue
		}
		var out []toolchainPin
		for _, entry := range tomlTable(text, "tools") {
			out = append(out, pin(entry.key, firstTOMLValue(entry.value), name)...)
		}
		return out
	}
	return nil
}

// goPins reads go.mod's two directives. The `toolchain` line is already a
// toolchain name and wins; the `go` line is a language version and is the
// fallback.
func goPins(r *pinReader) []toolchainPin {
	text, ok := r.text("go.mod")
	if !ok {
		return nil
	}
	var toolchain, language string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "toolchain":
			toolchain = fields[1]
		case "go":
			language = fields[1]
		}
	}
	if toolchain != "" {
		// Already `go1.24.3`. Its own shape is not exactVersion's, so the entry
		// is built here rather than through pin(): a toolchain name IS exact,
		// and refusing it for having a `go` on the front would drop the most
		// precise statement a go.mod can make.
		if strings.HasPrefix(toolchain, "go") && exactVersion.MatchString(strings.TrimPrefix(toolchain, "go")) {
			return []toolchainPin{{
				Language: "go", Version: toolchain, Exact: true, Source: "go.mod",
				EnvName: toolchainEnvName["go"], EnvValue: toolchain,
			}}
		}
	}
	return pin("go", language, "go.mod")
}

func nodePins(r *pinReader) []toolchainPin {
	var out []toolchainPin
	for _, name := range []string{".nvmrc", ".node-version"} {
		if text, ok := r.text(name); ok {
			out = append(out, pin("node", firstLine(text), name)...)
		}
	}
	return out
}

func pythonPins(r *pinReader) []toolchainPin {
	if text, ok := r.text(".python-version"); ok {
		return pin("python", firstLine(text), ".python-version")
	}
	return nil
}

func rubyPins(r *pinReader) []toolchainPin {
	if text, ok := r.text(".ruby-version"); ok {
		return pin("ruby", firstLine(text), ".ruby-version")
	}
	return nil
}

func javaPins(r *pinReader) []toolchainPin {
	var out []toolchainPin
	if text, ok := r.text(".java-version"); ok {
		out = append(out, pin("java", firstLine(text), ".java-version")...)
	}
	// SDKMAN's file is `key=value` lines, of which `java` is the one this
	// answers for.
	if text, ok := r.text(".sdkmanrc"); ok {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			key, value, found := strings.Cut(line, "=")
			if found && strings.TrimSpace(key) == "java" {
				out = append(out, pin("java", value, ".sdkmanrc")...)
			}
		}
	}
	return out
}

func rustPins(r *pinReader) []toolchainPin {
	if text, ok := r.text("rust-toolchain.toml"); ok {
		for _, entry := range tomlTable(text, "toolchain") {
			if entry.key == "channel" {
				return pin("rust", firstTOMLValue(entry.value), "rust-toolchain.toml")
			}
		}
		return nil
	}
	// The legacy form is the bare channel on its own line.
	if text, ok := r.text("rust-toolchain"); ok {
		return pin("rust", firstLine(text), "rust-toolchain")
	}
	return nil
}

func flutterPins(r *pinReader) []toolchainPin {
	// fvm's newer file, then the one it used to write. Both are JSON and both
	// hold one version under a name of their own.
	if text, ok := r.text(".fvmrc"); ok {
		var doc struct {
			Flutter string `json:"flutter"`
		}
		if json.Unmarshal([]byte(text), &doc) == nil {
			return pin("flutter", doc.Flutter, ".fvmrc")
		}
		return nil
	}
	if text, ok := r.text(filepath.Join(".fvm", "fvm_config.json")); ok {
		var doc struct {
			SDKVersion string `json:"flutterSdkVersion"`
		}
		if json.Unmarshal([]byte(text), &doc) == nil {
			return pin("flutter", doc.SDKVersion, ".fvm/fvm_config.json")
		}
	}
	return nil
}

// pubspecPins reads a Flutter or Dart package's `environment` block, which is
// where both SDK constraints live. They are constraints — ">=3.0.0 <4.0.0" —
// so they are reported as ranges and never become an environment value.
func pubspecPins(r *pinReader) []toolchainPin {
	text, ok := r.text("pubspec.yaml")
	if !ok {
		return nil
	}
	var out []toolchainPin
	inEnvironment := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			// A top-level key ends the block, which is what makes this read the
			// `sdk:` under `environment:` and not the one under `dependencies:`.
			inEnvironment = strings.HasPrefix(strings.TrimSpace(line), "environment:")
			continue
		}
		if !inEnvironment {
			continue
		}
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if !found {
			continue
		}
		version := strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "sdk":
			out = append(out, pin("dart", version, "pubspec.yaml")...)
		case "flutter":
			out = append(out, pin("flutter", version, "pubspec.yaml")...)
		}
	}
	return out
}

// packageJSONPins reads `engines`, which is a CONSTRAINT and almost never a
// pin. It is read anyway because ">=20" is still what the repository said, and
// a cloud deciding what a session needs would rather know than guess — it just
// arrives as `exact:false` with no environment value.
func packageJSONPins(r *pinReader) []toolchainPin {
	text, ok := r.text("package.json")
	if !ok {
		return nil
	}
	var doc struct {
		Engines map[string]string `json:"engines"`
	}
	if json.Unmarshal([]byte(text), &doc) != nil {
		return nil
	}
	var out []toolchainPin
	// Only the runtime. `npm`, `pnpm` and `yarn` in `engines` are package
	// managers rather than language runtimes, and this method answers for
	// runtimes — a wider answer would need names `claude.run` cannot accept
	// anyway.
	if version, has := doc.Engines["node"]; has {
		out = append(out, pin("node", version, "package.json")...)
	}
	return out
}

// pyprojectPins reads the two places a Python version is stated. Both are
// constraints by convention (">=3.11", "^3.11"), so both are reported as
// ranges.
func pyprojectPins(r *pinReader) []toolchainPin {
	text, ok := r.text("pyproject.toml")
	if !ok {
		return nil
	}
	for _, entry := range tomlTable(text, "project") {
		if entry.key == "requires-python" {
			return pin("python", firstTOMLValue(entry.value), "pyproject.toml")
		}
	}
	for _, entry := range tomlTable(text, "tool.poetry.dependencies") {
		if entry.key == "python" {
			return pin("python", firstTOMLValue(entry.value), "pyproject.toml")
		}
	}
	return nil
}

var gemfileRuby = regexp.MustCompile(`(?m)^\s*ruby\s+["']([^"']+)["']`)

func gemfilePins(r *pinReader) []toolchainPin {
	text, ok := r.text("Gemfile")
	if !ok {
		return nil
	}
	if m := gemfileRuby.FindStringSubmatch(text); m != nil {
		return pin("ruby", m[1], "Gemfile")
	}
	return nil
}

// --- the two small parsers --------------------------------------------------

type tomlEntry struct{ key, value string }

// tomlTable returns the `key = value` lines inside one table, and only that
// table.
//
// Not a TOML parser and not pretending to be one: it understands table headers,
// `key = value` lines and comments, which is the whole of what these three
// files use for the four keys read here. Anything more structured — an inline
// table, a multi-line array — falls out as a value `firstTOMLValue` does not
// recognise, and an unrecognised value produces no pin rather than a wrong one.
func tomlTable(text, want string) []tomlEntry {
	var out []tomlEntry
	current := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = strings.Trim(line, "[]")
			current = strings.TrimSpace(current)
			continue
		}
		if current != want {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if i := strings.Index(value, "#"); i >= 0 && !strings.Contains(value[:i], `"`) {
			value = value[:i]
		}
		out = append(out, tomlEntry{key: strings.Trim(strings.TrimSpace(key), `"'`), value: strings.TrimSpace(value)})
	}
	return out
}

// firstTOMLValue unwraps a quoted string, or the first element of an array of
// them — mise writes `node = ["20", "18"]` for a primary version and its
// fallbacks, and the first is the one that would be used.
func firstTOMLValue(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") {
		value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		if comma := strings.Index(value, ","); comma >= 0 {
			value = value[:comma]
		}
		value = strings.TrimSpace(value)
	}
	return strings.Trim(value, `"'`)
}

// firstLine is a whole-file pin file's content: `.nvmrc` and its siblings hold
// one version and, often, a trailing newline somebody's editor added.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// `.nvmrc` is sometimes written `v20.11.0`. The `v` is nvm's prefix and
		// not part of the version any other tool would accept.
		return strings.TrimPrefix(trimmed, "v")
	}
	return ""
}
