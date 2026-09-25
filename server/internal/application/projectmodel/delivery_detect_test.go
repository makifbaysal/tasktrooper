package projectmodel

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func deployCheck(mut func(*domain.ComponentCheck)) domain.ComponentCheck {
	c := domain.ComponentCheck{
		Status:      domain.ModelStatusActive,
		Purpose:     domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckDeploy)},
		Environment: domain.EnvironmentProduction,
		Workflow:    ".github/workflows/deploy.yml",
		JobKey:      "deploy",
	}
	if mut != nil {
		mut(&c)
	}
	return c
}

func TestDetectDelivery_Mobile(t *testing.T) {
	comp := domain.Component{Mobile: &domain.Fact[domain.MobileFacts]{Detected: &domain.MobileFacts{Platform: "ios"}}}
	fact, ok := detectDelivery(comp, nil, nil)
	require.True(t, ok)
	require.NotNil(t, fact.Detected)
	require.Equal(t, domain.DeliveryBatch, fact.Detected.Mode)
	require.Equal(t, domain.ExecutorStore, fact.Detected.Executor)
	require.Equal(t, domain.ConfidenceMedium, fact.Confidence)
}

func TestDetectDelivery_MobileTakesPriorityOverDeployChecks(t *testing.T) {
	comp := domain.Component{Mobile: &domain.Fact[domain.MobileFacts]{Detected: &domain.MobileFacts{Platform: "android"}}}
	checks := []domain.ComponentCheck{deployCheck(func(c *domain.ComponentCheck) { c.Triggers = []string{"push:main"} })}
	fact, ok := detectDelivery(comp, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryBatch, fact.Detected.Mode)
	require.Equal(t, domain.ExecutorStore, fact.Detected.Executor)
}

func TestDetectDelivery_PushToMainIsOnMerge(t *testing.T) {
	checks := []domain.ComponentCheck{deployCheck(func(c *domain.ComponentCheck) {
		c.Triggers = []string{"push:main"}
		c.Workflow = ".github/workflows/deploy-prod.yml"
	})}
	fact, ok := detectDelivery(domain.Component{}, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryOnMerge, fact.Detected.Mode)
	require.Equal(t, domain.ExecutorGitHubActions, fact.Detected.Executor)
	require.Equal(t, "deploy-prod.yml", fact.Detected.Workflow)
	require.Equal(t, domain.ConfidenceHigh, fact.Confidence)
	require.Equal(t, true, fact.Detected.AutoRollback)
	require.Equal(t, domain.DefaultSoakMinutes, fact.Detected.Verify.SoakMinutes)
	require.NotEmpty(t, fact.Evidence)
}

func TestDetectDelivery_PushToMasterIsAlsoOnMerge(t *testing.T) {
	checks := []domain.ComponentCheck{deployCheck(func(c *domain.ComponentCheck) { c.Triggers = []string{"push:master"} })}
	fact, ok := detectDelivery(domain.Component{}, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryOnMerge, fact.Detected.Mode)
}

func TestDetectDelivery_DispatchableWithoutPushIsDispatch(t *testing.T) {
	checks := []domain.ComponentCheck{deployCheck(func(c *domain.ComponentCheck) {
		c.Dispatchable = true
		c.Triggers = []string{"workflow_dispatch"}
		c.Workflow = ".github/workflows/deploy.yml"
	})}
	fact, ok := detectDelivery(domain.Component{}, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryDispatch, fact.Detected.Mode)
	require.Equal(t, domain.ExecutorGitHubActions, fact.Detected.Executor)
	require.Equal(t, "deploy.yml", fact.Detected.Workflow)
	require.Equal(t, domain.ConfidenceMedium, fact.Confidence)
}

func TestDetectDelivery_PushToMainBeatsDispatchable(t *testing.T) {
	checks := []domain.ComponentCheck{
		deployCheck(func(c *domain.ComponentCheck) {
			c.JobKey = "dispatch-deploy"
			c.Dispatchable = true
			c.Triggers = []string{"workflow_dispatch"}
			c.Workflow = ".github/workflows/dispatch.yml"
		}),
		deployCheck(func(c *domain.ComponentCheck) {
			c.JobKey = "merge-deploy"
			c.Triggers = []string{"push:main"}
			c.Workflow = ".github/workflows/onmerge.yml"
		}),
	}
	fact, ok := detectDelivery(domain.Component{}, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryOnMerge, fact.Detected.Mode)
	require.Equal(t, "onmerge.yml", fact.Detected.Workflow)
}

func TestDetectDelivery_MissingOrDismissedChecksIgnored(t *testing.T) {
	checks := []domain.ComponentCheck{
		deployCheck(func(c *domain.ComponentCheck) { c.Triggers = []string{"push:main"}; c.Missing = true }),
		deployCheck(func(c *domain.ComponentCheck) {
			c.Triggers = []string{"push:main"}
			c.Status = domain.ModelStatusDismissed
		}),
	}
	fact, ok := detectDelivery(domain.Component{}, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryNone, fact.Detected.Mode)
}

func TestDetectDelivery_ConfirmedVercelProductionEnvironment(t *testing.T) {
	envs := []domain.ComponentEnvironment{{
		Environment: domain.EnvironmentProduction,
		Provider:    domain.CloudVercel,
		Status:      domain.LinkConfirmed,
		URL:         "https://widgets.example.com",
	}}
	comp := domain.Component{Role: domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleFrontend)}}
	fact, ok := detectDelivery(comp, nil, envs)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryOnMerge, fact.Detected.Mode)
	require.Equal(t, domain.ExecutorVercel, fact.Detected.Executor)
	require.Equal(t, domain.ConfidenceHigh, fact.Confidence)
	require.Equal(t, []domain.SmokeCheck{{Method: "GET", Path: "/"}}, fact.Detected.Verify.Smoke)
}

func TestDetectDelivery_SuggestedVercelProductionEnvironmentIsMedium(t *testing.T) {
	envs := []domain.ComponentEnvironment{{
		Environment: domain.EnvironmentProduction,
		Provider:    domain.CloudVercel,
		Status:      domain.LinkSuggested,
	}}
	// Non-frontend role: no default smoke check.
	comp := domain.Component{Role: domain.Fact[domain.ComponentRole]{Detected: ptr(domain.ComponentRoleBackend)}}
	fact, ok := detectDelivery(comp, nil, envs)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryOnMerge, fact.Detected.Mode)
	require.Equal(t, domain.ConfidenceMedium, fact.Confidence)
	require.Empty(t, fact.Detected.Verify.Smoke)
}

func TestDetectDelivery_AutoConfirmedSuggestedVercelIsHigh(t *testing.T) {
	envs := []domain.ComponentEnvironment{{
		Environment:   domain.EnvironmentProduction,
		Provider:      domain.CloudVercel,
		Status:        domain.LinkSuggested,
		AutoConfirmed: true,
	}}
	fact, ok := detectDelivery(domain.Component{}, nil, envs)
	require.True(t, ok)
	require.Equal(t, domain.ConfidenceHigh, fact.Confidence)
}

func TestDetectDelivery_DismissedVercelEnvironmentIgnored(t *testing.T) {
	envs := []domain.ComponentEnvironment{{
		Environment: domain.EnvironmentProduction,
		Provider:    domain.CloudVercel,
		Status:      domain.LinkDismissed,
	}}
	fact, ok := detectDelivery(domain.Component{}, nil, envs)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryNone, fact.Detected.Mode)
}

func TestDetectDelivery_ReleaseTagTriggeredIsBatch(t *testing.T) {
	checks := []domain.ComponentCheck{{
		Status:   domain.ModelStatusActive,
		Purpose:  domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckRelease)},
		Triggers: []string{"push:tags"},
		Workflow: ".github/workflows/release.yml",
		JobKey:   "release",
	}}
	fact, ok := detectDelivery(domain.Component{}, checks, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryBatch, fact.Detected.Mode)
	require.Equal(t, domain.ExecutorGitHubActions, fact.Detected.Executor)
	require.Equal(t, "v{version}", fact.Detected.TagPattern)
	require.Equal(t, domain.ConfidenceMedium, fact.Confidence)
}

func TestDetectDelivery_NothingDeploysIsNoneHigh(t *testing.T) {
	fact, ok := detectDelivery(domain.Component{}, nil, nil)
	require.True(t, ok)
	require.Equal(t, domain.DeliveryNone, fact.Detected.Mode)
	require.Equal(t, domain.ConfidenceHigh, fact.Confidence)
}

// --- refreshDeliveryDetection: the scan-level wiring ---

func TestRefreshDeliveryDetection_SetsDetectedAndKeepsOverride(t *testing.T) {
	store := newFakeStore()
	repo := domain.Repository{ID: uuid.New()}
	repos := newFakeRepos(repo)
	envs := newFakeEnvironmentStore()

	svc := NewService(Deps{Store: store, Repos: repos})
	svc.SetEnvironmentReader(envs)

	override := domain.ComponentDelivery{Mode: domain.DeliveryNone, AutoRollback: true}
	comp := store.seedComponent(domain.Component{
		RepositoryID: repo.ID,
		Path:         ".",
		Status:       domain.ComponentStatusActive,
		Delivery:     domain.Fact[domain.ComponentDelivery]{Override: &override},
	})
	store.seedCheck(deployCheckWith(repo.ID, comp.ID, func(c *domain.ComponentCheck) {
		c.Triggers = []string{"push:main"}
		c.Workflow = ".github/workflows/deploy.yml"
	}))

	require.NoError(t, svc.refreshDeliveryDetection(context.Background(), repo.ID))

	saved, err := store.GetComponent(context.Background(), comp.ID)
	require.NoError(t, err)
	require.NotNil(t, saved.Delivery.Detected)
	require.Equal(t, domain.DeliveryOnMerge, saved.Delivery.Detected.Mode)
	// The override survives: Get() still reads it back.
	require.NotNil(t, saved.Delivery.Override)
	require.Equal(t, domain.DeliveryNone, saved.Delivery.Get().Mode)
}

func TestRefreshDeliveryDetection_SkipsDismissedComponents(t *testing.T) {
	store := newFakeStore()
	repo := domain.Repository{ID: uuid.New()}
	repos := newFakeRepos(repo)
	svc := NewService(Deps{Store: store, Repos: repos})

	comp := store.seedComponent(domain.Component{
		RepositoryID: repo.ID,
		Path:         ".",
		Status:       domain.ComponentStatusDismissed,
	})

	require.NoError(t, svc.refreshDeliveryDetection(context.Background(), repo.ID))

	saved, err := store.GetComponent(context.Background(), comp.ID)
	require.NoError(t, err)
	require.Nil(t, saved.Delivery.Detected)
}

func deployCheckWith(repositoryID, componentID uuid.UUID, mut func(*domain.ComponentCheck)) domain.ComponentCheck {
	c := deployCheck(mut)
	c.RepositoryID = repositoryID
	c.ComponentID = componentID
	return c
}
