package postgres

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

type IndexStore struct {
	pool   *DB
	caps   VectorCapabilities
	healer vectorHealer

	// hosts describes THIS host's filesystem, so a root_path written by another
	// host can be re-anchored on the way out of the store. See
	// localizeIndexRootPath.
	hosts hostRoots

	// embeddings answers what this install's embedding calls resolve to right
	// now, which is the only thing this store cannot read out of its own
	// tables. Optional: nil leaves the model-name half of the staleness check
	// unanswerable, and the dimension half (which needs no configuration at
	// all) still runs. See assertEmbeddingComparable.
	embeddings port.EmbeddingProvenanceResolver

	// vectors is coherent with this process's own writes: every method here
	// that writes workspace_chunks or deletes workspace_indexes rows
	// invalidates it. Deletes that cascade from elsewhere (a repository or
	// session removed) are caught on read; see searchChunksInMemory. Writes by
	// another host on the same database are not seen until an entry expires
	// (chunkVectorMaxAge).
	vectors chunkVectorCache
}

func NewIndexStore(pool *DB) *IndexStore {
	return &IndexStore{pool: pool}
}

// SetHostRoots tells the store which filesystem it is reading rows on behalf
// of, exactly as RepositoryStore.SetHostRoots does; the zero value is the old
// pass-through.
func (s *IndexStore) SetHostRoots(workspaceRoot string, allowedRoots []string) *IndexStore {
	s.hosts.set(workspaceRoot, allowedRoots)
	return s
}

// localizeIndexRootPath re-anchors a workspace_indexes.root_path onto this
// host, and is applied to every index row leaving this store.
//
// Re-anchoring rather than invalidating is a deliberate choice, and it rests on
// what an index row actually contains. Everything derived from the tree is
// stored RELATIVE to root_path — workspace_symbols.file_path,
// workspace_chunks.file_path and workspace_file_hashes.file_path are all
// filepath.Rel results, and a chunk carries its own text in the row rather than
// a pointer into a file. The index body is therefore host independent: only the
// anchor is host specific, and moving the anchor cannot make a stored chunk
// describe a file it did not come from. It re-points at the same repo-relative
// file in this host's checkout of the same repository, which is exactly the
// translation the repository's own root_path already gets.
//
// After a read, root_path reaches the filesystem in one place:
// indexer.Injector renders the code skeleton with mapper.BuildSkeletonRanked,
// and only when the run context carries no workspace dir of its own (a live
// workspace already wins there). That walk reads the CURRENT tree, so anchoring
// it here describes this host's checkout — while leaving the path foreign makes
// the walk fail, and the error is swallowed, so the agent silently loses the
// skeleton section instead of getting a usable one.
//
// Invalidate-and-rebuild was the alternative and is far more destructive: it
// would discard every embedding for a repository on a condition that flips each
// time the repository changes host, so the two hosts would take turns re-embedding
// the same tree forever — real provider spend and minutes of latency, to
// correct a staleness the hash-incremental pass (workspace_file_hashes →
// DeleteFileData/SaveFileHashes) already fixes file by file on the next pass.
// The staleness that remains — chunks from the other host's commit — is the
// same staleness a single host already lives with between index passes, not a
// new class of wrongness introduced here.
//
// Writes stay host-absolute and are left alone: every index pass stamps
// root_path with this host's absolute root (indexer.Service → UpdateIndexTree),
// so a row converges on whoever indexed last and the read-time translation
// keeps the other host safe in the meantime.
func (s *IndexStore) localizeIndexRootPath(idx *domain.WorkspaceIndex) {
	if s == nil || idx == nil || idx.RootPath == "" {
		return
	}
	resolved, reanchored, first := s.hosts.localize(idx.RootPath)
	if !reanchored {
		return
	}
	if first {
		log.Info().
			Str("index", idx.ID.String()).
			Str("branch", idx.Branch).
			Str("stored_root_path", idx.RootPath).
			Str("host_root_path", resolved).
			Msg("workspace index root_path was written by another host; re-anchored to this host's workspace root")
	}
	idx.RootPath = resolved
}

// SetCapabilities enables the pgvector / pg_trgm SQL paths; without them all
// searches fall back to loading rows and scoring cosine in Go.
func (s *IndexStore) SetCapabilities(caps VectorCapabilities) {
	s.caps = caps
}

// SetEmbeddingResolver gives the store the one fact it cannot read from its own
// tables: which embedding model queries are produced by NOW.
//
// It is wired late (from platform/runtime, once the LLM-provider service
// exists) and is optional, so a deployment without one keeps the behaviour it
// had — plus the dimension check, which needs nothing configured.
//
// The signature is deliberately the shape indexer.Service forwards through
// (application code must not name this adapter), so wiring it on the indexer
// wires it here too.
func (s *IndexStore) SetEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	s.embeddings = r
}

// clearIndexes runs a DELETE on workspace_indexes and drops the cached
// vectors of every row it removed (their chunks go by cascade).
func (s *IndexStore) clearIndexes(ctx context.Context, deleteReturningID string, args ...any) (err error) {
	var cleared []uuid.UUID
	defer func() {
		// On an error we cannot tell which rows the statement removed.
		if err != nil {
			s.vectors.invalidateAll()
			return
		}
		s.vectors.invalidate(cleared...)
	}()
	rows, err := s.pool.Query(ctx, deleteReturningID, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if scanErr := rows.Scan(&id); scanErr != nil {
			return scanErr
		}
		cleared = append(cleared, id)
	}
	return rows.Err()
}

func (s *IndexStore) CreateIndex(ctx context.Context, sessionID uuid.UUID, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	err := s.clearIndexes(ctx, `DELETE FROM workspace_indexes WHERE session_id = $1 RETURNING id`, sessionID)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("clear index: %w", err)
	}
	var idx domain.WorkspaceIndex
	var status string
	var sid uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO workspace_indexes (session_id, root_path, status, tree_text)
		VALUES ($1, $2, $3, $4)
		RETURNING id, session_id, repository_id, branch, commit_sha, root_path, status, file_count, chunk_count, symbol_count,
		          files_total, files_processed, tree_text, indexed_at, error, embedding_model, embedding_dims
	`, sessionID, rootPath, string(domain.IndexStatusPending), treeText).Scan(
		&idx.ID, &sid, &idx.ProjectID, &idx.Branch, &idx.CommitSHA, &idx.RootPath, &status,
		&idx.FileCount, &idx.ChunkCount, &idx.SymbolCount,
		&idx.FilesTotal, &idx.FilesProcessed, &idx.TreeText, &idx.IndexedAt, &idx.Error,
		&idx.EmbeddingModel, &idx.EmbeddingDims,
	)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("create index: %w", err)
	}
	idx.SessionID = &sid
	idx.Status = domain.IndexStatus(status)
	s.localizeIndexRootPath(&idx)
	return idx, nil
}

func (s *IndexStore) CreateProjectIndex(ctx context.Context, projectID uuid.UUID, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	err := s.clearIndexes(ctx, `DELETE FROM workspace_indexes WHERE repository_id = $1 RETURNING id`, projectID)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("clear project index: %w", err)
	}
	var idx domain.WorkspaceIndex
	var status string
	var pid uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO workspace_indexes (repository_id, root_path, status, tree_text)
		VALUES ($1, $2, $3, $4)
		RETURNING id, session_id, repository_id, branch, commit_sha, root_path, status, file_count, chunk_count, symbol_count,
		          files_total, files_processed, tree_text, indexed_at, error, embedding_model, embedding_dims
	`, projectID, rootPath, string(domain.IndexStatusPending), treeText).Scan(
		&idx.ID, &idx.SessionID, &pid, &idx.Branch, &idx.CommitSHA, &idx.RootPath, &status,
		&idx.FileCount, &idx.ChunkCount, &idx.SymbolCount,
		&idx.FilesTotal, &idx.FilesProcessed, &idx.TreeText, &idx.IndexedAt, &idx.Error,
		&idx.EmbeddingModel, &idx.EmbeddingDims,
	)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("create project index: %w", err)
	}
	idx.ProjectID = &pid
	idx.Status = domain.IndexStatus(status)
	s.localizeIndexRootPath(&idx)
	return idx, nil
}

// CreateProjectBranchIndex creates the index row for one (repository, branch).
// Unlike CreateProjectIndex it clears only that branch's previous row — other
// branches and the default-branch index survive.
func (s *IndexStore) CreateProjectBranchIndex(ctx context.Context, projectID uuid.UUID, branch, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	err := s.clearIndexes(ctx, `DELETE FROM workspace_indexes WHERE repository_id = $1 AND branch = $2 RETURNING id`, projectID, branch)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("clear branch index: %w", err)
	}
	var idx domain.WorkspaceIndex
	var status string
	var pid uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO workspace_indexes (repository_id, branch, root_path, status, tree_text)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, session_id, repository_id, branch, commit_sha, root_path, status, file_count, chunk_count, symbol_count,
		          files_total, files_processed, tree_text, indexed_at, error, embedding_model, embedding_dims
	`, projectID, branch, rootPath, string(domain.IndexStatusPending), treeText).Scan(
		&idx.ID, &idx.SessionID, &pid, &idx.Branch, &idx.CommitSHA, &idx.RootPath, &status,
		&idx.FileCount, &idx.ChunkCount, &idx.SymbolCount,
		&idx.FilesTotal, &idx.FilesProcessed, &idx.TreeText, &idx.IndexedAt, &idx.Error,
		&idx.EmbeddingModel, &idx.EmbeddingDims,
	)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("create branch index: %w", err)
	}
	idx.ProjectID = &pid
	idx.Status = domain.IndexStatus(status)
	s.localizeIndexRootPath(&idx)
	return idx, nil
}

const indexColumns = `id, session_id, repository_id, branch, commit_sha, root_path, status, file_count, chunk_count, symbol_count,
	       files_total, files_processed, tree_text, indexed_at, error, embedding_model, embedding_dims`

func (s *IndexStore) GetIndexBySession(ctx context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error) {
	return s.scanIndex(ctx, `
		SELECT `+indexColumns+`
		FROM workspace_indexes WHERE session_id = $1 ORDER BY created_at DESC LIMIT 1
	`, sessionID)
}

// GetIndexByProject returns the repository's default-branch index. Branch
// indexes are separate rows resolved via GetIndexByProjectBranch.
func (s *IndexStore) GetIndexByProject(ctx context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error) {
	return s.scanIndex(ctx, `
		SELECT `+indexColumns+`
		FROM workspace_indexes WHERE repository_id = $1 AND branch = '' ORDER BY created_at DESC LIMIT 1
	`, projectID)
}

func (s *IndexStore) GetIndexByProjectBranch(ctx context.Context, projectID uuid.UUID, branch string) (domain.WorkspaceIndex, error) {
	var idx domain.WorkspaceIndex
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT `+indexColumns+`
		FROM workspace_indexes WHERE repository_id = $1 AND branch = $2 ORDER BY created_at DESC LIMIT 1
	`, projectID, branch).Scan(
		&idx.ID, &idx.SessionID, &idx.ProjectID, &idx.Branch, &idx.CommitSHA, &idx.RootPath, &status,
		&idx.FileCount, &idx.ChunkCount, &idx.SymbolCount,
		&idx.FilesTotal, &idx.FilesProcessed, &idx.TreeText, &idx.IndexedAt, &idx.Error,
		&idx.EmbeddingModel, &idx.EmbeddingDims,
	)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("get branch index: %w", err)
	}
	idx.Status = domain.IndexStatus(status)
	s.localizeIndexRootPath(&idx)
	return idx, nil
}

func (s *IndexStore) scanIndex(ctx context.Context, query string, arg any) (domain.WorkspaceIndex, error) {
	idx, err := s.scanIndexRow(s.pool.QueryRow(ctx, query, arg))
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("get index: %w", err)
	}
	return idx, nil
}

// scanIndexRow expects the column order of indexColumns.
func (s *IndexStore) scanIndexRow(row interface{ Scan(dest ...any) error }) (domain.WorkspaceIndex, error) {
	var idx domain.WorkspaceIndex
	var status string
	err := row.Scan(
		&idx.ID, &idx.SessionID, &idx.ProjectID, &idx.Branch, &idx.CommitSHA, &idx.RootPath, &status,
		&idx.FileCount, &idx.ChunkCount, &idx.SymbolCount,
		&idx.FilesTotal, &idx.FilesProcessed, &idx.TreeText, &idx.IndexedAt, &idx.Error,
		&idx.EmbeddingModel, &idx.EmbeddingDims,
	)
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}
	idx.Status = domain.IndexStatus(status)
	s.localizeIndexRootPath(&idx)
	return idx, nil
}

// branchIndexColumns is indexColumns with tree_text blanked: a listing of every
// branch index has no use for the one column that holds a whole file tree.
const branchIndexColumns = `id, session_id, repository_id, branch, commit_sha, root_path, status, file_count, chunk_count, symbol_count,
	       files_total, files_processed, '' AS tree_text, indexed_at, error, embedding_model, embedding_dims`

// ListBranchIndexes returns every non-default-branch index row, oldest first,
// with TreeText left empty.
func (s *IndexStore) ListBranchIndexes(ctx context.Context) ([]domain.WorkspaceIndex, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+branchIndexColumns+`
		FROM workspace_indexes WHERE branch <> '' ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list branch indexes: %w", err)
	}
	defer rows.Close()
	var out []domain.WorkspaceIndex
	for rows.Next() {
		idx, err := s.scanIndexRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan branch index: %w", err)
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

// DeleteIndex removes one index row; its symbols, chunks, edges and file
// hashes go with it by cascade. A row that is already gone is not an error.
func (s *IndexStore) DeleteIndex(ctx context.Context, indexID uuid.UUID) error {
	defer s.vectors.invalidate(indexID)
	if _, err := s.pool.Exec(ctx, `DELETE FROM workspace_indexes WHERE id = $1`, indexID); err != nil {
		return fmt.Errorf("delete index: %w", err)
	}
	return nil
}

func (s *IndexStore) UpdateIndexStatus(ctx context.Context, indexID uuid.UUID, status domain.IndexStatus, fileCount, chunkCount, symbolCount int, errMsg string) error {
	now := time.Now()
	_, err := s.pool.Exec(ctx, `
		UPDATE workspace_indexes
		SET status = $2, file_count = $3, chunk_count = $4, symbol_count = $5,
		    error = CASE
		      WHEN $7 != '' THEN $7
		      WHEN $2 IN ('running', 'completed') THEN ''
		      ELSE error
		    END,
		    indexed_at = CASE WHEN $2 = 'completed' THEN $6 ELSE indexed_at END,
		    updated_at = now()
		WHERE id = $1
	`, indexID, string(status), fileCount, chunkCount, symbolCount, now, errMsg)
	if err != nil {
		return fmt.Errorf("update index status: %w", err)
	}
	return nil
}

func (s *IndexStore) UpdateIndexProgress(ctx context.Context, indexID uuid.UUID, filesProcessed, filesTotal int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE workspace_indexes
		SET files_processed = $2, files_total = $3, updated_at = now()
		WHERE id = $1
	`, indexID, filesProcessed, filesTotal)
	if err != nil {
		return fmt.Errorf("update index progress: %w", err)
	}
	return nil
}

// sendBatch runs a prepared batch and returns the first statement error.
//
// Indexing writes rows per file — a few symbols, a few chunks, its edges — and
// each Exec was its own network round trip to Cloud SQL. One file could cost a
// hundred of them, and with several index workers running that latency, not the
// database, was the ceiling. A batch pays for one.
func sendBatch(ctx context.Context, pool *DB, batch *pgx.Batch, what string) error {
	if batch.Len() == 0 {
		return nil
	}
	results := pool.SendBatch(ctx, batch)
	var firstErr error
	for i := 0; i < batch.Len(); i++ {
		if _, err := results.Exec(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("insert %s: %w", what, err)
		}
	}
	// Close before returning the error: an unclosed batch result leaks the
	// connection it holds, and a failing index would drain the pool.
	if closeErr := results.Close(); closeErr != nil && firstErr == nil {
		firstErr = fmt.Errorf("insert %s: %w", what, closeErr)
	}
	return firstErr
}

func (s *IndexStore) SaveSymbols(ctx context.Context, indexID uuid.UUID, symbols []domain.WorkspaceSymbol) error {
	batch := &pgx.Batch{}
	for _, sym := range symbols {
		batch.Queue(`
			INSERT INTO workspace_symbols (index_id, file_path, kind, name, signature, doc, start_line, end_line)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, indexID, sym.FilePath, sym.Kind, sym.Name, sym.Signature, sym.Doc, sym.StartLine, sym.EndLine)
	}
	return sendBatch(ctx, s.pool, batch, "symbol")
}

func (s *IndexStore) SaveChunks(ctx context.Context, indexID uuid.UUID, chunks []domain.WorkspaceChunk) error {
	defer s.vectors.invalidate(indexID)
	// Built once and re-sent by execWithVectorHeal: a pgx.Batch is read-only
	// once queued, so replaying it after the column is relaxed is safe.
	build := func() (*pgx.Batch, int, error) {
		batch := &pgx.Batch{}
		dim := 0
		for _, ch := range chunks {
			emb, err := json.Marshal(ch.Embedding)
			if err != nil {
				return nil, 0, fmt.Errorf("marshal embedding: %w", err)
			}
			if len(ch.Embedding) == 0 {
				// A chunk the embedder could not take is stored with SQL NULL,
				// not JSON null: every search reads NULL as "no vector" and
				// skips it, where JSON null scored 0 and could still rank.
				emb = nil
			}
			if s.caps.Vector && len(ch.Embedding) > 0 {
				dim = len(ch.Embedding)
				batch.Queue(`
					INSERT INTO workspace_chunks (index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding, embedding_vec)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::vector)
				`, indexID, ch.FilePath, ch.SymbolName, ch.Kind, ch.StartLine, ch.EndLine, ch.Language, ch.Signature, ch.Content, emb, vectorLiteral(ch.Embedding))
				continue
			}
			batch.Queue(`
				INSERT INTO workspace_chunks (index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			`, indexID, ch.FilePath, ch.SymbolName, ch.Kind, ch.StartLine, ch.EndLine, ch.Language, ch.Signature, ch.Content, emb)
		}
		return batch, dim, nil
	}

	batch, dim, err := build()
	if err != nil {
		return err
	}
	if dim == 0 {
		return sendBatch(ctx, s.pool, batch, "chunk")
	}
	// Switching embedding provider changes the vector's dimension while the
	// column is still typed for the previous model — relax it and write again
	// instead of failing the whole index run.
	return execWithVectorHeal(ctx,
		func(ctx context.Context) error {
			retry, _, buildErr := build()
			if buildErr != nil {
				return buildErr
			}
			return sendBatch(ctx, s.pool, retry, "chunk")
		},
		func(ctx context.Context) error { return s.healer.relax(ctx, s.pool, dim) },
	)
}

func (s *IndexStore) SaveEdges(ctx context.Context, indexID uuid.UUID, edges []domain.WorkspaceEdge) error {
	batch := &pgx.Batch{}
	for _, e := range edges {
		batch.Queue(`
			INSERT INTO workspace_edges (index_id, from_file, from_symbol, to_file, to_symbol, edge_kind)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, indexID, e.FromFile, e.FromSymbol, e.ToFile, e.ToSymbol, e.EdgeKind)
	}
	return sendBatch(ctx, s.pool, batch, "edge")
}

func (s *IndexStore) SaveFileHashes(ctx context.Context, indexID uuid.UUID, hashes []domain.WorkspaceFileHash) error {
	batch := &pgx.Batch{}
	for _, h := range hashes {
		batch.Queue(`
			INSERT INTO workspace_file_hashes (index_id, file_path, content_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT (index_id, file_path) DO UPDATE SET content_hash = EXCLUDED.content_hash
		`, indexID, h.FilePath, h.Hash)
	}
	return sendBatch(ctx, s.pool, batch, "file hash")
}

func (s *IndexStore) GetFileHashes(ctx context.Context, indexID uuid.UUID) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT file_path, content_hash FROM workspace_file_hashes WHERE index_id = $1
	`, indexID)
	if err != nil {
		return nil, fmt.Errorf("get file hashes: %w", err)
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		result[path] = hash
	}
	return result, rows.Err()
}

// staleEmbeddingRefusal is the entire staleness decision, as a pure function of
// five numbers and strings, so it can be read and tested without a database.
//
// Two independent proofs, either of which is enough:
//
//	the CONFIGURATION proof — the configured embedding model is not the one this
//	  index was built with (domain.EmbeddingProvenanceStale, which also treats a
//	  blank index model as stale, because an index from before migration 116
//	  records no model and "unknown" is not "matches").
//	the ARITHMETIC proof — the index's vectors and the query's vector are of
//	  different lengths. This one needs nothing configured and cannot be wrong:
//	  the query was embedded by whatever is answering embed calls right now, and
//	  a 768-float query against 1536-float rows is a different model by
//	  definition. It is what still guards a deployment with no resolver wired.
//
// Neither is a "no results" answer. A cosine similarity between two models'
// vectors is arithmetically fine and semantically meaningless — it ranks
// unrelated code confidently — so the refusal has to be an error the caller can
// recognise, not an empty slice it will report as "nothing matched".
func staleEmbeddingRefusal(indexModel string, indexDimensions int, configuredModel string, configuredDimensions, queryDimensions int) error {
	if domain.EmbeddingProvenanceStale(indexModel, indexDimensions, configuredModel, configuredDimensions) {
		return fmt.Errorf("%s: %w",
			domain.EmbeddingStaleMessage(indexModel, indexDimensions, configuredModel, configuredDimensions),
			domain.ErrIndexEmbeddingStale)
	}
	if indexDimensions > 0 && queryDimensions > 0 && indexDimensions != queryDimensions {
		return fmt.Errorf("%s: %w",
			domain.EmbeddingStaleMessage(indexModel, indexDimensions, configuredModel, queryDimensions),
			domain.ErrIndexEmbeddingStale)
	}
	return nil
}

// assertEmbeddingComparable reads this index's recorded provenance and refuses
// the search when it cannot be compared with the query.
//
// It costs one extra scoped SELECT of two small columns per search, which is
// the price of the guarantee: the alternative is carrying the provenance down
// from every caller, and a caller that forgets gets the silent wrong ranking
// back. It is applied at SearchChunksByIndex, which every embedding search in
// this store funnels through (session search resolves an index and calls it;
// hybrid search calls it and then only ADDS trigram matches).
//
// A resolver failure is deliberately NOT a refusal. It means "this process
// could not read the embedding setting just now" — a database hiccup, not
// evidence of a mismatch — and turning that into a blocked code search for
// every agent on the box is a worse outcome than one search served on an index
// that is very probably fine. The arithmetic proof still runs, and it is the
// one that catches the case this cannot see.
func (s *IndexStore) assertEmbeddingComparable(ctx context.Context, indexID uuid.UUID, queryDimensions int) error {
	var indexModel string
	var indexDims int
	err := s.pool.QueryRow(ctx, `
		SELECT embedding_model, embedding_dims FROM workspace_indexes WHERE id = $1
	`, indexID).Scan(&indexModel, &indexDims)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No index row: the search below has nothing to return anyway, and
			// inventing a staleness verdict about a row that does not exist
			// would mislabel "never indexed" as "indexed with the wrong model".
			return nil
		}
		return fmt.Errorf("read index embedding provenance: %w", err)
	}
	configuredModel, configuredDims := "", 0
	if s.embeddings != nil {
		model, dims, resolveErr := s.embeddings.ResolvedEmbedding(ctx)
		if resolveErr != nil {
			log.Warn().Err(resolveErr).Str("index_id", indexID.String()).
				Msg("could not resolve the configured embedding model; searching on the dimension check alone")
		} else {
			configuredModel, configuredDims = model, dims
		}
	}
	return staleEmbeddingRefusal(indexModel, indexDims, configuredModel, configuredDims, queryDimensions)
}

func (s *IndexStore) SearchChunks(ctx context.Context, sessionID uuid.UUID, queryEmbedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	idx, err := s.GetIndexBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.SearchChunksByIndex(ctx, idx.ID, queryEmbedding, topK)
}

func (s *IndexStore) SearchChunksByIndex(ctx context.Context, indexID uuid.UUID, queryEmbedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	if len(queryEmbedding) == 0 {
		return nil, nil
	}
	// Before any vector is compared, not after: every path below scores this
	// query against stored rows, and the arithmetic succeeds whether or not the
	// two came from the same model.
	if err := s.assertEmbeddingComparable(ctx, indexID, len(queryEmbedding)); err != nil {
		return nil, err
	}
	if s.caps.Vector {
		if chunks, err := s.searchChunksVector(ctx, indexID, queryEmbedding, topK); err == nil {
			return chunks, nil
		}
	}
	return s.searchChunksInMemory(ctx, indexID, queryEmbedding, topK)
}

// SearchChunksHybrid fuses vector similarity with trigram text matching via
// reciprocal rank fusion. Falls back to plain vector search when pg_trgm (or
// the raw query) is unavailable.
func (s *IndexStore) SearchChunksHybrid(ctx context.Context, indexID uuid.UUID, rawQuery string, queryEmbedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	vecResults, err := s.SearchChunksByIndex(ctx, indexID, queryEmbedding, topK*2)
	if err != nil {
		return nil, err
	}
	if !s.caps.Trgm || rawQuery == "" {
		return capChunks(vecResults, topK), nil
	}
	txtResults, err := s.searchChunksTrigram(ctx, indexID, rawQuery, topK*2)
	if err != nil || len(txtResults) == 0 {
		return capChunks(vecResults, topK), nil
	}
	return fuseRRF(vecResults, txtResults, topK), nil
}

func (s *IndexStore) searchChunksVector(ctx context.Context, indexID uuid.UUID, queryEmbedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content,
		       1 - (embedding_vec <=> $2::vector) AS score
		FROM workspace_chunks
		WHERE index_id = $1 AND embedding_vec IS NOT NULL
		ORDER BY embedding_vec <=> $2::vector
		LIMIT $3
	`, indexID, vectorLiteral(queryEmbedding), topK)
	if err != nil {
		return nil, fmt.Errorf("vector search chunks: %w", err)
	}
	defer rows.Close()
	var result []domain.WorkspaceChunk
	for rows.Next() {
		var ch domain.WorkspaceChunk
		if err := rows.Scan(
			&ch.ID, &ch.IndexID, &ch.FilePath, &ch.SymbolName, &ch.Kind,
			&ch.StartLine, &ch.EndLine, &ch.Language, &ch.Signature, &ch.Content, &ch.Score,
		); err != nil {
			return nil, fmt.Errorf("scan vector chunk: %w", err)
		}
		result = append(result, ch)
	}
	return result, rows.Err()
}

func (s *IndexStore) searchChunksTrigram(ctx context.Context, indexID uuid.UUID, rawQuery string, topK int) ([]domain.WorkspaceChunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content,
		       similarity(content, $2) AS score
		FROM workspace_chunks
		WHERE index_id = $1 AND (content % $2 OR content ILIKE '%' || $2 || '%')
		ORDER BY score DESC
		LIMIT $3
	`, indexID, rawQuery, topK)
	if err != nil {
		return nil, fmt.Errorf("trigram search chunks: %w", err)
	}
	defer rows.Close()
	var result []domain.WorkspaceChunk
	for rows.Next() {
		var ch domain.WorkspaceChunk
		if err := rows.Scan(
			&ch.ID, &ch.IndexID, &ch.FilePath, &ch.SymbolName, &ch.Kind,
			&ch.StartLine, &ch.EndLine, &ch.Language, &ch.Signature, &ch.Content, &ch.Score,
		); err != nil {
			return nil, fmt.Errorf("scan trigram chunk: %w", err)
		}
		result = append(result, ch)
	}
	return result, rows.Err()
}

// searchChunksInMemory is the path every code search takes without pgvector,
// which includes the embedded database. It ranks against cached vectors and
// reads row bodies only for the winners.
//
// The ranking is the one the original single-SELECT loop produced, ties
// included, and has to stay that way: candidates keep the SELECT's row order,
// rows whose embedding does not decode are left out, and a slice of the same
// length is sorted by sort.Slice with the same comparator. pdqsort's
// permutation depends only on the length and the comparator's answers, so
// equal scores come out in the same order they always did.
func (s *IndexStore) searchChunksInMemory(ctx context.Context, indexID uuid.UUID, queryEmbedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	if topK <= 0 {
		return []domain.WorkspaceChunk{}, nil
	}
	for attempt := 0; ; attempt++ {
		vecs, err := s.vectors.get(ctx, indexID, s.loadIndexVectors)
		if err != nil {
			return nil, err
		}
		chunks, complete, err := s.readRankedChunks(ctx, indexID, vecs, vecs.rank(queryEmbedding, topK))
		if err != nil || complete || attempt > 0 {
			return chunks, err
		}
		// A winner's row is gone, so these vectors predate a delete this store
		// did not make (a repository or session cascade): rank once more
		// against what is there now.
		s.vectors.invalidate(indexID)
	}
}

// loadIndexVectors decodes an index's embeddings exactly as the original
// search did, in the SELECT's row order (which is what fixes tie order), and
// packs them into one exactly-sized arena.
func (s *IndexStore) loadIndexVectors(ctx context.Context, indexID uuid.UUID) (*indexVectors, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, embedding FROM workspace_chunks WHERE index_id = $1`, indexID)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()

	type span struct {
		start, end int
		null       bool
	}
	var (
		ids     []uuid.UUID
		spans   []span
		flat    []float32
		scratch []float32
	)
	for rows.Next() {
		var id uuid.UUID
		var embJSON []byte
		if err := rows.Scan(&id, &embJSON); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		if err := json.Unmarshal(embJSON, &scratch); err != nil {
			continue
		}
		ids = append(ids, id)
		spans = append(spans, span{start: len(flat), end: len(flat) + len(scratch), null: scratch == nil})
		flat = append(flat, scratch...)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	arena := make([]float32, len(flat))
	copy(arena, flat)
	vecs := make([][]float32, len(spans))
	for i, sp := range spans {
		// A stored JSON null decoded to a nil Embedding and [] to an empty
		// one; callers have always been handed that difference.
		if !sp.null {
			vecs[i] = arena[sp.start:sp.end:sp.end]
		}
	}
	return &indexVectors{
		ids:  ids,
		vecs: vecs,
		cost: len(arena) + len(ids)*chunkVectorRowOverhead,
	}, nil
}

// readRankedChunks fetches the bodies of the ranked rows and returns them in
// rank order, each with its own copy of the embedding because callers mutate
// what they get. complete is false when a ranked row no longer exists.
func (s *IndexStore) readRankedChunks(ctx context.Context, indexID uuid.UUID, vecs *indexVectors, ranked []rankedVector) ([]domain.WorkspaceChunk, bool, error) {
	result := make([]domain.WorkspaceChunk, 0, len(ranked))
	if len(ranked) == 0 {
		return result, true, nil
	}
	ids := make([]uuid.UUID, len(ranked))
	for i, r := range ranked {
		ids[i] = vecs.ids[r.row]
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content
		FROM workspace_chunks WHERE index_id = $1 AND id = ANY($2)
	`, indexID, ids)
	if err != nil {
		return nil, false, fmt.Errorf("search chunks: %w", err)
	}
	defer rows.Close()
	byID := make(map[uuid.UUID]domain.WorkspaceChunk, len(ids))
	for rows.Next() {
		var ch domain.WorkspaceChunk
		if err := rows.Scan(
			&ch.ID, &ch.IndexID, &ch.FilePath, &ch.SymbolName, &ch.Kind,
			&ch.StartLine, &ch.EndLine, &ch.Language, &ch.Signature, &ch.Content,
		); err != nil {
			return nil, false, fmt.Errorf("scan chunk: %w", err)
		}
		byID[ch.ID] = ch
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	for _, r := range ranked {
		ch, ok := byID[vecs.ids[r.row]]
		if !ok {
			continue
		}
		ch.Embedding = slices.Clone(vecs.vecs[r.row])
		ch.Score = r.score
		result = append(result, ch)
	}
	return result, len(result) == len(ranked), nil
}

// chunkVectorCacheBudget bounds the cached vectors of all indexes together, in
// float32s (32M ≈ 128 MB). An index that alone exceeds it is searched without
// being cached.
const chunkVectorCacheBudget = 32 << 20

// chunkVectorRowOverhead charges each cached row for its id and slice header
// (16 + 24 bytes) in float32 units, so an index of many short or empty vectors
// is not counted as free.
const chunkVectorRowOverhead = 10

// chunkVectorMarkLimit caps how many per-index invalidation marks are kept
// before they are folded into the shared floor.
const chunkVectorMarkLimit = 1024

// indexVectors is one index's decodable embeddings, in the row order the
// search SELECT returned them. It is shared between searches and never
// written after it is built.
type indexVectors struct {
	ids  []uuid.UUID
	vecs [][]float32
	cost int
}

type rankedVector struct {
	row   int
	score float64
}

func (v *indexVectors) rank(query []float32, topK int) []rankedVector {
	scored := make([]rankedVector, len(v.vecs))
	for i, vec := range v.vecs {
		scored[i] = rankedVector{row: i, score: cosineSimilarity(query, vec)}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	return scored[:min(topK, len(scored))]
}

// chunkVectorMaxAge is how long a cached index is trusted without a write of
// this process's invalidating it: long enough for the searches of one run to
// share a load, short enough that another host's re-index shows.
const chunkVectorMaxAge = time.Minute

type chunkVectorCacheEntry struct {
	indexID  uuid.UUID
	vectors  *indexVectors
	loadedAt time.Time
}

// chunkVectorCache is an LRU of indexVectors bounded by total cost and by
// chunkVectorMaxAge. The zero value is ready to use.
//
// Versions keep a load that raced a write from reinstating what the write
// replaced. An index's version is the later of its own last invalidation
// (marks) and the last cache-wide one (floor), both drawn from seq; a load
// installs only if the version it read before its SELECT is still current.
// The version is also part of the singleflight key, so a search that starts
// after an invalidation never joins a load that started before it.
type chunkVectorCache struct {
	mu      sync.Mutex
	budget  int
	used    int
	lru     list.List
	entries map[uuid.UUID]*list.Element
	seq     uint64
	floor   uint64
	marks   map[uuid.UUID]uint64
	flights singleflight.Group
	loads   atomic.Int64
	now     func() time.Time
}

func (c *chunkVectorCache) clockLocked() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *chunkVectorCache) limitLocked() int {
	if c.budget > 0 {
		return c.budget
	}
	return chunkVectorCacheBudget
}

func (c *chunkVectorCache) versionLocked(id uuid.UUID) uint64 {
	return max(c.marks[id], c.floor)
}

func (c *chunkVectorCache) lookup(id uuid.UUID) (*indexVectors, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[id]; ok {
		entry := el.Value.(*chunkVectorCacheEntry)
		if c.clockLocked().Sub(entry.loadedAt) < chunkVectorMaxAge {
			c.lru.MoveToFront(el)
			return entry.vectors, c.versionLocked(id)
		}
		c.removeLocked(id)
	}
	return nil, c.versionLocked(id)
}

func (c *chunkVectorCache) get(ctx context.Context, id uuid.UUID, load func(context.Context, uuid.UUID) (*indexVectors, error)) (*indexVectors, error) {
	vecs, version := c.lookup(id)
	if vecs != nil {
		return vecs, nil
	}
	// Detached from ctx: the load is shared, and one caller giving up must not
	// fail the others waiting on it.
	loadCtx := context.WithoutCancel(ctx)
	flight := c.flights.DoChan(id.String()+"@"+strconv.FormatUint(version, 10), func() (any, error) {
		cached, loadVersion := c.lookup(id)
		if cached != nil {
			return cached, nil
		}
		c.loads.Add(1)
		loaded, err := load(loadCtx, id)
		if err != nil {
			return nil, err
		}
		c.install(id, loadVersion, loaded)
		return loaded, nil
	})
	select {
	case res := <-flight:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*indexVectors), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// install caches vecs for id unless id was invalidated since version was read
// or vecs alone exceeds the budget, evicting least recently used indexes to
// make room. It reports whether vecs was cached.
func (c *chunkVectorCache) install(id uuid.UUID, version uint64, vecs *indexVectors) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := c.limitLocked()
	if c.versionLocked(id) != version || vecs.cost > limit {
		return false
	}
	c.removeLocked(id)
	for c.used+vecs.cost > limit {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.removeLocked(oldest.Value.(*chunkVectorCacheEntry).indexID)
	}
	if c.entries == nil {
		c.entries = make(map[uuid.UUID]*list.Element)
	}
	c.entries[id] = c.lru.PushFront(&chunkVectorCacheEntry{indexID: id, vectors: vecs, loadedAt: c.clockLocked()})
	c.used += vecs.cost
	return true
}

func (c *chunkVectorCache) removeLocked(id uuid.UUID) {
	el, ok := c.entries[id]
	if !ok {
		return
	}
	c.used -= el.Value.(*chunkVectorCacheEntry).vectors.cost
	c.lru.Remove(el)
	delete(c.entries, id)
}

// invalidate must run after the write it covers has committed: a load that
// reads before the commit then fails its install, and one that reads after it
// sees the write.
func (c *chunkVectorCache) invalidate(ids ...uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		c.seq++
		if c.marks == nil {
			c.marks = make(map[uuid.UUID]uint64)
		}
		c.marks[id] = c.seq
		c.removeLocked(id)
	}
	if len(c.marks) > chunkVectorMarkLimit {
		// Raising the floor past every mark keeps each version moving forward,
		// so the marks can go; cached entries stay, since they are current.
		c.seq++
		c.floor = c.seq
		c.marks = nil
	}
}

func (c *chunkVectorCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.floor = c.seq
	c.marks = nil
	c.entries = nil
	c.lru.Init()
	c.used = 0
}

func capChunks(chunks []domain.WorkspaceChunk, topK int) []domain.WorkspaceChunk {
	if len(chunks) > topK {
		return chunks[:topK]
	}
	return chunks
}

// fuseRRF merges two ranked lists with reciprocal rank fusion (k=60).
func fuseRRF(a, b []domain.WorkspaceChunk, topK int) []domain.WorkspaceChunk {
	const k = 60.0
	type entry struct {
		chunk domain.WorkspaceChunk
		score float64
	}
	merged := map[uuid.UUID]*entry{}
	add := func(list []domain.WorkspaceChunk) {
		for rank, ch := range list {
			if e, ok := merged[ch.ID]; ok {
				e.score += 1.0 / (k + float64(rank+1))
			} else {
				merged[ch.ID] = &entry{chunk: ch, score: 1.0 / (k + float64(rank+1))}
			}
		}
	}
	add(a)
	add(b)
	entries := make([]*entry, 0, len(merged))
	for _, e := range merged {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].score > entries[j].score })
	if topK > len(entries) {
		topK = len(entries)
	}
	out := make([]domain.WorkspaceChunk, 0, topK)
	for i := 0; i < topK; i++ {
		c := entries[i].chunk
		c.Score = entries[i].score
		out = append(out, c)
	}
	return out
}

func (s *IndexStore) SearchSymbols(ctx context.Context, name string, indexID uuid.UUID) ([]domain.WorkspaceSymbol, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, index_id, file_path, kind, name, signature, doc, start_line, end_line
		FROM workspace_symbols WHERE index_id = $1 AND name = $2
	`, indexID, name)
	if err != nil {
		return nil, fmt.Errorf("search symbols: %w", err)
	}
	defer rows.Close()
	var symbols []domain.WorkspaceSymbol
	for rows.Next() {
		var sym domain.WorkspaceSymbol
		if err := rows.Scan(
			&sym.ID, &sym.IndexID, &sym.FilePath, &sym.Kind, &sym.Name,
			&sym.Signature, &sym.Doc, &sym.StartLine, &sym.EndLine,
		); err != nil {
			return nil, err
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

func (s *IndexStore) GetChunkBySymbol(ctx context.Context, indexID uuid.UUID, filePath, symbolName string) (domain.WorkspaceChunk, error) {
	var ch domain.WorkspaceChunk
	var embJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding
		FROM workspace_chunks
		WHERE index_id = $1 AND file_path = $2 AND symbol_name = $3
		LIMIT 1
	`, indexID, filePath, symbolName).Scan(
		&ch.ID, &ch.IndexID, &ch.FilePath, &ch.SymbolName, &ch.Kind,
		&ch.StartLine, &ch.EndLine, &ch.Language, &ch.Signature, &ch.Content, &embJSON,
	)
	if err != nil {
		return domain.WorkspaceChunk{}, fmt.Errorf("get chunk by symbol: %w", err)
	}
	_ = json.Unmarshal(embJSON, &ch.Embedding)
	return ch, nil
}

func (s *IndexStore) ListEdges(ctx context.Context, indexID uuid.UUID) ([]domain.WorkspaceEdge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT index_id, from_file, from_symbol, to_file, to_symbol, edge_kind
		FROM workspace_edges WHERE index_id = $1
	`, indexID)
	if err != nil {
		return nil, fmt.Errorf("list edges: %w", err)
	}
	defer rows.Close()
	var edges []domain.WorkspaceEdge
	for rows.Next() {
		var e domain.WorkspaceEdge
		if err := rows.Scan(&e.IndexID, &e.FromFile, &e.FromSymbol, &e.ToFile, &e.ToSymbol, &e.EdgeKind); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func (s *IndexStore) DeleteIndexData(ctx context.Context, indexID uuid.UUID) error {
	defer s.vectors.invalidate(indexID)
	tables := []string{
		"workspace_file_hashes",
		"workspace_edges",
		"workspace_chunks",
		"workspace_symbols",
	}
	for _, table := range tables {
		if _, err := s.pool.Exec(ctx, "DELETE FROM "+table+" WHERE index_id = $1", indexID); err != nil {
			return fmt.Errorf("delete %s: %w", table, err)
		}
	}
	return nil
}

// CopyIndexData clones every derived row of one index into another so a fresh
// branch index starts from the default branch's embeddings instead of
// re-embedding the whole repository. The copied vectors keep the provenance
// they were made with: a branch identical to its base finishes without
// embedding anything, and without the stamp its searches would be refused as
// built by an unrecorded model.
func (s *IndexStore) CopyIndexData(ctx context.Context, fromIndexID, toIndexID uuid.UUID) error {
	defer s.vectors.invalidate(toIndexID)
	stmts := []string{
		`UPDATE workspace_indexes AS dst SET embedding_model = src.embedding_model, embedding_dims = src.embedding_dims
		 FROM workspace_indexes AS src WHERE src.id = $1 AND dst.id = $2`,
		`INSERT INTO workspace_symbols (index_id, file_path, kind, name, signature, doc, start_line, end_line)
		 SELECT $2, file_path, kind, name, signature, doc, start_line, end_line FROM workspace_symbols WHERE index_id = $1`,
		`INSERT INTO workspace_edges (index_id, from_file, from_symbol, to_file, to_symbol, edge_kind)
		 SELECT $2, from_file, from_symbol, to_file, to_symbol, edge_kind FROM workspace_edges WHERE index_id = $1`,
		`INSERT INTO workspace_file_hashes (index_id, file_path, content_hash)
		 SELECT $2, file_path, content_hash FROM workspace_file_hashes WHERE index_id = $1`,
	}
	if s.caps.Vector {
		stmts = append(stmts,
			`INSERT INTO workspace_chunks (index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding, embedding_vec)
			 SELECT $2, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding, embedding_vec FROM workspace_chunks WHERE index_id = $1`)
	} else {
		stmts = append(stmts,
			`INSERT INTO workspace_chunks (index_id, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding)
			 SELECT $2, file_path, symbol_name, kind, start_line, end_line, language, signature, content, embedding FROM workspace_chunks WHERE index_id = $1`)
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt, fromIndexID, toIndexID); err != nil {
			return fmt.Errorf("copy index data: %w", err)
		}
	}
	return nil
}

// DeleteFileData removes the derived rows of specific files, so an
// incremental reindex touches only what changed.
func (s *IndexStore) DeleteFileData(ctx context.Context, indexID uuid.UUID, filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}
	defer s.vectors.invalidate(indexID)
	stmts := []string{
		`DELETE FROM workspace_file_hashes WHERE index_id = $1 AND file_path = ANY($2)`,
		`DELETE FROM workspace_chunks WHERE index_id = $1 AND file_path = ANY($2)`,
		`DELETE FROM workspace_symbols WHERE index_id = $1 AND file_path = ANY($2)`,
		`DELETE FROM workspace_edges WHERE index_id = $1 AND from_file = ANY($2)`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt, indexID, filePaths); err != nil {
			return fmt.Errorf("delete file data: %w", err)
		}
	}
	return nil
}

// DeleteFilesNotIn drops the rows of files that have left the tree. keepPaths
// is the full current file list, so an empty one would delete everything the
// index has — that is refused rather than obeyed: a walk that returned nothing
// is a broken walk far more often than an empty repository.
func (s *IndexStore) DeleteFilesNotIn(ctx context.Context, indexID uuid.UUID, keepPaths []string) error {
	if len(keepPaths) == 0 {
		return nil
	}
	defer s.vectors.invalidate(indexID)
	stmts := []string{
		`DELETE FROM workspace_file_hashes WHERE index_id = $1 AND NOT (file_path = ANY($2))`,
		`DELETE FROM workspace_chunks WHERE index_id = $1 AND NOT (file_path = ANY($2))`,
		`DELETE FROM workspace_symbols WHERE index_id = $1 AND NOT (file_path = ANY($2))`,
		`DELETE FROM workspace_edges WHERE index_id = $1 AND NOT (from_file = ANY($2))`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt, indexID, keepPaths); err != nil {
			return fmt.Errorf("delete removed file data: %w", err)
		}
	}
	return nil
}

func (s *IndexStore) UpdateIndexTree(ctx context.Context, indexID uuid.UUID, rootPath, treeText string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE workspace_indexes SET root_path = $2, tree_text = $3, updated_at = now() WHERE id = $1
	`, indexID, rootPath, treeText)
	if err != nil {
		return fmt.Errorf("update index tree: %w", err)
	}
	return nil
}

// UpdateIndexEmbedding records what the pass that just finished actually
// embedded with (migration 116).
//
// Both columns are NOT NULL with an empty default, so this is an overwrite
// rather than a merge: a pass that re-embedded the whole tree is what writes
// here, and it knows both values. A pass that embedded nothing measurable
// passes dimensions 0, which EmbeddingProvenanceStale reads as "unknown size"
// and ignores rather than as "zero-length vectors".
func (s *IndexStore) UpdateIndexEmbedding(ctx context.Context, indexID uuid.UUID, model string, dimensions int) error {
	if dimensions < 0 {
		dimensions = 0
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE workspace_indexes SET embedding_model = $2, embedding_dims = $3, updated_at = now() WHERE id = $1
	`, indexID, model, dimensions)
	if err != nil {
		return fmt.Errorf("update index embedding provenance: %w", err)
	}
	return nil
}

func (s *IndexStore) UpdateIndexCommit(ctx context.Context, indexID uuid.UUID, commitSHA string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE workspace_indexes SET commit_sha = $2, updated_at = now() WHERE id = $1
	`, indexID, commitSHA)
	if err != nil {
		return fmt.Errorf("update index commit: %w", err)
	}
	return nil
}

func (s *IndexStore) CountIndexData(ctx context.Context, indexID uuid.UUID) (int, int, error) {
	var chunks, symbols int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM workspace_chunks WHERE index_id = $1`, indexID).Scan(&chunks); err != nil {
		return 0, 0, fmt.Errorf("count chunks: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM workspace_symbols WHERE index_id = $1`, indexID).Scan(&symbols); err != nil {
		return 0, 0, fmt.Errorf("count symbols: %w", err)
	}
	return chunks, symbols, nil
}
