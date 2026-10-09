package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// LocalIndexStore is the code index the executor keeps on the person's own
// computer. It is a disposable cache: it has no repository table of its own to
// hang index rows from, so a repository is registered by key the first time it
// is indexed, and old branch indexes are listed and dropped to bound its size.
type LocalIndexStore interface {
	IndexStore
	EnsureRepository(ctx context.Context, id uuid.UUID, repoKey string) error
	ListBranchIndexes(ctx context.Context) ([]domain.WorkspaceIndex, error)
	DeleteIndex(ctx context.Context, indexID uuid.UUID) error
}

// LocalIndexStoreOpener starts the store on first use and hands back the same
// one after that; a failed start is retried by the next call.
type LocalIndexStoreOpener interface {
	Open(ctx context.Context) (LocalIndexStore, error)
}

// LocalIndexRef names one index: a repository by the caller's stable key, and
// optionally the branch a task works on. An empty branch is the repository's
// base index.
type LocalIndexRef struct {
	RepoKey string
	Branch  string
}

type LocalIndexEnsure struct {
	LocalIndexRef
	// Dir is the absolute checkout to index, already on disk.
	Dir string
}

const (
	LocalIndexPhaseStore    = "store"
	LocalIndexPhaseWaiting  = "waiting"
	LocalIndexPhaseSeeded   = "seeded"
	LocalIndexPhaseIndexing = "indexing"
)

type LocalIndexProgress struct {
	Phase          string
	FilesProcessed int
	FilesTotal     int
	SeededFrom     string
}

// LocalIndexState describes one index after a pass or as found by a search.
// SeededFrom is set when a new branch index started from another index's rows
// ("base", or "branch:<name>") instead of embedding the whole tree.
type LocalIndexState struct {
	RepoKey        string
	Branch         string
	Status         domain.IndexStatus
	Files          int
	Chunks         int
	Symbols        int
	CommitSHA      string
	EmbeddingModel string
	EmbeddingDims  int
	SeededFrom     string
}

type LocalIndexQuery struct {
	LocalIndexRef
	Query string
	K     int
}

type LocalIndexHit struct {
	Path      string
	Symbol    string
	Kind      string
	Language  string
	Signature string
	StartLine int
	EndLine   int
	Score     float64
	Snippet   string
}

type LocalIndexSearchResult struct {
	Index LocalIndexState
	Hits  []LocalIndexHit
}

// LocalIndexAttachment is a run's view of the local index. The run puts
// RepositoryID and Branch on its context, which is how the index-backed tools
// and the injector find the index; Tools are those tools. ContextMessage
// builds the index context for messages, ok false when there is none.
type LocalIndexAttachment struct {
	RepositoryID   uuid.UUID
	Branch         string
	Tools          []ToolExecutor
	ContextMessage func(ctx context.Context, messages []domain.Message, rewriteQuery bool) (msg domain.Message, ok bool, err error)
}

// LocalCodeIndex is the code index, embeddings and code search that run on
// this computer: code and vectors never leave it. chat, when given, is the
// model a chat turn's query rewrite may use (the run's own provider); nil
// leaves the query as written.
type LocalCodeIndex interface {
	Ensure(ctx context.Context, req LocalIndexEnsure, progress func(LocalIndexProgress)) (LocalIndexState, error)
	Search(ctx context.Context, query LocalIndexQuery) (LocalIndexSearchResult, error)
	Attach(ctx context.Context, ref LocalIndexRef, chat LLMClient) (LocalIndexAttachment, error)
}

// EmbeddingsEndpoint is where this computer's embedding engine answers. The
// desktop restarts its embedder on another port now and then, and the
// executor outlives that: the endpoint moves without a restart.
type EmbeddingsEndpoint interface {
	// SetEmbeddingsEndpoint answers an ErrLocalIndexInvalid error for a URL
	// that is not on this computer. An empty baseURL leaves no engine.
	SetEmbeddingsEndpoint(baseURL, source string) error
	EmbeddingsEndpoint() (baseURL, source string)
}

var (
	// ErrLocalIndexInvalid marks a request that names no valid index or checkout.
	ErrLocalIndexInvalid = errors.New("invalid local index request")
	// ErrLocalIndexUnavailable marks a computer that cannot index at all: no
	// embedder configured, or no place for the store.
	ErrLocalIndexUnavailable = errors.New("the local code index is not available")
	// ErrLocalIndexNotReady marks an index that does not exist yet, is not
	// finished, or was built with another embedding model.
	ErrLocalIndexNotReady = errors.New("the local code index is not ready")
	// ErrLocalIndexEmbedder marks a failure of the embedding engine itself.
	ErrLocalIndexEmbedder = errors.New("the embedding engine failed")
)
