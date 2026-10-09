package indexer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeEmbeddingResolver struct {
	model string
	dims  int
	err   error
}

func (f *fakeEmbeddingResolver) ResolvedEmbedding(context.Context) (string, int, error) {
	return f.model, f.dims, f.err
}

func newProvenanceService(t *testing.T, store *fakeIndexStore, llm *fakeLLM) *indexer.Service {
	return newProvenanceServiceWithModel(t, store, llm, "configured-embed-model")
}

func newProvenanceServiceWithModel(t *testing.T, store *fakeIndexStore, llm *fakeLLM, model string) *indexer.Service {
	t.Helper()
	return indexer.NewService(
		store,
		llm,
		mapper.NewService(domain.MappingConfig{Enabled: true, MaxFiles: 50, TreeMaxDepth: 4}),
		chunker.DefaultRegistry(),
		domain.IndexerConfig{Enabled: true, TopK: 5, ReindexOnChange: true},
		domain.GraphConfig{Enabled: true},
		model,
	)
}

func TestIndexPassRecordsWhatItEmbeddedWith(t *testing.T) {
	store := newFakeIndexStore()
	llm := &fakeLLM{embedFn: func(context.Context, string, string) ([]float32, error) {
		return make([]float32, 384), nil
	}}
	svc := newProvenanceService(t, store, llm)

	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "nomic-embed-text-v1.5"})

	sessionID := uuid.New()
	idx, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idx.EmbeddingModel != "nomic-embed-text-v1.5" {
		t.Fatalf("returned index recorded model %q", idx.EmbeddingModel)
	}
	if idx.EmbeddingDims != 384 {
		t.Fatalf("returned index recorded %d dimensions, want the 384 the model actually returned", idx.EmbeddingDims)
	}

	stored, err := store.GetIndexBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored.EmbeddingModel != "nomic-embed-text-v1.5" || stored.EmbeddingDims != 384 {
		t.Fatalf("stored provenance = %q/%d, want nomic-embed-text-v1.5/384", stored.EmbeddingModel, stored.EmbeddingDims)
	}
}

func TestIndexPassFallsBackToTheConfiguredModel(t *testing.T) {
	store := newFakeIndexStore()
	svc := newProvenanceService(t, store, &fakeLLM{})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idx.EmbeddingModel != "configured-embed-model" {
		t.Fatalf("recorded model %q, want the configured one", idx.EmbeddingModel)
	}
}

func TestIndexPassSurvivesAnUnansweredResolver(t *testing.T) {
	store := newFakeIndexStore()
	svc := newProvenanceService(t, store, &fakeLLM{})
	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{err: errors.New("settings unavailable")})

	idx, err := svc.IndexSession(context.Background(), uuid.New(), mapperFixtureRoot())
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if idx.Status != domain.IndexStatusCompleted {
		t.Fatalf("status = %s, want completed", idx.Status)
	}
	if idx.EmbeddingModel != "configured-embed-model" {
		t.Fatalf("recorded model %q, want the configured one", idx.EmbeddingModel)
	}
}

func TestAnnotateEmbeddingProvenance(t *testing.T) {
	cases := []struct {
		name       string
		index      domain.WorkspaceIndex
		resolver   *fakeEmbeddingResolver
		configured string
		wantStale  bool
		wantPhrase string
	}{
		{
			name:      "same model is not stale",
			index:     domain.WorkspaceIndex{EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDims: 768},
			resolver:  &fakeEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768},
			wantStale: false,
		},
		{
			name:       "a different model is stale and says which",
			index:      domain.WorkspaceIndex{EmbeddingModel: "text-embedding-3-small", EmbeddingDims: 1536},
			resolver:   &fakeEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768},
			wantStale:  true,
			wantPhrase: "text-embedding-3-small",
		},
		{

			name:       "an index with no recorded model is stale once a model is configured",
			index:      domain.WorkspaceIndex{},
			resolver:   &fakeEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768},
			wantStale:  true,
			wantPhrase: "predates embedding provenance tracking",
		},
		{

			name:       "an index with no recorded model is not stale when nothing is configured",
			index:      domain.WorkspaceIndex{},
			resolver:   &fakeEmbeddingResolver{},
			configured: "-",
			wantStale:  false,
		},
		{

			name:       "the same name at a different size is stale",
			index:      domain.WorkspaceIndex{EmbeddingModel: "embed-v1", EmbeddingDims: 768},
			resolver:   &fakeEmbeddingResolver{model: "embed-v1", dims: 1536},
			wantStale:  true,
			wantPhrase: "1536",
		},
		{

			name:      "an unanswered resolver reports nothing",
			index:     domain.WorkspaceIndex{EmbeddingModel: "text-embedding-3-small", EmbeddingDims: 1536},
			resolver:  &fakeEmbeddingResolver{err: errors.New("settings unavailable")},
			wantStale: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			configured := "configured-embed-model"
			if tc.configured == "-" {
				configured = ""
			}
			svc := newProvenanceServiceWithModel(t, newFakeIndexStore(), &fakeLLM{}, configured)
			svc.SetEmbeddingResolver(tc.resolver)

			idx := tc.index
			svc.AnnotateEmbeddingProvenance(context.Background(), &idx)

			if idx.EmbeddingStale != tc.wantStale {
				t.Fatalf("EmbeddingStale = %v, want %v (warning %q)", idx.EmbeddingStale, tc.wantStale, idx.EmbeddingWarning)
			}
			if !tc.wantStale {
				if idx.EmbeddingWarning != "" {
					t.Fatalf("a usable index carried a warning: %q", idx.EmbeddingWarning)
				}
				return
			}
			if idx.EmbeddingWarning == "" {
				t.Fatal("a stale index carried no sentence explaining why")
			}
			if tc.wantPhrase != "" && !contains(idx.EmbeddingWarning, tc.wantPhrase) {
				t.Fatalf("warning %q does not mention %q", idx.EmbeddingWarning, tc.wantPhrase)
			}
			if !contains(idx.EmbeddingWarning, "Re-index") {
				t.Fatalf("warning %q does not say what to do about it", idx.EmbeddingWarning)
			}
		})
	}
}

func TestStaleIndexIsReEmbeddedWhole(t *testing.T) {
	store := newFakeIndexStore()
	embeds := 0
	llm := &fakeLLM{embedFn: func(context.Context, string, string) ([]float32, error) {
		embeds++
		return make([]float32, 768), nil
	}}
	svc := newProvenanceService(t, store, llm)
	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "model-a", dims: 768})

	sessionID := uuid.New()
	if _, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	first := embeds
	if first == 0 {
		t.Fatal("first pass embedded nothing")
	}

	embeds = 0
	if _, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if embeds != 0 {
		t.Fatalf("an unchanged tree re-embedded %d chunks", embeds)
	}

	svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "model-b", dims: 1536})
	embeds = 0
	idx, err := svc.IndexSession(context.Background(), sessionID, mapperFixtureRoot())
	if err != nil {
		t.Fatalf("third pass: %v", err)
	}
	if embeds != first {
		t.Fatalf("a model change re-embedded %d chunks, want all %d", embeds, first)
	}
	if idx.EmbeddingModel != "model-b" {
		t.Fatalf("index still records %q after re-embedding with model-b", idx.EmbeddingModel)
	}
}

func TestABranchSeededFromItsBaseCarriesTheBaseProvenance(t *testing.T) {
	tests := []struct {
		name       string
		nowModel   string
		wantEmbeds bool
	}{
		{"identical tree, same model", "model-a", false},
		{"identical tree, model changed since the base", "model-b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := newFakeIndexStore()
			embeds := 0
			llm := &fakeLLM{embedFn: func(context.Context, string, string) ([]float32, error) {
				embeds++
				return make([]float32, 768), nil
			}}
			svc := newProvenanceService(t, store, llm)
			svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: "model-a", dims: 768})
			repo := uuid.New()
			if _, err := svc.IndexProject(ctx, repo, mapperFixtureRoot()); err != nil {
				t.Fatalf("base pass: %v", err)
			}
			svc.SetEmbeddingResolver(&fakeEmbeddingResolver{model: tt.nowModel, dims: 768})
			embeds = 0

			idx, err := svc.IndexBranch(ctx, repo, "feature/same", mapperFixtureRoot())

			if err != nil {
				t.Fatalf("branch pass: %v", err)
			}
			if (embeds > 0) != tt.wantEmbeds {
				t.Fatalf("the branch pass embedded %d chunks, want embedding: %v", embeds, tt.wantEmbeds)
			}
			stored, err := store.GetIndexByProjectBranch(ctx, repo, "feature/same")
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			for _, got := range []domain.WorkspaceIndex{idx, stored} {
				if got.EmbeddingModel != tt.nowModel || got.EmbeddingDims != 768 {
					t.Fatalf("branch index records %q/%d, want %s/768", got.EmbeddingModel, got.EmbeddingDims, tt.nowModel)
				}
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
