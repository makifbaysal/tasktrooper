package observability

import (
	"context"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/application/alerting"
	"github.com/makifbaysal/tasktrooper/server/internal/application/errordef"
	"github.com/makifbaysal/tasktrooper/server/internal/application/opsquery"
	"github.com/makifbaysal/tasktrooper/server/internal/application/signals"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeComponents struct{ comps []domain.Component }

func (f *fakeComponents) ListComponents(_ context.Context, repo uuid.UUID) ([]domain.Component, error) {
	var out []domain.Component
	for _, c := range f.comps {
		if c.RepositoryID == repo {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeComponents) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	for _, c := range f.comps {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Component{}, errNotFound
}

type fakeErrors struct {
	got    opsquery.ErrorsQuery
	answer domain.OpsErrors
}

func (f *fakeErrors) Errors(_ context.Context, q opsquery.ErrorsQuery) (domain.OpsErrors, error) {
	f.got = q
	return f.answer, nil
}

type fakeDefinitions struct {
	annotate  func(domain.OpsErrors) domain.OpsErrors
	proposals []errordef.Proposal
	outcomes  []errordef.ProposalOutcome
}

func (f *fakeDefinitions) Annotate(_ context.Context, in domain.OpsErrors, _ bool) domain.OpsErrors {
	if f.annotate != nil {
		return f.annotate(in)
	}
	return in
}

func (f *fakeDefinitions) Propose(_ context.Context, _ uuid.UUID, p []errordef.Proposal) ([]errordef.ProposalOutcome, error) {
	f.proposals = p
	return f.outcomes, nil
}

type fakeJobs struct {
	job      *domain.AIJob
	recorded []domain.AIJobItem
}

func (f *fakeJobs) SessionJob(context.Context) (domain.AIJob, bool) {
	if f.job == nil {
		return domain.AIJob{}, false
	}
	return *f.job, true
}

func (f *fakeJobs) Record(_ context.Context, item domain.AIJobItem) {
	f.recorded = append(f.recorded, item)
}

type fakeDataSources struct {
	sources    []domain.DataSource
	discovered []uuid.UUID
}

func (f *fakeDataSources) List(context.Context) ([]domain.DataSource, error) { return f.sources, nil }
func (f *fakeDataSources) Discover(_ context.Context, id uuid.UUID, _ string, _ int) (domain.DataSourceDiscovery, error) {
	f.discovered = append(f.discovered, id)
	return domain.DataSourceDiscovery{}, nil
}
func (f *fakeDataSources) Recipes(context.Context) ([]domain.Recipe, error) { return nil, nil }

type fakeSourceUse struct{ bindings []domain.SignalBinding }

func (f *fakeSourceUse) ListConfirmedSignalBindings(context.Context, domain.DeployEnvironment) ([]domain.SignalBinding, error) {
	return f.bindings, nil
}

type fakeBindings struct {
	got  signals.BindingRequest
	view domain.SignalBindingView
}

func (f *fakeBindings) Propose(_ context.Context, _ uuid.UUID, _ domain.DeployEnvironment, _ string, req signals.BindingRequest) (domain.SignalBindingView, error) {
	f.got = req
	return f.view, nil
}

type fakeAlerts struct {
	rules   []domain.AlertRule
	created []alerting.RuleRequest
}

func (f *fakeAlerts) List(context.Context, domain.AlertRuleFilter) ([]domain.AlertRule, error) {
	return f.rules, nil
}

func (f *fakeAlerts) CreateAIDraft(_ context.Context, req alerting.RuleRequest) (domain.AlertRule, error) {
	f.created = append(f.created, req)
	return domain.AlertRule{ID: uuid.New(), AlertRuleSpec: domain.AlertRuleSpec{Name: *req.Name}, CreatedBy: domain.AlertRuleByAI}, nil
}

func testKit() *ToolKit {
	return &ToolKit{
		Components:  &fakeComponents{},
		Errors:      &fakeErrors{},
		Definitions: &fakeDefinitions{},
		Jobs:        &fakeJobs{},
		DataSources: &fakeDataSources{},
		SourceUse:   &fakeSourceUse{},
		Bindings:    &fakeBindings{},
		Alerts:      &fakeAlerts{},
	}
}
