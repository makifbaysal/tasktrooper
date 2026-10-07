package indexer_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/indexer"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ProjectRefreshSuite struct {
	suite.Suite
	store     *fakeIndexStore
	svc       *indexer.Service
	projectID uuid.UUID
	embeds    atomic.Int64
}

func TestProjectRefreshSuite(t *testing.T) {
	suite.Run(t, new(ProjectRefreshSuite))
}

func (s *ProjectRefreshSuite) SetupTest() {
	s.store = newFakeIndexStore()
	s.embeds.Store(0)
	llm := &fakeLLM{embedFn: func(_ context.Context, input string, _ string) ([]float32, error) {
		s.embeds.Add(1)
		return []float32{float32(len(input))}, nil
	}}
	s.svc = indexer.NewService(
		s.store,
		llm,
		mapper.NewService(domain.MappingConfig{Enabled: true, MaxFiles: 50, TreeMaxDepth: 4}),
		chunker.DefaultRegistry(),
		domain.IndexerConfig{Enabled: true, TopK: 5, ReindexOnChange: true},
		domain.GraphConfig{Enabled: true},
		"embed-model",
	)
	s.projectID = uuid.New()
	s.pass(s.svc.StartIndexProject)
	s.Require().Positive(s.embeds.Load(), "the first pass embeds the tree")
}

func (s *ProjectRefreshSuite) pass(start func(context.Context, uuid.UUID, string, func())) {
	done := make(chan struct{})
	start(context.Background(), s.projectID, mapperFixtureRoot(), func() { close(done) })
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		s.FailNow("index pass did not finish")
	}
	idx, err := s.store.GetIndexByProject(context.Background(), s.projectID)
	s.Require().NoError(err)
	s.Require().Equal(domain.IndexStatusCompleted, idx.Status)
}

func (s *ProjectRefreshSuite) TestRefreshReEmbedsNothingWhenNoFileChanged() {
	s.embeds.Store(0)

	s.pass(s.svc.RefreshIndexProject)

	s.Zero(s.embeds.Load(), "a push or poll refresh is incremental")
}

func (s *ProjectRefreshSuite) TestRestartStillReEmbedsEveryFile() {
	s.embeds.Store(0)

	s.pass(s.svc.RestartIndexProject)

	s.Positive(s.embeds.Load(), "an explicit reindex is still a full pass")
}
