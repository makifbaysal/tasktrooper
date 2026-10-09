package catalog

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// SetShippedSkillVectors lets a skill whose text the catalog shipped a vector
// for skip the embedder. engine says what this install embeds with now; a
// shipped vector is used only when its model and engine are exactly that.
func (s *Service) SetShippedSkillVectors(vectors port.SkillVectorSource, engine port.EmbeddingSourceResolver) {
	s.shippedVectors = vectors
	s.embeddingEngine = engine
}

func (s *Service) embedSkill(ctx context.Context, name, description, content string) ([]float32, error) {
	text := domain.SkillEmbeddingText(name, description, content)
	if vec, ok := s.shippedSkillVector(ctx, text); ok {
		return vec, nil
	}
	return s.llm.Embed(ctx, text, s.embeddingModel)
}

func (s *Service) shippedSkillVector(ctx context.Context, text string) ([]float32, bool) {
	if s.shippedVectors == nil || s.embeddingEngine == nil {
		return nil, false
	}
	set, err := s.shippedVectors.SkillVectors(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("the catalog's skill vectors could not be read; embedding skills live")
		return nil, false
	}
	if set.Len() == 0 {
		return nil, false
	}
	model, source, err := s.embeddingEngine.ResolvedEmbeddingSource(ctx)
	if err != nil {
		return nil, false
	}
	return set.Lookup(model, source, text)
}
