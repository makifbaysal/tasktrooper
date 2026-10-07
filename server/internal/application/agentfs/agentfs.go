package agentfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Flavor string

const (
	FlavorClaude Flavor = "claude"
	FlavorCursor Flavor = "cursor"
)

func KnownFlavor(f Flavor) bool {
	return f == FlavorClaude || f == FlavorCursor || f == FlavorAntigravity || f == FlavorOpencode
}

const ttPrefix = "tt-"

const nameMax = 64

type Bundle struct {
	Agent  domain.Agent
	Skills []domain.Skill
	// TechStacks names the stacks the skills are filed under. The layout stays
	// flat — a CLI discovers skills by directory and nesting them under a stack
	// would hide them — so the stack reaches the session as metadata on each
	// rendered skill instead.
	TechStacks []domain.TechStack
	Rules      []string
}

// stackName is the name of the stack a skill belongs to, empty for a general
// skill and for one whose stack is not in this bundle.
func (b Bundle) stackName(s domain.Skill) string {
	if s.TechStackID == nil {
		return ""
	}
	for _, st := range b.TechStacks {
		if st.ID == *s.TechStackID {
			return strings.TrimSpace(st.Name)
		}
	}
	return ""
}

type Result struct {
	Written   []string
	Unchanged int
	Removed   []string
}

type file struct {
	rel  string
	body string
}

func Materialize(root string, flavor Flavor, b Bundle) (Result, error) {
	var res Result
	if strings.TrimSpace(root) == "" {
		return res, errors.New("agentfs: root is empty")
	}
	files, err := render(flavor, b)
	if err != nil {
		return res, err
	}

	wanted := make(map[string]struct{}, len(files))
	for _, f := range files {
		wanted[f.rel] = struct{}{}
	}

	for _, f := range files {
		abs := filepath.Join(root, filepath.FromSlash(f.rel))
		changed, err := writeIfChanged(abs, f.body)
		if err != nil {
			return res, err
		}
		if changed {
			res.Written = append(res.Written, f.rel)
		} else {
			res.Unchanged++
		}
	}
	sort.Strings(res.Written)

	stale, err := ownedPaths(root, flavor)
	if err != nil {
		return res, err
	}
	for _, rel := range stale {
		if _, keep := wanted[rel]; keep {
			continue
		}
		if err := removeOwned(root, rel); err != nil {
			return res, err
		}
		res.Removed = append(res.Removed, rel)
	}
	sort.Strings(res.Removed)
	return res, nil
}

func Clean(root string, flavor Flavor) error {
	owned, err := ownedPaths(root, flavor)
	if err != nil {
		return err
	}
	for _, rel := range owned {
		if err := removeOwned(root, rel); err != nil {
			return err
		}
	}
	return nil
}

func render(flavor Flavor, b Bundle) ([]file, error) {
	switch flavor {
	case FlavorClaude:
		return renderClaude(b), nil
	case FlavorCursor:
		return renderCursor(b), nil
	case FlavorAntigravity:
		return renderAntigravity(b), nil
	case FlavorOpencode:
		return renderOpencode(b), nil
	default:
		return nil, fmt.Errorf("agentfs: unknown flavor %q", flavor)
	}
}

func writeIfChanged(abs, body string) (bool, error) {
	existing, err := os.ReadFile(abs)
	if err == nil && string(existing) == body {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("agentfs: read %s: %w", abs, err)
	}
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("agentfs: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".agentfs-*")
	if err != nil {
		return false, fmt.Errorf("agentfs: temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func(wrapped error) (bool, error) {
		os.Remove(tmpName)
		return false, wrapped
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return cleanup(fmt.Errorf("agentfs: write %s: %w", tmpName, err))
	}
	if err := tmp.Close(); err != nil {
		return cleanup(fmt.Errorf("agentfs: close %s: %w", tmpName, err))
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		return cleanup(fmt.Errorf("agentfs: chmod %s: %w", tmpName, err))
	}
	if err := rename(tmpName, abs); err != nil {
		return cleanup(fmt.Errorf("agentfs: rename onto %s: %w", abs, err))
	}
	return true, nil
}

func removeOwned(root, rel string) error {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	target := abs
	if filepath.Base(abs) == claudeSkillFile {
		target = filepath.Dir(abs)
	}
	if err := RemoveAll(target); err != nil {
		return fmt.Errorf("agentfs: remove %s: %w", target, err)
	}
	return nil
}

func ownedPaths(root string, flavor Flavor) ([]string, error) {
	var out []string
	for _, dir := range ownedDirs(flavor) {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir.path)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("agentfs: read dir %s: %w", dir.path, err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, ttPrefix) {
				continue
			}

			if dir.skillDirs != e.IsDir() {
				continue
			}
			if dir.skillDirs {
				out = append(out, dir.path+"/"+name+"/"+claudeSkillFile)
				continue
			}
			out = append(out, dir.path+"/"+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

type ownedDir struct {
	path      string
	skillDirs bool
}

func ownedDirs(flavor Flavor) []ownedDir {
	switch flavor {
	case FlavorClaude, FlavorAntigravity:
		return []ownedDir{
			{path: claudeSkillsDir, skillDirs: true},
			{path: claudeAgentsDir},
		}
	case FlavorOpencode:
		return []ownedDir{{path: claudeSkillsDir, skillDirs: true}}
	case FlavorCursor:
		return []ownedDir{{path: cursorRulesDir}}
	default:
		return nil
	}
}

func slug(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	lastHyphen := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "skill"
	}
	if len(s) > nameMax {
		s = strings.Trim(s[:nameMax], "-")
	}
	return ttPrefix + s
}

func uniqueNames(names []string) []string {
	seen := make(map[string]int, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		base := slug(n)
		candidate := base
		for {
			count := seen[candidate]
			seen[candidate] = count + 1
			if count == 0 {
				break
			}
			candidate = fmt.Sprintf("%s-%d", base, count+1)
		}
		out = append(out, candidate)
	}
	return out
}

func yamlString(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func usableSkills(skills []domain.Skill) []domain.Skill {
	out := make([]domain.Skill, 0, len(skills))
	for _, s := range skills {
		if !s.Enabled || strings.TrimSpace(s.Content) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func ruleBody(rules []string) string {
	kept := make([]string, 0, len(rules))
	for _, r := range rules {
		if trimmed := strings.TrimSpace(r); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, "\n\n")
}

func describe(description, name string) string {
	if d := strings.TrimSpace(description); d != "" {
		return d
	}
	return name
}
