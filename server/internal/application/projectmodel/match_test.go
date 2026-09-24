package projectmodel

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func componentWithStack(path string, stack domain.ComponentStack) domain.Component {
	name := path
	return domain.Component{
		ID:    uuid.New(),
		Path:  path,
		Name:  domain.Fact[string]{Detected: &name},
		Stack: domain.Fact[domain.ComponentStack]{Detected: &stack},
	}
}

func TestMatchByPackageExact(t *testing.T) {
	billing := componentWithStack("services/billing", domain.ComponentStack{PackageName: "@acme/billing"})
	candidates := []matchCandidate{
		{Component: billing, RepositoryID: uuid.New(), RepositoryName: "billing-repo"},
		{Component: componentWithStack("services/other", domain.ComponentStack{PackageName: "@acme/other"}), RepositoryID: uuid.New(), RepositoryName: "other-repo"},
	}

	res, ok := matchLink(domain.LinkTarget{PackageName: "@acme/billing"}, candidates)
	require.True(t, ok)
	assert.Equal(t, domain.ConfidenceExact, res.Confidence)
	assert.Equal(t, billing.ID, res.Candidate.Component.ID)
	assert.Contains(t, res.Reason, "@acme/billing")
}

func TestMatchByPackageNoMatch(t *testing.T) {
	candidates := []matchCandidate{
		{Component: componentWithStack("services/other", domain.ComponentStack{PackageName: "@acme/other"}), RepositoryID: uuid.New(), RepositoryName: "other-repo"},
	}
	_, ok := matchLink(domain.LinkTarget{PackageName: "@acme/billing"}, candidates)
	assert.False(t, ok)
}

func TestMatchByNameStripsServiceSuffix(t *testing.T) {
	comp := componentWithStack("apps/billing", domain.ComponentStack{})
	candidates := []matchCandidate{
		{Component: comp, RepositoryID: uuid.New(), RepositoryName: "billing-service"},
	}

	res, ok := matchLink(domain.LinkTarget{ServiceHint: "billing-svc"}, candidates)
	require.True(t, ok)
	assert.Equal(t, domain.ConfidenceMedium, res.Confidence)
	assert.Equal(t, comp.ID, res.Candidate.Component.ID)
}

func TestMatchByNameAddsPortToReason(t *testing.T) {
	stack := domain.ComponentStack{DevPort: 4000}
	comp := componentWithStack("apps/billing", stack)
	candidates := []matchCandidate{
		{Component: comp, RepositoryID: uuid.New(), RepositoryName: "billing"},
	}

	res, ok := matchLink(domain.LinkTarget{ServiceHint: "billing", Port: 4000}, candidates)
	require.True(t, ok)
	assert.Contains(t, res.Reason, "port 4000")
}

func TestMatchByNamePrefersSharedProjectOnTie(t *testing.T) {
	shared := componentWithStack("apps/billing", domain.ComponentStack{})
	unrelated := componentWithStack("apps/billing", domain.ComponentStack{})
	candidates := []matchCandidate{
		{Component: unrelated, RepositoryID: uuid.New(), RepositoryName: "aaa-billing", SharesProject: false},
		{Component: shared, RepositoryID: uuid.New(), RepositoryName: "zzz-billing", SharesProject: true},
	}

	res, ok := matchLink(domain.LinkTarget{ServiceHint: "billing"}, candidates)
	require.True(t, ok)
	assert.Equal(t, shared.ID, res.Candidate.Component.ID)
}

func TestMatchByNameTieBreaksByRepoNameThenPath(t *testing.T) {
	first := componentWithStack("apps/billing", domain.ComponentStack{})
	second := componentWithStack("apps/billing", domain.ComponentStack{})
	candidates := []matchCandidate{
		{Component: second, RepositoryID: uuid.New(), RepositoryName: "zzz-billing"},
		{Component: first, RepositoryID: uuid.New(), RepositoryName: "aaa-billing"},
	}

	res, ok := matchLink(domain.LinkTarget{ServiceHint: "billing"}, candidates)
	require.True(t, ok)
	assert.Equal(t, first.ID, res.Candidate.Component.ID)
}

func TestMatchLinkTriesPackageBeforeName(t *testing.T) {
	byName := componentWithStack("apps/billing", domain.ComponentStack{})
	byPackage := componentWithStack("apps/other", domain.ComponentStack{PackageName: "@acme/billing"})
	candidates := []matchCandidate{
		{Component: byName, RepositoryID: uuid.New(), RepositoryName: "billing"},
		{Component: byPackage, RepositoryID: uuid.New(), RepositoryName: "other"},
	}

	res, ok := matchLink(domain.LinkTarget{ServiceHint: "billing", PackageName: "@acme/billing"}, candidates)
	require.True(t, ok)
	assert.Equal(t, byPackage.ID, res.Candidate.Component.ID)
	assert.Equal(t, domain.ConfidenceExact, res.Confidence)
}

func TestMatchLinkNoCandidatesNoMatch(t *testing.T) {
	_, ok := matchLink(domain.LinkTarget{ServiceHint: "billing"}, nil)
	assert.False(t, ok)
}
