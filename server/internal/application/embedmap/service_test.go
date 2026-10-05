package embedmap_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/embedmap"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type stubStore struct {
	stats     port.EmbeddingFileStats
	repos     []port.EmbeddingRepositorySource
	total     int
	chunks    []port.EmbeddingChunk
	err       error
	lastLimit int
	lastIndex uuid.UUID
}

func (s *stubStore) FileStats(context.Context) (port.EmbeddingFileStats, error) {
	return s.stats, s.err
}

func (s *stubStore) ListRepositorySources(context.Context) ([]port.EmbeddingRepositorySource, error) {
	return s.repos, s.err
}

func (s *stubStore) RepositorySource(_ context.Context, id uuid.UUID) (port.EmbeddingRepositorySource, error) {
	for _, r := range s.repos {
		if r.RepositoryID == id {
			return r, nil
		}
	}
	return port.EmbeddingRepositorySource{}, port.ErrNotFound
}

func (s *stubStore) SampleFileChunks(_ context.Context, limit int) (int, []port.EmbeddingChunk, error) {
	s.lastLimit = limit
	return s.total, s.chunks, s.err
}

func (s *stubStore) SampleCodeChunks(_ context.Context, indexID uuid.UUID, limit int) (int, []port.EmbeddingChunk, error) {
	s.lastIndex = indexID
	s.lastLimit = limit
	return s.total, s.chunks, s.err
}

func (s *stubStore) ChunkEmbeddings(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID][]float32, error) {
	return map[uuid.UUID][]float32{}, s.err
}

func arcChunks(n, dim int) []port.EmbeddingChunk {
	out := make([]port.EmbeddingChunk, n)
	for i := range out {
		v := make([]float32, dim)
		for j := range v {
			v[j] = float32(math.Cos(float64((i+1)*(j+2))) + 1.5)
		}
		out[i] = port.EmbeddingChunk{
			ID:         uuid.NewSHA1(uuid.Nil, []byte{byte(i)}).String(),
			GroupID:    "apps/web/src/api.ts",
			GroupLabel: "apps/web/src/api.ts",
			ChunkIndex: i,
			Content:    "  export const\tfoo =\n  1 ",
			Language:   "typescript",
			Symbol:     "foo",
			Embedding:  v,
		}
	}
	return out
}

func TestSourcesReportsFilesAndRepositories(t *testing.T) {
	indexedAt := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	repoID, indexID := uuid.New(), uuid.New()
	svc := embedmap.New(&stubStore{
		stats: port.EmbeddingFileStats{ChunkCount: 412, DocumentCount: 7},
		repos: []port.EmbeddingRepositorySource{{
			RepositoryID: repoID, Name: "local-llm", Branch: "main", IndexID: indexID,
			ChunkCount: 8321, FileCount: 640, IndexedAt: &indexedAt,
		}},
	})

	sources, err := svc.Sources(context.Background())

	require.NoError(t, err)
	require.True(t, sources.Files.Available)
	require.Equal(t, 412, sources.Files.ChunkCount)
	require.Equal(t, 7, sources.Files.DocumentCount)
	require.Len(t, sources.Repositories, 1)
	require.Equal(t, repoID.String(), sources.Repositories[0].ID)
	require.Equal(t, indexID.String(), sources.Repositories[0].IndexID)
	require.Equal(t, "main", sources.Repositories[0].Branch)
	require.Equal(t, &indexedAt, sources.Repositories[0].IndexedAt)
}

func TestSourcesEmptyCorpusIsNotAvailable(t *testing.T) {
	svc := embedmap.New(&stubStore{})

	sources, err := svc.Sources(context.Background())

	require.NoError(t, err)
	require.False(t, sources.Files.Available)
	require.Empty(t, sources.Repositories)
}

func TestBuildFilesProjectsEveryChunk(t *testing.T) {
	store := &stubStore{total: 12, chunks: arcChunks(12, 64)}
	svc := embedmap.New(store)

	res, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceFiles})

	require.NoError(t, err)
	require.Equal(t, embedmap.SourceFiles, res.Source)
	require.Empty(t, res.RepositoryID)
	require.Equal(t, 12, res.Total)
	require.Equal(t, 12, res.Sampled)
	require.False(t, res.Truncated)
	require.Equal(t, embedmap.DefaultDims, res.Dimensions)
	require.Len(t, res.Points, 12)
	for _, p := range res.Points {
		require.Len(t, p.Vector, res.Dimensions)
	}

	require.Equal(t, "export const foo = 1", res.Points[0].Snippet)
}

func TestBuildCodeCarriesBranchAndRepository(t *testing.T) {
	repoID, indexID := uuid.New(), uuid.New()
	store := &stubStore{
		repos:  []port.EmbeddingRepositorySource{{RepositoryID: repoID, IndexID: indexID, Branch: "main"}},
		total:  9000,
		chunks: arcChunks(20, 32),
	}
	svc := embedmap.New(store)

	res, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceCode, RepositoryID: repoID})

	require.NoError(t, err)
	require.Equal(t, embedmap.SourceCode, res.Source)
	require.Equal(t, repoID.String(), res.RepositoryID)
	require.Equal(t, "main", res.Branch)
	require.Equal(t, indexID, store.lastIndex)
	require.Equal(t, 9000, res.Total)
	require.Equal(t, 20, res.Sampled)
	require.True(t, res.Truncated, "a source larger than the limit was sampled")

	require.Equal(t, 32, res.Dimensions)
	require.Equal(t, "typescript", res.Points[0].Language)
	require.Equal(t, "foo", res.Points[0].Symbol)
	require.Equal(t, "apps/web/src/api.ts", res.Points[0].GroupLabel)
}

func TestBuildClampsLimitAndDims(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		dims      int
		wantLimit int
		wantDims  int
	}{
		{name: "defaults when absent", wantLimit: embedmap.DefaultLimit, wantDims: embedmap.DefaultDims},
		{name: "below floor", limit: 1, dims: 1, wantLimit: embedmap.MinLimit, wantDims: embedmap.MinDims},
		{name: "above ceiling", limit: 999999, dims: 4096, wantLimit: embedmap.MaxLimit, wantDims: embedmap.MaxDims},
		{name: "negative", limit: -5, dims: -5, wantLimit: embedmap.MinLimit, wantDims: embedmap.MinDims},
		{name: "in range", limit: 750, dims: 12, wantLimit: 750, wantDims: 12},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubStore{total: 40, chunks: arcChunks(40, 256)}
			svc := embedmap.New(store)

			res, err := svc.Build(context.Background(), embedmap.Query{
				Source: embedmap.SourceFiles, Limit: tc.limit, Dims: tc.dims,
			})

			require.NoError(t, err)
			require.Equal(t, tc.wantLimit, store.lastLimit, "clamped limit is what reaches the store")
			require.Equal(t, tc.wantDims, res.Dimensions)
			for _, p := range res.Points {
				require.Len(t, p.Vector, tc.wantDims)
			}
		})
	}
}

func TestBuildEmptySourceIsNotAnError(t *testing.T) {
	svc := embedmap.New(&stubStore{})

	res, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceFiles})

	require.NoError(t, err)
	require.Equal(t, 0, res.Total)
	require.Equal(t, 0, res.Sampled)
	require.False(t, res.Truncated)
	require.NotNil(t, res.Points, "an empty map serializes as [] not null")
	require.Empty(t, res.Points)
}

func TestBuildUnindexedRepositoryIsAnEmptyMap(t *testing.T) {
	svc := embedmap.New(&stubStore{})

	res, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceCode, RepositoryID: uuid.New()})

	require.NoError(t, err)
	require.Empty(t, res.Points)
	require.Equal(t, 0, res.Total)
}

func TestBuildDropsUnusableEmbeddings(t *testing.T) {
	chunks := arcChunks(4, 8)
	chunks[1].Embedding = []float32{0, 0, 0, 0, 0, 0, 0, 0}
	chunks[2].Embedding = []float32{1, 2, 3}
	store := &stubStore{total: 4, chunks: chunks}
	svc := embedmap.New(store)

	res, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceFiles})

	require.NoError(t, err)
	require.Equal(t, 4, res.Total, "total counts what is stored")
	require.Equal(t, 2, res.Sampled, "sampled counts what could be projected")
	require.Len(t, res.Points, 2)
	require.Equal(t, chunks[0].ID, res.Points[0].ID)
	require.Equal(t, chunks[3].ID, res.Points[1].ID, "metadata stays aligned with the surviving rows")
}

func TestBuildRejectsBadQueries(t *testing.T) {
	svc := embedmap.New(&stubStore{})

	_, err := svc.Build(context.Background(), embedmap.Query{Source: "workspace"})
	require.ErrorIs(t, err, embedmap.ErrInvalidSource)

	_, err = svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceCode})
	require.ErrorIs(t, err, embedmap.ErrRepositoryRequired)
}

func TestBuildPropagatesStoreFailure(t *testing.T) {
	boom := errors.New("connection refused")
	svc := embedmap.New(&stubStore{err: boom})

	_, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceFiles})

	require.ErrorIs(t, err, boom)
}

func TestServiceWithoutStoreIsUnavailable(t *testing.T) {
	svc := embedmap.New(nil)

	_, err := svc.Sources(context.Background())
	require.ErrorIs(t, err, embedmap.ErrUnavailable)

	_, err = svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceFiles})
	require.ErrorIs(t, err, embedmap.ErrUnavailable)
}

type stubEmbeddingResolver struct {
	model string
	dims  int
	err   error
}

func (s *stubEmbeddingResolver) ResolvedEmbedding(context.Context) (string, int, error) {
	return s.model, s.dims, s.err
}

func TestSourcesLabelAStaleIndex(t *testing.T) {
	staleID := uuid.New()
	freshID := uuid.New()
	store := &stubStore{repos: []port.EmbeddingRepositorySource{
		{
			RepositoryID: staleID, Name: "legacy", IndexID: uuid.New(), ChunkCount: 10,
			EmbeddingModel: "text-embedding-3-small", EmbeddingDims: 1536,
		},
		{
			RepositoryID: freshID, Name: "current", IndexID: uuid.New(), ChunkCount: 10,
			EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDims: 768,
		},
	}}
	svc := embedmap.New(store)
	svc.SetEmbeddingResolver(&stubEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768})

	sources, err := svc.Sources(context.Background())
	require.NoError(t, err)
	require.Len(t, sources.Repositories, 2)

	stale := sources.Repositories[0]
	require.Equal(t, staleID.String(), stale.ID)
	require.True(t, stale.EmbeddingStale, "an index built by another model was presented as usable")
	require.Contains(t, stale.EmbeddingWarning, "text-embedding-3-small")
	require.Contains(t, stale.EmbeddingWarning, "Re-index")

	fresh := sources.Repositories[1]
	require.Equal(t, freshID.String(), fresh.ID)
	require.False(t, fresh.EmbeddingStale, "a current index was marked stale")
	require.Empty(t, fresh.EmbeddingWarning)
}

func TestSourcesClaimNothingWithoutAResolver(t *testing.T) {
	store := &stubStore{repos: []port.EmbeddingRepositorySource{
		{RepositoryID: uuid.New(), Name: "legacy", IndexID: uuid.New(), ChunkCount: 10, EmbeddingModel: "old-model"},
	}}
	svc := embedmap.New(store)

	sources, err := svc.Sources(context.Background())
	require.NoError(t, err)
	require.Len(t, sources.Repositories, 1)
	require.False(t, sources.Repositories[0].EmbeddingStale)

	svc.SetEmbeddingResolver(&stubEmbeddingResolver{err: errors.New("settings unavailable")})
	sources, err = svc.Sources(context.Background())
	require.NoError(t, err)
	require.False(t, sources.Repositories[0].EmbeddingStale, "a failed settings lookup became a staleness verdict")
}

func TestBuildCarriesTheStalenessVerdict(t *testing.T) {
	repoID := uuid.New()
	store := &stubStore{
		repos: []port.EmbeddingRepositorySource{{
			RepositoryID: repoID, Name: "legacy", IndexID: uuid.New(), ChunkCount: 4,
			EmbeddingModel: "text-embedding-3-small", EmbeddingDims: 1536,
		}},
		total:  4,
		chunks: arcChunks(4, 8),
	}
	svc := embedmap.New(store)
	svc.SetEmbeddingResolver(&stubEmbeddingResolver{model: "nomic-embed-text-v1.5", dims: 768})

	out, err := svc.Build(context.Background(), embedmap.Query{Source: embedmap.SourceCode, RepositoryID: repoID})
	require.NoError(t, err)
	require.NotEmpty(t, out.Points, "a stale map must still be drawn; refusing leaves an empty panel and no explanation")
	require.True(t, out.EmbeddingStale)
	require.Contains(t, out.EmbeddingWarning, "text-embedding-3-small")
}
