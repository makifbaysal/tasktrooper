package evolution

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

// Golden results keep the before/after pair they always had: "before" is the current setup, "after" what the probe adopted.
const (
	goldenPhaseBefore = "before"
	goldenPhaseAfter  = "after"
)

// The judge stays on its own model when one is configured, so the grader is not literally the weights that proposed the changes; with no override it is the agent's ModelHeavy — keep-or-drop on a prompt change is a hard-tier decision.
func (s *Service) judgeRouting(agentRec domain.Agent) (string, domain.LLMProviderType) {
	model, provider := agentRec.Model, agentRec.ProviderType
	if agentRec.ModelHeavy != "" {
		model = agentRec.ModelHeavy
	}
	if s.cfg.Model != "" {
		model = s.cfg.Model
	}
	if s.cfg.ProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.ProviderType)
	}
	if s.cfg.JudgeModel != "" {
		model = s.cfg.JudgeModel
	}
	if s.cfg.JudgeProviderType != "" {
		provider = domain.LLMProviderType(s.cfg.JudgeProviderType)
	}
	return model, provider
}
