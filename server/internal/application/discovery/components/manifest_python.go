package components

import (
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/discovery/inventory"
)

type pythonEcosystem struct{}

func (pythonEcosystem) Name() string { return "python" }

func (pythonEcosystem) Roots(tree *inventory.Tree) []string {
	dirs := map[string]bool{}
	for _, d := range manifestDirs(tree, "pyproject.toml") {
		dirs[d] = true
	}
	for _, d := range manifestDirs(tree, "requirements.txt") {
		if dirs[d] {
			continue
		}
		if tree.Has(inventory.Join(d, "pyproject.toml")) {
			continue
		}
		dirs[d] = true
	}
	return sortedSet(dirs)
}

var (
	pyNameRe      = regexp.MustCompile(`(?m)^name\s*=\s*"([^"]+)"`)
	pyPythonVerRe = regexp.MustCompile(`(?m)^(?:requires-python|python)\s*=\s*"([^"]+)"`)
)

// pyKnownDeps is the curated set of Python frameworks/libraries discovery
// names; anything else is noise at this altitude.
var pyKnownDeps = []string{
	"fastapi", "django", "flask", "starlette", "aiohttp", "sanic",
	"sqlalchemy", "alembic", "pytest", "ruff", "mypy", "celery",
}

func (pythonEcosystem) Read(tree *inventory.Tree, dir string) *ManifestInfo {
	info := &ManifestInfo{Ecosystem: "python", Dependencies: map[string]string{}}
	pyproject := inventory.Join(dir, "pyproject.toml")
	if body := tree.ReadString(pyproject); body != "" {
		info.Path = pyproject
		if m := pyNameRe.FindStringSubmatch(body); m != nil {
			info.Name = m[1]
		}
		if m := pyPythonVerRe.FindStringSubmatch(body); m != nil {
			info.LanguageVersion = trimVersion(m[1])
		}
		for _, dep := range pyKnownDeps {
			if depMentioned(body, dep) {
				info.Dependencies[dep] = ""
			}
		}
	} else {
		req := inventory.Join(dir, "requirements.txt")
		body := tree.ReadString(req)
		if body == "" {
			return nil
		}
		info.Path = req
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.FieldsFunc(line, func(r rune) bool {
				return r == '=' || r == '>' || r == '<' || r == '~' || r == '!' || r == '['
			})
			if len(parts) == 0 {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(parts[0]))
			for _, dep := range pyKnownDeps {
				if name == dep {
					info.Dependencies[dep] = ""
				}
			}
		}
	}
	if v := strings.TrimSpace(tree.ReadString(inventory.Join(dir, ".python-version"))); v != "" && info.LanguageVersion == "" {
		info.LanguageVersion = v
	}
	return info
}

func depMentioned(body, dep string) bool {
	re := regexp.MustCompile(`(?im)[\s"'\[]` + regexp.QuoteMeta(dep) + `[\s"'=<>\[]`)
	return re.MatchString(" " + body)
}
