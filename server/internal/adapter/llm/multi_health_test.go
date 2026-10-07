package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type HealthCacheSuite struct {
	suite.Suite
	provider *mocks.LLMClient
	client   *MultiProviderClient
	clock    *testClock
	view     domain.LLMProviderView
}

func TestHealthCacheSuite(t *testing.T) {
	suite.Run(t, new(HealthCacheSuite))
}

func (s *HealthCacheSuite) SetupTest() {
	s.provider = &mocks.LLMClient{}
	s.client = NewMultiProviderClient(nil, StaticResolver(staticSet(domain.LLMProviderOpenAI,
		map[domain.LLMProviderType]port.LLMClient{domain.LLMProviderOpenAI: s.provider})))
	s.clock = &testClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	s.client.health.now = s.clock.read
	s.view = domain.LLMProviderView{
		Definition: domain.LLMProviderDefinition{Type: domain.LLMProviderOpenAI, Label: "OpenAI"},
		Config:     domain.LLMProviderConfig{ProviderType: domain.LLMProviderOpenAI, Configured: true, UpdatedAt: s.clock.read()},
		Active:     true,
	}
}

func (s *HealthCacheSuite) TearDownTest() {
	s.provider.AssertExpectations(s.T())
}

func (s *HealthCacheSuite) check() ProviderHealth {
	out := s.client.HealthCheck(context.Background(), []domain.LLMProviderView{s.view})
	s.Require().Len(out, 1)
	return out[0]
}

func (s *HealthCacheSuite) TestRepeatedChecksWithinTTLProbeOnce() {
	s.provider.On("Models", mock.Anything).Return([]string{"gpt"}, nil).Once()

	s.Equal("ok", s.check().Status)
	s.clock.advance(healthProbeTTL - time.Second)
	got := s.check()

	s.Equal("ok", got.Status)
	s.True(got.Active)
	s.Equal("OpenAI", got.Label)
}

func (s *HealthCacheSuite) TestExpiredProbeIsRepeated() {
	s.provider.On("Models", mock.Anything).Return([]string{"gpt"}, nil).Twice()

	s.check()
	s.clock.advance(healthProbeTTL + time.Second)
	s.check()
}

func (s *HealthCacheSuite) TestFailureIsRetriedSoonerThanSuccess() {
	s.provider.On("Models", mock.Anything).Return([]string(nil), errors.New("connection refused")).Once()
	s.provider.On("Models", mock.Anything).Return([]string{"gpt"}, nil).Once()

	first := s.check()
	s.Equal("error", first.Status)
	s.Equal("connection refused", first.Message)
	s.clock.advance(healthProbeErrorTTL + time.Second)

	s.Equal("ok", s.check().Status)
}

func (s *HealthCacheSuite) TestProbeResultIsReusedForMinutes() {
	at := s.clock.read()
	tests := []struct {
		name   string
		status string
		age    time.Duration
		fresh  bool
	}{
		{"healthy, just under ten minutes", "ok", 10*time.Minute - time.Second, true},
		{"healthy, ten minutes", "ok", 10 * time.Minute, false},
		{"failing, just under two minutes", "error", 2*time.Minute - time.Second, true},
		{"failing, two minutes", "error", 2 * time.Minute, false},
		{"failing, re-probed long before a healthy one would be", "error", 5 * time.Minute, false},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			probe := healthProbe{status: tt.status, at: at}
			s.Equal(tt.fresh, probe.fresh(at.Add(tt.age)))
		})
	}
}

func (s *HealthCacheSuite) TestUnreachableProviderIsNotReprobedOnEveryPoll() {
	s.provider.On("Models", mock.Anything).Return([]string(nil), errors.New("connection refused")).Once()

	for range 8 {
		s.Equal("error", s.check().Status)
		s.clock.advance(10 * time.Second)
	}
}

func (s *HealthCacheSuite) TestInvalidateForcesAFreshProbe() {
	s.provider.On("Models", mock.Anything).Return([]string{"gpt"}, nil).Twice()

	s.check()
	s.client.InvalidateHealth()
	s.check()
}

func (s *HealthCacheSuite) TestConfigurationChangeMissesTheCache() {
	s.provider.On("Models", mock.Anything).Return([]string{"gpt"}, nil).Twice()

	s.check()
	s.view.Config.UpdatedAt = s.clock.read().Add(time.Second)
	s.check()
}

func (s *HealthCacheSuite) TestUnconfiguredProviderIsNotProbed() {
	s.view.Config.Configured = false

	s.Equal("disconnected", s.check().Status)
}

func (s *HealthCacheSuite) TestConcurrentChecksShareOneProbe() {
	release := make(chan struct{})
	s.provider.On("Models", mock.Anything).Run(func(mock.Arguments) { <-release }).Return([]string{"gpt"}, nil).Once()

	var wg sync.WaitGroup
	results := make([]ProviderHealth, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.check()
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for _, r := range results {
		s.Equal("ok", r.Status)
	}
}

func (s *HealthCacheSuite) TestCallerStopsWaitingWhenItsContextEnds() {
	release := make(chan struct{})
	defer close(release)
	s.provider.On("Models", mock.Anything).Run(func(mock.Arguments) { <-release }).Return([]string{"gpt"}, nil).Once()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	out := s.client.HealthCheck(ctx, []domain.LLMProviderView{s.view})

	s.Equal("error", out[0].Status)
	s.Contains(out[0].Message, "deadline")
}
