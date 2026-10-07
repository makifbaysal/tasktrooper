package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type scoredWorkspaceChunk struct {
	chunk domain.WorkspaceChunk
	score float64
}

// LegacySearchChunksInMemory is searchChunksInMemory as it was before the
// vector cache, copied verbatim. It is the reference the cached search must
// match field for field and in order, ties included.
func (s *IndexStore) LegacySearchChunksInMemory(ctx context.Context, indexID uuid.UUID, queryEmbedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding
		FROM workspace_chunks WHERE index_id = $1
	`, indexID)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	var scored []scoredWorkspaceChunk
	for rows.Next() {
		var ch domain.WorkspaceChunk
		var embJSON []byte
		if err := rows.Scan(
			&ch.ID, &ch.IndexID, &ch.FilePath, &ch.SymbolName, &ch.Kind,
			&ch.StartLine, &ch.EndLine, &ch.Language, &ch.Signature, &ch.Content, &embJSON,
		); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		if err := json.Unmarshal(embJSON, &ch.Embedding); err != nil {
			continue
		}
		score := cosineSimilarity(queryEmbedding, ch.Embedding)
		ch.Score = score
		scored = append(scored, scoredWorkspaceChunk{chunk: ch, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	if topK > len(scored) {
		topK = len(scored)
	}
	result := make([]domain.WorkspaceChunk, 0, topK)
	for i := 0; i < topK; i++ {
		result = append(result, scored[i].chunk)
	}
	return result, nil
}

// ChunkVectorLoads counts the embedding SELECTs the vector cache has run.
func (s *IndexStore) ChunkVectorLoads() int64 {
	return s.vectors.loads.Load()
}

func (s *IndexStore) ChunkVectorCached(indexID uuid.UUID) bool {
	s.vectors.mu.Lock()
	defer s.vectors.mu.Unlock()
	_, ok := s.vectors.entries[indexID]
	return ok
}

func (s *IndexStore) ChunkVectorCacheUsed() int {
	s.vectors.mu.Lock()
	defer s.vectors.mu.Unlock()
	return s.vectors.used
}

func (s *IndexStore) SetChunkVectorBudget(cost int) {
	s.vectors.mu.Lock()
	defer s.vectors.mu.Unlock()
	s.vectors.budget = cost
}

// LoadChunkVectorsAcross runs one cache load with write landing between its
// SELECT and its install, the interleaving a concurrent search and index pass
// can produce, and reports whether the load was installed.
func (s *IndexStore) LoadChunkVectorsAcross(ctx context.Context, indexID uuid.UUID, write func()) (bool, error) {
	_, version := s.vectors.lookup(indexID)
	vecs, err := s.loadIndexVectors(ctx, indexID)
	if err != nil {
		return false, err
	}
	write()
	return s.vectors.install(indexID, version, vecs), nil
}

func (s *IndexStore) SetChunkVectorClock(now func() time.Time) {
	s.vectors.mu.Lock()
	defer s.vectors.mu.Unlock()
	s.vectors.now = now
}

func ChunkVectorMaxAge() time.Duration { return chunkVectorMaxAge }
