package postgres_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
)

// IndexVectorCacheSuite pins the cached in-memory chunk search to the
// original single-SELECT search it replaced, and the cache to every write
// that can make it stale. The store runs without pgvector capabilities, which
// is how the embedded database runs it.
type IndexVectorCacheSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	repos  *postgres.RepositoryStore
	rng    *rand.Rand
}

func TestIndexVectorCacheSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(IndexVectorCacheSuite))
}

func (s *IndexVectorCacheSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.repos = postgres.NewRepositoryStore(s.db)
}

func (s *IndexVectorCacheSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *IndexVectorCacheSuite) SetupTest() {
	s.rng = rand.New(rand.NewPCG(7, 11))
}

const cacheTestDims = 8

func (s *IndexVectorCacheSuite) randomVector(dims int) []float32 {
	v := make([]float32, dims)
	for i := range v {
		v[i] = float32(s.rng.NormFloat64())
	}
	return v
}

func (s *IndexVectorCacheSuite) newRepository() domain.Repository {
	name := "index-vector-cache-" + uuid.NewString()
	repo, err := s.repos.Create(s.ctx, name, "", "/tmp/"+name, "", "")
	s.Require().NoError(err)
	return repo
}

func (s *IndexVectorCacheSuite) newIndex(store *postgres.IndexStore) domain.WorkspaceIndex {
	repo := s.newRepository()
	idx, err := store.CreateProjectIndex(s.ctx, repo.ID, repo.RootPath, "")
	s.Require().NoError(err)
	return idx
}

// newBranchIndex gives the branch index a repository of its own:
// idx_workspace_indexes_repository is still UNIQUE (repository_id) (migration
// 060 dropped it under its pre-022 name), so a repository holds one index row.
func (s *IndexVectorCacheSuite) newBranchIndex(store *postgres.IndexStore, branch string) domain.WorkspaceIndex {
	repo := s.newRepository()
	idx, err := store.CreateProjectBranchIndex(s.ctx, repo.ID, branch, repo.RootPath, "")
	s.Require().NoError(err)
	return idx
}

func codeChunk(file string, n int, embedding []float32) domain.WorkspaceChunk {
	return domain.WorkspaceChunk{
		FilePath:   file,
		SymbolName: fmt.Sprintf("F%02d", n),
		Kind:       "function",
		StartLine:  n*10 + 1,
		EndLine:    n*10 + 5,
		Language:   "go",
		Signature:  fmt.Sprintf("func F%02d() int", n),
		Content:    fmt.Sprintf("func F%02d() int { return %d }", n, n),
		Embedding:  embedding,
	}
}

func (s *IndexVectorCacheSuite) saveRandomChunks(store *postgres.IndexStore, indexID uuid.UUID, files ...string) {
	chunks := make([]domain.WorkspaceChunk, len(files))
	for i, file := range files {
		chunks[i] = codeChunk(file, i, s.randomVector(cacheTestDims))
	}
	s.Require().NoError(store.SaveChunks(s.ctx, indexID, chunks))
}

// insertRawEmbedding writes an embedding SaveChunks would never produce, which
// rows written by earlier builds do contain.
func (s *IndexVectorCacheSuite) insertRawEmbedding(indexID uuid.UUID, n int, embeddingJSON string) {
	ch := codeChunk(fmt.Sprintf("pkg/f%02d.go", n), n, nil)
	_, err := s.pool.Exec(s.ctx, `
		INSERT INTO workspace_chunks (index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::text::jsonb)
	`, indexID, ch.FilePath, ch.SymbolName, ch.Kind, ch.StartLine, ch.EndLine, ch.Language, ch.Signature, ch.Content, embeddingJSON)
	s.Require().NoError(err)
}

func (s *IndexVectorCacheSuite) searchMatchesLegacy(store *postgres.IndexStore, indexID uuid.UUID, query []float32, topK int) []domain.WorkspaceChunk {
	s.T().Helper()
	want, err := store.LegacySearchChunksInMemory(s.ctx, indexID, query, topK)
	s.Require().NoError(err)
	got, err := store.SearchChunksByIndex(s.ctx, indexID, query, topK)
	s.Require().NoError(err)
	s.Require().Equal(want, got)
	return got
}

func filePaths(chunks []domain.WorkspaceChunk) []string {
	out := make([]string, len(chunks))
	for i, ch := range chunks {
		out[i] = ch.FilePath
	}
	return out
}

type cacheTestQuery struct {
	name      string
	embedding []float32
}

// seedEquivalenceFixture writes 50 rows one at a time, so the physical order —
// which is the order the unordered SELECT returns and therefore what decides
// tie order — is the layout order. File paths sort in the same order, so an
// index scan on (index_id, file_path) would agree with a heap scan.
func (s *IndexVectorCacheSuite) seedEquivalenceFixture(store *postgres.IndexStore, indexID uuid.UUID) []cacheTestQuery {
	tieA := s.randomVector(cacheTestDims)
	tieB := s.randomVector(cacheTestDims)
	opposite := make([]float32, cacheTestDims)
	for i, v := range tieA {
		opposite[i] = -v
	}
	nearB := make([]float32, cacheTestDims)
	for i, v := range tieB {
		nearB[i] = v + 0.05*float32(s.rng.NormFloat64())
	}

	layout := []string{
		"rand", "rand", "tieA", "string", "rand", "tieB", "zero", "tieA", "rand", "null",
		"tieB", "rand", "tieA", "short", "rand", "empty", "tieB", "rand", "opposite", "tieA",
		"object", "rand", "tieB", "rand", "zero", "tieA", "rand", "mixed", "tieB", "rand",
		"rand", "tieA", "short", "rand", "overflow", "tieB", "rand", "opposite", "tieA", "rand",
		"null", "rand", "tieB", "rand", "rand", "empty", "rand", "tieA", "rand", "rand",
	}
	s.Require().Len(layout, 50)
	raw := map[string]string{
		"string":   `"not a vector"`,
		"object":   `{"x": 1}`,
		"mixed":    `[1, "two", 3]`,
		"overflow": `[1e39, 0, 0, 0, 0, 0, 0, 0]`,
		"null":     `null`,
		"empty":    `[]`,
	}
	for n, kind := range layout {
		if embeddingJSON, ok := raw[kind]; ok {
			s.insertRawEmbedding(indexID, n, embeddingJSON)
			continue
		}
		var emb []float32
		switch kind {
		case "tieA":
			emb = tieA
		case "tieB":
			emb = tieB
		case "opposite":
			emb = opposite
		case "zero":
			emb = make([]float32, cacheTestDims)
		case "short":
			emb = s.randomVector(5)
		default:
			emb = s.randomVector(cacheTestDims)
		}
		s.Require().NoError(store.SaveChunks(s.ctx, indexID, []domain.WorkspaceChunk{codeChunk(fmt.Sprintf("pkg/f%02d.go", n), n, emb)}))
	}

	return []cacheTestQuery{
		{name: "exact tie group", embedding: tieA},
		{name: "near tie group", embedding: nearB},
		{name: "random", embedding: s.randomVector(cacheTestDims)},
		{name: "zero query ties every row", embedding: make([]float32, cacheTestDims)},
		{name: "other dimension", embedding: s.randomVector(5)},
	}
}

func (s *IndexVectorCacheSuite) TestCachedSearchMatchesTheOriginalColdAndWarm() {
	writer := postgres.NewIndexStore(s.db)
	idx := s.newIndex(writer)
	queries := s.seedEquivalenceFixture(writer, idx.ID)

	all, err := writer.LegacySearchChunksInMemory(s.ctx, idx.ID, queries[3].embedding, 100)
	s.Require().NoError(err)
	s.Require().Len(all, 46, "the four undecodable rows are skipped, JSON null and [] are not")
	var nilEmbeddings, emptyEmbeddings int
	for _, ch := range all {
		switch {
		case ch.Embedding == nil:
			nilEmbeddings++
		case len(ch.Embedding) == 0:
			emptyEmbeddings++
		}
	}
	s.Require().Equal(2, nilEmbeddings)
	s.Require().Equal(2, emptyEmbeddings)
	exact, err := writer.LegacySearchChunksInMemory(s.ctx, idx.ID, queries[0].embedding, 9)
	s.Require().NoError(err)
	for i := 1; i < 8; i++ {
		s.Require().Equal(exact[0].Score, exact[i].Score, "the tie group must tie exactly for the cuts to straddle it")
	}
	s.Require().Greater(exact[7].Score, exact[8].Score)

	for _, q := range queries {
		for _, topK := range []int{0, 1, 3, 5, 8, 9, 20, 46, 47, 100} {
			s.Run(fmt.Sprintf("%s top %d", q.name, topK), func() {
				store := postgres.NewIndexStore(s.db)
				want, err := store.LegacySearchChunksInMemory(s.ctx, idx.ID, q.embedding, topK)
				s.Require().NoError(err)

				cold, err := store.SearchChunksByIndex(s.ctx, idx.ID, q.embedding, topK)
				s.Require().NoError(err)
				warm, err := store.SearchChunksByIndex(s.ctx, idx.ID, q.embedding, topK)
				s.Require().NoError(err)

				s.Equal(want, cold)
				s.Equal(want, warm)
				s.EqualValues(min(topK, 1), store.ChunkVectorLoads())
			})
		}
	}
}

func (s *IndexVectorCacheSuite) TestReturnedEmbeddingsAreCallerOwned() {
	store := postgres.NewIndexStore(s.db)
	idx := s.newIndex(store)
	s.saveRandomChunks(store, idx.ID, "a.go", "a.go", "b.go", "c.go")
	query := s.randomVector(cacheTestDims)

	first := s.searchMatchesLegacy(store, idx.ID, query, 3)
	for i := range first {
		for j := range first[i].Embedding {
			first[i].Embedding[j] = 42
		}
	}

	s.searchMatchesLegacy(store, idx.ID, query, 3)
	s.EqualValues(1, store.ChunkVectorLoads())
}

func (s *IndexVectorCacheSuite) TestEveryChunkWriteInvalidatesTheIndexItTouches() {
	store := postgres.NewIndexStore(s.db)
	idx := s.newIndex(store)
	s.saveRandomChunks(store, idx.ID, "a.go", "a.go", "b.go", "b.go", "b.go")
	query := s.randomVector(cacheTestDims)

	s.searchMatchesLegacy(store, idx.ID, query, 4)
	s.searchMatchesLegacy(store, idx.ID, query, 4)
	s.EqualValues(1, store.ChunkVectorLoads(), "a repeated search must be served from the cache")
	s.True(store.ChunkVectorCached(idx.ID))

	s.Require().NoError(store.SaveChunks(s.ctx, idx.ID, []domain.WorkspaceChunk{codeChunk("c.go", 9, query)}))
	s.False(store.ChunkVectorCached(idx.ID))
	got := s.searchMatchesLegacy(store, idx.ID, query, 4)
	s.Equal("c.go", got[0].FilePath)

	s.Require().NoError(store.DeleteFileData(s.ctx, idx.ID, []string{"c.go"}))
	got = s.searchMatchesLegacy(store, idx.ID, query, 4)
	s.NotContains(filePaths(got), "c.go")

	s.Require().NoError(store.DeleteFilesNotIn(s.ctx, idx.ID, []string{"a.go"}))
	got = s.searchMatchesLegacy(store, idx.ID, query, 4)
	s.Equal([]string{"a.go", "a.go"}, filePaths(got))

	branch := s.newBranchIndex(store, "feature/copy")
	s.Empty(s.searchMatchesLegacy(store, branch.ID, query, 4))
	s.True(store.ChunkVectorCached(branch.ID), "an empty index is cached like any other")
	s.Require().NoError(store.CopyIndexData(s.ctx, idx.ID, branch.ID))
	got = s.searchMatchesLegacy(store, branch.ID, query, 4)
	s.Equal([]string{"a.go", "a.go"}, filePaths(got))

	s.Require().NoError(store.DeleteIndexData(s.ctx, idx.ID))
	s.Empty(s.searchMatchesLegacy(store, idx.ID, query, 4))

	s.Require().NoError(store.DeleteIndex(s.ctx, branch.ID))
	s.False(store.ChunkVectorCached(branch.ID))
	s.Empty(s.searchMatchesLegacy(store, branch.ID, query, 4))

	s.EqualValues(8, store.ChunkVectorLoads(), "each write must cost exactly one reload of the index it touched")
}

func (s *IndexVectorCacheSuite) TestRecreatingAnIndexDropsOnlyTheReplacedVectors() {
	store := postgres.NewIndexStore(s.db)
	idx := s.newIndex(store)
	repoID := *idx.ProjectID
	s.saveRandomChunks(store, idx.ID, "a.go", "b.go")
	branch := s.newBranchIndex(store, "feature/redo")
	s.saveRandomChunks(store, branch.ID, "a.go", "b.go")
	query := s.randomVector(cacheTestDims)

	s.Len(s.searchMatchesLegacy(store, idx.ID, query, 5), 2)
	s.Len(s.searchMatchesLegacy(store, branch.ID, query, 5), 2)

	redone, err := store.CreateProjectBranchIndex(s.ctx, *branch.ProjectID, "feature/redo", branch.RootPath, "")
	s.Require().NoError(err)
	s.False(store.ChunkVectorCached(branch.ID))
	s.True(store.ChunkVectorCached(idx.ID), "replacing one branch index must leave the others cached")
	s.Empty(s.searchMatchesLegacy(store, branch.ID, query, 5))
	s.Empty(s.searchMatchesLegacy(store, redone.ID, query, 5))

	replacement, err := store.CreateProjectIndex(s.ctx, repoID, idx.RootPath, "")
	s.Require().NoError(err)
	s.False(store.ChunkVectorCached(idx.ID))
	s.Empty(s.searchMatchesLegacy(store, idx.ID, query, 5))
	s.Empty(s.searchMatchesLegacy(store, replacement.ID, query, 5))

	session, err := postgres.NewSessionStore(s.db).Create(s.ctx, "index-vector-cache", "", "/tmp/index-vector-cache-session", nil, nil, nil)
	s.Require().NoError(err)
	sessionIdx, err := store.CreateIndex(s.ctx, session.ID, "/tmp/index-vector-cache-session", "")
	s.Require().NoError(err)
	s.saveRandomChunks(store, sessionIdx.ID, "a.go")
	s.Len(s.searchMatchesLegacy(store, sessionIdx.ID, query, 5), 1)
	_, err = store.CreateIndex(s.ctx, session.ID, "/tmp/index-vector-cache-session", "")
	s.Require().NoError(err)
	s.False(store.ChunkVectorCached(sessionIdx.ID))
	s.Empty(s.searchMatchesLegacy(store, sessionIdx.ID, query, 5))
}

func (s *IndexVectorCacheSuite) TestRowsDeletedBehindTheStoreAreNotReturned() {
	store := postgres.NewIndexStore(s.db)
	idx := s.newIndex(store)
	s.saveRandomChunks(store, idx.ID, "a.go", "b.go", "c.go", "d.go", "e.go", "f.go")
	query := s.randomVector(cacheTestDims)

	before := s.searchMatchesLegacy(store, idx.ID, query, 3)
	_, err := s.pool.Exec(s.ctx, `DELETE FROM workspace_chunks WHERE id = $1`, before[0].ID)
	s.Require().NoError(err)
	after := s.searchMatchesLegacy(store, idx.ID, query, 3)
	s.Equal(before[1:], after[:2])

	s.Require().NoError(s.repos.Delete(s.ctx, *idx.ProjectID))
	s.Empty(s.searchMatchesLegacy(store, idx.ID, query, 3))
}

func (s *IndexVectorCacheSuite) TestALoadThatRacedAWriteIsNotInstalled() {
	store := postgres.NewIndexStore(s.db)
	idx := s.newIndex(store)
	s.saveRandomChunks(store, idx.ID, "a.go", "b.go", "c.go")
	query := s.randomVector(cacheTestDims)

	installed, err := store.LoadChunkVectorsAcross(s.ctx, idx.ID, func() {
		s.Require().NoError(store.SaveChunks(s.ctx, idx.ID, []domain.WorkspaceChunk{codeChunk("late.go", 9, query)}))
	})
	s.Require().NoError(err)
	s.False(installed)
	s.False(store.ChunkVectorCached(idx.ID))
	got := s.searchMatchesLegacy(store, idx.ID, query, 1)
	s.Equal("late.go", got[0].FilePath)

	installed, err = store.LoadChunkVectorsAcross(s.ctx, idx.ID, func() {})
	s.Require().NoError(err)
	s.True(installed)
}

func (s *IndexVectorCacheSuite) TestConcurrentColdSearchesLoadTheIndexOnce() {
	writer := postgres.NewIndexStore(s.db)
	idx := s.newIndex(writer)
	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("pkg/f%02d.go", i)
	}
	s.saveRandomChunks(writer, idx.ID, files...)
	query := s.randomVector(cacheTestDims)
	want, err := writer.LegacySearchChunksInMemory(s.ctx, idx.ID, query, 5)
	s.Require().NoError(err)

	store := postgres.NewIndexStore(s.db)
	const searchers = 16
	results := make([][]domain.WorkspaceChunk, searchers)
	errs := make([]error, searchers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range searchers {
		wg.Go(func() {
			<-start
			results[i], errs[i] = store.SearchChunksByIndex(s.ctx, idx.ID, query, 5)
		})
	}
	close(start)
	wg.Wait()

	for i := range searchers {
		s.Require().NoError(errs[i])
		s.Equal(want, results[i])
	}
	s.EqualValues(1, store.ChunkVectorLoads())
}

func (s *IndexVectorCacheSuite) TestLeastRecentlyUsedIndexIsEvictedPastTheBudget() {
	writer := postgres.NewIndexStore(s.db)
	a, b, c := s.newIndex(writer), s.newIndex(writer), s.newIndex(writer)
	for _, idx := range []domain.WorkspaceIndex{a, b, c} {
		s.saveRandomChunks(writer, idx.ID, "a.go", "b.go", "c.go", "d.go", "e.go")
	}
	query := s.randomVector(cacheTestDims)

	store := postgres.NewIndexStore(s.db)
	s.searchMatchesLegacy(store, a.ID, query, 3)
	cost := store.ChunkVectorCacheUsed()
	s.Require().Positive(cost)
	store.SetChunkVectorBudget(2*cost + cost/2)

	s.searchMatchesLegacy(store, b.ID, query, 3)
	s.searchMatchesLegacy(store, a.ID, query, 3)
	s.searchMatchesLegacy(store, c.ID, query, 3)

	s.True(store.ChunkVectorCached(a.ID), "a was used after b, so b goes first")
	s.False(store.ChunkVectorCached(b.ID))
	s.True(store.ChunkVectorCached(c.ID))
	s.Equal(2*cost, store.ChunkVectorCacheUsed())
	s.EqualValues(3, store.ChunkVectorLoads())

	s.searchMatchesLegacy(store, b.ID, query, 3)
	s.EqualValues(4, store.ChunkVectorLoads())
	s.False(store.ChunkVectorCached(a.ID))
}

func (s *IndexVectorCacheSuite) TestAnIndexLargerThanTheBudgetIsSearchedUncached() {
	writer := postgres.NewIndexStore(s.db)
	idx := s.newIndex(writer)
	s.saveRandomChunks(writer, idx.ID, "a.go", "b.go", "c.go", "d.go")
	query := s.randomVector(cacheTestDims)
	s.searchMatchesLegacy(writer, idx.ID, query, 2)
	cost := writer.ChunkVectorCacheUsed()

	store := postgres.NewIndexStore(s.db)
	store.SetChunkVectorBudget(cost - 1)
	s.searchMatchesLegacy(store, idx.ID, query, 2)
	s.searchMatchesLegacy(store, idx.ID, query, 2)

	s.False(store.ChunkVectorCached(idx.ID))
	s.Zero(store.ChunkVectorCacheUsed())
	s.EqualValues(2, store.ChunkVectorLoads())
}

func (s *IndexVectorCacheSuite) TestAnotherHostsWriteIsSeenOnceTheEntryExpires() {
	store := postgres.NewIndexStore(s.db)
	idx := s.newIndex(store)
	s.saveRandomChunks(store, idx.ID, "a.go", "b.go", "c.go")
	query := s.randomVector(cacheTestDims)
	now := time.Now()
	store.SetChunkVectorClock(func() time.Time { return now })
	s.searchMatchesLegacy(store, idx.ID, query, 1)

	otherHost := postgres.NewIndexStore(s.db)
	s.Require().NoError(otherHost.SaveChunks(s.ctx, idx.ID, []domain.WorkspaceChunk{codeChunk("late.go", 9, query)}))
	now = now.Add(postgres.ChunkVectorMaxAge() - time.Second)
	got, err := store.SearchChunksByIndex(s.ctx, idx.ID, query, 1)
	s.Require().NoError(err)
	s.NotEqual("late.go", got[0].FilePath, "within the max age the cached vectors answer")

	now = now.Add(time.Second)
	got = s.searchMatchesLegacy(store, idx.ID, query, 1)
	s.Equal("late.go", got[0].FilePath)
	s.EqualValues(2, store.ChunkVectorLoads())
}
