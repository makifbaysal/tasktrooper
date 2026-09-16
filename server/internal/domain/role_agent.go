package domain

// Role agents that author files in a repository. The system architect is
// deliberately not one of them: it designs and reviews, and it refuses a task
// that asks it to write code, docs or workflows into a branch.
const (
	AgentBackendDeveloper  = "backend-developer"
	AgentFrontendDeveloper = "frontend-developer"
	AgentMobileDeveloper   = "mobile-developer"
	// AgentSystemArchitect is the default assignee for an analiz task: it
	// designs and reviews rather than writing code, and it is the only role
	// whose tool policy (architectToolPolicy) is guaranteed to carry every
	// tool RequiredAnalizTools names, so routing to it never needs a tool-grant
	// confirmation.
	AgentSystemArchitect = "system-architect"
)

// DeveloperAgentForKind names the role agent that owns hands-on work (docs,
// CI/CD workflows, deploy setup, incident fixes) for a repository kind.
//
// A monorepo has no developer of its own, so it resolves to the developer that
// owns most of its sub-projects. Ties go to backend, then frontend, then
// mobile, and a monorepo with no recorded sub-projects falls back to backend.
func DeveloperAgentForKind(kind string, subProjects []RepoSubProject) string {
	return agentForArea(ResolveAnalizArea(kind, subProjects))
}

// ResolveAnalizArea names the backend/frontend/mobile area that owns a
// repository, the same area concept DeveloperAgentForKind resolves to an
// agent — an analiz assignment setting picks an agent per AREA, not per repo
// kind directly, because a monorepo has no kind of its own to key on.
func ResolveAnalizArea(kind string, subProjects []RepoSubProject) string {
	if kind != RepoKindMonorepo {
		return areaForSingleKind(kind)
	}
	counts := make(map[string]int, 3)
	for _, sp := range subProjects {
		if sp.Kind == RepoKindMonorepo {
			continue
		}
		counts[areaForSingleKind(sp.Kind)]++
	}
	best, bestCount := RepoKindBackend, 0
	for _, area := range []string{RepoKindBackend, RepoKindFrontend, RepoKindMobile} {
		if counts[area] > bestCount {
			best, bestCount = area, counts[area]
		}
	}
	return best
}

func areaForSingleKind(kind string) string {
	switch kind {
	case RepoKindFrontend:
		return RepoKindFrontend
	case RepoKindMobile:
		return RepoKindMobile
	default: // backend, worker, or not detected yet
		return RepoKindBackend
	}
}

func agentForArea(area string) string {
	switch area {
	case RepoKindFrontend:
		return AgentFrontendDeveloper
	case RepoKindMobile:
		return AgentMobileDeveloper
	default:
		return AgentBackendDeveloper
	}
}

// AnalizAssigneeForArea resolves the agent name an analiz task for the given
// area (RepoKindBackend/Frontend/Mobile) should be assigned to, from the
// backend/frontend/mobile analiz-assignment settings. An unset field falls
// back to AgentSystemArchitect, the same default SettingsStore.Get applies
// when the app_settings rows do not exist yet.
func AnalizAssigneeForArea(s AppSettings, area string) string {
	var v string
	switch area {
	case RepoKindFrontend:
		v = s.AnalizAssigneeFrontend
	case RepoKindMobile:
		v = s.AnalizAssigneeMobile
	default:
		v = s.AnalizAssigneeBackend
	}
	if v == "" {
		return AgentSystemArchitect
	}
	return v
}
