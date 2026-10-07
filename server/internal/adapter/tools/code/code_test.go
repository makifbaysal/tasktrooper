package code_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/tools/code"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/application/registry"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeIndexStore struct {
	chunks  []domain.WorkspaceChunk
	symbols []domain.WorkspaceSymbol
	edges   []domain.WorkspaceEdge
	index   domain.WorkspaceIndex
}

func (f *fakeIndexStore) CreateIndex(_ context.Context, sessionID uuid.UUID, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	sid := sessionID
	f.index = domain.WorkspaceIndex{ID: uuid.New(), SessionID: &sid, RootPath: rootPath, TreeText: treeText, Status: domain.IndexStatusCompleted}
	return f.index, nil
}

func (f *fakeIndexStore) CreateProjectIndex(_ context.Context, projectID uuid.UUID, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	pid := projectID
	f.index = domain.WorkspaceIndex{ID: uuid.New(), ProjectID: &pid, RootPath: rootPath, TreeText: treeText, Status: domain.IndexStatusCompleted}
	return f.index, nil
}

func (f *fakeIndexStore) GetIndexByProject(_ context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error) {
	if f.index.ProjectID != nil && *f.index.ProjectID == projectID {
		return f.index, nil
	}
	return domain.WorkspaceIndex{}, context.Canceled
}

func (f *fakeIndexStore) GetIndexBySession(_ context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error) {
	if f.index.SessionID != nil && *f.index.SessionID == sessionID {
		return f.index, nil
	}
	return domain.WorkspaceIndex{}, context.Canceled
}

// DeleteFilesNotIn is the incremental reindex's pruning step. This fake keeps
// no per-file rows, so there is nothing to prune — the code tools under test
// only ever read.
func (f *fakeIndexStore) DeleteFilesNotIn(_ context.Context, _ uuid.UUID, _ []string) error {
	return nil
}

func (f *fakeIndexStore) UpdateIndexProgress(_ context.Context, _ uuid.UUID, _, _ int) error {
	return nil
}

func (f *fakeIndexStore) UpdateIndexStatus(_ context.Context, _ uuid.UUID, _ domain.IndexStatus, _, _, _ int, _ string) error {
	return nil
}

func (f *fakeIndexStore) SaveSymbols(_ context.Context, _ uuid.UUID, _ []domain.WorkspaceSymbol) error {
	return nil
}

func (f *fakeIndexStore) SaveChunks(_ context.Context, _ uuid.UUID, chunks []domain.WorkspaceChunk) error {
	f.chunks = append(f.chunks, chunks...)
	return nil
}

func (f *fakeIndexStore) SaveEdges(_ context.Context, _ uuid.UUID, edges []domain.WorkspaceEdge) error {
	f.edges = append(f.edges, edges...)
	return nil
}

func (f *fakeIndexStore) SaveFileHashes(_ context.Context, _ uuid.UUID, _ []domain.WorkspaceFileHash) error {
	return nil
}

func (f *fakeIndexStore) GetFileHashes(_ context.Context, _ uuid.UUID) (map[string]string, error) {
	return nil, nil
}

func (f *fakeIndexStore) SearchChunks(ctx context.Context, _ uuid.UUID, embedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	return f.SearchChunksByIndex(ctx, f.index.ID, embedding, topK)
}

func (f *fakeIndexStore) SearchChunksByIndex(_ context.Context, _ uuid.UUID, _ []float32, topK int) ([]domain.WorkspaceChunk, error) {
	if topK > len(f.chunks) {
		topK = len(f.chunks)
	}
	return append([]domain.WorkspaceChunk(nil), f.chunks[:topK]...), nil
}

func (f *fakeIndexStore) SearchChunksHybrid(ctx context.Context, id uuid.UUID, _ string, embedding []float32, topK int) ([]domain.WorkspaceChunk, error) {
	return f.SearchChunksByIndex(ctx, id, embedding, topK)
}

func (f *fakeIndexStore) SearchSymbols(_ context.Context, name string, _ uuid.UUID) ([]domain.WorkspaceSymbol, error) {
	var out []domain.WorkspaceSymbol
	for _, sym := range f.symbols {
		if sym.Name == name {
			out = append(out, sym)
		}
	}
	return out, nil
}

func (f *fakeIndexStore) GetChunkBySymbol(_ context.Context, _ uuid.UUID, filePath, symbolName string) (domain.WorkspaceChunk, error) {
	for _, ch := range f.chunks {
		if ch.FilePath == filePath && ch.SymbolName == symbolName {
			return ch, nil
		}
	}
	return domain.WorkspaceChunk{}, context.Canceled
}

func (f *fakeIndexStore) ListEdges(_ context.Context, _ uuid.UUID) ([]domain.WorkspaceEdge, error) {
	return append([]domain.WorkspaceEdge(nil), f.edges...), nil
}

func (f *fakeIndexStore) DeleteIndexData(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (f *fakeIndexStore) GetIndexByProjectBranch(_ context.Context, projectID uuid.UUID, branch string) (domain.WorkspaceIndex, error) {
	if f.index.ProjectID != nil && *f.index.ProjectID == projectID && f.index.Branch == branch {
		return f.index, nil
	}
	return domain.WorkspaceIndex{}, context.Canceled
}

func (f *fakeIndexStore) CreateProjectBranchIndex(_ context.Context, projectID uuid.UUID, branch, rootPath, treeText string) (domain.WorkspaceIndex, error) {
	pid := projectID
	f.index = domain.WorkspaceIndex{ID: uuid.New(), ProjectID: &pid, Branch: branch, RootPath: rootPath, TreeText: treeText, Status: domain.IndexStatusCompleted}
	return f.index, nil
}

func (f *fakeIndexStore) CopyIndexData(_ context.Context, _, _ uuid.UUID) error { return nil }

func (f *fakeIndexStore) DeleteFileData(_ context.Context, _ uuid.UUID, _ []string) error { return nil }

func (f *fakeIndexStore) UpdateIndexTree(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}

func (f *fakeIndexStore) UpdateIndexCommit(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func (f *fakeIndexStore) UpdateIndexEmbedding(_ context.Context, _ uuid.UUID, _ string, _ int) error {
	return nil
}

func (f *fakeIndexStore) CountIndexData(_ context.Context, _ uuid.UUID) (int, int, error) {
	return len(f.chunks), len(f.symbols), nil
}

type fakeLLM struct {
	embedding []float32
}

func (f *fakeLLM) Chat(_ context.Context, _ domain.AgentRequest) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (f *fakeLLM) ChatStream(_ context.Context, _ domain.AgentRequest, _ func(string)) (domain.AgentResponse, error) {
	return domain.AgentResponse{}, nil
}

func (f *fakeLLM) Models(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeLLM) Embed(_ context.Context, _ string, _ string) ([]float32, error) {
	return f.embedding, nil
}

type CodeToolsSuite struct {
	suite.Suite
	fixtureRoot string
	sessionID   uuid.UUID
	ctx         context.Context
	kit         *code.ToolKit
	store       *fakeIndexStore
}

func TestCodeToolsSuite(t *testing.T) {
	suite.Run(t, new(CodeToolsSuite))
}

func (s *CodeToolsSuite) SetupSuite() {
	s.fixtureRoot, _ = filepath.Abs(filepath.Join("..", "..", "..", "application", "mapper", "testdata", "sample"))
	s.sessionID = uuid.New()
	s.ctx = registry.ContextWithWorkspaceDir(
		registry.ContextWithSessionID(context.Background(), s.sessionID),
		s.fixtureRoot,
	)
	sid := s.sessionID
	s.store = &fakeIndexStore{
		index: domain.WorkspaceIndex{
			ID:        uuid.New(),
			SessionID: &sid,
			RootPath:  s.fixtureRoot,
			Status:    domain.IndexStatusCompleted,
		},
		chunks: []domain.WorkspaceChunk{
			{
				FilePath:   "pkg/main.go",
				SymbolName: "main",
				StartLine:  10,
				EndLine:    12,
				Signature:  "func main()",
				Content:    "func main() {}",
			},
		},
		symbols: []domain.WorkspaceSymbol{
			{
				FilePath:  "pkg/main.go",
				Kind:      "func",
				Name:      "main",
				Signature: "func main()",
				StartLine: 10,
				EndLine:   12,
			},
			{
				FilePath:  "pkg/main.go",
				Kind:      "method",
				Name:      "Greet",
				Signature: "func (h Hello) Greet() string",
				StartLine: 20,
				EndLine:   22,
			},
			{FilePath: "pkg/a.go", Kind: "func", Name: "helper", Signature: "func helper()"},
			{FilePath: "pkg/b.go", Kind: "func", Name: "helper", Signature: "func helper()"},
		},
		edges: []domain.WorkspaceEdge{
			{
				FromFile:   "pkg/main.go",
				FromSymbol: "main",
				ToFile:     "pkg/main.go",
				ToSymbol:   "Greet",
				EdgeKind:   "call",
			},
		},
	}
	mapperSvc := mapper.NewService(domain.MappingConfig{Enabled: true, TreeMaxDepth: 4, MaxFiles: 50})
	s.kit = code.NewToolKit(s.store, &fakeLLM{embedding: []float32{0.1, 0.2}}, mapperSvc, domain.IndexerConfig{TopK: 3}, domain.GraphConfig{MaxExpansionDepth: 2, MaxExpandedChunks: 8}, "embed-model")
}

func (s *CodeToolsSuite) TestCodebaseSearch() {
	tool := code.NewExecutors(s.kit)[0]
	s.Equal("codebase_search", tool.Name())

	result := tool.Execute(s.ctx, `{"query":"entry point"}`)
	s.False(result.IsError)

	var resp struct {
		Results []struct {
			FilePath   string `json:"file_path"`
			SymbolName string `json:"symbol_name"`
		} `json:"results"`
	}
	s.Require().NoError(json.Unmarshal([]byte(result.Content), &resp))
	s.Require().Len(resp.Results, 1)
	s.Equal("pkg/main.go", resp.Results[0].FilePath)
	s.Equal("main", resp.Results[0].SymbolName)
}

func (s *CodeToolsSuite) TestGrepCode() {
	tool := code.NewExecutors(s.kit)[1]
	result := tool.Execute(s.ctx, `{"pattern":"func main","max_results":5}`)
	s.False(result.IsError)
	s.Contains(result.Content, "pkg/main.go")
}

func (s *CodeToolsSuite) TestGetRepoTree() {
	tool := code.NewExecutors(s.kit)[2]
	result := tool.Execute(s.ctx, `{"prefix":"pkg"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "main.go")
}

func (s *CodeToolsSuite) TestGetSymbolSkeletonByFile() {
	tool := code.NewExecutors(s.kit)[3]
	result := tool.Execute(s.ctx, `{"file_path":"pkg/main.go"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "main")
}

func (s *CodeToolsSuite) TestExpandSymbolContext() {
	tool := code.NewExecutors(s.kit)[4]
	result := tool.Execute(s.ctx, `{"symbol_name":"main","file_path":"pkg/main.go"}`)
	s.False(result.IsError)
	s.Contains(result.Content, "pkg/main.go")
	s.Contains(result.Content, "main")
}

func (s *CodeToolsSuite) TestMissingWorkspace() {
	tool := code.NewExecutors(s.kit)[2]
	result := tool.Execute(context.Background(), `{}`)
	s.True(result.IsError)
}

func (s *CodeToolsSuite) TestInvalidArguments() {
	tool := code.NewExecutors(s.kit)[0]
	result := tool.Execute(s.ctx, `not-json`)
	s.True(result.IsError)
}

// Index rows are root-relative and slash-separated; a model on Windows sends
// `pkg\b.go` or the absolute `C:\ws\pkg\b.go`. The absolute form is the one
// every OS can exercise.
func (s *CodeToolsSuite) TestExpandSymbolContextAcceptsAnAbsoluteFilePath() {
	tool := code.NewExecutors(s.kit)[4]
	payload, err := json.Marshal(map[string]string{
		"symbol_name": "helper",
		"file_path":   filepath.Join(s.fixtureRoot, "pkg", "b.go"),
	})
	s.Require().NoError(err)

	result := tool.Execute(s.ctx, string(payload))

	s.False(result.IsError, result.Content)
	s.Contains(result.Content, "pkg/b.go:helper")
	s.NotContains(result.Content, "pkg/a.go:helper")
}

func (s *CodeToolsSuite) TestGetSymbolSkeletonAcceptsAnAbsoluteFilePath() {
	tool := code.NewExecutors(s.kit)[3]
	for _, args := range []map[string]string{
		{"symbol_name": "Greet", "file_path": filepath.Join(s.fixtureRoot, "pkg", "main.go")},
		{"file_path": filepath.Join(s.fixtureRoot, "pkg", "main.go")},
	} {
		payload, err := json.Marshal(args)
		s.Require().NoError(err)

		result := tool.Execute(s.ctx, string(payload))

		s.False(result.IsError, result.Content)
		s.Contains(result.Content, `"file_path":"pkg/main.go"`)
	}
}

func (s *CodeToolsSuite) TestGetRepoTreeAcceptsAnyPrefixSpelling() {
	tool := code.NewExecutors(s.kit)[2]
	sep := string(filepath.Separator)
	for _, prefix := range []string{"." + sep + "pkg" + sep, filepath.Join(s.fixtureRoot, "pkg")} {
		payload, err := json.Marshal(map[string]string{"prefix": prefix})
		s.Require().NoError(err)

		result := tool.Execute(s.ctx, string(payload))

		s.False(result.IsError, result.Content)
		s.Contains(result.Content, "main.go", prefix)
	}
}
