package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const (
	perChunkEmbedTimeout = 90 * time.Second

	defaultEmbedRetryWait = 60 * time.Second
)

const (
	defaultEmbedConcurrency = 2

	maxTransientEmbedAttempts = 4
	transientEmbedBackoff     = time.Second

	maxConsecutiveEmbedFailures = 10
)

type Service struct {
	store          port.IndexStore
	llm            port.LLMClient
	mapper         *mapper.Service
	chunkers       *chunker.Registry
	cfg            domain.IndexerConfig
	graphCfg       domain.GraphConfig
	embeddingModel string

	embedBudget time.Duration

	embedSlots chan struct{}
	projectMu  sync.Mutex

	projectRunning map[uuid.UUID]context.CancelFunc
	branchRunning  map[string]context.CancelFunc

	mirrors MirrorRestorer

	embeddings port.EmbeddingProvenanceResolver
}

type MirrorRestorer interface {
	EnsureIndexMirror(ctx context.Context, repositoryID uuid.UUID, rootPath string) error
}

func (s *Service) SetMirrorRestorer(m MirrorRestorer) {
	if s == nil {
		return
	}
	s.mirrors = m
}

func (s *Service) SetEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	if s == nil {
		return
	}
	s.setEmbeddingResolver(fallbackEmbeddingResolver{inner: r, model: s.embeddingModel})
}

func (s *Service) setEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	s.embeddings = r
	if aware, ok := s.store.(embeddingResolverAware); ok {
		aware.SetEmbeddingResolver(r)
	}
}

type embeddingResolverAware interface {
	SetEmbeddingResolver(r port.EmbeddingProvenanceResolver)
}

type fallbackEmbeddingResolver struct {
	inner port.EmbeddingProvenanceResolver
	model string
}

func (r fallbackEmbeddingResolver) ResolvedEmbedding(ctx context.Context) (string, int, error) {
	if r.inner != nil {
		model, dims, err := r.inner.ResolvedEmbedding(ctx)
		if err != nil {
			return "", 0, err
		}
		if model != "" {
			return model, dims, nil
		}
	}

	return r.model, 0, nil
}

func NewService(
	store port.IndexStore,
	llm port.LLMClient,
	mapperSvc *mapper.Service,
	chunkers *chunker.Registry,
	cfg domain.IndexerConfig,
	graphCfg domain.GraphConfig,
	embeddingModel string,
) *Service {
	svc := &Service{
		store:          store,
		llm:            llm,
		mapper:         mapperSvc,
		chunkers:       chunkers,
		cfg:            cfg,
		graphCfg:       graphCfg,
		embeddingModel: embeddingModel,
		embedBudget:    perChunkEmbedTimeout,
		embedSlots:     make(chan struct{}, embedConcurrency(cfg)),
		projectRunning: make(map[uuid.UUID]context.CancelFunc),
	}

	svc.setEmbeddingResolver(fallbackEmbeddingResolver{model: embeddingModel})
	return svc
}

func (s *Service) SetEmbeddingLimits(cfg domain.EmbeddingConfig) {
	request := cfg.RequestTimeout
	if request <= 0 {
		request = perChunkEmbedTimeout
	}
	retries := cfg.MaxRetries
	if retries < 0 {
		retries = 0
	}
	wait := cfg.MaxRetryWait
	if wait <= 0 {
		wait = defaultEmbedRetryWait
	}
	s.embedBudget = request*time.Duration(retries+1) + wait*time.Duration(retries)
}

func (s *Service) GetProjectStatus(ctx context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error) {
	return s.store.GetIndexByProject(ctx, projectID)
}

func (s *Service) SearchProject(ctx context.Context, projectID uuid.UUID, query string, topK int) ([]domain.WorkspaceChunk, error) {
	idx, err := s.store.GetIndexByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("index not found")
	}
	if idx.Status != domain.IndexStatusCompleted {
		return nil, fmt.Errorf("index not ready: %s", idx.Status)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if topK <= 0 {
		topK = s.cfg.TopK
	}
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}
	embedding, err := s.llm.Embed(ctx, query, s.embeddingModel)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	chunks, err := s.store.SearchChunksHybrid(ctx, idx.ID, query, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	for i := range chunks {
		chunks[i].Embedding = nil
	}
	return chunks, nil
}

func (s *Service) IsProjectIndexActive(projectID uuid.UUID) bool {
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	_, ok := s.projectRunning[projectID]
	return ok
}

func (s *Service) StopIndexProject(projectID uuid.UUID) bool {
	if s == nil {
		return false
	}
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	stopped := false
	if cancel, ok := s.projectRunning[projectID]; ok {
		cancel()
		stopped = true
	}
	prefix := projectID.String() + "|"
	for key, cancel := range s.branchRunning {
		if strings.HasPrefix(key, prefix) {
			cancel()
			stopped = true
		}
	}
	return stopped
}

func (s *Service) RestartIndexProject(ctx context.Context, projectID uuid.UUID, rootPath string, onDone func()) {
	s.projectMu.Lock()
	delete(s.projectRunning, projectID)
	s.projectMu.Unlock()
	s.startIndexProject(ctx, projectID, rootPath, onDone, true)
}

func (s *Service) StartIndexProject(ctx context.Context, projectID uuid.UUID, rootPath string, onDone func()) {
	s.startIndexProject(ctx, projectID, rootPath, onDone, false)
}

func detachIndexContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	return domain.WithBackgroundLLM(context.WithoutCancel(ctx))
}

func (s *Service) startIndexProject(ctx context.Context, projectID uuid.UUID, rootPath string, onDone func(), force bool) {
	if !s.cfg.Enabled {
		if onDone != nil {
			onDone()
		}
		return
	}
	s.projectMu.Lock()
	if _, running := s.projectRunning[projectID]; running {
		s.projectMu.Unlock()
		if onDone != nil {
			onDone()
		}
		return
	}
	passCtx, cancel := context.WithCancel(detachIndexContext(ctx))
	s.projectRunning[projectID] = cancel
	s.projectMu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.projectMu.Lock()
			delete(s.projectRunning, projectID)
			s.projectMu.Unlock()
			if onDone != nil {
				onDone()
			}
		}()
		if _, err := s.indexProject(passCtx, projectID, rootPath, force); err != nil {
			log.Warn().Err(err).Str("project_id", projectID.String()).Msg("project index failed")
		}
	}()
}

func (s *Service) GetStatus(ctx context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error) {
	return s.store.GetIndexBySession(ctx, sessionID)
}

func (s *Service) IndexSession(ctx context.Context, sessionID uuid.UUID, rootPath string) (domain.WorkspaceIndex, error) {
	if !s.cfg.Enabled {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("resolve root path: %w", err)
	}

	paths, err := WalkIndexableFiles(absRoot, IndexWalkOptions{IncludeGenerated: s.cfg.IndexGenerated})
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}

	treeText, err := s.mapper.BuildTree(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("build tree: %w", err)
	}

	existing, existingErr := s.store.GetIndexBySession(ctx, sessionID)
	var index domain.WorkspaceIndex
	isNew := existingErr != nil
	if isNew {
		index, err = s.store.CreateIndex(ctx, sessionID, absRoot, treeText)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("create index: %w", err)
		}
	} else {
		index = existing
		index.RootPath = absRoot
		index.TreeText = treeText
	}

	return s.runIndexPass(ctx, index, absRoot, paths, treeText, isNew, false)
}

func (s *Service) failIndex(ctx context.Context, indexID uuid.UUID, err error) {
	if indexID == uuid.Nil || err == nil {
		return
	}
	_ = s.store.UpdateIndexStatus(ctx, indexID, domain.IndexStatusFailed, 0, 0, 0, err.Error())
}

func (s *Service) indexProject(ctx context.Context, projectID uuid.UUID, rootPath string, force bool) (index domain.WorkspaceIndex, retErr error) {
	var indexID uuid.UUID
	defer func() {
		if retErr != nil {
			s.failIndex(ctx, indexID, retErr)
		}
	}()

	if !s.cfg.Enabled {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("resolve root path: %w", err)
	}

	existing, existingErr := s.store.GetIndexByProject(ctx, projectID)
	if existingErr == nil {
		indexID = existing.ID
	}

	if s.mirrors != nil {
		if err := s.mirrors.EnsureIndexMirror(ctx, projectID, absRoot); err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("the code to index is not on this server and could not be restored, so nothing was indexed: %w", err)
		}
	}

	paths, err := WalkIndexableFiles(absRoot, IndexWalkOptions{IncludeGenerated: s.cfg.IndexGenerated})
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}

	treeText, err := s.mapper.BuildTree(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("build tree: %w", err)
	}

	isNew := existingErr != nil
	if isNew {
		index, err = s.store.CreateProjectIndex(ctx, projectID, absRoot, treeText)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("create project index: %w", err)
		}
	} else {
		index = existing
		index.RootPath = absRoot
		index.TreeText = treeText
	}
	indexID = index.ID
	return s.runIndexPass(ctx, index, absRoot, paths, treeText, isNew, force)
}

func (s *Service) IndexBranch(ctx context.Context, projectID uuid.UUID, branch, workspacePath string) (index domain.WorkspaceIndex, retErr error) {
	var indexID uuid.UUID
	defer func() {
		if retErr != nil {
			s.failIndex(ctx, indexID, retErr)
		}
	}()

	if !s.cfg.Enabled {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}
	if branch == "" {
		return domain.WorkspaceIndex{}, fmt.Errorf("branch is required")
	}

	absRoot, err := filepath.Abs(workspacePath)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	paths, err := WalkIndexableFiles(absRoot, IndexWalkOptions{IncludeGenerated: s.cfg.IndexGenerated})
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}
	treeText, err := s.mapper.BuildTree(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("build tree: %w", err)
	}

	existing, existingErr := s.store.GetIndexByProjectBranch(ctx, projectID, branch)
	isNew := existingErr != nil
	if isNew {
		index, err = s.store.CreateProjectBranchIndex(ctx, projectID, branch, absRoot, treeText)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("create branch index: %w", err)
		}
		if base, baseErr := s.store.GetIndexByProject(ctx, projectID); baseErr == nil && base.Status == domain.IndexStatusCompleted {
			if copyErr := s.store.CopyIndexData(ctx, base.ID, index.ID); copyErr != nil {
				log.Warn().Err(copyErr).Str("branch", branch).Msg("seed branch index from base failed; full pass")
			} else {

				isNew = false
			}
		}
	} else {
		index = existing
		index.RootPath = absRoot
		index.TreeText = treeText
	}
	indexID = index.ID
	return s.runIndexPass(ctx, index, absRoot, paths, treeText, isNew, false)
}

func (s *Service) StartIndexBranch(ctx context.Context, projectID uuid.UUID, branch, workspacePath string) {
	if s == nil || !s.cfg.Enabled || branch == "" || projectID == uuid.Nil {
		return
	}
	key := projectID.String() + "|" + branch
	s.projectMu.Lock()
	if s.branchRunning == nil {
		s.branchRunning = make(map[string]context.CancelFunc)
	}
	if _, busy := s.branchRunning[key]; busy {
		s.projectMu.Unlock()
		return
	}
	passCtx, cancel := context.WithTimeout(detachIndexContext(ctx), 30*time.Minute)
	s.branchRunning[key] = cancel
	s.projectMu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.projectMu.Lock()
			delete(s.branchRunning, key)
			s.projectMu.Unlock()
		}()
		if _, err := s.IndexBranch(passCtx, projectID, branch, workspacePath); err != nil {
			log.Warn().Err(err).Str("project_id", projectID.String()).Str("branch", branch).Msg("branch index refresh failed")
		}
	}()
}

func (s *Service) runIndexPass(ctx context.Context, index domain.WorkspaceIndex, absRoot string, paths []string, treeText string, isNew, force bool) (domain.WorkspaceIndex, error) {
	if err := s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusRunning, 0, 0, 0, ""); err != nil {
		return domain.WorkspaceIndex{}, err
	}
	_ = s.store.UpdateIndexProgress(ctx, index.ID, 0, len(paths))

	prov := s.newPassProvenance(ctx)

	if prov.staleAgainst(index) {
		log.Info().
			Str("index_id", index.ID.String()).
			Str("indexed_with", index.EmbeddingModel).
			Int("indexed_dims", index.EmbeddingDims).
			Str("configured", prov.model).
			Msg("index was built with a different embedding model; re-embedding every file instead of only the changed ones")
		force = true
	}

	if len(paths) == 0 {
		return s.finalizeIndex(ctx, index, absRoot, paths, treeText, prov)
	}

	workList := paths
	incremental := false
	if s.cfg.ReindexOnChange && !isNew {
		storedHashes, err := s.store.GetFileHashes(ctx, index.ID)
		if err == nil && len(storedHashes) > 0 {
			changes := DetectChangedFiles(absRoot, paths, storedHashes)
			if force {

				changes.Changed = append(changes.Changed, changes.Unchanged...)
				changes.Unchanged = nil
			}
			if len(changes.Added) == 0 && len(changes.Changed) == 0 && len(changes.Removed) == 0 {

				_ = s.store.UpdateIndexTree(ctx, index.ID, absRoot, treeText)
				_ = s.store.UpdateIndexProgress(ctx, index.ID, len(paths), len(paths))
				if err := s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusCompleted, len(paths), index.ChunkCount, index.SymbolCount, ""); err != nil {
					return domain.WorkspaceIndex{}, err
				}
				index.Status = domain.IndexStatusCompleted
				index.FileCount = len(paths)
				index.FilesProcessed = len(paths)
				index.FilesTotal = len(paths)
				return index, nil
			}

			stale := append(append([]string{}, changes.Changed...), changes.Removed...)
			stale = append(stale, changes.Added...)
			if err := s.store.DeleteFileData(ctx, index.ID, stale); err != nil {
				return domain.WorkspaceIndex{}, fmt.Errorf("clear stale file data: %w", err)
			}
			workList = append(append([]string{}, changes.Added...), changes.Changed...)
			incremental = true
		}
	}

	if !incremental && !isNew {

		if err := s.store.DeleteFileData(ctx, index.ID, workList); err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("clear stale file data: %w", err)
		}
	}

	if !isNew {
		if err := s.store.DeleteFilesNotIn(ctx, index.ID, paths); err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("drop removed file data: %w", err)
		}
	}

	progress := func(processed, total int) {
		_ = s.store.UpdateIndexProgress(ctx, index.ID, processed, total)
	}

	if workers := s.indexConcurrency(); workers > 1 && len(workList) > 1 {
		return s.runWorkList(ctx, index, absRoot, paths, treeText, workList, workers, prov)
	}

	for i, rel := range workList {
		if err := ctx.Err(); err != nil {

			msg := fmt.Sprintf("indexing stopped after %d/%d files; the next run continues from here", i, len(workList))

			stopCtx := context.WithoutCancel(ctx)
			_ = s.store.UpdateIndexStatus(stopCtx, index.ID, domain.IndexStatusFailed, 0, 0, 0, msg)
			log.Info().Str("index_id", index.ID.String()).Int("done", i).Int("total", len(workList)).Msg("index pass stopped")
			return domain.WorkspaceIndex{}, fmt.Errorf("%s: %w", msg, err)
		}
		progress(i, len(workList))
		fileChunks, fileSymbols, fileEdges, err := s.processFile(absRoot, rel)
		if err != nil {
			_ = s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusFailed, 0, 0, 0, err.Error())
			return domain.WorkspaceIndex{}, fmt.Errorf("process %s: %w", rel, err)
		}

		if err := s.persistFile(ctx, index.ID, absRoot, rel, fileChunks, fileSymbols, fileEdges, prov); err != nil {
			_ = s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusFailed, 0, 0, 0, err.Error())
			return domain.WorkspaceIndex{}, err
		}
		progress(i+1, len(workList))
	}

	return s.finalizeIndex(ctx, index, absRoot, paths, treeText, prov)
}

const defaultIndexConcurrency = 4

const maxIndexConcurrency = 16

func (s *Service) indexConcurrency() int {
	n := s.cfg.Concurrency
	if n <= 0 {
		n = defaultIndexConcurrency
	}
	if n > maxIndexConcurrency {
		n = maxIndexConcurrency
	}
	return n
}

func (s *Service) runWorkList(
	ctx context.Context,
	index domain.WorkspaceIndex,
	absRoot string,
	paths []string,
	treeText string,
	workList []string,
	workers int,
	prov *passProvenance,
) (domain.WorkspaceIndex, error) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(workers)

	var done int64
	total := len(workList)
	for _, rel := range workList {
		rel := rel
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			fileChunks, fileSymbols, fileEdges, err := s.processFile(absRoot, rel)
			if err != nil {
				return fmt.Errorf("process %s: %w", rel, err)
			}
			if err := s.persistFile(groupCtx, index.ID, absRoot, rel, fileChunks, fileSymbols, fileEdges, prov); err != nil {
				return err
			}

			n := atomic.AddInt64(&done, 1)
			_ = s.store.UpdateIndexProgress(ctx, index.ID, int(n), total)
			return nil
		})
	}

	if err := group.Wait(); err != nil {

		stopCtx := context.WithoutCancel(ctx)
		msg := err.Error()
		if ctx.Err() != nil {
			msg = fmt.Sprintf("indexing stopped after %d/%d files; the next run continues from here", atomic.LoadInt64(&done), total)
		}
		_ = s.store.UpdateIndexStatus(stopCtx, index.ID, domain.IndexStatusFailed, 0, 0, 0, msg)
		return domain.WorkspaceIndex{}, fmt.Errorf("%s", msg)
	}
	return s.finalizeIndex(ctx, index, absRoot, paths, treeText, prov)
}

const (
	rateLimitBackoff    = 5 * time.Second
	rateLimitMaxBackoff = 2 * time.Minute
)

func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate_limited") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "too many requests")
}

func (s *Service) embedWithBackoff(ctx context.Context, input string, budget time.Duration) ([]float32, error) {
	wait := rateLimitBackoff
	transient := 0
	for attempt := 1; ; attempt++ {
		embedCtx, cancel := context.WithTimeout(ctx, budget)
		emb, err := s.llm.Embed(embedCtx, input, s.embeddingModel)
		cancel()
		if err == nil {
			return emb, nil
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isRateLimited(err) {
			if !isTransientEmbedError(err) || transient >= maxTransientEmbedAttempts-1 {
				return nil, err
			}
			transient++
			backoff := transientEmbedBackoff << (transient - 1)
			log.Info().Err(err).Dur("wait", backoff).Int("attempt", transient+1).
				Msg("embedder briefly unreachable; retrying the same chunk")
			retry := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				retry.Stop()
				return nil, ctx.Err()
			case <-retry.C:
			}
			continue
		}
		sleep := wait
		if hinted := retryAfterHint(err); hinted > 0 {
			sleep = hinted
		}
		log.Info().Err(err).Dur("wait", sleep).Int("attempt", attempt).
			Msg("embedding rate limited; waiting and continuing from the same chunk")
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if wait < rateLimitMaxBackoff {
			wait *= 2
			if wait > rateLimitMaxBackoff {
				wait = rateLimitMaxBackoff
			}
		}
	}
}

func retryAfterHint(err error) time.Duration {
	msg := strings.ToLower(err.Error())
	idx := strings.Index(msg, "retry after ")
	if idx < 0 {
		return 0
	}
	rest := msg[idx+len("retry after "):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	secs, err2 := strconv.Atoi(rest[:end])
	if err2 != nil || secs <= 0 {
		return 0
	}
	if d := time.Duration(secs) * time.Second; d < rateLimitMaxBackoff {
		return d
	}
	return rateLimitMaxBackoff
}

func (s *Service) persistFile(
	ctx context.Context,
	indexID uuid.UUID,
	absRoot, relPath string,
	chunks []domain.WorkspaceChunk,
	symbols []domain.WorkspaceSymbol,
	edges []domain.WorkspaceEdge,
	prov *passProvenance,
) error {
	unembedded := 0
	for i := range chunks {
		input := FormatEmbedInput(chunks[i].FilePath, chunks[i].SymbolName, chunks[i].Signature, capEmbedContent(chunks[i].Content))
		budget := s.embedBudget
		if budget <= 0 {
			budget = perChunkEmbedTimeout
		}
		emb, err := s.embedChunk(ctx, input, budget)
		switch {
		case err == nil:
			prov.embedSucceeded()
			prov.observe(emb)
			chunks[i].Embedding = emb
		case ctx.Err() != nil:
			return fmt.Errorf("embed chunk: %w", err)
		default:

			if streak := prov.embedFailed(); streak >= maxConsecutiveEmbedFailures {
				return fmt.Errorf("embed chunk: %d chunks in a row could not be embedded: %w", streak, err)
			}
			unembedded++
			log.Warn().Err(err).Str("file", chunks[i].FilePath).Str("symbol", chunks[i].SymbolName).
				Msg("chunk stored without an embedding")
		}
		chunks[i].IndexID = indexID
	}
	for i := range symbols {
		symbols[i].IndexID = indexID
	}
	for i := range edges {
		edges[i].IndexID = indexID
	}
	if len(symbols) > 0 {
		if err := s.store.SaveSymbols(ctx, indexID, symbols); err != nil {
			return fmt.Errorf("save symbols: %w", err)
		}
	}
	if len(chunks) > 0 {
		if err := s.store.SaveChunks(ctx, indexID, chunks); err != nil {
			return fmt.Errorf("save chunks: %w", err)
		}
	}
	if len(edges) > 0 {
		if err := s.store.SaveEdges(ctx, indexID, edges); err != nil {
			return fmt.Errorf("save edges: %w", err)
		}
	}
	if unembedded > 0 {

		return nil
	}
	hash, err := HashFile(absRoot, relPath)
	if err != nil {
		return fmt.Errorf("hash %s: %w", relPath, err)
	}
	return s.store.SaveFileHashes(ctx, indexID, []domain.WorkspaceFileHash{{FilePath: relPath, Hash: hash}})
}

func (s *Service) finalizeIndex(
	ctx context.Context,
	index domain.WorkspaceIndex,
	absRoot string,
	paths []string,
	treeText string,
	prov *passProvenance,
) (domain.WorkspaceIndex, error) {

	if err := s.store.UpdateIndexTree(ctx, index.ID, absRoot, treeText); err != nil {
		log.Warn().Err(err).Str("index_id", index.ID.String()).Msg("update index tree failed")
	}

	chunkCount, symbolCount := index.ChunkCount, index.SymbolCount
	if storedChunks, storedSymbols, err := s.store.CountIndexData(ctx, index.ID); err == nil {
		chunkCount, symbolCount = storedChunks, storedSymbols
	}

	now := time.Now()
	index.Status = domain.IndexStatusCompleted
	index.FileCount = len(paths)
	index.ChunkCount = chunkCount
	index.SymbolCount = symbolCount
	index.TreeText = treeText
	index.IndexedAt = &now

	if err := s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusCompleted, index.FileCount, index.ChunkCount, index.SymbolCount, ""); err != nil {
		return domain.WorkspaceIndex{}, err
	}
	_ = s.store.UpdateIndexProgress(ctx, index.ID, index.FileCount, index.FileCount)

	if sha := GitHeadSHA(absRoot); sha != "" {
		if err := s.store.UpdateIndexCommit(ctx, index.ID, sha); err != nil {
			log.Warn().Err(err).Str("index_id", index.ID.String()).Msg("stamp index commit failed")
		} else {
			index.CommitSHA = sha
		}
	}

	index.EmbeddingModel = prov.modelName()
	index.EmbeddingDims = prov.dimensions()
	if err := s.store.UpdateIndexEmbedding(ctx, index.ID, index.EmbeddingModel, index.EmbeddingDims); err != nil {
		log.Warn().Err(err).Str("index_id", index.ID.String()).Msg("stamp index embedding provenance failed")
	}

	return index, nil
}

func GitHeadSHA(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *Service) processFile(root, relPath string) ([]domain.WorkspaceChunk, []domain.WorkspaceSymbol, []domain.WorkspaceEdge, error) {
	fullPath := filepath.Join(root, relPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, nil, nil, err
	}

	rawChunks, err := s.chunkers.Chunk(relPath, content)
	if err != nil {
		return nil, nil, nil, err
	}

	chunks := make([]domain.WorkspaceChunk, 0, len(rawChunks))
	symbols := make([]domain.WorkspaceSymbol, 0, len(rawChunks))
	seenSymbols := make(map[string]struct{})

	for _, ch := range rawChunks {
		chunkID := uuid.New()
		chunks = append(chunks, domain.WorkspaceChunk{
			ID:         chunkID,
			FilePath:   relPath,
			SymbolName: ch.SymbolName,
			Kind:       ch.Kind,
			StartLine:  ch.StartLine,
			EndLine:    ch.EndLine,
			Language:   ch.Language,
			Signature:  ch.Signature,
			Content:    ch.Content,
		})

		if ch.SymbolName == "" || ch.Kind == "block" {
			continue
		}
		key := relPath + ":" + ch.SymbolName
		if _, ok := seenSymbols[key]; ok {
			continue
		}
		seenSymbols[key] = struct{}{}
		symbols = append(symbols, domain.WorkspaceSymbol{
			ID:        uuid.New(),
			FilePath:  relPath,
			Kind:      ch.Kind,
			Name:      ch.SymbolName,
			Signature: ch.Signature,
			StartLine: ch.StartLine,
			EndLine:   ch.EndLine,
		})
	}

	edges := s.buildEdges(relPath, content, rawChunks)
	return chunks, symbols, edges, nil
}

func (s *Service) buildEdges(relPath string, content []byte, chunks []chunker.Chunk) []domain.WorkspaceEdge {
	return BuildFileEdges(relPath, content, chunks)
}

func BuildFileEdges(relPath string, content []byte, chunks []chunker.Chunk) []domain.WorkspaceEdge {
	var edges []domain.WorkspaceEdge
	lower := strings.ToLower(relPath)

	switch {
	case strings.HasSuffix(lower, ".go"):
		importEdges, err := graph.ExtractGoImports(relPath, content)
		if err == nil {
			for _, e := range importEdges {
				edges = append(edges, domainEdgeFromGraph(e))
			}
		}
		callEdges, err := graph.BuildGoCallGraph(relPath, content)
		if err == nil {
			for _, e := range callEdges {
				edges = append(edges, domainEdgeFromGraph(e))
			}
		}
	case graph.HasGrammarGraph(relPath):

		for _, e := range graph.ExtractImports(relPath, content) {
			edges = append(edges, domainEdgeFromGraph(e))
		}
		for _, ch := range chunks {
			if !callableChunkKinds[ch.Kind] {
				continue
			}
			for _, e := range graph.BuildCallGraphInRange(relPath, ch.SymbolName, content, ch.StartLine, ch.EndLine) {
				edges = append(edges, domainEdgeFromGraph(e))
			}
		}
	}

	return edges
}

var callableChunkKinds = map[string]bool{
	"function": true,
	"method":   true,
	"const":    true,
	"property": true,
}

func domainEdgeFromGraph(e graph.Edge) domain.WorkspaceEdge {
	return domain.WorkspaceEdge{
		FromFile:   e.From.FilePath,
		FromSymbol: e.From.SymbolName,
		ToFile:     e.To.FilePath,
		ToSymbol:   e.To.SymbolName,
		EdgeKind:   string(e.Kind),
	}
}

func DefaultChunkerRegistry() *chunker.Registry {
	return chunker.DefaultRegistry()
}

func NewInjectorFromService(s *Service, mapperSvc *mapper.Service) *Injector {
	return NewInjector(s.store, s.llm, mapperSvc, s.embeddingModel, s.graphCfg)
}

func isTransientEmbedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"embeddings unreachable", "connection reset", "connection refused", "broken pipe",
		"unexpected eof", "embeddings returned 502", "embeddings returned 503", "embeddings returned 504",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func embedConcurrency(cfg domain.IndexerConfig) int {
	if cfg.EmbedConcurrency > 0 {
		return cfg.EmbedConcurrency
	}
	return defaultEmbedConcurrency
}

func (s *Service) embedChunk(ctx context.Context, input string, budget time.Duration) ([]float32, error) {
	if s.embedSlots != nil {
		select {
		case s.embedSlots <- struct{}{}:
			defer func() { <-s.embedSlots }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.embedWithBackoff(ctx, input, budget)
}
