package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// fakeComponentStore is the narrow in-memory port.ComponentStore these tools'
// tests need; SaveComponent/ListAllComponents/ListComponentsForRepositories
// exist only to satisfy the interface.
type fakeComponentStore struct {
	components map[uuid.UUID]domain.Component
}

func newFakeComponentStore() *fakeComponentStore {
	return &fakeComponentStore{components: map[uuid.UUID]domain.Component{}}
}

var _ port.ComponentStore = (*fakeComponentStore)(nil)

func (s *fakeComponentStore) ListComponents(_ context.Context, repositoryID uuid.UUID) ([]domain.Component, error) {
	var out []domain.Component
	for _, c := range s.components {
		if c.RepositoryID == repositoryID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *fakeComponentStore) ListComponentsForRepositories(_ context.Context, ids []uuid.UUID) ([]domain.Component, error) {
	return nil, nil
}

func (s *fakeComponentStore) ListAllComponents(context.Context) ([]domain.Component, error) {
	out := make([]domain.Component, 0, len(s.components))
	for _, c := range s.components {
		out = append(out, c)
	}
	return out, nil
}

func (s *fakeComponentStore) GetComponent(_ context.Context, id uuid.UUID) (domain.Component, error) {
	c, ok := s.components[id]
	if !ok {
		return domain.Component{}, fmt.Errorf("component %s: %w", id, port.ErrNotFound)
	}
	return c, nil
}

func (s *fakeComponentStore) SaveComponent(_ context.Context, c domain.Component) (domain.Component, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	s.components[c.ID] = c
	return c, nil
}

func (s *fakeComponentStore) put(c domain.Component) domain.Component {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.Status == "" {
		c.Status = domain.ComponentStatusActive
	}
	s.components[c.ID] = c
	return c
}

// fakeCloudAccounts is the smallest in-memory port.CloudAccountStore.
type fakeCloudAccounts struct {
	accounts map[uuid.UUID]domain.CloudAccount
	fields   map[uuid.UUID]map[string]string
}

func newFakeCloudAccounts() *fakeCloudAccounts {
	return &fakeCloudAccounts{accounts: map[uuid.UUID]domain.CloudAccount{}, fields: map[uuid.UUID]map[string]string{}}
}

func (f *fakeCloudAccounts) ListCloudAccounts(context.Context) ([]domain.CloudAccount, error) {
	out := make([]domain.CloudAccount, 0, len(f.accounts))
	for _, a := range f.accounts {
		out = append(out, a)
	}
	return out, nil
}

func (f *fakeCloudAccounts) GetCloudAccount(_ context.Context, id uuid.UUID) (domain.CloudAccount, error) {
	a, ok := f.accounts[id]
	if !ok {
		return domain.CloudAccount{}, port.ErrNotFound
	}
	return a, nil
}

func (f *fakeCloudAccounts) CreateCloudAccount(_ context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	if acct.ID == uuid.Nil {
		acct.ID = uuid.New()
	}
	f.accounts[acct.ID] = acct
	f.fields[acct.ID] = fields
	return acct, nil
}

func (f *fakeCloudAccounts) UpdateCloudAccount(_ context.Context, acct domain.CloudAccount, fields map[string]string) (domain.CloudAccount, error) {
	f.accounts[acct.ID] = acct
	if fields != nil {
		f.fields[acct.ID] = fields
	}
	return acct, nil
}

func (f *fakeCloudAccounts) DeleteCloudAccount(_ context.Context, id uuid.UUID) error {
	delete(f.accounts, id)
	return nil
}

func (f *fakeCloudAccounts) CloudCredential(_ context.Context, id uuid.UUID) (domain.CloudCredential, error) {
	a, ok := f.accounts[id]
	if !ok {
		return domain.CloudCredential{}, port.ErrNotFound
	}
	return domain.CloudCredential{AccountID: id, Provider: a.Provider, Meta: a.Meta, Fields: f.fields[id]}, nil
}

func (f *fakeCloudAccounts) put(a domain.CloudAccount) domain.CloudAccount {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.Status == "" {
		a.Status = domain.CloudAccountOK
	}
	f.accounts[a.ID] = a
	f.fields[a.ID] = map[string]string{"token": "tok"}
	return a
}

// fakeEnvironmentStore is the smallest in-memory port.EnvironmentStore.
type fakeEnvironmentStore struct {
	envs map[uuid.UUID]domain.ComponentEnvironment
}

func newFakeEnvironmentStore() *fakeEnvironmentStore {
	return &fakeEnvironmentStore{envs: map[uuid.UUID]domain.ComponentEnvironment{}}
}

func (f *fakeEnvironmentStore) ListEnvironments(_ context.Context, repositoryID uuid.UUID) ([]domain.ComponentEnvironment, error) {
	var out []domain.ComponentEnvironment
	for _, e := range f.envs {
		if e.RepositoryID == repositoryID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeEnvironmentStore) ListAllEnvironments(context.Context) ([]domain.ComponentEnvironment, error) {
	out := make([]domain.ComponentEnvironment, 0, len(f.envs))
	for _, e := range f.envs {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeEnvironmentStore) GetEnvironment(_ context.Context, id uuid.UUID) (domain.ComponentEnvironment, error) {
	e, ok := f.envs[id]
	if !ok {
		return domain.ComponentEnvironment{}, port.ErrNotFound
	}
	return e, nil
}

func (f *fakeEnvironmentStore) SaveEnvironment(_ context.Context, e domain.ComponentEnvironment) (domain.ComponentEnvironment, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.envs[e.ID] = e
	return e, nil
}

func (f *fakeEnvironmentStore) DeleteEnvironment(_ context.Context, id uuid.UUID) error {
	delete(f.envs, id)
	return nil
}

func (f *fakeEnvironmentStore) put(e domain.ComponentEnvironment) domain.ComponentEnvironment {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.envs[e.ID] = e
	return e
}

// fakeCloudProvider drives every runtime read with canned, per-test
// responses; Verify/ListResources exist only so it satisfies port.CloudProvider.
type fakeCloudProvider struct {
	kind      domain.CloudProviderKind
	logsPage  domain.RuntimeLogPage
	logsErr   error
	errGroups []domain.RuntimeErrorGroup
	errorsErr error
	deploys   []domain.CloudDeployment
	deployErr error
}

func (p *fakeCloudProvider) Kind() domain.CloudProviderKind { return p.kind }

func (p *fakeCloudProvider) Verify(context.Context, domain.CloudCredential) (map[string]string, error) {
	return map[string]string{}, nil
}

func (p *fakeCloudProvider) ListResources(context.Context, domain.CloudCredential) ([]domain.CloudResource, error) {
	return nil, nil
}

func (p *fakeCloudProvider) Resource(context.Context, domain.CloudCredential, domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	return domain.CloudResourceDetail{}, nil
}

func (p *fakeCloudProvider) Deployments(context.Context, domain.CloudCredential, domain.CloudResourceRef, domain.DeployEnvironment, int) ([]domain.CloudDeployment, error) {
	if p.deployErr != nil {
		return nil, p.deployErr
	}
	return p.deploys, nil
}

func (p *fakeCloudProvider) Logs(context.Context, domain.CloudCredential, domain.CloudResourceRef, domain.RuntimeLogQuery) (domain.RuntimeLogPage, error) {
	if p.logsErr != nil {
		return domain.RuntimeLogPage{}, p.logsErr
	}
	return p.logsPage, nil
}

func (p *fakeCloudProvider) Errors(context.Context, domain.CloudCredential, domain.CloudResourceRef, time.Time) ([]domain.RuntimeErrorGroup, error) {
	if p.errorsErr != nil {
		return nil, p.errorsErr
	}
	return p.errGroups, nil
}
