package domain

type ResourceBlock struct {
	Resource string `json:"resource"`
	Detail   string `json:"detail,omitempty"`
}

const ResourceMobileDevice = "mobile_device"

const ResourceClaudeCodeQuota = "llm_provider_code_quota"

const ResourceDeployWatch = "deploy_watch"

const ResourceWorkOrder = "work_order"

const ResourceHumanDecision = "human_decision"

// ResourceReleaseWatch parks the release engineer's card while the release
// sweeper watches the deploy and the soak window; it is handed back only when
// a verdict is needed or something failed.
const ResourceReleaseWatch = "release_watch"

func ValidResource(name string) bool {
	return name == ResourceMobileDevice || name == ResourceClaudeCodeQuota ||
		name == ResourceDeployWatch || name == ResourceWorkOrder ||
		name == ResourceHumanDecision || name == ResourceReleaseWatch
}
