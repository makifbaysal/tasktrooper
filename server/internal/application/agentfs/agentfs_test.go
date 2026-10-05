package agentfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func skill(name, description, content string) domain.Skill {
	return domain.Skill{Name: name, Description: description, Content: content, Enabled: true}
}

func bundle(skills ...domain.Skill) Bundle {
	return Bundle{
		Agent: domain.Agent{
			Name:         "QA Agent",
			Description:  "Verifies the product against acceptance criteria",
			SystemPrompt: "You verify. You do not implement.",
		},
		Skills: skills,
		Rules:  []string{"Never move a card you did not verify."},
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestMaterializeClaudeWritesSkillsAndAgent(t *testing.T) {
	root := t.TempDir()
	res, err := Materialize(root, FlavorClaude, bundle(skill("Code Review", "Reviews a diff", "Read the diff. Report findings.")))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	want := []string{".claude/agents/tt-qa-agent.md", ".claude/skills/tt-code-review/SKILL.md"}
	if strings.Join(res.Written, ",") != strings.Join(want, ",") {
		t.Fatalf("written = %v, want %v", res.Written, want)
	}

	got := read(t, root, ".claude/skills/tt-code-review/SKILL.md")

	if !strings.Contains(got, `name: "tt-code-review"`) {
		t.Errorf("skill frontmatter name missing:\n%s", got)
	}
	if !strings.Contains(got, `description: "Reviews a diff"`) {
		t.Errorf("skill description missing:\n%s", got)
	}
	if !strings.Contains(got, "Read the diff. Report findings.") {
		t.Errorf("skill body missing:\n%s", got)
	}

	agent := read(t, root, ".claude/agents/tt-qa-agent.md")
	if !strings.Contains(agent, "You verify. You do not implement.") {
		t.Errorf("agent prompt missing:\n%s", agent)
	}

	if !strings.Contains(agent, "Never move a card you did not verify.") {
		t.Errorf("agent rules missing:\n%s", agent)
	}
}

func TestMaterializeClaudeOmitsToolsForEmptyPolicy(t *testing.T) {
	root := t.TempDir()
	if _, err := Materialize(root, FlavorClaude, bundle()); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if agent := read(t, root, ".claude/agents/tt-qa-agent.md"); strings.Contains(agent, "tools:") {
		t.Errorf("empty policy wrote a tools field:\n%s", agent)
	}
}

func TestMaterializeClaudeWritesToolsForRestrictedPolicy(t *testing.T) {
	root := t.TempDir()
	b := bundle()
	b.Agent.ToolPolicy = domain.ToolPolicy{AllowTools: []string{"read_file", "grep_code"}}
	if _, err := Materialize(root, FlavorClaude, b); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	agent := read(t, root, ".claude/agents/tt-qa-agent.md")
	if !strings.Contains(agent, "tools:") {
		t.Fatalf("restricted policy wrote no tools field:\n%s", agent)
	}

	if strings.Contains(agent, "Bash") {
		t.Errorf("policy without run_terminal granted Bash:\n%s", agent)
	}
	if strings.Contains(agent, domain.NativeSubagentTool) {
		t.Errorf("the role's own subagent definition can spawn subagents:\n%s", agent)
	}
}

func TestMaterializeCursorRoleIsAlwaysAppliedAndSortsFirst(t *testing.T) {
	root := t.TempDir()
	res, err := Materialize(root, FlavorCursor, bundle(skill("Code Review", "Reviews a diff", "Read the diff.")))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	want := []string{".cursor/rules/tt-000-qa-agent.mdc", ".cursor/rules/tt-code-review.mdc"}
	if strings.Join(res.Written, ",") != strings.Join(want, ",") {
		t.Fatalf("written = %v, want %v", res.Written, want)
	}
	role := read(t, root, ".cursor/rules/tt-000-qa-agent.mdc")
	if !strings.Contains(role, "alwaysApply: true") {
		t.Errorf("role rule is not always applied:\n%s", role)
	}

	sk := read(t, root, ".cursor/rules/tt-code-review.mdc")
	if !strings.Contains(sk, "alwaysApply: false") {
		t.Errorf("skill rule is always applied:\n%s", sk)
	}
}

func TestMaterializeSkipsDisabledAndEmptySkills(t *testing.T) {
	root := t.TempDir()
	disabled := skill("Disabled", "d", "body")
	disabled.Enabled = false

	empty := skill("Empty", "e", "   ")
	res, err := Materialize(root, FlavorClaude, bundle(disabled, empty))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	for _, rel := range res.Written {
		if strings.Contains(rel, "skills/") {
			t.Errorf("wrote a skill that should have been dropped: %s", rel)
		}
	}
}

func TestMaterializeIsIdempotent(t *testing.T) {
	root := t.TempDir()
	b := bundle(skill("Code Review", "Reviews a diff", "Read the diff."))
	if _, err := Materialize(root, FlavorClaude, b); err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	res, err := Materialize(root, FlavorClaude, b)
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}

	if len(res.Written) != 0 {
		t.Errorf("second run rewrote %v", res.Written)
	}
	if res.Unchanged != 2 {
		t.Errorf("Unchanged = %d, want 2", res.Unchanged)
	}
}

func TestMaterializeRemovesStaleOwnedFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Materialize(root, FlavorClaude, bundle(skill("Gone", "g", "body"))); err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	res, err := Materialize(root, FlavorClaude, bundle())
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}

	if len(res.Removed) != 1 || res.Removed[0] != ".claude/skills/tt-gone/SKILL.md" {
		t.Fatalf("Removed = %v, want the stale skill", res.Removed)
	}

	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "tt-gone")); !os.IsNotExist(err) {
		t.Errorf("stale skill directory survived: %v", err)
	}
}

func TestMaterializeLeavesRepositoryOwnedFilesAlone(t *testing.T) {
	root := t.TempDir()
	theirs := filepath.Join(root, ".claude", "skills", "review")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("theirs"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Materialize(root, FlavorClaude, bundle()); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if err := Clean(root, FlavorClaude); err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if got := read(t, root, ".claude/skills/review/SKILL.md"); got != "theirs" {
		t.Errorf("repository skill = %q, want %q", got, "theirs")
	}
}

func TestCleanRemovesEverythingOwned(t *testing.T) {
	root := t.TempDir()
	if _, err := Materialize(root, FlavorCursor, bundle(skill("Code Review", "r", "body"))); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if err := Clean(root, FlavorCursor); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".cursor", "rules"))
	if err != nil {
		t.Fatalf("read rules dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Clean left %d entries", len(entries))
	}
}

func TestSlugCollisionsGetDistinctPaths(t *testing.T) {
	root := t.TempDir()

	res, err := Materialize(root, FlavorClaude, bundle(
		skill("Code Review", "first", "one"),
		skill("code-review", "second", "two"),
	))
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(res.Written) != 3 {
		t.Fatalf("written = %v, want two skills and one agent", res.Written)
	}
	if !strings.Contains(read(t, root, ".claude/skills/tt-code-review/SKILL.md"), "one") {
		t.Error("first skill was overwritten")
	}
	if !strings.Contains(read(t, root, ".claude/skills/tt-code-review-2/SKILL.md"), "two") {
		t.Error("second skill did not get its own path")
	}
}

func TestSlugFallsBackForNonASCIINames(t *testing.T) {

	if got := slug("测试"); got != "tt-skill" {
		t.Errorf("slug(non-ascii) = %q, want %q", got, "tt-skill")
	}
	if got := slug("  Kod İnceleme  "); !strings.HasPrefix(got, ttPrefix) {
		t.Errorf("slug(%q) = %q, want the tt- prefix", "Kod İnceleme", got)
	}
}

func TestYAMLStringFlattensAndEscapes(t *testing.T) {
	got := yamlString("Reviews: diffs\nand tests")
	if got != `"Reviews: diffs and tests"` {
		t.Errorf("yamlString = %s", got)
	}
	if got := yamlString(`say "hi"`); got != `"say \"hi\""` {
		t.Errorf("yamlString quotes = %s", got)
	}
}

func TestMaterializeRejectsUnknownFlavor(t *testing.T) {
	if _, err := Materialize(t.TempDir(), Flavor("codex"), bundle()); err == nil {
		t.Fatal("unknown flavor was accepted")
	}
}

func TestExcludeAddsPatternsOnceAndOnlyInAGitCheckout(t *testing.T) {
	root := t.TempDir()

	if err := Exclude(root); err != nil {
		t.Fatalf("Exclude on a plain directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("Exclude created a .git directory: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatalf("mkdir .git/info: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("*.log"), 0o644); err != nil {
		t.Fatalf("seed exclude: %v", err)
	}
	if err := Exclude(root); err != nil {
		t.Fatalf("Exclude: %v", err)
	}
	first := read(t, root, ".git/info/exclude")
	if !strings.Contains(first, "*.log") {
		t.Error("Exclude dropped the existing patterns")
	}
	for _, p := range excludePatterns {
		if !strings.Contains(first, p) {
			t.Errorf("pattern %q not written:\n%s", p, first)
		}
	}
	if err := Exclude(root); err != nil {
		t.Fatalf("second Exclude: %v", err)
	}
	if second := read(t, root, ".git/info/exclude"); second != first {
		t.Errorf("second Exclude changed the file:\n%s", second)
	}
}

func TestMaterializeStampsTheTechStackOnSkillFiles(t *testing.T) {
	stack := domain.TechStack{ID: uuid.New(), Name: "Django", Description: "DRF backend"}
	stacked := skill("Viewsets", "Writes a viewset", "body")
	stacked.TechStackID = &stack.ID

	b := bundle(stacked, skill("Code Review", "Reviews a diff", "body"))
	b.TechStacks = []domain.TechStack{stack}

	root := t.TempDir()
	if _, err := Materialize(root, FlavorClaude, b); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	got := read(t, root, ".claude/skills/tt-viewsets/SKILL.md")
	if !strings.Contains(got, `tech_stack: "Django"`) {
		t.Errorf("stack missing from skill frontmatter:\n%s", got)
	}
	if general := read(t, root, ".claude/skills/tt-code-review/SKILL.md"); strings.Contains(general, "tech_stack") {
		t.Errorf("a general skill must carry no tech_stack:\n%s", general)
	}

	cursorRoot := t.TempDir()
	if _, err := Materialize(cursorRoot, FlavorCursor, b); err != nil {
		t.Fatalf("Materialize cursor: %v", err)
	}
	if rule := read(t, cursorRoot, ".cursor/rules/tt-viewsets.mdc"); !strings.Contains(rule, `tech_stack: "Django"`) {
		t.Errorf("stack missing from cursor rule frontmatter:\n%s", rule)
	}
}

// A skill filed under a stack the bundle does not carry renders as a general
// one rather than with a dangling name.
func TestMaterializeIgnoresAnUnknownTechStack(t *testing.T) {
	unknown := uuid.New()
	orphan := skill("Orphan", "Stack is gone", "body")
	orphan.TechStackID = &unknown

	root := t.TempDir()
	if _, err := Materialize(root, FlavorClaude, bundle(orphan)); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got := read(t, root, ".claude/skills/tt-orphan/SKILL.md"); strings.Contains(got, "tech_stack") {
		t.Errorf("unknown stack must not reach the file:\n%s", got)
	}
}
