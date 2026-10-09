package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fixedVectors struct {
	set *domain.SkillVectorSet
	err error
}

func (f fixedVectors) SkillVectors(context.Context) (*domain.SkillVectorSet, error) {
	return f.set, f.err
}

type fixedEngine struct{ model, source string }

func (f fixedEngine) ResolvedEmbeddingSource(context.Context) (string, string, error) {
	return f.model, f.source, nil
}

var shippedVector = []float32{0.5, 0.25, 0.125}

// shippedFor ships a vector for the first skill of every template
// seedTemplatesWithSkills writes, and none for the second.
func shippedFor(names ...string) *domain.SkillVectorSet {
	set := domain.NewSkillVectorSet(domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8, len(shippedVector))
	for _, name := range names {
		set.Put(domain.SkillVectorKey(domain.SkillEmbeddingText(name+"-skill-1", "d", "body")), shippedVector)
	}
	return set
}

func agentsWithUnembeddedSkills(t *testing.T, store *memCatalogStore, names ...string) {
	t.Helper()
	templates := &memTemplateStore{}
	setup := NewService(store, &notReadyEmbedLLM{}, "")
	setup.SetTemplateStore(templates)
	seedTemplatesWithSkills(t, templates, names...)
	for _, tpl := range templates.templates {
		_, err := setup.CreateAgentFromTemplate(context.Background(), tpl.ID, domain.CreateAgentRequest{})
		require.NoError(t, err)
	}
}

func TestBackfillTakesTheShippedVectorOnlyFromTheEngineThatMadeIt(t *testing.T) {
	tests := []struct {
		name        string
		engine      fixedEngine
		vectors     fixedVectors
		wantShipped int
	}{
		{"same model and engine", fixedEngine{domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8}, fixedVectors{set: shippedFor("qa-agent")}, 1},
		{"same model, another engine", fixedEngine{domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceTEI}, fixedVectors{set: shippedFor("qa-agent")}, 0},
		{"engine not known", fixedEngine{domain.PinnedLocalEmbeddingModel, ""}, fixedVectors{set: shippedFor("qa-agent")}, 0},
		{"another model", fixedEngine{"text-embedding-3-small", domain.EmbeddingSourceONNXInt8}, fixedVectors{set: shippedFor("qa-agent")}, 0},
		{"catalog shipped none", fixedEngine{domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8}, fixedVectors{}, 0},
		{"vectors unreadable", fixedEngine{domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8}, fixedVectors{err: errors.New("bad file")}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newMemCatalogStore()
			agentsWithUnembeddedSkills(t, store, "qa-agent")
			live := &readyEmbedLLM{}
			svc := NewService(store, live, "")
			svc.SetShippedSkillVectors(tt.vectors, tt.engine)

			updated, err := svc.BackfillSkillEmbeddings(ctx)

			require.NoError(t, err)
			require.Equal(t, 2, updated)
			require.Equal(t, int32(2-tt.wantShipped), live.calls.Load())
			agents, _ := store.ListAgents(ctx)
			skills, _ := store.ListSkillsByAgent(ctx, agents[0].ID)
			shipped := 0
			for _, sk := range skills {
				if len(sk.Embedding) == len(shippedVector) && sk.Embedding[0] == shippedVector[0] {
					shipped++
				}
			}
			require.Equal(t, tt.wantShipped, shipped)
		})
	}
}

func TestCreatingASkillTheCatalogShippedNeverWaitsOnTheEmbedder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store := newMemCatalogStore()
	agentsWithUnembeddedSkills(t, store, "backend-developer")
	agents, _ := store.ListAgents(ctx)
	svc := NewService(store, &notReadyEmbedLLM{}, "")
	svc.SetShippedSkillVectors(fixedVectors{set: shippedFor("ship")}, fixedEngine{domain.PinnedLocalEmbeddingModel, domain.EmbeddingSourceONNXInt8})

	created, err := svc.CreateSkillForAgent(ctx, agents[0].ID, domain.CreateSkillRequest{
		Name: "ship-skill-1", Description: "d", Content: "body", Enabled: true,
	})

	require.NoError(t, err)
	require.Equal(t, shippedVector, created.Embedding)
}
