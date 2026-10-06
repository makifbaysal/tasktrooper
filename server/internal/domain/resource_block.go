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

// ResourceAnalysisQuestions parks an analiz task that recorded a blocking
// open question: the human answers it on the report page, not by a sweeper
// noticing a resource is free, so this is released by
// repository.Service.SubmitQuestions rather than a *_sweeper.go.
const ResourceAnalysisQuestions = "analysis_questions"

// The merge holds record why release.Service.MergeGate is refusing a done
// task's merge, so the card says what it waits for instead of the reason
// living only in a comment. Like work_order they never move board_column. The
// gate itself sets and clears them, so ValidResource does not accept them: an
// agent parking itself on one would be a hold nothing ever re-evaluates.
const (
	// ResourceDeployOrder: a deploy_depends_on task is not released yet.
	// Released automatically when it is.
	ResourceDeployOrder = "deploy_order"
	// ResourceBeforeDeploy: human-written before-deploy steps are unconfirmed.
	ResourceBeforeDeploy = "before_deploy"
	// ResourceDeliveryProfile: the component's delivery profile is unconfirmed.
	ResourceDeliveryProfile = "delivery_profile"
	// ResourceDeployEnv: the deploy target lacks environment variables a
	// human has to enter, or they could not be read.
	ResourceDeployEnv = "deploy_env"
)

var MergeHoldResources = []string{ResourceDeployOrder, ResourceBeforeDeploy, ResourceDeliveryProfile, ResourceDeployEnv}

func IsMergeHold(resource string) bool {
	for _, r := range MergeHoldResources {
		if r == resource {
			return true
		}
	}
	return false
}

func ValidResource(name string) bool {
	return name == ResourceMobileDevice || name == ResourceClaudeCodeQuota ||
		name == ResourceDeployWatch || name == ResourceWorkOrder ||
		name == ResourceHumanDecision || name == ResourceReleaseWatch ||
		name == ResourceAnalysisQuestions
}
