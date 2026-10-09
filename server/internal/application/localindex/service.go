// Package localindex is the code index the executor keeps on the person's
// computer: it indexes checkouts the runner already put on disk, searches
// them, and gives agent runs their index context and index-backed tools. It
// drives the server's own indexer and injector over a store of its own, so
// the chunking, embedding, incremental passes, branch seeding and hybrid
// search are the ones the server runs, and neither code nor vectors leave
// this computer.
package localindex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	maxRepoKeyLength = 200
	maxSearchK       = 50
	defaultTopK      = 5

	// injectMaxChunkTokens is what the server's board and chat injections cap
	// their chunks at.
	injectMaxChunkTokens = 6000

	defaultKeepBranches = 8
)

// branchName is narrower than git's rules on purpose: it is the same shape
// the runner accepts for a checkout, so anything it prepared fits.
var branchName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]{0,254}$`)

// repositoryNamespace makes a repository key's id the same on every start, so
// an index built before a restart is found after it.
var repositoryNamespace = uuid.MustParse("0a646c9d-0086-4957-88a7-cb7342448f40")

func RepositoryID(repoKey string) uuid.UUID {
	return uuid.NewSHA1(repositoryNamespace, []byte(repoKey))
}

type Config struct {
	Indexer   domain.IndexerConfig
	Graph     domain.GraphConfig
	Mapping   domain.MappingConfig
	Embedding domain.EmbeddingConfig
	// KeepBranches bounds the branch indexes one repository keeps; each one
	// holds a full copy of the rows it was seeded from. 0 means 8.
	KeepBranches int
}

// ToolFactory builds the index-backed tools over the opened store. The tools
// are adapters, so the process wiring supplies them.
type ToolFactory func(store port.IndexStore, embedder port.LLMClient) []port.ToolExecutor

// Embedder is the embedding engine plus what it says it is: the provenance
// every pass is stamped with and every search is checked against.
type Embedder interface {
	port.LLMClient
	port.EmbeddingProvenanceResolver
}

type Deps struct {
	Opener port.LocalIndexStoreOpener
	// Embedder nil, or one that says it is not Configured, means this
	// computer has no embedding engine, and every call answers
	// port.ErrLocalIndexUnavailable.
	Embedder Embedder
	Tools    ToolFactory
	Config   Config
}

type Service struct {
	deps     Deps
	mapper   *mapper.Service
	chunkers *chunker.Registry
	keys     keyLocks

	openMu sync.Mutex
	opened *opened
}

type opened struct {
	store    port.LocalIndexStore
	indexer  *indexer.Service
	injector *indexer.Injector
	tools    []port.ToolExecutor
}

var _ port.LocalCodeIndex = (*Service)(nil)

func NewService(deps Deps) *Service {
	return &Service{
		deps:     deps,
		mapper:   mapper.NewService(deps.Config.Mapping),
		chunkers: chunker.DefaultRegistry(),
	}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", port.ErrLocalIndexInvalid, fmt.Sprintf(format, args...))
}

func validRef(ref port.LocalIndexRef) error {
	key := ref.RepoKey
	if strings.TrimSpace(key) == "" {
		return invalid("repo_key is required")
	}
	if len(key) > maxRepoKeyLength {
		return invalid("repo_key is longer than %d characters", maxRepoKeyLength)
	}
	if strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return invalid("repo_key contains control characters")
	}
	if ref.Branch != "" && !branchName.MatchString(ref.Branch) {
		return invalid("branch %q is not a branch name this executor indexes", ref.Branch)
	}
	return nil
}

// configurable is an embedder whose engine can be absent for a while: the
// desktop starts it after the executor, and moves it.
type configurable interface {
	Configured() bool
}

func (s *Service) available() error {
	if c, ok := s.deps.Embedder.(configurable); s.deps.Embedder == nil || (ok && !c.Configured()) {
		return fmt.Errorf("%w: no embedding engine is configured on this computer (embeddings_base_url)", port.ErrLocalIndexUnavailable)
	}
	if s.deps.Opener == nil {
		return fmt.Errorf("%w: no place for the index store is configured on this computer (data_dir)", port.ErrLocalIndexUnavailable)
	}
	return nil
}

func (s *Service) isOpen() bool {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	return s.opened != nil
}

func (s *Service) open(ctx context.Context) (*opened, error) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if s.opened != nil {
		return s.opened, nil
	}
	store, err := s.deps.Opener.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: the index store did not start: %w", port.ErrLocalIndexUnavailable, err)
	}
	reporting := reportingStore{LocalIndexStore: store}
	client := indexClient{embedder: s.deps.Embedder}
	cfg := s.deps.Config
	idx := indexer.NewService(reporting, client, s.mapper, s.chunkers, cfg.Indexer, cfg.Graph, "")
	idx.SetEmbeddingLimits(cfg.Embedding)
	idx.SetEmbeddingResolver(s.deps.Embedder)
	injector := indexer.NewInjector(store, client, s.mapper, "", cfg.Graph)
	injector.SetQueryRewrite(cfg.Indexer.QueryRewrite)
	var tools []port.ToolExecutor
	if s.deps.Tools != nil {
		tools = s.deps.Tools(store, s.deps.Embedder)
	}
	s.opened = &opened{store: store, indexer: idx, injector: injector, tools: tools}
	return s.opened, nil
}

// Ensure brings the index of one checkout up to date. Only files whose bytes
// changed since the last pass are embedded again, and a branch seen for the
// first time starts from the rows of an index of the same repository.
func (s *Service) Ensure(ctx context.Context, req port.LocalIndexEnsure, progress func(port.LocalIndexProgress)) (port.LocalIndexState, error) {
	if progress == nil {
		progress = func(port.LocalIndexProgress) {}
	}
	if err := validRef(req.LocalIndexRef); err != nil {
		return port.LocalIndexState{}, err
	}
	if info, err := os.Stat(req.Dir); err != nil || !info.IsDir() {
		return port.LocalIndexState{}, invalid("the checkout %s is not a directory on this computer", req.Dir)
	}
	if err := s.available(); err != nil {
		return port.LocalIndexState{}, err
	}
	// Before any pass: a pass that cannot name its model stamps the index
	// with none, and the next search would refuse it as stale.
	if _, _, err := s.deps.Embedder.ResolvedEmbedding(ctx); err != nil {
		return port.LocalIndexState{}, fmt.Errorf("%w: %w", port.ErrLocalIndexEmbedder, err)
	}
	if !s.isOpen() {
		progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseStore})
	}
	o, err := s.open(ctx)
	if err != nil {
		return port.LocalIndexState{}, err
	}
	repoID := RepositoryID(req.RepoKey)
	if err := o.store.EnsureRepository(ctx, repoID, req.RepoKey); err != nil {
		return port.LocalIndexState{}, err
	}

	unlock, err := s.keys.lock(ctx, req.RepoKey+"\x00"+req.Branch, func() {
		progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseWaiting})
	})
	if err != nil {
		return port.LocalIndexState{}, err
	}
	defer unlock()

	passCtx := withProgress(ctx, func(processed, total int) {
		progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseIndexing, FilesProcessed: processed, FilesTotal: total})
	})
	var idx domain.WorkspaceIndex
	seededFrom := ""
	if req.Branch == "" {
		idx, err = o.indexer.IndexProject(passCtx, repoID, req.Dir)
	} else {
		seededFrom, err = s.seedBranch(ctx, o, repoID, req.Branch, req.Dir)
		if err != nil {
			return port.LocalIndexState{}, err
		}
		if seededFrom != "" {
			progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseSeeded, SeededFrom: seededFrom})
		}
		idx, err = o.indexer.IndexBranch(passCtx, repoID, req.Branch, req.Dir)
		if err == nil {
			s.pruneBranches(ctx, o, repoID, idx.ID)
		}
	}
	if err != nil {
		return port.LocalIndexState{}, passError(ctx, err)
	}
	state := stateOf(req.RepoKey, idx)
	state.SeededFrom = seededFrom
	return state, nil
}

func passError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	return err
}

func stateOf(repoKey string, idx domain.WorkspaceIndex) port.LocalIndexState {
	return port.LocalIndexState{
		RepoKey:        repoKey,
		Branch:         idx.Branch,
		Status:         idx.Status,
		Files:          idx.FileCount,
		Chunks:         idx.ChunkCount,
		Symbols:        idx.SymbolCount,
		CommitSHA:      idx.CommitSHA,
		EmbeddingModel: idx.EmbeddingModel,
		EmbeddingDims:  idx.EmbeddingDims,
	}
}

// resolve finds the index a branch reads: the branch's own once it is
// complete, the repository's base index until then. It is the order the
// injector and the code tools resolve by.
func resolve(ctx context.Context, store port.IndexStore, repoID uuid.UUID, branch string) (domain.WorkspaceIndex, bool) {
	if branch != "" {
		if idx, err := store.GetIndexByProjectBranch(ctx, repoID, branch); err == nil && idx.Status == domain.IndexStatusCompleted {
			return idx, true
		}
	}
	idx, err := store.GetIndexByProject(ctx, repoID)
	if err != nil || idx.Status != domain.IndexStatusCompleted {
		return domain.WorkspaceIndex{}, false
	}
	return idx, true
}

func (s *Service) Search(ctx context.Context, query port.LocalIndexQuery) (port.LocalIndexSearchResult, error) {
	if err := validRef(query.LocalIndexRef); err != nil {
		return port.LocalIndexSearchResult{}, err
	}
	text := strings.TrimSpace(query.Query)
	if text == "" {
		return port.LocalIndexSearchResult{}, invalid("query is required")
	}
	if err := s.available(); err != nil {
		return port.LocalIndexSearchResult{}, err
	}
	k := query.K
	if k <= 0 {
		k = s.topK()
	}
	if k > maxSearchK {
		k = maxSearchK
	}
	o, err := s.open(ctx)
	if err != nil {
		return port.LocalIndexSearchResult{}, err
	}
	idx, ok := resolve(ctx, o.store, RepositoryID(query.RepoKey), query.Branch)
	if !ok {
		return port.LocalIndexSearchResult{}, fmt.Errorf("%w: repository %q has no finished index on this computer; run index.ensure first", port.ErrLocalIndexNotReady, query.RepoKey)
	}
	embedding, err := s.deps.Embedder.Embed(ctx, text, "")
	if err != nil {
		return port.LocalIndexSearchResult{}, fmt.Errorf("%w: embed the query: %w", port.ErrLocalIndexEmbedder, err)
	}
	chunks, err := o.store.SearchChunksHybrid(ctx, idx.ID, text, embedding, k)
	if err != nil {
		if errors.Is(err, domain.ErrIndexEmbeddingStale) {
			return port.LocalIndexSearchResult{}, fmt.Errorf("%w: %w", port.ErrLocalIndexNotReady, err)
		}
		return port.LocalIndexSearchResult{}, err
	}
	hits := make([]port.LocalIndexHit, 0, len(chunks))
	for _, ch := range chunks {
		hits = append(hits, port.LocalIndexHit{
			Path: ch.FilePath, Symbol: ch.SymbolName, Kind: ch.Kind, Language: ch.Language,
			Signature: ch.Signature, StartLine: ch.StartLine, EndLine: ch.EndLine,
			Score: ch.Score, Snippet: ch.Content,
		})
	}
	return port.LocalIndexSearchResult{Index: stateOf(query.RepoKey, idx), Hits: hits}, nil
}

func (s *Service) topK() int {
	if s.deps.Config.Indexer.TopK > 0 {
		return s.deps.Config.Indexer.TopK
	}
	return defaultTopK
}

// Attach opens the store if this process has not yet, so an index built
// before a restart serves the first run after it.
func (s *Service) Attach(ctx context.Context, ref port.LocalIndexRef, chat port.LLMClient) (port.LocalIndexAttachment, error) {
	if err := validRef(ref); err != nil {
		return port.LocalIndexAttachment{}, err
	}
	if err := s.available(); err != nil {
		return port.LocalIndexAttachment{}, err
	}
	o, err := s.open(ctx)
	if err != nil {
		return port.LocalIndexAttachment{}, err
	}
	repoID := RepositoryID(ref.RepoKey)
	return port.LocalIndexAttachment{
		RepositoryID: repoID,
		Branch:       ref.Branch,
		Tools:        o.tools,
		ContextMessage: func(ctx context.Context, messages []domain.Message, rewriteQuery bool) (domain.Message, bool, error) {
			ctx = registry.ContextWithBranch(registry.ContextWithProjectID(ctx, repoID), ref.Branch)
			if chat != nil {
				ctx = withChat(ctx, chat)
			}
			injected, err := o.injector.InjectContext(ctx, uuid.Nil, messages, s.injectOptions(rewriteQuery && chat != nil))
			if err != nil {
				return domain.Message{}, false, err
			}
			if len(injected) != len(messages)+1 {
				return domain.Message{}, false, nil
			}
			return injected[0], true, nil
		},
	}, nil
}

func (s *Service) injectOptions(rewriteQuery bool) domain.InjectOptions {
	mapping := s.deps.Config.Mapping
	return domain.InjectOptions{
		TopK:            s.topK(),
		IncludeTree:     mapping.Enabled,
		IncludeSkeleton: mapping.Enabled && mapping.InjectOnSessionStart,
		MaxChunkTokens:  injectMaxChunkTokens,
		RewriteQuery:    rewriteQuery,
	}
}
