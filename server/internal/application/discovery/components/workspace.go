package components

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"gopkg.in/yaml.v3"
)

// workspaceDecl is one workspace file's claim about which directories are
// member components; Evidence is what the shape gets attributed to when the
// claim resolves to real components.
type workspaceDecl struct {
	Evidence domain.SourceEvidence
	Dirs     map[string]bool
}

func discoverWorkspaces(tree *inventory.Tree, manifests map[string]*ManifestInfo) []workspaceDecl {
	var decls []workspaceDecl
	dirSet := allDirs(tree)

	if p := "pnpm-workspace.yaml"; tree.Has(p) {
		var cfg struct {
			Packages []string `yaml:"packages"`
		}
		if yaml.Unmarshal([]byte(tree.ReadString(p)), &cfg) == nil && len(cfg.Packages) > 0 {
			decls = append(decls, workspaceDecl{Evidence: domain.SourceEvidence{Path: p}, Dirs: expandGlobs(cfg.Packages, dirSet)})
		}
	}

	for _, info := range manifests {
		if info.Ecosystem != "node" || len(info.Workspaces) == 0 {
			continue
		}
		decls = append(decls, workspaceDecl{Evidence: domain.SourceEvidence{Path: info.Path}, Dirs: expandGlobs(info.Workspaces, dirSet)})
	}

	if p := "go.work"; tree.Has(p) {
		if dirs := parseGoWork(tree.ReadString(p)); len(dirs) > 0 {
			decls = append(decls, workspaceDecl{Evidence: domain.SourceEvidence{Path: p}, Dirs: toDirSet(dirs)})
		}
	}

	for dir, info := range manifests {
		if info.Ecosystem != "rust" {
			continue
		}
		p := inventory.Join(dir, "Cargo.toml")
		if members := parseCargoWorkspaceMembers(tree.ReadString(p)); len(members) > 0 {
			decls = append(decls, workspaceDecl{Evidence: domain.SourceEvidence{Path: p}, Dirs: expandGlobs(members, dirSet)})
		}
	}

	for dir, info := range manifests {
		if info.Ecosystem != "gradle" {
			continue
		}
		for _, base := range []string{"settings.gradle", "settings.gradle.kts"} {
			p := inventory.Join(dir, base)
			body := tree.ReadString(p)
			if body == "" {
				continue
			}
			restricted := map[string]bool{}
			for _, mod := range parseGradleIncludes(body) {
				rel := strings.ReplaceAll(strings.TrimPrefix(mod, ":"), ":", "/")
				if underAppsServicesPackages(rel) {
					restricted[inventory.Join(dir, rel)] = true
				}
			}
			if len(restricted) > 0 {
				decls = append(decls, workspaceDecl{Evidence: domain.SourceEvidence{Path: p}, Dirs: restricted})
			}
			break
		}
	}

	if p := "lerna.json"; tree.Has(p) {
		var cfg struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal([]byte(tree.ReadString(p)), &cfg) == nil && len(cfg.Packages) > 0 {
			decls = append(decls, workspaceDecl{Evidence: domain.SourceEvidence{Path: p}, Dirs: expandGlobs(cfg.Packages, dirSet)})
		}
	}

	sort.SliceStable(decls, func(i, j int) bool { return decls[i].Evidence.Path < decls[j].Evidence.Path })
	return decls
}

func allDirs(tree *inventory.Tree) map[string]bool {
	dirs := map[string]bool{}
	for _, f := range tree.Files {
		d := inventory.Dir(f)
		for d != "." && d != "" {
			dirs[d] = true
			d = inventory.Dir(d)
		}
	}
	return dirs
}

func toDirSet(dirs []string) map[string]bool {
	out := map[string]bool{}
	for _, d := range dirs {
		out[d] = true
	}
	return out
}

func expandGlobs(globs []string, dirs map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, g := range globs {
		for _, d := range expandGlob(g, dirs) {
			out[d] = true
		}
	}
	return out
}

// expandGlob resolves one workspace glob against the tree's known
// directories; "dir/*" is immediate children, "dir/**" is any depth, and
// anything else more exotic than that is left unmatched rather than guessed.
func expandGlob(glob string, dirs map[string]bool) []string {
	glob = strings.TrimSuffix(strings.TrimSpace(glob), "/")
	glob = strings.TrimPrefix(glob, "./")
	if glob == "" {
		return nil
	}
	switch {
	case strings.HasSuffix(glob, "/**"):
		prefix := strings.TrimSuffix(glob, "/**")
		pfx := prefix + "/"
		var out []string
		for d := range dirs {
			if d == prefix || strings.HasPrefix(d, pfx) {
				out = append(out, d)
			}
		}
		return out
	case strings.HasSuffix(glob, "/*"):
		prefix := strings.TrimSuffix(glob, "/*")
		pfx := prefix + "/"
		var out []string
		for d := range dirs {
			if !strings.HasPrefix(d, pfx) {
				continue
			}
			if !strings.Contains(strings.TrimPrefix(d, pfx), "/") {
				out = append(out, d)
			}
		}
		return out
	case strings.ContainsAny(glob, "*?"):
		return nil
	default:
		if glob == "." || dirs[glob] {
			return []string{glob}
		}
		return nil
	}
}

var goWorkUseRe = regexp.MustCompile(`use\s+\(([^)]*)\)|use\s+(\S+)`)

func parseGoWork(body string) []string {
	var out []string
	for _, m := range goWorkUseRe.FindAllStringSubmatch(body, -1) {
		block := m[1] + m[2]
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
			if line == "" {
				continue
			}
			out = append(out, path.Clean(strings.TrimPrefix(line, "./")))
		}
	}
	return out
}

var cargoMembersRe = regexp.MustCompile(`(?s)\[workspace\].*?members\s*=\s*\[([^\]]*)\]`)

func parseCargoWorkspaceMembers(body string) []string {
	m := cargoMembersRe.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	var out []string
	for _, item := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, item[1])
	}
	return out
}

var (
	gradleIncludeRe = regexp.MustCompile(`include\s*\(([^)]*)\)|(?m)^\s*include\s+([^\n]*)`)
	gradleQuotedRe  = regexp.MustCompile(`["']([^"']*)["']`)
)

func parseGradleIncludes(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range gradleIncludeRe.FindAllStringSubmatch(body, -1) {
		args := m[1] + m[2]
		for _, q := range gradleQuotedRe.FindAllStringSubmatch(args, -1) {
			mod := strings.Trim(strings.TrimSpace(q[1]), ":")
			if mod == "" || seen[mod] {
				continue
			}
			seen[mod] = true
			out = append(out, mod)
		}
	}
	return out
}
