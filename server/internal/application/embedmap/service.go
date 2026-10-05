package embedmap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	SourceFiles = "files"
	SourceCode  = "code"
)

const (
	DefaultLimit = 2000
	MinLimit     = 100
	MaxLimit     = 5000

	MaxLocateChunks = 50

	DefaultDims = 50
	MinDims     = 2
	MaxDims     = 128

	snippetRunes = 200
)

var (
	ErrInvalidSource = errors.New(`source must be "files" or "code"`)

	ErrRepositoryRequired = errors.New("repository_id is required when source=code")

	ErrUnavailable = errors.New("embedding map unavailable")

	ErrLocateRepositoryRequired = errors.New("repository_id is required")
	ErrLocateAnchorsRequired    = errors.New("anchor_ids must not be empty")
	ErrLocateChunksRequired     = errors.New("chunk_ids must not be empty")
	ErrLocateTooManyAnchors     = fmt.Errorf("anchor_ids holds at most %d ids", MaxLimit)
	ErrLocateTooManyChunks      = fmt.Errorf("chunk_ids holds at most %d ids", MaxLocateChunks)
)

type Service struct {
	store port.EmbeddingMapStore

	embeddings port.EmbeddingProvenanceResolver
}

func New(store port.EmbeddingMapStore) *Service {
	return &Service{store: store}
}

func (s *Service) SetEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	if s == nil {
		return
	}
	s.embeddings = r
}

func (s *Service) resolvedEmbedding(ctx context.Context) (string, int) {
	if s == nil || s.embeddings == nil {
		return "", 0
	}
	model, dims, err := s.embeddings.ResolvedEmbedding(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("embedding map: could not resolve the configured embedding model; sources not checked for staleness")
		return "", 0
	}
	return model, dims
}

type Query struct {
	Source       string
	RepositoryID uuid.UUID
	Limit        int
	Dims         int
}

type FileSource struct {
	Available     bool `json:"available"`
	ChunkCount    int  `json:"chunk_count"`
	DocumentCount int  `json:"document_count"`
}

type RepositorySource struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Branch     string     `json:"branch"`
	IndexID    string     `json:"index_id"`
	ChunkCount int        `json:"chunk_count"`
	FileCount  int        `json:"file_count"`
	IndexedAt  *time.Time `json:"indexed_at"`

	EmbeddingModel string `json:"embedding_model,omitempty"`

	EmbeddingStale   bool   `json:"embedding_stale,omitempty"`
	EmbeddingWarning string `json:"embedding_warning,omitempty"`
}

type Sources struct {
	Files        FileSource         `json:"files"`
	Repositories []RepositorySource `json:"repositories"`
}

type Point struct {
	ID         string    `json:"id"`
	GroupID    string    `json:"group_id"`
	GroupLabel string    `json:"group_label"`
	ChunkIndex int       `json:"chunk_index"`
	Snippet    string    `json:"snippet"`
	Language   string    `json:"language"`
	Symbol     string    `json:"symbol"`
	Vector     []float64 `json:"vector"`
}

type Map struct {
	Source       string  `json:"source"`
	RepositoryID string  `json:"repository_id"`
	Branch       string  `json:"branch"`
	Dimensions   int     `json:"dimensions"`
	Total        int     `json:"total"`
	Sampled      int     `json:"sampled"`
	Truncated    bool    `json:"truncated"`
	Points       []Point `json:"points"`

	EmbeddingStale   bool   `json:"embedding_stale,omitempty"`
	EmbeddingWarning string `json:"embedding_warning,omitempty"`
}

func (s *Service) Sources(ctx context.Context) (Sources, error) {
	if s == nil || s.store == nil {
		return Sources{}, ErrUnavailable
	}
	stats, err := s.store.FileStats(ctx)
	if err != nil {
		return Sources{}, fmt.Errorf("embedding map file stats: %w", err)
	}
	repos, err := s.store.ListRepositorySources(ctx)
	if err != nil {
		return Sources{}, fmt.Errorf("embedding map repository sources: %w", err)
	}
	out := Sources{
		Files: FileSource{
			Available:     stats.ChunkCount > 0,
			ChunkCount:    stats.ChunkCount,
			DocumentCount: stats.DocumentCount,
		},
		Repositories: make([]RepositorySource, 0, len(repos)),
	}

	configuredModel, configuredDims := s.resolvedEmbedding(ctx)
	for _, r := range repos {
		src := RepositorySource{
			ID:             r.RepositoryID.String(),
			Name:           r.Name,
			Branch:         r.Branch,
			IndexID:        r.IndexID.String(),
			ChunkCount:     r.ChunkCount,
			FileCount:      r.FileCount,
			IndexedAt:      r.IndexedAt,
			EmbeddingModel: r.EmbeddingModel,
		}
		if domain.EmbeddingProvenanceStale(r.EmbeddingModel, r.EmbeddingDims, configuredModel, configuredDims) {
			src.EmbeddingStale = true
			src.EmbeddingWarning = domain.EmbeddingStaleMessage(r.EmbeddingModel, r.EmbeddingDims, configuredModel, configuredDims)
		}
		out.Repositories = append(out.Repositories, src)
	}
	return out, nil
}

func (s *Service) Build(ctx context.Context, q Query) (Map, error) {
	if s == nil || s.store == nil {
		return Map{}, ErrUnavailable
	}
	limit := clamp(q.Limit, MinLimit, MaxLimit, DefaultLimit)
	dims := clamp(q.Dims, MinDims, MaxDims, DefaultDims)

	switch q.Source {
	case SourceFiles:
		total, chunks, err := s.store.SampleFileChunks(ctx, limit)
		if err != nil {
			return Map{}, fmt.Errorf("sample file chunks: %w", err)
		}
		return project(SourceFiles, "", "", total, limit, dims, chunks), nil

	case SourceCode:
		if q.RepositoryID == uuid.Nil {
			return Map{}, ErrRepositoryRequired
		}
		repoID := q.RepositoryID.String()
		src, err := s.store.RepositorySource(ctx, q.RepositoryID)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {

				return emptyMap(SourceCode, repoID, "", dims), nil
			}
			return Map{}, fmt.Errorf("resolve repository index: %w", err)
		}
		total, chunks, err := s.store.SampleCodeChunks(ctx, src.IndexID, limit)
		if err != nil {
			return Map{}, fmt.Errorf("sample code chunks: %w", err)
		}
		out := project(SourceCode, repoID, src.Branch, total, limit, dims, chunks)

		configuredModel, configuredDims := s.resolvedEmbedding(ctx)
		if domain.EmbeddingProvenanceStale(src.EmbeddingModel, src.EmbeddingDims, configuredModel, configuredDims) {
			out.EmbeddingStale = true
			out.EmbeddingWarning = domain.EmbeddingStaleMessage(src.EmbeddingModel, src.EmbeddingDims, configuredModel, configuredDims)
		}
		return out, nil

	default:
		return Map{}, ErrInvalidSource
	}
}

func emptyMap(source, repositoryID, branch string, dims int) Map {
	return Map{
		Source:       source,
		RepositoryID: repositoryID,
		Branch:       branch,
		Dimensions:   dims,
		Points:       []Point{},
	}
}

func project(source, repositoryID, branch string, total, limit, dims int, chunks []port.EmbeddingChunk) Map {
	out := emptyMap(source, repositoryID, branch, dims)
	out.Total = total
	out.Truncated = total > limit
	if len(chunks) == 0 {
		return out
	}

	vectors := make([][]float32, len(chunks))
	for i, ch := range chunks {
		vectors[i] = ch.Embedding
	}
	res := PCA(vectors, dims)
	if res.Dims > 0 {
		out.Dimensions = res.Dims
	}
	out.Points = make([]Point, 0, len(res.Kept))
	for i, idx := range res.Kept {
		ch := chunks[idx]
		out.Points = append(out.Points, Point{
			ID:         ch.ID,
			GroupID:    ch.GroupID,
			GroupLabel: ch.GroupLabel,
			ChunkIndex: ch.ChunkIndex,
			Snippet:    snippet(ch.Content),
			Language:   ch.Language,
			Symbol:     ch.Symbol,
			Vector:     res.Coords[i],
		})
	}
	out.Sampled = len(out.Points)
	return out
}

func snippet(content string) string {
	collapsed := strings.Join(strings.Fields(content), " ")
	runes := []rune(collapsed)
	if len(runes) > snippetRunes {
		return string(runes[:snippetRunes])
	}
	return collapsed
}

func clamp(value, min, max, def int) int {
	if value == 0 {
		return def
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
