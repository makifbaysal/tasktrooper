package board

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DesignSystemNotes renders the design system block a repository's runs
// carry: the project base and repository layer they build with.
type DesignSystemNotes interface {
	ContextNote(ctx context.Context, repositoryID uuid.UUID, showTool bool) string
}

func (r *Runner) SetDesignSystems(d DesignSystemNotes) { r.designSystems = d }

func (r *Runner) designSystemNote(ctx context.Context, repositoryID uuid.UUID, policy domain.ToolPolicy) string {
	if r.designSystems == nil || repositoryID == uuid.Nil {
		return ""
	}
	return r.designSystems.ContextNote(ctx, repositoryID, domain.ToolAllowedByPolicy("get_design_system", policy))
}
