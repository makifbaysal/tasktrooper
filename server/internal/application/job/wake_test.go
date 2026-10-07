package job

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Jobs are stored undecodable so run fails them before reaching the agent
// loop, which these tests leave nil.
var undecodableRequest = json.RawMessage(`{not json`)

type queueJobStore struct {
	mu      sync.Mutex
	pending []domain.Job
	claims  int
	failed  []uuid.UUID
}

func (q *queueJobStore) Create(_ context.Context, _ []byte, callbackURL string) (domain.Job, error) {
	return q.enqueue(callbackURL), nil
}

func (q *queueJobStore) enqueue(callbackURL string) domain.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := domain.Job{ID: uuid.New(), Status: domain.JobStatusPending, Request: undecodableRequest, CallbackURL: callbackURL}
	q.pending = append(q.pending, job)
	return job
}

func (q *queueJobStore) Get(context.Context, uuid.UUID) (domain.Job, error) { return domain.Job{}, nil }

func (q *queueJobStore) List(context.Context, string, int) ([]domain.Job, error) { return nil, nil }

func (q *queueJobStore) UpdateStatus(_ context.Context, id uuid.UUID, status domain.JobStatus, _ []byte, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if status == domain.JobStatusFailed {
		q.failed = append(q.failed, id)
	}
	return nil
}

func (q *queueJobStore) Delete(context.Context, uuid.UUID) error { return nil }

func (q *queueJobStore) ClaimPending(context.Context) (*domain.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	if len(q.pending) == 0 {
		return nil, nil
	}
	job := q.pending[0]
	q.pending = q.pending[1:]
	job.Status = domain.JobStatusRunning
	return &job, nil
}

func (q *queueJobStore) claimCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claims
}

func (q *queueJobStore) pendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

func (q *queueJobStore) failedIDs() []uuid.UUID {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]uuid.UUID(nil), q.failed...)
}

const testWorkers = 3

type WorkerWakeSuite struct {
	suite.Suite
	store *queueJobStore
	svc   *Service
}

func TestWorkerWakeSuite(t *testing.T) {
	suite.Run(t, new(WorkerWakeSuite))
}

func (s *WorkerWakeSuite) SetupTest() {
	s.store = &queueJobStore{}
	s.svc = NewService(s.store, nil, nil, testWorkers, time.Minute, domain.ToolPolicy{})
	s.svc.fallbackPoll = time.Hour
}

func (s *WorkerWakeSuite) TearDownTest() {
	s.svc.Stop()
}

func (s *WorkerWakeSuite) startIdle() {
	s.svc.Start(context.Background())
	s.Require().Eventually(func() bool { return s.store.claimCount() >= testWorkers }, time.Second, 5*time.Millisecond)
}

func (s *WorkerWakeSuite) TestIdleWorkersStopClaimingAfterTheStartupDrain() {
	s.startIdle()

	time.Sleep(200 * time.Millisecond)

	s.Equal(testWorkers, s.store.claimCount())
}

func (s *WorkerWakeSuite) TestStartDrainsJobsLeftPendingBeforeARestart() {
	leftover := []domain.Job{s.store.enqueue(""), s.store.enqueue("")}

	s.svc.Start(context.Background())

	s.Require().Eventually(func() bool { return len(s.store.failedIDs()) == len(leftover) }, time.Second, 5*time.Millisecond)
	s.ElementsMatch([]uuid.UUID{leftover[0].ID, leftover[1].ID}, s.store.failedIDs())
}

func (s *WorkerWakeSuite) TestCreateWakesAnIdleWorkerWithoutWaitingForThePoll() {
	s.startIdle()

	job, err := s.svc.Create(context.Background(), domain.JobRequest{}, domain.ToolPolicy{})
	s.Require().NoError(err)

	s.Require().Eventually(func() bool { return s.store.pendingCount() == 0 && len(s.store.failedIDs()) == 1 }, time.Second, 5*time.Millisecond)
	s.Equal([]uuid.UUID{job.ID}, s.store.failedIDs())
}

func (s *WorkerWakeSuite) TestJobsCreatedBackToBackAreAllDrained() {
	s.startIdle()

	const jobs = 10
	created := make([]uuid.UUID, 0, jobs)
	for range jobs {
		job, err := s.svc.Create(context.Background(), domain.JobRequest{}, domain.ToolPolicy{})
		s.Require().NoError(err)
		created = append(created, job.ID)
	}

	s.Require().Eventually(func() bool { return len(s.store.failedIDs()) == jobs }, time.Second, 5*time.Millisecond)
	s.ElementsMatch(created, s.store.failedIDs())
	s.Zero(s.store.pendingCount())
}

func (s *WorkerWakeSuite) TestFallbackPollDefaultsToFiveMinutes() {
	s.Equal(5*time.Minute, NewService(s.store, nil, nil, testWorkers, time.Minute, domain.ToolPolicy{}).fallbackPoll)
}

func (s *WorkerWakeSuite) TestIdlePoolLooksOncePerFallbackTickNotOncePerWorker() {
	const tick = 40 * time.Millisecond
	s.svc.fallbackPoll = tick
	s.startIdle()
	before := s.store.claimCount()
	started := time.Now()

	s.Require().Eventually(func() bool { return s.store.claimCount() > before }, time.Second, 5*time.Millisecond)
	time.Sleep(10 * tick)

	ticks := int(time.Since(started)/tick) + 2
	s.LessOrEqual(s.store.claimCount()-before, ticks,
		"one claim per tick at most; a timer per worker would claim about %d times", testWorkers*ticks)
}

func (s *WorkerWakeSuite) TestFallbackPollPicksUpAJobNobodySignalled() {
	s.svc.fallbackPoll = 20 * time.Millisecond
	s.startIdle()

	job := s.store.enqueue("")

	s.Require().Eventually(func() bool { return len(s.store.failedIDs()) == 1 }, time.Second, 5*time.Millisecond)
	s.Equal([]uuid.UUID{job.ID}, s.store.failedIDs())
}
