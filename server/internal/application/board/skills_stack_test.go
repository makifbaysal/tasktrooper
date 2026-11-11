package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type stackModel struct {
	ProjectModel
	stacks []domain.ComponentStack
	err    error
	asked  []uuid.UUID
}

func (m *stackModel) ComponentStacks(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]domain.ComponentStack, error) {
	m.asked = ids
	return m.stacks, m.err
}

func TestSkillsInProjectStack(t *testing.T) {
	goStack := domain.TechStack{ID: uuid.New(), Name: "Go"}
	rustStack := domain.TechStack{ID: uuid.New(), Name: "Rust"}
	goID, rustID := goStack.ID, rustStack.ID
	skills := []domain.Skill{{Name: "gen"}, {Name: "go", TechStackID: &goID}, {Name: "rust", TechStackID: &rustID}}
	stacks := []domain.TechStack{goStack, rustStack}
	comp := uuid.New()
	job := RunJob{Task: domain.BoardTask{ComponentID: &comp}}

	m := &stackModel{stacks: []domain.ComponentStack{{Languages: []domain.StackItem{{Name: "Go"}}}}}
	r := &Runner{projectModel: m}
	got := r.skillsInProjectStack(context.Background(), job, skills, stacks)
	assert.Len(t, got, 2)
	assert.Equal(t, []uuid.UUID{comp}, m.asked)

	m.err = errors.New("down")
	assert.Len(t, r.skillsInProjectStack(context.Background(), job, skills, stacks), 3)
	assert.Len(t, (&Runner{}).skillsInProjectStack(context.Background(), job, skills, stacks), 3)
}
