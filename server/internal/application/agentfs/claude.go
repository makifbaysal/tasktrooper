package agentfs

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	claudeSkillsDir = ".claude/skills"
	claudeAgentsDir = ".claude/agents"
	claudeSkillFile = "SKILL.md"
)

func renderClaude(b Bundle) []file {
	skills := usableSkills(b.Skills)
	names := make([]string, 0, len(skills))
	for _, s := range skills {
		names = append(names, s.Name)
	}
	slugs := uniqueNames(names)

	files := make([]file, 0, len(skills)+1)
	for i, s := range skills {
		files = append(files, file{
			rel:  fmt.Sprintf("%s/%s/%s", claudeSkillsDir, slugs[i], claudeSkillFile),
			body: claudeSkill(slugs[i], s, b.stackName(s)),
		})
	}
	agentSlug := slug(b.Agent.Name)
	files = append(files, file{
		rel:  fmt.Sprintf("%s/%s.md", claudeAgentsDir, agentSlug),
		body: claudeAgent(agentSlug, b),
	})
	return files
}

func claudeSkill(name string, s domain.Skill, techStack string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", yamlString(name))
	fmt.Fprintf(&b, "description: %s\n", yamlString(describe(s.Description, s.Name)))
	if techStack != "" {
		fmt.Fprintf(&b, "tech_stack: %s\n", yamlString(techStack))
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", strings.TrimSpace(s.Name))
	b.WriteString(strings.TrimSpace(s.Content))
	b.WriteString("\n")
	return b.String()
}

func claudeAgent(name string, b Bundle) string {
	var out strings.Builder
	out.WriteString("---\n")
	fmt.Fprintf(&out, "name: %s\n", yamlString(name))
	fmt.Fprintf(&out, "description: %s\n", yamlString(describe(b.Agent.Description, b.Agent.Name)))
	if tools := subagentTools(b.Agent.ToolPolicy); len(tools) > 0 {
		fmt.Fprintf(&out, "tools: %s\n", yamlString(strings.Join(tools, ", ")))
	}
	if model := strings.TrimSpace(b.Agent.Model); model != "" {
		fmt.Fprintf(&out, "model: %s\n", yamlString(model))
	}
	out.WriteString("---\n\n")

	sections := make([]string, 0, 2)
	if prompt := strings.TrimSpace(b.Agent.SystemPrompt); prompt != "" {
		sections = append(sections, prompt)
	}
	if rules := ruleBody(b.Rules); rules != "" {
		sections = append(sections, "## Rules\n\n"+rules)
	}
	if len(sections) == 0 {
		sections = append(sections, defaultAgentRoleKey.Render(agentNameInput{Name: strings.TrimSpace(b.Agent.Name)}))
	}
	out.WriteString(strings.Join(sections, "\n\n"))
	out.WriteString("\n")
	return out.String()
}

// subagentTools is the session's built-in surface minus the subagent tool:
// this file is what the session spawns as its own subagent, and a delegate
// that can delegate again multiplies sessions the run never chose to pay for.
func subagentTools(p domain.ToolPolicy) []string {
	tools := domain.NativeToolsForPolicy(p)
	out := tools[:0:0]
	for _, t := range tools {
		if t != domain.NativeSubagentTool {
			out = append(out, t)
		}
	}
	return out
}
