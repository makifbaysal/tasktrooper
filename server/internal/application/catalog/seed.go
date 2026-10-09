package catalog

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Stored without a vector: embedding here would pace the seed past the boot deadline, so BackfillSkillEmbeddings fills them in afterwards.
func (s *Service) seedSkill(ctx context.Context, agentID uuid.UUID, req domain.CreateSkillRequest) error {
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := s.store.CreateSkill(ctx, domain.Skill{
		AgentID: agentID, Name: req.Name, Description: req.Description, Category: req.Category,
		Tags: tags, Content: req.Content, Enabled: req.Enabled,
		TechStackID: req.TechStackID,
	})
	return err
}

func (s *Service) BackfillSkillEmbeddings(ctx context.Context) (int, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	updated := 0
	for _, agent := range agents {
		skills, err := s.store.ListSkillsByAgent(ctx, agent.ID)
		if err != nil {
			return updated, fmt.Errorf("list skills of %s: %w", agent.Name, err)
		}
		for _, sk := range skills {
			if len(sk.Embedding) > 0 {
				continue
			}
			emb, err := s.embedSkill(ctx, sk.Name, sk.Description, sk.Content)
			if err != nil {
				return updated, fmt.Errorf("embed skill %s: %w", sk.Name, err)
			}
			if len(emb) == 0 {
				continue
			}
			sk.Embedding = emb
			if _, err := s.store.UpdateSkill(ctx, sk); err != nil {
				return updated, fmt.Errorf("update skill %s: %w", sk.Name, err)
			}
			updated++
		}
	}
	return updated, nil
}
