package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

const (
	callbackTimeout = 30 * time.Second

	// Only a backstop: Create wakes a worker itself. The poll catches jobs a
	// failed claim left queued and any row inserted without going through Create.
	defaultFallbackPoll = 5 * time.Minute
)

type Service struct {
	store         port.JobStore
	agentLoop     *agent.Loop
	rag           RAGInjector
	maxWorkers    int
	timeout       time.Duration
	defaultPolicy domain.ToolPolicy

	urlPolicy    urlguard.Policy
	wake         chan struct{}
	fallbackPoll time.Duration
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

type RAGInjector interface {
	InjectContext(ctx context.Context, messages []domain.Message, fileIDs []string) ([]domain.Message, error)
}

func NewService(store port.JobStore, agentLoop *agent.Loop, rag RAGInjector, maxWorkers int, timeout time.Duration, defaultPolicy domain.ToolPolicy) *Service {
	return &Service{
		store:         store,
		agentLoop:     agentLoop,
		rag:           rag,
		maxWorkers:    maxWorkers,
		timeout:       timeout,
		defaultPolicy: defaultPolicy,
		urlPolicy:     urlguard.Default(),
		wake:          make(chan struct{}, max(maxWorkers, 1)),
		fallbackPoll:  defaultFallbackPoll,
	}
}

func (s *Service) SetURLPolicy(p urlguard.Policy) { s.urlPolicy = p }

func (s *Service) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	for i := 0; i < s.maxWorkers; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}
	s.wg.Add(1)
	go s.fallback(ctx)
}

// fallback is one timer for the whole pool rather than one per worker: it only
// has to get SOME idle worker to look, and a timer per worker woke an idle
// process once per worker per period.
func (s *Service) fallback(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.fallbackPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.signalWorkers()
		}
	}
}

func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Service) Create(ctx context.Context, req domain.JobRequest, policy domain.ToolPolicy) (domain.Job, error) {

	req.CallbackURL = strings.TrimSpace(req.CallbackURL)
	if req.CallbackURL != "" {
		if _, err := s.urlPolicy.Precheck(req.CallbackURL); err != nil {
			log.Warn().Err(err).Msg("refused a job callback_url")
			return domain.Job{}, fmt.Errorf("callback_url is not an allowed destination")
		}
	}

	merged := domain.MergeToolPolicy(s.defaultPolicy, policy)
	merged = domain.MergeToolPolicy(merged, req.ToolPolicy)

	payload := struct {
		domain.JobRequest
		ToolPolicy domain.ToolPolicy `json:"tool_policy"`
	}{
		JobRequest: req,
		ToolPolicy: merged,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("marshal job request: %w", err)
	}
	job, err := s.store.Create(ctx, raw, req.CallbackURL)
	if err != nil {
		return domain.Job{}, err
	}
	s.signalWorkers()
	return job, nil
}

// signalWorkers never blocks: a full wake buffer already guarantees a worker
// will drain after this point, and a drain runs until the queue is empty.
func (s *Service) signalWorkers() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Job, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, status string, limit int) ([]domain.Job, error) {
	return s.store.List(ctx, status, limit)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == domain.JobStatusPending || job.Status == domain.JobStatusRunning {
		return s.store.UpdateStatus(ctx, id, domain.JobStatusCancelled, nil, "cancelled by user")
	}
	return s.store.Delete(ctx, id)
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	s.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			s.drain(ctx)
		}
	}
}

func (s *Service) drain(ctx context.Context) {
	for ctx.Err() == nil && s.processOne(ctx) {
	}
}

func (s *Service) processOne(ctx context.Context) (claimed bool) {
	job, err := s.store.ClaimPending(ctx)
	if err != nil {
		log.Error().Err(err).Msg("claim job failed")
		return false
	}
	if job == nil {
		return false
	}
	// This worker is busy until the job ends; hand any remaining queue to an
	// idle one instead of leaving it for the fallback poll.
	s.signalWorkers()
	s.run(ctx, job)
	return true
}

func (s *Service) run(ctx context.Context, job *domain.Job) {
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	var payload struct {
		domain.JobRequest
		ToolPolicy domain.ToolPolicy `json:"tool_policy"`
	}
	if err := json.Unmarshal(job.Request, &payload); err != nil {
		_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusFailed, nil, err.Error())
		return
	}

	messages := payload.Messages
	if s.rag != nil && len(payload.FileIDs) > 0 {
		var err error
		messages, err = s.rag.InjectContext(runCtx, messages, payload.FileIDs)
		if err != nil {
			_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusFailed, nil, err.Error())
			return
		}
	}

	resp, err := s.agentLoop.Run(runCtx, messages, payload.Model, "", payload.ToolPolicy)
	if err != nil {
		_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusFailed, nil, err.Error())
		s.fireCallback(ctx, job.CallbackURL, job.ID, domain.JobStatusFailed, nil, err.Error())
		return
	}

	result := domain.JobResult{Message: resp.Message, Usage: resp.Usage}
	raw, _ := json.Marshal(result)
	_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusCompleted, raw, "")
	s.fireCallback(ctx, job.CallbackURL, job.ID, domain.JobStatusCompleted, raw, "")
}

func (s *Service) fireCallback(ctx context.Context, url string, jobID uuid.UUID, status domain.JobStatus, result []byte, errMsg string) {
	if url == "" {
		return
	}
	payload := map[string]interface{}{
		"job_id": jobID.String(),
		"status": status,
	}
	if len(result) > 0 {
		payload["result"] = json.RawMessage(result)
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	body, _ := json.Marshal(payload)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callbackTimeout)
		defer cancel()

		target, err := s.urlPolicy.Validate(ctx, url)
		if err != nil {
			log.Warn().Err(err).Str("url", urlguard.LogRaw(url)).Msg("job callback destination refused")
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL.String(), bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := s.urlPolicy.ClientFor(target, callbackTimeout)

		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

		resp, err := client.Do(req)
		if err != nil {

			log.Warn().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("job callback failed")
			return
		}
		resp.Body.Close()
	}()
}
