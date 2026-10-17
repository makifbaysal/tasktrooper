package errordef

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/application/datasource"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ServiceSuite struct {
	suite.Suite
	ctx       context.Context
	store     *memStore
	errors    *fakeErrors
	incidents *fakeIncidents
	tasks     *fakeTasks
	roles     *fakeRoles
	svc       *Service
	now       time.Time
	repo      uuid.UUID
	api       domain.Component
	web       domain.Component
	backendID uuid.UUID
}

func TestServiceSuite(t *testing.T) { suite.Run(t, new(ServiceSuite)) }

func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.store = newMemStore()
	s.errors = &fakeErrors{}
	s.incidents = &fakeIncidents{}
	s.tasks = &fakeTasks{}
	s.backendID = uuid.New()
	s.roles = &fakeRoles{developerRole: uuid.New(), byArea: map[string]uuid.UUID{domain.RepoKindBackend: s.backendID}}
	s.repo = uuid.New()
	s.api = domain.Component{ID: uuid.New(), RepositoryID: s.repo, Path: "services/api", Status: domain.ComponentStatusActive,
		Role: domain.Detected(domain.ComponentRoleBackend, domain.ConfidenceExact)}
	s.web = domain.Component{ID: uuid.New(), RepositoryID: s.repo, Path: "web", Status: domain.ComponentStatusActive,
		Role: domain.Detected(domain.ComponentRoleFrontend, domain.ConfidenceExact)}
	s.now = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	s.store.now = func() time.Time { return s.now }
	svc, err := NewService(Deps{
		Store:      s.store,
		Components: &fakeComponents{comps: []domain.Component{s.api, s.web}},
		Errors:     s.errors,
		Incidents:  s.incidents,
		Tasks:      s.tasks,
		Roles:      s.roles,
	}, WithClock(func() time.Time { return s.now }))
	s.Require().NoError(err)
	s.svc = svc
}

func ptr[T any](v T) *T { return &v }

// sweep runs one sweep a pace later than the last, so every window is due.
func (s *ServiceSuite) sweep() {
	s.now = s.now.Add(pace(taskWindow))
	s.svc.SweepOnce(s.ctx)
}

func (s *ServiceSuite) create(name string, match domain.ErrorMatch, action domain.ErrorAction, comp *uuid.UUID) domain.ErrorDefinition {
	req := DefinitionRequest{
		RepositoryID: ptr(s.repo.String()), Name: ptr(name), Match: &match,
		Category: ptr("bug"), Severity: ptr("high"), Action: ptr(string(action)),
	}
	if comp != nil {
		req.ComponentID = ptr(comp.String())
	}
	d, err := s.svc.Create(s.ctx, req)
	s.Require().NoError(err)
	return d
}

func (s *ServiceSuite) groupOf(comp domain.Component, fp, message string, count int) domain.OpsErrorGroup {
	return domain.OpsErrorGroup{ComponentID: comp.ID, RepositoryID: s.repo, EnvironmentID: uuid.New(), Provider: domain.CloudGCP,
		RuntimeErrorGroup: domain.RuntimeErrorGroup{Fingerprint: fp, Message: message, Count: count, FirstSeen: s.now, LastSeen: s.now}}
}

func (s *ServiceSuite) TestCreateReportsEveryFieldProblem() {
	tests := []struct {
		name  string
		req   DefinitionRequest
		field string
		code  string
	}{
		{name: "no repository", req: DefinitionRequest{Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug")}, field: "repository_id", code: datasource.FieldRequired},
		{name: "empty match", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{}, Category: ptr("bug")}, field: "match", code: datasource.FieldRequired},
		{name: "bad regex", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{MessageRegex: "("}, Category: ptr("bug")}, field: "match.message_regex", code: datasource.FieldInvalid},
		{name: "unknown category", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("oops")}, field: "category", code: datasource.FieldInvalid},
		{name: "unknown action", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug"), Action: ptr("page")}, field: "action", code: datasource.FieldInvalid},
		{name: "threshold on a task action", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug"), Action: ptr("task"),
			AlertThreshold: domain.SetNullable(domain.ErrorAlertThreshold{Count: 3, WindowSeconds: 600})}, field: "alert_threshold", code: datasource.FieldInvalidOption},
		{name: "threshold window too short", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug"), Action: ptr("alert"),
			AlertThreshold: domain.SetNullable(domain.ErrorAlertThreshold{Count: 3, WindowSeconds: 10})}, field: "alert_threshold.window_seconds", code: datasource.FieldInvalid},
		{name: "component of another repository", req: DefinitionRequest{RepositoryID: ptr(uuid.NewString()), ComponentID: ptr(s.api.ID.String()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug")}, field: "component_id", code: datasource.FieldInvalidOption},
		{name: "unknown component", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), ComponentID: ptr(uuid.NewString()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug")}, field: "component_id", code: datasource.FieldInvalid},
		{name: "bad status", req: DefinitionRequest{RepositoryID: ptr(s.repo.String()), Name: ptr("x"), Match: &domain.ErrorMatch{Status: 42}, Category: ptr("bug")}, field: "match.status", code: datasource.FieldInvalid},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.svc.Create(s.ctx, tt.req)
			var fe FieldErrors
			s.Require().ErrorAs(err, &fe)
			s.Equal(tt.code, fe[tt.field], fe)
			s.ErrorIs(err, ErrInvalidInput)
		})
	}
}

func (s *ServiceSuite) TestCreateIsAnActiveHumanDefinitionWithDefaults() {
	d := s.create("Timeout", domain.ErrorMatch{MessageRegex: " timeout "}, "", nil)
	s.Equal(domain.ErrorDefinitionHuman, d.Source)
	s.Equal(domain.ErrorDefinitionActive, d.Status)
	s.Equal(domain.ErrorActionNone, d.Action)
	s.Equal("timeout", d.Match.MessageRegex)
	s.Equal([]string{}, d.Evidence)
}

func (s *ServiceSuite) TestUpdatePatchesAndClearsButNeverMovesRepository() {
	d := s.create("Timeout", domain.ErrorMatch{Status: 504}, domain.ErrorActionAlert, &s.api.ID)
	d, err := s.svc.Update(s.ctx, d.ID, DefinitionRequest{
		ComponentID:    ptr(""),
		AlertThreshold: domain.SetNullable(domain.ErrorAlertThreshold{Count: 5, WindowSeconds: 300}),
	})
	s.Require().NoError(err)
	s.Nil(d.ComponentID)
	s.Equal(5, d.AlertThreshold.Count)
	s.Equal("Timeout", d.Name)

	d, err = s.svc.Update(s.ctx, d.ID, DefinitionRequest{AlertThreshold: domain.ClearNullable[domain.ErrorAlertThreshold]()})
	s.Require().NoError(err)
	s.Nil(d.AlertThreshold)

	_, err = s.svc.Update(s.ctx, d.ID, DefinitionRequest{RepositoryID: ptr(uuid.NewString())})
	var fe FieldErrors
	s.Require().ErrorAs(err, &fe)
	s.Equal(datasource.FieldNotPermitted, fe["repository_id"])
}

func (s *ServiceSuite) TestProposeStoresSuggestionsAndSkipsWhatExists() {
	dismissed := s.create("Old", domain.ErrorMatch{Status: 404}, domain.ErrorActionIgnore, nil)
	_, err := s.store.SetErrorDefinitionStatus(s.ctx, dismissed.ID, domain.ErrorDefinitionDismissed)
	s.Require().NoError(err)

	out, err := s.svc.Propose(s.ctx, s.repo, []Proposal{
		{Source: domain.ErrorDefinitionCode, DefinitionRequest: DefinitionRequest{Name: ptr("Validation"), Match: &domain.ErrorMatch{ExceptionType: "ValidationError"}, Category: ptr("user_error"), Action: ptr("ignore")}},
		{Source: domain.ErrorDefinitionAI, DefinitionRequest: DefinitionRequest{Name: ptr("Not found again"), Match: &domain.ErrorMatch{Status: 404}, Category: ptr("expected")}},
		{Source: domain.ErrorDefinitionAI, DefinitionRequest: DefinitionRequest{Name: ptr(""), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug")}},
		{Source: "", DefinitionRequest: DefinitionRequest{Name: ptr("Validation twice"), Match: &domain.ErrorMatch{ExceptionType: "ValidationError"}, Category: ptr("user_error")}},
	})
	s.Require().NoError(err)
	s.Require().Len(out, 4)
	s.Require().NotNil(out[0].Stored)
	s.Equal(domain.ErrorDefinitionSuggested, out[0].Stored.Status)
	s.Equal(domain.ErrorDefinitionCode, out[0].Stored.Source)
	s.Equal(dismissed.ID, *out[1].Existing)
	s.Equal(datasource.FieldRequired, out[2].Fields["name"])
	s.Equal(out[0].Stored.ID, *out[3].Existing)
}

func (s *ServiceSuite) TestAcceptAndDismissOnlyMoveSuggestions() {
	out, err := s.svc.Propose(s.ctx, s.repo, []Proposal{{Source: domain.ErrorDefinitionAI, DefinitionRequest: DefinitionRequest{
		Name: ptr("x"), Match: &domain.ErrorMatch{Status: 500}, Category: ptr("bug")}}})
	s.Require().NoError(err)
	id := out[0].Stored.ID

	d, err := s.svc.Accept(s.ctx, id)
	s.Require().NoError(err)
	s.Equal(domain.ErrorDefinitionActive, d.Status)
	s.Equal(domain.ErrorDefinitionAI, d.Source)
	_, err = s.svc.Accept(s.ctx, id)
	s.Require().NoError(err)
	_, err = s.svc.Dismiss(s.ctx, id)
	s.ErrorIs(err, ErrNotSuggested)

	human := s.create("y", domain.ErrorMatch{Status: 501}, "", nil)
	_, err = s.svc.Dismiss(s.ctx, human.ID)
	s.ErrorIs(err, ErrNotSuggested)
}

func (s *ServiceSuite) TestAnnotateMarksGroupsAndHidesIgnoredOnes() {
	wide := s.create("Any timeout", domain.ErrorMatch{MessageRegex: "timeout"}, domain.ErrorActionAlert, nil)
	own := s.create("API timeout", domain.ErrorMatch{MessageRegex: "timeout"}, domain.ErrorActionIgnore, &s.api.ID)
	s.create("Suggested only", domain.ErrorMatch{MessageRegex: "boom"}, domain.ErrorActionIgnore, nil)
	_, err := s.store.SetErrorDefinitionStatus(s.ctx, s.mustFind("Suggested only"), domain.ErrorDefinitionSuggested)
	s.Require().NoError(err)
	s.svc.forget(s.repo)

	in := domain.OpsErrors{Groups: []domain.OpsErrorGroup{
		s.groupOf(s.api, "a", "upstream timeout", 3),
		s.groupOf(s.web, "b", "upstream timeout", 2),
		s.groupOf(s.web, "c", "boom", 1),
	}}
	out := s.svc.Annotate(s.ctx, in, false)
	s.Equal(1, out.IgnoredCount)
	s.Require().Len(out.Groups, 2)
	s.Equal(wide.ID, out.Groups[0].Definition.ID)
	s.Nil(out.Groups[1].Definition)

	all := s.svc.Annotate(s.ctx, in, true)
	s.Require().Len(all.Groups, 3)
	s.Equal(own.ID, all.Groups[0].Definition.ID)
	s.Equal(domain.ErrorActionIgnore, all.Groups[0].Definition.Action)
}

func (s *ServiceSuite) TestAnnotateReadsDefinitionsFromCacheUntilAWrite() {
	s.create("A", domain.ErrorMatch{Status: 500}, "", nil)
	in := domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.api, "a", "status 500", 1)}}
	s.svc.Annotate(s.ctx, in, false)
	reads := s.store.lists
	s.svc.Annotate(s.ctx, in, false)
	s.Equal(reads, s.store.lists)
	s.create("B", domain.ErrorMatch{Status: 501}, "", nil)
	reads = s.store.lists
	s.svc.Annotate(s.ctx, in, false)
	s.Equal(reads+1, s.store.lists)
}

func (s *ServiceSuite) mustFind(name string) uuid.UUID {
	for id, d := range s.store.defs {
		if d.Name == name {
			return id
		}
	}
	s.FailNow("no definition " + name)
	return uuid.Nil
}

func (s *ServiceSuite) TestAlertFiresOnceAtTheThresholdAndResolvesBelowIt() {
	d := s.create("Timeout", domain.ErrorMatch{MessageRegex: "timeout"}, domain.ErrorActionAlert, &s.api.ID)
	_, err := s.svc.Update(s.ctx, d.ID, DefinitionRequest{AlertThreshold: domain.SetNullable(domain.ErrorAlertThreshold{Count: 5, WindowSeconds: 600})})
	s.Require().NoError(err)

	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.api, "a", "timeout", 3), s.groupOf(s.api, "b", "timeout", 2)}}
	s.sweep()
	s.Require().Len(s.incidents.ingested, 1)
	in := s.incidents.ingested[0]
	s.Equal(domain.ErrorDefinitionFingerprint(d.ID), in.Fingerprint)
	s.Equal(domain.IncidentSourceErrorDefinition, in.Source)
	s.Equal(domain.IncidentSeverityHigh, in.Severity)
	s.False(in.Resolved)
	s.Equal(domain.EnvironmentProduction, s.errors.queries[0].Environment)
	s.Equal(s.now.Add(-10*time.Minute), s.errors.queries[0].Since)
	s.Equal([]uuid.UUID{s.api.ID}, s.errors.queries[0].ComponentIDs)

	s.sweep()
	s.Len(s.incidents.ingested, 1)

	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.api, "a", "timeout", 1)},
		Unavailable: []domain.OpsUnavailable{{ComponentID: s.api.ID}}}
	s.sweep()
	s.Len(s.incidents.ingested, 1)

	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.api, "a", "timeout", 1)}}
	s.sweep()
	s.Require().Len(s.incidents.ingested, 2)
	s.True(s.incidents.ingested[1].Resolved)
	s.Nil(s.store.defs[d.ID].FiringSince)
}

func (s *ServiceSuite) TestIncidentActionNeverResolvesOnItsOwn() {
	d := s.create("Crash", domain.ErrorMatch{MessageRegex: "panic"}, domain.ErrorActionIncident, nil)
	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.web, "a", "panic: nil map", 1)}}
	s.sweep()
	s.Require().Len(s.incidents.ingested, 1)
	s.ElementsMatch([]uuid.UUID{s.api.ID, s.web.ID}, s.errors.queries[0].ComponentIDs)

	s.errors.answer = domain.OpsErrors{}
	s.sweep()
	s.Len(s.incidents.ingested, 1)
	s.Nil(s.store.defs[d.ID].FiringSince)
}

func (s *ServiceSuite) TestAMoreSpecificIgnoreDefinitionSilencesABroaderAlert() {
	s.create("Any timeout", domain.ErrorMatch{MessageRegex: "timeout"}, domain.ErrorActionIncident, nil)
	s.create("Known timeout", domain.ErrorMatch{Fingerprint: "known"}, domain.ErrorActionIgnore, nil)
	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.api, "known", "timeout", 50)}}
	s.sweep()
	s.Empty(s.incidents.ingested)
}

func (s *ServiceSuite) TestTaskActionOpensOneTaskPerGroupForTheComponentsDeveloper() {
	d := s.create("Nil user", domain.ErrorMatch{ExceptionType: "NullPointerException"}, domain.ErrorActionTask, nil)
	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{
		s.groupOf(s.api, "fp1", "java.lang.NullPointerException: user", 9),
		s.groupOf(s.api, "fp2", "java.lang.NullPointerException: order", 4),
	}}
	s.sweep()
	s.Require().Len(s.tasks.created, 1)
	task := s.tasks.created[0]
	s.Equal(s.backendID, *task.AssigneeAgentID)
	s.Equal(s.api.ID, *task.ComponentID)
	s.Equal(domain.TaskColumnTodo, task.Column)
	s.Equal(domain.TaskPriorityHigh, task.Priority)
	s.Contains(task.Title, "Nil user")
	s.Contains(task.Description, "fp1")
	s.NotNil(s.store.defs[d.ID].TaskID)

	s.sweep()
	s.Require().Len(s.tasks.created, 2)
	s.Contains(s.tasks.created[1].Description, "fp2")

	s.sweep()
	s.Len(s.tasks.created, 2)
}

func (s *ServiceSuite) TestTaskActionReleasesItsClaimWhenTheBoardFails() {
	s.create("Nil user", domain.ErrorMatch{MessageRegex: "nil"}, domain.ErrorActionTask, nil)
	s.errors.answer = domain.OpsErrors{Groups: []domain.OpsErrorGroup{s.groupOf(s.web, "fp1", "nil pointer", 1)}}
	s.tasks.fail = true
	s.sweep()
	s.Empty(s.store.links)

	s.tasks.fail = false
	s.sweep()
	s.Require().Len(s.tasks.created, 1)
	s.Nil(s.tasks.created[0].AssigneeAgentID)
}

func (s *ServiceSuite) TestSweepReadsNothingWithoutActingDefinitions() {
	s.create("Labelled", domain.ErrorMatch{Status: 500}, domain.ErrorActionNone, nil)
	s.sweep()
	s.Empty(s.errors.queries)
}
