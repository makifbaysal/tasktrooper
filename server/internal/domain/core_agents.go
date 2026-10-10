package domain

var CoreAgentSlugs = []string{
	"product-manager",
	"system-architect",
	"qa-agent",
	"security-agent",
	"release-engineer",
}

func IsCoreAgentSlug(slug string) bool {
	for _, core := range CoreAgentSlugs {
		if core == slug {
			return true
		}
	}
	return false
}
