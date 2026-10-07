// Package deploy makes shipping a first-class, repeatable definition instead
// of a hand-written workflow file: a catalog of provider-specific deploy
// recipes (the template) plus the per-repository, per-environment target they
// are rendered against (provider vars, health URL, rollback policy).
package deploy

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

//go:embed templates
var templateFS embed.FS

// templateDoc is the YAML frontmatter of a recipe file.
type templateDoc struct {
	ID           string   `yaml:"id"`
	Provider     string   `yaml:"provider"`
	Name         string   `yaml:"name"`
	Summary      string   `yaml:"summary"`
	Kinds        []string `yaml:"kinds"`
	Envs         []string `yaml:"envs"`
	WorkflowFile string   `yaml:"workflow_file"`
	RollbackHint string   `yaml:"rollback_hint"`
	RequiredVars []struct {
		Key      string `yaml:"key"`
		Label    string `yaml:"label"`
		Example  string `yaml:"example"`
		Required *bool  `yaml:"required"`
	} `yaml:"required_vars"`
}

var (
	loadOnce  sync.Once
	templates []domain.DeployTemplate
	byID      map[string]domain.DeployTemplate
)

// Templates returns every embedded deploy recipe, ordered by name.
func Templates() []domain.DeployTemplate {
	loadOnce.Do(loadTemplates)
	out := make([]domain.DeployTemplate, len(templates))
	copy(out, templates)
	return out
}

// TemplatesForKind narrows the catalog to the recipes that fit a repo kind, so
// a mobile repo is never offered a Cloud Run deploy.
func TemplatesForKind(kind string) []domain.DeployTemplate {
	var out []domain.DeployTemplate
	for _, t := range Templates() {
		if t.SupportsKind(kind) {
			out = append(out, t)
		}
	}
	return out
}

// Template resolves a recipe by id, or by provider when the caller only knows
// which cloud it ships to.
func Template(idOrProvider string) (domain.DeployTemplate, bool) {
	loadOnce.Do(loadTemplates)
	key := strings.TrimSpace(strings.ToLower(idOrProvider))
	if t, ok := byID[key]; ok {
		return t, true
	}
	for _, t := range templates {
		if t.Provider == key {
			return t, true
		}
	}
	return domain.DeployTemplate{}, false
}

// loadTemplates parses the embedded recipes once. A malformed recipe is a
// build-time authoring bug, so it panics rather than silently shrinking the
// catalog at runtime.
func loadTemplates() {
	byID = map[string]domain.DeployTemplate{}
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		panic(fmt.Sprintf("deploy templates: %v", err))
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		full := path.Join("templates", entry.Name())
		raw, err := templateFS.ReadFile(full)
		if err != nil {
			panic(fmt.Sprintf("deploy templates: %s: %v", full, err))
		}
		tpl, err := parseTemplate(string(raw))
		if err != nil {
			panic(fmt.Sprintf("deploy templates: %s: %v", full, err))
		}
		if _, dupe := byID[tpl.ID]; dupe {
			panic(fmt.Sprintf("deploy templates: duplicate id %q", tpl.ID))
		}
		byID[tpl.ID] = tpl
		templates = append(templates, tpl)
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].Name < templates[j].Name })
}

// parseTemplate reads CRLF as LF: a checkout with core.autocrlf=true embeds
// the recipes with CRLF, and a "---\r\n" fence failing here would panic inside
// loadOnce and leave the deploy catalog empty for the life of the process.
func parseTemplate(raw string) (domain.DeployTemplate, error) {
	content := strings.ReplaceAll(strings.TrimPrefix(raw, "\ufeff"), "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return domain.DeployTemplate{}, fmt.Errorf("missing frontmatter opening ---")
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end == -1 {
		return domain.DeployTemplate{}, fmt.Errorf("missing frontmatter closing ---")
	}
	var doc templateDoc
	if err := yaml.Unmarshal([]byte(rest[:end]), &doc); err != nil {
		return domain.DeployTemplate{}, fmt.Errorf("frontmatter: %w", err)
	}
	body := strings.TrimSpace(rest[end+len("\n---\n"):])
	if doc.ID == "" || doc.Provider == "" || doc.Name == "" || doc.WorkflowFile == "" {
		return domain.DeployTemplate{}, fmt.Errorf("frontmatter requires id, provider, name and workflow_file")
	}
	if !domain.ValidDeployProvider(doc.Provider) {
		return domain.DeployTemplate{}, fmt.Errorf("unknown provider %q", doc.Provider)
	}
	if body == "" {
		return domain.DeployTemplate{}, fmt.Errorf("empty template body")
	}
	tpl := domain.DeployTemplate{
		ID:           doc.ID,
		Provider:     doc.Provider,
		Name:         doc.Name,
		Summary:      doc.Summary,
		Kinds:        doc.Kinds,
		Envs:         doc.Envs,
		WorkflowFile: doc.WorkflowFile,
		RollbackHint: doc.RollbackHint,
		Body:         body,
	}
	if len(tpl.Envs) == 0 {
		tpl.Envs = domain.DeployEnvs()
	}
	for _, v := range doc.RequiredVars {
		required := true
		if v.Required != nil {
			required = *v.Required
		}
		tpl.RequiredVars = append(tpl.RequiredVars, domain.DeployTemplateVar{
			Key: v.Key, Label: v.Label, Example: v.Example, Required: required,
		})
	}
	return tpl, nil
}

// Render substitutes {{var}} placeholders in the recipe body with the target's
// values. A placeholder with no value is left visible as
// {{key — SET THIS}} so the agent authoring the workflow cannot ship a
// half-filled file without noticing.
func Render(tpl domain.DeployTemplate, target domain.DeployTarget) string {
	return substitute(tpl.Body, EffectiveVars(tpl, target))
}

// EffectiveVars is the var map Render actually substitutes: the target's own
// vars plus values derived from the target record itself. Anything judging
// completeness (MissingVars) must use this same map, or it reports vars as
// missing that rendering fills anyway.
func EffectiveVars(tpl domain.DeployTemplate, target domain.DeployTarget) map[string]string {
	vars := map[string]string{}
	for k, v := range target.Vars {
		vars[k] = v
	}
	if target.Env != "" {
		vars["env"] = target.Env
	}
	if target.HealthURL != "" {
		vars["health_url"] = target.HealthURL
	}
	if target.BaseURL != "" {
		vars["base_url"] = target.BaseURL
	}
	if _, ok := vars["workflow_file"]; !ok {
		vars["workflow_file"] = tpl.WorkflowFile
	}
	return vars
}

func substitute(body string, vars map[string]string) string {
	var b strings.Builder
	rest := body
	for {
		open := strings.Index(rest, "{{")
		if open == -1 {
			b.WriteString(rest)
			return b.String()
		}
		end := strings.Index(rest[open:], "}}")
		if end == -1 {
			b.WriteString(rest)
			return b.String()
		}
		end += open
		key := strings.TrimSpace(rest[open+2 : end])
		b.WriteString(rest[:open])
		// `${{ ... }}` is GitHub Actions' own expression syntax and must survive
		// rendering untouched; only bare {{key}} placeholders are ours.
		isActionsExpr := open > 0 && rest[open-1] == '$'
		switch {
		case isActionsExpr || !isTemplateKey(key):
			// Emit only the "{{" and rescan right after it: an
			// expression-shaped span may still contain a real {{key}} whose
			// closing braces this span's "}}" actually were.
			b.WriteString(rest[open : open+2])
			rest = rest[open+2:]
			continue
		default:
			if val, ok := vars[key]; ok && strings.TrimSpace(val) != "" {
				b.WriteString(val)
			} else {
				b.WriteString("{{" + key + " — SET THIS}}")
			}
		}
		rest = rest[end+2:]
	}
}

// isTemplateKey accepts only bare identifiers, so anything expression-shaped
// (dots, spaces, operators) is passed through as literal workflow syntax.
func isTemplateKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
