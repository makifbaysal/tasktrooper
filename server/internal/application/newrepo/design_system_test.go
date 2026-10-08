package newrepo

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeDesigns struct{ eff domain.EffectiveDesignSystem }

func (f fakeDesigns) Effective(context.Context, uuid.UUID) (domain.EffectiveDesignSystem, error) {
	return f.eff, nil
}

func TestAUIRepositoryInheritsItsProjectsDesignSystem(t *testing.T) {
	s := &Service{}
	s.SetDesignSystems(fakeDesigns{eff: domain.EffectiveDesignSystem{
		Project: &domain.InitiativeProject{Name: "TaskTrooper"},
		Base:    &domain.DesignSystem{Version: 3},
	}})

	design := s.inheritedDesignSystem(context.Background(), uuid.New(), domain.ComponentRoleFrontend)
	require.NotNil(t, design)
	brief := bootstrapDescription("web", domain.ComponentRoleFrontend, domain.NewRepositoryRequest{Scaffold: true}, nil, design)
	assert.Contains(t, brief, "the project's design system: `DESIGN.md`, `design/tokens.json`")
	assert.Contains(t, brief, "## The design system")
	assert.Contains(t, brief, "TaskTrooper project, whose design system v3 is approved")
	assert.Contains(t, brief, "`get_design_system` with `files: true`")

	assert.Nil(t, s.inheritedDesignSystem(context.Background(), uuid.New(), domain.ComponentRoleBackend), "a backend has no screens to theme")
	plain := bootstrapDescription("api", domain.ComponentRoleBackend, domain.NewRepositoryRequest{Scaffold: true}, nil, nil)
	assert.NotContains(t, plain, "## The design system")
}

func TestARepositoryWithoutAProjectBaseInheritsNothing(t *testing.T) {
	s := &Service{}
	s.SetDesignSystems(fakeDesigns{eff: domain.EffectiveDesignSystem{Layer: &domain.DesignSystem{Version: 1}}})
	assert.Nil(t, s.inheritedDesignSystem(context.Background(), uuid.New(), domain.ComponentRoleMobile))
}
