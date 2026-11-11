package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// TechStack is one technology an agent's skills can be filed under; a skill
// with none is general, with one only applies inside it. At most one, never
// several: a Django skill is not also a Flutter skill.
type TechStack struct {
	ID          uuid.UUID `json:"id"`
	AgentID     uuid.UUID `json:"agent_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateTechStackRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}

// UpdateTechStackRequest is a patch: reordering sends nothing but positions, so
// a full replace would make the caller resend a description it never touched. A
// nil field is left as it is.
type UpdateTechStackRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Position    *int    `json:"position"`
}

// techAlias folds the many spellings of one language or runtime onto a single
// key, so a stack called ".NET" matches a component whose detected language is
// "C#", and "TypeScript (Node.js)" one detected as JavaScript.
var techAlias = map[string]string{
	"go": "go", "golang": "go",
	"java": "java", "kotlin": "kotlin",
	"c#": "dotnet", "csharp": "dotnet", ".net": "dotnet", "dotnet": "dotnet",
	"typescript": "js", "javascript": "js", "node": "js", "nodejs": "js", "node.js": "js",
	"rust": "rust", "python": "python", "swift": "swift",
	"dart": "dart", "flutter": "dart",
	"ruby": "ruby", "php": "php", "scala": "scala", "elixir": "elixir",
	"c++": "cpp", "cpp": "cpp",
}

func techKeys(s string) []string {
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '#' || r == '+' || r == '.')
	})
	var out []string
	for _, f := range fields {
		if k, ok := techAlias[f]; ok {
			out = append(out, k)
		} else if k, ok := techAlias[strings.Trim(f, ".")]; ok {
			out = append(out, k)
		}
	}
	return out
}

// StacksOutOfScope returns the ids of the agent stacks that name a known
// language none of the given component stacks uses. A stack naming no known
// language (PostgreSQL, React, Mobile) is never excluded, and nothing is
// excluded when no language was detected at all: unknown means keep everything.
func StacksOutOfScope(stacks []TechStack, components []ComponentStack) map[uuid.UUID]bool {
	present := map[string]bool{}
	for _, c := range components {
		items := append(append(append([]StackItem{}, c.Languages...), c.Frameworks...), c.Libraries...)
		if c.Runtime != nil {
			items = append(items, *c.Runtime)
		}
		for _, it := range items {
			for _, k := range techKeys(it.Name) {
				present[k] = true
			}
		}
	}
	if len(present) == 0 {
		return nil
	}
	out := map[uuid.UUID]bool{}
	for _, st := range stacks {
		keys := techKeys(st.Name)
		if len(keys) == 0 {
			continue
		}
		matched := false
		for _, k := range keys {
			if present[k] {
				matched = true
				break
			}
		}
		if !matched {
			out[st.ID] = true
		}
	}
	return out
}

// SkillsForStacks drops the skills filed under an out-of-scope stack; general
// skills and skills of unnamed stacks stay.
func SkillsForStacks(skills []Skill, stacks []TechStack, components []ComponentStack) []Skill {
	out := StacksOutOfScope(stacks, components)
	if len(out) == 0 {
		return skills
	}
	kept := make([]Skill, 0, len(skills))
	for _, sk := range skills {
		if sk.TechStackID != nil && out[*sk.TechStackID] {
			continue
		}
		kept = append(kept, sk)
	}
	return kept
}
