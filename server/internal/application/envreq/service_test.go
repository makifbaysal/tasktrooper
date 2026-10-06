package envreq

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeStore struct{ rows []domain.EnvRequirement }

func (f *fakeStore) ListEnvRequirements(_ context.Context, repositoryID uuid.UUID) ([]domain.EnvRequirement, error) {
	var out []domain.EnvRequirement
	for _, r := range f.rows {
		if r.RepositoryID == repositoryID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertEnvRequirement(_ context.Context, r domain.EnvRequirement) (domain.EnvRequirement, error) {
	for i, have := range f.rows {
		if have.RepositoryID == r.RepositoryID && have.Name == r.Name && sameComponent(have.ComponentID, r.ComponentID) {
			if have.Source == domain.EnvSourceHuman && r.Source != domain.EnvSourceHuman {
				return have, nil
			}
			f.rows[i] = r
			return r, nil
		}
	}
	f.rows = append(f.rows, r)
	return r, nil
}

func sameComponent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

type fakeEnvs struct{ envs []domain.ComponentEnvironment }

func (f fakeEnvs) ListEnvironments(context.Context, uuid.UUID) ([]domain.ComponentEnvironment, error) {
	return f.envs, nil
}

type fakeComponents struct{ comps []domain.Component }

func (f fakeComponents) ListComponents(context.Context, uuid.UUID) ([]domain.Component, error) {
	return f.comps, nil
}

type fakeExamples struct{ examples []domain.EnvExample }

func (f fakeExamples) EnvExamples(context.Context, uuid.UUID, *uuid.UUID) ([]domain.EnvExample, error) {
	return f.examples, nil
}

type fakeCloud struct {
	caps       *domain.EnvCapabilities
	vars       map[string][]domain.DeployEnvironment
	readErr    error
	writes     []domain.CloudEnvWrite
	redeployed []uuid.UUID
}

func (f *fakeCloud) EnvCapabilities(context.Context, uuid.UUID) (domain.EnvCapabilities, error) {
	if f.caps != nil {
		return *f.caps, nil
	}
	return domain.EnvCapabilities{Targets: domain.EnvTargets}, nil
}

func (f *fakeCloud) EnvVars(context.Context, uuid.UUID) ([]domain.CloudEnvVar, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	var out []domain.CloudEnvVar
	for k, t := range f.vars {
		out = append(out, domain.CloudEnvVar{Key: k, Targets: t})
	}
	return out, nil
}

func (f *fakeCloud) WriteEnvVars(_ context.Context, _ uuid.UUID, writes []domain.CloudEnvWrite) error {
	f.writes = append(f.writes, writes...)
	for _, w := range writes {
		f.vars[w.Key] = append(f.vars[w.Key], w.Targets...)
	}
	return nil
}

func (f *fakeCloud) Redeploy(_ context.Context, envID uuid.UUID) (domain.CloudDeployment, error) {
	f.redeployed = append(f.redeployed, envID)
	return domain.CloudDeployment{ID: "dpl_new"}, nil
}

type fixture struct {
	svc     *Service
	store   *fakeStore
	cloud   *fakeCloud
	repo    uuid.UUID
	comp    domain.Component
	env     domain.ComponentEnvironment
	resumed int
}

func newFixture(examples ...domain.EnvExample) *fixture {
	repo := uuid.New()
	comp := domain.Component{ID: uuid.New(), RepositoryID: repo, Path: "."}
	env := domain.ComponentEnvironment{
		ID: uuid.New(), RepositoryID: repo, ComponentID: comp.ID, Environment: domain.EnvironmentProduction,
		Provider: domain.CloudVercel, AccountID: ptr(uuid.New()), Status: domain.LinkConfirmed,
		Resource: &domain.CloudResourceRef{ID: "prj_1", Name: "site"},
	}
	f := &fixture{store: &fakeStore{}, cloud: &fakeCloud{vars: map[string][]domain.DeployEnvironment{}}, repo: repo, comp: comp, env: env}
	f.svc = New(Deps{
		Store:        f.store,
		Environments: fakeEnvs{envs: []domain.ComponentEnvironment{env}},
		Cloud:        f.cloud,
		Components:   fakeComponents{comps: []domain.Component{comp}},
		Examples:     fakeExamples{examples: examples},
		Secret:       func() (string, error) { return "generated-secret", nil },
		Hash:         func(p string) (string, error) { return "bcrypt(" + p + ")", nil },
	})
	f.svc.SetResumer(func(context.Context, uuid.UUID) { f.resumed++ })
	return f
}

func ptr[T any](v T) *T { return &v }

func (f *fixture) declare(name string, kind domain.EnvVarKind, value string) {
	f.store.rows = append(f.store.rows, domain.EnvRequirement{RepositoryID: f.repo, Name: name, Kind: kind, Value: value, Source: domain.EnvSourceAgent})
}

func (f *fixture) written(key string) (domain.CloudEnvWrite, bool) {
	for _, w := range f.cloud.writes {
		if w.Key == key {
			return w, true
		}
	}
	return domain.CloudEnvWrite{}, false
}

func TestEnsureFillsWhatItCanAndReportsWhatOnlyAHumanCan(t *testing.T) {
	f := newFixture(
		domain.EnvExample{Name: "CONTENT_BACKEND", Path: ".env.example"},
		domain.EnvExample{Name: "GITHUB_REPO", Value: "o/r", Path: ".env.example"},
	)
	f.declare("SESSION_SECRET", domain.EnvKindGenerated, "")
	f.declare("GITHUB_BRANCH", domain.EnvKindValue, "master")
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")
	f.declare("ADMIN_PASSWORD_HASH", domain.EnvKindHumanBcrypt, "")
	f.declare("DEBUG", domain.EnvKindOptional, "")

	res, err := f.svc.Ensure(context.Background(), f.repo, f.comp.ID, nil)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"GITHUB_BRANCH", "SESSION_SECRET"}, res.Created)
	assert.ElementsMatch(t, []string{"ADMIN_PASSWORD_HASH", "CONTENT_BACKEND", "GITHUB_REPO", "GITHUB_TOKEN"}, res.Missing,
		"an example-only variable waits for a human to classify it, even when the example has a value")
	secret, _ := f.written("SESSION_SECRET")
	assert.Equal(t, "generated-secret", secret.Value)
	assert.True(t, secret.Sensitive)
	assert.Equal(t, domain.EnvTargets, secret.Targets)
	branch, _ := f.written("GITHUB_BRANCH")
	assert.Equal(t, "master", branch.Value)
	assert.False(t, branch.Sensitive)
	_, wroteOptional := f.written("DEBUG")
	assert.False(t, wroteOptional)
}

func TestEnsureLeavesSetVariablesAloneAndOnlyFillsAMissingPreview(t *testing.T) {
	f := newFixture()
	f.declare("SESSION_SECRET", domain.EnvKindGenerated, "")
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")
	f.cloud.vars["SESSION_SECRET"] = []domain.DeployEnvironment{domain.EnvironmentProduction}
	f.cloud.vars["GITHUB_TOKEN"] = []domain.DeployEnvironment{domain.EnvironmentProduction}

	res, err := f.svc.Ensure(context.Background(), f.repo, f.comp.ID, nil)

	require.NoError(t, err)
	assert.Empty(t, res.Missing, "a human secret set for production does not block the merge")
	require.Len(t, f.cloud.writes, 1)
	assert.Equal(t, []domain.DeployEnvironment{domain.EnvironmentPreview}, f.cloud.writes[0].Targets)
}

func TestEnsureHasNothingToCheckWithoutAManagedTarget(t *testing.T) {
	f := newFixture()
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")

	res, err := f.svc.Ensure(context.Background(), f.repo, uuid.New(), nil)

	require.NoError(t, err)
	assert.Empty(t, res.Missing)
}

func TestEnsureCannotSayWhenTheProviderIsUnreadable(t *testing.T) {
	f := newFixture()
	f.cloud.readErr = errors.New("vercel down")

	_, err := f.svc.Ensure(context.Background(), f.repo, f.comp.ID, nil)

	assert.ErrorIs(t, err, domain.ErrDeployEnvUnchecked)
}

func TestApplyWritesHumanSecretsStraightToTheProviderAndResumes(t *testing.T) {
	f := newFixture(domain.EnvExample{Name: "CONTENT_BACKEND", Path: ".env.example"})
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")
	f.declare("ADMIN_PASSWORD_HASH", domain.EnvKindHumanBcrypt, "")

	st, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{
		{Name: "GITHUB_TOKEN", Value: "  ghp_abc\n"},
		{Name: "ADMIN_PASSWORD_HASH", Value: "correct horse"},
		{Name: "CONTENT_BACKEND", Kind: domain.EnvKindOptional},
	})

	require.NoError(t, err)
	token, _ := f.written("GITHUB_TOKEN")
	assert.Equal(t, "ghp_abc", token.Value)
	assert.True(t, token.Sensitive)
	hash, _ := f.written("ADMIN_PASSWORD_HASH")
	assert.Equal(t, "bcrypt(correct horse)", hash.Value)
	assert.Empty(t, st.Missing())
	assert.Equal(t, 1, f.resumed)
	for _, r := range f.store.rows {
		assert.NotContains(t, r.Value, "ghp_abc", "a secret is never stored")
		assert.NotContains(t, r.Value, "correct horse")
	}
	assert.Equal(t, domain.EnvKindOptional, f.store.rows[len(f.store.rows)-1].Kind)
	assert.Equal(t, domain.EnvSourceHuman, f.store.rows[len(f.store.rows)-1].Source)
}

func TestApplyClassifyingAValueWritesItAndStoresIt(t *testing.T) {
	f := newFixture(domain.EnvExample{Name: "GITHUB_REPO", Value: "o/r", Path: ".env.example"})

	st, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "GITHUB_REPO", Kind: domain.EnvKindValue, Value: "o/r"}})

	require.NoError(t, err)
	w, ok := f.written("GITHUB_REPO")
	require.True(t, ok)
	assert.Equal(t, "o/r", w.Value)
	assert.Empty(t, st.Missing())
}

func TestApplyDoesNotResumeWhileSomethingIsStillMissing(t *testing.T) {
	f := newFixture()
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")
	f.declare("STRIPE_KEY", domain.EnvKindHumanSecret, "")

	st, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "GITHUB_TOKEN", Value: "ghp_abc"}})

	require.NoError(t, err)
	assert.Equal(t, []string{"STRIPE_KEY"}, st.Missing())
	assert.Zero(t, f.resumed)
}

func TestApplyRejectsAShortPassword(t *testing.T) {
	f := newFixture()
	f.declare("ADMIN_PASSWORD_HASH", domain.EnvKindHumanBcrypt, "")

	_, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "ADMIN_PASSWORD_HASH", Value: "short"}})

	assert.ErrorIs(t, err, domain.ErrEnvRequirementInvalid)
	assert.Empty(t, f.cloud.writes)
}

func TestDeclareRejectsASecretValueAndStoresNothing(t *testing.T) {
	f := newFixture()

	_, err := f.svc.Declare(context.Background(), f.repo, nil, uuid.New(), []domain.EnvRequirement{
		{Name: "GITHUB_REPO", Kind: domain.EnvKindValue, Value: "o/r"},
		{Name: "GITHUB_TOKEN", Kind: domain.EnvKindHumanSecret, Value: "ghp_leaked"},
	})

	assert.ErrorIs(t, err, domain.ErrEnvRequirementInvalid)
	assert.Empty(t, f.store.rows)
}

func TestDeclareRecordsTheAgentAndTheTask(t *testing.T) {
	f := newFixture()
	task := uuid.New()

	out, err := f.svc.Declare(context.Background(), f.repo, nil, task, []domain.EnvRequirement{
		{Name: "SESSION_SECRET", Kind: domain.EnvKindGenerated, Description: "signs the admin cookie"},
	})

	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, domain.EnvSourceAgent, out[0].Source)
	assert.Equal(t, task, *out[0].TaskID)
}

func TestAComponentRowOverridesTheRepositoryWideOne(t *testing.T) {
	f := newFixture()
	f.declare("API_URL", domain.EnvKindValue, "https://repo-wide")
	f.store.rows = append(f.store.rows, domain.EnvRequirement{
		RepositoryID: f.repo, ComponentID: &f.comp.ID, Name: "API_URL", Kind: domain.EnvKindValue, Value: "https://component", Source: domain.EnvSourceHuman,
	})

	_, err := f.svc.Ensure(context.Background(), f.repo, f.comp.ID, nil)

	require.NoError(t, err)
	w, _ := f.written("API_URL")
	assert.Equal(t, "https://component", w.Value)
}

func TestExampleFilesBelongToTheComponentUnderThem(t *testing.T) {
	web := domain.Component{ID: uuid.New(), Path: "apps/web"}
	api := domain.Component{ID: uuid.New(), Path: "apps/api"}
	root := domain.Component{ID: uuid.New(), Path: "."}
	comps := []domain.Component{web, api, root}

	assert.Equal(t, web.ID, exampleComponent("apps/web/.env.example", comps))
	assert.Equal(t, api.ID, exampleComponent("apps/api/.env.sample", comps))
	assert.Equal(t, root.ID, exampleComponent(".env.example", comps))
	assert.Equal(t, root.ID, exampleComponent("tools/.env.example", comps))
	assert.Equal(t, web.ID, exampleComponent(".env.example", []domain.Component{web}), "one component owns every file")
}

func TestRedeployGoesToTheComponentsProductionTarget(t *testing.T) {
	f := newFixture()

	d, err := f.svc.Redeploy(context.Background(), f.repo, f.comp.ID)

	require.NoError(t, err)
	assert.Equal(t, "dpl_new", d.ID)
	assert.Equal(t, []uuid.UUID{f.env.ID}, f.cloud.redeployed)
}

func TestRandomSecretsAreLongAndDistinct(t *testing.T) {
	a, err := randomSecret()
	require.NoError(t, err)
	b, _ := randomSecret()
	assert.GreaterOrEqual(t, len(a), 43)
	assert.NotEqual(t, a, b)
	assert.False(t, strings.ContainsAny(a, "+/="), "URL-safe without padding")
}

func TestBcryptHashVerifiesAgainstThePassword(t *testing.T) {
	h, err := bcryptHash("correct horse")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "$2a$"))
}

func TestATargetWithoutPreviewsIsWrittenForProductionAlone(t *testing.T) {
	f := newFixture()
	f.cloud.caps = &domain.EnvCapabilities{Targets: []domain.DeployEnvironment{domain.EnvironmentProduction}, OverwrittenOnDeploy: true}
	f.declare("SESSION_SECRET", domain.EnvKindGenerated, "")
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")

	res, err := f.svc.Ensure(context.Background(), f.repo, f.comp.ID, nil)
	require.NoError(t, err)
	assert.True(t, res.OverwrittenOnDeploy)
	require.Len(t, f.cloud.writes, 1)
	assert.Equal(t, []domain.DeployEnvironment{domain.EnvironmentProduction}, f.cloud.writes[0].Targets)

	again, err := f.svc.Ensure(context.Background(), f.repo, f.comp.ID, nil)
	require.NoError(t, err)
	assert.Empty(t, again.Created, "a set production variable is complete where there are no previews")

	_, err = f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "GITHUB_TOKEN", Value: "ghp_abc"}})
	require.NoError(t, err)
	token, _ := f.written("GITHUB_TOKEN")
	assert.Equal(t, []domain.DeployEnvironment{domain.EnvironmentProduction}, token.Targets)
}

func TestRedeployIsRefusedWhereAWriteAlreadyRollsOut(t *testing.T) {
	f := newFixture()
	f.cloud.caps = &domain.EnvCapabilities{Targets: []domain.DeployEnvironment{domain.EnvironmentProduction}, WritesRollOut: true}

	_, err := f.svc.Redeploy(context.Background(), f.repo, f.comp.ID)

	assert.ErrorIs(t, err, domain.ErrEnvRequirementInvalid)
	assert.Empty(t, f.cloud.redeployed)
}

func TestApplyChangesAValueTheProviderAlreadyHas(t *testing.T) {
	f := newFixture()
	f.declare("GITHUB_BRANCH", domain.EnvKindValue, "master")
	f.cloud.vars["GITHUB_BRANCH"] = domain.EnvTargets

	_, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "GITHUB_BRANCH", Value: "main"}})

	require.NoError(t, err)
	w, ok := f.written("GITHUB_BRANCH")
	require.True(t, ok, "a changed value is written even though the variable exists")
	assert.Equal(t, "main", w.Value)
	stored := f.store.rows[len(f.store.rows)-1]
	assert.Equal(t, "main", stored.Value)
	assert.Equal(t, domain.EnvSourceHuman, stored.Source)
}

func TestApplyRotatesASecretTheProviderAlreadyHas(t *testing.T) {
	f := newFixture()
	f.declare("GITHUB_TOKEN", domain.EnvKindHumanSecret, "")
	f.declare("SESSION_SECRET", domain.EnvKindGenerated, "")
	f.cloud.vars["GITHUB_TOKEN"] = domain.EnvTargets
	f.cloud.vars["SESSION_SECRET"] = domain.EnvTargets

	_, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{
		{Name: "GITHUB_TOKEN", Value: "ghp_new"},
		{Name: "SESSION_SECRET", Regenerate: true},
	})

	require.NoError(t, err)
	token, _ := f.written("GITHUB_TOKEN")
	assert.Equal(t, "ghp_new", token.Value)
	secret, ok := f.written("SESSION_SECRET")
	require.True(t, ok)
	assert.Equal(t, "generated-secret", secret.Value)
	assert.True(t, secret.Sensitive)
}

func TestApplyAddsAVariableNobodyDeclared(t *testing.T) {
	f := newFixture()

	st, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "FEATURE_FLAG", Kind: domain.EnvKindValue, Value: "on"}})

	require.NoError(t, err)
	w, _ := f.written("FEATURE_FLAG")
	assert.Equal(t, "on", w.Value)
	require.Len(t, st.Vars, 1)
	assert.Equal(t, "FEATURE_FLAG", st.Vars[0].Name)
}

func TestApplyRejectsAnInvalidNewName(t *testing.T) {
	f := newFixture()

	_, err := f.svc.Apply(context.Background(), f.repo, f.comp.ID, []EnvInput{{Name: "BAD-NAME", Kind: domain.EnvKindValue, Value: "x"}})

	assert.ErrorIs(t, err, domain.ErrEnvRequirementInvalid)
}
