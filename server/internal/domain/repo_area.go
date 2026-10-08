package domain

// RepoArea names the backend/frontend/mobile/data/game area that owns a
// repository — what a role assignment's Areas is scoped by, and what
// RoleResolver.AgentForRole matches against. A monorepo has no kind of its own
// to key on, so it resolves to the area that owns most of its sub-projects;
// ties go to backend, then frontend, mobile, data, game, and a monorepo with no
// recorded sub-projects falls back to backend.
func RepoArea(kind string, subProjects []RepoSubProject) string {
	if kind != RepoKindMonorepo {
		return areaForSingleKind(kind)
	}
	counts := make(map[string]int, 5)
	for _, sp := range subProjects {
		if sp.Kind == RepoKindMonorepo {
			continue
		}
		counts[areaForSingleKind(sp.Kind)]++
	}
	best, bestCount := RepoKindBackend, 0
	for _, area := range RoleAreas() {
		if counts[area] > bestCount {
			best, bestCount = area, counts[area]
		}
	}
	return best
}

// RoleAreas is every area RepoArea can resolve to, in its tie-break order.
func RoleAreas() []string {
	return []string{RepoKindBackend, RepoKindFrontend, RepoKindMobile, RepoKindData, RepoKindGame}
}

func areaForSingleKind(kind string) string {
	switch kind {
	case RepoKindFrontend, RepoKindMobile, RepoKindData, RepoKindGame:
		return kind
	default: // backend, worker, or not detected yet
		return RepoKindBackend
	}
}

// AreaFallbacks is where a role lookup goes when nobody covers area itself:
// the data and game areas are newer than most installs' role assignments, and
// a repository that used to resolve to backend must not end up with nobody.
func AreaFallbacks(area string) []string {
	switch area {
	case RepoKindData, RepoKindGame:
		return []string{RepoKindBackend}
	default:
		return nil
	}
}
