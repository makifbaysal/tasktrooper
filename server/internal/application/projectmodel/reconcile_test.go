package projectmodel

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func basePlanInput(repositoryID uuid.UUID) planInput {
	return planInput{
		repositoryID: repositoryID,
		scanID:       uuid.New(),
		now:          time.Now().UTC(),
		resources:    map[string]uuid.UUID{},
	}
}

func findComponent(components []domain.Component, path string) (domain.Component, bool) {
	for _, c := range components {
		if c.Path == path {
			return c, true
		}
	}
	return domain.Component{}, false
}

func TestPlanReconcileComponentOverridesSurviveRescan(t *testing.T) {
	repositoryID := uuid.New()
	componentID := uuid.New()
	overriddenName := "Custom Name"
	overriddenRole := domain.ComponentRoleFrontend
	overriddenStack := domain.ComponentStack{PackageName: "custom-pkg"}
	detectedName := "old-name"
	detectedRole := domain.ComponentRoleBackend

	existing := domain.Component{
		ID:           componentID,
		RepositoryID: repositoryID,
		Path:         "api",
		Name:         domain.Fact[string]{Detected: &detectedName, Override: &overriddenName},
		Role:         domain.Fact[domain.ComponentRole]{Detected: &detectedRole, Override: &overriddenRole},
		Stack:        domain.Fact[domain.ComponentStack]{Override: &overriddenStack},
		Status:       domain.ComponentStatusActive,
	}

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{existing}
	in.result = domain.ScanResult{Components: []domain.DetectedComponent{
		{
			Path:           "api",
			Name:           "new-name",
			Role:           domain.ComponentRoleBackend,
			RoleConfidence: domain.ConfidenceHigh,
			Stack:          domain.ComponentStack{Languages: []domain.StackItem{{Name: "go"}}},
			PackageName:    "pkg",
			DevPort:        3000,
		},
	}}

	out := planReconcile(in)
	require.Len(t, out.saveComponents, 1)
	updated := out.saveComponents[0]

	assert.Equal(t, "Custom Name", updated.Name.Get())
	assert.True(t, updated.Name.Overridden())
	assert.Equal(t, "new-name", *updated.Name.Detected)

	assert.Equal(t, domain.ComponentRoleFrontend, updated.Role.Get())
	assert.Equal(t, domain.ComponentRoleBackend, *updated.Role.Detected)

	assert.Equal(t, "custom-pkg", updated.Stack.Get().PackageName)
	assert.Equal(t, "pkg", updated.Stack.Detected.PackageName)
	assert.Equal(t, 3000, updated.Stack.Detected.DevPort)

	require.NotNil(t, updated.LastScanID)
	assert.Equal(t, in.scanID, *updated.LastScanID)
}

func TestPlanReconcileDismissedComponentSurvivesUndetected(t *testing.T) {
	repositoryID := uuid.New()
	existing := domain.Component{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Path:         "legacy",
		Status:       domain.ComponentStatusDismissed,
	}
	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{existing}

	out := planReconcile(in)
	assert.Empty(t, out.deleteComponents)
	assert.Empty(t, out.saveComponents)
	require.Len(t, out.finalComponents, 1)
	assert.Equal(t, domain.ComponentStatusDismissed, out.finalComponents[0].Status)
}

func TestPlanReconcileUntouchedVanishedComponentDeleted(t *testing.T) {
	repositoryID := uuid.New()
	existing := domain.Component{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Path:         "gone",
		Status:       domain.ComponentStatusActive,
	}
	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{existing}

	out := planReconcile(in)
	assert.Equal(t, []uuid.UUID{existing.ID}, out.deleteComponents)
	assert.Empty(t, out.finalComponents)
}

func TestPlanReconcileManuallyAddedComponentSurvivesUndetected(t *testing.T) {
	repositoryID := uuid.New()
	existing := domain.Component{
		ID:            uuid.New(),
		RepositoryID:  repositoryID,
		Path:          "hand-added",
		Status:        domain.ComponentStatusActive,
		ManuallyAdded: true,
	}
	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{existing}

	out := planReconcile(in)
	assert.Empty(t, out.deleteComponents)
	require.Len(t, out.finalComponents, 1)
}

func TestPlanReconcileCommandsMergePerPurpose(t *testing.T) {
	overriddenTest := "make test-custom"
	existing := []domain.ComponentCommand{
		{Purpose: domain.CommandTest, Command: domain.Fact[string]{Detected: ptr("go test ./..."), Override: &overriddenTest}},
		{Purpose: domain.CommandLint, Command: domain.Fact[string]{Detected: ptr("golangci-lint run")}},
	}
	detected := []domain.DetectedCommand{
		{Purpose: domain.CommandTest, Command: "go test ./... -v"},
		{Purpose: domain.CommandBuild, Command: "go build ./..."},
	}

	merged := mergeCommands(existing, detected)

	byPurpose := map[domain.CommandPurpose]domain.ComponentCommand{}
	for _, c := range merged {
		byPurpose[c.Purpose] = c
	}

	require.Contains(t, byPurpose, domain.CommandTest)
	assert.Equal(t, overriddenTest, byPurpose[domain.CommandTest].Command.Get())
	assert.Equal(t, "go test ./... -v", *byPurpose[domain.CommandTest].Command.Detected)

	require.Contains(t, byPurpose, domain.CommandBuild)
	assert.Equal(t, "go build ./...", byPurpose[domain.CommandBuild].Command.Get())

	_, lintKept := byPurpose[domain.CommandLint]
	assert.False(t, lintKept, "a purpose with no override and no detection this scan must be dropped")
}

func componentFixture(repositoryID uuid.UUID, path string, role domain.ComponentRole) domain.Component {
	name := path
	return domain.Component{
		ID:           uuid.New(),
		RepositoryID: repositoryID,
		Path:         path,
		Name:         domain.Fact[string]{Detected: &name},
		Role:         domain.Fact[domain.ComponentRole]{Detected: &role},
		Status:       domain.ComponentStatusActive,
	}
}

func TestPlanReconcileChecks(t *testing.T) {
	repositoryID := uuid.New()
	comp := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)

	t.Run("touched vanished check becomes missing", func(t *testing.T) {
		overriddenGate := domain.CheckGateInfo
		existingCheck := domain.ComponentCheck{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			ComponentID:  comp.ID,
			Source:       domain.CheckSourceCI,
			Workflow:     "ci.yml",
			JobKey:       "test",
			Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckTest)},
			Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired), Override: &overriddenGate},
			Status:       domain.ModelStatusActive,
		}
		in := basePlanInput(repositoryID)
		in.existingComponents = []domain.Component{comp}
		in.existingChecks = []domain.ComponentCheck{existingCheck}
		in.result = domain.ScanResult{Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}}}

		out := planReconcile(in)
		require.Len(t, out.saveChecks, 1)
		assert.True(t, out.saveChecks[0].Missing)
		assert.Equal(t, existingCheck.ID, out.saveChecks[0].ID)
		assert.Empty(t, out.deleteChecks)
	})

	t.Run("untouched vanished check is deleted", func(t *testing.T) {
		existingCheck := domain.ComponentCheck{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			ComponentID:  comp.ID,
			Source:       domain.CheckSourceCI,
			Workflow:     "ci.yml",
			JobKey:       "test",
			Purpose:      domain.Fact[domain.CheckPurpose]{Detected: ptr(domain.CheckTest)},
			Gate:         domain.Fact[domain.CheckGate]{Detected: ptr(domain.CheckGateRequired)},
			Status:       domain.ModelStatusActive,
		}
		in := basePlanInput(repositoryID)
		in.existingComponents = []domain.Component{comp}
		in.existingChecks = []domain.ComponentCheck{existingCheck}
		in.result = domain.ScanResult{Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}}}

		out := planReconcile(in)
		assert.Empty(t, out.saveChecks)
		assert.Equal(t, []uuid.UUID{existingCheck.ID}, out.deleteChecks)
	})

	t.Run("manual checks are never touched", func(t *testing.T) {
		manual := domain.ComponentCheck{
			ID:           uuid.New(),
			RepositoryID: repositoryID,
			ComponentID:  comp.ID,
			Source:       domain.CheckSourceManual,
			Workflow:     "",
			JobKey:       "manual-smoke",
			Status:       domain.ModelStatusActive,
		}
		in := basePlanInput(repositoryID)
		in.existingComponents = []domain.Component{comp}
		in.existingChecks = []domain.ComponentCheck{manual}
		in.result = domain.ScanResult{Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}}}

		out := planReconcile(in)
		assert.Empty(t, out.saveChecks)
		assert.Empty(t, out.deleteChecks)
		require.Len(t, out.finalChecks, 1)
		assert.Equal(t, manual.ID, out.finalChecks[0].ID)
	})

	t.Run("new CI check confidence lands on the purpose fact", func(t *testing.T) {
		in := basePlanInput(repositoryID)
		in.existingComponents = []domain.Component{comp}
		in.result = domain.ScanResult{
			Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}},
			Checks: []domain.DetectedCheck{
				{ComponentPath: "api", Workflow: "ci.yml", JobKey: "test", Purpose: domain.CheckTest, Confidence: domain.ConfidenceHigh, LocalCommands: []domain.LocalCommand{{Argv: []string{"go", "test", "./..."}}}},
			},
		}

		out := planReconcile(in)
		require.Len(t, out.saveChecks, 1)
		created := out.saveChecks[0]
		assert.Equal(t, domain.ConfidenceHigh, created.Purpose.Confidence)
		assert.Equal(t, domain.CheckGateRequired, created.Gate.Get())
		assert.Equal(t, domain.CheckSourceCI, created.Source)
	})
}

func linkFixture(repositoryID, fromComponentID uuid.UUID, signalKey string, status domain.LinkStatus, source domain.LinkSource) domain.ComponentLink {
	return domain.ComponentLink{
		ID:              uuid.New(),
		RepositoryID:    repositoryID,
		FromComponentID: fromComponentID,
		SignalKey:       signalKey,
		Status:          status,
		Source:          source,
	}
}

func TestPlanReconcileLinksDismissedSurvivesAsMissing(t *testing.T) {
	repositoryID := uuid.New()
	comp := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)
	dismissed := linkFixture(repositoryID, comp.ID, "sdk:stripe", domain.LinkDismissed, domain.LinkSourceScan)

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{comp}
	in.existingLinks = []domain.ComponentLink{dismissed}
	in.result = domain.ScanResult{Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}}}

	out := planReconcile(in)
	require.Len(t, out.saveLinks, 1)
	assert.Equal(t, domain.LinkDismissed, out.saveLinks[0].Status)
	assert.True(t, out.saveLinks[0].Missing)
	assert.Empty(t, out.deleteLinks)
}

func TestPlanReconcileLinksUntouchedVanishedDeleted(t *testing.T) {
	repositoryID := uuid.New()
	comp := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)
	confirmed := linkFixture(repositoryID, comp.ID, "sdk:stripe", domain.LinkConfirmed, domain.LinkSourceScan)

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{comp}
	in.existingLinks = []domain.ComponentLink{confirmed}
	in.result = domain.ScanResult{Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}}}

	out := planReconcile(in)
	assert.Empty(t, out.saveLinks)
	assert.Equal(t, []uuid.UUID{confirmed.ID}, out.deleteLinks)
}

func TestPlanReconcileLinksUserLinkNeverTouched(t *testing.T) {
	repositoryID := uuid.New()
	comp := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)
	userLink := linkFixture(repositoryID, comp.ID, "", domain.LinkConfirmed, domain.LinkSourceUser)

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{comp}
	in.existingLinks = []domain.ComponentLink{userLink}
	in.result = domain.ScanResult{Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}}}

	out := planReconcile(in)
	assert.Empty(t, out.saveLinks)
	assert.Empty(t, out.deleteLinks)
	require.Len(t, out.finalLinks, 1)
	assert.Equal(t, userLink.ID, out.finalLinks[0].ID)
}

func TestPlanReconcileNewLinkStatusByConfidence(t *testing.T) {
	repositoryID := uuid.New()
	comp := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)
	resourceID := uuid.New()

	tests := []struct {
		name        string
		confidence  domain.Confidence
		wantCreated bool
		wantStatus  domain.LinkStatus
		wantAutoOK  bool
	}{
		{"exact confirms and auto-confirms", domain.ConfidenceExact, true, domain.LinkConfirmed, true},
		{"high confirms without auto-confirm", domain.ConfidenceHigh, true, domain.LinkConfirmed, false},
		{"medium suggests", domain.ConfidenceMedium, true, domain.LinkSuggested, false},
		{"low is dropped", domain.ConfidenceLow, false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := basePlanInput(repositoryID)
			in.existingComponents = []domain.Component{comp}
			in.resources = map[string]uuid.UUID{"api|db:primary": resourceID}
			in.result = domain.ScanResult{
				Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}},
				Links: []domain.DetectedLink{
					{
						ComponentPath: "api",
						SignalKey:     "db:primary",
						Target:        domain.LinkTarget{Kind: domain.LinkTargetResource},
						Confidence:    tt.confidence,
					},
				},
			}

			out := planReconcile(in)
			if !tt.wantCreated {
				assert.Empty(t, out.saveLinks)
				return
			}
			require.Len(t, out.saveLinks, 1)
			assert.Equal(t, tt.wantStatus, out.saveLinks[0].Status)
			assert.Equal(t, tt.wantAutoOK, out.saveLinks[0].AutoConfirmed)
			require.NotNil(t, out.saveLinks[0].ToResourceID)
			assert.Equal(t, resourceID, *out.saveLinks[0].ToResourceID)
		})
	}
}

func TestPlanReconcileUnresolvedLinkMatchedByPackage(t *testing.T) {
	repositoryID := uuid.New()
	source := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)

	otherRepoID := uuid.New()
	targetStack := domain.ComponentStack{PackageName: "@acme/billing"}
	target := componentWithStack("services/billing", targetStack)
	target.RepositoryID = otherRepoID

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{source}
	in.matchCandidates = []matchCandidate{{Component: target, RepositoryID: otherRepoID, RepositoryName: "billing-repo"}}
	in.result = domain.ScanResult{
		Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}},
		Links: []domain.DetectedLink{
			{
				ComponentPath: "api",
				SignalKey:     "pkg:billing",
				Target:        domain.LinkTarget{Kind: domain.LinkTargetUnresolved, PackageName: "@acme/billing"},
				Confidence:    domain.ConfidenceLow,
			},
		},
	}

	out := planReconcile(in)
	require.Len(t, out.saveLinks, 1)
	link := out.saveLinks[0]
	assert.Equal(t, domain.LinkConfirmed, link.Status)
	assert.True(t, link.AutoConfirmed)
	require.NotNil(t, link.ToComponentID)
	assert.Equal(t, target.ID, *link.ToComponentID)
	assert.Contains(t, link.Reason, "package")
}

func TestPlanReconcileUnresolvedLinkMatchedByName(t *testing.T) {
	repositoryID := uuid.New()
	source := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)

	otherRepoID := uuid.New()
	target := componentWithStack("apps/billing", domain.ComponentStack{})
	target.RepositoryID = otherRepoID

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{source}
	in.matchCandidates = []matchCandidate{{Component: target, RepositoryID: otherRepoID, RepositoryName: "billing-service"}}
	in.result = domain.ScanResult{
		Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}},
		Links: []domain.DetectedLink{
			{
				ComponentPath: "api",
				SignalKey:     "env:BILLING_URL",
				Target:        domain.LinkTarget{Kind: domain.LinkTargetUnresolved, ServiceHint: "billing-svc"},
				Confidence:    domain.ConfidenceLow,
			},
		},
	}

	out := planReconcile(in)
	require.Len(t, out.saveLinks, 1)
	link := out.saveLinks[0]
	assert.Equal(t, domain.LinkSuggested, link.Status)
	assert.False(t, link.AutoConfirmed)
	require.NotNil(t, link.ToComponentID)
	assert.Equal(t, target.ID, *link.ToComponentID)
}

func TestPlanReconcileSelfLinkNeverCreated(t *testing.T) {
	repositoryID := uuid.New()
	comp := componentFixture(repositoryID, "api", domain.ComponentRoleBackend)

	in := basePlanInput(repositoryID)
	in.existingComponents = []domain.Component{comp}
	in.result = domain.ScanResult{
		Components: []domain.DetectedComponent{{Path: "api", Name: "api", Role: domain.ComponentRoleBackend}},
		Links: []domain.DetectedLink{
			{
				ComponentPath: "api",
				SignalKey:     "self:import",
				Target:        domain.LinkTarget{Kind: domain.LinkTargetComponent, ComponentPath: "api"},
				Confidence:    domain.ConfidenceExact,
			},
		},
	}

	out := planReconcile(in)
	assert.Empty(t, out.saveLinks)
	assert.Empty(t, out.finalLinks)
}
