package indexer_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeIndexStore struct {
	mu        sync.Mutex
	indexes   map[uuid.UUID]domain.WorkspaceIndex
	bySession map[uuid.UUID]uuid.UUID
	byProject map[uuid.UUID]uuid.UUID
	chunks    map[uuid.UUID][]domain.WorkspaceChunk
	symbols   map[uuid.UUID][]domain.WorkspaceSymbol
	edges     map[uuid.UUID][]domain.WorkspaceEdge
	hashes    map[uuid.UUID]map[string]string
	err       error
}

func newFakeIndexStore() *fakeIndexStore {
	return &fakeIndexStore{
		indexes:   make(map[uuid.UUID]domain.WorkspaceIndex),
		bySession: make(map[uuid.UUID]uuid.UUID),
		byProject: make(map[uuid.UUID]uuid.UUID),
		chunks:    make(map[uuid.UUID][]domain.WorkspaceChunk),
		symbols:   make(map[uuid.UUID][]domain.WorkspaceSymbol),
		edges:     make(map[uuid.UUID][]domain.WorkspaceEdge),
		hashes:    make(map[uuid.UUID]map[string]string),
	}
}

func (f *fakeIndexStore) CreateIndex(_ context.Context, sessionID uuid.UUID, rootPath string, treeText string) (domain.WorkspaceIndex, error) {
	if f.err != nil {
		return domain.WorkspaceIndex{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	sid := sessionID
	idx := domain.WorkspaceIndex{
		ID:        id,
		SessionID: &sid,
		RootPath:  rootPath,
		Status:    domain.IndexStatusPending,
		TreeText:  treeText,
	}
	f.indexes[id] = idx
	f.bySession[sessionID] = id
	return idx, nil
}

func (f *fakeIndexStore) CreateProjectIndex(_ context.Context, projectID uuid.UUID, rootPath string, treeText string) (domain.WorkspaceIndex, error) {
	if f.err != nil {
		return domain.WorkspaceIndex{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	pid := projectID
	idx := domain.WorkspaceIndex{
		ID:        id,
		ProjectID: &pid,
		RootPath:  rootPath,
		Status:    domain.IndexStatusPending,
		TreeText:  treeText,
	}
	f.indexes[id] = idx
	f.byProject[projectID] = id
	return idx, nil
}

func (f *fakeIndexStore) GetIndexByProject(_ context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error) {
	if f.err != nil {
		return domain.WorkspaceIndex{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byProject[projectID]
	if !ok {
		return domain.WorkspaceIndex{}, errors.New("index not found")
	}
	return f.indexes[id], nil
}

func (f *fakeIndexStore) GetIndexByProjectBranch(_ context.Context, projectID uuid.UUID, branch string) (domain.WorkspaceIndex, error) {
	if f.err != nil {
		return domain.WorkspaceIndex{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, idx := range f.indexes {
		if idx.ProjectID != nil && *idx.ProjectID == projectID && idx.Branch == branch {
			return idx, nil
		}
	}
	return domain.WorkspaceIndex{}, errors.New("index not found")
}

func (f *fakeIndexStore) CreateProjectBranchIndex(_ context.Context, projectID uuid.UUID, branch, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	if f.err != nil {
		return domain.WorkspaceIndex{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, idx := range f.indexes {
		if idx.ProjectID != nil && *idx.ProjectID == projectID && idx.Branch == branch {
			delete(f.indexes, id)
		}
	}
	pid := projectID
	idx := domain.WorkspaceIndex{
		ID:        uuid.New(),
		ProjectID: &pid,
		Branch:    branch,
		RootPath:  rootPath,
		Status:    domain.IndexStatusPending,
		TreeText:  treeText,
	}
	f.indexes[idx.ID] = idx
	return idx, nil
}

func (f *fakeIndexStore) CopyIndexData(_ context.Context, fromIndexID, toIndexID uuid.UUID) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if src, ok := f.indexes[fromIndexID]; ok {
		if dst, ok := f.indexes[toIndexID]; ok {
			dst.EmbeddingModel, dst.EmbeddingDims = src.EmbeddingModel, src.EmbeddingDims
			f.indexes[toIndexID] = dst
		}
	}
	f.chunks[toIndexID] = append([]domain.WorkspaceChunk(nil), f.chunks[fromIndexID]...)
	f.symbols[toIndexID] = append([]domain.WorkspaceSymbol(nil), f.symbols[fromIndexID]...)
	f.edges[toIndexID] = append([]domain.WorkspaceEdge(nil), f.edges[fromIndexID]...)
	if src, ok := f.hashes[fromIndexID]; ok {
		dst := make(map[string]string, len(src))
		for k, v := range src {
			dst[k] = v
		}
		if f.hashes == nil {
			f.hashes = make(map[uuid.UUID]map[string]string)
		}
		f.hashes[toIndexID] = dst
	}
	return nil
}

func (f *fakeIndexStore) DeleteFileData(_ context.Context, indexID uuid.UUID, filePaths []string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	drop := make(map[string]bool, len(filePaths))
	for _, p := range filePaths {
		drop[p] = true
	}
	var chunks []domain.WorkspaceChunk
	for _, c := range f.chunks[indexID] {
		if !drop[c.FilePath] {
			chunks = append(chunks, c)
		}
	}
	f.chunks[indexID] = chunks
	var symbols []domain.WorkspaceSymbol
	for _, s := range f.symbols[indexID] {
		if !drop[s.FilePath] {
			symbols = append(symbols, s)
		}
	}
	f.symbols[indexID] = symbols
	var edges []domain.WorkspaceEdge
	for _, e := range f.edges[indexID] {
		if !drop[e.FromFile] {
			edges = append(edges, e)
		}
	}
	f.edges[indexID] = edges
	if h, ok := f.hashes[indexID]; ok {
		for _, p := range filePaths {
			delete(h, p)
		}
	}
	return nil
}

func (f *fakeIndexStore) DeleteFilesNotIn(ctx context.Context, indexID uuid.UUID, keepPaths []string) error {
	if f.err != nil {
		return f.err
	}
	if len(keepPaths) == 0 {
		return nil
	}
	keep := make(map[string]bool, len(keepPaths))
	for _, p := range keepPaths {
		keep[p] = true
	}
	f.mu.Lock()
	var gone []string
	for path := range f.hashes[indexID] {
		if !keep[path] {
			gone = append(gone, path)
		}
	}
	for _, c := range f.chunks[indexID] {
		if !keep[c.FilePath] {
			gone = append(gone, c.FilePath)
		}
	}
	f.mu.Unlock()
	if len(gone) == 0 {
		return nil
	}
	return f.DeleteFileData(ctx, indexID, gone)
}

func (f *fakeIndexStore) UpdateIndexEmbedding(_ context.Context, indexID uuid.UUID, model string, dimensions int) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.indexes[indexID]
	if !ok {
		return errors.New("index not found")
	}
	idx.EmbeddingModel = model
	idx.EmbeddingDims = dimensions
	f.indexes[indexID] = idx
	return nil
}

func (f *fakeIndexStore) UpdateIndexCommit(_ context.Context, indexID uuid.UUID, commitSHA string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.indexes[indexID]
	if !ok {
		return errors.New("index not found")
	}
	idx.CommitSHA = commitSHA
	f.indexes[indexID] = idx
	return nil
}

func (f *fakeIndexStore) UpdateIndexTree(_ context.Context, indexID uuid.UUID, rootPath, treeText string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.indexes[indexID]
	if !ok {
		return errors.New("index not found")
	}
	idx.RootPath = rootPath
	idx.TreeText = treeText
	f.indexes[indexID] = idx
	return nil
}

func (f *fakeIndexStore) CountIndexData(_ context.Context, indexID uuid.UUID) (int, int, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.chunks[indexID]), len(f.symbols[indexID]), nil
}

func (f *fakeIndexStore) UpdateIndexProgress(_ context.Context, indexID uuid.UUID, filesProcessed, filesTotal int) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.indexes[indexID]
	if !ok {
		return errors.New("index not found")
	}
	idx.FilesProcessed = filesProcessed
	idx.FilesTotal = filesTotal
	f.indexes[indexID] = idx
	return nil
}

func (f *fakeIndexStore) GetIndexBySession(_ context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error) {
	if f.err != nil {
		return domain.WorkspaceIndex{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.bySession[sessionID]
	if !ok {
		return domain.WorkspaceIndex{}, errors.New("index not found")
	}
	return f.indexes[id], nil
}

func (f *fakeIndexStore) UpdateIndexStatus(_ context.Context, indexID uuid.UUID, status domain.IndexStatus, fileCount, chunkCount, symbolCount int, errMsg string) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.indexes[indexID]
	if !ok {
		return errors.New("index not found")
	}
	idx.Status = status
	idx.FileCount = fileCount
	idx.ChunkCount = chunkCount
	idx.SymbolCount = symbolCount
	if errMsg != "" {
		idx.Error = errMsg
	} else if status == domain.IndexStatusRunning || status == domain.IndexStatusCompleted {
		idx.Error = ""
	}
	f.indexes[indexID] = idx
	return nil
}

func (f *fakeIndexStore) SaveSymbols(_ context.Context, indexID uuid.UUID, symbols []domain.WorkspaceSymbol) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.symbols[indexID] = append(f.symbols[indexID], symbols...)
	return nil
}

func (f *fakeIndexStore) SaveChunks(_ context.Context, indexID uuid.UUID, chunks []domain.WorkspaceChunk) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chunks[indexID] = append(f.chunks[indexID], chunks...)
	return nil
}

func (f *fakeIndexStore) SaveEdges(_ context.Context, indexID uuid.UUID, edges []domain.WorkspaceEdge) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edges[indexID] = append(f.edges[indexID], edges...)
	return nil
}

func (f *fakeIndexStore) SaveFileHashes(_ context.Context, indexID uuid.UUID, hashes []domain.WorkspaceFileHash) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hashes[indexID] == nil {
		f.hashes[indexID] = make(map[string]string)
	}
	for _, h := range hashes {
		f.hashes[indexID][h.FilePath] = h.Hash
	}
	return nil
}

func (f *fakeIndexStore) GetFileHashes(_ context.Context, indexID uuid.UUID) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	src := f.hashes[indexID]
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out, nil
}

func (f *fakeIndexStore) SearchChunks(ctx context.Context, sessionID uuid.UUID, embedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	f.mu.Lock()
	id, ok := f.bySession[sessionID]
	f.mu.Unlock()
	if !ok {
		return nil, nil
	}
	return f.SearchChunksByIndex(ctx, id, embedding, topK)
}

func (f *fakeIndexStore) SearchChunksHybrid(ctx context.Context, indexID uuid.UUID, _ string, embedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	return f.SearchChunksByIndex(ctx, indexID, embedding, topK)
}

func (f *fakeIndexStore) SearchChunksByIndex(_ context.Context, indexID uuid.UUID, embedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	chunks := f.chunks[indexID]
	type scored struct {
		ch    domain.WorkspaceChunk
		score float64
	}
	var scoredChunks []scored
	for _, ch := range chunks {
		scoredChunks = append(scoredChunks, scored{ch: ch, score: cosineSimilarity(embedding, ch.Embedding)})
	}
	sort.Slice(scoredChunks, func(i, j int) bool {
		return scoredChunks[i].score > scoredChunks[j].score
	})
	if topK > len(scoredChunks) {
		topK = len(scoredChunks)
	}
	result := make([]domain.WorkspaceChunk, 0, topK)
	for i := 0; i < topK; i++ {
		ch := scoredChunks[i].ch
		ch.Score = scoredChunks[i].score
		result = append(result, ch)
	}
	return result, nil
}

func (f *fakeIndexStore) SearchSymbols(_ context.Context, name string, indexID uuid.UUID) ([]domain.WorkspaceSymbol, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []domain.WorkspaceSymbol
	for _, sym := range f.symbols[indexID] {
		if sym.Name == name {
			result = append(result, sym)
		}
	}
	return result, nil
}

func (f *fakeIndexStore) GetChunkBySymbol(_ context.Context, indexID uuid.UUID, filePath, symbolName string) (domain.WorkspaceChunk, error) {
	if f.err != nil {
		return domain.WorkspaceChunk{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.chunks[indexID] {
		if ch.FilePath == filePath && ch.SymbolName == symbolName {
			return ch, nil
		}
	}
	return domain.WorkspaceChunk{}, errors.New("chunk not found")
}

func (f *fakeIndexStore) ListEdges(_ context.Context, indexID uuid.UUID) ([]domain.WorkspaceEdge, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.WorkspaceEdge(nil), f.edges[indexID]...), nil
}

func (f *fakeIndexStore) DeleteIndexData(_ context.Context, indexID uuid.UUID) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.chunks, indexID)
	delete(f.symbols, indexID)
	delete(f.edges, indexID)
	delete(f.hashes, indexID)
	return nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

type fakeLLM struct {
	embedFn func(ctx context.Context, input string, model string) ([]float32, error)
}

func (f *fakeLLM) Chat(_ context.Context, _ domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, errors.New("not implemented")
}

func (f *fakeLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, errors.New("not implemented")
}

func (f *fakeLLM) Models(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeLLM) Embed(ctx context.Context, input string, model string) ([]float32, error) {
	if f.embedFn != nil {
		return f.embedFn(ctx, input, model)
	}
	return []float32{float32(len(input))}, nil
}

func mapperFixtureRoot() string {
	return filepath.Join("..", "mapper", "testdata", "sample")
}
