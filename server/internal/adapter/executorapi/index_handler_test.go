package executorapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stretchr/testify/mock"

	appcontext "github.com/makifbaysal/tasktrooper/server/internal/application/context"
	"github.com/makifbaysal/tasktrooper/server/internal/application/executor"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

const indexWorkspace = "repos/app/task-1"

func (s *HandlerSuite) withIndex() *mocks.LocalCodeIndex {
	root := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(root, indexWorkspace), 0o755))
	index := mocks.NewLocalCodeIndex(s.T())
	s.server.Close()
	s.svc = executor.NewService(executor.Deps{
		LLM:           s.llm,
		Providers:     map[string]executor.Provider{"main": {Type: domain.LLMProviderOpenAI, DefaultModel: "gpt-4o"}},
		WorkspaceRoot: root,
		Index:         index,
		Limits:        executor.Limits{MaxIterations: 5, TaskMaxIterations: 5, History: appcontext.Budget{KeepRecentMessages: 10}},
	})
	s.server = httptest.NewServer(NewHandler(s.svc, Options{Token: testToken, Version: "test", Secrets: []string{testKey}}))
	return index
}

func (s *HandlerSuite) TestIndexEnsureStreamsProgressThenTheIndexState() {
	index := s.withIndex()
	index.On("Ensure", mock.Anything, mock.MatchedBy(func(req port.LocalIndexEnsure) bool {
		return req.RepoKey == "github.com/acme/app" && req.Branch == "tt/task-1" && filepath.IsAbs(req.Dir)
	}), mock.Anything).
		Run(func(args mock.Arguments) {
			progress := args.Get(2).(func(port.LocalIndexProgress))
			progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseSeeded, SeededFrom: "base"})
			progress(port.LocalIndexProgress{Phase: port.LocalIndexPhaseIndexing, FilesProcessed: 2, FilesTotal: 2})
		}).
		Return(port.LocalIndexState{
			RepoKey: "github.com/acme/app", Branch: "tt/task-1", Status: domain.IndexStatusCompleted,
			Files: 2, Chunks: 5, Symbols: 3, EmbeddingModel: "nomic-embed-text-v1.5@onnx-int8", EmbeddingDims: 768, SeededFrom: "base",
		}, nil).Once()

	frames := s.frames(s.request(context.Background(), http.MethodPost, PathIndexEnsure, testToken, map[string]any{
		"id": "ix-1", "workspace": indexWorkspace, "repo_key": "github.com/acme/app", "branch": "tt/task-1",
	}))

	s.Require().Len(frames, 4)
	s.Equal("started", frames[0].Event)
	s.Equal("index_progress", frames[1].Payload["kind"])
	s.Equal("seeded", frames[1].Payload["phase"])
	s.Equal("base", frames[1].Payload["seeded_from"])
	s.Equal("indexing", frames[2].Payload["phase"])
	s.InDelta(2, frames[2].Payload["files_total"], 0)
	done := frames[3]
	s.Equal("done", done.Event)
	s.True(*done.OK)
	s.Equal("completed", done.Result["status"])
	s.Equal("tt/task-1", done.Result["branch"])
	s.InDelta(5, done.Result["chunks"], 0)
	s.Equal("nomic-embed-text-v1.5@onnx-int8", done.Result["embedding_model"])
	for _, f := range frames {
		s.Equal("ix-1", f.ID)
	}
}

func (s *HandlerSuite) TestIndexEnsureIsCancelledByItsStreamID() {
	index := s.withIndex()
	running := make(chan struct{})
	index.On("Ensure", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			close(running)
			<-args.Get(0).(context.Context).Done()
		}).
		Return(port.LocalIndexState{}, context.Canceled).Once()
	streamed := make(chan []frameLine, 1)
	go func() {
		streamed <- s.frames(s.request(context.Background(), http.MethodPost, PathIndexEnsure, testToken, map[string]any{
			"id": "ix-2", "workspace": indexWorkspace, "repo_key": "github.com/acme/app",
		}))
	}()
	<-running

	resp := s.request(context.Background(), http.MethodPost, PathCancel, testToken, map[string]string{"id": "ix-2"})
	defer resp.Body.Close()
	var cancelled cancelResponse
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&cancelled))
	s.True(cancelled.Cancelled)

	select {
	case frames := <-streamed:
		last := frames[len(frames)-1]
		s.Equal("done", last.Event)
		s.False(*last.OK)
		s.Equal(executor.CodeCancelled, last.Error.Code)
	case <-time.After(5 * time.Second):
		s.Fail("the index pass did not end after the cancel")
	}
}

func (s *HandlerSuite) TestIndexSearchAnswersRankedChunks() {
	index := s.withIndex()
	index.On("Search", mock.Anything, port.LocalIndexQuery{
		LocalIndexRef: port.LocalIndexRef{RepoKey: "github.com/acme/app", Branch: "tt/task-1"},
		Query:         "refresh token",
		K:             3,
	}).Return(port.LocalIndexSearchResult{
		Index: port.LocalIndexState{RepoKey: "github.com/acme/app", Branch: "tt/task-1", Status: domain.IndexStatusCompleted},
		Hits: []port.LocalIndexHit{{
			Path: "auth.go", Symbol: "RefreshToken", Kind: "function", Language: "go",
			StartLine: 3, EndLine: 9, Score: 0.82, Snippet: "func RefreshToken(refresh string) (string, error) {",
		}},
	}, nil).Once()

	resp := s.request(context.Background(), http.MethodPost, PathIndexSearch, testToken, map[string]any{
		"repo_key": "github.com/acme/app", "branch": "tt/task-1", "query": "refresh token", "k": 3,
	})
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
	var body executor.IndexSearchResult
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal("tt/task-1", body.Index.Branch)
	s.Require().Len(body.Results, 1)
	s.Equal(executor.IndexHit{
		Path: "auth.go", Symbol: "RefreshToken", Kind: "function", Language: "go",
		StartLine: 3, EndLine: 9, Score: 0.82, Snippet: "func RefreshToken(refresh string) (string, error) {",
	}, body.Results[0])
}

type fakeEndpoint struct {
	baseURL, source string
}

func (f *fakeEndpoint) SetEmbeddingsEndpoint(baseURL, source string) error {
	if baseURL != "" && !strings.HasPrefix(baseURL, "http://127.0.0.1:") {
		return fmt.Errorf("%w: %s is not on this computer", port.ErrLocalIndexInvalid, baseURL)
	}
	if source == "" {
		source = domain.EmbeddingSourceONNXInt8
	}
	f.baseURL, f.source = baseURL, source
	return nil
}

func (f *fakeEndpoint) EmbeddingsEndpoint() (string, string) { return f.baseURL, f.source }

func (s *HandlerSuite) TestTheEmbedderMovesWithoutARestart() {
	resp := s.request(context.Background(), http.MethodPost, PathEmbeddings, testToken, map[string]any{"embeddings_base_url": "http://127.0.0.1:5000/v1"})
	s.Equal(http.StatusServiceUnavailable, resp.StatusCode, "an executor without an index has no embedder to move")
	s.Equal(executor.CodeNotReady, s.errorOf(resp).Code)

	endpoint := &fakeEndpoint{}
	s.server.Close()
	s.svc = executor.NewService(executor.Deps{
		LLM:        s.llm,
		Providers:  map[string]executor.Provider{"main": {Type: domain.LLMProviderOpenAI, DefaultModel: "gpt-4o"}},
		Embeddings: endpoint,
		Limits:     executor.Limits{MaxIterations: 5, TaskMaxIterations: 5, History: appcontext.Budget{KeepRecentMessages: 10}},
	})
	s.server = httptest.NewServer(NewHandler(s.svc, Options{Token: testToken, Version: "test"}))

	moved := s.request(context.Background(), http.MethodPost, PathEmbeddings, testToken, map[string]any{"embeddings_base_url": " http://127.0.0.1:6001/v1 "})
	defer moved.Body.Close()
	s.Equal(http.StatusOK, moved.StatusCode)
	var state executor.EmbeddingsState
	s.Require().NoError(json.NewDecoder(moved.Body).Decode(&state))
	s.Equal(executor.EmbeddingsState{V: 1, BaseURL: "http://127.0.0.1:6001/v1", Source: domain.EmbeddingSourceONNXInt8}, state)

	refused := s.request(context.Background(), http.MethodPost, PathEmbeddings, testToken, map[string]any{"embeddings_base_url": "https://embed.example.com/v1"})
	s.Equal(http.StatusBadRequest, refused.StatusCode)
	s.Equal(executor.CodeBadRequest, s.errorOf(refused).Code)
	s.Equal("http://127.0.0.1:6001/v1", endpoint.baseURL)
}
