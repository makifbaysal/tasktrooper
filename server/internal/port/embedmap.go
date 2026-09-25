package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type EmbeddingChunk struct {
	ID         string
	GroupID    string
	GroupLabel string
	ChunkIndex int
	Content    string
	Language   string
	Symbol     string
	Embedding  []float32
}

type EmbeddingFileStats struct {
	ChunkCount    int
	DocumentCount int
}

type EmbeddingRepositorySource struct {
	RepositoryID   uuid.UUID
	Name           string
	Branch         string
	IndexID        uuid.UUID
	ChunkCount     int
	FileCount      int
	IndexedAt      *time.Time
	EmbeddingModel string
	EmbeddingDims  int
}

type EmbeddingMapStore interface {
	FileStats(ctx context.Context) (EmbeddingFileStats, error)
	ListRepositorySources(ctx context.Context) ([]EmbeddingRepositorySource, error)
	RepositorySource(ctx context.Context, repositoryID uuid.UUID) (EmbeddingRepositorySource, error)
	SampleFileChunks(ctx context.Context, limit int) (total int, chunks []EmbeddingChunk, err error)
	SampleCodeChunks(ctx context.Context, indexID uuid.UUID, limit int) (total int, chunks []EmbeddingChunk, err error)
}
