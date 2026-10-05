package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// EmbeddingMapStore reads chunk embeddings for the 2D embedding map.
//
// Everything here reads the portable JSONB `embedding` column rather than the
// optional `embedding_vec` (migration 036): the map must work on a database
// without pgvector, and the projection is done in Go either way.
type EmbeddingMapStore struct {
	pool *DB
}

func NewEmbeddingMapStore(pool *DB) *EmbeddingMapStore {
	return &EmbeddingMapStore{pool: pool}
}

// embedSnippetChars is how much chunk text is read per row. The service cuts it
// down to a 200-rune tooltip; reading a bounded prefix keeps a 2000-row sample
// from dragging whole file bodies across the wire.
const embedSnippetChars = 400

func (s *EmbeddingMapStore) FileStats(ctx context.Context) (port.EmbeddingFileStats, error) {
	var stats port.EmbeddingFileStats
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)::int, count(DISTINCT file_id)::int FROM file_chunks
	`).Scan(&stats.ChunkCount, &stats.DocumentCount)
	if err != nil {
		return port.EmbeddingFileStats{}, fmt.Errorf("embedding map file stats: %w", err)
	}
	return stats, nil
}

// repositorySourceQuery picks ONE index per repository, deterministically: the
// index with the most chunks wins, ties go to the most recently indexed one,
// and a remaining tie is broken by index id. A repository indexed on several
// branches therefore shows its richest index rather than an arbitrary row, and
// the same call always returns the same one.
const repositorySourceQuery = `
	SELECT id, name, branch, index_id, chunk_count, file_count, indexed_at, embedding_model, embedding_dims FROM (
		SELECT DISTINCT ON (r.id)
		       r.id AS id, r.name AS name, wi.branch AS branch, wi.id AS index_id,
		       c.chunk_count AS chunk_count, wi.file_count AS file_count, wi.indexed_at AS indexed_at,
		       wi.embedding_model AS embedding_model, wi.embedding_dims AS embedding_dims
		FROM repositories r
		JOIN workspace_indexes wi ON wi.repository_id = r.id
		JOIN LATERAL (
			SELECT count(*)::int AS chunk_count FROM workspace_chunks wc WHERE wc.index_id = wi.id
		) c ON true
		WHERE c.chunk_count > 0 %s
		ORDER BY r.id, c.chunk_count DESC, wi.indexed_at DESC NULLS LAST, wi.id
	) picked
	ORDER BY name, id
`

func scanRepositorySource(row interface{ Scan(dest ...any) error }) (port.EmbeddingRepositorySource, error) {
	var src port.EmbeddingRepositorySource
	err := row.Scan(&src.RepositoryID, &src.Name, &src.Branch, &src.IndexID, &src.ChunkCount, &src.FileCount, &src.IndexedAt,
		&src.EmbeddingModel, &src.EmbeddingDims)
	return src, err
}

func (s *EmbeddingMapStore) ListRepositorySources(ctx context.Context) ([]port.EmbeddingRepositorySource, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(repositorySourceQuery, ""))
	if err != nil {
		return nil, fmt.Errorf("list embedding map repositories: %w", err)
	}
	defer rows.Close()
	var out []port.EmbeddingRepositorySource
	for rows.Next() {
		src, scanErr := scanRepositorySource(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan embedding map repository: %w", scanErr)
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *EmbeddingMapStore) RepositorySource(ctx context.Context, repositoryID uuid.UUID) (port.EmbeddingRepositorySource, error) {
	src, err := scanRepositorySource(s.pool.QueryRow(ctx, fmt.Sprintf(repositorySourceQuery, "AND r.id = $1"), repositoryID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return port.EmbeddingRepositorySource{}, fmt.Errorf("embedding map repository: %w", port.ErrNotFound)
		}
		return port.EmbeddingRepositorySource{}, fmt.Errorf("embedding map repository: %w", err)
	}
	return src, nil
}

// SampleFileChunks samples the uploaded-document corpus. Ordering is stable
// (filename, chunk index, id) and the sample is a stride over that order, so
// the points are spread across every document instead of being the first N of
// the alphabetically-first file.
func (s *EmbeddingMapStore) SampleFileChunks(ctx context.Context, limit int) (int, []port.EmbeddingChunk, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM file_chunks`).Scan(&total); err != nil {
		return 0, nil, fmt.Errorf("count file chunks: %w", err)
	}
	if total == 0 || limit <= 0 {
		return total, nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, file_id, filename, chunk_index, snippet, embedding FROM (
			SELECT fc.id AS id, fc.file_id AS file_id, f.filename AS filename, fc.chunk_index AS chunk_index,
			       left(fc.content, `+fmt.Sprint(embedSnippetChars)+`) AS snippet, fc.embedding AS embedding,
			       row_number() OVER (ORDER BY f.filename, fc.chunk_index, fc.id) AS rn
			FROM file_chunks fc
			JOIN files f ON f.id = fc.file_id
		) ordered
		WHERE `+evenSamplePredicate+`
		ORDER BY rn
		LIMIT $1
	`, limit, total)
	if err != nil {
		return 0, nil, fmt.Errorf("sample file chunks: %w", err)
	}
	defer rows.Close()

	var out []port.EmbeddingChunk
	for rows.Next() {
		var id, fileID uuid.UUID
		var filename, content string
		var chunkIndex int
		var embJSON []byte
		if err := rows.Scan(&id, &fileID, &filename, &chunkIndex, &content, &embJSON); err != nil {
			return 0, nil, fmt.Errorf("scan file chunk: %w", err)
		}
		var embedding []float32
		if err := json.Unmarshal(embJSON, &embedding); err != nil {
			continue
		}
		out = append(out, port.EmbeddingChunk{
			ID:         id.String(),
			GroupID:    fileID.String(),
			GroupLabel: filename,
			ChunkIndex: chunkIndex,
			Content:    content,
			Embedding:  embedding,
		})
	}
	return total, out, rows.Err()
}

// SampleCodeChunks samples one workspace index the same way. chunk_index is a
// 0-based ordinal within the file (start line, then id) computed over the whole
// index, so a point keeps its position in the file even when its neighbours
// were sampled out.
func (s *EmbeddingMapStore) SampleCodeChunks(ctx context.Context, indexID uuid.UUID, limit int) (int, []port.EmbeddingChunk, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM workspace_chunks WHERE index_id = $1`, indexID).Scan(&total)
	if err != nil {
		return 0, nil, fmt.Errorf("count workspace chunks: %w", err)
	}
	if total == 0 || limit <= 0 {
		return total, nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, file_path, file_ordinal, symbol_name, language, snippet, embedding FROM (
			SELECT wc.id AS id, wc.file_path AS file_path, wc.symbol_name AS symbol_name,
			       wc.language AS language, left(wc.content, `+fmt.Sprint(embedSnippetChars)+`) AS snippet,
			       wc.embedding AS embedding,
			       row_number() OVER (ORDER BY wc.file_path, wc.start_line, wc.id) AS rn,
			       (row_number() OVER (PARTITION BY wc.file_path ORDER BY wc.start_line, wc.id) - 1)::int AS file_ordinal
			FROM workspace_chunks wc
			WHERE wc.index_id = $3
		) ordered
		WHERE `+evenSamplePredicate+`
		ORDER BY rn
		LIMIT $1
	`, limit, total, indexID)
	if err != nil {
		return 0, nil, fmt.Errorf("sample workspace chunks: %w", err)
	}
	defer rows.Close()

	var out []port.EmbeddingChunk
	for rows.Next() {
		var id uuid.UUID
		var filePath, symbolName, language, content string
		var ordinal int
		var embJSON []byte
		if err := rows.Scan(&id, &filePath, &ordinal, &symbolName, &language, &content, &embJSON); err != nil {
			return 0, nil, fmt.Errorf("scan workspace chunk: %w", err)
		}
		var embedding []float32
		if err := json.Unmarshal(embJSON, &embedding); err != nil {
			continue
		}
		out = append(out, port.EmbeddingChunk{
			ID:         id.String(),
			GroupID:    filePath,
			GroupLabel: filePath,
			ChunkIndex: ordinal,
			Content:    content,
			Language:   language,
			Symbol:     symbolName,
			Embedding:  embedding,
		})
	}
	return total, out, rows.Err()
}

func (s *EmbeddingMapStore) ChunkEmbeddings(ctx context.Context, indexID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	out := make(map[uuid.UUID][]float32, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, embedding FROM workspace_chunks WHERE index_id = $1 AND id = ANY($2)`, indexID, ids)
	if err != nil {
		return nil, fmt.Errorf("load workspace chunk embeddings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var embJSON []byte
		if err := rows.Scan(&id, &embJSON); err != nil {
			return nil, fmt.Errorf("scan workspace chunk embedding: %w", err)
		}
		var embedding []float32
		if err := json.Unmarshal(embJSON, &embedding); err != nil {
			continue
		}
		out[id] = embedding
	}
	return out, rows.Err()
}

// evenSamplePredicate keeps a row when floor(rn·limit/total) steps up at rn —
// the step function rises exactly `limit` times across rn = 1..total, so the
// sample is exactly `limit` rows spread evenly over the whole ordering. $1 is
// the limit and $2 the total.
//
// A plain "every ceil(total/limit)-th row" stride is the obvious alternative
// and is quietly much worse near the boundary: at total = limit+1 it strides by
// 2 and returns half the points asked for.
const evenSamplePredicate = `(rn * $1) / $2 <> ((rn - 1) * $1) / $2`
