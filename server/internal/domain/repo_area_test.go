package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestRepoArea(t *testing.T) {
	backend := domain.RepoSubProject{Path: "backend", Kind: domain.RepoKindBackend}
	mobile := domain.RepoSubProject{Path: "mobile", Kind: domain.RepoKindMobile}
	web := domain.RepoSubProject{Path: "web", Kind: domain.RepoKindFrontend}
	admin := domain.RepoSubProject{Path: "admin", Kind: domain.RepoKindFrontend}
	data := domain.RepoSubProject{Path: "pipelines", Kind: domain.RepoKindData}
	notebooks := domain.RepoSubProject{Path: "research", Kind: domain.RepoKindData}
	game := domain.RepoSubProject{Path: "game", Kind: domain.RepoKindGame}

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
		{"data", domain.RepoKindData, nil, domain.RepoKindData},
		{"game", domain.RepoKindGame, nil, domain.RepoKindGame},
		{"monorepo mostly data", domain.RepoKindMonorepo, []domain.RepoSubProject{data, notebooks, backend}, domain.RepoKindData},
		{"monorepo tie between mobile and game goes to mobile", domain.RepoKindMonorepo, []domain.RepoSubProject{game, mobile}, domain.RepoKindMobile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.RepoArea(tc.kind, tc.subs); got != tc.want {
				t.Fatalf("RepoArea(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestAreaFallbacks(t *testing.T) {
	for _, area := range []string{domain.RepoKindData, domain.RepoKindGame} {
		if got := domain.AreaFallbacks(area); len(got) != 1 || got[0] != domain.RepoKindBackend {
			t.Fatalf("AreaFallbacks(%q) = %v, want [backend]", area, got)
		}
	}
	for _, area := range []string{domain.RepoKindBackend, domain.RepoKindFrontend, domain.RepoKindMobile} {
		if got := domain.AreaFallbacks(area); got != nil {
			t.Fatalf("AreaFallbacks(%q) = %v, want none", area, got)
		}
	}
}
