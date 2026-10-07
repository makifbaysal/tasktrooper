package indexer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/suite"
)

type InjectSuite struct {
	suite.Suite
	store     *fakeIndexStore
	llm       *fakeLLM
	injector  *indexer.Injector
	sessionID uuid.UUID
	indexID   uuid.UUID
}

func (s *InjectSuite) SetupTest() {
	s.store = newFakeIndexStore()
	s.llm = &fakeLLM{
		embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
			if strings.Contains(input, "encryption") {
				return []float32{1, 0}, nil
			}
			return []float32{0, 1}, nil
		},
	}
	s.injector = indexer.NewInjector(
		s.store,
		s.llm,
		mapper.NewService(domain.MappingConfig{Enabled: true}),
		"embed-model",
		domain.GraphConfig{Enabled: true, MaxExpansionDepth: 2, MaxExpandedChunks: 4},
	)
	s.sessionID = uuid.New()
	s.indexID = uuid.New()

	sid := s.sessionID
	idx := domain.WorkspaceIndex{
		ID:        s.indexID,
		SessionID: &sid,
		RootPath:  mapperFixtureRoot(),
		Status:    domain.IndexStatusCompleted,
		TreeText:  "project/\n├── pkg/\n",
	}
	s.store.indexes[s.indexID] = idx
	s.store.bySession[s.sessionID] = s.indexID

	s.store.chunks[s.indexID] = []domain.WorkspaceChunk{
		{
			ID:         uuid.New(),
			IndexID:    s.indexID,
			FilePath:   "internal/auth/encrypt.go",
			SymbolName: "Encrypt",
			Kind:       "function",
			StartLine:  42,
			EndLine:    78,
			Language:   "go",
			Signature:  "func Encrypt(password string) (string, error)",
			Content:    "func Encrypt(password string) (string, error) {\n\treturn hash(password)\n}",
			Embedding:  []float32{1, 0},
		},
	}
	s.store.symbols[s.indexID] = []domain.WorkspaceSymbol{
		{
			ID:        uuid.New(),
			IndexID:   s.indexID,
			FilePath:  "internal/auth/encrypt.go",
			Kind:      "function",
			Name:      "Encrypt",
			Signature: "func Encrypt(password string) (string, error)",
			StartLine: 42,
			EndLine:   78,
		},
	}
}

func (s *InjectSuite) TestInjectContextPrependsSystemMessage() {
	messages := []domain.Message{
		{Role: domain.RoleUser, Content: "How do I fix encryption errors?"},
	}
	out, err := s.injector.InjectContext(context.Background(), s.sessionID, messages, domain.InjectOptions{
		TopK:        1,
		IncludeTree: true,
	})
	s.Require().NoError(err)
	s.Len(out, 2)
	s.Equal(domain.RoleSystem, out[0].Role)
	s.Contains(out[0].Content, "## Repository structure")
	s.Contains(out[0].Content, "project/")
	s.Contains(out[0].Content, "## Relevant code")
	s.Contains(out[0].Content, "### internal/auth/encrypt.go:Encrypt (lines 42-78)")
	s.Contains(out[0].Content, "```go")
	s.Equal(domain.RoleUser, out[1].Role)
}

func (s *InjectSuite) TestTheSkeletonShowsTheWorkspaceAsItIsNow() {
	root := s.T().TempDir()
	source := filepath.Join(root, "svc.go")
	s.Require().NoError(os.WriteFile(source, []byte("package svc\n\nfunc FirstName() {}\n"), 0o644))
	idx := s.store.indexes[s.indexID]
	idx.RootPath = root
	idx.CommitSHA = "c1"
	s.store.indexes[s.indexID] = idx
	inject := func() string {
		out, err := s.injector.InjectContext(context.Background(), s.sessionID,
			[]domain.Message{{Role: domain.RoleUser, Content: "where is the service?"}},
			domain.InjectOptions{TopK: 1, IncludeSkeleton: true})
		s.Require().NoError(err)
		return out[0].Content
	}

	s.Contains(inject(), "FirstName")
	s.Require().NoError(os.WriteFile(source, []byte("package svc\n\nfunc SecondName() {}\n"), 0o644))
	s.Contains(inject(), "SecondName", "an edit made after the index commit is what the next run reads")
}

func (s *InjectSuite) TestInjectContextSkipsWhenIndexNotCompleted() {
	sid := s.sessionID
	s.store.indexes[s.indexID] = domain.WorkspaceIndex{
		ID:        s.indexID,
		SessionID: &sid,
		Status:    domain.IndexStatusRunning,
	}
	messages := []domain.Message{{Role: domain.RoleUser, Content: "hello"}}
	out, err := s.injector.InjectContext(context.Background(), s.sessionID, messages, domain.InjectOptions{TopK: 1})
	s.Require().NoError(err)
	s.Len(out, 1)
}

func (s *InjectSuite) TestInjectContextExpandGraphTargetSymbol() {
	messages := []domain.Message{{Role: domain.RoleUser, Content: "change Encrypt"}}
	out, err := s.injector.InjectContext(context.Background(), s.sessionID, messages, domain.InjectOptions{
		TargetSymbol:   "Encrypt",
		TargetFilePath: "internal/auth/encrypt.go",
		ExpandGraph:    true,
		IncludeTree:    true,
		TopK:           1,
	})
	s.Require().NoError(err)
	s.Require().Len(out, 2)
	s.Contains(out[0].Content, "Encrypt")
}

func TestInjectSuite(t *testing.T) {
	suite.Run(t, new(InjectSuite))
}
