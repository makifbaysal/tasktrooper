package components

import (
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"gopkg.in/yaml.v3"
)

// buildCommands assembles one DetectedCommand per purpose, scripts beating
// Makefile beating Taskfile/justfile beating the ecosystem's own default —
// the first source to answer a purpose wins, and later sources only fill
// what is still missing.
func buildCommands(tree *inventory.Tree, dir string, info *ManifestInfo) []domain.DetectedCommand {
	byPurpose := map[domain.CommandPurpose]domain.DetectedCommand{}
	apply := func(cmds []domain.DetectedCommand) {
		for _, c := range cmds {
			if _, exists := byPurpose[c.Purpose]; exists {
				continue
			}
			byPurpose[c.Purpose] = c
		}
	}

	if info != nil && info.Ecosystem == "node" {
		apply(nodeScriptCommands(tree, dir, info))
	}
	apply(makefileCommands(tree, dir))
	apply(taskfileCommands(tree, dir))
	apply(justfileCommands(tree, dir))
	apply(ecosystemDefaults(tree, dir, info))

	out := make([]domain.DetectedCommand, 0, len(byPurpose))
	for _, c := range byPurpose {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Purpose < out[j].Purpose })
	return out
}

var nodeScriptPurposes = map[string]domain.CommandPurpose{
	"build": domain.CommandBuild,
	"test":  domain.CommandTest, "test:unit": domain.CommandTest,
	"lint":      domain.CommandLint,
	"typecheck": domain.CommandTypecheck, "type-check": domain.CommandTypecheck,
	"check-types": domain.CommandTypecheck, "tsc": domain.CommandTypecheck,
	"format": domain.CommandFormat, "fmt": domain.CommandFormat,
	"e2e": domain.CommandE2E, "test:e2e": domain.CommandE2E,
	"migrate": domain.CommandMigrate, "db:migrate": domain.CommandMigrate,
}

func nodeScriptCommands(tree *inventory.Tree, dir string, info *ManifestInfo) []domain.DetectedCommand {
	manager := packageManagerFor(tree, dir, info)
	if manager == "" {
		manager = "npm"
	}
	manifestPath := inventory.Join(dir, "package.json")
	body := tree.ReadString(manifestPath)

	var out []domain.DetectedCommand
	addScript := func(purpose domain.CommandPurpose, name string) {
		out = append(out, domain.DetectedCommand{
			Purpose: purpose,
			Command: renderScript(manager, name),
			Source:  domain.SourceEvidence{Path: manifestPath, Line: scriptLine(body, name)},
		})
	}

	names := make([]string, 0, len(info.Scripts))
	for n := range info.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	seen := map[domain.CommandPurpose]bool{}
	for _, name := range names {
		purpose, ok := nodeScriptPurposes[name]
		if !ok || seen[purpose] {
			continue
		}
		seen[purpose] = true
		addScript(purpose, name)
	}
	for _, name := range []string{"dev", "start:dev", "serve", "start"} {
		if _, ok := info.Scripts[name]; ok {
			addScript(domain.CommandDev, name)
			break
		}
	}

	out = append(out, installCommand(tree, dir, manager))
	return out
}

func renderScript(manager, name string) string {
	switch manager {
	case "pnpm":
		return "pnpm " + name
	case "yarn":
		return "yarn " + name
	case "bun":
		return "bun run " + name
	default:
		if name == "test" {
			return "npm test"
		}
		return "npm run " + name
	}
}

func scriptLine(packageJSON, name string) int {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(name) + `"\s*:`)
	loc := re.FindStringIndex(packageJSON)
	if loc == nil {
		return 0
	}
	return lineOf(packageJSON, loc[0])
}

var lockfileForManager = map[string]string{"pnpm": "pnpm-lock.yaml", "yarn": "yarn.lock", "npm": "package-lock.json", "bun": "bun.lock"}
var installForManager = map[string]string{
	"pnpm": "pnpm install --frozen-lockfile", "yarn": "yarn install --frozen-lockfile",
	"npm": "npm ci", "bun": "bun install",
}

func installCommand(tree *inventory.Tree, dir, manager string) domain.DetectedCommand {
	source := inventory.Join(dir, "package.json")
	if lockName := lockfileForManager[manager]; lockName != "" {
		for d := dir; ; d = inventory.Dir(d) {
			if tree.Has(inventory.Join(d, lockName)) {
				source = inventory.Join(d, lockName)
				break
			}
			if d == "." {
				break
			}
		}
	}
	return domain.DetectedCommand{Purpose: domain.CommandInstall, Command: installForManager[manager], Source: domain.SourceEvidence{Path: source}}
}

var makeTargetRe = regexp.MustCompile(`(?m)^([a-zA-Z][a-zA-Z0-9_./-]*):(?:[^=]|$)`)

func makefileCommands(tree *inventory.Tree, dir string) []domain.DetectedCommand {
	path := inventory.Join(dir, "Makefile")
	body := tree.ReadString(path)
	if body == "" {
		return nil
	}
	var out []domain.DetectedCommand
	seen := map[string]bool{}
	for _, m := range makeTargetRe.FindAllStringSubmatchIndex(body, -1) {
		target := body[m[2]:m[3]]
		if seen[target] {
			continue
		}
		seen[target] = true
		purpose, ok := purposeForTargetName(target)
		if !ok {
			continue
		}
		out = append(out, domain.DetectedCommand{
			Purpose: purpose, Command: "make " + target,
			Source: domain.SourceEvidence{Path: path, Line: lineOf(body, m[0])},
		})
	}
	return out
}

func taskfileCommands(tree *inventory.Tree, dir string) []domain.DetectedCommand {
	for _, base := range []string{"Taskfile.yml", "Taskfile.yaml"} {
		path := inventory.Join(dir, base)
		raw, err := tree.Read(path)
		if err != nil {
			continue
		}
		var doc yaml.Node
		if yaml.Unmarshal(raw, &doc) != nil {
			continue
		}
		tasks := findMappingValue(&doc, "tasks")
		if tasks == nil {
			continue
		}
		var out []domain.DetectedCommand
		for i := 0; i+1 < len(tasks.Content); i += 2 {
			name := tasks.Content[i].Value
			purpose, ok := purposeForTargetName(name)
			if !ok {
				continue
			}
			out = append(out, domain.DetectedCommand{
				Purpose: purpose, Command: "task " + name,
				Source: domain.SourceEvidence{Path: path, Line: tasks.Content[i].Line},
			})
		}
		return out
	}
	return nil
}

func findMappingValue(doc *yaml.Node, key string) *yaml.Node {
	root := doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

var justRecipeRe = regexp.MustCompile(`(?m)^([a-zA-Z_][a-zA-Z0-9_-]*)\s*:`)

func justfileCommands(tree *inventory.Tree, dir string) []domain.DetectedCommand {
	path := inventory.Join(dir, "justfile")
	body := tree.ReadString(path)
	if body == "" {
		path = inventory.Join(dir, "Justfile")
		body = tree.ReadString(path)
	}
	if body == "" {
		return nil
	}
	var out []domain.DetectedCommand
	seen := map[string]bool{}
	for _, m := range justRecipeRe.FindAllStringSubmatchIndex(body, -1) {
		name := body[m[2]:m[3]]
		if seen[name] {
			continue
		}
		seen[name] = true
		purpose, ok := purposeForTargetName(name)
		if !ok {
			continue
		}
		out = append(out, domain.DetectedCommand{
			Purpose: purpose, Command: "just " + name,
			Source: domain.SourceEvidence{Path: path, Line: lineOf(body, m[0])},
		})
	}
	return out
}

func purposeForTargetName(name string) (domain.CommandPurpose, bool) {
	switch {
	case name == "build" || strings.HasPrefix(name, "build-"):
		return domain.CommandBuild, true
	case name == "test" || strings.HasPrefix(name, "test-"):
		return domain.CommandTest, true
	case name == "lint" || name == "check" || name == "vet":
		return domain.CommandLint, true
	case name == "dev" || name == "run" || name == "up":
		return domain.CommandDev, true
	case name == "fmt" || name == "format":
		return domain.CommandFormat, true
	case name == "typecheck" || name == "type-check":
		return domain.CommandTypecheck, true
	case name == "e2e":
		return domain.CommandE2E, true
	case strings.Contains(name, "migrate"):
		return domain.CommandMigrate, true
	}
	return "", false
}

func ecosystemDefaults(tree *inventory.Tree, dir string, info *ManifestInfo) []domain.DetectedCommand {
	if info == nil {
		return nil
	}
	ev := domain.SourceEvidence{Path: info.Path}
	switch info.Ecosystem {
	case "go":
		return []domain.DetectedCommand{
			{Purpose: domain.CommandBuild, Command: "go build ./...", Source: ev},
			{Purpose: domain.CommandTest, Command: "go test ./...", Source: ev},
			{Purpose: domain.CommandLint, Command: "go vet ./...", Source: ev},
		}
	case "rust":
		return []domain.DetectedCommand{
			{Purpose: domain.CommandBuild, Command: "cargo build", Source: ev},
			{Purpose: domain.CommandTest, Command: "cargo test", Source: ev},
			{Purpose: domain.CommandLint, Command: "cargo clippy", Source: ev},
		}
	case "dart":
		return []domain.DetectedCommand{
			{Purpose: domain.CommandBuild, Command: "flutter build", Source: ev},
			{Purpose: domain.CommandTest, Command: "flutter test", Source: ev},
			{Purpose: domain.CommandLint, Command: "flutter analyze", Source: ev},
		}
	case "python":
		var out []domain.DetectedCommand
		if pytestConfigured(tree, dir, info) {
			out = append(out, domain.DetectedCommand{Purpose: domain.CommandTest, Command: "pytest", Source: ev})
		}
		if ruffConfigured(tree, dir, info) {
			out = append(out, domain.DetectedCommand{Purpose: domain.CommandLint, Command: "ruff check .", Source: ev})
		}
		if mypyConfigured(tree, dir, info) {
			out = append(out, domain.DetectedCommand{Purpose: domain.CommandTypecheck, Command: "mypy .", Source: ev})
		}
		return out
	}
	return nil
}

func pytestConfigured(tree *inventory.Tree, dir string, info *ManifestInfo) bool {
	if tree.Has(inventory.Join(dir, "pytest.ini")) || tree.Has(inventory.Join(dir, "conftest.py")) {
		return true
	}
	_, ok := info.hasDep("pytest")
	return ok || strings.Contains(tree.ReadString(inventory.Join(dir, "pyproject.toml")), "[tool.pytest")
}

func ruffConfigured(tree *inventory.Tree, dir string, info *ManifestInfo) bool {
	if tree.Has(inventory.Join(dir, "ruff.toml")) || tree.Has(inventory.Join(dir, ".ruff.toml")) {
		return true
	}
	_, ok := info.hasDep("ruff")
	return ok || strings.Contains(tree.ReadString(inventory.Join(dir, "pyproject.toml")), "[tool.ruff")
}

func mypyConfigured(tree *inventory.Tree, dir string, info *ManifestInfo) bool {
	if tree.Has(inventory.Join(dir, "mypy.ini")) {
		return true
	}
	_, ok := info.hasDep("mypy")
	return ok || strings.Contains(tree.ReadString(inventory.Join(dir, "pyproject.toml")), "[tool.mypy")
}

var (
	portFlagRe = regexp.MustCompile(`(?:--port[= ]|-p )(\d+)`)
	portEnvRe  = regexp.MustCompile(`PORT=(\d+)`)
	vitePortRe = regexp.MustCompile(`port\s*:\s*(\d+)`)
	exposeRe   = regexp.MustCompile(`(?m)^EXPOSE\s+(\d+)`)
	envPortRe  = regexp.MustCompile(`(?m)^PORT=(\d+)`)
	listenRe   = regexp.MustCompile(`ListenAndServe\(":(\d+)"`)
)

// detectDevPort reads the port a component's dev server or listener binds,
// in the order the spec ranks the evidence, and only guesses a framework's
// default once nothing in the tree states one.
func detectDevPort(tree *inventory.Tree, dir string, info *ManifestInfo, commands []domain.DetectedCommand) int {
	for _, c := range commands {
		if c.Purpose != domain.CommandDev {
			continue
		}
		if p := portFromText(c.Command); p != 0 {
			return p
		}
	}
	for _, base := range []string{"vite.config.ts", "vite.config.js", "vite.config.mjs", "vite.config.mts"} {
		if m := vitePortRe.FindStringSubmatch(tree.ReadString(inventory.Join(dir, base))); m != nil {
			return atoiSafe(m[1])
		}
	}
	if m := exposeRe.FindStringSubmatch(tree.ReadString(inventory.Join(dir, "Dockerfile"))); m != nil {
		return atoiSafe(m[1])
	}
	if m := envPortRe.FindStringSubmatch(tree.ReadString(inventory.Join(dir, ".env.example"))); m != nil {
		return atoiSafe(m[1])
	}
	if info != nil && info.Ecosystem == "go" {
		for i, f := range tree.Under(dir) {
			if i >= maxGoFilesScanned {
				break
			}
			if !strings.HasSuffix(f, ".go") {
				continue
			}
			if m := listenRe.FindStringSubmatch(tree.ReadString(f)); m != nil {
				return atoiSafe(m[1])
			}
		}
	}
	if info != nil && info.Ecosystem == "node" {
		if _, _, ok := info.anyDep("next", "nuxt"); ok {
			return 3000
		}
		if _, _, ok := info.anyDep("vite", "@sveltejs/kit"); ok {
			return 5173
		}
		if _, _, ok := info.anyDep("astro"); ok {
			return 4321
		}
		if _, _, ok := info.anyDep("@angular/core"); ok {
			return 4200
		}
	}
	return 0
}

func portFromText(s string) int {
	if m := portFlagRe.FindStringSubmatch(s); m != nil {
		return atoiSafe(m[1])
	}
	if m := portEnvRe.FindStringSubmatch(s); m != nil {
		return atoiSafe(m[1])
	}
	return 0
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
