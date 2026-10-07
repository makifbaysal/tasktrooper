package http

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repository"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type versionedBoardStore struct {
	port.BoardTaskStore
	tasks   []domain.BoardTask
	version uint64
	listed  int
}

func (f *versionedBoardStore) ListBoardVisible(context.Context, time.Time) ([]domain.BoardTask, error) {
	f.listed++
	return append([]domain.BoardTask(nil), f.tasks...), nil
}

func (f *versionedBoardStore) BoardVersion() uint64 { return f.version }

func (f *versionedBoardStore) write(tasks ...domain.BoardTask) {
	f.tasks = tasks
	f.version++
}

type unversionedBoardStore struct {
	port.BoardTaskStore
	tasks  []domain.BoardTask
	listed int
}

func (f *unversionedBoardStore) ListBoardVisible(context.Context, time.Time) ([]domain.BoardTask, error) {
	f.listed++
	return f.tasks, nil
}

type BoardETagSuite struct {
	suite.Suite
	store   *versionedBoardStore
	app     *fiber.App
	task    domain.BoardTask
	handler *Handler
	now     time.Time
}

func TestBoardETagSuite(t *testing.T) {
	suite.Run(t, new(BoardETagSuite))
}

func (s *BoardETagSuite) SetupTest() {
	s.task = domain.BoardTask{ID: uuid.New(), RepositoryID: uuid.New(), Title: "Push", Column: domain.TaskColumnTodo, Key: "T-1"}
	s.store = &versionedBoardStore{tasks: []domain.BoardTask{s.task}}
	s.now = time.Now()
	s.app = s.appFor(s.store)
}

func (s *BoardETagSuite) appFor(tasks port.BoardTaskStore) *fiber.App {
	h := &Handler{repositorySvc: repository.NewService(nil, tasks, nil, nil, nil, nil, nil, nil, nil, nil)}
	h.boardList.now = func() time.Time { return s.now }
	s.handler = h
	app := fiber.New()
	app.Get("/v1/tasks", h.ListAllBoardTasks)
	return app
}

type boardResponse struct {
	status int
	etag   string
	body   string
}

func (s *BoardETagSuite) get(app *fiber.App, ifNoneMatch string) boardResponse {
	req := httptest.NewRequest("GET", "/v1/tasks", nil)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := app.Test(req)
	s.Require().NoError(err)
	body, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	return boardResponse{status: resp.StatusCode, etag: resp.Header.Get("ETag"), body: string(body)}
}

func (s *BoardETagSuite) TestFirstReadIsAFullResponseWithAnETag() {
	resp := s.get(s.app, "")

	s.Equal(200, resp.status)
	s.NotEmpty(resp.etag)
	s.Contains(resp.body, s.task.ID.String())
	s.Contains(resp.body, `"count":1`)
}

func (s *BoardETagSuite) TestUnchangedBoardIsNotReadAgain() {
	first := s.get(s.app, "")

	again := s.get(s.app, first.etag)

	s.Equal(304, again.status)
	s.Equal(first.etag, again.etag)
	s.Empty(again.body)
	s.Equal(1, s.store.listed)
}

func (s *BoardETagSuite) TestAWriteInvalidatesTheETag() {
	first := s.get(s.app, "")
	moved := s.task
	moved.Column = domain.TaskColumnInProgress
	s.store.write(moved)

	after := s.get(s.app, first.etag)

	s.Equal(200, after.status)
	s.NotEqual(first.etag, after.etag)
	s.Contains(after.body, string(domain.TaskColumnInProgress))
	s.Equal(304, s.get(s.app, after.etag).status)
	s.Equal(2, s.store.listed)
}

func (s *BoardETagSuite) TestAWriteThatChangesNothingVisibleStillAnswers304() {
	first := s.get(s.app, "")
	s.store.write(s.task)

	after := s.get(s.app, first.etag)

	s.Equal(304, after.status)
	s.Equal(2, s.store.listed)
}

func (s *BoardETagSuite) TestReleasedTaskAgingOffTheBoardForcesARead() {
	entered := time.Now().Add(-domain.ReleasedBoardWindow - time.Minute)
	released := s.task
	released.Column = domain.TaskColumnReleased
	released.ColumnEnteredAt = &entered
	s.store.tasks = []domain.BoardTask{released}
	first := s.get(s.app, "")

	s.get(s.app, first.etag)

	s.Equal(2, s.store.listed, "a list holding a task past its released window is never served from the ETag alone")
}

func (s *BoardETagSuite) TestWeakAndListedETagsMatch() {
	first := s.get(s.app, "")

	s.Equal(304, s.get(s.app, `"other", W/`+first.etag).status)
	s.Equal(304, s.get(s.app, "*").status)
	s.Equal(200, s.get(s.app, `"other"`).status)
}

func (s *BoardETagSuite) TestStoreWithoutAVersionAlwaysReadsButStillAnswers304() {
	store := &unversionedBoardStore{tasks: []domain.BoardTask{s.task}}
	app := s.appFor(store)
	first := s.get(app, "")

	again := s.get(app, first.etag)

	s.Equal(304, again.status)
	s.Equal(2, store.listed)
}

// Another host writing the same database never moves this process's version,
// so a 304 rests on the version alone for boardListMaxAge at most.
func (s *BoardETagSuite) TestAWriteThisProcessCannotSeeShowsWithinTheMaxAge() {
	first := s.get(s.app, "")
	moved := s.task
	moved.Column = domain.TaskColumnInProgress
	s.store.tasks = []domain.BoardTask{moved}

	s.now = s.now.Add(boardListMaxAge - time.Second)
	s.Equal(304, s.get(s.app, first.etag).status, "within the max age the version alone answers")
	s.now = s.now.Add(time.Second)
	after := s.get(s.app, first.etag)

	s.Equal(200, after.status)
	s.Contains(after.body, string(domain.TaskColumnInProgress))
	s.Equal(304, s.get(s.app, after.etag).status)
	s.Equal(2, s.store.listed)
}

func (s *BoardETagSuite) TestAnUnchangedBoardPastTheMaxAgeIsReadAgainButStill304() {
	first := s.get(s.app, "")
	s.now = s.now.Add(boardListMaxAge)

	again := s.get(s.app, first.etag)

	s.Equal(304, again.status)
	s.Equal(2, s.store.listed)
	s.Equal(304, s.get(s.app, first.etag).status)
	s.Equal(2, s.store.listed, "the re-read refreshed the entry")
}
