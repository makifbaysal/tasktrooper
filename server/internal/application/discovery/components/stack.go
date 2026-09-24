package components

import (
	"path"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// languageByExt maps a file extension to the language name reported in both
// the repository-wide histogram and each component's own top-3 — the same
// judgment call as repofacts.languageByExt: extensions absent here count as
// files but never as a "language".
var languageByExt = map[string]string{
	".go": "Go", ".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript",
	".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".py": "Python", ".rb": "Ruby", ".rs": "Rust", ".java": "Java",
	".kt": "Kotlin", ".kts": "Kotlin", ".swift": "Swift", ".m": "Objective-C",
	".mm": "Objective-C", ".dart": "Dart", ".php": "PHP", ".cs": "C#",
	".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".hpp": "C++",
	".scala": "Scala", ".ex": "Elixir", ".exs": "Elixir", ".sh": "Shell",
	".bash": "Shell", ".zsh": "Shell", ".sql": "SQL", ".vue": "Vue",
	".svelte": "Svelte", ".tf": "Terraform", ".proto": "Protobuf",
}

func repoLanguages(tree *inventory.Tree) []domain.LanguageShare {
	files := map[string]int{}
	bytes := map[string]int64{}
	for _, f := range tree.Files {
		lang, ok := languageByExt[strings.ToLower(path.Ext(f))]
		if !ok {
			continue
		}
		files[lang]++
		bytes[lang] += tree.Size(f)
	}
	out := make([]domain.LanguageShare, 0, len(files))
	for l, n := range files {
		out = append(out, domain.LanguageShare{Language: l, Files: n, Bytes: bytes[l]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Files != out[j].Files {
			return out[i].Files > out[j].Files
		}
		return out[i].Language < out[j].Language
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func componentLanguages(tree *inventory.Tree, dir string) []domain.StackItem {
	files := map[string]int{}
	for _, f := range tree.Under(dir) {
		if lang, ok := languageByExt[strings.ToLower(path.Ext(f))]; ok {
			files[lang]++
		}
	}
	type kv struct {
		lang string
		n    int
	}
	ranked := make([]kv, 0, len(files))
	for l, n := range files {
		ranked = append(ranked, kv{l, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].lang < ranked[j].lang
	})
	if len(ranked) > 3 {
		ranked = ranked[:3]
	}
	out := make([]domain.StackItem, 0, len(ranked))
	for _, r := range ranked {
		out = append(out, domain.StackItem{Name: r.lang})
	}
	return out
}

// buildStack assembles the whole ComponentStack for one component: its
// language histogram (with a version when the tree pins one), runtime,
// curated frameworks/libraries, package manager and container base image.
func buildStack(tree *inventory.Tree, dir string, info *ManifestInfo) domain.ComponentStack {
	langs := componentLanguages(tree, dir)
	primary := primaryLanguage(info, langs)
	version := ""
	if info != nil {
		version = info.LanguageVersion
	}
	if version == "" && primary != "" {
		version = toolVersionsVersion(tree, dir, primary)
	}
	if primary != "" && version != "" {
		for i := range langs {
			if langs[i].Name == primary {
				langs[i].Version = version
			}
		}
	}

	stack := domain.ComponentStack{Languages: langs}
	if info != nil {
		stack.Frameworks = matchCurated(info, frameworkDisplayName, 0)
		stack.Libraries = matchCurated(info, libraryDisplayName, 8)
		if tree.Has(inventory.Join(dir, "sqlc.yaml")) || tree.Has(inventory.Join(dir, "sqlc.yml")) {
			stack.Libraries = append(stack.Libraries, domain.StackItem{Name: "sqlc"})
		}
	}
	if rt := runtimeFor(primary, version); rt != nil {
		stack.Runtime = rt
	}
	stack.PackageManager = packageManagerFor(tree, dir, info)
	stack.Container = containerFor(tree, dir)
	return stack
}

func primaryLanguage(info *ManifestInfo, langs []domain.StackItem) string {
	want := map[string]bool{}
	switch {
	case info == nil:
		return ""
	case info.Ecosystem == "node" || info.Ecosystem == "deno":
		want["TypeScript"], want["JavaScript"] = true, true
	case info.Ecosystem == "gradle" || info.Ecosystem == "maven":
		want["Kotlin"], want["Java"] = true, true
	default:
		return map[string]string{
			"go": "Go", "python": "Python", "rust": "Rust", "dart": "Dart",
			"swift": "Swift", "ruby": "Ruby", "php": "PHP", "dotnet": "C#",
		}[info.Ecosystem]
	}
	for _, l := range langs {
		if want[l.Name] {
			return l.Name
		}
	}
	for name := range want {
		return name
	}
	return ""
}

func runtimeFor(lang, version string) *domain.StackItem {
	name, ok := map[string]string{
		"TypeScript": "Node", "JavaScript": "Node", "Go": "Go", "Python": "Python",
		"Java": "JVM", "Kotlin": "JVM", "Rust": "Rust", "Dart": "Dart", "Swift": "Swift",
		"Ruby": "Ruby", "PHP": "PHP", "C#": ".NET",
	}[lang]
	if !ok {
		return nil
	}
	return &domain.StackItem{Name: name, Version: version}
}

var toolVersionsNames = map[string][]string{
	"Go": {"golang", "go"}, "TypeScript": {"nodejs", "node"}, "JavaScript": {"nodejs", "node"},
	"Python": {"python"}, "Rust": {"rust"}, "Dart": {"flutter", "dart"},
	"Ruby": {"ruby"}, "PHP": {"php"},
}

// toolVersionsVersion reads asdf-style .tool-versions (component dir, then
// repo root) and, for Node, the .nvmrc/.node-version convention — the
// sources the spec names for a language version no manifest itself pins.
func toolVersionsVersion(tree *inventory.Tree, dir, lang string) string {
	names := toolVersionsNames[lang]
	if len(names) > 0 {
		for _, d := range []string{dir, "."} {
			body := tree.ReadString(inventory.Join(d, ".tool-versions"))
			if body == "" {
				continue
			}
			for _, line := range strings.Split(body, "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				for _, n := range names {
					if fields[0] == n {
						return fields[1]
					}
				}
			}
			if d == "." {
				break
			}
		}
	}
	if lang == "TypeScript" || lang == "JavaScript" {
		for _, base := range []string{".nvmrc", ".node-version"} {
			if v := strings.TrimSpace(tree.ReadString(inventory.Join(dir, base))); v != "" {
				return strings.TrimPrefix(v, "v")
			}
		}
	}
	return ""
}

func matchCurated(info *ManifestInfo, lookup func(string) (string, bool), limit int) []domain.StackItem {
	seen := map[string]bool{}
	var out []domain.StackItem
	add := func(deps map[string]string) {
		keys := make([]string, 0, len(deps))
		for k := range deps {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			name, ok := lookup(k)
			if !ok || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, domain.StackItem{Name: name, Version: trimVersion(deps[k])})
		}
	}
	add(info.Dependencies)
	add(info.DevDependencies)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

var lockfileManagers = []struct{ file, manager string }{
	{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"},
	{"package-lock.json", "npm"},
	{"poetry.lock", "poetry"}, {"uv.lock", "uv"}, {"Pipfile.lock", "pipenv"},
	{"Cargo.lock", "cargo"}, {"Podfile.lock", "CocoaPods"}, {"Package.resolved", "SwiftPM"},
	{"Gemfile.lock", "bundler"}, {"composer.lock", "composer"},
}

// packageManagerFor prefers the lockfile that sits next to (or above) the
// component — the only source that cannot be wrong — and only falls back to
// the ecosystem's own convention when no lockfile was ever committed.
func packageManagerFor(tree *inventory.Tree, dir string, info *ManifestInfo) string {
	d := dir
	for {
		for _, lf := range lockfileManagers {
			if tree.Has(inventory.Join(d, lf.file)) {
				return lf.manager
			}
		}
		if d == "." {
			break
		}
		d = inventory.Dir(d)
	}
	if info != nil {
		switch info.Ecosystem {
		case "go":
			return "go modules"
		case "gradle":
			return "gradle"
		case "maven":
			return "maven"
		}
	}
	return ""
}

func containerFor(tree *inventory.Tree, dir string) string {
	body := tree.ReadString(inventory.Join(dir, "Dockerfile"))
	if body == "" {
		return ""
	}
	lastFrom := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 5 || strings.ToUpper(line[:5]) != "FROM " {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			lastFrom = fields[1]
		}
	}
	if lastFrom == "" {
		return "Docker"
	}
	return "Docker · " + lastFrom
}
