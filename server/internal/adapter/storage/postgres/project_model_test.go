package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/storage/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func ptr[T any](v T) *T { return &v }

type ProjectModelStoreSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	store  *postgres.ProjectModelStore
	repos  *postgres.RepositoryStore
	repoA  uuid.UUID
	repoB  uuid.UUID
}

func TestProjectModelStoreSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(ProjectModelStoreSuite))
}

func (s *ProjectModelStoreSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	pg, err := newTestDatabase(s.ctx)
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	db := postgres.NewDB(pool)
	s.store = postgres.NewProjectModelStore(db)
	s.repos = postgres.NewRepositoryStore(db)
}

func (s *ProjectModelStoreSuite) SetupTest() {
	suffix := uuid.NewString()
	repoA, err := s.repos.Create(s.ctx, "repo-a-"+suffix, "", "/tmp/repo-a-"+suffix, "", "")
	s.Require().NoError(err)
	repoB, err := s.repos.Create(s.ctx, "repo-b-"+suffix, "", "/tmp/repo-b-"+suffix, "", "")
	s.Require().NoError(err)
	s.repoA = repoA.ID
	s.repoB = repoB.ID
}

func (s *ProjectModelStoreSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *ProjectModelStoreSuite) TestSaveComponentRoundTripsFactOverridesAndMobile() {
	name := domain.Fact[string]{
		Detected:   ptr("api"),
		Override:   ptr("Public API"),
		Confidence: domain.ConfidenceHigh,
		Evidence:   []domain.SourceEvidence{{Path: "package.json", Line: 3}},
	}
	mobile := &domain.Fact[domain.MobileFacts]{
		Detected: &domain.MobileFacts{
			Platform:     domain.MobilePlatformIOS,
			Identity:     domain.AppIdentity{BundleID: "com.acme.app"},
			BuildTargets: domain.BuildTargets{XcodeScheme: "Acme"},
		},
		Confidence: domain.ConfidenceExact,
	}

	c := domain.Component{
		RepositoryID: s.repoA,
		Path:         "apps/mobile",
		Name:         name,
		Role:         domain.Detected(domain.ComponentRoleMobile, domain.ConfidenceHigh, domain.SourceEvidence{Path: "go.mod"}),
		Stack: domain.Detected(domain.ComponentStack{
			Languages: []domain.StackItem{{Name: "Swift"}},
		}, domain.ConfidenceHigh),
		Commands: []domain.ComponentCommand{
			{Purpose: domain.CommandBuild, Command: domain.Detected("xcodebuild", domain.ConfidenceHigh)},
		},
		Mobile:        mobile,
		Docs:          domain.RepositoryDocs{CodingStandards: "docs/standards.md"},
		Gates:         domain.ComponentGates{CoverageEnabled: ptr(true), CoverageThreshold: ptr(80.0)},
		Status:        domain.ComponentStatusActive,
		ManuallyAdded: true,
	}

	created, err := s.store.SaveComponent(s.ctx, c)
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, created.ID)
	s.False(created.CreatedAt.IsZero())
	s.Equal("Public API", created.Name.Get(), "the override must win over the detected value")
	s.Require().NotNil(created.Mobile)
	s.Equal("com.acme.app", created.Mobile.Get().Identity.BundleID)

	got, err := s.store.GetComponent(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)

	list, err := s.store.ListComponents(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Require().Len(list, 1)
	s.Equal(created.ID, list[0].ID)
}

func (s *ProjectModelStoreSuite) TestSaveComponentNeedsReviewRoundTrips() {
	created, err := s.store.SaveComponent(s.ctx, domain.Component{
		RepositoryID: s.repoA, Path: "apps/worker", NeedsReview: true,
	})
	s.Require().NoError(err)
	s.True(created.NeedsReview)

	got, err := s.store.GetComponent(s.ctx, created.ID)
	s.Require().NoError(err)
	s.True(got.NeedsReview)

	cleared := got
	cleared.NeedsReview = false
	saved, err := s.store.SaveComponent(s.ctx, cleared)
	s.Require().NoError(err)
	s.False(saved.NeedsReview)
}

func (s *ProjectModelStoreSuite) TestSaveComponentMobileNilStaysNil() {
	created, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)
	s.Nil(created.Mobile)

	got, err := s.store.GetComponent(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Nil(got.Mobile)
}

func (s *ProjectModelStoreSuite) TestSaveComponentUniqueRepositoryPathConflicts() {
	_, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "apps/api"})
	s.Require().NoError(err)

	_, err = s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "apps/api"})
	s.Require().Error(err, "a second component at the same repository/path must not silently create a duplicate")
}

func (s *ProjectModelStoreSuite) TestSaveCheckRoundTripsLocalCommandsAndGate() {
	comp, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)

	check := domain.ComponentCheck{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Source:       domain.CheckSourceCI,
		Workflow:     ".github/workflows/ci.yml",
		WorkflowName: "CI",
		JobKey:       "test",
		JobName:      "Test",
		Purpose:      domain.Detected(domain.CheckTest, domain.ConfidenceHigh),
		Environment:  domain.EnvironmentProduction,
		Triggers:     []string{"push", "pull_request"},
		PathFilters:  []string{"server/**"},
		Steps:        []domain.CheckStep{{Name: "run tests", Run: "go test ./..."}},
		LocalCommands: domain.Detected([]domain.LocalCommand{
			{Dir: ".", Argv: []string{"go", "test", "./..."}},
		}, domain.ConfidenceHigh),
		Gate:         domain.Detected(domain.CheckGateRequired, domain.ConfidenceHigh),
		Dispatchable: true,
		Status:       domain.ModelStatusActive,
	}

	created, err := s.store.SaveCheck(s.ctx, check)
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, created.ID)
	s.Equal(domain.CheckGateRequired, created.Gate.Get())
	s.Equal([]domain.LocalCommand{{Dir: ".", Argv: []string{"go", "test", "./..."}}}, created.LocalCommands.Get())

	got, err := s.store.GetCheck(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)

	list, err := s.store.ListChecks(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Require().Len(list, 1)
	s.Equal(created.ID, list[0].ID)
}

func (s *ProjectModelStoreSuite) TestSaveCheckNeedsReviewRoundTrips() {
	comp, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)

	created, err := s.store.SaveCheck(s.ctx, domain.ComponentCheck{
		RepositoryID: s.repoA,
		ComponentID:  comp.ID,
		Workflow:     "ci.yml",
		JobKey:       "test",
		Gate:         domain.Detected(domain.CheckGateRequired, domain.ConfidenceHigh),
		NeedsReview:  true,
	})
	s.Require().NoError(err)
	s.True(created.NeedsReview)

	got, err := s.store.GetCheck(s.ctx, created.ID)
	s.Require().NoError(err)
	s.True(got.NeedsReview)
}

func (s *ProjectModelStoreSuite) TestSaveLinkTargetHostPortRoundTrips() {
	comp, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)

	created, err := s.store.SaveLink(s.ctx, domain.ComponentLink{
		RepositoryID:    s.repoA,
		FromComponentID: comp.ID,
		Protocol:        domain.LinkHTTP,
		Status:          domain.LinkSuggested,
		Confidence:      domain.ConfidenceMedium,
		Hint:            "billing-svc:8080",
		TargetHost:      "billing-svc",
		TargetPort:      8080,
	})
	s.Require().NoError(err)
	s.Equal("billing-svc", created.TargetHost)
	s.Equal(8080, created.TargetPort)

	got, err := s.store.GetLink(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal("billing-svc", got.TargetHost)
	s.Equal(8080, got.TargetPort)
}

func (s *ProjectModelStoreSuite) TestSaveLinkToResourceRoundTrips() {
	comp, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)

	resource, err := s.store.EnsureResource(s.ctx, domain.SystemResource{
		Kind:        domain.ResourceDatabase,
		Vendor:      "postgres",
		Name:        "primary",
		IdentityKey: "db:primary:" + uuid.NewString(),
		Details:     map[string]string{"engine": "postgres"},
	})
	s.Require().NoError(err)

	link := domain.ComponentLink{
		RepositoryID:    s.repoA,
		FromComponentID: comp.ID,
		ToResourceID:    &resource.ID,
		Protocol:        domain.LinkSQL,
		Detail:          "reads/writes",
		EnvVars:         []string{"DATABASE_URL"},
		Evidence:        []domain.SourceEvidence{{Path: "db.go", Line: 10}},
		Confidence:      domain.ConfidenceHigh,
		Reason:          "found DATABASE_URL",
		Status:          domain.LinkConfirmed,
		Source:          domain.LinkSourceScan,
		AutoConfirmed:   true,
		SignalKey:       "env:DATABASE_URL",
	}

	created, err := s.store.SaveLink(s.ctx, link)
	s.Require().NoError(err)
	s.Require().NotNil(created.ToResourceID)
	s.Equal(resource.ID, *created.ToResourceID)
	s.Nil(created.ToComponentID)
	s.True(created.Resolved())

	got, err := s.store.GetLink(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(created, got)
}

func (s *ProjectModelStoreSuite) TestListIncomingLinksCrossesRepositories() {
	compA, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)
	compB, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoB, Path: "."})
	s.Require().NoError(err)

	_, err = s.store.SaveLink(s.ctx, domain.ComponentLink{
		RepositoryID:    s.repoA,
		FromComponentID: compA.ID,
		ToComponentID:   &compB.ID,
		Protocol:        domain.LinkHTTP,
		Status:          domain.LinkConfirmed,
	})
	s.Require().NoError(err)

	_, err = s.store.SaveLink(s.ctx, domain.ComponentLink{
		RepositoryID:    s.repoB,
		FromComponentID: compB.ID,
		ToComponentID:   &compB.ID,
		Protocol:        domain.LinkHTTP,
	})
	s.Require().NoError(err)

	incoming, err := s.store.ListIncomingLinks(s.ctx, s.repoB)
	s.Require().NoError(err)
	s.Require().Len(incoming, 1, "only the cross-repository edge counts as incoming")
	s.Equal(compA.ID, incoming[0].FromComponentID)

	incomingForA, err := s.store.ListIncomingLinks(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Empty(incomingForA)
}

func (s *ProjectModelStoreSuite) TestListLinksForRepositoriesIncludesIncomingEdges() {
	compA, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)
	compB, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoB, Path: "."})
	s.Require().NoError(err)

	outgoing, err := s.store.SaveLink(s.ctx, domain.ComponentLink{
		RepositoryID:    s.repoA,
		FromComponentID: compA.ID,
		ToComponentID:   &compB.ID,
		Protocol:        domain.LinkHTTP,
	})
	s.Require().NoError(err)

	unresolved, err := s.store.SaveLink(s.ctx, domain.ComponentLink{
		RepositoryID:    s.repoA,
		FromComponentID: compA.ID,
		Protocol:        domain.LinkHTTP,
		Hint:            "some-service",
	})
	s.Require().NoError(err)

	links, err := s.store.ListLinksForRepositories(s.ctx, []uuid.UUID{s.repoB})
	s.Require().NoError(err)
	s.Require().Len(links, 1, "repoB did not create the link, but it is the link's target")
	s.Equal(outgoing.ID, links[0].ID)

	linksForA, err := s.store.ListLinksForRepositories(s.ctx, []uuid.UUID{s.repoA})
	s.Require().NoError(err)
	ids := []uuid.UUID{}
	for _, l := range linksForA {
		ids = append(ids, l.ID)
	}
	s.Contains(ids, outgoing.ID)
	s.Contains(ids, unresolved.ID)
}

func (s *ProjectModelStoreSuite) TestEnsureResourceDedupesByIdentityKey() {
	key := "stripe:" + uuid.NewString()
	first, err := s.store.EnsureResource(s.ctx, domain.SystemResource{
		Kind:        domain.ResourcePayments,
		Vendor:      "stripe",
		Name:        "Stripe",
		IdentityKey: key,
		Details:     map[string]string{"env": "prod"},
	})
	s.Require().NoError(err)

	second, err := s.store.EnsureResource(s.ctx, domain.SystemResource{
		Kind:        domain.ResourcePayments,
		Vendor:      "stripe",
		Name:        "Stripe (renamed)",
		IdentityKey: key,
		Details:     map[string]string{"region": "us"},
	})
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID, "must dedupe by identity key, not mint a new row")
	s.Equal("Stripe (renamed)", second.Name)
	s.Equal(map[string]string{"env": "prod", "region": "us"}, second.Details, "details merge; new keys win, old keys survive")

	list, err := s.store.ListResources(s.ctx, []uuid.UUID{first.ID})
	s.Require().NoError(err)
	s.Require().Len(list, 1)
	s.Equal(second.Details, list[0].Details)

	empty, err := s.store.ListResources(s.ctx, nil)
	s.Require().NoError(err)
	s.Nil(empty)
}

func (s *ProjectModelStoreSuite) TestSaveNoteUpsertsByRepositoryNilComponentTopic() {
	first, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA,
		Topic:        domain.NotePurpose,
		BodyMD:       "first",
		Evidence:     []domain.SourceEvidence{{Path: "README.md"}},
	})
	s.Require().NoError(err)

	second, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA,
		Topic:        domain.NotePurpose,
		BodyMD:       "second",
	})
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID, "same repository/topic with no component must update, not duplicate")
	s.Equal("second", second.BodyMD)

	list, err := s.store.ListNotes(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Require().Len(list, 1)
}

func (s *ProjectModelStoreSuite) TestSaveNoteUpsertsByComponentAndTopic() {
	comp, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)

	repoLevel, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA, Topic: domain.NoteGotchas, BodyMD: "repo-level",
	})
	s.Require().NoError(err)

	componentLevel, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA, ComponentID: &comp.ID, Topic: domain.NoteGotchas, BodyMD: "component-level",
	})
	s.Require().NoError(err)
	s.NotEqual(repoLevel.ID, componentLevel.ID, "a component-scoped note is distinct from the repository-level note on the same topic")

	updated, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA, ComponentID: &comp.ID, Topic: domain.NoteGotchas, BodyMD: "component-level v2",
	})
	s.Require().NoError(err)
	s.Equal(componentLevel.ID, updated.ID)
	s.Equal("component-level v2", updated.BodyMD)

	list, err := s.store.ListNotes(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Require().Len(list, 2)

	s.Require().NoError(s.store.DeleteNote(s.ctx, repoLevel.ID))
	_, err = s.store.GetNote(s.ctx, repoLevel.ID)
	s.Require().ErrorIs(err, port.ErrNotFound)
}

func (s *ProjectModelStoreSuite) TestMarkNotesStaleMatchesDirectoryPrefix() {
	inside, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA, Topic: domain.NoteInvariants, BodyMD: "x",
		Evidence: []domain.SourceEvidence{{Path: "internal/orders"}},
	})
	s.Require().NoError(err)

	exact, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA, Topic: domain.NoteConventions, BodyMD: "y",
		Evidence: []domain.SourceEvidence{{Path: "internal/orders/handler.go"}},
	})
	s.Require().NoError(err)

	untouched, err := s.store.SaveNote(s.ctx, domain.ProjectNote{
		RepositoryID: s.repoA, Topic: domain.NoteGotchas, BodyMD: "z",
		Evidence: []domain.SourceEvidence{{Path: "internal/billing/handler.go"}},
	})
	s.Require().NoError(err)

	stale, err := s.store.MarkNotesStale(s.ctx, s.repoA, []string{"internal/orders/handler.go"})
	s.Require().NoError(err)
	s.Require().Len(stale, 2)
	ids := []uuid.UUID{stale[0].ID, stale[1].ID}
	s.Contains(ids, inside.ID)
	s.Contains(ids, exact.ID)
	for _, n := range stale {
		s.True(n.Stale)
	}

	got, err := s.store.GetNote(s.ctx, untouched.ID)
	s.Require().NoError(err)
	s.False(got.Stale)
}

func (s *ProjectModelStoreSuite) TestScanCreateUpdateLatestAndFailInterrupted() {
	created, err := s.store.CreateScan(s.ctx, domain.ProjectScan{
		RepositoryID: s.repoA,
		Trigger:      domain.ScanTriggerImport,
		Status:       domain.ScanQueued,
	})
	s.Require().NoError(err)
	s.NotEqual(uuid.Nil, created.ID)
	s.NotNil(created.Events)
	s.Empty(created.Events)
	s.Nil(created.Result)

	updated := created
	updated.Status = domain.ScanSucceeded
	updated.Stage = domain.ScanStageNotes
	updated.CommitSHA = "abc123"
	updated.Events = []domain.ScanEvent{{Stage: domain.ScanStageClone, Done: true, Summary: "cloned", At: time.Now()}}
	updated.Result = &domain.ScanResult{
		Shape:      domain.RepoShapeSingle,
		FileCount:  12,
		Components: []domain.DetectedComponent{{Path: ".", Name: "api"}},
	}
	updated.ReviewCount = 2
	finishedAt := time.Now()
	updated.FinishedAt = &finishedAt

	s.Require().NoError(s.store.UpdateScan(s.ctx, updated))

	got, err := s.store.GetScan(s.ctx, created.ID)
	s.Require().NoError(err)
	s.Equal(domain.ScanSucceeded, got.Status)
	s.Equal(domain.ScanStageNotes, got.Stage)
	s.Equal("abc123", got.CommitSHA)
	s.Equal(2, got.ReviewCount)
	s.Require().NotNil(got.Result)
	s.Equal(12, got.Result.FileCount)
	s.Require().NotNil(got.FinishedAt)

	latest, err := s.store.LatestScan(s.ctx, s.repoA)
	s.Require().NoError(err)
	s.Equal(created.ID, latest.ID)
	s.Nil(latest.Result, "the latest scan must not load its result")

	interrupted, err := s.store.CreateScan(s.ctx, domain.ProjectScan{
		RepositoryID: s.repoA, Trigger: domain.ScanTriggerManual, Status: domain.ScanRunning,
	})
	s.Require().NoError(err)

	n, err := s.store.FailInterruptedScans(s.ctx)
	s.Require().NoError(err)
	s.GreaterOrEqual(n, 1)

	gotInterrupted, err := s.store.GetScan(s.ctx, interrupted.ID)
	s.Require().NoError(err)
	s.Equal(domain.ScanFailed, gotInterrupted.Status)
	s.Equal("interrupted by a server restart", gotInterrupted.Error)
	s.Require().NotNil(gotInterrupted.FinishedAt)
}

func (s *ProjectModelStoreSuite) TestApplyReconcileCreatesComponentsChecksAndLinksInOneCall() {
	compID := uuid.New()
	checkID := uuid.New()
	linkID := uuid.New()

	reconcile := port.ModelReconcile{
		RepositoryID: s.repoA,
		SaveComponents: []domain.Component{
			{ID: compID, RepositoryID: s.repoA, Path: "."},
		},
		SaveChecks: []domain.ComponentCheck{
			{ID: checkID, RepositoryID: s.repoA, ComponentID: compID, Workflow: "ci.yml", JobKey: "test"},
		},
		SaveLinks: []domain.ComponentLink{
			{ID: linkID, RepositoryID: s.repoA, FromComponentID: compID, Protocol: domain.LinkHTTP, Hint: "some-service"},
		},
	}
	s.Require().NoError(s.store.ApplyReconcile(s.ctx, reconcile))

	comp, err := s.store.GetComponent(s.ctx, compID)
	s.Require().NoError(err)
	s.Equal(".", comp.Path)

	check, err := s.store.GetCheck(s.ctx, checkID)
	s.Require().NoError(err)
	s.Equal(compID, check.ComponentID)

	link, err := s.store.GetLink(s.ctx, linkID)
	s.Require().NoError(err)
	s.Equal(compID, link.FromComponentID)
	s.False(link.Resolved())
}

func (s *ProjectModelStoreSuite) TestApplyReconcileDeletesAndIgnoresOtherRepository() {
	compA, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoA, Path: "."})
	s.Require().NoError(err)
	compB, err := s.store.SaveComponent(s.ctx, domain.Component{RepositoryID: s.repoB, Path: "."})
	s.Require().NoError(err)

	s.Require().NoError(s.store.ApplyReconcile(s.ctx, port.ModelReconcile{
		RepositoryID:     s.repoA,
		DeleteComponents: []uuid.UUID{compA.ID, compB.ID},
	}))

	_, err = s.store.GetComponent(s.ctx, compA.ID)
	s.Require().ErrorIs(err, port.ErrNotFound, "a component belonging to the reconciled repository must be deleted")

	stillThere, err := s.store.GetComponent(s.ctx, compB.ID)
	s.Require().NoError(err, "a delete id belonging to another repository must be ignored")
	s.Equal(compB.ID, stillThere.ID)
}
