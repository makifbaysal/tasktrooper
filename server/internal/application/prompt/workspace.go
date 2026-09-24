package prompt

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const workspaceRepoDescriptionMax = 120

// The toolless intake/planner stages used to ask for state the platform already stores; the snapshot answers it and this rule makes re-asking forbidden.
const workspaceFactsRule = `## Workspace state (system facts — never ask the stakeholder about these)
The snapshot below is ground truth. It already answers the following, so asking them is forbidden:
- Whether the team has access to a repository, codebase, or its credentials — every listed repository is checked out and fully accessible to the agent team.
- Which repositories, projects, or teammates exist, and what stack a listed repository uses.
- Repo URLs, git hosting, CMS logins, deploy credentials, or a "contact for the dev team" — the agent team IS the dev team and the platform holds the access.
If the request names a product with no repository in the snapshot, plan the work to create and register that repository. Do not ask the stakeholder to supply access details.`

// Empty input still renders: "none registered" is a fact worth stating and keeps the never-ask rule attached.
// projectTypes and componentsByRepo are optional (nil when the project model
// has not scanned yet, or is not wired) — the snapshot still renders without them.
func WorkspaceFactsBlock(projects []domain.InitiativeProject, repos []domain.Repository, projectTypes map[uuid.UUID]domain.ProjectType, componentsByRepo map[uuid.UUID][]domain.ComponentSummary) string {
	var sb strings.Builder
	sb.WriteString(workspaceFactsRule)
	sb.WriteString("\n\n")

	projectNames := make(map[uuid.UUID]string, len(projects))
	labels := make([]string, 0, len(projects))
	for _, p := range projects {
		projectNames[p.ID] = p.Name
		labels = append(labels, projectLabel(p, projectTypes))
	}
	if len(labels) == 0 {
		sb.WriteString("Projects (0): none registered\n")
	} else {
		sb.WriteString(fmt.Sprintf("Projects (%d): %s\n", len(labels), strings.Join(labels, ", ")))
	}

	if len(repos) == 0 {
		sb.WriteString("Repositories (0): none registered — the team registers one as part of delivery.")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("Repositories (%d) — all checked out and fully accessible to the agent team:\n", len(repos)))
	for _, r := range repos {
		var line strings.Builder
		line.WriteString("- ")
		line.WriteString(r.Name)
		if r.Kind != "" {
			line.WriteString(" (kind=")
			line.WriteString(r.Kind)
			line.WriteString(")")
		}
		if linked := linkedProjectNames(projectNames, r.ProjectIDs); len(linked) > 0 {
			line.WriteString(" — projects: ")
			line.WriteString(strings.Join(linked, ", "))
		}
		if label := componentsLabel(componentsByRepo[r.ID]); label != "" {
			line.WriteString(" — ")
			line.WriteString(label)
		}
		if desc := truncateRunes(r.Description, workspaceRepoDescriptionMax); desc != "" {
			line.WriteString(" — ")
			line.WriteString(desc)
		}
		sb.WriteString(line.String())
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func projectLabel(p domain.InitiativeProject, projectTypes map[uuid.UUID]domain.ProjectType) string {
	t, ok := projectTypes[p.ID]
	if !ok || t == "" {
		return p.Name
	}
	return fmt.Sprintf("%s (%s)", p.Name, t)
}

// componentsLabel renders a repository's components: a single-component repo
// (or one not yet scanned) just names the role, a monorepo lists every
// component as "path (role)".
func componentsLabel(components []domain.ComponentSummary) string {
	if len(components) == 0 {
		return ""
	}
	if len(components) == 1 {
		return "role: " + string(components[0].Role)
	}
	parts := make([]string, 0, len(components))
	for _, c := range components {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Path, c.Role))
	}
	return "components: " + strings.Join(parts, ", ")
}

func linkedProjectNames(names map[uuid.UUID]string, ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
