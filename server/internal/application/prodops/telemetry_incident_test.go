package prodops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prodops"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type fakeReleaseRoles struct {
	roleErr   error
	agentID   uuid.UUID
	noAgent   bool
	askedRole string
}

func (f *fakeReleaseRoles) RoleByKey(_ context.Context, key string) (domain.AgentRole, error) {
	f.askedRole = key
	return domain.AgentRole{ID: uuid.New()}, f.roleErr
}

func (f *fakeReleaseRoles) AgentForRole(context.Context, uuid.UUID, string) (*uuid.UUID, error) {
	if f.noAgent {
		return nil, nil
	}
	id := f.agentID
	return &id, nil
}

func telemetryInput(repoID uuid.UUID) domain.IncidentInput {
	return domain.IncidentInput{
		RepositoryID: repoID,
		Env:          domain.DeployEnvProd,
		Source:       domain.IncidentSourceTelemetry,
		Fingerprint:  "telemetry:abc123:42",
		Severity:     domain.IncidentSeverityHigh,
		Title:        "orders-api error rate above 5%",
		Detail:       "Service: orders-api",
		Payload: map[string]any{
			"service": "orders-api", "endpoint": "POST /orders", "telemetry_incident_id": "42",
		},
	}
}

func newTelemetryIngestFixture(roles *fakeReleaseRoles, defaultAgent uuid.UUID) (*prodops.Service, *fakeIncidentTaskBoard, uuid.UUID) {
	repo := domain.Repository{ID: uuid.New(), Kind: domain.RepoKindBackend}
	tasks := &fakeIncidentTaskBoard{}
	svc := prodops.NewService(prodops.Deps{
		Incidents: newFakeIncidents(),
		Repos:     fakeIncidentRepos{repo: repo},
		Tasks:     tasks,
	})
	svc.SetRoleResolver(fakeIncidentRoleResolver{agentID: defaultAgent})
	if roles != nil {
		svc.SetReleaseRoles(roles)
	}
	return svc, tasks, repo.ID
}

func TestTelemetryIncidentGoesToTheReleaseEngineerWithTheTelemetryBrief(t *testing.T) {
	roles := &fakeReleaseRoles{agentID: uuid.New()}
	svc, tasks, repoID := newTelemetryIngestFixture(roles, uuid.New())

	_, err := svc.Ingest(context.Background(), telemetryInput(repoID))
	require.NoError(t, err)
	require.Len(t, tasks.created, 1)

	req := tasks.created[0]
	require.Equal(t, domain.RoleKeyRelease, roles.askedRole)
	require.NotNil(t, req.AssigneeAgentID)
	require.Equal(t, roles.agentID, *req.AssigneeAgentID)
	require.Equal(t, "Incident: orders-api error rate above 5%", req.Title)
	require.Contains(t, req.Description, "TaskTrooper Telemetry opened an incident on **prod**")
	require.Contains(t, req.Description, "`orders-api` · endpoint `POST /orders`")
	require.Contains(t, req.Description, "Telemetry incident id: `42`")
	require.Contains(t, req.Description, "telemetry-incident-response")
	require.Contains(t, req.Description, "propose_incident_remedy")
	require.NotContains(t, req.Description, "Policy: suggest")
}

func TestTelemetryIncidentFallsBackToTheDefaultAssigneeWithoutAReleaseEngineer(t *testing.T) {
	for name, roles := range map[string]*fakeReleaseRoles{
		"no role":  {roleErr: errors.New("no such role")},
		"no agent": {noAgent: true},
		"no hook":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			def := uuid.New()
			svc, tasks, repoID := newTelemetryIngestFixture(roles, def)
			_, err := svc.Ingest(context.Background(), telemetryInput(repoID))
			require.NoError(t, err)
			require.Len(t, tasks.created, 1)
			require.NotNil(t, tasks.created[0].AssigneeAgentID)
			require.Equal(t, def, *tasks.created[0].AssigneeAgentID)
			require.Contains(t, tasks.created[0].Description, "Production incident on **prod**")
		})
	}
}

func TestOtherSourcesKeepTheDeveloperBriefEvenWithAReleaseRoleWired(t *testing.T) {
	def := uuid.New()
	svc, tasks, repoID := newTelemetryIngestFixture(&fakeReleaseRoles{agentID: uuid.New()}, def)
	in := telemetryInput(repoID)
	in.Source = domain.IncidentSourceWebhook
	_, err := svc.Ingest(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, def, *tasks.created[0].AssigneeAgentID)
	require.Contains(t, tasks.created[0].Description, "Policy: suggest")
}
