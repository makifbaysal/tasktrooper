package opsquery_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/makifbaysal/tasktrooper/server/internal/port/mocks"
)

type OpsQuerySuite struct {
	suite.Suite
	ctx     context.Context
	now     time.Time
	envs    *mocks.EnvironmentStore
	comps   *mocks.ComponentStore
	runtime *mocks.RuntimeReader
	svc     *opsquery.Service
}

func TestOpsQuerySuite(t *testing.T) { suite.Run(t, new(OpsQuerySuite)) }

func (s *OpsQuerySuite) SetupTest() {
	s.ctx = context.Background()
	s.now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	s.reset()
}

// reset builds fresh mocks bound to the current (sub)test, so a table case's
// expectations are asserted against its own run.
func (s *OpsQuerySuite) reset(opts ...opsquery.Option) {
	s.envs = mocks.NewEnvironmentStore(s.T())
	s.comps = mocks.NewComponentStore(s.T())
	s.runtime = mocks.NewRuntimeReader(s.T())
	// A short timeout keeps a failing expectation (which never returns) from
	// stalling the run for the production 20s.
	base := []opsquery.Option{opsquery.WithClock(func() time.Time { return s.now }), opsquery.WithEnvironmentTimeout(2 * time.Second)}
	s.svc = opsquery.NewService(opsquery.Deps{Environments: s.envs, Components: s.comps, Runtime: s.runtime}, append(base, opts...)...)
}

func (s *OpsQuerySuite) env(kind domain.DeployEnvironment, mutate ...func(*domain.ComponentEnvironment)) domain.ComponentEnvironment {
	acct := uuid.New()
	e := domain.ComponentEnvironment{
		ID: uuid.New(), RepositoryID: uuid.New(), ComponentID: uuid.New(), Environment: kind,
		Provider: domain.CloudGCP, AccountID: &acct,
		Resource: &domain.CloudResourceRef{Kind: domain.CloudResourceCloudRunService, ID: uuid.NewString()},
		Status:   domain.LinkConfirmed,
	}
	for _, m := range mutate {
		m(&e)
	}
	return e
}

func (s *OpsQuerySuite) seed(envs []domain.ComponentEnvironment, components ...domain.Component) {
	s.envs.On("ListAllEnvironments", mock.Anything).Return(envs, nil).Once()
	s.comps.On("ListAllComponents", mock.Anything).Return(components, nil).Once()
}

func (s *OpsQuerySuite) supportsErrors(envs ...domain.ComponentEnvironment) {
	for _, e := range envs {
		s.runtime.On("ErrorsSupported", envWithID(e.ID)).Return(true).Once()
	}
}

func envWithID(id uuid.UUID) interface{} {
	return mock.MatchedBy(func(e domain.ComponentEnvironment) bool { return e.ID == id })
}

func (s *OpsQuerySuite) errorsFor(e domain.ComponentEnvironment, groups ...domain.RuntimeErrorGroup) {
	s.runtime.On("Errors", mock.Anything, e.ID, mock.Anything).Return(groups, nil).Once()
}

func groupFor(e domain.ComponentEnvironment) domain.RuntimeErrorGroup {
	return domain.RuntimeErrorGroup{Fingerprint: e.ID.String(), Message: "boom", Count: 1}
}

func groupEnvironments(out domain.OpsErrors) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(out.Groups))
	for _, g := range out.Groups {
		ids = append(ids, g.EnvironmentID)
	}
	return ids
}

func (s *OpsQuerySuite) TestErrorsReadsOnlyConfirmedBoundEnvironmentsOfTheAskedKind() {
	read := s.env(domain.EnvironmentProduction)
	suggested := s.env(domain.EnvironmentProduction, func(e *domain.ComponentEnvironment) { e.Status = domain.LinkSuggested })
	dismissed := s.env(domain.EnvironmentProduction, func(e *domain.ComponentEnvironment) { e.Status = domain.LinkDismissed })
	custom := s.env(domain.EnvironmentProduction, func(e *domain.ComponentEnvironment) {
		e.AccountID, e.Resource, e.Provider, e.URL = nil, nil, "", "https://app.example.com"
	})
	staging := s.env(domain.EnvironmentStaging)
	s.seed([]domain.ComponentEnvironment{read, suggested, dismissed, custom, staging})
	s.supportsErrors(read)
	s.errorsFor(read, groupFor(read))

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Equal([]uuid.UUID{read.ID}, groupEnvironments(out))
	s.Empty(out.Unavailable)
	s.False(out.Truncated)

	g := out.Groups[0]
	s.Equal(read.ComponentID, g.ComponentID)
	s.Equal(read.RepositoryID, g.RepositoryID)
	s.Equal(domain.CloudGCP, g.Provider)
	s.Equal("boom", g.Message)
}

func (s *OpsQuerySuite) TestComponentFilterAndDismissedComponents() {
	tests := []struct {
		name   string
		filter func(a, b, gone domain.ComponentEnvironment) []uuid.UUID
		want   func(a, b, gone domain.ComponentEnvironment) []uuid.UUID
	}{
		{
			name:   "no filter reads every live component",
			filter: func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return nil },
			want:   func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return []uuid.UUID{a.ID, b.ID} },
		},
		{
			name:   "a filter keeps only the named components",
			filter: func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return []uuid.UUID{b.ComponentID} },
			want:   func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return []uuid.UUID{b.ID} },
		},
		{
			name: "a dismissed component stays out even when named",
			filter: func(a, b, gone domain.ComponentEnvironment) []uuid.UUID {
				return []uuid.UUID{a.ComponentID, gone.ComponentID}
			},
			want: func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return []uuid.UUID{a.ID} },
		},
		{
			name:   "an unknown component matches nothing",
			filter: func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return []uuid.UUID{uuid.New()} },
			want:   func(a, b, gone domain.ComponentEnvironment) []uuid.UUID { return []uuid.UUID{} },
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.reset()
			a, b, gone := s.env(domain.EnvironmentProduction), s.env(domain.EnvironmentProduction), s.env(domain.EnvironmentProduction)
			s.seed([]domain.ComponentEnvironment{a, b, gone},
				domain.Component{ID: a.ComponentID, Status: domain.ComponentStatusActive},
				domain.Component{ID: b.ComponentID, Status: domain.ComponentStatusActive},
				domain.Component{ID: gone.ComponentID, Status: domain.ComponentStatusDismissed},
			)
			want := tt.want(a, b, gone)
			read := map[uuid.UUID]bool{}
			for _, id := range want {
				read[id] = true
			}
			for _, e := range []domain.ComponentEnvironment{a, b, gone} {
				if read[e.ID] {
					s.supportsErrors(e)
					s.errorsFor(e, groupFor(e))
				}
			}

			out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction, ComponentIDs: tt.filter(a, b, gone)})
			s.Require().NoError(err)
			s.ElementsMatch(want, groupEnvironments(out))
		})
	}
}

func (s *OpsQuerySuite) TestErrorsSkipsAnEnvironmentWithoutAnErrorSurfaceSilently() {
	vercel := s.env(domain.EnvironmentProduction, func(e *domain.ComponentEnvironment) { e.Provider = domain.CloudVercel })
	gcp := s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{vercel, gcp})
	s.runtime.On("ErrorsSupported", envWithID(vercel.ID)).Return(false).Once()
	s.supportsErrors(gcp)
	s.errorsFor(gcp, groupFor(gcp))

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Equal([]uuid.UUID{gcp.ID}, groupEnvironments(out))
	s.Empty(out.Unavailable)
}

func (s *OpsQuerySuite) TestAFailingEnvironmentIsReportedWithACodeAndTheRestStands() {
	healthy := s.env(domain.EnvironmentProduction)
	auth := s.env(domain.EnvironmentProduction)
	unbound := s.env(domain.EnvironmentProduction)
	outage := s.env(domain.EnvironmentProduction)
	slow := s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{healthy, auth, unbound, outage, slow})
	s.supportsErrors(healthy, auth, unbound, outage, slow)
	s.errorsFor(healthy, groupFor(healthy))
	s.runtime.On("Errors", mock.Anything, auth.ID, mock.Anything).Return(nil, fmt.Errorf("gcp: %w", port.ErrCloudAuth)).Once()
	s.runtime.On("Errors", mock.Anything, unbound.ID, mock.Anything).Return(nil, cloud.ErrNotConnected).Once()
	s.runtime.On("Errors", mock.Anything, outage.ID, mock.Anything).Return(nil, errors.New("upstream 503")).Once()
	s.runtime.On("Errors", mock.Anything, slow.ID, mock.Anything).Return(nil, fmt.Errorf("read: %w", context.DeadlineExceeded)).Once()

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Equal([]uuid.UUID{healthy.ID}, groupEnvironments(out))

	codes := map[uuid.UUID]domain.OpsUnavailableCode{}
	for _, u := range out.Unavailable {
		codes[u.EnvironmentID] = u.Code
		s.NotEmpty(u.Message)
		s.NotEqual(uuid.Nil, u.ComponentID)
	}
	s.Equal(map[uuid.UUID]domain.OpsUnavailableCode{
		auth.ID:    domain.OpsCloudAuth,
		unbound.ID: domain.OpsNotConnected,
		outage.ID:  domain.OpsProviderError,
		slow.ID:    domain.OpsTimeout,
	}, codes)
}

func (s *OpsQuerySuite) TestASlowOrPanickingEnvironmentIsCutOffWithoutHoldingUpTheOthers() {
	s.reset(opsquery.WithEnvironmentTimeout(50 * time.Millisecond))
	healthy := s.env(domain.EnvironmentProduction)
	honoursCtx := s.env(domain.EnvironmentProduction)
	ignoresCtx := s.env(domain.EnvironmentProduction)
	panics := s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{healthy, honoursCtx, ignoresCtx, panics})
	s.supportsErrors(healthy, honoursCtx, ignoresCtx, panics)
	s.errorsFor(healthy, groupFor(healthy))

	release := make(chan struct{})
	s.T().Cleanup(func() { close(release) })
	s.runtime.On("Errors", mock.Anything, honoursCtx.ID, mock.Anything).Return(
		func(ctx context.Context, _ uuid.UUID, _ time.Time) ([]domain.RuntimeErrorGroup, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, nil).Once()
	s.runtime.On("Errors", mock.Anything, ignoresCtx.ID, mock.Anything).Return(
		func(context.Context, uuid.UUID, time.Time) ([]domain.RuntimeErrorGroup, error) {
			<-release
			return nil, nil
		}, nil).Once()
	s.runtime.On("Errors", mock.Anything, panics.ID, mock.Anything).Run(func(mock.Arguments) { panic("nil map") }).Return(nil, nil).Once()

	started := time.Now()
	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Less(time.Since(started), time.Second)
	s.Equal([]uuid.UUID{healthy.ID}, groupEnvironments(out))

	codes := map[uuid.UUID]domain.OpsUnavailableCode{}
	for _, u := range out.Unavailable {
		codes[u.EnvironmentID] = u.Code
	}
	s.Equal(map[uuid.UUID]domain.OpsUnavailableCode{
		honoursCtx.ID: domain.OpsTimeout,
		ignoresCtx.ID: domain.OpsTimeout,
		panics.ID:     domain.OpsProviderError,
	}, codes)
}

func (s *OpsQuerySuite) TestNoMoreThanFourEnvironmentsAreReadAtOnce() {
	envs := make([]domain.ComponentEnvironment, 12)
	for i := range envs {
		envs[i] = s.env(domain.EnvironmentProduction)
	}
	s.seed(envs)
	s.supportsErrors(envs...)
	var inFlight, peak atomic.Int32
	for _, e := range envs {
		s.runtime.On("Errors", mock.Anything, e.ID, mock.Anything).Run(func(mock.Arguments) {
			n := inFlight.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inFlight.Add(-1)
		}).Return([]domain.RuntimeErrorGroup{}, nil).Once()
	}

	_, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.LessOrEqual(peak.Load(), int32(4))
	s.GreaterOrEqual(peak.Load(), int32(2))
}

func (s *OpsQuerySuite) TestErrorsReadsAtMostFiftyEnvironments() {
	envs := make([]domain.ComponentEnvironment, opsquery.MaxErrorEnvironments+5)
	for i := range envs {
		envs[i] = s.env(domain.EnvironmentProduction)
	}
	s.seed(envs)
	s.supportsErrors(envs...)
	for _, e := range envs[:opsquery.MaxErrorEnvironments] {
		s.errorsFor(e)
	}

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.True(out.Truncated)
	s.Empty(out.Unavailable)
}

func (s *OpsQuerySuite) TestErrorsAreSortedNewFirstThenMostRecentlySeen() {
	a, b := s.env(domain.EnvironmentProduction), s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{a, b})
	s.supportsErrors(a, b)
	t0 := s.now.Add(-time.Hour)
	s.errorsFor(a,
		domain.RuntimeErrorGroup{Fingerprint: "old-recent", LastSeen: t0.Add(50 * time.Minute)},
		domain.RuntimeErrorGroup{Fingerprint: "new-older", LastSeen: t0.Add(10 * time.Minute), New: true},
	)
	s.errorsFor(b,
		domain.RuntimeErrorGroup{Fingerprint: "new-recent", LastSeen: t0.Add(40 * time.Minute), New: true},
		domain.RuntimeErrorGroup{Fingerprint: "old-older", LastSeen: t0.Add(5 * time.Minute)},
	)

	out, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	got := make([]string, 0, len(out.Groups))
	for _, g := range out.Groups {
		got = append(got, g.Fingerprint)
	}
	s.Equal([]string{"new-recent", "new-older", "old-recent", "old-older"}, got)
}

func (s *OpsQuerySuite) TestErrorsSinceDefaultsAndIsClampedToThirtyDays() {
	tests := []struct {
		name  string
		since time.Time
		want  time.Time
	}{
		{name: "zero is the last 24 hours", want: s.now.Add(-24 * time.Hour)},
		{name: "within thirty days is kept", since: s.now.Add(-48 * time.Hour), want: s.now.Add(-48 * time.Hour)},
		{name: "beyond thirty days is clamped", since: s.now.Add(-90 * 24 * time.Hour), want: s.now.Add(-30 * 24 * time.Hour)},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.reset()
			e := s.env(domain.EnvironmentProduction)
			s.seed([]domain.ComponentEnvironment{e})
			s.supportsErrors(e)
			s.runtime.On("Errors", mock.Anything, e.ID, tt.want).Return([]domain.RuntimeErrorGroup{}, nil).Once()

			_, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction, Since: tt.since})
			s.Require().NoError(err)
		})
	}
}

func (s *OpsQuerySuite) TestAnUnknownEnvironmentKindIsInvalidInput() {
	_, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: "moonbase"})
	s.ErrorIs(err, opsquery.ErrInvalidInput)
	_, err = s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: ""})
	s.ErrorIs(err, opsquery.ErrInvalidInput)
}

func (s *OpsQuerySuite) entry(minutesAgo int, msg string) domain.RuntimeLogEntry {
	return domain.RuntimeLogEntry{Timestamp: s.now.Add(-time.Duration(minutesAgo) * time.Minute), Severity: domain.LogInfo, Message: msg}
}

func (s *OpsQuerySuite) TestLogsMergesNewestFirstAndCutsToTheLimit() {
	a, b := s.env(domain.EnvironmentProduction), s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{a, b})
	since, until := s.now.Add(-2*time.Hour), s.now
	want := domain.RuntimeLogQuery{Since: since, Until: until, MinSeverity: domain.LogWarning, Text: "timeout", Limit: 4}
	s.runtime.On("Logs", mock.Anything, a.ID, want).Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{
		s.entry(1, "a1"), s.entry(3, "a3"), s.entry(5, "a5"),
	}}, nil).Once()
	s.runtime.On("Logs", mock.Anything, b.ID, want).Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{
		s.entry(2, "b2"), s.entry(4, "b4"),
	}}, nil).Once()

	out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{
		Environment: domain.EnvironmentProduction, Since: since, Until: until,
		MinSeverity: domain.LogWarning, Text: "timeout", Limit: 4,
	})
	s.Require().NoError(err)
	got := make([]string, 0, len(out.Entries))
	for _, e := range out.Entries {
		got = append(got, e.Message)
	}
	s.Equal([]string{"a1", "b2", "a3", "b4"}, got)
	s.True(out.Truncated)
	s.Equal(a.ID, out.Entries[0].EnvironmentID)
	s.Equal(a.ComponentID, out.Entries[0].ComponentID)
	s.Equal(b.ID, out.Entries[1].EnvironmentID)
}

func (s *OpsQuerySuite) TestLogsTruncatedWhenAnEnvironmentHadMore() {
	tests := []struct {
		name string
		page domain.RuntimeLogPage
		want bool
	}{
		{name: "a short page is complete", page: domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "x")}}, want: false},
		{name: "a cursor means more", page: domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "x")}, NextCursor: "c2"}, want: true},
		{name: "a provider cap means more", page: domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "x")}, Truncated: true}, want: true},
		{name: "a full page may hide more", page: domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "x"), s.entry(2, "y")}}, want: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.reset()
			e := s.env(domain.EnvironmentProduction)
			s.seed([]domain.ComponentEnvironment{e})
			s.runtime.On("Logs", mock.Anything, e.ID, mock.Anything).Return(tt.page, nil).Once()

			out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction, Limit: 2})
			s.Require().NoError(err)
			s.Equal(tt.want, out.Truncated)
		})
	}
}

func (s *OpsQuerySuite) TestLogsRefusesMoreThanTenEnvironmentsWithoutAComponentFilter() {
	envs := make([]domain.ComponentEnvironment, opsquery.MaxLogEnvironments+1)
	for i := range envs {
		envs[i] = s.env(domain.EnvironmentProduction)
	}
	s.seed(envs)

	_, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction})
	var tooMany *opsquery.TooManyEnvironmentsError
	s.Require().ErrorAs(err, &tooMany)
	s.Equal(opsquery.MaxLogEnvironments+1, tooMany.Matched)
	s.Equal(opsquery.MaxLogEnvironments, tooMany.Max)
}

func (s *OpsQuerySuite) TestLogsWithAComponentFilterReadsTheFirstTenAndSaysSo() {
	envs := make([]domain.ComponentEnvironment, opsquery.MaxLogEnvironments+2)
	ids := make([]uuid.UUID, len(envs))
	for i := range envs {
		envs[i] = s.env(domain.EnvironmentProduction)
		ids[i] = envs[i].ComponentID
	}
	s.seed(envs)
	for _, e := range envs[:opsquery.MaxLogEnvironments] {
		s.runtime.On("Logs", mock.Anything, e.ID, mock.Anything).Return(domain.RuntimeLogPage{}, nil).Once()
	}

	out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction, ComponentIDs: ids})
	s.Require().NoError(err)
	s.True(out.Truncated)
	s.NotNil(out.Entries)
	s.NotNil(out.Unavailable)
}

func (s *OpsQuerySuite) TestLogsWindowAndLimitDefaults() {
	tests := []struct {
		name string
		q    opsquery.LogsQuery
		want domain.RuntimeLogQuery
	}{
		{
			name: "zero window is the last hour, zero limit is 200",
			q:    opsquery.LogsQuery{Environment: domain.EnvironmentProduction},
			want: domain.RuntimeLogQuery{Since: s.now.Add(-time.Hour), Until: s.now, Limit: opsquery.DefaultLogLimit},
		},
		{
			name: "load older: an hour before until, limit capped at 500",
			q:    opsquery.LogsQuery{Environment: domain.EnvironmentProduction, Until: s.now.Add(-3 * time.Hour), Limit: 5000},
			want: domain.RuntimeLogQuery{Since: s.now.Add(-4 * time.Hour), Until: s.now.Add(-3 * time.Hour), Limit: opsquery.MaxLogLimit},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.reset()
			e := s.env(domain.EnvironmentProduction)
			s.seed([]domain.ComponentEnvironment{e})
			s.runtime.On("Logs", mock.Anything, e.ID, tt.want).Return(domain.RuntimeLogPage{}, nil).Once()

			_, err := s.svc.Logs(s.ctx, tt.q)
			s.Require().NoError(err)
		})
	}
}

func (s *OpsQuerySuite) TestLogsRejectsAWindowThatEndsBeforeItStarts() {
	_, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction, Since: s.now, Until: s.now.Add(-time.Minute)})
	s.ErrorIs(err, opsquery.ErrInvalidInput)
}

func (s *OpsQuerySuite) TestLogsReportsAFailingEnvironmentAndMergesTheRest() {
	ok, broken := s.env(domain.EnvironmentProduction), s.env(domain.EnvironmentProduction)
	s.seed([]domain.ComponentEnvironment{ok, broken})
	s.runtime.On("Logs", mock.Anything, ok.ID, mock.Anything).Return(domain.RuntimeLogPage{Entries: []domain.RuntimeLogEntry{s.entry(1, "fine")}}, nil).Once()
	s.runtime.On("Logs", mock.Anything, broken.ID, mock.Anything).Return(domain.RuntimeLogPage{}, port.ErrCloudAuth).Once()

	out, err := s.svc.Logs(s.ctx, opsquery.LogsQuery{Environment: domain.EnvironmentProduction})
	s.Require().NoError(err)
	s.Require().Len(out.Entries, 1)
	s.Equal("fine", out.Entries[0].Message)
	s.Require().Len(out.Unavailable, 1)
	s.Equal(broken.ID, out.Unavailable[0].EnvironmentID)
	s.Equal(broken.ComponentID, out.Unavailable[0].ComponentID)
	s.Equal(domain.OpsCloudAuth, out.Unavailable[0].Code)
	s.Contains(out.Unavailable[0].Message, "reconnecting")
}

func (s *OpsQuerySuite) TestAStoreFailureFailsTheQuery() {
	s.envs.On("ListAllEnvironments", mock.Anything).Return(nil, errors.New("connection reset")).Once()
	_, err := s.svc.Errors(s.ctx, opsquery.ErrorsQuery{Environment: domain.EnvironmentProduction})
	s.Error(err)
}
