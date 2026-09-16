package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestDeveloperAgentForKind(t *testing.T) {
	backend := domain.RepoSubProject{Path: "backend", Kind: domain.RepoKindBackend}
	mobile := domain.RepoSubProject{Path: "mobile", Kind: domain.RepoKindMobile}
	web := domain.RepoSubProject{Path: "web", Kind: domain.RepoKindFrontend}
	admin := domain.RepoSubProject{Path: "admin", Kind: domain.RepoKindFrontend}

	cases := []struct {
		name string
		kind string
		subs []domain.RepoSubProject
		want string
	}{
		{"backend", domain.RepoKindBackend, nil, domain.AgentBackendDeveloper},
		{"worker", domain.RepoKindWorker, nil, domain.AgentBackendDeveloper},
		{"frontend", domain.RepoKindFrontend, nil, domain.AgentFrontendDeveloper},
		{"mobile", domain.RepoKindMobile, nil, domain.AgentMobileDeveloper},
		{"undetected kind", "", nil, domain.AgentBackendDeveloper},
		{"monorepo without sub-projects", domain.RepoKindMonorepo, nil, domain.AgentBackendDeveloper},
		{"monorepo tie goes to backend", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile, backend}, domain.AgentBackendDeveloper},
		{"monorepo mostly frontend", domain.RepoKindMonorepo, []domain.RepoSubProject{web, backend, admin}, domain.AgentFrontendDeveloper},
		{"monorepo only mobile", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile}, domain.AgentMobileDeveloper},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.DeveloperAgentForKind(tc.kind, tc.subs); got != tc.want {
				t.Fatalf("DeveloperAgentForKind(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestResolveAnalizArea(t *testing.T) {
	backend := domain.RepoSubProject{Path: "backend", Kind: domain.RepoKindBackend}
	mobile := domain.RepoSubProject{Path: "mobile", Kind: domain.RepoKindMobile}
	web := domain.RepoSubProject{Path: "web", Kind: domain.RepoKindFrontend}
	admin := domain.RepoSubProject{Path: "admin", Kind: domain.RepoKindFrontend}

	cases := []struct {
		name string
		kind string
		subs []domain.RepoSubProject
		want string
	}{
		{"backend", domain.RepoKindBackend, nil, domain.RepoKindBackend},
		{"worker", domain.RepoKindWorker, nil, domain.RepoKindBackend},
		{"frontend", domain.RepoKindFrontend, nil, domain.RepoKindFrontend},
		{"mobile", domain.RepoKindMobile, nil, domain.RepoKindMobile},
		{"undetected kind", "", nil, domain.RepoKindBackend},
		{"monorepo without sub-projects", domain.RepoKindMonorepo, nil, domain.RepoKindBackend},
		{"monorepo tie goes to backend", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile, backend}, domain.RepoKindBackend},
		{"monorepo mostly frontend", domain.RepoKindMonorepo, []domain.RepoSubProject{web, backend, admin}, domain.RepoKindFrontend},
		{"monorepo only mobile", domain.RepoKindMonorepo, []domain.RepoSubProject{mobile}, domain.RepoKindMobile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.ResolveAnalizArea(tc.kind, tc.subs); got != tc.want {
				t.Fatalf("ResolveAnalizArea(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestAnalizAssigneeForArea(t *testing.T) {
	cases := []struct {
		name string
		s    domain.AppSettings
		area string
		want string
	}{
		{"unset backend falls back to system-architect", domain.AppSettings{}, domain.RepoKindBackend, domain.AgentSystemArchitect},
		{"unset frontend falls back to system-architect", domain.AppSettings{}, domain.RepoKindFrontend, domain.AgentSystemArchitect},
		{"unset mobile falls back to system-architect", domain.AppSettings{}, domain.RepoKindMobile, domain.AgentSystemArchitect},
		{
			"configured backend agent wins",
			domain.AppSettings{AnalizAssigneeBackend: "backend-developer"},
			domain.RepoKindBackend,
			"backend-developer",
		},
		{
			"configured frontend agent wins",
			domain.AppSettings{AnalizAssigneeFrontend: "frontend-developer"},
			domain.RepoKindFrontend,
			"frontend-developer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.AnalizAssigneeForArea(tc.s, tc.area); got != tc.want {
				t.Fatalf("AnalizAssigneeForArea() = %q, want %q", got, tc.want)
			}
		})
	}
}
