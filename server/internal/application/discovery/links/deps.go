package links

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// manifestDep is one runtime dependency line read off a manifest, with the
// evidence needed to cite it.
type manifestDep struct {
	name     string
	evidence domain.SourceEvidence
}

// depsExtractor reads every manifest a component owns and matches its
// runtime dependencies against the catalog.
type depsExtractor struct{}

func (depsExtractor) extract(ctx *scanCtx) {
	for _, file := range ctx.tree.Files {
		base := baseName(file)
		owner, ok := ctx.ownerOf(file)
		if !ok {
			continue
		}
		switch {
		case base == "package.json":
			addCatalogDeps(ctx, owner, ecoNPM, parsePackageJSON(ctx.tree, file))
		case base == "go.mod":
			addCatalogDeps(ctx, owner, ecoGo, parseGoMod(ctx.tree, file))
		case base == "requirements.txt":
			addCatalogDeps(ctx, owner, ecoPy, parseRequirementsTxt(ctx.tree, file))
		case base == "pyproject.toml":
			addCatalogDeps(ctx, owner, ecoPy, parsePyProjectDeps(ctx.tree, file))
		case base == "Cargo.toml":
			addCatalogDeps(ctx, owner, ecoRust, parseCargoToml(ctx.tree, file))
		case base == "Gemfile":
			addCatalogDeps(ctx, owner, ecoRuby, parseGemfile(ctx.tree, file))
		case base == "composer.json":
			addCatalogDeps(ctx, owner, ecoPHP, parseComposerJSON(ctx.tree, file))
		case base == "build.gradle" || base == "build.gradle.kts":
			addCatalogDeps(ctx, owner, ecoJVM, parseGradle(ctx.tree, file))
		}
	}
}

func addCatalogDeps(ctx *scanCtx, componentPath string, eco ecosystem, deps []manifestDep) {
	for _, dep := range deps {
		entry, ok := catalog.match(eco, dep.name)
		if !ok {
			continue
		}
		target := domain.LinkTarget{
			Kind:         domain.LinkTargetResource,
			ResourceKind: entry.Resource.Kind,
			Vendor:       entry.Resource.Vendor,
			Name:         entry.Resource.Name,
		}
		ctx.collector.addLink(rawSignal{
			componentPath: componentPath,
			signalKey:     "dep:" + entry.ID,
			protocol:      entry.Protocol,
			target:        target,
			evidence:      []domain.SourceEvidence{dep.evidence},
			confidence:    domain.ConfidenceHigh,
			family:        familyOf(entry.Resource.Kind, entry.Resource.Vendor),
		})
	}
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// --- package.json ---

type packageJSONFile struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func parsePackageJSON(tree *inventory.Tree, file string) []manifestDep {
	raw, err := tree.Read(file)
	if err != nil {
		return nil
	}
	var pkg packageJSONFile
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil
	}
	var deps []manifestDep
	for name := range pkg.Dependencies {
		deps = append(deps, manifestDep{name: name, evidence: jsonKeyEvidence(raw, file, name)})
	}
	// prisma/@prisma/client are read from either dependencies or
	// devDependencies: the CLI is commonly a dev dependency even though it
	// drives the runtime schema.
	for _, name := range []string{"prisma", "@prisma/client"} {
		if _, ok := pkg.Dependencies[name]; ok {
			continue
		}
		if _, ok := pkg.DevDependencies[name]; ok {
			deps = append(deps, manifestDep{name: name, evidence: jsonKeyEvidence(raw, file, name)})
		}
	}
	return deps
}

// npmDeps returns every declared dependency name (both dependencies and
// devDependencies), for the workspace matcher, which cares whether a
// component names another one at all, not just at runtime.
func npmDeps(tree *inventory.Tree, file string) []manifestDep {
	raw, err := tree.Read(file)
	if err != nil {
		return nil
	}
	var pkg packageJSONFile
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil
	}
	var deps []manifestDep
	for name := range pkg.Dependencies {
		deps = append(deps, manifestDep{name: name, evidence: jsonKeyEvidence(raw, file, name)})
	}
	for name := range pkg.DevDependencies {
		deps = append(deps, manifestDep{name: name, evidence: jsonKeyEvidence(raw, file, name)})
	}
	return deps
}

// jsonKeyEvidence finds the 1-based line a JSON key's value starts on, for
// citing a manifest entry without a full JSON-with-positions parser.
func jsonKeyEvidence(raw []byte, file, key string) domain.SourceEvidence {
	needle := regexp.QuoteMeta(`"` + key + `"`)
	re := regexp.MustCompile(needle + `\s*:`)
	loc := re.FindIndex(raw)
	if loc == nil {
		return domain.SourceEvidence{Path: file}
	}
	line := 1 + strings.Count(string(raw[:loc[0]]), "\n")
	return domain.SourceEvidence{Path: file, Line: line}
}

// --- go.mod ---

var goRequireRe = regexp.MustCompile(`^\s*([A-Za-z0-9._~/-]+(?:\.[A-Za-z]{2,})[A-Za-z0-9._~/-]*)\s+v[0-9][A-Za-z0-9.+_-]*(\s+//\s*indirect)?`)

func parseGoMod(tree *inventory.Tree, file string) []manifestDep {
	raw := tree.ReadString(file)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	var deps []manifestDep
	inBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inBlock = true
			continue
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "("):
			trimmed = strings.TrimPrefix(trimmed, "require ")
		case !inBlock:
			continue
		}
		m := goRequireRe.FindStringSubmatch(trimmed)
		if m == nil || m[2] != "" {
			continue
		}
		deps = append(deps, manifestDep{name: m[1], evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
	}
	return deps
}

// goModRequireAll includes indirect requires and the module's own path (from
// the "module" line), for the workspace matcher's replace-directive check.
func goModDirectives(tree *inventory.Tree, file string) (modulePath string, requires []manifestDep, replaces map[string]string) {
	raw := tree.ReadString(file)
	if raw == "" {
		return "", nil, nil
	}
	lines := strings.Split(raw, "\n")
	replaces = map[string]string{}
	inRequireBlock, inReplaceBlock := false, false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "module "):
			modulePath = strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		case strings.HasPrefix(trimmed, "require ("):
			inRequireBlock = true
		case inRequireBlock && trimmed == ")":
			inRequireBlock = false
		case strings.HasPrefix(trimmed, "replace ("):
			inReplaceBlock = true
		case inReplaceBlock && trimmed == ")":
			inReplaceBlock = false
		case inRequireBlock, strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "("):
			t := strings.TrimPrefix(trimmed, "require ")
			fields := strings.Fields(t)
			if len(fields) >= 2 && strings.HasPrefix(fields[1], "v") {
				requires = append(requires, manifestDep{name: fields[0], evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
			}
		case inReplaceBlock, strings.HasPrefix(trimmed, "replace ") && !strings.Contains(trimmed, "("):
			t := strings.TrimPrefix(trimmed, "replace ")
			if idx := strings.Index(t, "=>"); idx >= 0 {
				lhs := strings.Fields(strings.TrimSpace(t[:idx]))
				rhs := strings.TrimSpace(t[idx+2:])
				if len(lhs) >= 1 && (strings.HasPrefix(rhs, "./") || strings.HasPrefix(rhs, "../")) {
					replaces[lhs[0]] = rhs
				}
			}
		}
	}
	return modulePath, requires, replaces
}

// --- requirements.txt ---

var pyNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+`)

func parseRequirementsTxt(tree *inventory.Tree, file string) []manifestDep {
	raw := tree.ReadString(file)
	if raw == "" {
		return nil
	}
	var deps []manifestDep
	for i, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		if idx := strings.IndexAny(trimmed, ";"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		name := pyNameRe.FindString(trimmed)
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if idx := strings.IndexByte(name, '['); idx >= 0 {
			name = name[:idx]
		}
		deps = append(deps, manifestDep{name: name, evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
	}
	return deps
}

// --- pyproject.toml ---

func parsePyProjectDeps(tree *inventory.Tree, file string) []manifestDep {
	raw := tree.ReadString(file)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	var deps []manifestDep
	section := ""
	inArray := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			inArray = false
			continue
		}
		switch section {
		case "[project]":
			if strings.HasPrefix(trimmed, "dependencies") && strings.Contains(trimmed, "=") {
				inArray = !strings.Contains(trimmed, "]")
				deps = append(deps, extractTOMLStringArrayDeps(trimmed, file, i+1)...)
				continue
			}
			if inArray {
				deps = append(deps, extractTOMLStringArrayDeps(trimmed, file, i+1)...)
				if strings.Contains(trimmed, "]") {
					inArray = false
				}
			}
		case "[tool.poetry.dependencies]":
			if trimmed == "" || !strings.Contains(trimmed, "=") {
				continue
			}
			name := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
			name = strings.Trim(name, `"'`)
			if name == "" || strings.EqualFold(name, "python") {
				continue
			}
			deps = append(deps, manifestDep{name: name, evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
		}
	}
	return deps
}

var toolPoetryDepNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+`)

// extractTOMLStringArrayDeps pulls PEP 508 requirement strings out of a
// (possibly multi-line) `dependencies = [...]` array line.
func extractTOMLStringArrayDeps(line, file string, lineNo int) []manifestDep {
	var deps []manifestDep
	re := regexp.MustCompile(`"([^"]+)"|'([^']+)'`)
	for _, m := range re.FindAllStringSubmatch(line, -1) {
		spec := m[1]
		if spec == "" {
			spec = m[2]
		}
		name := toolPoetryDepNameRe.FindString(strings.TrimSpace(spec))
		if name == "" {
			continue
		}
		deps = append(deps, manifestDep{name: name, evidence: domain.SourceEvidence{Path: file, Line: lineNo}})
	}
	return deps
}

// --- Cargo.toml ---

func parseCargoToml(tree *inventory.Tree, file string) []manifestDep {
	raw := tree.ReadString(file)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	var deps []manifestDep
	section := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
			continue
		}
		if section != "[dependencies]" {
			continue
		}
		if trimmed == "" || !strings.Contains(trimmed, "=") {
			continue
		}
		name := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[0])
		name = strings.Trim(name, `"'`)
		if name == "" {
			continue
		}
		deps = append(deps, manifestDep{name: name, evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
	}
	return deps
}

// --- Gemfile ---

var gemfileRe = regexp.MustCompile(`^\s*gem\s+["']([^"']+)["']`)

func parseGemfile(tree *inventory.Tree, file string) []manifestDep {
	raw := tree.ReadString(file)
	if raw == "" {
		return nil
	}
	var deps []manifestDep
	for i, line := range strings.Split(raw, "\n") {
		m := gemfileRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		deps = append(deps, manifestDep{name: m[1], evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
	}
	return deps
}

// --- composer.json ---

type composerJSONFile struct {
	Require map[string]string `json:"require"`
}

func parseComposerJSON(tree *inventory.Tree, file string) []manifestDep {
	raw, err := tree.Read(file)
	if err != nil {
		return nil
	}
	var doc composerJSONFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	var deps []manifestDep
	for name := range doc.Require {
		if name == "php" || strings.HasPrefix(name, "ext-") {
			continue
		}
		deps = append(deps, manifestDep{name: name, evidence: jsonKeyEvidence(raw, file, name)})
	}
	return deps
}

// --- build.gradle(.kts) ---

var gradleDepRe = regexp.MustCompile(`(?:implementation|api)\s*[( ]\s*["']([A-Za-z0-9_.-]+:[A-Za-z0-9_.-]+)(?::[A-Za-z0-9_.+-]+)?["']`)

func parseGradle(tree *inventory.Tree, file string) []manifestDep {
	raw := tree.ReadString(file)
	if raw == "" {
		return nil
	}
	var deps []manifestDep
	for i, line := range strings.Split(raw, "\n") {
		m := gradleDepRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		deps = append(deps, manifestDep{name: m[1], evidence: domain.SourceEvidence{Path: file, Line: i + 1}})
	}
	return deps
}
